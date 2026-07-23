package migrations

import "xorm.io/xorm"

func init() {
  Register(Migration{
    Timestamp: "20260720120000",
    Desc:      "创建知识库相关表",
    Up: func(engine *xorm.Engine) error {
      queries := []string{
        `CREATE TABLE IF NOT EXISTS knowledge_bases (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          name TEXT NOT NULL,
          description TEXT DEFAULT '',
          created_by TEXT NOT NULL,
          created_at DATETIME DEFAULT (datetime('now','localtime')),
          updated_at DATETIME DEFAULT (datetime('now','localtime'))
        )`,
        `CREATE TABLE IF NOT EXISTS kb_folders (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          kb_id INTEGER NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
          parent_id INTEGER REFERENCES kb_folders(id) ON DELETE CASCADE,
          name TEXT NOT NULL,
          permissions_set INTEGER DEFAULT 0,
          created_at DATETIME,
          updated_at DATETIME,
          UNIQUE(kb_id, parent_id, name)
        )`,
        `CREATE INDEX IF NOT EXISTS idx_folders_parent ON kb_folders(parent_id)`,
        `CREATE INDEX IF NOT EXISTS idx_folders_kb ON kb_folders(kb_id)`,
        `CREATE TABLE IF NOT EXISTS kb_folder_users (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          folder_id INTEGER NOT NULL REFERENCES kb_folders(id) ON DELETE CASCADE,
          username TEXT NOT NULL,
          UNIQUE(folder_id, username)
        )`,
        `CREATE INDEX IF NOT EXISTS idx_folder_users_username ON kb_folder_users(username)`,
        `CREATE TABLE IF NOT EXISTS kb_folder_groups (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          folder_id INTEGER NOT NULL REFERENCES kb_folders(id) ON DELETE CASCADE,
          group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
          UNIQUE(folder_id, group_id)
        )`,
        `CREATE INDEX IF NOT EXISTS idx_folder_groups_group ON kb_folder_groups(group_id)`,
        `CREATE TABLE IF NOT EXISTS kb_documents (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          kb_id INTEGER NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
          folder_id INTEGER NOT NULL REFERENCES kb_folders(id) ON DELETE CASCADE,
          title TEXT NOT NULL,
          content TEXT NOT NULL DEFAULT '',
          url TEXT DEFAULT '',
          source_id TEXT DEFAULT '',
          source_type TEXT NOT NULL DEFAULT 'manual',
          file_type TEXT DEFAULT 'md',
          file_size INTEGER DEFAULT 0,
          status TEXT NOT NULL DEFAULT 'pending',
          error_msg TEXT DEFAULT '',
          checksum TEXT DEFAULT '',
          created_by TEXT NOT NULL,
          created_at DATETIME,
          updated_at DATETIME
        )`,
        `CREATE INDEX IF NOT EXISTS idx_documents_folder ON kb_documents(folder_id)`,
        `CREATE INDEX IF NOT EXISTS idx_documents_kb ON kb_documents(kb_id)`,
        `CREATE TABLE IF NOT EXISTS kb_links (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          source_doc INTEGER NOT NULL REFERENCES kb_documents(id) ON DELETE CASCADE,
          target_doc INTEGER NOT NULL REFERENCES kb_documents(id) ON DELETE CASCADE,
          keyword TEXT NOT NULL,
          created_at DATETIME,
          UNIQUE(source_doc, target_doc, keyword)
        )`,
        `CREATE INDEX IF NOT EXISTS idx_links_target ON kb_links(target_doc)`,
        `CREATE TABLE IF NOT EXISTS kb_tags (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          doc_id INTEGER NOT NULL REFERENCES kb_documents(id) ON DELETE CASCADE,
          tag TEXT NOT NULL COLLATE NOCASE,
          UNIQUE(doc_id, tag)
        )`,
        `CREATE INDEX IF NOT EXISTS idx_tags_tag ON kb_tags(tag)`,
        `CREATE TABLE IF NOT EXISTS kb_import_tasks (
          id TEXT PRIMARY KEY,
          kb_id INTEGER NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
          username TEXT NOT NULL,
          status TEXT DEFAULT 'pending',
          progress INTEGER DEFAULT 0,
          file_count INTEGER DEFAULT 0,
          error_msg TEXT DEFAULT '',
          created_at DATETIME,
          updated_at DATETIME
        )`,
        `CREATE TABLE IF NOT EXISTS kb_audit_log (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          username TEXT NOT NULL,
          action TEXT NOT NULL,
          detail TEXT DEFAULT '',
          source TEXT DEFAULT 'web',
          created_at DATETIME
        )`,
        `CREATE INDEX IF NOT EXISTS idx_audit_user ON kb_audit_log(username)`,
        `CREATE VIRTUAL TABLE IF NOT EXISTS kb_documents_fts USING fts5(title, content, content=kb_documents, content_rowid=id)`,
        `CREATE TRIGGER IF NOT EXISTS kb_documents_ai AFTER INSERT ON kb_documents BEGIN
          INSERT INTO kb_documents_fts(rowid, title, content) VALUES (new.id, new.title, new.content);
        END`,
        `CREATE TRIGGER IF NOT EXISTS kb_documents_ad AFTER DELETE ON kb_documents BEGIN
          INSERT INTO kb_documents_fts(kb_documents_fts, rowid, title, content) VALUES('delete', old.id, old.title, old.content);
        END`,
        `CREATE TRIGGER IF NOT EXISTS kb_documents_au AFTER UPDATE ON kb_documents BEGIN
          INSERT INTO kb_documents_fts(kb_documents_fts, rowid, title, content) VALUES('delete', old.id, old.title, old.content);
          INSERT INTO kb_documents_fts(rowid, title, content) VALUES (new.id, new.title, new.content);
        END`,
      }
      for _, q := range queries {
        if _, err := engine.Exec(q); err != nil {
          return err
        }
      }
      return nil
    },
  })
}
