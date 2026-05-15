package migrations

import (
  "path/filepath"
  "strings"
  "testing"

  _ "modernc.org/sqlite"
  "xorm.io/xorm"
)

func setupTestDB(t *testing.T) *xorm.Engine {
  t.Helper()
  dbPath := filepath.Join(t.TempDir(), "test.db")
  engine, err := xorm.NewEngine("sqlite", dbPath)
  if err != nil {
    t.Fatalf("创建测试数据库失败: %v", err)
  }
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    t.Fatalf("创建 settings 表失败: %v", err)
  }
  return engine
}

// createOldSchema 创建不包含迁移列的旧版本表
func createOldSchema(t *testing.T, engine *xorm.Engine) {
  t.Helper()
  _, err := engine.Exec(`CREATE TABLE IF NOT EXISTS local_users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'user',
    created_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    t.Fatalf("创建 local_users 失败: %v", err)
  }
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS containers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    container_id TEXT,
    image TEXT NOT NULL,
    status TEXT DEFAULT 'stopped',
    ip TEXT,
    cpu_limit REAL DEFAULT 0,
    memory_limit INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT (datetime('now','localtime')),
    updated_at DATETIME DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    t.Fatalf("创建 containers 失败: %v", err)
  }
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    source TEXT NOT NULL DEFAULT 'local',
    description TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    t.Fatalf("创建 groups 失败: %v", err)
  }
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS user_skills (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL,
    skill_name TEXT NOT NULL,
    UNIQUE(username, skill_name)
  )`)
  if err != nil {
    t.Fatalf("创建 user_skills 失败: %v", err)
  }
  _, err = engine.Exec(`CREATE TABLE IF NOT EXISTS shared_folders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
  )`)
  if err != nil {
    t.Fatalf("创建 shared_folders 失败: %v", err)
  }
}

// createFullSchema 创建包含所有列的当前版本表
func createFullSchema(t *testing.T, engine *xorm.Engine) {
  t.Helper()
  _, err := engine.Exec(`CREATE TABLE IF NOT EXISTS local_users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'user',
    source TEXT NOT NULL DEFAULT 'local',
    created_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    t.Fatalf("创建 local_users 失败: %v", err)
  }
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
    t.Fatalf("创建 containers 失败: %v", err)
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
    t.Fatalf("创建 groups 失败: %v", err)
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
    t.Fatalf("创建 user_skills 失败: %v", err)
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
    t.Fatalf("创建 shared_folders 失败: %v", err)
  }
}

// ============================================================
// 测试用例
// ============================================================

func TestRegisterAndAllSorted(t *testing.T) {
  all := All()
  if len(all) == 0 {
    t.Fatal("应该有已注册的迁移")
  }
  for i := 1; i < len(all); i++ {
    if all[i].Timestamp <= all[i-1].Timestamp {
      t.Fatalf("迁移未按时间戳排序: %s <= %s", all[i].Timestamp, all[i-1].Timestamp)
    }
  }
}

func TestColumnExists(t *testing.T) {
  engine := setupTestDB(t)
  defer engine.Close()

  _, err := engine.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY, name TEXT)")
  if err != nil {
    t.Fatal(err)
  }

  exists, err := ColumnExists(engine, "test_table", "id")
  if err != nil {
    t.Fatal(err)
  }
  if !exists {
    t.Fatal("id 列应存在")
  }

  exists, err = ColumnExists(engine, "test_table", "nonexistent")
  if err != nil {
    t.Fatal(err)
  }
  if exists {
    t.Fatal("nonexistent 列不应存在")
  }
}

func TestGetSetSchemaVersion(t *testing.T) {
  engine := setupTestDB(t)
  defer engine.Close()

  if v := getSchemaVersion(engine); v != "" {
    t.Fatalf("初始版本应为空, 得到 %q", v)
  }
  if err := setSchemaVersion(engine, "20250501000000"); err != nil {
    t.Fatal(err)
  }
  if v := getSchemaVersion(engine); v != "20250501000000" {
    t.Fatalf("版本应为 20250501000000, 得到 %q", v)
  }
  if err := setSchemaVersion(engine, "20250502000000"); err != nil {
    t.Fatal(err)
  }
  if v := getSchemaVersion(engine); v != "20250502000000" {
    t.Fatalf("版本应为 20250502000000, 得到 %q", v)
  }
}

// TestRunAllFromOldDB 验证从旧数据库升级时迁移正确执行
func TestRunAllFromOldDB(t *testing.T) {
  engine := setupTestDB(t)
  defer engine.Close()

  createOldSchema(t, engine)

  if err := RunAll(engine); err != nil {
    t.Fatalf("RunAll 失败: %v", err)
  }

  // 验证所有迁移列已添加
  checks := []struct {
    table  string
    column string
  }{
    {"local_users", "source"},
    {"containers", "mcp_token"},
    {"groups", "parent_id"},
    {"user_skills", "source"},
    {"user_skills", "updated_at"},
    {"shared_folders", "description"},
    {"shared_folders", "is_public"},
    {"shared_folders", "created_by"},
    {"shared_folders", "created_at"},
    {"shared_folders", "updated_at"},
  }
  for _, c := range checks {
    exists, err := ColumnExists(engine, c.table, c.column)
    if err != nil {
      t.Fatalf("检查 %s.%s 失败: %v", c.table, c.column, err)
    }
    if !exists {
      t.Fatalf("迁移后 %s.%s 列应存在", c.table, c.column)
    }
  }

  // 验证 schema 版本已更新
  ver := getSchemaVersion(engine)
  if ver == "" {
    t.Fatal("schema 版本应已设置")
  }
}

// TestRunAllIdempotent 验证迁移可重复执行
func TestRunAllIdempotent(t *testing.T) {
  engine := setupTestDB(t)
  defer engine.Close()

  createOldSchema(t, engine)

  if err := RunAll(engine); err != nil {
    t.Fatal(err)
  }
  ver1 := getSchemaVersion(engine)

  // 第二次执行不应报错
  if err := RunAll(engine); err != nil {
    t.Fatalf("第二次 RunAll 不应失败: %v", err)
  }
  ver2 := getSchemaVersion(engine)

  if ver1 != ver2 {
    t.Fatalf("版本不应变化: %q -> %q", ver1, ver2)
  }
}

// TestRunAllFreshInstall 验证全新安装时迁移安全跳过
func TestRunAllFreshInstall(t *testing.T) {
  engine := setupTestDB(t)
  defer engine.Close()

  createFullSchema(t, engine)

  if err := RunAll(engine); err != nil {
    t.Fatalf("RunAll 在全新安装时不应失败: %v", err)
  }

  // 版本应已设置
  ver := getSchemaVersion(engine)
  if ver == "" {
    t.Fatal("schema 版本应已设置")
  }
}

// TestRunAllPartialUpgrade 验证部分迁移后继续执行剩余迁移
func TestRunAllPartialUpgrade(t *testing.T) {
  engine := setupTestDB(t)
  defer engine.Close()

  createOldSchema(t, engine)

  // 手动设置版本到某个中间值，模拟部分迁移
  if err := setSchemaVersion(engine, "20250503000000"); err != nil {
    t.Fatal(err)
  }

  if err := RunAll(engine); err != nil {
    t.Fatalf("RunAll 部分升级失败: %v", err)
  }

  // 验证 source 和 mcp_token 应存在（由旧版本迁移）
  // parent_id 作为边界：版本 20250503000000 正好对应 groups.parent_id 迁移
  // 所以该迁移也被跳过，不受影响

  // 验证 20250504000000 及之后的迁移已执行
  checks := []struct {
    table  string
    column string
  }{
    {"user_skills", "source"},
    {"user_skills", "updated_at"},
    {"shared_folders", "description"},
    {"shared_folders", "is_public"},
    {"shared_folders", "created_by"},
    {"shared_folders", "created_at"},
    {"shared_folders", "updated_at"},
  }
  for _, c := range checks {
    exists, err := ColumnExists(engine, c.table, c.column)
    if err != nil {
      t.Fatalf("检查 %s.%s 失败: %v", c.table, c.column, err)
    }
    if !exists {
      t.Fatalf("部分迁移后 %s.%s 列应存在", c.table, c.column)
    }
  }

  ver := getSchemaVersion(engine)
  if ver <= "20250503000000" {
    t.Fatalf("版本应更新超过 20250503000000, 得到 %q", ver)
  }
}

// ============================================================
// 错误路径测试（提升覆盖率至 100%）
// ============================================================

func TestRunAllNoMigrations(t *testing.T) {
  engine := setupTestDB(t)
  defer engine.Close()

  mu.Lock()
  saved := registry
  registry = nil
  mu.Unlock()

  defer func() {
    mu.Lock()
    registry = saved
    mu.Unlock()
  }()

  if err := RunAll(engine); err != nil {
    t.Fatalf("空迁移列表应直接返回 nil: %v", err)
  }
}

func TestColumnExistsError(t *testing.T) {
  engine := setupTestDB(t)
  defer engine.Close()

  _, err := engine.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY, name TEXT)")
  if err != nil {
    t.Fatal(err)
  }

  engine.Close()

  _, err = ColumnExists(engine, "test_table", "id")
  if err == nil {
    t.Fatal("ColumnExists 应在引擎关闭后返回错误")
  }
}

func TestRunAllMigrationError(t *testing.T) {
  engine := setupTestDB(t)
  defer engine.Close()

  createOldSchema(t, engine)

  engine.Close()

  err := RunAll(engine)
  if err == nil {
    t.Fatal("RunAll 应在引擎关闭后返回错误")
  }
  if !strings.Contains(err.Error(), "失败") {
    t.Fatalf("错误应包含迁移失败信息: %v", err)
  }
}

func TestRunAllSetSchemaVersionError(t *testing.T) {
  engine := setupTestDB(t)
  defer engine.Close()

  createOldSchema(t, engine)

  _, err := engine.Exec("DROP TABLE IF EXISTS settings")
  if err != nil {
    t.Fatal(err)
  }

  err = RunAll(engine)
  if err == nil {
    t.Fatal("RunAll 应在 settings 表缺失后返回错误")
  }
  if !strings.Contains(err.Error(), "更新 schema 版本") {
    t.Fatalf("错误应包含版本更新失败信息: %v", err)
  }
}

func TestSharedFoldersMigrationAlterError(t *testing.T) {
  engine := setupTestDB(t)
  defer engine.Close()

  _, err := engine.Exec("DROP TABLE IF EXISTS shared_folders")
  if err != nil {
    t.Fatal(err)
  }

  all := All()
  for _, m := range all {
    if m.Timestamp == "20250506000000" {
      err := m.Up(engine)
      if err == nil {
        t.Fatal("shared_folders 迁移应在表缺失后返回错误")
      }
      return
    }
  }
  t.Fatal("未找到 20250506000000 迁移")
}

func TestAllMigrationsUpError(t *testing.T) {
  engine := setupTestDB(t)
  defer engine.Close()

  createOldSchema(t, engine)

  engine.Close()

  all := All()
  for _, m := range all {
    err := m.Up(engine)
    if err == nil {
      t.Fatalf("迁移 %s (%s) 的 Up() 应在引擎关闭后返回错误", m.Timestamp, m.Desc)
    }
  }
}
