package migrations

import (
  "fmt"

  "xorm.io/xorm"
)

func init() {
  Register(Migration{
    Timestamp: "20250515000000",
    Desc:      "创建 picoclaw_adapter_packages 表",
    Up: func(engine *xorm.Engine) error {
      _, err := engine.Exec(fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
        id                        INTEGER PRIMARY KEY AUTOINCREMENT,
        adapter_version           TEXT NOT NULL,
        adapter_schema_version    INTEGER NOT NULL DEFAULT 1,
        latest_supported_config_version INTEGER NOT NULL DEFAULT 3,
        content                   TEXT NOT NULL,
        hash                      TEXT NOT NULL DEFAULT '',
        refreshed_at              TEXT NOT NULL DEFAULT (datetime('now', 'localtime')),
        created_at                TEXT NOT NULL DEFAULT (datetime('now', 'localtime'))
      )`, "picoclaw_adapter_packages"))
      return err
    },
  })
}
