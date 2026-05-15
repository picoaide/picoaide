package migrations

import (
  "testing"

  _ "modernc.org/sqlite"
)

func TestMigrationCreatesPicoclawAdapterTable(t *testing.T) {
  engine := setupTestDB(t)
  defer engine.Close()

  createOldSchema(t, engine)

  if err := RunAll(engine); err != nil {
    t.Fatalf("RunAll 失败: %v", err)
  }

  // 验证表存在
  rows, err := engine.Query("SELECT name FROM sqlite_master WHERE type='table' AND name='picoclaw_adapter_packages'")
  if err != nil {
    t.Fatalf("查询表是否存在失败: %v", err)
  }
  if len(rows) == 0 {
    t.Fatal("picoclaw_adapter_packages 表应存在")
  }

  // 验证各列存在
  columns := []string{
    "id",
    "adapter_version",
    "adapter_schema_version",
    "latest_supported_config_version",
    "content",
    "hash",
    "refreshed_at",
    "created_at",
  }
  for _, col := range columns {
    exists, err := ColumnExists(engine, "picoclaw_adapter_packages", col)
    if err != nil {
      t.Fatalf("检查列 %s 失败: %v", col, err)
    }
    if !exists {
      t.Fatalf("picoclaw_adapter_packages.%s 列应存在", col)
    }
  }
}

func TestMigrationCreatesPicoclawAdapterTableIdempotent(t *testing.T) {
  engine := setupTestDB(t)
  defer engine.Close()

  createOldSchema(t, engine)

  if err := RunAll(engine); err != nil {
    t.Fatalf("第一次 RunAll 失败: %v", err)
  }

  if err := RunAll(engine); err != nil {
    t.Fatalf("第二次 RunAll 不应失败: %v", err)
  }

  rows, err := engine.Query("SELECT name FROM sqlite_master WHERE type='table' AND name='picoclaw_adapter_packages'")
  if err != nil {
    t.Fatalf("查询表是否存在失败: %v", err)
  }
  if len(rows) == 0 {
    t.Fatal("picoclaw_adapter_packages 表应存在")
  }
}
