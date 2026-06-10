package email

import (
  "bytes"
  "crypto/tls"
  "encoding/base64"
  "fmt"
  "mime"
  "mime/multipart"
  "net"
  "net/smtp"
  "net/textproto"
  "regexp"
  "strings"
  "time"
)

const smtpTimeout = 5 * time.Second

var htmlTagRe = regexp.MustCompile(`(?i)<[^>]*>`)
var blockTagRe = regexp.MustCompile(`(?i)<\s*(br|/p|/div|/li|/h[1-6]|/tr|/blockquote)[^>]*>`)

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

func generateMessageID(cfg *Config) string {
  return fmt.Sprintf("<%d@%s>", time.Now().UnixNano(), cfg.SMTPHost)
}

func writeHeader(buf *bytes.Buffer, key, value string) {
  fmt.Fprintf(buf, "%s: %s\r\n", key, value)
}

// encodeBase64Body 将文本按 base64 编码，每 76 字符换行
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

// BuildMIMEMessage 构建 RFC 2822 MIME 邮件消息
func BuildMIMEMessage(msg *OutgoingMessage, cfg *Config) ([]byte, error) {
  var buf bytes.Buffer
  messageID := generateMessageID(cfg)

  plainBody := stripHTML(msg.BodyHTML)

  if len(msg.Attachments) > 0 {
    buildMixed(&buf, msg, cfg, messageID, plainBody)
  } else {
    buildAlternative(&buf, msg, cfg, messageID, plainBody)
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

func buildAlternative(buf *bytes.Buffer, msg *OutgoingMessage, cfg *Config, messageID, plainBody string) {
  writer := multipart.NewWriter(buf)
  boundary := writer.Boundary()
  ct := fmt.Sprintf("multipart/alternative; boundary=\"%s\"", boundary)
  buildHeaders(buf, msg, cfg, messageID, ct)

  // text/plain part (base64 编码，兼容中文)
  textPart, _ := writer.CreatePart(textproto.MIMEHeader{
    "Content-Type":              {"text/plain; charset=\"utf-8\""},
    "Content-Transfer-Encoding": {"base64"},
  })
  textPart.Write([]byte(encodeBase64Body(plainBody)))

  // text/html part (base64 编码)
  htmlPart, _ := writer.CreatePart(textproto.MIMEHeader{
    "Content-Type":              {"text/html; charset=\"utf-8\""},
    "Content-Transfer-Encoding": {"base64"},
  })
  htmlPart.Write([]byte(encodeBase64Body(msg.BodyHTML)))

  writer.Close()
}

func buildMixed(buf *bytes.Buffer, msg *OutgoingMessage, cfg *Config, messageID, plainBody string) {
  writer := multipart.NewWriter(buf)
  boundary := writer.Boundary()
  ct := fmt.Sprintf("multipart/mixed; boundary=\"%s\"", boundary)
  buildHeaders(buf, msg, cfg, messageID, ct)

  // First part: multipart/alternative (text + html)
  altBuf := new(bytes.Buffer)
  altWriter := multipart.NewWriter(altBuf)
  altBoundary := altWriter.Boundary()

  altPart, _ := writer.CreatePart(textproto.MIMEHeader{
    "Content-Type": {fmt.Sprintf("multipart/alternative; boundary=\"%s\"", altBoundary)},
  })

  // text/plain sub-part (base64 编码)
  textSub, _ := altWriter.CreatePart(textproto.MIMEHeader{
    "Content-Type":              {"text/plain; charset=\"utf-8\""},
    "Content-Transfer-Encoding": {"base64"},
  })
  textSub.Write([]byte(encodeBase64Body(plainBody)))

  // text/html sub-part (base64 编码)
  htmlSub, _ := altWriter.CreatePart(textproto.MIMEHeader{
    "Content-Type":              {"text/html; charset=\"utf-8\""},
    "Content-Transfer-Encoding": {"base64"},
  })
  htmlSub.Write([]byte(encodeBase64Body(msg.BodyHTML)))

  altWriter.Close()
  altPart.Write(altBuf.Bytes())

  // Attachment parts
  for _, a := range msg.Attachments {
    encoded := base64.StdEncoding.EncodeToString(a.Content)
    // Split into 76-char lines
    var encodedBuf bytes.Buffer
    for i := 0; i < len(encoded); i += 76 {
      end := i + 76
      if end > len(encoded) {
        end = len(encoded)
      }
      encodedBuf.WriteString(encoded[i:end])
      encodedBuf.WriteString("\r\n")
    }

    attachPart, _ := writer.CreatePart(textproto.MIMEHeader{
      "Content-Type":              {fmt.Sprintf("%s; charset=\"utf-8\"", a.MimeType)},
      "Content-Disposition":       {fmt.Sprintf("attachment; filename=\"%s\"", a.Filename)},
      "Content-Transfer-Encoding": {"base64"},
    })
    attachPart.Write(encodedBuf.Bytes())
  }

  writer.Close()
}

// SendMail 发送邮件，返回 Message-ID
func SendMail(cfg *Config, msg *OutgoingMessage) (messageID string, err error) {
  data, err := BuildMIMEMessage(msg, cfg)
  if err != nil {
    return "", fmt.Errorf("构建邮件失败: %w", err)
  }

  client, err := dialSMTPServer(cfg)
  if err != nil {
    return "", err
  }
  defer client.Close()

  if err = smtpAuth(client, cfg); err != nil {
    return "", err
  }

  if err = client.Mail(cfg.Email); err != nil {
    return "", &ProtocolError{Err: fmt.Errorf("设置发件人失败: %w", err)}
  }

  recipients := append(append([]string{}, msg.To...), msg.Cc...)
  recipients = append(recipients, msg.Bcc...)
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

  return extractMessageID(data), nil
}

func dialSMTPServer(cfg *Config) (*smtp.Client, error) {
  addr := net.JoinHostPort(cfg.SMTPHost, fmt.Sprintf("%d", cfg.SMTPPort))

  if cfg.SMTPTLS {
    conn, err := net.DialTimeout("tcp", addr, smtpTimeout)
    if err != nil {
      return nil, &NetworkError{Err: fmt.Errorf("连接 SMTP 服务器失败: %w", err)}
    }
    client, err := smtp.NewClient(conn, cfg.SMTPHost)
    if err != nil {
      conn.Close()
      return nil, &ProtocolError{Err: fmt.Errorf("创建 SMTP 客户端失败: %w", err)}
    }

    // SSL/TLS 模式需要手工 TLS 握手
    tlsConn := tls.Client(conn, &tls.Config{ServerName: cfg.SMTPHost})
    if err := tlsConn.Handshake(); err != nil {
      conn.Close()
      return nil, &NetworkError{Err: fmt.Errorf("SMTP TLS 握手失败: %w", err)}
    }
    client, err = smtp.NewClient(tlsConn, cfg.SMTPHost)
    if err != nil {
      conn.Close()
      return nil, &ProtocolError{Err: fmt.Errorf("创建 SMTP TLS 客户端失败: %w", err)}
    }
    return client, nil
  }

  conn, err := net.DialTimeout("tcp", addr, smtpTimeout)
  if err != nil {
    return nil, &NetworkError{Err: fmt.Errorf("连接 SMTP 服务器失败: %w", err)}
  }

  client, err := smtp.NewClient(conn, cfg.SMTPHost)
  if err != nil {
    conn.Close()
    return nil, &ProtocolError{Err: fmt.Errorf("创建 SMTP 客户端失败: %w", err)}
  }

  // Try STARTTLS
  if ok, _ := client.Extension("STARTTLS"); ok {
    if err = client.StartTLS(&tls.Config{ServerName: cfg.SMTPHost}); err != nil {
      client.Close()
      return nil, &NetworkError{Err: fmt.Errorf("STARTTLS 升级失败: %w", err)}
    }
  }

  return client, nil
}

func smtpAuth(client *smtp.Client, cfg *Config) error {
  if ok, _ := client.Extension("AUTH"); !ok {
    return nil
  }
  auth := smtp.PlainAuth("", cfg.LoginUser, cfg.LoginPass, cfg.SMTPHost)
  if err := client.Auth(auth); err != nil {
    return &AuthError{Err: fmt.Errorf("SMTP 认证失败: %w", err)}
  }
  return nil
}

func extractMessageID(data []byte) string {
  re := regexp.MustCompile(`(?m)^Message-ID:\s*(.*)$`)
  m := re.FindSubmatch(data)
  if len(m) < 2 {
    return ""
  }
  return strings.TrimSpace(string(m[1]))
}
