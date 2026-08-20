package extract

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestDetectFormat(t *testing.T) {
	cases := []struct {
		filename string
		want     Format
		wantErr  bool
	}{
		{"lease.docx", FormatDOCX, false},
		{"LEASE.DOCX", FormatDOCX, false},
		{"lease.pdf", FormatPDF, false},
		{"scan.PDF", FormatPDF, false},
		{"lease.txt", "", true},
		{"lease", "", true},
	}
	for _, c := range cases {
		t.Run(c.filename, func(t *testing.T) {
			got, err := DetectFormat(c.filename)
			if (err != nil) != c.wantErr {
				t.Fatalf("DetectFormat(%q) err = %v, wantErr %v", c.filename, err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("DetectFormat(%q) = %q, want %q", c.filename, got, c.want)
			}
		})
	}
}

func TestText_DOCX_ExtractsParagraphs(t *testing.T) {
	para1 := strings.Repeat("This is the first clause of the lease agreement. ", 3)
	para2 := strings.Repeat("This is the second clause about rent payment terms. ", 3)
	data := buildMinimalDOCX(t, para1, para2)

	got, err := Text(data, FormatDOCX)
	if err != nil {
		t.Fatalf("Text() error = %v", err)
	}
	if !strings.Contains(got, strings.TrimSpace(para1)) || !strings.Contains(got, strings.TrimSpace(para2)) {
		t.Errorf("Text() = %q, want both paragraphs present", got)
	}
	// Paragraphs must be blank-line separated so clause.Split segments
	// them the same way it segments pasted plain text.
	if !strings.Contains(got, "\n\n") {
		t.Errorf("Text() = %q, want paragraphs separated by a blank line", got)
	}
}

func TestText_DOCX_EmptyDocumentFails(t *testing.T) {
	data := buildMinimalDOCX(t) // no paragraphs at all
	_, err := Text(data, FormatDOCX)
	if err != ErrNoExtractableText {
		t.Fatalf("Text() error = %v, want ErrNoExtractableText", err)
	}
}

func TestText_DOCX_NotAZipFails(t *testing.T) {
	_, err := Text([]byte("not a docx"), FormatDOCX)
	if err == nil {
		t.Fatal("Text() error = nil, want an error for a non-zip file")
	}
}

func TestText_PDF_ExtractsTextLayer(t *testing.T) {
	content := strings.Repeat("This lease requires two months deposit and one month advance rent. ", 4)
	data := buildMinimalPDF(t, content)

	got, err := Text(data, FormatPDF)
	if err != nil {
		t.Fatalf("Text() error = %v", err)
	}
	if !strings.Contains(got, "deposit") {
		t.Errorf("Text() = %q, want extracted text to contain the page content", got)
	}
}

func TestText_PDF_NoTextLayerFails(t *testing.T) {
	// Simulates a scanned/image-only PDF: a valid page with an empty
	// content stream, so there's no text layer to extract.
	data := buildMinimalPDF(t, "")
	_, err := Text(data, FormatPDF)
	if err != ErrNoExtractableText {
		t.Fatalf("Text() error = %v, want ErrNoExtractableText", err)
	}
}

// buildMinimalDOCX assembles a minimal but real .docx (a zip container
// with the required OOXML parts) containing one paragraph per string in
// paragraphs.
func buildMinimalDOCX(t *testing.T, paragraphs ...string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	write := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("buildMinimalDOCX: create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("buildMinimalDOCX: write %s: %v", name, err)
		}
	}

	write("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`)

	write("_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`)

	var body strings.Builder
	for _, p := range paragraphs {
		body.WriteString("<w:p><w:r><w:t xml:space=\"preserve\">")
		body.WriteString(p)
		body.WriteString("</w:t></w:r></w:p>")
	}

	write("word/document.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>`+body.String()+`</w:body>
</w:document>`)

	if err := zw.Close(); err != nil {
		t.Fatalf("buildMinimalDOCX: close zip: %v", err)
	}
	return buf.Bytes()
}

// buildMinimalPDF assembles a minimal but real single-page PDF (correct
// xref offsets computed as it's written) whose content stream shows text
// via a single Tj operator. Passing "" produces a valid page with an
// empty content stream, standing in for a scanned/image-only PDF that has
// no text layer.
func buildMinimalPDF(t *testing.T, text string) []byte {
	t.Helper()

	var buf bytes.Buffer
	var offsets []int
	write := func(format string, args ...any) {
		fmt.Fprintf(&buf, format, args...)
	}
	startObj := func() {
		offsets = append(offsets, buf.Len())
	}

	write("%%PDF-1.4\n")

	startObj()
	write("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	startObj()
	write("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	startObj()
	write("3 0 obj\n<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 4 0 R >> >> /MediaBox [0 0 612 792] /Contents 5 0 R >>\nendobj\n")

	startObj()
	write("4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")

	var content string
	if text != "" {
		content = fmt.Sprintf("BT /F1 12 Tf 72 712 Td (%s) Tj ET", escapePDFString(text))
	}
	startObj()
	write("5 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", len(content), content)

	xrefStart := buf.Len()
	write("xref\n0 %d\n", len(offsets)+1)
	write("0000000000 65535 f \n")
	for _, off := range offsets {
		write("%010d 00000 n \n", off)
	}
	write("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF", len(offsets)+1, xrefStart)

	return buf.Bytes()
}

func escapePDFString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`)
	return r.Replace(s)
}
