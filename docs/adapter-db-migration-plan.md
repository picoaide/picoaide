# Picoclaw Adapter 数据库化管理 — 实现方案

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development 或 superpowers:executing-plans 按任务逐步实现。

**目标:** 将 Picoclaw Adapter 配置从文件系统迁移到 SQLite 数据库，实现数据库统一管理

**架构:** 新增 `picoclaw_adapter_packages` 表，以序列化 JSON blob 存储完整适配器数据。启动时从 `//go:embed` → DB  seeding，运行时所有读取走 DB，远程刷新和 ZIP 上传直接写入 DB。删除全部磁盘 I/O 相关代码。

**Tech Stack:** Go, xorm, SQLite, `//go:embed`, `encoding/json`

---

## 设计分析

### 数据结构

适配器本质上是一组结构化 JSON 文件的集合（index + 3 config schemas + 3 UI schemas + 2 migration rules），运行时全部加载到 `PicoClawAdapterPackage` 内存结构。最自然的存储方式：**单行 JSON blob**。

```sql
CREATE TABLE IF NOT EXISTS picoclaw_adapter_packages (
    id                        INTEGER PRIMARY KEY AUTOINCREMENT,
    adapter_version           TEXT NOT NULL,
    adapter_schema_version    INTEGER NOT NULL DEFAULT 1,
    latest_supported_config_version INTEGER NOT NULL DEFAULT 3,
    content                   TEXT NOT NULL,       -- 完整适配器序列化
    hash                      TEXT NOT NULL DEFAULT '',  -- 远程 hash，用于变更检测
    refreshed_at              TEXT NOT NULL DEFAULT (datetime('now', 'localtime')),
    created_at                TEXT NOT NULL DEFAULT (datetime('now', 'localtime'))
);
```

`content` 列存储 `SerializableAdapterContent` 的 JSON：

```go
type SerializableAdapterContent struct {
    Index         PicoClawAdapterIndex                `json:"index"`
    ConfigSchemas map[int]PicoClawConfigSchema        `json:"config_schemas"`
    UISchemas     map[int]PicoClawUISchema            `json:"ui_schemas"`
    Migrations    map[string]PicoClawConfigMigration  `json:"migrations"`
}
```

### 数据流

```
启动 ─→ DB 有数据？─是─→ 从 DB 加载 ─→ 内存 PicoClawAdapterPackage
             │
             否
             │
             ▼
     从 //go:embed 加载
             │
             ▼
     序列化为 JSON blob
             │
             ▼
     写入 DB（INSERT）
             │
             ▼
     返回内存结构
```

```
远程刷新 ─→ 下载 hash 文件 ─→ 与 DB.hash 比较
                                    │
                                  相同 → 无变更，跳过
                                    │
                                  不同 → 下载所有文件 → 校验 → 解析
                                              │
                                              ▼
                                        写入 DB（UPSERT）
                                              │
                                              ▼
                                        返回最新包
```

```
ZIP 上传 ─→ 解压 → 校验 hash → 验证 → 解析 → 写入 DB（REPLACE）
```

### 变更范围

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `internal/auth/models.go` | 新增 | `PicoclawAdapterPackage` 模型 |
| `internal/auth/migrations/*.go` | 新增 | 建表 migration |
| `internal/user/picoclaw_adapter.go` | 大改 | 新增 DB 加载/写入，保留 embed 用于 seed，删磁盘 I/O |
| `internal/user/picoclaw_embed.go` | 小改 | 提取 `LoadFromEmbed()` 供 DB seed 调用 |
| `internal/user/picoclaw_migration.go` | 中改 | 去掉 `cacheDir` 依赖，改用 engine |
| `internal/user/picoclaw_fixups.go` | 中改 | 适配器加载走 DB |
| `internal/user/picoclaw_config_fields.go` | 中改 | 适配器加载走 DB |
| `internal/web/admin_config.go` | 小改 | handler 传 engine |
| `internal/web/server.go` | 小改 | 启动时 seed DB，启动后台刷新 |
| `cmd/picoaide/main.go` | 小改 | init 命令适配 |
| `internal/user/picoclaw_adapter_test.go` | 大改 | 重写测试 |
| `internal/user/picoclaw_migration_test.go` | 中改 | 适配 |

---

## 任务分解

### Task 1: 新增数据库表和模型

**Files:**
- Create: `internal/auth/migrations/20250515_000000_add_picoclaw_adapter_table.go`
- Modify: `internal/auth/models.go`

- [ ] **Step 1: 编写 migration 文件**

`internal/auth/migrations/20250515_000000_add_picoclaw_adapter_table.go`:

```go
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
            )`, PicoclawAdapterPackage{}.TableName()))
            return err
        },
    })
}
```

- [ ] **Step 2: 在 models.go 中添加模型**

在 `internal/auth/models.go` 末尾追加：

```go
// PicoclawAdapterPackage 适配器包数据库记录
type PicoclawAdapterPackage struct {
    ID                          int64  `xorm:"pk autoincr 'id'"`
    AdapterVersion              string `xorm:"notnull 'adapter_version'"`
    AdapterSchemaVersion        int    `xorm:"notnull 'adapter_schema_version'"`
    LatestSupportedConfigVersion int   `xorm:"notnull 'latest_supported_config_version'"`
    Content                     string `xorm:"notnull 'content'"`
    Hash                        string `xorm:"notnull 'hash'"`
    RefreshedAt                 string `xorm:"notnull 'refreshed_at'"`
    CreatedAt                   string `xorm:"notnull 'created_at'"`
}

func (PicoclawAdapterPackage) TableName() string {
    return "picoclaw_adapter_packages"
}
```

- [ ] **Step 3: 运行测试确认 migration 正常**

```bash
go test ./internal/auth/migrations/ -v -run TestMigrations
```

预期: PASS（新 migration 被正确执行）

- [ ] **Step 4: 提交**

```bash
git add internal/auth/migrations/20250515_000000_add_picoclaw_adapter_table.go internal/auth/models.go
git commit -m "feat(db): add picoclaw_adapter_packages table"
```

---

### Task 2: 定义序列化结构体和 DB 读写函数

**Files:**
- Modify: `internal/user/picoclaw_adapter.go`

- [ ] **Step 1: 添加序列化结构体和 DB 常量**

在 `picoclaw_adapter.go` 中 `PicoClawAdapterPackage` 结构体之前或之后添加：

```go
// SerializableAdapterContent 适配器包的可序列化内容（不含 Root 字段）
type SerializableAdapterContent struct {
    Index         PicoClawAdapterIndex              `json:"index"`
    ConfigSchemas map[int]PicoClawConfigSchema      `json:"config_schemas"`
    UISchemas     map[int]PicoClawUISchema          `json:"ui_schemas"`
    Migrations    map[string]PicoClawConfigMigration `json:"migrations"`
}

const picoclawAdapterDBKey = "picoclaw_adapter_packages"
```

- [ ] **Step 2: 实现 `Serialize()` 和 `FromDB()` 方法**

```go
func (p *PicoClawAdapterPackage) Serialize() (string, error) {
    content := SerializableAdapterContent{
        Index:         p.Index,
        ConfigSchemas: p.ConfigSchemas,
        UISchemas:     p.UISchemas,
        Migrations:    p.Migrations,
    }
    data, err := json.Marshal(content)
    if err != nil {
        return "", fmt.Errorf("序列化适配器包失败: %w", err)
    }
    return string(data), nil
}

func PicoClawAdapterPackageFromDB(engine *xorm.Engine) (*PicoClawAdapterPackage, error) {
    record := &auth.PicoclawAdapterPackage{}
    has, err := engine.Desc("id").Get(record)
    if err != nil {
        return nil, fmt.Errorf("读取数据库适配器包失败: %w", err)
    }
    if !has {
        return nil, nil
    }
    var content SerializableAdapterContent
    if err := json.Unmarshal([]byte(record.Content), &content); err != nil {
        return nil, fmt.Errorf("解析适配器包内容失败: %w", err)
    }
    pkg := &PicoClawAdapterPackage{
        Index:         content.Index,
        ConfigSchemas: content.ConfigSchemas,
        UISchemas:     content.UISchemas,
        Migrations:    content.Migrations,
    }
    return pkg, nil
}

func SavePicoClawAdapterPackageToDB(engine *xorm.Engine, pkg *PicoClawAdapterPackage, hash string) error {
    content, err := pkg.Serialize()
    if err != nil {
        return err
    }
    now := time.Now().Format("2006-01-02 15:04:05")
    record := &auth.PicoclawAdapterPackage{
        AdapterVersion:              pkg.Index.AdapterVersion,
        AdapterSchemaVersion:        pkg.Index.AdapterSchemaVersion,
        LatestSupportedConfigVersion: pkg.Index.LatestSupportedConfigVersion,
        Content:                     content,
        Hash:                        hash,
        RefreshedAt:                 now,
        CreatedAt:                   now,
    }
    // 先删后插（单行表，始终保持唯一记录）
    if _, err := engine.Where("1=1").Delete(&auth.PicoclawAdapterPackage{}); err != nil {
        return fmt.Errorf("清理旧适配器包失败: %w", err)
    }
    if _, err := engine.Insert(record); err != nil {
        return fmt.Errorf("保存适配器包到数据库失败: %w", err)
    }
    return nil
}
```

注意：需要添加 `auth` 和 `time` 的 import。

- [ ] **Step 3: 提交**

```bash
git add internal/user/picoclaw_adapter.go
git commit -m "feat(adapter): add DB serialize/deserialize functions"
```

---

### Task 3: 从 embed 加载并 seed 到 DB

**Files:**
- Modify: `internal/user/picoclaw_embed.go`
- Modify: `internal/user/picoclaw_adapter.go`

- [ ] **Step 1: 在 picoclaw_embed.go 中提取 `LoadFromEmbed()`**

将嵌入内容直接解析为 `PicoClawAdapterPackage`（跳过写磁盘），返回内存结构：

```go
func NewPicoClawAdapterPackageFromEmbed() (*PicoClawAdapterPackage, error) {
    if !picoclawAdapterEmbedExists() {
        return loadFromBundledDir()
    }
    return loadFromEmbedFS()
}

func loadFromEmbedFS() (*PicoClawAdapterPackage, error) {
    // 读取 index.json
    indexData, err := picoclawRulesEmbed.ReadFile(filepath.Join(picoclawRulesEmbedDir, picoclawAdapterIndexFile))
    if err != nil {
        return nil, fmt.Errorf("读取嵌入 index.json 失败: %w", err)
    }
    var index PicoClawAdapterIndex
    if err := json.Unmarshal(indexData, &index); err != nil {
        return nil, fmt.Errorf("解析嵌入 index.json 失败: %w", err)
    }
    pkg := &PicoClawAdapterPackage{
        Index:         index,
        ConfigSchemas: make(map[int]PicoClawConfigSchema),
        UISchemas:     make(map[int]PicoClawUISchema),
        Migrations:    make(map[string]PicoClawConfigMigration),
    }
    // 读取 config schemas
    for versionStr, schemaPath := range index.ConfigSchemas {
        version, _ := strconv.Atoi(versionStr)
        data, err := picoclawRulesEmbed.ReadFile(filepath.Join(picoclawRulesEmbedDir, schemaPath))
        if err != nil {
            return nil, fmt.Errorf("读取嵌入 schema %s 失败: %w", schemaPath, err)
        }
        var schema PicoClawConfigSchema
        if err := json.Unmarshal(data, &schema); err != nil {
            return nil, fmt.Errorf("解析嵌入 schema %s 失败: %w", schemaPath, err)
        }
        pkg.ConfigSchemas[version] = schema
    }
    // 读取 UI schemas
    for versionStr, uiPath := range index.UISchemas {
        version, _ := strconv.Atoi(versionStr)
        data, err := picoclawRulesEmbed.ReadFile(filepath.Join(picoclawRulesEmbedDir, uiPath))
        if err != nil {
            return nil, fmt.Errorf("读取嵌入 UI schema %s 失败: %w", uiPath, err)
        }
        ui, err := unmarshalUISchema(data)
        if err != nil {
            return nil, fmt.Errorf("解析嵌入 UI schema %s 失败: %w", uiPath, err)
        }
        pkg.UISchemas[version] = *ui
    }
    // 读取 migrations
    for _, ref := range index.Migrations {
        data, err := picoclawRulesEmbed.ReadFile(filepath.Join(picoclawRulesEmbedDir, ref.Path))
        if err != nil {
            return nil, fmt.Errorf("读取嵌入 migration %s 失败: %w", ref.Path, err)
        }
        var migration PicoClawConfigMigration
        if err := json.Unmarshal(data, &migration); err != nil {
            return nil, fmt.Errorf("解析嵌入 migration %s 失败: %w", ref.Path, err)
        }
        key := fmt.Sprintf("%d:%d", ref.FromConfig, ref.ToConfig)
        pkg.Migrations[key] = migration
    }
    if err := pkg.Validate(); err != nil {
        return nil, err
    }
    return pkg, nil
}

func loadFromBundledDir() (*PicoClawAdapterPackage, error) {
    bundledRoot, err := findBundledPicoClawAdapterRoot()
    if err != nil {
        return nil, err
    }
    return LoadPicoClawAdapterPackage(bundledRoot)
}
```

注意: `unmarshalUISchema` 逻辑与 `picoclaw_adapter.go` 中 `loadReferencedFiles` 内的相同，可以直接复用。需要检查 `LoadPicoClawAdapterPackage` 在同一个包内，直接调用即可。

- [ ] **Step 2: 在 picoclaw_adapter.go 中添加 `SeedAdapterIfNeeded` 函数**

```go
func SeedPicoClawAdapterToDB(engine *xorm.Engine) error {
    // 检查 DB 是否已有数据
    existing, _ := PicoClawAdapterPackageFromDB(engine)
    if existing != nil {
        return nil // 已存在，不覆盖
    }
    // 从嵌入加载
    pkg, err := NewPicoClawAdapterPackageFromEmbed()
    if err != nil {
        return fmt.Errorf("从嵌入加载适配器失败: %w", err)
    }
    // 写入 DB
    return SavePicoClawAdapterPackageToDB(engine, pkg, "")
}
```

- [ ] **Step 3: 提交**

```bash
git add internal/user/picoclaw_embed.go internal/user/picoclaw_adapter.go
git commit -m "feat(adapter): load from embed and seed to DB"
```

---

### Task 4: 修改 `NewPicoClawAdapterPackage` 优先读 DB

**Files:**
- Modify: `internal/user/picoclaw_adapter.go`

- [ ] **Step 1: 添加 `NewPicoClawAdapterPackageFromDB` 构造器，保留旧函数兼容**

```go
// NewPicoClawAdapterPackage 从数据库加载适配器包（向后兼容包装）
// cacheDir 参数不再使用，保留签名以兼容现有调用
func NewPicoClawAdapterPackage(cacheDir string) (*PicoClawAdapterPackage, error) {
    engine, err := auth.GetEngine()
    if err != nil {
        return nil, fmt.Errorf("获取数据库连接失败: %w", err)
    }
    pkg, err := PicoClawAdapterPackageFromDB(engine)
    if err != nil {
        return nil, err
    }
    if pkg != nil {
        return pkg, nil
    }
    // DB 无数据，从 embed seed
    pkg, err = NewPicoClawAdapterPackageFromEmbed()
    if err != nil {
        return nil, err
    }
    if err := SavePicoClawAdapterPackageToDB(engine, pkg, ""); err != nil {
        return nil, err
    }
    return pkg, nil
}
```

注意：需要添加 `auth` import。

- [ ] **Step 2: 提交**

```bash
git add internal/user/picoclaw_adapter.go
git commit -m "refactor(adapter): NewPicoClawAdapterPackage reads from DB"
```

---

### Task 5: 重写远程刷新 → 写入 DB

**Files:**
- Modify: `internal/user/picoclaw_adapter.go`

- [ ] **Step 1: 重写 `RefreshPicoClawAdapterFromRemote` 和 `RefreshPicoClawAdapterFromRemoteIfChanged`**

改变：下载 → 校验 → 解析后，不再写磁盘，改为调用 `SavePicoClawAdapterPackageToDB(engine, pkg, hashData)`。同时删除所有 `os.*` 文件操作。

```go
func RefreshPicoClawAdapterFromRemote(cacheDir, remoteBaseURL string, client *http.Client) (*PicoClawAdapterPackage, error) {
    if strings.TrimSpace(remoteBaseURL) == "" {
        return nil, errors.New("Picoclaw adapter remote base URL is empty")
    }
    if client == nil {
        client = &http.Client{Timeout: 20 * time.Second}
    }
    base := strings.TrimRight(remoteBaseURL, "/")
    entries, hashData, err := fetchPicoClawAdapterHash(client, base)
    if err != nil {
        return nil, err
    }
    // 下载所有文件到内存
    files := make(map[string][]byte)
    for _, entry := range entries {
        data, err := fetchPicoClawAdapterFile(client, base, entry.Path)
        if err != nil {
            return nil, err
        }
        if got := sha256Hex(data); got != entry.SHA256 {
            return nil, fmt.Errorf("Picoclaw adapter 文件 %s hash 不匹配: got %s want %s", entry.Path, got, entry.SHA256)
        }
        files[entry.Path] = data
    }
    // 在内存中解析为适配器包
    pkg, err := parsePicoClawAdapterFiles(files, hashData)
    if err != nil {
        return nil, err
    }
    // 写入数据库
    engine, err := auth.GetEngine()
    if err != nil {
        return nil, fmt.Errorf("获取数据库连接失败: %w", err)
    }
    if err := SavePicoClawAdapterPackageToDB(engine, pkg, string(hashData)); err != nil {
        return nil, err
    }
    return pkg, nil
}
```

需要新写 `parsePicoClawAdapterFiles` 函数（将内存文件 map 解析为 `PicoClawAdapterPackage`）：

```go
// parsePicoClawAdapterFiles 从下载的文件 map 中解析适配器包
func parsePicoClawAdapterFiles(files map[string][]byte, hashData []byte) (*PicoClawAdapterPackage, error) {
    indexData, ok := files[picoclawAdapterIndexFile]
    if !ok {
        return nil, fmt.Errorf("缺少 index.json")
    }
    var index PicoClawAdapterIndex
    if err := json.Unmarshal(indexData, &index); err != nil {
        return nil, fmt.Errorf("解析 index.json 失败: %w", err)
    }
    pkg := &PicoClawAdapterPackage{
        Index:         index,
        ConfigSchemas: make(map[int]PicoClawConfigSchema),
        UISchemas:     make(map[int]PicoClawUISchema),
        Migrations:    make(map[string]PicoClawConfigMigration),
    }
    // 解析 config schemas
    for versionStr, schemaPath := range index.ConfigSchemas {
        version, _ := strconv.Atoi(versionStr)
        data, ok := files[schemaPath]
        if !ok {
            return nil, fmt.Errorf("缺少 config schema 文件: %s", schemaPath)
        }
        var schema PicoClawConfigSchema
        if err := json.Unmarshal(data, &schema); err != nil {
            return nil, fmt.Errorf("解析 %s 失败: %w", schemaPath, err)
        }
        pkg.ConfigSchemas[version] = schema
    }
    // 解析 UI schemas
    for versionStr, uiPath := range index.UISchemas {
        version, _ := strconv.Atoi(versionStr)
        data, ok := files[uiPath]
        if !ok {
            return nil, fmt.Errorf("缺少 UI schema 文件: %s", uiPath)
        }
        var ui PicoClawUISchema
        if err := json.Unmarshal(data, &ui); err != nil {
            return nil, fmt.Errorf("解析 %s 失败: %w", uiPath, err)
        }
        pkg.UISchemas[version] = ui
    }
    // 解析 migrations
    for _, ref := range index.Migrations {
        data, ok := files[ref.Path]
        if !ok {
            return nil, fmt.Errorf("缺少 migration 文件: %s", ref.Path)
        }
        var migration PicoClawConfigMigration
        if err := json.Unmarshal(data, &migration); err != nil {
            return nil, fmt.Errorf("解析 %s 失败: %w", ref.Path, err)
        }
        pkg.Migrations[fmt.Sprintf("%d:%d", ref.FromConfig, ref.ToConfig)] = migration
    }
    if err := pkg.Validate(); err != nil {
        return nil, err
    }
    return pkg, nil
}
```

重写 `RefreshPicoClawAdapterFromRemoteIfChanged`（去掉文件 hash 比较，改为比较 DB hash）：

```go
func RefreshPicoClawAdapterFromRemoteIfChanged(cacheDir string, remoteBaseURLs []string, client *http.Client) (*PicoClawAdapterPackage, bool, error) {
    if len(remoteBaseURLs) == 0 {
        return nil, false, errors.New("Picoclaw adapter remote base URLs are empty")
    }
    if client == nil {
        client = &http.Client{Timeout: 20 * time.Second}
    }

    // 从 DB 读取本地 hash
    engine, err := auth.GetEngine()
    if err != nil {
        return nil, false, fmt.Errorf("获取数据库连接失败: %w", err)
    }
    localHash := ""
    if record := &auth.PicoclawAdapterPackage{}; engine.Desc("id").Get(record) == nil && record.ID > 0 {
        localHash = record.Hash
    }

    var lastErr error
    for _, baseURL := range remoteBaseURLs {
        baseURL = strings.TrimRight(baseURL, "/")
        if baseURL == "" {
            continue
        }
        entries, remoteHash, err := fetchPicoClawAdapterHash(client, baseURL)
        if err != nil {
            lastErr = err
            continue
        }
        if localHash != "" && string(remoteHash) == localHash {
            return nil, false, nil
        }
        pkg, err := doRefreshPicoClawAdapterToDB(engine, baseURL, client, entries, remoteHash)
        if err != nil {
            lastErr = err
            continue
        }
        return pkg, true, nil
    }
    return nil, false, fmt.Errorf("所有远程 URL 均失败: %w", lastErr)
}

// doRefreshPicoClawAdapterToDB 从远程下载并直接写入数据库
func doRefreshPicoClawAdapterToDB(engine *xorm.Engine, base string, client *http.Client, entries []PicoClawAdapterHashEntry, hashData []byte) (*PicoClawAdapterPackage, error) {
    files := make(map[string][]byte, len(entries))
    for _, entry := range entries {
        data, err := fetchPicoClawAdapterFile(client, base, entry.Path)
        if err != nil {
            return nil, err
        }
        if got := sha256Hex(data); got != entry.SHA256 {
            return nil, fmt.Errorf("文件 %s hash 不匹配: got %s want %s", entry.Path, got, entry.SHA256)
        }
        files[entry.Path] = data
    }
    pkg, err := parsePicoClawAdapterFiles(files, hashData)
    if err != nil {
        return nil, err
    }
    if err := SavePicoClawAdapterPackageToDB(engine, pkg, string(hashData)); err != nil {
        return nil, err
    }
    return pkg, nil
}
```

- [ ] **Step 2: 更新 `AutoRefreshPicoClawAdapter` 去掉阶段一磁盘释放**

```go
func AutoRefreshPicoClawAdapter(cacheDir string, remoteBaseURLs []string) {
    // 阶段一：确保 DB 中有适配器数据
    engine, err := auth.GetEngine()
    if err != nil {
        slog.Warn("获取数据库连接失败，跳过适配器刷新", "error", err)
        return
    }
    if err := SeedPicoClawAdapterToDB(engine); err != nil {
        slog.Warn("初始化 Picoclaw 适配器失败", "error", err)
    }

    if len(remoteBaseURLs) == 0 {
        return
    }

    backoff := 30 * time.Second
    const maxBackoff = 1 * time.Hour
    const checkInterval = 24 * time.Hour

    for {
        _, changed, err := RefreshPicoClawAdapterFromRemoteIfChanged(cacheDir, remoteBaseURLs, nil)
        if err == nil {
            if changed {
                slog.Info("Picoclaw 适配器已自动更新")
            }
            time.Sleep(checkInterval)
            backoff = 30 * time.Second
            continue
        }
        slog.Warn("Picoclaw 适配器远程刷新失败，即将重试", "error", err, "retry_after", backoff)
        time.Sleep(backoff)
        backoff *= 2
        if backoff > maxBackoff {
            backoff = maxBackoff
        }
    }
}
```

- [ ] **Step 3: 更新 `SavePicoClawAdapterZip` 写入 DB 而非磁盘**

```go
func SavePicoClawAdapterZip(cacheDir string, data []byte) (*PicoClawAdapterPackage, error) {
    if len(data) == 0 {
        return nil, errors.New("adapter zip 为空")
    }
    reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
    if err != nil {
        return nil, fmt.Errorf("读取 adapter zip 失败: %w", err)
    }

    // 解压到内存
    files := make(map[string][]byte)
    var hashData []byte
    for _, file := range reader.File {
        clean := path.Clean(file.Name)
        if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
            continue
        }
        rc, err := file.Open()
        if err != nil {
            return nil, err
        }
        content, err := io.ReadAll(rc)
        rc.Close()
        if err != nil {
            return nil, err
        }
        if clean == picoclawAdapterHashFile {
            hashData = content
        }
        files[clean] = content
    }

    pkg, err := parsePicoClawAdapterFiles(files, hashData)
    if err != nil {
        return nil, err
    }

    engine, err := auth.GetEngine()
    if err != nil {
        return nil, fmt.Errorf("获取数据库连接失败: %w", err)
    }
    if err := SavePicoClawAdapterPackageToDB(engine, pkg, string(hashData)); err != nil {
        return nil, err
    }
    return pkg, nil
}
```

- [ ] **Step 4: 清理不再使用的磁盘相关函数**

删除以下函数（保留签名以兼容调用方，但内部走 DB）：
- `ForceReleasePicoClawAdapterCache` — 不再需要释放磁盘缓存，可改为日志记录
- `ReleasePicoClawAdapterCacheIfValid` — 不再需要，保留空壳返回 nil
- `activatePicoClawAdapter` — 删除
- `copyPicoClawAdapterDir` — 删除
- `releasePicoClawAdapterFromEmbed` — 不再需要（由 `NewPicoClawAdapterPackageFromEmbed` 替代）
- `LoadPicoClawAdapterPackage` — 如果再无磁盘调用方则删除；暂时保留供开发环境使用

```go
// ForceReleasePicoClawAdapterCache 已废弃（适配器存储在数据库中）
func ForceReleasePicoClawAdapterCache(cacheDir string) error {
    return nil
}

// ReleasePicoClawAdapterCacheIfValid 已废弃（适配器存储在数据库中）
func ReleasePicoClawAdapterCacheIfValid(cacheDir string) error {
    return nil
}
```

- [ ] **Step 5: 提交**

```bash
git add internal/user/picoclaw_adapter.go
git commit -m "refactor(adapter): remote refresh and zip upload write to DB"
```

---

### Task 6: 简化嵌入导出代码

**Files:**
- Modify: `internal/user/picoclaw_embed.go`

- [ ] **Step 1: 精简 picoclaw_embed.go**

删除 `releasePicoClawAdapterFromEmbed` 函数（已由 `NewPicoClawAdapterPackageFromEmbed` 替代）。保留 `picoclawAdapterEmbedExists()` 和 `picoclawRulesEmbed`（供 seed 使用）。

文件结构变为：

```go
package user

import (
    "encoding/json"
    "fmt"
    "io/fs"
    "path/filepath"
    "strconv"
    "strings"
    "xorm.io/xorm" // 如果不再需要，则删除
)

//go:embed all:picoclaw_rules
var picoclawRulesEmbed embed.FS

const picoclawRulesEmbedDir = "picoclaw_rules"

func picoclawAdapterEmbedExists() bool {
    entries, err := fs.ReadDir(picoclawRulesEmbed, picoclawRulesEmbedDir)
    return err == nil && len(entries) > 0
}

func NewPicoClawAdapterPackageFromEmbed() (*PicoClawAdapterPackage, error) {
    // ... (前面 Task 3 的实现)
}
```

- [ ] **Step 2: 提交**

```bash
git add internal/user/picoclaw_embed.go
git commit -m "refactor(embed): simplify to in-memory loading"
```

---

### Task 7: 更新 MigrationService 和 MigrationRulesInfo 去除 cacheDir

**Files:**
- Modify: `internal/user/picoclaw_migration.go`

- [ ] **Step 1: 更新 `PicoClawMigrationService` 结构体**

```go
type PicoClawMigrationService struct {
    rules PicoClawMigrationRuleSet
}
```

去除 `cacheDir` 字段。

- [ ] **Step 2: 更新 `NewPicoClawMigrationService`**

```go
func NewPicoClawMigrationService(cacheDir string) (*PicoClawMigrationService, error) {
    svc := &PicoClawMigrationService{}
    rules, err := svc.loadRules()
    if err != nil {
        return nil, err
    }
    svc.rules = rules
    return svc, nil
}
```

`cacheDir` 参数保留签名兼容但不再使用。

- [ ] **Step 3: 更新 `LoadPicoClawMigrationRulesInfo`**

```go
func LoadPicoClawMigrationRulesInfo(cacheDir string) (PicoClawMigrationRulesInfo, error) {
    pkg, err := NewPicoClawAdapterPackage(cacheDir)
    if err != nil {
        return PicoClawMigrationRulesInfo{}, err
    }
    rules := pkg.ToMigrationRuleSet()
    engine, err := auth.GetEngine()
    var updatedAt string
    if err == nil {
        if record := &auth.PicoclawAdapterPackage{}; engine.Desc("id").Get(record) == nil && record.ID > 0 {
            updatedAt = record.RefreshedAt
        }
    }
    info := PicoClawMigrationRulesInfo{
        LatestSupportedConfigVersion:   rules.LatestSupportedConfigVersion,
        PicoAideSupportedConfigVersion: rules.LatestSupportedConfigVersion,
        AdapterSchemaVersion:           pkg.Index.AdapterSchemaVersion,
        AdapterVersion:                 pkg.Index.AdapterVersion,
        Versions:                       rules.Versions,
        CachePath:                      "db://picoclaw_adapter_packages",
        UpdatedAt:                      updatedAt,
    }
    return info, nil
}
```

- [ ] **Step 4: 简化包装函数**

`ForceReleasePicoClawMigrationRulesCache`、`ReleasePicoClawMigrationRulesCacheIfValid` 等全部不需要了，直接返回 nil：

```go
func ForceReleasePicoClawMigrationRulesCache(cacheDir string) error { return nil }

func ReleasePicoClawMigrationRulesCacheIfValid(cacheDir string) error { return nil }

func ReleasePicoClawMigrationRulesCache(cacheDir string) error { return nil }
```

- [ ] **Step 5: 提交**

```bash
git add internal/user/picoclaw_migration.go
git commit -m "refactor(migration): remove cacheDir dependency"
```

---

### Task 8: 更新加载适配器的调用方

**Files:**
- Modify: `internal/user/picoclaw_fixups.go`
- Modify: `internal/user/picoclaw_config_fields.go`

这些文件中的函数通过 `config.RuleCacheDir()` 调用 `NewPicoClawAdapterPackage`，签名不变所以只需确保导入正确。

- [ ] **Step 1: 确认 picoclaw_fixups.go 中的函数能正常工作**

检查 `ApplyConfigToJSONWithMigration`（第 32 行）调用 `NewPicoClawMigrationService(config.RuleCacheDir())`，签名不变，无需修改。

检查 `configVersionForPicoClawTag` 和 `picoClawConfigSchemaForVersion` 调用 `NewPicoClawAdapterPackage(config.RuleCacheDir())`，签名不变，无需修改。

- [ ] **Step 2: 确认 picoclaw_config_fields.go 中的函数能正常工作**

检查所有 `NewPicoClawAdapterPackage(config.RuleCacheDir())` 调用（`ListPicoClawAdminChannels`、`ListPicoClawUserChannels`、`GetPicoClawConfigFields`、`SavePicoClawConfigSectionFields`），签名不变，无需修改。

- [ ] **Step 3: 如果有不用的 import，清理**

删除 `picoclaw_fixups.go` 和 `picoclaw_config_fields.go` 中不再需要的 `os` 和 `filepath` import（如果编译器报错）。

- [ ] **Step 4: 提交**

```bash
git add internal/user/picoclaw_fixups.go internal/user/picoclaw_config_fields.go
git commit -m "chore: remove unused imports in fixups and config_fields"
```

---

### Task 9: 更新 server.go 启动逻辑

**Files:**
- Modify: `internal/web/server.go`

- [ ] **Step 1: 替换启动时磁盘释放为 DB seed**

```go
// 之前:
if err := user.ReleasePicoClawMigrationRulesCacheIfValid(config.RuleCacheDir()); err != nil {
    slog.Warn("初始化迁移规则缓存失败", "error", err)
}

// 之后:
if err := user.SeedPicoClawAdapterToDB(s.engine); err != nil {
    slog.Warn("初始化 Picoclaw 适配器失败", "error", err)
}
```

注意：Server 结构体目前没有暴露 engine，需要通过 `auth.GetEngine()` 获取。在 `NewPicoClawAdapterPackage` 内部已经处理了 DB 连接获取，所以 seed 调用可以改为：

```go
engine, err := auth.GetEngine()
if err == nil {
    if err := user.SeedPicoClawAdapterToDB(engine); err != nil {
        slog.Warn("初始化 Picoclaw 适配器失败", "error", err)
    }
}
```

- [ ] **Step 2: 提交**

```bash
git add internal/web/server.go
git commit -m "refactor(server): seed adapter to DB on startup"
```

---

### Task 10: 更新 HTTP handlers

**Files:**
- Modify: `internal/web/admin_config.go`

- [ ] **Step 1: 确认 handler 签名不变**

所有 handler 仍然调用 `config.RuleCacheDir()` 向下传递，内部已自动走 DB。无需修改 handler 代码。

但 `handleAdminMigrationRulesGet` 的 `CachePath` 现在返回 `"db://picoclaw_adapter_packages"`，前端可能需要适配。确认前端是否使用了这个字段——如果只是展示则无影响。

- [ ] **Step 2: 提交**

```bash
git add internal/web/admin_config.go
git commit -m "chore: adapter handlers now use DB implicitly"
```

---

### Task 11: 更新测试

**Files:**
- Modify: `internal/user/picoclaw_adapter_test.go`
- Modify: `internal/user/picoclaw_migration_test.go`

- [ ] **Step 1: 重写 TestLoadPicoClawAdapterPackageBundled**

```go
func TestNewPicoClawAdapterPackageFromEmbed(t *testing.T) {
    pkg, err := NewPicoClawAdapterPackageFromEmbed()
    if err != nil {
        t.Fatalf("NewPicoClawAdapterPackageFromEmbed() 失败: %v", err)
    }
    if pkg.Index.AdapterVersion == "" {
        t.Error("AdapterVersion 不应为空")
    }
    if len(pkg.ConfigSchemas) == 0 {
        t.Error("ConfigSchemas 不应为空")
    }
    if len(pkg.UISchemas) == 0 {
        t.Error("UISchemas 不应为空")
    }
    if len(pkg.Migrations) == 0 {
        t.Error("Migrations 不应为空")
    }
}
```

- [ ] **Step 2: 添加 DB 相关测试**

```go
func TestPicoClawAdapterPackageSerializeAndDB(t *testing.T) {
    // 从 embed 加载
    pkg, err := NewPicoClawAdapterPackageFromEmbed()
    if err != nil {
        t.Fatalf("NewPicoClawAdapterPackageFromEmbed() 失败: %v", err)
    }
    // 序列化
    content, err := pkg.Serialize()
    if err != nil {
        t.Fatalf("Serialize() 失败: %v", err)
    }
    // 解析回去
    var parsed SerializableAdapterContent
    if err := json.Unmarshal([]byte(content), &parsed); err != nil {
        t.Fatalf("Unmarshal 失败: %v", err)
    }
    if parsed.Index.AdapterVersion != pkg.Index.AdapterVersion {
        t.Errorf("版本不匹配: got %s, want %s", parsed.Index.AdapterVersion, pkg.Index.AdapterVersion)
    }
    if len(parsed.ConfigSchemas) != len(pkg.ConfigSchemas) {
        t.Errorf("ConfigSchemas 数量不匹配: got %d, want %d", len(parsed.ConfigSchemas), len(pkg.ConfigSchemas))
    }
}
```

- [ ] **Step 3: 清理旧测试**

删除 `TestRefreshPicoClawAdapterFromRemoteVerifiesHashes`（需要网络和远程服务器，不适合单元测试）。如果需要保留，添加 `t.Skip("需要网络")`。

删除/更新 `TestSavePicoClawAdapterZipInstallsPackage` 和 `TestSavePicoClawAdapterZipRejectsTopLevelDirectory` 等——改为测试 `parsePicoClawAdapterFiles` 函数。

- [ ] **Step 4: 更新迁移测试**

确保 `picoclaw_migration_test.go` 中的测试仍然可以创建 `PicoClawMigrationService` 并运行迁移。

- [ ] **Step 5: 测试验证**

```bash
go test ./internal/user/ -v -run "TestNewPicoClawAdapterPackage|TestPicoClawAdapterPackageSerialize|TestMigrate" -count=1
```

预期: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/user/picoclaw_adapter_test.go internal/user/picoclaw_migration_test.go
git commit -m "test(adapter): update tests for DB-backed adapter"
```

---

### Task 12: 运行完整检查和修复

- [ ] **Step 1: 运行 format**

```bash
./format.sh
```

预期: 无错误

- [ ] **Step 2: 运行 lint**

```bash
make lint
```

预期: 无错误

- [ ] **Step 3: 运行测试**

```bash
make test-go
```

预期: ALL PASS

- [ ] **Step 4: 构建**

```bash
go build -o picoaide ./cmd/picoaide/
```

预期: 编译成功，无错误

- [ ] **Step 5: 提交**

```bash
git add .
git commit -m "chore: run formatter and lint, all tests pass"
```

---

## 废弃/待删除函数清单

| 函数 | 文件名 | 状态 |
|------|--------|------|
| `releasePicoClawAdapterFromEmbed` | `picoclaw_embed.go` | 删除 |
| `ForceReleasePicoClawAdapterCache` | `picoclaw_adapter.go` | 空壳返回 nil |
| `ReleasePicoClawAdapterCacheIfValid` | `picoclaw_adapter.go` | 空壳返回 nil |
| `LoadPicoClawAdapterPackage` | `picoclaw_adapter.go` | 保留（供开发环境 loadFromBundledDir 使用），但不再作为主路径 |
| `activatePicoClawAdapter` | `picoclaw_adapter.go` | 删除 |
| `copyPicoClawAdapterDir` | `picoclaw_adapter.go` | 删除 |
| `findBundledPicoClawAdapterRoot` | `picoclaw_adapter.go` | 保留（供 loadFromBundledDir 回退使用） |
| `ReleasePicoClawMigrationRulesCache` | `picoclaw_migration.go` | 空壳 |
| `ForceReleasePicoClawMigrationRulesCache` | `picoclaw_migration.go` | 空壳 |
| `ReleasePicoClawMigrationRulesCacheIfValid` | `picoclaw_migration.go` | 空壳 |
| `(s *PicoClawMigrationService) ReleaseBundledRulesCache` | `picoclaw_migration.go` | 删除 |
