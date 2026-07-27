package migrations

import "xorm.io/xorm"

func init() {
  Register(Migration{
    Timestamp: "20260727120000",
    Desc:      "创建用户 IM 绑定表",
    Up: func(engine *xorm.Engine) error {
      _, err := engine.Exec(`CREATE TABLE IF NOT EXISTS user_im_bindings (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        username TEXT NOT NULL,
        platform TEXT NOT NULL,
        external_user_id TEXT NOT NULL DEFAULT '',
        external_chat_id TEXT NOT NULL DEFAULT '',
        created_at DATETIME DEFAULT (datetime('now','localtime')),
        updated_at DATETIME DEFAULT (datetime('now','localtime')),
        UNIQUE(username, platform),
        UNIQUE(platform, external_user_id)
      )`)
      if err != nil {
        return err
      }
      _, err = engine.Exec(`CREATE INDEX IF NOT EXISTS idx_uib_username ON user_im_bindings(username)`)
      if err != nil {
        return err
      }
      _, err = engine.Exec(`CREATE INDEX IF NOT EXISTS idx_uib_platform_extid ON user_im_bindings(platform, external_user_id)`)
      return err
    },
  })
}
