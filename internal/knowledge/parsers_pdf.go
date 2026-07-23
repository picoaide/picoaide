package knowledge

import (
  "bytes"
  "fmt"
  "io"
  "os/exec"
  "strings"

  "github.com/pdfcpu/pdfcpu/pkg/api"
  "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

type PDFParser struct{}

func (PDFParser) Parse(filename string, data []byte) (*ParseResult, error) {
  rs := bytes.NewReader(data)
  var contentBuf bytes.Buffer

  conf := model.NewDefaultConfiguration()
  err := api.ExtractContent(rs, []string{"1-"}, func(r io.Reader, pageNr int) error {
    b, err := io.ReadAll(r)
    if err != nil {
      return err
    }
    contentBuf.WriteString(extractTextFromPDFStream(string(b)))
    contentBuf.WriteString("\n")
    return nil
  }, conf)

  if err != nil {
    contentBuf.Reset()
  }

  text := contentBuf.String()

  if strings.TrimSpace(text) == "" {
    ocrText, err := ocrPDF(data)
    if err == nil {
      text = ocrText
    }
  }

  title := extractTextTitle(text)

  return &ParseResult{
    Title:    title,
    Content:  text,
    FileType: "pdf",
    FileSize: int64(len(data)),
  }, nil
}

// ponytail: simple PDF operator text extraction, not a full PDF text engine
func extractTextFromPDFStream(s string) string {
  var b strings.Builder
  for _, line := range strings.Split(s, "\n") {
    line = strings.TrimSpace(line)
    if strings.HasPrefix(line, "(") {
      for _, suffix := range []string{") Tj", ") TJ", "\""} {
        if idx := strings.LastIndex(line, suffix); idx > 0 {
          b.WriteString(line[1:idx])
          break
        }
      }
    }
  }
  return b.String()
}

func ocrPDF(data []byte) (string, error) {
  path, err := exec.LookPath("tesseract")
  if err != nil {
    return "", fmt.Errorf("tesseract not available")
  }
  cmd := exec.Command(path, "stdin", "stdout", "-l", "eng")
  cmd.Stdin = bytes.NewReader(data)
  out, err := cmd.Output()
  if err != nil {
    return "", fmt.Errorf("tesseract failed: %w", err)
  }
  return string(out), nil
}

func ocrImage(data []byte, ext string) (string, error) {
  path, err := exec.LookPath("tesseract")
  if err != nil {
    return "", fmt.Errorf("tesseract not found: %w", err)
  }
  cmd := exec.Command(path, "stdin", "stdout", "-l", "eng")
  cmd.Stdin = bytes.NewReader(data)
  out, err := cmd.Output()
  if err != nil {
    return "", fmt.Errorf("tesseract failed: %w", err)
  }
  return string(out), nil
}
