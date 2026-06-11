package email

import "time"

type Config struct {
  Email     string
  SMTPHost  string
  SMTPPort  int
  SMTPTLS   bool
  IMAPHost  string
  IMAPPort  int
  IMAPTLS   bool
  LoginUser string
  LoginPass string
}

type Attachment struct {
  Filename string
  MimeType string
  Content  []byte
}

type OutgoingMessage struct {
  To          []string
  Cc          []string
  Bcc         []string
  Subject     string
  BodyHTML    string
  ReplyTo     string
  InReplyTo   string
  References  string
  Attachments []Attachment
}

type MessageSummary struct {
  UID     uint32    `json:"uid"`
  Subject string    `json:"subject"`
  From    string    `json:"from"`
  Date    time.Time `json:"date"`
  Flags   []string  `json:"flags"`
}

type Message struct {
  UID         uint32           `json:"uid"`
  Subject     string           `json:"subject"`
  From        string           `json:"from"`
  To          []string         `json:"to"`
  Cc          []string         `json:"cc"`
  Date        time.Time        `json:"date"`
  BodyText    string           `json:"bodyText"`
  BodyHTML    string           `json:"bodyHtml"`
  Attachments []AttachmentInfo `json:"attachments"`
  Flags       []string         `json:"flags"`
  MessageID   string           `json:"messageId"`
  InReplyTo   string           `json:"inReplyTo"`
  References  string           `json:"references"`
}

type AttachmentInfo struct {
  Filename string `json:"filename"`
  MimeType string `json:"mimeType"`
  Size     int64  `json:"size"`
}

type Folder struct {
  Name      string `json:"name"`
  Delimiter string `json:"delimiter"`
  Unread    uint32 `json:"unread"`
}
