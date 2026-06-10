package email

import (
  "crypto/tls"
  "fmt"

  "github.com/emersion/go-imap/client"
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
// 桩函数（后续任务实现）
// ============================================================

func ListMessages(cfg *Config, folder string, limit, offset int) ([]*MessageSummary, uint32, error) {
  return nil, 0, fmt.Errorf("IMAP 操作将在后续任务中实现")
}

func FetchMessage(cfg *Config, uid uint32, markSeen bool) (*Message, error) {
  return nil, fmt.Errorf("IMAP 操作将在后续任务中实现")
}

func SearchMessages(cfg *Config, folder, query string, limit int) ([]*MessageSummary, error) {
  return nil, fmt.Errorf("IMAP 操作将在后续任务中实现")
}

func DeleteMessage(cfg *Config, uid uint32, hard bool) error {
  return fmt.Errorf("IMAP 操作将在后续任务中实现")
}

func MoveMessage(cfg *Config, uid uint32, targetFolder string) error {
  return fmt.Errorf("IMAP 操作将在后续任务中实现")
}

func ListFolders(cfg *Config) ([]*Folder, error) {
  return nil, fmt.Errorf("IMAP 操作将在后续任务中实现")
}

func Reply(cfg *Config, uid uint32, body string, replyAll bool) (string, error) {
  return "", fmt.Errorf("IMAP 操作将在后续任务中实现")
}

func Forward(cfg *Config, uid uint32, to []string, body string) (string, error) {
  return "", fmt.Errorf("IMAP 操作将在后续任务中实现")
}
