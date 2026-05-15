package migrations

import (
  "xorm.io/xorm"
)

func init() {
  Register(Migration{
    Timestamp: "20250503000000",
    Desc:      "添加 groups.parent_id 列",
    Up: func(engine *xorm.Engine) error {
      exists, err := ColumnExists(engine, "groups", "parent_id")
      if err != nil {
        return err
      }
      if !exists {
        _, err = engine.Exec("ALTER TABLE groups ADD COLUMN parent_id INTEGER REFERENCES groups(id) ON DELETE SET NULL")
      }
      return err
    },
  })
}
