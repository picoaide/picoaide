package migrations

import (
  "xorm.io/xorm"
)

func init() {
  Register(Migration{
    Timestamp: "20260521000000",
    Desc:      "创建 mcp_servers 和 mcp_server_grants 表",
    Up: func(engine *xorm.Engine) error {
      _, err := engine.Exec(`CREATE TABLE IF NOT EXISTS mcp_servers (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT UNIQUE NOT NULL,
        transport TEXT NOT NULL DEFAULT 'stdio',
        command TEXT NOT NULL DEFAULT '',
        args TEXT NOT NULL DEFAULT '[]',
        url TEXT NOT NULL DEFAULT '',
        env TEXT NOT NULL DEFAULT '{}',
        enabled INTEGER NOT NULL DEFAULT 1,
        created_at DATETIME DEFAULT (datetime('now','localtime')),
        updated_at DATETIME DEFAULT (datetime('now','localtime'))
      )`)
      if err != nil {
        return err
      }
      _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS mcp_server_grants (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        server_id INTEGER NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
        grant_type TEXT NOT NULL,
        grant_value TEXT NOT NULL,
        UNIQUE(server_id, grant_type, grant_value)
      )`)
      return err
    },
  })
}
