package migrations

import (
  "xorm.io/xorm"
)

func init() {
  Register(Migration{
    Timestamp: "20250505000000",
    Desc:      "添加 user_skills.updated_at 列",
    Up: func(engine *xorm.Engine) error {
      exists, err := ColumnExists(engine, "user_skills", "updated_at")
      if err != nil {
        return err
      }
      if !exists {
        _, err = engine.Exec("ALTER TABLE user_skills ADD COLUMN updated_at TEXT NOT NULL DEFAULT '2000-01-01 00:00:00'")
      }
      return err
    },
  })
}
