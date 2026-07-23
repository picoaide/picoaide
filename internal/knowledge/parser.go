package knowledge

import (
  "archive/zip"
  "bytes"
  "fmt"
  "io"
  "path/filepath"
  "strings"

  "codeberg.org/readeck/go-readability/v2"
)

type ParseResult struct {
  Title    string
  Content  string
  FileType string
  FileSize int64
  Pages    []ParseResult
}

type Parser interface {
  Parse(filename string, data []byte) (*ParseResult, error)
}

var registry = map[string]Parser{}

func RegisterParser(ext string, p Parser) {
  registry[ext] = p
}

func init() {
  RegisterParser(".md", TextParser{})
  RegisterParser(".txt", TextParser{})
  RegisterParser(".html", HTMLParser{})
  RegisterParser(".pdf", PDFParser{})
  RegisterParser(".docx", DOCXParser{})
  RegisterParser(".zip", ZIPParser{})
}

func Parse(filename string, data []byte) (*ParseResult, error) {
  ext := strings.ToLower(filepath.Ext(filename))
  p, ok := registry[ext]
  if !ok {
    return nil, fmt.Errorf("unsupported file type: %s", ext)
  }
  return p.Parse(filename, data)
}

type TextParser struct{}

func (TextParser) Parse(filename string, data []byte) (*ParseResult, error) {
  content := string(data)
  title := extractTextTitle(content)
  ext := strings.ToLower(filepath.Ext(filename))
  if ext == ".md" {
    ext = "md"
  } else {
    ext = "txt"
  }
  return &ParseResult{
    Title:    title,
    Content:  content,
    FileType: ext,
    FileSize: int64(len(data)),
  }, nil
}

func extractTextTitle(content string) string {
  maxTitleLen := 200
  lines := strings.SplitN(content, "\n", 2)
  if len(lines) == 0 {
    return ""
  }
  first := strings.TrimSpace(lines[0])
  if strings.HasPrefix(first, "# ") {
    first = strings.TrimPrefix(first, "# ")
  }
  if len(first) > maxTitleLen {
    first = first[:maxTitleLen]
  }
  return first
}

type HTMLParser struct{}

func (HTMLParser) Parse(filename string, data []byte) (*ParseResult, error) {
  article, err := readability.FromReader(bytes.NewReader(data), nil)
  if err != nil {
    return &ParseResult{
      Title:    extractHTMLTitle(data),
      Content:  string(data),
      FileType: "html",
      FileSize: int64(len(data)),
    }, nil
  }
  var buf bytes.Buffer
  if err := article.RenderText(&buf); err != nil {
    return nil, fmt.Errorf("render text: %w", err)
  }
  return &ParseResult{
    Title:    article.Title(),
    Content:  buf.String(),
    FileType: "html",
    FileSize: int64(len(data)),
  }, nil
}

// ponytail: minimal title extraction for fallback
func extractHTMLTitle(data []byte) string {
  low := bytes.ToLower(data)
  start := bytes.Index(low, []byte("<title>"))
  if start == -1 {
    return ""
  }
  start += 7
  end := bytes.Index(low[start:], []byte("</title>"))
  if end == -1 {
    return ""
  }
  return strings.TrimSpace(string(data[start : start+end]))
}

type ZIPParser struct{}

func (ZIPParser) Parse(filename string, data []byte) (*ParseResult, error) {
  zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
  if err != nil {
    return nil, fmt.Errorf("open zip: %w", err)
  }
  r := &ParseResult{
    Title:    filename,
    FileType: "zip",
    FileSize: int64(len(data)),
  }
  for _, f := range zr.File {
    if f.FileInfo().IsDir() {
      continue
    }
    if isPathTraversal(f.Name) {
      continue
    }
    rc, err := f.Open()
    if err != nil {
      continue
    }
    fdata, err := io.ReadAll(rc)
    rc.Close()
    if err != nil {
      continue
    }
    sub, err := parseWithDepth(f.Name, fdata, 1)
    if err != nil {
      continue
    }
    r.Pages = append(r.Pages, *sub)
  }
  return r, nil
}

func isPathTraversal(name string) bool {
  return strings.Contains(name, "../") ||
    strings.HasPrefix(name, "/") ||
    strings.HasPrefix(name, "..")
}

func parseWithDepth(filename string, data []byte, depth int) (*ParseResult, error) {
  ext := strings.ToLower(filepath.Ext(filename))
  if ext == ".zip" {
    if depth >= 2 {
      return &ParseResult{
        Title:    filename,
        Content:  "max depth reached",
        FileType: "zip",
        FileSize: int64(len(data)),
      }, nil
    }
    return parseZipNested(filename, data, depth)
  }
  return Parse(filename, data)
}

func parseZipNested(filename string, data []byte, depth int) (*ParseResult, error) {
  zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
  if err != nil {
    return nil, err
  }
  r := &ParseResult{
    Title:    filename,
    FileType: "zip",
    FileSize: int64(len(data)),
  }
  for _, f := range zr.File {
    if f.FileInfo().IsDir() {
      continue
    }
    if isPathTraversal(f.Name) {
      continue
    }
    rc, err := f.Open()
    if err != nil {
      continue
    }
    fdata, err := io.ReadAll(rc)
    rc.Close()
    if err != nil {
      continue
    }
    sub, err := parseWithDepth(f.Name, fdata, depth+1)
    if err != nil {
      continue
    }
    r.Pages = append(r.Pages, *sub)
  }
  return r, nil
}
