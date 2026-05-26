package migrations

import (
  "xorm.io/xorm"
)

func init() {
  Register(Migration{
    Timestamp: "20260525000000",
    Desc:      "local_users 表添加 ip 列",
    Up: func(engine *xorm.Engine) error {
      exists, err := ColumnExists(engine, "local_users", "ip")
      if err != nil {
        return err
      }
      if !exists {
        _, err = engine.Exec("ALTER TABLE local_users ADD COLUMN ip TEXT NOT NULL DEFAULT ''")
      }
      return err
    },
  })
}
