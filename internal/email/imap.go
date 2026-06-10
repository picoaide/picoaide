package email

import "fmt"

var errNotImplemented = fmt.Errorf("IMAP 操作将在后续任务中实现")

func ListMessages(cfg *Config, folder string, limit, offset int) ([]*MessageSummary, uint32, error) {
  return nil, 0, errNotImplemented
}

func FetchMessage(cfg *Config, uid uint32, markSeen bool) (*Message, error) {
  return nil, errNotImplemented
}

func SearchMessages(cfg *Config, folder, query string, limit int) ([]*MessageSummary, error) {
  return nil, errNotImplemented
}

func DeleteMessage(cfg *Config, uid uint32, hard bool) error {
  return errNotImplemented
}

func MoveMessage(cfg *Config, uid uint32, targetFolder string) error {
  return errNotImplemented
}

func ListFolders(cfg *Config) ([]*Folder, error) {
  return nil, errNotImplemented
}

func Reply(cfg *Config, uid uint32, body string, replyAll bool) (string, error) {
  return "", errNotImplemented
}

func Forward(cfg *Config, uid uint32, to []string, body string) (string, error) {
  return "", errNotImplemented
}

func TestConnection(cfg *Config) (smtpOK bool, imapOK bool, err error) {
  return false, false, errNotImplemented
}
