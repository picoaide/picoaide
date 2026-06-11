package email

import (
  "bytes"
  "encoding/base64"
  "regexp"
  "strings"
  "testing"
)

func TestStripHTML(t *testing.T) {
  tests := []struct {
    name  string
    input string
    want  string
  }{
    {"simple tags", "<p>Hello</p>", "Hello"},
    {"nested tags", "<div><p>Hello <b>World</b></p></div>", "Hello World"},
    {"self-closing", "Hello<br/>World", "Hello\nWorld"},
    {"with newlines", "<p>Line1</p><p>Line2</p>", "Line1\nLine2"},
    {"empty", "", ""},
    {"no html", "plain text", "plain text"},
  }
  for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
      got := stripHTML(tt.input)
      if got != tt.want {
        t.Errorf("stripHTML(%q) = %q, want %q", tt.input, got, tt.want)
      }
    })
  }
}

func TestBuildMIMEMessage_ContentPresent(t *testing.T) {
  msg := &OutgoingMessage{
    To:       []string{"test@example.com"},
    Subject:  "Test Subject",
    BodyHTML: "<h1>Hello World</h1><p>This is a test.</p>中文内容",
  }
  cfg := &Config{
    Email:     "sender@example.com",
    SMTPHost:  "smtp.example.com",
    SMTPPort:  587,
    LoginUser: "sender",
    LoginPass: "pass",
  }

  data, err := BuildMIMEMessage(msg, cfg)
  if err != nil {
    t.Fatalf("BuildMIMEMessage failed: %v", err)
  }

  // Verify body content is present in base64-encoded form
  re := regexp.MustCompile(`Content-Type: text/plain; charset="utf-8"\r\nContent-Transfer-Encoding: base64\r\n\r\n([A-Za-z0-9+/=\r\n]+)`)
  m := re.FindSubmatch(data)
  if len(m) < 2 {
    t.Fatalf("Could not find plain text part in MIME output:\n%s", string(data))
  }
  decoded, err := base64.StdEncoding.DecodeString(string(regexp.MustCompile(`\s+`).ReplaceAllString(string(m[1]), "")))
  if err != nil {
    t.Fatalf("Failed to decode base64: %v", err)
  }
  if !bytes.Contains(decoded, []byte("Hello World")) {
    t.Fatalf("Body content 'Hello World' not found in decoded text. Decoded: %q", string(decoded))
  }
  if !bytes.Contains(decoded, []byte("中文")) {
    t.Fatalf("Chinese content '中文' not found in decoded text. Decoded: %q", string(decoded))
  }

  reHTML := regexp.MustCompile(`Content-Type: text/html; charset="utf-8"\r\nContent-Transfer-Encoding: base64\r\n\r\n([A-Za-z0-9+/=\r\n]+)`)
  mHTML := reHTML.FindSubmatch(data)
  if len(mHTML) < 2 {
    t.Fatalf("Could not find HTML part in MIME output:\n%s", string(data))
  }
  decodedHTML, err := base64.StdEncoding.DecodeString(string(regexp.MustCompile(`\s+`).ReplaceAllString(string(mHTML[1]), "")))
  if err != nil {
    t.Fatalf("Failed to decode HTML base64: %v", err)
  }
  if !bytes.Contains(decodedHTML, []byte("<h1>Hello World</h1>")) {
    t.Fatalf("HTML content not found. Decoded: %q", string(decodedHTML))
  }
}

func TestBuildMIMEMessage_Simple(t *testing.T) {
  msg := &OutgoingMessage{
    To:       []string{"recipient@example.com"},
    Subject:  "Test Subject",
    BodyHTML: "<h1>Hello</h1><p>This is a test.</p>",
  }
  cfg := &Config{
    Email:     "sender@example.com",
    SMTPHost:  "smtp.example.com",
    SMTPPort:  587,
    LoginUser: "sender",
    LoginPass: "pass",
  }

  data, err := BuildMIMEMessage(msg, cfg)
  if err != nil {
    t.Fatalf("BuildMIMEMessage failed: %v", err)
  }

  if !bytes.Contains(data, []byte("From: sender@example.com")) {
    t.Error("missing From header")
  }
  if !bytes.Contains(data, []byte("To: recipient@example.com")) {
    t.Error("missing To header")
  }
  if !bytes.Contains(data, []byte("Subject: Test Subject")) {
    t.Errorf("missing or incorrect Subject header, got:\n%s", string(data))
  }
  if !bytes.Contains(data, []byte("MIME-Version: 1.0")) {
    t.Error("missing MIME-Version header")
  }
  if !bytes.Contains(data, []byte("Content-Type: multipart/alternative")) {
    t.Error("missing multipart/alternative content type")
  }
  if !bytes.Contains(data, []byte("Content-Type: text/plain")) {
    t.Error("missing text/plain part")
  }
  if !bytes.Contains(data, []byte("Content-Type: text/html")) {
    t.Error("missing text/html part")
  }
  if !bytes.Contains(data, []byte("Message-ID: <")) {
    t.Error("missing Message-ID header")
  }
}

func TestBuildMIMEMessage_WithAttachments(t *testing.T) {
  msg := &OutgoingMessage{
    To:       []string{"recipient@example.com"},
    Subject:  "With Attachment",
    BodyHTML: "<p>See attached</p>",
    Attachments: []Attachment{
      {
        Filename: "test.txt",
        MimeType: "text/plain",
        Content:  []byte("hello world"),
      },
    },
  }
  cfg := &Config{
    Email:     "sender@example.com",
    SMTPHost:  "smtp.example.com",
    SMTPPort:  587,
    LoginUser: "sender",
    LoginPass: "pass",
  }

  data, err := BuildMIMEMessage(msg, cfg)
  if err != nil {
    t.Fatalf("BuildMIMEMessage failed: %v", err)
  }

  if !bytes.Contains(data, []byte("Content-Type: multipart/mixed")) {
    t.Error("expected multipart/mixed for attachments")
  }
  if !bytes.Contains(data, []byte("Content-Disposition: attachment; filename=\"test.txt\"")) {
    t.Error("missing Content-Disposition attachment header")
  }
  if !bytes.Contains(data, []byte("Content-Type: text/plain; charset=\"utf-8\"")) {
    t.Error("missing attachment content type")
  }
  if !bytes.Contains(data, []byte("aGVsbG8gd29ybGQ=")) {
    t.Error("missing base64 content, got:\n" + string(data))
  }
}

func TestBuildMIMEMessage_BccRemoved(t *testing.T) {
  msg := &OutgoingMessage{
    To:  []string{"to@example.com"},
    Bcc: []string{"bcc@example.com"},
  }
  cfg := &Config{Email: "from@example.com"}

  data, err := BuildMIMEMessage(msg, cfg)
  if err != nil {
    t.Fatalf("BuildMIMEMessage failed: %v", err)
  }

  if bytes.Contains(data, []byte("bcc@example.com")) {
    t.Error("BCC addresses should not appear in headers")
  }
}

func TestBuildMIMEMessage_EmptyBody(t *testing.T) {
  msg := &OutgoingMessage{
    To:      []string{"to@example.com"},
    Subject: "Empty",
  }
  cfg := &Config{Email: "from@example.com"}

  _, err := BuildMIMEMessage(msg, cfg)
  if err != nil {
    t.Fatalf("BuildMIMEMessage with empty body failed: %v", err)
  }
}

func TestBuildMIMEMessage_ReplyHeaders(t *testing.T) {
  msg := &OutgoingMessage{
    To:        []string{"to@example.com"},
    Subject:   "Re: Test",
    BodyHTML:  "<p>reply</p>",
    InReplyTo: "<orig@example.com>",
    References: "<orig@example.com> <prev@example.com>",
  }
  cfg := &Config{Email: "from@example.com"}

  data, err := BuildMIMEMessage(msg, cfg)
  if err != nil {
    t.Fatalf("BuildMIMEMessage failed: %v", err)
  }

  if !bytes.Contains(data, []byte("In-Reply-To: <orig@example.com>")) {
    t.Error("missing In-Reply-To header")
  }
  if !bytes.Contains(data, []byte("References: <orig@example.com> <prev@example.com>")) {
    t.Error("missing References header with full chain")
  }
}

func TestStripHTML_Complex(t *testing.T) {
  input := `<html><head><style>.cls{}</style></head><body>
    <script>alert('x')</script>
    <div class="content">
      <h1>Title</h1>
      <p>Para <a href="link">text</a>.</p>
    </div>
  </body></html>`
  got := stripHTML(input)
  if strings.Contains(got, "<style>") {
    t.Error("style tag content should be stripped")
  }
  if strings.Contains(got, "<script>") {
    t.Error("script tag content should be stripped")
  }
  if strings.Contains(got, "alert") {
    t.Error("script content should be stripped")
  }
  if !strings.Contains(got, "Title") {
    t.Error("should contain visible text: Title")
  }
  if !strings.Contains(got, "Para") {
    t.Error("should contain visible text: Para")
  }
  if !strings.Contains(got, "text") {
    t.Error("should contain link text")
  }
}
