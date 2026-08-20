// Package extract turns an uploaded contract file (DOCX or text-layer PDF)
// into plain text — the same shape internal/clause.Split already consumes
// from a pasted-text submission, so nothing downstream needs to know
// whether the contract arrived as text, DOCX, or PDF.
//
// v1 deliberately does not do OCR. A scanned/photographed PDF has no text
// layer to extract, and would silently come back empty or near-empty —
// that's treated as a failure (ErrNoExtractableText), not a success with a
// blank contract, so the caller can show "we couldn't read this file"
// instead of silently analyzing nothing.
package extract

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
)

// Format identifies which parser Text should use.
type Format string

const (
	FormatDOCX Format = "docx"
	FormatPDF  Format = "pdf"
)

// ErrUnsupportedFormat is returned for any extension other than the two
// v1 supports.
var ErrUnsupportedFormat = errors.New("extract: unsupported file format (only .docx and .pdf are supported)")

// ErrNoExtractableText is returned when parsing succeeded but produced
// (near-)no text — for PDFs this is almost always a scanned/image-only
// document, which v1 doesn't OCR.
var ErrNoExtractableText = errors.New("extract: no readable text found in this file — for PDFs, this usually means it's a scanned image copy rather than a text document, which isn't supported yet")

// minExtractedChars is the floor below which extraction is treated as
// failed rather than as an unusually short document. A real lease
// contract is always well past this; a near-empty result means the file
// had no usable text layer, not that the contract was short.
const minExtractedChars = 200

// DetectFormat maps a filename's extension to a Format.
func DetectFormat(filename string) (Format, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".docx":
		return FormatDOCX, nil
	case ".pdf":
		return FormatPDF, nil
	default:
		return "", ErrUnsupportedFormat
	}
}

// Text extracts plain text from data in the given format. Paragraphs are
// separated by a blank line, matching the shape internal/clause.Split
// expects from pasted plain text.
func Text(data []byte, format Format) (string, error) {
	var (
		text string
		err  error
	)
	switch format {
	case FormatDOCX:
		text, err = docxText(data)
	case FormatPDF:
		text, err = pdfText(data)
	default:
		return "", ErrUnsupportedFormat
	}
	if err != nil {
		return "", err
	}
	if len(strings.TrimSpace(text)) < minExtractedChars {
		return "", ErrNoExtractableText
	}
	return text, nil
}

// docxText reads word/document.xml out of the docx zip container and
// walks its tokens, treating each <w:p> as one paragraph and each <w:t>
// as a text run within it — a docx's actual structure, not a guess at one.
func docxText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("extract: open docx: %w", err)
	}

	var docFile *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			docFile = f
			break
		}
	}
	if docFile == nil {
		return "", fmt.Errorf("extract: docx missing word/document.xml")
	}

	rc, err := docFile.Open()
	if err != nil {
		return "", fmt.Errorf("extract: read docx content: %w", err)
	}
	defer rc.Close()

	var out, para strings.Builder
	inText := false

	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("extract: parse docx xml: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				para.WriteString("\t")
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				flushParagraph(&out, &para)
			}
		case xml.CharData:
			if inText {
				para.Write(t)
			}
		}
	}
	// A malformed docx missing a final </w:p> shouldn't lose its last
	// paragraph's text.
	flushParagraph(&out, &para)

	return out.String(), nil
}

func flushParagraph(out, para *strings.Builder) {
	if trimmed := strings.TrimSpace(para.String()); trimmed != "" {
		out.WriteString(trimmed)
		out.WriteString("\n\n")
	}
	para.Reset()
}

// pdfText extracts the text layer via github.com/ledongthuc/pdf. It
// returns whatever text the PDF's content streams contain — for a
// scanned/image-only PDF that's nothing, which Text's minExtractedChars
// check turns into ErrNoExtractableText rather than a silent empty result.
func pdfText(data []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("extract: open pdf: %w", err)
	}

	reader, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("extract: read pdf text: %w", err)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil {
		return "", fmt.Errorf("extract: read pdf text: %w", err)
	}
	return buf.String(), nil
}
