package auth

import (
  "crypto/rand"
  "database/sql"
  "encoding/hex"
  "fmt"
  "os"
  "path/filepath"
  "strings"
  "time"

  _ "modernc.org/sqlite"
  "xorm.io/xorm"
)

const dbFileName = "picoaide.db"

const argon2idHashPrefix = "$argon2id$"

var passwordHashParams = struct {
  memory  uint32
  time    uint32
  threads uint8
  keyLen  uint32
  saltLen int
}{
  memory:  4 * 1024,
  time:    1,
  threads: 1,
  keyLen:  32,
  saltLen: 16,
}

var (
  engine    *xorm.Engine
  dbDataDir string
)

// ============================================================
// 数据库初始化
// ============================================================

// InitDB 打开或创建 SQLite 数据库
func InitDB(dataDir string) error {
  dbDataDir = dataDir

  if err := os.MkdirAll(dataDir, 0755); err != nil {
    return fmt.Errorf("创建数据库目录失败: %w", err)
  }

  dbPath := filepath.Join(dataDir, dbFileName)

  var err error
  engine, err = xorm.NewEngine("sqlite", dbPath)
  if err != nil {
    return fmt.Errorf("打开数据库失败: %w", err)
  }

  if err := engine.Ping(); err != nil {
    // 数据库损坏，备份后重建
    engine.Close()
    engine = nil
    backupPath := dbPath + ".broken." + time.Now().Format("20060102-150405")
    fmt.Fprintf(os.Stderr, "数据库损坏，已备份到 %s，正在重建\n", backupPath)
    os.Rename(dbPath, backupPath)

    engine, err = xorm.NewEngine("sqlite", dbPath)
    if err != nil {
      return fmt.Errorf("重建数据库失败: %w", err)
    }
  }

  engine.SetMaxOpenConns(1)
  // 禁用 xorm 缓存，避免与手动 SQL 操作产生不一致
  engine.SetDefaultCacher(nil)

  if err := syncSchema(); err != nil {
    return fmt.Errorf("创建数据表失败: %w", err)
  }

  return nil
}

// ResetDB 关闭当前数据库连接并重置全局状态（测试用）
func ResetDB() {
  if engine != nil {
    engine.Close()
  }
  engine = nil
  dbDataDir = ""
}

// GetEngine 返回 xorm 引擎（供新代码使用）
func GetEngine() (*xorm.Engine, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  return engine, nil
}

// ensureDB 确保数据库连接可用，engine 为 nil 时自动重连
func ensureDB() error {
  if engine != nil {
    return nil
  }
  if dbDataDir == "" {
    return fmt.Errorf("数据库未初始化")
  }
  return InitDB(dbDataDir)
}

// syncSchema 使用原始 SQL 创建表结构（保留 SQLite datetime 默认值），并做必要的迁移
func syncSchema() error {
  _, err := engine.Exec(`CREATE TABLE IF NOT EXISTS local_users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'user',
    source TEXT NOT NULL DEFAULT 'local',
    created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime'))
  )`)
  if err != nil {
    return err
  }
  // 迁移：旧数据库 local_users 表没有 source 列
  engine.Exec(`ALTER TABLE local_users ADD COLUMN source TEXT NOT NULL DEFAULT 'local'`)
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS containers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    container_id TEXT,
    image TEXT NOT NULL,
    status TEXT DEFAULT 'stopped',
    ip TEXT,
    cpu_limit REAL DEFAULT 0,
    memory_limit INTEGER DEFAULT 0,
    mcp_token TEXT DEFAULT '',
    created_at DATETIME DEFAULT (datetime('now','localtime')),
    updated_at DATETIME DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    return err
  }
  // 迁移：已有表添加 mcp_token 字段
  engine.Exec(`ALTER TABLE containers ADD COLUMN mcp_token TEXT DEFAULT ''`)
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS settings_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key TEXT NOT NULL,
    old_value TEXT,
    new_value TEXT,
    changed_by TEXT NOT NULL DEFAULT 'system',
    changed_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS whitelist (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    added_by TEXT NOT NULL DEFAULT 'system',
    added_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    parent_id INTEGER REFERENCES groups(id) ON DELETE SET NULL,
    source TEXT NOT NULL DEFAULT 'local',
    description TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS user_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    UNIQUE(username, group_id)
  )`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE INDEX IF NOT EXISTS idx_user_groups_username ON user_groups(username)`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE INDEX IF NOT EXISTS idx_user_groups_group_id ON user_groups(group_id)`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS group_skills (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    skill_name TEXT NOT NULL,
    UNIQUE(group_id, skill_name)
  )`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS user_channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL,
    channel TEXT NOT NULL,
    allowed INTEGER NOT NULL DEFAULT 1,
    enabled INTEGER NOT NULL DEFAULT 0,
    configured INTEGER NOT NULL DEFAULT 0,
    config_version INTEGER NOT NULL DEFAULT 0,
    updated_at DATETIME NOT NULL DEFAULT (datetime('now','localtime')),
    UNIQUE(username, channel)
  )`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE INDEX IF NOT EXISTS idx_user_channels_username ON user_channels(username)`)
  if err != nil {
    return err
  }

  // 迁移：旧数据库 groups 表没有 parent_id 列
  engine.Exec(`ALTER TABLE groups ADD COLUMN parent_id INTEGER REFERENCES groups(id) ON DELETE SET NULL`)

  return nil
}

// ============================================================
// MCP Token 管理
// ============================================================

// GenerateMCPToken 为用户生成 MCP token（用户名:随机hex）并存入 DB
func GenerateMCPToken(username string) (string, error) {
  if err := ensureDB(); err != nil {
    return "", err
  }
  b := make([]byte, 32)
  if _, err := rand.Read(b); err != nil {
    return "", fmt.Errorf("生成随机数失败: %w", err)
  }
  token := username + ":" + hex.EncodeToString(b)

  // 先尝试 UPDATE，如果无匹配行则 INSERT
  affected, err := engine.Where("username = ?", username).
    Cols("mcp_token").
    Update(&ContainerRecord{MCPToken: token})
  if err != nil {
    return "", fmt.Errorf("保存 MCP token 失败: %w", err)
  }
  if affected == 0 {
    // 无匹配行，执行 INSERT
    _, err = engine.Insert(&ContainerRecord{
      Username: username,
      Image:    "",
      Status:   "stopped",
      MCPToken: token,
    })
    if err != nil {
      return "", fmt.Errorf("创建容器记录失败: %w", err)
    }
  }
  return token, nil
}

// GetMCPToken 获取用户的 MCP token
func GetMCPToken(username string) (string, error) {
  if err := ensureDB(); err != nil {
    return "", err
  }
  var rec ContainerRecord
  has, err := engine.Where("username = ?", username).Get(&rec)
  if err != nil {
    return "", err
  }
  if !has {
    return "", nil
  }
  return rec.MCPToken, nil
}

// ValidateMCPToken 验证 MCP token，返回用户名
func ValidateMCPToken(token string) (string, bool) {
  if token == "" {
    return "", false
  }
  parts := strings.SplitN(token, ":", 2)
  if len(parts) != 2 {
    return "", false
  }
  username := parts[0]
  stored, err := GetMCPToken(username)
  if err != nil || stored != token {
    return "", false
  }
  return username, true
}

// ensure interface compatibility: core.DB embeds *sql.DB
var _ = func() *sql.DB {
  var e *xorm.Engine
  if e != nil {
    return e.DB().DB
  }
  return nil
}
