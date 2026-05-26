package migrations

import (
  "xorm.io/xorm"
)

func init() {
  Register(Migration{
    Timestamp: "20260525203656",
    Desc:      "mcp_servers 表添加 headers 列",
    Up: func(engine *xorm.Engine) error {
      exists, err := ColumnExists(engine, "mcp_servers", "headers")
      if err != nil {
        return err
      }
      if !exists {
        _, err = engine.Exec("ALTER TABLE mcp_servers ADD COLUMN headers TEXT NOT NULL DEFAULT '{}'")
      }
      return err
    },
  })
}
