package resume

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testNow is fixed so year-of-experience assertions are deterministic. The
// fixture CV ends its second role in Dec 2020 and starts its first in Jul 2016.
var testNow = time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return data
}

// buildDOCX builds a real DOCX archive (zip + WordprocessingML) in memory, so
// the DOCX path is exercised without shipping a binary fixture.
func buildDOCX(t *testing.T, paragraphs []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	write := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	write("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`)

	var doc strings.Builder
	doc.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, p := range paragraphs {
		doc.WriteString(`<w:p><w:r><w:t>`)
		_ = xml.EscapeText(&doc, []byte(p))
		doc.WriteString(`</w:t></w:r></w:p>`)
	}
	doc.WriteString(`</w:body></w:document>`)
	write("word/document.xml", doc.String())

	if err := zw.Close(); err != nil {
		t.Fatalf("closing zip: %v", err)
	}
	return buf.Bytes()
}

func fixtureParagraphs(t *testing.T) []string {
	t.Helper()
	text := string(loadFixture(t, "sample_cv.txt"))
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, strings.TrimRight(l, "\r"))
	}
	return out
}

func TestDetectKindPrefersContentOverFilename(t *testing.T) {
	pdf := loadFixture(t, "sample_cv.pdf")
	docx := buildDOCX(t, []string{"hello"})

	tests := []struct {
		name     string
		filename string
		head     []byte
		want     Kind
		wantErr  bool
	}{
		{"pdf detected", "cv.pdf", pdf[:16], KindPDF, false},
		{"docx detected", "cv.docx", docx[:16], KindDOCX, false},
		{"pdf content with wrong extension", "cv.docx", pdf[:16], KindPDF, false},
		{"docx content with wrong extension", "cv.pdf", docx[:16], KindDOCX, false},
		{"pdf with leading whitespace", "cv.pdf", append([]byte("\n  "), []byte("%PDF-1.7")...), KindPDF, false},
		{"pdf extension but not a pdf", "cv.pdf", []byte("this is plain text, not a pdf at all"), "", true},
		{"docx extension but not a zip", "cv.docx", []byte("plain text"), "", true},
		{"unsupported extension", "cv.txt", []byte("CV contents"), "", true},
		{"no extension", "cv", []byte("CV contents"), "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DetectKind(tc.filename, tc.head)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got kind %q", got)
				}
				if !errors.Is(err, ErrUnsupportedFormat) {
					t.Errorf("error should be ErrUnsupportedFormat, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("kind = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractPDFTextFromRealDocument(t *testing.T) {
	data := loadFixture(t, "sample_cv.pdf")
	text, err := ExtractText(KindPDF, data)
	if err != nil {
		t.Fatalf("ExtractText(pdf): %v", err)
	}
	for _, want := range []string{"VALERIAN", "Senior Full Stack Engineer", "React", "TypeScript", "Go", "PostgreSQL", "EDUCATION", "LANGUAGES"} {
		if !containsFold(text, want) {
			t.Errorf("extracted PDF text is missing %q\n--- extracted (%d chars) ---\n%s", want, len(text), first(text, 1200))
		}
	}
	if len([]rune(text)) < 800 {
		t.Errorf("extracted only %d chars from a full CV page", len([]rune(text)))
	}
}

func TestExtractDOCXTextRoundTrip(t *testing.T) {
	docx := buildDOCX(t, fixtureParagraphs(t))
	text, err := ExtractText(KindDOCX, docx)
	if err != nil {
		t.Fatalf("ExtractText(docx): %v", err)
	}
	for _, want := range []string{"Senior Full Stack Engineer", "React", "PostgreSQL", "Bachelor of Computer Science"} {
		if !containsFold(text, want) {
			t.Errorf("extracted DOCX text is missing %q\n--- extracted (%d chars) ---", want, len(text))
		}
	}
}

func TestExtractDOCXRejectsNonWordZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("random.txt")
	_, _ = w.Write([]byte("not a word document"))
	_ = zw.Close()

	_, err := ExtractText(KindDOCX, buf.Bytes())
	if !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("error = %v, want ErrInvalidDocument", err)
	}
}

func TestExtractDOCXRejectsZipBomb(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// Highly compressible and far beyond the uncompressed budget.
	w, _ := zw.Create("word/document.xml")
	chunk := bytes.Repeat([]byte("A"), 1<<20)
	for i := 0; i < 80; i++ {
		if _, err := w.Write(chunk); err != nil {
			t.Fatalf("writing: %v", err)
		}
	}
	ct, _ := zw.Create("[Content_Types].xml")
	_, _ = ct.Write([]byte("<Types/>"))
	_ = zw.Close()

	_, err := ExtractText(KindDOCX, buf.Bytes())
	if err == nil {
		t.Fatal("a decompression bomb must be rejected")
	}
	if !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("error = %v, want ErrInvalidDocument", err)
	}
}

func TestExtractShortDocumentFailsLoudly(t *testing.T) {
	short := buildDOCX(t, []string{"Too short to be a CV."})
	if _, err := ExtractText(KindDOCX, short); err == nil {
		t.Fatal("a document below the minimum text length must fail rather than produce a profile")
	} else if !errors.Is(err, ErrNoTextExtracted) {
		t.Fatalf("error = %v, want ErrNoTextExtracted", err)
	}
}

func TestHashIsStable(t *testing.T) {
	data := loadFixture(t, "sample_cv.pdf")
	if Hash(data) != Hash(data) {
		t.Fatal("Hash must be deterministic")
	}
	if Hash(data) == Hash(append(data, 'x')) {
		t.Fatal("Hash must change with content")
	}
	if len(Hash(data)) != 64 {
		t.Fatalf("expected a hex sha256, got %q", Hash(data))
	}
}

func TestNormalizeText(t *testing.T) {
	in := "Line one  \r\n\r\n\r\nLine two\x00 with  spaces\tand\ttabs\n\n\n"
	got := NormalizeText(in)
	if strings.Contains(got, "\x00") {
		t.Error("control characters must be stripped")
	}
	if strings.Contains(got, "  ") {
		t.Error("runs of spaces must collapse")
	}
	if !strings.HasPrefix(got, "Line one\n") || !strings.Contains(got, "Line two with spaces and tabs") {
		t.Errorf("unexpected normalisation result: %q", got)
	}
}

// --- profile extraction ---------------------------------------------------

func TestExtractProfileFromRealCVDocuments(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"pdf", loadFixture(t, "sample_cv.pdf")},
		{"docx", buildDOCX(t, fixtureParagraphs(t))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, err := ExtractText(Kind(tc.name), tc.data)
			if err != nil {
				t.Fatalf("extract: %v", err)
			}
			draft := ExtractProfile(text, testNow)

			for _, key := range []string{FieldSkills, FieldCurrentTitle, FieldYearsExperience, FieldSeniority, FieldEducation, FieldLanguages, FieldProjects} {
				if _, ok := draft.Fields[key]; !ok {
					t.Errorf("field %q was not extracted", key)
				}
			}

			skills, _ := draft.Fields[FieldSkills].Value.([]string)
			for _, want := range []string{"React", "TypeScript", "Go", "PostgreSQL"} {
				if !containsFold(strings.Join(skills, ","), want) {
					t.Errorf("skills missing %q; got %v", want, skills)
				}
			}

			sr := draft.Fields[FieldSeniority].Value
			if sr != "senior" {
				t.Errorf("seniority = %v, want senior", sr)
			}

			title, _ := draft.Fields[FieldCurrentTitle].Value.(string)
			if !containsFold(title, "Full Stack") {
				t.Errorf("current title = %q", title)
			}

			if draft.TextChars < MinTextChars {
				t.Errorf("TextChars = %d", draft.TextChars)
			}

			// Every extracted field must be marked as an unconfirmed draft.
			for key, f := range draft.Fields {
				if f.Provenance != ProvenanceInferred {
					t.Errorf("field %q provenance = %q, want %q", key, f.Provenance, ProvenanceInferred)
				}
				if f.Confidence <= 0 || f.Confidence > 1 {
					t.Errorf("field %q confidence out of range: %v", key, f.Confidence)
				}
			}
			if len(draft.Warnings) == 0 {
				t.Error("the draft must warn that values are inferred")
			}
		})
	}
}

func TestYearsExperienceUsesDateRanges(t *testing.T) {
	text := NormalizeText(string(loadFixture(t, "sample_cv.txt")))
	draft := ExtractProfile(text, testNow)
	years, ok := draft.Fields[FieldYearsExperience].Value.(float64)
	if !ok {
		t.Fatal("years of experience was not extracted")
	}
	// Jul 2016 -> Oct 2026 is ~10.25 years.
	if years < 9.5 || years > 11 {
		t.Fatalf("years = %v, want roughly 10.25", years)
	}
}

func TestYearsExperienceHandlesOverlappingRoles(t *testing.T) {
	// Two concurrent part-time roles must not double-count.
	text := "Summary\nEngineer\n\nEXPERIENCE\nDeveloper | Acme | Jan 2020 - Jan 2023\nConsultant | Beta | Jan 2021 - Jan 2023\n"
	draft := ExtractProfile(text, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	years, _ := draft.Fields[FieldYearsExperience].Value.(float64)
	if years < 2.9 || years > 3.1 {
		t.Fatalf("overlapping ranges must be unioned to ~3 years, got %v", years)
	}
}

func TestYearsExperiencePresent(t *testing.T) {
	text := "EXPERIENCE\nSenior Engineer | Acme | Mar 2020 - Present\n"
	draft := ExtractProfile(text, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))
	years, _ := draft.Fields[FieldYearsExperience].Value.(float64)
	if years < 5.9 || years > 6.1 {
		t.Fatalf("years = %v, want ~6", years)
	}
}

func TestNoDateRangesMeansNoYearsAsserted(t *testing.T) {
	text := "EXPERIENCE\nDeveloper | Acme | sometime in the past\n"
	draft := ExtractProfile(text, testNow)
	if _, ok := draft.Fields[FieldYearsExperience]; ok {
		t.Fatal("years of experience must not be guessed when no date range exists")
	}
}

func TestSkillMatchingAvoidsSubstringFalsePositives(t *testing.T) {
	text := "Summary\nI worked at Logo Corp. JavaScript was my main language and I love Javanese food."
	draft := ExtractProfile(text, testNow)
	skills, _ := draft.Fields[FieldSkills].Value.([]string)

	has := func(want string) bool {
		for _, s := range skills {
			if s == want { // exact match: "JavaScript" must not satisfy a "Java" check
				return true
			}
		}
		return false
	}

	if !has("JavaScript") {
		t.Errorf("JavaScript should be detected: %v", skills)
	}
	if has("Java") {
		t.Errorf("'Java' must not match 'JavaScript' or 'Javanese': %v", skills)
	}
	if has("Go") {
		t.Errorf("'Go' must not match 'Logo': %v", skills)
	}
}

func TestSkillMatchingPositivesAndNegatives(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		want    []string
		notWant []string
	}{
		{"standalone java is detected", "Experience with Java and Spring Boot in banking.", []string{"Java", "Spring Boot"}, []string{"JavaScript"}},
		{"nodejs compact form matches", "Built services with Node.js and PostgreSQL.", []string{"Node.js", "PostgreSQL"}, nil},
		{"nodejs without dot also matches", "Built services with nodejs and postgresql.", []string{"Node.js", "PostgreSQL"}, nil},
		{"ci cd compact form matches", "Owned CI/CD pipelines and release automation.", []string{"CI/CD"}, nil},
		{"go is not matched inside words", "We used Django and a logo redesign.", []string{"Django"}, []string{"Go"}},
		{"regex free words are not skills", "Experienced in teamwork and communication.", nil, []string{"Go", "React"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			draft := ExtractProfile(tc.text, testNow)
			skills, _ := draft.Fields[FieldSkills].Value.([]string)
			has := func(want string) bool {
				for _, s := range skills {
					if s == want {
						return true
					}
				}
				return false
			}
			for _, w := range tc.want {
				if !has(w) {
					t.Errorf("expected skill %q in %v", w, skills)
				}
			}
			for _, nw := range tc.notWant {
				if has(nw) {
					t.Errorf("skill %q must not be detected in %v", nw, skills)
				}
			}
		})
	}
}

func TestExtractProfileIsDeterministic(t *testing.T) {
	text := NormalizeText(string(loadFixture(t, "sample_cv.txt")))
	first := ExtractProfile(text, testNow)
	for i := 0; i < 10; i++ {
		again := ExtractProfile(text, testNow)
		if len(again.Fields) != len(first.Fields) {
			t.Fatal("field count changed between runs")
		}
		for k, v := range first.Fields {
			other, ok := again.Fields[k]
			if !ok {
				t.Fatalf("field %q disappeared", k)
			}
			if !equalValues(v.Value, other.Value) {
				t.Fatalf("field %q changed: %v vs %v", k, v.Value, other.Value)
			}
		}
	}
}

func TestContactDetailsAreNotStored(t *testing.T) {
	text := "JANE DOE\nEngineer\njane.doe@example.com | +62 811 222 333\n\nEXPERIENCE\nEngineer | Acme | Jan 2015 - Jan 2020\n"
	draft := ExtractProfile(text, testNow)
	for key, f := range draft.Fields {
		if s, ok := f.Value.(string); ok && (strings.Contains(s, "@") || strings.Contains(s, "+62")) {
			t.Errorf("field %q leaked contact details: %q", key, s)
		}
	}
	found := false
	for _, w := range draft.Warnings {
		if strings.Contains(strings.ToLower(w), "contact details") {
			found = true
		}
	}
	if !found {
		t.Error("the draft should note that contact details were skipped")
	}
}

func TestEmptyTextProducesEmptyDraftWithWarning(t *testing.T) {
	draft := ExtractProfile("   \n  \n", testNow)
	if len(draft.Fields) != 0 {
		t.Errorf("expected no fields, got %v", draft.Fields)
	}
	if len(draft.Warnings) < 2 {
		t.Errorf("expected warnings, got %v", draft.Warnings)
	}
}

func TestNameGuessSkipsContactAndSectionLines(t *testing.T) {
	text := "SKILLS\nReact\nSUMMARY\nJOHN SMITH\nEngineer\n"
	draft := ExtractProfile(text, testNow)
	name, _ := draft.Fields[FieldFullName].Value.(string)
	if name != "John Smith" {
		t.Errorf("name = %q, want John Smith", name)
	}
}

// --- helpers --------------------------------------------------------------

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func equalValues(a, b any) bool {
	as, aok := a.([]string)
	bs, bok := b.([]string)
	if aok && bok {
		if len(as) != len(bs) {
			return false
		}
		for i := range as {
			if as[i] != bs[i] {
				return false
			}
		}
		return true
	}
	return a == b
}

func first(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
