package email

import (
  "crypto/tls"
  "fmt"
  "io"
  "mime"
  "strings"

  "github.com/emersion/go-imap"
  "github.com/emersion/go-imap/client"
  "golang.org/x/text/encoding/charmap"
  "golang.org/x/text/encoding/simplifiedchinese"
  "golang.org/x/text/transform"
)

// ============================================================
// IMAP 连接
// ============================================================

// dialIMAP 连接并登录 IMAP 服务器
func dialIMAP(cfg *Config) (*client.Client, error) {
  addr := fmt.Sprintf("%s:%d", cfg.IMAPHost, cfg.IMAPPort)
  var c *client.Client
  var err error

  if cfg.IMAPTLS {
    c, err = client.DialTLS(addr, &tls.Config{ServerName: cfg.IMAPHost})
  } else {
    c, err = client.Dial(addr)
    if err == nil {
      if err := c.StartTLS(&tls.Config{ServerName: cfg.IMAPHost}); err != nil {
        c.Logout()
        return nil, &ProtocolError{Err: fmt.Errorf("IMAP STARTTLS 失败: %w", err)}
      }
    }
  }
  if err != nil {
    return nil, &NetworkError{Err: fmt.Errorf("连接 IMAP 服务器失败: %w", err)}
  }

  if err := c.Login(cfg.LoginUser, cfg.LoginPass); err != nil {
    c.Logout()
    return nil, &AuthError{Err: fmt.Errorf("IMAP 登录失败: %w", err)}
  }

  return c, nil
}

// ============================================================
// TestConnection
// ============================================================

// TestConnection 测试 SMTP 和 IMAP 连接
func TestConnection(cfg *Config) (smtpOK bool, imapOK bool, err error) {
  var smtpErr, imapErr error

  smtpClient, smtpErr := dialSMTPServer(cfg)
  if smtpErr == nil {
    smtpClient.Close()
    smtpOK = true
  }

  imapClient, imapErr := dialIMAP(cfg)
  if imapErr == nil {
    imapClient.Logout()
    imapOK = true
  }

  if smtpOK && imapOK {
    return true, true, nil
  }

  var msg string
  if !smtpOK {
    msg = "SMTP: " + smtpErr.Error()
  }
  if !imapOK {
    if msg != "" {
      msg += "; "
    }
    msg += "IMAP: " + imapErr.Error()
  }
  return smtpOK, imapOK, fmt.Errorf("%s", msg)
}

// ============================================================
// 辅助函数
// ============================================================

func decodeSubject(s string) string {
  if s == "" {
    return s
  }
  dec := &mime.WordDecoder{
    CharsetReader: func(charset string, input io.Reader) (io.Reader, error) {
      charset = strings.ToLower(charset)
      switch charset {
      case "gbk", "gb2312", "gb18030":
        return transform.NewReader(input, simplifiedchinese.GBK.NewDecoder()), nil
      case "big5":
        return transform.NewReader(input, charmap.Windows1255.NewDecoder()), nil
      default:
        return nil, fmt.Errorf("不支持的字符集: %s", charset)
      }
    },
  }
  decoded, err := dec.DecodeHeader(s)
  if err != nil {
    return s
  }
  return decoded
}

func storeFlags(c *client.Client, seqSet *imap.SeqSet, op imap.FlagsOp, flags []interface{}) error {
  ch := make(chan *imap.Message, 1)
  done := make(chan error, 1)
  go func() {
    done <- c.Store(seqSet, imap.FormatFlagsOp(op, false), flags, ch)
  }()
  for range ch {
  }
  return <-done
}

func expunge(c *client.Client) error {
  ch := make(chan uint32, 1)
  done := make(chan error, 1)
  go func() {
    done <- c.Expunge(ch)
  }()
  for range ch {
  }
  return <-done
}

// extractAttachments 递归提取附件信息
func extractAttachments(bs *imap.BodyStructure, msg *Message) {
  if bs == nil {
    return
  }
  if strings.EqualFold(bs.Disposition, "attachment") {
    info := AttachmentInfo{
      Filename: bs.DispositionParams["filename"],
      MimeType: bs.MIMEType + "/" + bs.MIMESubType,
      Size:     int64(bs.Size),
    }
    if info.Filename == "" {
      info.Filename = bs.DispositionParams["name"]
    }
    msg.Attachments = append(msg.Attachments, info)
  }
  for _, part := range bs.Parts {
    extractAttachments(part, msg)
  }
}

// ============================================================
// 邮件列表
// ============================================================

// ListMessages 列出指定文件夹中的邮件，支持分页
func ListMessages(cfg *Config, folder string, limit, offset int) ([]*MessageSummary, uint32, error) {
  c, err := dialIMAP(cfg)
  if err != nil {
    return nil, 0, err
  }
  defer c.Logout()

  mbox, err := c.Select(folder, true)
  if err != nil {
    return nil, 0, &ProtocolError{Err: fmt.Errorf("选择文件夹 %s 失败: %w", folder, err)}
  }

  total := mbox.Messages
  if limit <= 0 {
    limit = 20
  }
  if limit > 100 {
    limit = 100
  }

  start := uint32(1)
  if total > uint32(offset) {
    start = total - uint32(offset)
  }
  if total > uint32(offset+limit) {
    start = total - uint32(offset) - uint32(limit) + 1
  }
  if start < 1 {
    start = 1
  }

  seqSet := new(imap.SeqSet)
  seqSet.AddRange(start, total-uint32(offset))

  messages := make(chan *imap.Message, limit)
  done := make(chan error, 1)
  go func() {
    done <- c.Fetch(seqSet, []imap.FetchItem{imap.FetchEnvelope, imap.FetchFlags}, messages)
  }()

  var result []*MessageSummary
  for msg := range messages {
    summary := &MessageSummary{
      UID:   msg.SeqNum,
      Flags: msg.Flags,
    }
    if msg.Envelope != nil {
      summary.Subject = decodeSubject(msg.Envelope.Subject)
      summary.Date = msg.Envelope.Date
      if len(msg.Envelope.From) > 0 {
        summary.From = msg.Envelope.From[0].Address()
      }
    }
    result = append(result, summary)
  }
  if err := <-done; err != nil {
    return nil, 0, &ProtocolError{Err: fmt.Errorf("获取邮件列表失败: %w", err)}
  }

  return result, total, nil
}

// ============================================================
// 获取完整邮件
// ============================================================

// FetchMessage 获取指定 UID 的完整邮件内容
func FetchMessage(cfg *Config, uid uint32, markSeen bool) (*Message, error) {
  c, err := dialIMAP(cfg)
  if err != nil {
    return nil, err
  }
  defer c.Logout()

  _, err = c.Select("INBOX", false)
  if err != nil {
    return nil, &ProtocolError{Err: fmt.Errorf("选择 INBOX 失败: %w", err)}
  }

  seqSet := new(imap.SeqSet)
  seqSet.AddNum(uid)

  // 获取邮件正文（BODY[TEXT] 不含 MIME 头）
  textSection := &imap.BodySectionName{}
  textSection.Specifier = imap.TextSpecifier
  items := []imap.FetchItem{imap.FetchEnvelope, imap.FetchFlags, imap.FetchBodyStructure, textSection.FetchItem()}

  messages := make(chan *imap.Message, 1)
  done := make(chan error, 1)
  go func() {
    done <- c.Fetch(seqSet, items, messages)
  }()

  msg := <-messages
  if err := <-done; err != nil {
    return nil, &ProtocolError{Err: fmt.Errorf("获取邮件失败: %w", err)}
  }
  if msg == nil {
    return nil, fmt.Errorf("未找到 UID %d 的邮件", uid)
  }

  result := &Message{
    UID:   uid,
    Flags: msg.Flags,
  }
  if msg.Envelope != nil {
    result.Subject = decodeSubject(msg.Envelope.Subject)
    result.Date = msg.Envelope.Date
    result.MessageID = msg.Envelope.MessageId
    if msg.Envelope.InReplyTo != "" {
      result.InReplyTo = msg.Envelope.InReplyTo
    }
    if len(msg.Envelope.From) > 0 {
      result.From = msg.Envelope.From[0].Address()
    }
    for _, addr := range msg.Envelope.To {
      result.To = append(result.To, addr.Address())
    }
    for _, addr := range msg.Envelope.Cc {
      result.Cc = append(result.Cc, addr.Address())
    }
  }

  // 解析正文（BODY[TEXT] 为纯文本内容，其他内容做 HTML 探测）
  for section, literal := range msg.Body {
    bodyBytes, _ := io.ReadAll(literal)
    bodyStr := string(bodyBytes)
    if section != nil && section.Specifier == imap.TextSpecifier {
      result.BodyText = bodyStr
    } else {
      if strings.Contains(strings.ToLower(bodyStr), "<html") || strings.Contains(strings.ToLower(bodyStr), "<!doctype") {
        result.BodyHTML = bodyStr
      } else if result.BodyText == "" {
        result.BodyText = bodyStr
      }
    }
  }

  if msg.BodyStructure != nil {
    extractAttachments(msg.BodyStructure, result)
  }

  if markSeen {
    seqSet = new(imap.SeqSet)
    seqSet.AddNum(uid)
    if err := storeFlags(c, seqSet, imap.AddFlags, []interface{}{imap.SeenFlag}); err != nil {
      return nil, &ProtocolError{Err: fmt.Errorf("标记已读失败: %w", err)}
    }
  }

  return result, nil
}

// ============================================================
// 搜索邮件
// ============================================================

func asciiOnly(s string) string {
  var b strings.Builder
  b.Grow(len(s))
  for _, r := range s {
    if r <= 127 {
      b.WriteRune(r)
    }
  }
  return b.String()
}

func parseSearchQuery(query string) *imap.SearchCriteria {
  criteria := imap.NewSearchCriteria()
  parts := strings.Fields(query)
  for i := 0; i < len(parts); i++ {
    upper := strings.ToUpper(parts[i])
    switch upper {
    case "FROM":
      if i+1 < len(parts) {
        i++
        criteria.Header["From"] = []string{asciiOnly(parts[i])}
      }
    case "SUBJECT":
      if i+1 < len(parts) {
        i++
        criteria.Header["Subject"] = []string{asciiOnly(parts[i])}
      }
    case "BODY":
      if i+1 < len(parts) {
        i++
        criteria.Text = append(criteria.Text, asciiOnly(parts[i]))
      }
    case "TO":
      if i+1 < len(parts) {
        i++
        criteria.Header["To"] = []string{asciiOnly(parts[i])}
      }
    case "CC":
      if i+1 < len(parts) {
        i++
        criteria.Header["Cc"] = []string{asciiOnly(parts[i])}
      }
    case "UNSEEN":
      criteria.WithoutFlags = []string{imap.SeenFlag}
    case "SEEN":
      criteria.WithFlags = []string{imap.SeenFlag}
    default:
      criteria.Text = append(criteria.Text, asciiOnly(parts[i]))
    }
  }
  return criteria
}

// SearchMessages 在指定文件夹中搜索邮件
func SearchMessages(cfg *Config, folder, query string, limit int) ([]*MessageSummary, error) {
  c, err := dialIMAP(cfg)
  if err != nil {
    return nil, err
  }
  defer c.Logout()

  _, err = c.Select(folder, true)
  if err != nil {
    return nil, &ProtocolError{Err: fmt.Errorf("选择文件夹 %s 失败: %w", folder, err)}
  }

  criteria := parseSearchQuery(query)
  seqNums, err := c.Search(criteria)
  if err != nil {
    return nil, &ProtocolError{Err: fmt.Errorf("搜索失败: %w", err)}
  }

  if len(seqNums) == 0 {
    return nil, nil
  }

  if limit <= 0 {
    limit = 50
  }
  if len(seqNums) > limit {
    seqNums = seqNums[len(seqNums)-limit:]
  }

  seqSet := new(imap.SeqSet)
  for _, num := range seqNums {
    seqSet.AddNum(num)
  }

  messages := make(chan *imap.Message, len(seqNums))
  done := make(chan error, 1)
  go func() {
    done <- c.Fetch(seqSet, []imap.FetchItem{imap.FetchEnvelope, imap.FetchFlags}, messages)
  }()

  var result []*MessageSummary
  for msg := range messages {
    summary := &MessageSummary{
      UID:   msg.SeqNum,
      Flags: msg.Flags,
    }
    if msg.Envelope != nil {
      summary.Subject = decodeSubject(msg.Envelope.Subject)
      summary.Date = msg.Envelope.Date
      if len(msg.Envelope.From) > 0 {
        summary.From = msg.Envelope.From[0].Address()
      }
    }
    result = append(result, summary)
  }
  if err := <-done; err != nil {
    return nil, err
  }
  return result, nil
}

// ============================================================
// 删除邮件
// ============================================================

// DeleteMessage 删除指定 UID 的邮件
func DeleteMessage(cfg *Config, uid uint32, hard bool) error {
  c, err := dialIMAP(cfg)
  if err != nil {
    return err
  }
  defer c.Logout()

  _, err = c.Select("INBOX", false)
  if err != nil {
    return &ProtocolError{Err: fmt.Errorf("选择 INBOX 失败: %w", err)}
  }

  seqSet := new(imap.SeqSet)
  seqSet.AddNum(uid)

  if hard {
    if err := storeFlags(c, seqSet, imap.AddFlags, []interface{}{imap.DeletedFlag}); err != nil {
      return &ProtocolError{Err: fmt.Errorf("标记删除失败: %w", err)}
    }
    if err := expunge(c); err != nil {
      return &ProtocolError{Err: fmt.Errorf("永久删除失败: %w", err)}
    }
  } else {
    if err := c.Move(seqSet, "Trash"); err != nil {
      if err := c.Move(seqSet, "INBOX.Trash"); err != nil {
        if err := storeFlags(c, seqSet, imap.AddFlags, []interface{}{imap.DeletedFlag}); err != nil {
          return &ProtocolError{Err: fmt.Errorf("移动到回收站失败: %w", err)}
        }
      }
    }
  }
  return nil
}

// ============================================================
// 移动邮件
// ============================================================

// MoveMessage 将邮件移动到目标文件夹
func MoveMessage(cfg *Config, uid uint32, targetFolder string) error {
  c, err := dialIMAP(cfg)
  if err != nil {
    return err
  }
  defer c.Logout()

  _, err = c.Select("INBOX", false)
  if err != nil {
    return &ProtocolError{Err: fmt.Errorf("选择 INBOX 失败: %w", err)}
  }

  seqSet := new(imap.SeqSet)
  seqSet.AddNum(uid)

  if err := c.Move(seqSet, targetFolder); err != nil {
    return &ProtocolError{Err: fmt.Errorf("移动到 %s 失败: %w", targetFolder, err)}
  }
  return nil
}

// ============================================================
// 文件夹列表
// ============================================================

// ListFolders 列出所有邮件文件夹及其未读计数
func ListFolders(cfg *Config) ([]*Folder, error) {
  c, err := dialIMAP(cfg)
  if err != nil {
    return nil, err
  }
  defer c.Logout()

  mailboxes := make(chan *imap.MailboxInfo, 50)
  done := make(chan error, 1)
  go func() {
    done <- c.List("", "*", mailboxes)
  }()

  var result []*Folder
  for mbox := range mailboxes {
    status, err := c.Status(mbox.Name, []imap.StatusItem{imap.StatusUnseen})
    unread := uint32(0)
    if err == nil && status != nil {
      unread = status.Unseen
    }
    result = append(result, &Folder{
      Name:      mbox.Name,
      Delimiter: mbox.Delimiter,
      Unread:    unread,
    })
  }
  if err := <-done; err != nil {
    return nil, &ProtocolError{Err: fmt.Errorf("列出文件夹失败: %w", err)}
  }
  return result, nil
}

// ============================================================
// 回复邮件
// ============================================================

// Reply 回复指定邮件
func Reply(cfg *Config, uid uint32, body string, replyAll bool) (string, error) {
  original, err := FetchMessage(cfg, uid, false)
  if err != nil {
    return "", fmt.Errorf("读取原邮件失败: %w", err)
  }

  subject := "Re: " + original.Subject
  if !strings.HasPrefix(strings.ToUpper(original.Subject), "RE:") {
    subject = "Re: " + original.Subject
  }

  to := []string{original.From}
  cc := []string{}
  if replyAll {
    cc = original.Cc
    var filteredCc []string
    for _, addr := range cc {
      if !strings.EqualFold(addr, cfg.Email) {
        filteredCc = append(filteredCc, addr)
      }
    }
    cc = filteredCc
    for _, addr := range original.To {
      if !strings.EqualFold(addr, cfg.Email) {
        to = append(to, addr)
      }
    }
  }

  outgoing := &OutgoingMessage{
    To:        to,
    Cc:        cc,
    Subject:   subject,
    BodyHTML:  body,
    InReplyTo: original.MessageID,
  }
  if original.References != "" {
    outgoing.References = original.References + " " + original.MessageID
  } else {
    outgoing.References = original.MessageID
  }

  return SendMail(cfg, outgoing)
}

// ============================================================
// 转发邮件
// ============================================================

// Forward 转发指定邮件
func Forward(cfg *Config, uid uint32, to []string, body string) (string, error) {
  original, err := FetchMessage(cfg, uid, false)
  if err != nil {
    return "", fmt.Errorf("读取原邮件失败: %w", err)
  }

  subject := "Fwd: " + original.Subject
  if !strings.HasPrefix(strings.ToUpper(original.Subject), "FWD:") {
    subject = "Fwd: " + original.Subject
  }

  forwardBody := body
  if original.BodyHTML != "" {
    forwardBody = body + "\n\n<hr>\n<blockquote>" + original.BodyHTML + "</blockquote>"
  } else if original.BodyText != "" {
    forwardBody = body + "\n\n---\n" + original.BodyText
  }

  outgoing := &OutgoingMessage{
    To:       to,
    Subject:  subject,
    BodyHTML: forwardBody,
  }
  return SendMail(cfg, outgoing)
}
