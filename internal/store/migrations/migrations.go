package migrations

import (
  "fmt"
  "sort"
  "sync"

  "xorm.io/xorm"
)

// ============================================================
// 迁移系统核心：注册表、执行引擎、辅助函数
// ============================================================

// Migration 定义一次数据库 Schema 迁移
type Migration struct {
  Timestamp string                   // 14 位：YYYYMMDDHHMMSS
  Desc      string                   // 中文描述
  Up        func(*xorm.Engine) error // 迁移函数，必须幂等
}

var (
  registry []Migration
  mu       sync.Mutex
)

// Register 注册一个迁移（在 init() 中调用）
func Register(m Migration) {
  mu.Lock()
  defer mu.Unlock()
  registry = append(registry, m)
}

// All 返回按时间戳排序的所有已注册迁移
func All() []Migration {
  mu.Lock()
  defer mu.Unlock()
  result := make([]Migration, len(registry))
  copy(result, registry)
  sort.Slice(result, func(i, j int) bool {
    return result[i].Timestamp < result[j].Timestamp
  })
  return result
}

// RunAll 执行所有未应用的迁移
func RunAll(engine *xorm.Engine) error {
  cur := getSchemaVersion(engine)
  all := All()

  if len(all) == 0 {
    return nil
  }

  for _, m := range all {
    if m.Timestamp <= cur {
      continue
    }
    if err := m.Up(engine); err != nil {
      return fmt.Errorf("迁移 %s (%s) 失败: %w", m.Timestamp, m.Desc, err)
    }
    if err := setSchemaVersion(engine, m.Timestamp); err != nil {
      return fmt.Errorf("更新 schema 版本 %s 失败: %w", m.Timestamp, err)
    }
  }
  return nil
}

// getSchemaVersion 从 settings 表读取当前 schema 版本
// 如果没有记录则返回空字符串
func getSchemaVersion(engine *xorm.Engine) string {
  rows, err := engine.Query("SELECT value FROM settings WHERE key='internal.schema_version' LIMIT 1")
  if err != nil || len(rows) == 0 {
    return ""
  }
  return string(rows[0]["value"])
}

// setSchemaVersion 将 schema 版本写入 settings 表
func setSchemaVersion(engine *xorm.Engine, version string) error {
  _, err := engine.Exec(
    "INSERT INTO settings (key, value, updated_at) VALUES ('internal.schema_version', ?, datetime('now','localtime')) "+
      "ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at",
    version,
  )
  return err
}

// ColumnExists 检查数据库表中是否存在指定列（幂等辅助函数）
func ColumnExists(engine *xorm.Engine, table, column string) (bool, error) {
  rows, err := engine.Query(fmt.Sprintf(
    "SELECT 1 FROM pragma_table_info('%s') WHERE name='%s' LIMIT 1", table, column))
  if err != nil {
    return false, err
  }
  return len(rows) > 0, nil
}
