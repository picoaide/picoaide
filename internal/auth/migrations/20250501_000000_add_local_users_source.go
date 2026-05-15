package migrations

import (
  "xorm.io/xorm"
)

func init() {
  Register(Migration{
    Timestamp: "20250501000000",
    Desc:      "添加 local_users.source 列",
    Up: func(engine *xorm.Engine) error {
      exists, err := ColumnExists(engine, "local_users", "source")
      if err != nil {
        return err
      }
      if !exists {
        _, err = engine.Exec("ALTER TABLE local_users ADD COLUMN source TEXT NOT NULL DEFAULT 'local'")
      }
      return err
    },
  })
}
