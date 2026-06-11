package migrations

import (
  "xorm.io/xorm"
)

func init() {
  Register(Migration{
    Timestamp: "20250504000000",
    Desc:      "添加 user_skills.source 列",
    Up: func(engine *xorm.Engine) error {
      exists, err := ColumnExists(engine, "user_skills", "source")
      if err != nil {
        return err
      }
      if !exists {
        _, err = engine.Exec("ALTER TABLE user_skills ADD COLUMN source TEXT NOT NULL DEFAULT ''")
      }
      return err
    },
  })
}
