package migrations

import "xorm.io/xorm"

func init() {
  Register(Migration{
    Timestamp: "20260522000000",
    Desc:      "添加 user_channels 表的 credentials 列",
    Up: func(engine *xorm.Engine) error {
      // 确保表存在
      rows, err := engine.Query("SELECT 1 FROM sqlite_master WHERE type='table' AND name='user_channels'")
      if err != nil {
        return err
      }
      if len(rows) == 0 {
        _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS user_channels (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          username TEXT NOT NULL,
          channel TEXT NOT NULL,
          allowed INTEGER NOT NULL DEFAULT 1,
          enabled INTEGER NOT NULL DEFAULT 0,
          configured INTEGER NOT NULL DEFAULT 0,
          config_version INTEGER NOT NULL DEFAULT 0,
          credentials TEXT DEFAULT '',
          updated_at DATETIME NOT NULL DEFAULT (datetime('now','localtime')),
          UNIQUE(username, channel)
        )`)
        return err
      }
      exists, err := ColumnExists(engine, "user_channels", "credentials")
      if err != nil {
        return err
      }
      if !exists {
        _, err = engine.Exec("ALTER TABLE user_channels ADD COLUMN credentials TEXT DEFAULT ''")
      }
      return err
    },
  })
}
