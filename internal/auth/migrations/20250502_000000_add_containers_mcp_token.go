package migrations

import (
  "xorm.io/xorm"
)

func init() {
  Register(Migration{
    Timestamp: "20250502000000",
    Desc:      "添加 containers.mcp_token 列",
    Up: func(engine *xorm.Engine) error {
      exists, err := ColumnExists(engine, "containers", "mcp_token")
      if err != nil {
        return err
      }
      if !exists {
        _, err = engine.Exec("ALTER TABLE containers ADD COLUMN mcp_token TEXT DEFAULT ''")
      }
      return err
    },
  })
}
