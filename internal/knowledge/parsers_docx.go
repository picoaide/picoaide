package knowledge

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

type DOCXParser struct{}

func (DOCXParser) Parse(filename string, data []byte) (*ParseResult, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open docx: %w", err)
	}

	text, err := extractDOCXText(zr)
	if err != nil {
		return nil, fmt.Errorf("extract docx text: %w", err)
	}

	imageText, err := extractDOCXImageText(zr)
	if err == nil && imageText != "" {
		text += "\n" + imageText
	}

	title := extractTextTitle(text)

	return &ParseResult{
		Title:    title,
		Content:  text,
		FileType: "docx",
		FileSize: int64(len(data)),
	}, nil
}

type wDocument struct {
	Body wBody `xml:"body"`
}

type wBody struct {
	Paragraphs []wParagraph `xml:"p"`
}

type wParagraph struct {
	Runs []wRun `xml:"r"`
}

type wRun struct {
	Text string `xml:"t"`
}

func extractDOCXText(zr *zip.Reader) (string, error) {
	rc, err := findFileInZip(zr, "word/document.xml")
	if err != nil {
		return "", err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}

	var doc wDocument
	if err := xml.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("parse document.xml: %w", err)
	}

	var b strings.Builder
	for _, p := range doc.Body.Paragraphs {
		for _, r := range p.Runs {
			b.WriteString(r.Text)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func findFileInZip(zr *zip.Reader, name string) (io.ReadCloser, error) {
	for _, f := range zr.File {
		if f.Name == name {
			return f.Open()
		}
	}
	return nil, fmt.Errorf("file not found: %s", name)
}

func extractDOCXImageText(zr *zip.Reader) (string, error) {
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "word/media/") {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			imgData, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				continue
			}
			text, err := ocrImage(imgData, filepath.Ext(f.Name))
			if err == nil && text != "" {
				return text, nil
			}
		}
	}
	return "", nil
}
