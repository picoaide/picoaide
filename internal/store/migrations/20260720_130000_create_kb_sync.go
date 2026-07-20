package migrations

import "xorm.io/xorm"

func init() {
	Register(Migration{
		Timestamp: "20260720130000",
		Desc:      "创建知识库同步配置表",
		Up: func(engine *xorm.Engine) error {
			_, err := engine.Exec(`
				CREATE TABLE IF NOT EXISTS kb_sync_sources (
					id            INTEGER PRIMARY KEY AUTOINCREMENT,
					kb_id         INTEGER NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
					url           TEXT NOT NULL,
					description   TEXT DEFAULT '',
					sync_enabled  INTEGER DEFAULT 0,
					sync_cron     TEXT DEFAULT '',
					last_sync_at  DATETIME,
					checksum      TEXT DEFAULT '',
					created_at    DATETIME DEFAULT (datetime('now','localtime')),
					updated_at    DATETIME DEFAULT (datetime('now','localtime'))
				);
				CREATE INDEX IF NOT EXISTS idx_kb_sync_sources_kb ON kb_sync_sources(kb_id);
			`)
			return err
		},
	})
}
