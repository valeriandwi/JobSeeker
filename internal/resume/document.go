// Package resume validates uploaded CV documents and extracts a structured,
// reviewable candidate profile from them.
//
// Privacy stance (from the approved plan, enforced here):
//   - Parsing is entirely local. Nothing is sent to a third-party service, and
//     there is no code path that transmits CV content anywhere.
//   - Extraction output is a *draft*. Every field carries its provenance so the
//     UI can show what was inferred and require confirmation.
//   - Contact details are deliberately not turned into profile fields.
package resume

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
)

// Kind identifies the document format.
type Kind string

// Supported document kinds.
const (
	KindPDF  Kind = "pdf"
	KindDOCX Kind = "docx"
)

// Limits applied to uploaded documents.
const (
	// MaxUncompressedBytes guards against zip bombs inside DOCX files.
	MaxUncompressedBytes = 64 << 20
	// MaxEntries caps the number of files inside a DOCX archive.
	MaxEntries = 512
	// MinTextChars is the minimum extracted text for a CV to be usable. Below
	// this we fail the parse rather than build a profile from noise.
	MinTextChars = 120
)

// Validation errors, mapped to HTTP statuses by the API layer.
var (
	ErrUnsupportedFormat = errors.New("unsupported document format")
	ErrInvalidDocument   = errors.New("document is not a valid file of its declared type")
	ErrNoTextExtracted   = errors.New("no readable text could be extracted from the document")
)

// DetectKind identifies the document type from the extension and magic bytes.
// The declared filename is a hint only: content always wins.
func DetectKind(filename string, head []byte) (Kind, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	trimmed := bytes.TrimLeft(head, " \t\r\n\v\f")

	isPDF := bytes.HasPrefix(trimmed, []byte("%PDF-"))
	isZIP := bytes.HasPrefix(head, []byte("PK\x03\x04")) || bytes.HasPrefix(head, []byte("PK\x05\x06"))

	switch {
	case isPDF:
		return KindPDF, nil
	case isZIP:
		// A ZIP could be a DOCX or something else; the caller verifies the
		// internal structure when parsing.
		return KindDOCX, nil
	case ext == ".pdf":
		return "", fmt.Errorf("%w: filename says PDF but the content is not a PDF", ErrUnsupportedFormat)
	case ext == ".docx":
		return "", fmt.Errorf("%w: filename says DOCX but the content is not a ZIP archive", ErrUnsupportedFormat)
	default:
		return "", fmt.Errorf("%w: accepted formats are PDF and DOCX", ErrUnsupportedFormat)
	}
}

// Hash returns the hex sha256 of the document for integrity tracking.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ExtractText returns the plain text of a PDF or DOCX document.
func ExtractText(kind Kind, data []byte) (string, error) {
	switch kind {
	case KindPDF:
		return ExtractPDFText(data)
	case KindDOCX:
		return ExtractDOCXText(data)
	default:
		return "", ErrUnsupportedFormat
	}
}

// ExtractPDFText extracts text from a PDF.
//
// Encrypted or malformed PDFs produce an error rather than partial garbage. A
// panic inside the third-party parser is caught and converted into a parse
// failure so one bad upload cannot take down a request.
func ExtractPDFText(data []byte) (string, error) {
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidDocument, err)
	}

	var buf bytes.Buffer
	panicked := false
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				panicked = true
			}
		}()
		text, perr := reader.GetPlainText()
		if perr != nil {
			return
		}
		_, _ = io.Copy(&buf, text)
	}()

	out := NormalizeText(buf.String())
	if len([]rune(out)) < MinTextChars {
		if panicked {
			return out, fmt.Errorf("%w: the PDF parser rejected the document structure", ErrNoTextExtracted)
		}
		return out, ErrNoTextExtracted
	}
	return out, nil
}

// ExtractDOCXText extracts text from a DOCX archive.
func ExtractDOCXText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidDocument, err)
	}
	if len(zr.File) > MaxEntries {
		return "", fmt.Errorf("%w: archive has %d entries (limit %d)", ErrInvalidDocument, len(zr.File), MaxEntries)
	}

	var totalUncompressed uint64
	var docXML []byte
	haveContentTypes := false

	for _, f := range zr.File {
		name := f.Name
		totalUncompressed += f.UncompressedSize64
		if totalUncompressed > MaxUncompressedBytes {
			return "", fmt.Errorf("%w: uncompressed size exceeds %d bytes", ErrInvalidDocument, MaxUncompressedBytes)
		}
		switch {
		case name == "[Content_Types].xml":
			haveContentTypes = true
		case name == "word/document.xml":
			rc, oerr := f.Open()
			if oerr != nil {
				return "", fmt.Errorf("%w: opening word/document.xml: %v", ErrInvalidDocument, oerr)
			}
			limited := io.LimitReader(rc, MaxUncompressedBytes)
			docXML, err = io.ReadAll(limited)
			_ = rc.Close()
			if err != nil {
				return "", fmt.Errorf("%w: reading word/document.xml: %v", ErrInvalidDocument, err)
			}
		}
	}

	if !haveContentTypes || docXML == nil {
		return "", fmt.Errorf("%w: not a Word document (missing [Content_Types].xml or word/document.xml)", ErrInvalidDocument)
	}

	text, err := xmlToText(docXML)
	if err != nil {
		return "", err
	}
	out := NormalizeText(text)
	if len([]rune(out)) < MinTextChars {
		return out, ErrNoTextExtracted
	}
	return out, nil
}

// xmlToText converts WordprocessingML into plain text, preserving paragraph
// and table-cell boundaries.
func xmlToText(docXML []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(docXML))
	var out strings.Builder
	inText := false

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("%w: parsing document.xml: %v", ErrInvalidDocument, err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "t":
				inText = true
			case "br":
				out.WriteString("\n")
			case "tab":
				out.WriteString("\t")
			}
		case xml.EndElement:
			switch el.Name.Local {
			case "t":
				inText = false
			case "p", "tc", "tr":
				out.WriteString("\n")
			}
		case xml.CharData:
			if inText {
				out.Write(el)
			}
		}
	}
	return out.String(), nil
}

// NormalizeText cleans extracted text: normalizes line endings, strips control
// characters, collapses runs of spaces and blank lines.
func NormalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20:
			return -1
		case r == 0x7f:
			return -1
		default:
			return r
		}
	}, s)

	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, l := range lines {
		l = strings.Join(strings.Fields(l), " ")
		if l == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
