package migrations

import "xorm.io/xorm"

func init() {
  Register(Migration{
    Timestamp: "20260610120000",
    Desc:      "创建 user_emails 表",
    Up: func(engine *xorm.Engine) error {
      _, err := engine.Exec(`CREATE TABLE IF NOT EXISTS user_emails (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        username TEXT NOT NULL UNIQUE,
        email TEXT NOT NULL,
        smtp_host TEXT NOT NULL,
        smtp_port INTEGER NOT NULL DEFAULT 587,
        smtp_tls INTEGER NOT NULL DEFAULT 1,
        imap_host TEXT NOT NULL,
        imap_port INTEGER NOT NULL DEFAULT 993,
        imap_tls INTEGER NOT NULL DEFAULT 1,
        login_user TEXT NOT NULL,
        login_password TEXT NOT NULL,
        enabled INTEGER NOT NULL DEFAULT 0,
        test_result TEXT NOT NULL DEFAULT '',
        created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
        updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime'))
      )`)
      return err
    },
  })
}
