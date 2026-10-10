package resume

import (
	"bytes"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/ledongthuc/pdf"
)

// PDF text layers are not always ASCII. Chromium/Skia output — and anything
// produced by "print to PDF" from a browser — can embed subset fonts whose
// /Encoding is a Dict with Differences and whose real mapping lives only in the
// font's /ToUnicode CMap. The parsing library prefers the Differences path in
// that case, so every glyph comes back as a substituted character:
//
//	7=]JlR=_   ;Y;il;\F^\HjP;   r;ZHiP;\FsPfËN[;PZD^[
//
// which parses to zero profile fields even though the document is fine.
//
// The same library does expose what is needed to correct it: every text run
// carries the BaseFont it was drawn with (page.go, Text.Font) and the page's
// font resources are reachable. So instead of a second PDF engine we re-decode
// each run with its own font's /ToUnicode table. No new dependency, and a
// document that already decodes correctly is never touched.

// unicodeCMap maps a font's character codes to the text they represent.
type unicodeCMap map[rune]string

func (m unicodeCMap) apply(s string) string {
	if len(m) == 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if u, ok := m[r]; ok {
			b.WriteString(u)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

var (
	cmapBlockRe       = regexp.MustCompile(`(?s)beginbfchar(.*?)endbfchar`)
	cmapRangeRe       = regexp.MustCompile(`(?s)beginbfrange(.*?)endbfrange`)
	cmapPairRe        = regexp.MustCompile(`<([0-9A-Fa-f]+)>\s*<([0-9A-Fa-f]+)>`)
	cmapArrayRe       = regexp.MustCompile(`<([0-9A-Fa-f]+)>\s*<([0-9A-Fa-f]+)>\s*\[([^\]]*)\]`)
	cmapRangeSimpleRe = regexp.MustCompile(`<([0-9A-Fa-f]+)>\s*<([0-9A-Fa-f]+)>\s*<([0-9A-Fa-f]+)>`)
	cmapHexWordRe     = regexp.MustCompile(`<([0-9A-Fa-f]+)>`)
)

// parseToUnicodeCMap reads a /ToUnicode stream and returns its code -> text map.
// A malformed or empty CMap yields nil, and the caller keeps the original text.
func parseToUnicodeCMap(r io.Reader) unicodeCMap {
	if r == nil {
		return nil
	}

	// CMaps are small; the limit keeps a hostile stream from being read whole.
	raw, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil || len(raw) == 0 {
		return nil
	}
	body := string(raw)
	if !strings.Contains(body, "beginbf") {
		return nil
	}

	out := make(unicodeCMap)

	// bfchar: <code> <text>
	for _, block := range cmapBlockRe.FindAllStringSubmatch(body, -1) {
		for _, pair := range cmapPairRe.FindAllStringSubmatch(block[1], -1) {
			code, ok := hexCode(pair[1])
			if !ok {
				continue
			}
			if text, ok := utf16Hex(pair[2]); ok {
				out[rune(code)] = text
			}
		}
	}

	// bfrange: <lo> <hi> <first> or <lo> <hi> [<text> ...]
	for _, block := range cmapRangeRe.FindAllStringSubmatch(body, -1) {
		inner := block[1]
		for _, m := range cmapArrayRe.FindAllStringSubmatch(inner, -1) {
			lo, okLo := hexCode(m[1])
			if !okLo {
				continue
			}
			for i, hex := range cmapHexWordRe.FindAllStringSubmatch(m[3], -1) {
				if text, ok := utf16Hex(hex[1]); ok {
					out[rune(lo+i)] = text
				}
			}
		}
		for _, m := range cmapRangeSimpleRe.FindAllStringSubmatch(cmapArrayRe.ReplaceAllString(inner, ""), -1) {
			lo, okLo := hexCode(m[1])
			hi, okHi := hexCode(m[2])
			if !okLo || !okHi || hi < lo || hi-lo > 0xFFFF {
				continue
			}
			// The third value is the destination of the range's first code;
			// the remaining codes follow it.
			dest, okDest := utf16Hex(m[3])
			if !okDest {
				continue
			}
			first, okFirst := runeOfFirst(dest)
			if !okFirst {
				continue
			}
			for c := lo; c <= hi; c++ {
				out[rune(c)] = string(first + rune(c-lo))
			}
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// hexCode parses a CMap hexadecimal code into an integer code point.
func hexCode(hex string) (int, bool) {
	if hex == "" || len(hex) > 8 {
		return 0, false
	}
	v, err := strconv.ParseInt(hex, 16, 32)
	if err != nil {
		return 0, false
	}
	return int(v), true
}

// utf16Hex decodes a CMap destination, which is UTF-16BE and may hold several
// code units (ligatures such as "ff" arrive as one destination).
func utf16Hex(hex string) (string, bool) {
	if hex == "" || len(hex)%4 != 0 || len(hex) > 64 {
		return "", false
	}
	units := make([]uint16, 0, len(hex)/4)
	for i := 0; i < len(hex); i += 4 {
		v, err := strconv.ParseUint(hex[i:i+4], 16, 32)
		if err != nil {
			return "", false
		}
		units = append(units, uint16(v))
	}
	return string(utf16.Decode(units)), true
}

// runeOfFirst returns the single rune a one-unit destination represents, used
// as the base for an ascending bfrange.
func runeOfFirst(s string) (rune, bool) {
	rs := []rune(s)
	if len(rs) != 1 {
		return 0, false
	}
	return rs[0], true
}

// extractPDFTextWithFontCMaps re-decodes a document whose text layer the library
// substituted, by walking each page's content stream and decoding every
// show-string with the font that was current at that point (see pdfcontent.go).
// It reports false when no page yielded text, so the caller keeps what it had.
func extractPDFTextWithFontCMaps(data []byte) (string, bool) {
	var pages []string
	ok := func() bool {
		defer func() { _ = recover() }()
		reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return false
		}
		for i := 1; i <= reader.NumPage(); i++ {
			page := reader.Page(i)
			if page.V.IsNull() {
				continue
			}
			if lines := extractPageLines(page); len(lines) > 0 {
				pages = append(pages, strings.Join(lines, "\n"))
			}
		}
		return len(pages) > 0
	}()
	if !ok {
		return "", false
	}
	return NormalizeText(strings.Join(pages, "\n")), true
}

// looksLikeProse reports whether extracted text reads as language rather than a
// mis-decoded character soup. Used only to decide whether a repair attempt is
// worthwhile; it never rejects text on its own.
func looksLikeProse(text string) bool {
	return proseRatio(text) >= 0.55
}

// proseRatio returns the share of whitespace-separated tokens that are words:
// two or more ASCII letters and nothing else.
func proseRatio(text string) float64 {
	fields := strings.Fields(text)
	if len(fields) < 10 {
		return 0
	}
	words := 0
	for _, f := range fields {
		n := 0
		ok := true
		for _, r := range f {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
				n++
			case r >= '0' && r <= '9', r == '\'', r == '-', r == '.', r == ',', r == '(', r == ')', r == '/', r == '+', r == '@', r == ':':
			default:
				ok = false
			}
			if !ok {
				break
			}
		}
		if ok && n >= 2 {
			words++
		}
	}
	return float64(words) / float64(len(fields))
}
