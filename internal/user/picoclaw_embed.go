package user

import (
  "embed"
  "encoding/json"
  "fmt"
  "io/fs"
  "path"
  "strconv"

  "xorm.io/xorm"
)

//go:embed all:picoclaw_rules
var picoclawRulesEmbed embed.FS

const picoclawRulesEmbedDir = "picoclaw_rules"

func picoclawAdapterEmbedExists() bool {
  entries, err := fs.ReadDir(picoclawRulesEmbed, picoclawRulesEmbedDir)
  return err == nil && len(entries) > 0
}

// NewPicoClawAdapterPackageFromEmbed 从嵌入的文件系统加载适配器包到内存
func NewPicoClawAdapterPackageFromEmbed() (*PicoClawAdapterPackage, error) {
  if !picoclawAdapterEmbedExists() {
    return loadFromBundledDir()
  }
  return loadFromEmbedFS()
}

func loadFromEmbedFS() (*PicoClawAdapterPackage, error) {
  indexData, err := picoclawRulesEmbed.ReadFile(path.Join(picoclawRulesEmbedDir, "index.json"))
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
  for versionStr, schemaPath := range index.ConfigSchemas {
    version, _ := strconv.Atoi(versionStr)
    data, err := picoclawRulesEmbed.ReadFile(path.Join(picoclawRulesEmbedDir, schemaPath))
    if err != nil {
      return nil, fmt.Errorf("读取嵌入 schema %s 失败: %w", schemaPath, err)
    }
    var schema PicoClawConfigSchema
    if err := json.Unmarshal(data, &schema); err != nil {
      return nil, fmt.Errorf("解析嵌入 schema %s 失败: %w", schemaPath, err)
    }
    pkg.ConfigSchemas[version] = schema
  }
  for versionStr, uiPath := range index.UISchemas {
    version, _ := strconv.Atoi(versionStr)
    data, err := picoclawRulesEmbed.ReadFile(path.Join(picoclawRulesEmbedDir, uiPath))
    if err != nil {
      return nil, fmt.Errorf("读取嵌入 UI schema %s 失败: %w", uiPath, err)
    }
    var ui PicoClawUISchema
    if err := json.Unmarshal(data, &ui); err != nil {
      return nil, fmt.Errorf("解析嵌入 UI schema %s 失败: %w", uiPath, err)
    }
    pkg.UISchemas[version] = ui
  }
  for _, ref := range index.Migrations {
    data, err := picoclawRulesEmbed.ReadFile(path.Join(picoclawRulesEmbedDir, ref.Path))
    if err != nil {
      return nil, fmt.Errorf("读取嵌入 migration %s 失败: %w", ref.Path, err)
    }
    var migration PicoClawConfigMigration
    if err := json.Unmarshal(data, &migration); err != nil {
      return nil, fmt.Errorf("解析嵌入 migration %s 失败: %w", ref.Path, err)
    }
    pkg.Migrations[fmt.Sprintf("%d:%d", ref.FromConfig, ref.ToConfig)] = migration
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

// SeedPicoClawAdapterToDB 将嵌入的适配器包种子写入数据库（仅当数据库为空时）
func SeedPicoClawAdapterToDB(engine *xorm.Engine) error {
  existing, err := PicoClawAdapterPackageFromDB(engine)
  if err != nil {
    return fmt.Errorf("检查数据库适配器失败: %w", err)
  }
  if existing != nil {
    return nil
  }
  pkg, err := NewPicoClawAdapterPackageFromEmbed()
  if err != nil {
    return fmt.Errorf("从嵌入加载适配器失败: %w", err)
  }
  return SavePicoClawAdapterPackageToDB(engine, pkg, "")
}
