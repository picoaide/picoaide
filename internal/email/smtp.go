package email

import (
  "bytes"
  "crypto/rand"
  "crypto/tls"
  "encoding/base64"
  "encoding/hex"
  "fmt"
  "mime"
  "net"
  "net/smtp"
  "regexp"
  "strings"
  "time"

  "github.com/gomarkdown/markdown"
)

const smtpTimeout = 5 * time.Second

var htmlTagRe = regexp.MustCompile(`(?i)<[^>]*>`)
var blockTagRe = regexp.MustCompile(`(?i)<\s*(br|/p|/div|/li|/h[1-6]|/tr|/blockquote)[^>]*>`)
var looksLikeHTML = regexp.MustCompile(`(?i)<\s*(html|body|div|p|br|h[1-6]|table|tr|td|th|a|img|ul|ol|li|span|b|i|u|strong|em|style|script|!--)[^>]*>`)

func stripHTML(html string) string {
  reBr := regexp.MustCompile(`(?i)<\s*br[^>]*>`)
  html = reBr.ReplaceAllString(html, "\n")
  reBlock := regexp.MustCompile(`(?i)<\s*(/p|/div|/li|/h[1-6]|/tr|/blockquote)[^>]*>`)
  html = reBlock.ReplaceAllString(html, "\n")
  reScript := regexp.MustCompile(`(?i)<\s*script[^>]*>.*?<\s*/script\s*>`)
  html = reScript.ReplaceAllString(html, "")
  reStyle := regexp.MustCompile(`(?i)<\s*style[^>]*>.*?<\s*/style\s*>`)
  html = reStyle.ReplaceAllString(html, "")
  html = htmlTagRe.ReplaceAllString(html, "")
  html = strings.ReplaceAll(html, "&nbsp;", " ")
  html = strings.ReplaceAll(html, "&amp;", "&")
  html = strings.ReplaceAll(html, "&lt;", "<")
  html = strings.ReplaceAll(html, "&gt;", ">")
  html = strings.ReplaceAll(html, "&quot;", "\"")

  lines := strings.Split(html, "\n")
  for i, line := range lines {
    lines[i] = strings.TrimSpace(line)
  }
  html = strings.Join(lines, "\n")
  re := regexp.MustCompile(`\n{3,}`)
  html = re.ReplaceAllString(html, "\n\n")
  return strings.TrimSpace(html)
}

func markdownToHTML(md string) string {
  return string(markdown.ToHTML([]byte(md), nil, nil))
}

func generateMessageID(cfg *Config) string {
  return fmt.Sprintf("<%d@%s>", time.Now().UnixNano(), cfg.SMTPHost)
}

func writeHeader(buf *bytes.Buffer, key, value string) {
  fmt.Fprintf(buf, "%s: %s\r\n", key, value)
}

func encodeBase64Body(text string) []byte {
  encoded := base64.StdEncoding.EncodeToString([]byte(text))
  var buf bytes.Buffer
  for i := 0; i < len(encoded); i += 76 {
    end := i + 76
    if end > len(encoded) {
      end = len(encoded)
    }
    buf.WriteString(encoded[i:end])
    buf.WriteString("\r\n")
  }
  return buf.Bytes()
}

func generateBoundary() string {
  b := make([]byte, 24)
  rand.Read(b)
  return hex.EncodeToString(b)
}

// BuildMIMEMessage 构建 RFC 2822 MIME 邮件消息
// 如果正文不是 HTML，自动当作 Markdown 处理并转换为 HTML
func BuildMIMEMessage(msg *OutgoingMessage, cfg *Config) ([]byte, error) {
  var buf bytes.Buffer
  messageID := generateMessageID(cfg)

  htmlBody := msg.BodyHTML
  plainBody := ""

  if looksLikeHTML.MatchString(htmlBody) {
    plainBody = stripHTML(htmlBody)
  } else {
    // 非 HTML，当作 Markdown 处理
    plainBody = htmlBody
    htmlBody = markdownToHTML(htmlBody)
  }

  if len(msg.Attachments) > 0 {
    buildMixed(&buf, msg, cfg, messageID, plainBody, htmlBody)
  } else {
    buildAlternative(&buf, msg, cfg, messageID, plainBody, htmlBody)
  }

  return buf.Bytes(), nil
}

func buildHeaders(buf *bytes.Buffer, msg *OutgoingMessage, cfg *Config, messageID string, contentType string) {
  writeHeader(buf, "From", cfg.Email)
  writeHeader(buf, "To", strings.Join(msg.To, ", "))
  if len(msg.Cc) > 0 {
    writeHeader(buf, "Cc", strings.Join(msg.Cc, ", "))
  }
  writeHeader(buf, "Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
  writeHeader(buf, "MIME-Version", "1.0")
  writeHeader(buf, "Content-Type", contentType)
  writeHeader(buf, "Message-ID", messageID)
  writeHeader(buf, "Date", time.Now().Format(time.RFC1123Z))

  if msg.ReplyTo != "" {
    writeHeader(buf, "Reply-To", msg.ReplyTo)
  }
  if msg.InReplyTo != "" {
    writeHeader(buf, "In-Reply-To", msg.InReplyTo)
  }
  if msg.References != "" {
    writeHeader(buf, "References", msg.References)
  }
  buf.WriteString("\r\n")
}

func buildAlternative(buf *bytes.Buffer, msg *OutgoingMessage, cfg *Config, messageID, plainBody, htmlBody string) {
  boundary := generateBoundary()
  ct := fmt.Sprintf("multipart/alternative; boundary=\"%s\"", boundary)
  buildHeaders(buf, msg, cfg, messageID, ct)

  fmt.Fprintf(buf, "--%s\r\n", boundary)
  fmt.Fprintf(buf, "Content-Type: text/plain; charset=\"utf-8\"\r\n")
  fmt.Fprintf(buf, "Content-Transfer-Encoding: base64\r\n")
  fmt.Fprintf(buf, "\r\n")
  buf.Write(encodeBase64Body(plainBody))

  fmt.Fprintf(buf, "--%s\r\n", boundary)
  fmt.Fprintf(buf, "Content-Type: text/html; charset=\"utf-8\"\r\n")
  fmt.Fprintf(buf, "Content-Transfer-Encoding: base64\r\n")
  fmt.Fprintf(buf, "\r\n")
  buf.Write(encodeBase64Body(htmlBody))

  fmt.Fprintf(buf, "--%s--\r\n", boundary)
}

func buildMixed(buf *bytes.Buffer, msg *OutgoingMessage, cfg *Config, messageID, plainBody, htmlBody string) {
  boundary := generateBoundary()
  ct := fmt.Sprintf("multipart/mixed; boundary=\"%s\"", boundary)
  buildHeaders(buf, msg, cfg, messageID, ct)

  innerBoundary := generateBoundary()

  fmt.Fprintf(buf, "--%s\r\n", boundary)
  fmt.Fprintf(buf, "Content-Type: multipart/alternative; boundary=\"%s\"\r\n", innerBoundary)
  fmt.Fprintf(buf, "\r\n")

  fmt.Fprintf(buf, "--%s\r\n", innerBoundary)
  fmt.Fprintf(buf, "Content-Type: text/plain; charset=\"utf-8\"\r\n")
  fmt.Fprintf(buf, "Content-Transfer-Encoding: base64\r\n")
  fmt.Fprintf(buf, "\r\n")
  buf.Write(encodeBase64Body(plainBody))

  fmt.Fprintf(buf, "--%s\r\n", innerBoundary)
  fmt.Fprintf(buf, "Content-Type: text/html; charset=\"utf-8\"\r\n")
  fmt.Fprintf(buf, "Content-Transfer-Encoding: base64\r\n")
  fmt.Fprintf(buf, "\r\n")
  buf.Write(encodeBase64Body(htmlBody))

  fmt.Fprintf(buf, "--%s--\r\n", innerBoundary)

  for _, a := range msg.Attachments {
    encoded := base64.StdEncoding.EncodeToString(a.Content)
    var encodedBuf bytes.Buffer
    for i := 0; i < len(encoded); i += 76 {
      end := i + 76
      if end > len(encoded) {
        end = len(encoded)
      }
      encodedBuf.WriteString(encoded[i:end])
      encodedBuf.WriteString("\r\n")
    }

    fmt.Fprintf(buf, "--%s\r\n", boundary)
    fmt.Fprintf(buf, "Content-Type: %s\r\n", a.MimeType)
    fmt.Fprintf(buf, "Content-Disposition: attachment; filename=\"%s\"\r\n", a.Filename)
    fmt.Fprintf(buf, "Content-Transfer-Encoding: base64\r\n")
    fmt.Fprintf(buf, "\r\n")
    buf.Write(encodedBuf.Bytes())
  }

  fmt.Fprintf(buf, "--%s--\r\n", boundary)
}

// dialSMTPServer 用于 TestConnection（仅测试连接，不发送数据）
func dialSMTPServer(cfg *Config) (net.Conn, error) {
  addr := net.JoinHostPort(cfg.SMTPHost, fmt.Sprintf("%d", cfg.SMTPPort))
  conn, err := net.DialTimeout("tcp", addr, smtpTimeout)
  if err != nil {
    return nil, &NetworkError{Err: fmt.Errorf("连接 SMTP 服务器失败: %w", err)}
  }
  if cfg.SMTPTLS {
    tlsConn := tls.Client(conn, &tls.Config{ServerName: cfg.SMTPHost})
    if err := tlsConn.Handshake(); err != nil {
      conn.Close()
      return nil, &NetworkError{Err: fmt.Errorf("SMTP TLS 握手失败: %w", err)}
    }
    return tlsConn, nil
  }
  return conn, nil
}

// SendMail 发送邮件，返回 Message-ID
func SendMail(cfg *Config, msg *OutgoingMessage) (messageID string, err error) {
  data, err := BuildMIMEMessage(msg, cfg)
  if err != nil {
    return "", fmt.Errorf("构建邮件失败: %w", err)
  }

  addr := net.JoinHostPort(cfg.SMTPHost, fmt.Sprintf("%d", cfg.SMTPPort))
  auth := smtp.PlainAuth("", cfg.LoginUser, cfg.LoginPass, cfg.SMTPHost)
  recipients := append(append([]string{}, msg.To...), msg.Cc...)
  recipients = append(recipients, msg.Bcc...)

  if cfg.SMTPTLS {
    conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: cfg.SMTPHost})
    if err != nil {
      return "", &NetworkError{Err: fmt.Errorf("连接 SMTP 服务器失败: %w", err)}
    }
    client, err := smtp.NewClient(conn, cfg.SMTPHost)
    if err != nil {
      conn.Close()
      return "", &ProtocolError{Err: fmt.Errorf("创建 SMTP 客户端失败: %w", err)}
    }
    defer client.Close()

    if err = client.Auth(auth); err != nil {
      return "", &AuthError{Err: fmt.Errorf("SMTP 认证失败: %w", err)}
    }
    if err = client.Mail(cfg.Email); err != nil {
      return "", &ProtocolError{Err: fmt.Errorf("设置发件人失败: %w", err)}
    }
    for _, rcpt := range recipients {
      if err = client.Rcpt(rcpt); err != nil {
        return "", &ProtocolError{Err: fmt.Errorf("设置收件人 %s 失败: %w", rcpt, err)}
      }
    }
    w, err := client.Data()
    if err != nil {
      return "", &ProtocolError{Err: fmt.Errorf("获取 DATA writer 失败: %w", err)}
    }
    if _, err = w.Write(data); err != nil {
      return "", &ProtocolError{Err: fmt.Errorf("写入邮件数据失败: %w", err)}
    }
    if err = w.Close(); err != nil {
      return "", &ProtocolError{Err: fmt.Errorf("关闭 DATA writer 失败: %w", err)}
    }
    client.Quit()
  } else {
    err = smtp.SendMail(addr, auth, cfg.Email, recipients, data)
    if err != nil {
      return "", fmt.Errorf("发送邮件失败: %w", err)
    }
  }

  return extractMessageID(data), nil
}

func extractMessageID(data []byte) string {
  re := regexp.MustCompile(`(?m)^Message-ID:\s*(.*)$`)
  m := re.FindSubmatch(data)
  if len(m) < 2 {
    return ""
  }
  return strings.TrimSpace(string(m[1]))
}
