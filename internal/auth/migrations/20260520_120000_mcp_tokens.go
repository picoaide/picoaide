package migrations

import (
  "xorm.io/xorm"
)

func init() {
  Register(Migration{
    Timestamp: "20260520120000",
    Desc:      "创建 mcp_tokens 表并迁移 containers.mcp_token 数据",
    Up: func(engine *xorm.Engine) error {
      // 创建 mcp_tokens 表
      _, err := engine.Exec(`CREATE TABLE IF NOT EXISTS mcp_tokens (
        username TEXT PRIMARY KEY,
        token TEXT NOT NULL DEFAULT '',
        created_at DATETIME DEFAULT (datetime('now','localtime')),
        updated_at DATETIME DEFAULT (datetime('now','localtime'))
      )`)
      if err != nil {
        return err
      }
      // 从 containers 表迁移已有 token（如果 mcp_token 列存在）
      exists, err := ColumnExists(engine, "containers", "mcp_token")
      if err != nil {
        return err
      }
      if exists {
        _, err = engine.Exec(`INSERT OR IGNORE INTO mcp_tokens (username, token, created_at, updated_at)
          SELECT username, mcp_token, created_at, updated_at FROM containers WHERE mcp_token != ''`)
      }
      return err
    },
  })
}
