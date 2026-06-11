package store

import (
  "context"
  "crypto/rand"
  "database/sql"
  "encoding/hex"
  "fmt"
  "log/slog"
  "os"
  "path/filepath"
  "strings"
  "time"

  _ "modernc.org/sqlite"
  "xorm.io/xorm"

  "github.com/picoaide/picoaide/internal/store/migrations"
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
  memory:  19 * 1024,
  time:    2,
  threads: 1,
  keyLen:  32,
  saltLen: 16,
}

var (
  engine        *xorm.Engine
  dbDataDir     string
  SkillsRootDir string
)

// InitDB 打开或创建 SQLite 数据库
func InitDB(dataDir string) error {
  dbDataDir = dataDir
  SkillsRootDir = filepath.Join(dataDir, "skills")

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
  engine.SetDefaultCacher(nil)

  if _, err := engine.Exec("PRAGMA foreign_keys = ON"); err != nil {
    return fmt.Errorf("启用外键约束失败: %w", err)
  }

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

// GetEngine 返回 xorm 引擎
func GetEngine() (*xorm.Engine, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  return engine, nil
}

// ensureDB 确保数据库连接可用
func ensureDB() error {
  if engine != nil {
    return nil
  }
  if dbDataDir == "" {
    return fmt.Errorf("数据库未初始化")
  }
  return InitDB(dbDataDir)
}

// syncSchema 创建表结构
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
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS skills (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS user_skills (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL,
    skill_name TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT '2000-01-01 00:00:00',
    UNIQUE(username, skill_name)
  )`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE INDEX IF NOT EXISTS idx_user_skills_username ON user_skills(username)`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE INDEX IF NOT EXISTS idx_user_skills_skill_name ON user_skills(skill_name)`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS shared_folders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    is_public INTEGER NOT NULL DEFAULT 0,
    created_by TEXT NOT NULL DEFAULT 'system',
    created_at DATETIME NOT NULL DEFAULT (datetime('now','localtime')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS shared_folder_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    folder_id INTEGER NOT NULL REFERENCES shared_folders(id) ON DELETE CASCADE,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    UNIQUE(folder_id, group_id)
  )`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE INDEX IF NOT EXISTS idx_sfg_folder_id ON shared_folder_groups(folder_id)`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE INDEX IF NOT EXISTS idx_sfg_group_id ON shared_folder_groups(group_id)`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS shared_folder_mounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    folder_id INTEGER NOT NULL REFERENCES shared_folders(id) ON DELETE CASCADE,
    username TEXT NOT NULL,
    mounted INTEGER NOT NULL DEFAULT 0,
    checked_at DATETIME NOT NULL DEFAULT (datetime('now','localtime')),
    UNIQUE(folder_id, username)
  )`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE INDEX IF NOT EXISTS idx_sfm_folder_id ON shared_folder_mounts(folder_id)`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE INDEX IF NOT EXISTS idx_sfm_username ON shared_folder_mounts(username)`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS user_cookies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL,
    domain TEXT NOT NULL,
    cookies TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT (datetime('now','localtime')),
    UNIQUE(username, domain)
  )`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE INDEX IF NOT EXISTS idx_user_cookies_username ON user_cookies(username)`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS memory_evolution_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL,
    session_key TEXT NOT NULL,
    changes_summary TEXT NOT NULL DEFAULT '',
    files_modified TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE INDEX IF NOT EXISTS idx_mel_username ON memory_evolution_log(username)`)
  if err != nil {
    return err
  }
  _, err = engine.Exec(`CREATE INDEX IF NOT EXISTS idx_mel_session_key ON memory_evolution_log(session_key)`)
  if err != nil {
    return err
  }

  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS user_emails (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL,
    smtp_host TEXT NOT NULL,
    smtp_port INTEGER NOT NULL DEFAULT 587,
    smtp_tls INTEGER NOT NULL DEFAULT 1,
    imap_host TEXT NOT NULL,
    imap_port INTEGER NOT NULL DEFAULT 993,
    imap_tls INTEGER NOT NULL DEFAULT 1,
    login_user TEXT NOT NULL,
    login_password TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0,
    test_result TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime'))
  )`)
  if err != nil {
    return err
  }

  if err := migrations.RunAll(engine); err != nil {
    return err
  }

  return nil
}

// StartAuditLogCleaner 定时清理 90 天前的审计日志
func StartAuditLogCleaner(ctx context.Context) {
  go func() {
    select {
    case <-time.After(5 * time.Second):
    case <-ctx.Done():
      return
    }
    ticker := time.NewTicker(6 * time.Hour)
    defer ticker.Stop()
    for {
      runCleanBatch(ctx)
      select {
      case <-ticker.C:
      case <-ctx.Done():
        return
      }
    }
  }()
}

func runCleanBatch(ctx context.Context) {
  for i := 0; i < 200; i++ {
    select {
    case <-ctx.Done():
      return
    default:
    }
    eng, err := GetEngine()
    if err != nil {
      slog.Warn("clean_memory_evolution_log_db_error", "error", err.Error())
      return
    }
    result, err := eng.Exec(`DELETE FROM memory_evolution_log WHERE id IN (
      SELECT id FROM memory_evolution_log WHERE created_at < datetime('now', '-90 days') LIMIT 1000
    )`)
    if err != nil {
      slog.Warn("clean_memory_evolution_log_error", "error", err.Error())
      return
    }
    affected, _ := result.RowsAffected()
    if affected == 0 {
      return
    }
    slog.Debug("clean_memory_evolution_log", "batch_deleted", affected)
    time.Sleep(100 * time.Millisecond)
  }
}

// GenerateMCPToken 为用户生成 MCP token
func GenerateMCPToken(username string) (string, error) {
  if err := ensureDB(); err != nil {
    return "", err
  }
  b := make([]byte, 32)
  if _, err := rand.Read(b); err != nil {
    return "", fmt.Errorf("生成随机数失败: %w", err)
  }
  token := username + ":" + hex.EncodeToString(b)
  _, err := engine.Exec(
    `INSERT INTO mcp_tokens (username, token, updated_at) VALUES (?, ?, datetime('now','localtime'))
     ON CONFLICT(username) DO UPDATE SET token = ?, updated_at = datetime('now','localtime')`,
    username, token, token,
  )
  if err != nil {
    return "", fmt.Errorf("保存 MCP token 失败: %w", err)
  }
  return token, nil
}

// GetMCPToken 获取用户的 MCP token
func GetMCPToken(username string) (string, error) {
  if err := ensureDB(); err != nil {
    return "", err
  }
  rows, err := engine.Query("SELECT token FROM mcp_tokens WHERE username = ? LIMIT 1", username)
  if err != nil {
    return "", err
  }
  if len(rows) == 0 {
    return "", nil
  }
  return string(rows[0]["token"]), nil
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

var _ = func() *sql.DB {
  var e *xorm.Engine
  if e != nil {
    return e.DB().DB
  }
  return nil
}()
