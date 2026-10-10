package resume

import (
	"os"
	"strings"
	"testing"
	"time"
)

// The bug these cover: a PDF whose font carries a Differences encoding makes
// the parsing library ignore the font's /ToUnicode map, so the text layer
// decodes into substituted characters and the field parser extracts nothing.
// Chromium/Skia "print to PDF" output (Type 3 fonts, no /BaseFont) does this.

func TestParseToUnicodeCMapBfchar(t *testing.T) {
	cmapText := `
/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
1 begincodespacerange
<00> <FF>
endcodespacerange
3 beginbfchar
<01> <0048>
<02> <0065>
<03> <006C006C>
endbfchar
endcmap
`
	m := parseToUnicodeCMap(strings.NewReader(cmapText))
	if len(m) != 3 {
		t.Fatalf("want 3 entries, got %d", len(m))
	}
	if got := m[rune(1)]; got != "H" {
		t.Errorf("code 1 = %q, want H", got)
	}
	if got := m[rune(2)]; got != "e" {
		t.Errorf("code 2 = %q, want e", got)
	}
	// A ligature arrives as several UTF-16 units.
	if got := m[rune(3)]; got != "ll" {
		t.Errorf("code 3 = %q, want ll", got)
	}
}

// The regression that made this fix necessary: the destination of a simple
// bfrange is the THIRD value, and dropping it left most of a font unmapped.
func TestParseToUnicodeCMapBfrangeSimple(t *testing.T) {
	cmapText := `
begincmap
2 beginbfrange
<41> <43> <0061>
<50> <51> <0078>
endbfrange
endcmap
`
	m := parseToUnicodeCMap(strings.NewReader(cmapText))
	want := map[int]string{0x41: "a", 0x42: "b", 0x43: "c", 0x50: "x", 0x51: "y"}
	for code, text := range want {
		if got := m[rune(code)]; got != text {
			t.Errorf("code %#x = %q, want %q", code, got, text)
		}
	}
	if len(m) != len(want) {
		t.Errorf("want %d entries, got %d", len(want), len(m))
	}
}

func TestParseToUnicodeCMapBfrangeArray(t *testing.T) {
	cmapText := `
begincmap
1 beginbfrange
<10> <12> [<0073> <0074> <0075>]
endbfrange
endcmap
`
	m := parseToUnicodeCMap(strings.NewReader(cmapText))
	for i, want := range []string{"s", "t", "u"} {
		if got := m[rune(0x10+i)]; got != want {
			t.Errorf("code %#x = %q, want %q", 0x10+i, got, want)
		}
	}
}

func TestParseToUnicodeCMapRejectsNonCMap(t *testing.T) {
	if m := parseToUnicodeCMap(strings.NewReader("not a cmap at all")); m != nil {
		t.Errorf("want nil, got %d entries", len(m))
	}
	if m := parseToUnicodeCMap(strings.NewReader("")); m != nil {
		t.Errorf("want nil for empty input, got %d entries", len(m))
	}
}

func TestCMapApply(t *testing.T) {
	m := unicodeCMap{rune(1): "H", rune(2): "i", rune(9): " "}
	if got := m.apply(string([]rune{1, 2, 9, 3})); got != "Hi \u0003" {
		t.Errorf("apply = %q", got)
	}
	var empty unicodeCMap
	if got := empty.apply("unchanged"); got != "unchanged" {
		t.Errorf("empty cmap must pass text through, got %q", got)
	}
}

// Garbage as produced by the real document this was written for.
func TestProseRatioDistinguishesGarbageFromLanguage(t *testing.T) {
	garbage := "7=]JlR=_\nvR,pl_a^a\n;Y;il;\\F^\\HjP;ÕåÕ\nr;ZHiP;\\FsPfËN[;PZD^[\ni^\\lH\\F\nHrHZ^fHisPlOåuH;ij^MHtfHiPH\\DHCmPZFP\\NH\\lHifiPjHµjD;ZH-H;Dl;\\F HtlWj;ffZPD;lP^\\jP\\DZmFP\\N"
	if looksLikeProse(garbage) {
		t.Errorf("garbage must not read as prose (ratio %.2f)", proseRatio(garbage))
	}
	prose := "Frontend Developer with 6+ years of experience building enterprise-scale React and Next.js applications, including dashboards and customer platforms. Experienced in technical ownership."
	if !looksLikeProse(prose) {
		t.Errorf("prose must read as prose (ratio %.2f)", proseRatio(prose))
	}
	if proseRatio(prose) <= proseRatio(garbage) {
		t.Error("prose must score above garbage")
	}
}

// The layout model: this is the exact operator pattern the real Chromium/Skia
// document uses — one Tm per line, then a Td/Tj per glyph.
func TestDecodeContentLinesPerGlyphPositioning(t *testing.T) {
	// Codes decode to letters; 9 is space. Kept deliberately small so the
	// expectation is exact.
	fonts := map[string]pdfFont{
		"F8": {cmap: unicodeCMap{
			rune(1): "H", rune(2): "e", rune(3): "l", rune(4): "o",
			rune(5): "w", rune(6): "r", rune(7): "d", rune(9): " ",
		}},
	}
	content := []byte(`BT
/F8 15.3599997 Tf
1 0 0 -1 12.78125 1068 Tm
<01> Tj
10 0 Td <02> Tj
7 0 Td <03> Tj
7 0 Td <03> Tj
9 0 Td <04> Tj
9 0 Td <09> Tj
9 0 Td <05> Tj
8 0 Td <04> Tj
8 0 Td <06> Tj
8 0 Td <03> Tj
8 0 Td <07> Tj
0 -20 Td <01> Tj
9 0 Td <02> Tj
EMC
ET`)

	lines := decodeContentLines(content, fonts)
	if len(lines) != 2 {
		t.Fatalf("want 2 lines (a horizontal-only Td must not break the line), got %d: %q", len(lines), lines)
	}
	if lines[0] != "Hello world" {
		t.Errorf("line 1 = %q, want %q", lines[0], "Hello world")
	}
	// A Td with a vertical component starts a new line.
	if lines[1] != "He" {
		t.Errorf("line 2 = %q, want %q", lines[1], "He")
	}
}

func TestDecodeContentLinesHandlesWholeStringShows(t *testing.T) {
	fonts := map[string]pdfFont{
		"F1": {cmap: unicodeCMap{rune(1): "H", rune(2): "i"}},
	}
	// A document that shows a whole string per Tj, with a newline via T*.
	content := []byte("BT\n/F1 12 Tf\n14 TL\n1 0 0 -1 10 700 Tm\n<0102> Tj\nT*\n<0102> Tj\nET")
	lines := decodeContentLines(content, fonts)
	if len(lines) != 2 || lines[0] != "Hi" || lines[1] != "Hi" {
		t.Fatalf("got %q", lines)
	}
}

func TestCodesToString(t *testing.T) {
	if got := codesToString([]byte{0x41, 0x42}, false); got != "AB" {
		t.Errorf("one-byte codes = %q", got)
	}
	if got := codesToString([]byte{0x00, 0x41, 0x00, 0x42}, true); got != "AB" {
		t.Errorf("two-byte codes = %q", got)
	}
	if got := codesToString([]byte{0x01, 0x02}, false); got != string([]rune{1, 2}) {
		t.Errorf("unmapped one-byte codes should pass through as runes, got %q", got)
	}
}

func TestCMapIsTwoByte(t *testing.T) {
	if cmapIsTwoByte(unicodeCMap{rune(0x41): "A"}) {
		t.Error("byte-sized codes must not be treated as two-byte")
	}
	if !cmapIsTwoByte(unicodeCMap{rune(0x1234): "A"}) {
		t.Error("codes above 0xFF must be treated as two-byte")
	}
}

// End-to-end against a real Chromium-produced CV when one is available in the
// development workspace. Skipped elsewhere rather than failing.
func TestExtractPDFTextOnChromiumCV(t *testing.T) {
	const path = "../../data/resumes/2e02ec53-74df-4437-b5d0-7062a2d81af0.bin"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("development CV not present: %v", err)
	}
	text, err := ExtractPDFText(data)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !looksLikeProse(text) {
		t.Fatalf("extracted text still does not read as prose (ratio %.2f)", proseRatio(text))
	}
	for _, want := range []string{"Work Experiences", "Jakarta", "React"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in the extracted text", want)
		}
	}
	if fields := ExtractProfile(text, time.Now()).Fields; len(fields) < 5 {
		t.Errorf("want several profile fields from a CV, got %d", len(fields))
	}
}
