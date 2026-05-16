package migrations

import (
  "xorm.io/xorm"
)

func init() {
  Register(Migration{
    Timestamp: "20250506000000",
    Desc:      "添加 shared_folders 表历史列（description, is_public, created_by, created_at, updated_at）",
    Up: func(engine *xorm.Engine) error {
      columns := []struct {
        name    string
        typeDef string
      }{
        {"description", "TEXT NOT NULL DEFAULT ''"},
        {"is_public", "INTEGER NOT NULL DEFAULT 0"},
        {"created_by", "TEXT NOT NULL DEFAULT 'system'"},
        {"created_at", "TEXT NOT NULL DEFAULT '2000-01-01 00:00:00'"},
        {"updated_at", "TEXT NOT NULL DEFAULT '2000-01-01 00:00:00'"},
      }
      for _, col := range columns {
        exists, err := ColumnExists(engine, "shared_folders", col.name)
        if err != nil {
          return err
        }
        if !exists {
          if _, err := engine.Exec("ALTER TABLE shared_folders ADD COLUMN " + col.name + " " + col.typeDef); err != nil {
            return err
          }
        }
      }
      return nil
    },
  })
}
