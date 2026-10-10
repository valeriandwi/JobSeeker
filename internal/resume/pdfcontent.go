package resume

import (
	"bytes"
	"io"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
)

// Content-stream text decoder.
//
// This exists for one narrow case: a document whose text layer the parsing
// library decodes into substituted characters because the font carries a
// Differences encoding, which makes the library ignore the font's /ToUnicode
// map (Chromium/Skia "print to PDF" output does this with Type 3 fonts).
//
// Repairing that needs one thing the library does not expose: which font each
// glyph was drawn with. The font maps are reachable (page resources) and so are
// the page's content streams, so this decoder walks the text operators itself
// and decodes each show-string with the font that is current at that point.
//
// It is deliberately not a general PDF engine: it handles the text operators
// that lay out a document (Tf, Td, TD, T*, Tm, TL, Tj, TJ, ', "), and it is only
// called when the primary extraction has already produced unusable text.

// pdfFont is one font resource with its decoded character map.
type pdfFont struct {
	cmap    unicodeCMap
	twoByte bool // Identity-H style: codes are two bytes wide
}

// fontResources maps a page's font resource names (F5, F7, ...) to their maps.
// Type 3 fonts often have no /BaseFont, so keying by resource name is the only
// reliable identity.
func fontResources(page pdf.Page) map[string]pdfFont {
	out := map[string]pdfFont{}
	resources := page.Resources()
	if resources.Kind() != pdf.Dict {
		return out
	}
	fonts := resources.Key("Font")
	if fonts.Kind() != pdf.Dict {
		return out
	}
	for _, name := range fonts.Keys() {
		font := fonts.Key(name)
		toUnicode := font.Key("ToUnicode")
		if toUnicode.Kind() != pdf.Stream {
			continue
		}
		stream := toUnicode.Reader()
		if stream == nil {
			continue
		}
		cmap := parseToUnicodeCMap(stream)
		_ = stream.Close()
		if len(cmap) == 0 {
			continue
		}
		out[name] = pdfFont{cmap: cmap, twoByte: cmapIsTwoByte(cmap)}
	}
	return out
}

// cmapIsTwoByte infers the code width from the map itself: a simple font's
// codes fit in a byte, an Identity-H font's do not.
func cmapIsTwoByte(cmap unicodeCMap) bool {
	for code := range cmap {
		if code > 0xFF {
			return true
		}
	}
	return false
}

// pageContentBytes returns the page's concatenated content streams.
func pageContentBytes(page pdf.Page) []byte {
	contents := page.V.Key("Contents")
	var buf bytes.Buffer
	appendStream := func(v pdf.Value) {
		if v.Kind() != pdf.Stream {
			return
		}
		reader := v.Reader()
		if reader == nil {
			return
		}
		_, _ = io.Copy(&buf, io.LimitReader(reader, 8<<20))
		_ = reader.Close()
		buf.WriteByte('\n')
	}
	switch contents.Kind() {
	case pdf.Stream:
		appendStream(contents)
	case pdf.Array:
		for i := 0; i < contents.Len(); i++ {
			appendStream(contents.Index(i))
		}
	}
	return buf.Bytes()
}

// extractPageLines decodes one page's content streams into lines of text.
func extractPageLines(page pdf.Page) []string {
	fonts := fontResources(page)
	if len(fonts) == 0 {
		return nil
	}
	content := pageContentBytes(page)
	if len(content) == 0 {
		return nil
	}
	return decodeContentLines(content, fonts)
}

// decodeContentLines walks the content stream and returns the text it draws.
//
// Layout model: Tm starts a line, a Td with a vertical component starts a new
// line, and a Td with only a horizontal component advances within the current
// line. Documents that position every glyph individually (Chromium/Skia output
// does: one Tf per block, one Tm per line, then Td/Tj per glyph) therefore read
// as continuous lines instead of one character per line.
func decodeContentLines(content []byte, fonts map[string]pdfFont) []string {
	var (
		lines   []string
		current strings.Builder
		stack   []token
		font    pdfFont
		haveFnt bool
		x, y    float64
		leading float64
	)

	flush := func() {
		line := strings.TrimRight(current.String(), " ")
		current.Reset()
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	newLine := func(nx, ny float64) {
		flush()
		x, y = nx, ny
	}
	show := func(raw []byte) {
		if !haveFnt {
			return
		}
		current.WriteString(font.cmap.apply(codesToString(raw, font.twoByte)))
	}

	tok := newTokenizer(content)
	for {
		t, ok := tok.next()
		if !ok {
			break
		}
		if t.kind != tokenOperator {
			stack = append(stack, t)
			if len(stack) > 32 {
				stack = stack[len(stack)-32:]
			}
			continue
		}

		switch t.text {
		case "BT":
			// A new text object; the Tm that follows positions the first line.
		case "Tf":
			if len(stack) >= 2 {
				if name := stack[len(stack)-2]; name.kind == tokenName {
					f, found := fonts[name.text]
					font, haveFnt = f, found
				}
			}
		case "Tm":
			if len(stack) >= 6 {
				newLine(stack[len(stack)-2].number, stack[len(stack)-1].number)
			}
		case "Td", "TD":
			if len(stack) >= 2 {
				dx, dy := stack[len(stack)-2].number, stack[len(stack)-1].number
				if t.text == "TD" {
					leading = -dy
				}
				if dy != 0 {
					newLine(x+dx, y+dy)
				} else {
					x += dx
				}
			}
		case "T*":
			newLine(x, y-leading)
		case "Tj":
			if n := len(stack); n > 0 && stack[n-1].kind == tokenString {
				show(stack[n-1].raw)
			}
		case "'":
			newLine(x, y-leading)
			if n := len(stack); n > 0 && stack[n-1].kind == tokenString {
				show(stack[n-1].raw)
			}
		case "\"":
			newLine(x, y-leading)
			if n := len(stack); n > 0 && stack[n-1].kind == tokenString {
				show(stack[n-1].raw)
			}
		case "TJ":
			if n := len(stack); n > 0 && stack[n-1].kind == tokenArray {
				for _, el := range stack[n-1].items {
					switch el.kind {
					case tokenString:
						show(el.raw)
					case tokenNumber:
						// Space glyphs are usually present; only a wide gap is
						// treated as a word break.
						if el.number <= -180 && current.Len() > 0 {
							current.WriteByte(' ')
						}
					}
				}
			}
		}
		stack = stack[:0]
	}
	flush()
	return lines
}

// codesToString converts a show-string's bytes into characters using the code
// width of the current font. Characters above 0xFF are kept as-is so the map
// lookup still has a chance.
func codesToString(raw []byte, twoByte bool) string {
	if !twoByte {
		runes := make([]rune, 0, len(raw))
		for _, b := range raw {
			runes = append(runes, rune(b))
		}
		return string(runes)
	}
	runes := make([]rune, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		runes = append(runes, rune(int(raw[i])<<8|int(raw[i+1])))
	}
	return string(runes)
}

// ---- a small content-stream tokenizer -------------------------------------

type tokenKind int

const (
	tokenOperator tokenKind = iota
	tokenNumber
	tokenString
	tokenName
	tokenArray
)

type token struct {
	kind   tokenKind
	text   string // operator or name
	raw    []byte // string bytes
	number float64
	items  []token // array elements
}

type tokenizer struct {
	data []byte
	pos  int
}

func newTokenizer(data []byte) *tokenizer { return &tokenizer{data: data} }

func (t *tokenizer) next() (token, bool) {
	t.skipSpace()
	if t.pos >= len(t.data) {
		return token{}, false
	}
	c := t.data[t.pos]
	switch {
	case c == '/':
		t.pos++
		start := t.pos
		for t.pos < len(t.data) && !isDelimiter(t.data[t.pos]) && !isSpace(t.data[t.pos]) {
			t.pos++
		}
		return token{kind: tokenName, text: decodeName(t.data[start:t.pos])}, true
	case c == '(':
		return token{kind: tokenString, raw: t.readLiteralString()}, true
	case c == '<' && t.pos+1 < len(t.data) && t.data[t.pos+1] != '<':
		return token{kind: tokenString, raw: t.readHexString()}, true
	case c == '<':
		t.pos += 2
		return token{kind: tokenOperator, text: "<<"}, true
	case c == '[':
		t.pos++
		arr := token{kind: tokenArray}
		for {
			t.skipSpace()
			if t.pos >= len(t.data) {
				break
			}
			if t.data[t.pos] == ']' {
				t.pos++
				break
			}
			el, ok := t.next()
			if !ok {
				break
			}
			arr.items = append(arr.items, el)
		}
		return arr, true
	case c == ']' || c == '>' || c == ')':
		t.pos++
		return t.next()
	case isNumberStart(c):
		start := t.pos
		for t.pos < len(t.data) && isNumberByte(t.data[t.pos]) {
			t.pos++
		}
		raw := string(t.data[start:t.pos])
		v, err := strconv.ParseFloat(raw, 64)
		_ = err
		return token{kind: tokenNumber, number: v, text: raw}, true
	default:
		start := t.pos
		for t.pos < len(t.data) && !isDelimiter(t.data[t.pos]) && !isSpace(t.data[t.pos]) {
			t.pos++
		}
		if t.pos == start {
			t.pos++
			return t.next()
		}
		return token{kind: tokenOperator, text: string(t.data[start:t.pos])}, true
	}
}

func (t *tokenizer) skipSpace() {
	for t.pos < len(t.data) {
		c := t.data[t.pos]
		if isSpace(c) {
			t.pos++
			continue
		}
		if c == '%' { // comment to end of line
			for t.pos < len(t.data) && t.data[t.pos] != '\n' {
				t.pos++
			}
			continue
		}
		return
	}
}

// readLiteralString reads a (...) string, honouring escapes and nesting.
func (t *tokenizer) readLiteralString() []byte {
	t.pos++ // consume '('
	var out []byte
	depth := 1
	for t.pos < len(t.data) {
		c := t.data[t.pos]
		switch c {
		case '\\':
			t.pos++
			if t.pos >= len(t.data) {
				return out
			}
			switch e := t.data[t.pos]; e {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case '(', ')', '\\':
				out = append(out, e)
			case '\n':
				// line continuation: nothing
			default:
				if e >= '0' && e <= '7' {
					oct := make([]byte, 0, 3)
					for len(oct) < 3 && t.pos < len(t.data) && t.data[t.pos] >= '0' && t.data[t.pos] <= '7' {
						oct = append(oct, t.data[t.pos])
						t.pos++
					}
					t.pos--
					if v, err := strconv.ParseInt(string(oct), 8, 32); err == nil {
						out = append(out, byte(v))
					}
				} else {
					out = append(out, e)
				}
			}
			t.pos++
		case '(':
			depth++
			out = append(out, c)
			t.pos++
		case ')':
			depth--
			t.pos++
			if depth == 0 {
				return out
			}
			out = append(out, c)
		default:
			out = append(out, c)
			t.pos++
		}
	}
	return out
}

// readHexString reads a <...> string into bytes.
func (t *tokenizer) readHexString() []byte {
	t.pos++ // consume '<'
	start := t.pos
	for t.pos < len(t.data) && t.data[t.pos] != '>' {
		t.pos++
	}
	hex := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			return r
		}
		return -1
	}, string(t.data[start:t.pos]))
	if t.pos < len(t.data) {
		t.pos++ // consume '>'
	}
	if len(hex)%2 == 1 {
		hex += "0"
	}
	out := make([]byte, 0, len(hex)/2)
	for i := 0; i+1 < len(hex); i += 2 {
		v, err := strconv.ParseUint(hex[i:i+2], 16, 16)
		if err != nil {
			continue
		}
		out = append(out, byte(v))
	}
	return out
}

// decodeName resolves the #xx escapes PDF allows inside names.
func decodeName(raw []byte) string {
	var b strings.Builder
	for i := 0; i < len(raw); i++ {
		if raw[i] == '#' && i+2 < len(raw) {
			if v, err := strconv.ParseUint(string(raw[i+1:i+3]), 16, 16); err == nil {
				b.WriteByte(byte(v))
				i += 2
				continue
			}
		}
		b.WriteByte(raw[i])
	}
	return b.String()
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '\f' || c == 0
}

func isDelimiter(c byte) bool {
	switch c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}

func isNumberStart(c byte) bool {
	return c == '+' || c == '-' || c == '.' || (c >= '0' && c <= '9')
}

func isNumberByte(c byte) bool {
	return (c >= '0' && c <= '9') || c == '.' || c == '+' || c == '-' || c == 'e' || c == 'E'
}
