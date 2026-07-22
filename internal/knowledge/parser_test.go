package knowledge

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestParseText_Markdown(t *testing.T) {
	data := []byte("# Hello World\n\nSome content here")
	r, err := Parse("test.md", data)
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "Hello World" {
		t.Errorf("title = %q, want %q", r.Title, "Hello World")
	}
	if r.Content != string(data) {
		t.Errorf("content mismatch")
	}
	if r.FileType != "md" {
		t.Errorf("filetype = %q, want md", r.FileType)
	}
}

func TestParseText_Plain(t *testing.T) {
	data := []byte("first line\nmore content")
	r, err := Parse("notes.txt", data)
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "first line" {
		t.Errorf("title = %q, want %q", r.Title, "first line")
	}
	if r.FileType != "txt" {
		t.Errorf("filetype = %q, want txt", r.FileType)
	}
}

func TestParse_Unsupported(t *testing.T) {
	_, err := Parse("file.xlsx", []byte("data"))
	if err == nil {
		t.Fatal("expected error for unsupported file type")
	}
}

func TestParseHTML(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title>Test Article</title></head><body><article><h1>Hello</h1><p>This is a test article with enough content for readability to extract.</p><p>More paragraphs to ensure we get meaningful text output from the parser.</p></article></body></html>`
	r, err := Parse("page.html", []byte(html))
	if err != nil {
		t.Fatal(err)
	}
	if r.Title == "" {
		t.Error("expected non-empty title")
	}
	if r.Content == "" {
		t.Error("expected non-empty content")
	}
	if r.FileType != "html" {
		t.Errorf("filetype = %q, want html", r.FileType)
	}
}

func TestParsePDF(t *testing.T) {
	// Create a minimal valid PDF with text
	pdf := createTestPDF(t)
	r, err := Parse("doc.pdf", pdf)
	if err != nil {
		t.Fatal(err)
	}
	if r.FileType != "pdf" {
		t.Errorf("filetype = %q, want pdf", r.FileType)
	}
}

func TestParseDOCX(t *testing.T) {
	docx := createTestDOCX(t)
	r, err := Parse("doc.docx", docx)
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "Test Document" {
		t.Errorf("title = %q, want %q", r.Title, "Test Document")
	}
	if !strings.Contains(r.Content, "Hello World") {
		t.Errorf("content should contain 'Hello World', got: %s", r.Content)
	}
	if r.FileType != "docx" {
		t.Errorf("filetype = %q, want docx", r.FileType)
	}
}

func TestParseZIP(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	f1, _ := zw.Create("a.txt")
	f1.Write([]byte("# File A\ncontent a"))

	f2, _ := zw.Create("b.txt")
	f2.Write([]byte("first line b\ncontent b"))

	zw.Close()

	r, err := Parse("archive.zip", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if r.FileType != "zip" {
		t.Errorf("filetype = %q, want zip", r.FileType)
	}
	if len(r.Pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(r.Pages))
	}
	if r.Pages[0].Title != "File A" {
		t.Errorf("first page title = %q, want 'File A'", r.Pages[0].Title)
	}
	if r.Pages[1].Title != "first line b" {
		t.Errorf("second page title = %q, want 'first line b'", r.Pages[1].Title)
	}
}

func TestParseZIP_PathTraversal(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	f1, _ := zw.Create("../evil.txt")
	f1.Write([]byte("evil content"))

	f2, _ := zw.Create("safe.txt")
	f2.Write([]byte("safe content"))

	zw.Close()

	r, err := Parse("archive.zip", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Pages) != 1 {
		t.Fatalf("expected 1 page (traversal rejected), got %d", len(r.Pages))
	}
	if r.Pages[0].Title != "safe content" {
		t.Errorf("title = %q, want 'safe content'", r.Pages[0].Title)
	}
}

func TestParseZIP_DepthLimit(t *testing.T) {
	// level1.zip -> level2.zip -> level3.zip (should only go 2 deep)
	var level3Buf bytes.Buffer
	w3 := zip.NewWriter(&level3Buf)
	f3, _ := w3.Create("deep.txt")
	f3.Write([]byte("too deep"))
	w3.Close()

	var level2Buf bytes.Buffer
	w2 := zip.NewWriter(&level2Buf)
	f2, _ := w2.Create("nested.zip")
	f2.Write(level3Buf.Bytes())
	w2.Close()

	var level1Buf bytes.Buffer
	w1 := zip.NewWriter(&level1Buf)
	f1, _ := w1.Create("middle.zip")
	f1.Write(level2Buf.Bytes())
	w1.Close()

	r, err := Parse("outer.zip", level1Buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(r.Pages))
	}
	if r.Pages[0].Title != "middle.zip" {
		t.Errorf("expected 'middle.zip', got %q", r.Pages[0].Title)
	}
}

func TestParse_EmptyFile(t *testing.T) {
	r, err := Parse("empty.md", []byte{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "" {
		t.Errorf("expected empty title, got %q", r.Title)
	}
	if r.Content != "" {
		t.Errorf("expected empty content, got %q", r.Content)
	}
}

func TestParseHTML_NoReadableContent(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title>No Article</title></head><body><div>just some text without article tag</div></body></html>`
	r, err := Parse("page.html", []byte(html))
	if err != nil {
		t.Fatal(err)
	}
	if r.Title == "" {
		t.Error("expected title to be extracted")
	}
	if r.Content == "" {
		t.Error("expected some content to be parsed")
	}
}

func TestParseZIP_EmptyZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	zw.Close()

	r, err := Parse("empty.zip", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if r.FileType != "zip" {
		t.Errorf("filetype = %q, want zip", r.FileType)
	}
	if len(r.Pages) != 0 {
		t.Errorf("expected 0 pages for empty zip, got %d", len(r.Pages))
	}
}

func TestParse_ExtensionCase(t *testing.T) {
	data := []byte("# Hello\nworld")
	r, err := Parse("README.MD", data)
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "Hello" {
		t.Errorf("title = %q, want 'Hello'", r.Title)
	}
	if r.FileType != "md" {
		t.Errorf("filetype = %q, want md", r.FileType)
	}

	r, err = Parse("NOTES.TXT", []byte("first line"))
	if err != nil {
		t.Fatal(err)
	}
	if r.FileType != "txt" {
		t.Errorf("filetype = %q, want txt", r.FileType)
	}
}

func TestParseDOCX_EmptyDocument(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	ct, _ := w.Create("[Content_Types].xml")
	ct.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`))

	rels, _ := w.Create("_rels/.rels")
	rels.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`))

	wrels, _ := w.Create("word/_rels/document.xml.rels")
	wrels.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
</Relationships>`))

	doc, _ := w.Create("word/document.xml")
	doc.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
  </w:body>
</w:document>`))

	w.Close()

	r, err := Parse("empty.docx", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if r.FileType != "docx" {
		t.Errorf("filetype = %q, want docx", r.FileType)
	}
}

func createTestPDF(t *testing.T) []byte {
	t.Helper()
	// Minimal valid PDF
	pdf := `%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Kids [3 0 R] /Count 1 >>
endobj
3 0 obj
<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>
endobj
4 0 obj
<< /Length 44 >>
stream
BT /F1 12 Tf 100 700 Td (Hello World) Tj ET
endstream
endobj
5 0 obj
<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>
endobj
xref
0 6
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
0000000266 00000 n 
0000000360 00000 n 
trailer
<< /Size 6 /Root 1 0 R >>
startxref
410
%%EOF`
	return []byte(pdf)
}

func createTestDOCX(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	// [Content_Types].xml
	ct, _ := w.Create("[Content_Types].xml")
	ct.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`))

	// _rels/.rels
	rels, _ := w.Create("_rels/.rels")
	rels.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`))

	// word/_rels/document.xml.rels
	wrels, _ := w.Create("word/_rels/document.xml.rels")
	wrels.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
</Relationships>`))

	doc, _ := w.Create("word/document.xml")
	doc.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:r>
        <w:t>Test Document</w:t>
      </w:r>
    </w:p>
    <w:p>
      <w:r>
        <w:t>Hello World</w:t>
      </w:r>
    </w:p>
  </w:body>
</w:document>`))

	w.Close()
	return buf.Bytes()
}
