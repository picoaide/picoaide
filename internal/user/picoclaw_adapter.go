package user

import (
  "archive/zip"
  "bytes"
  "crypto/sha256"
  "encoding/hex"
  "encoding/json"
  "errors"
  "fmt"
  "io"
  "log/slog"
  "net/http"
  "os"
  "path"
  "path/filepath"
  "runtime"
  "sort"
  "strconv"
  "strings"
  "time"

  "github.com/picoaide/picoaide/internal/auth"
  "xorm.io/xorm"
)

const PicoAideSupportedAdapterSchemaVersion = 1
const picoclawAdapterDir = "picoclaw"
const picoclawAdapterIndexFile = "index.json"
const picoclawAdapterHashFile = "hash"

type PicoClawAdapterIndex struct {
  AdapterSchemaVersion         int                           `json:"adapter_schema_version"`
  AdapterVersion               string                        `json:"adapter_version"`
  LatestSupportedConfigVersion int                           `json:"latest_supported_config_version"`
  PicoClawVersions             []PicoClawAdapterVersion      `json:"picoclaw_versions"`
  ConfigSchemas                map[string]string             `json:"config_schemas"`
  UISchemas                    map[string]string             `json:"ui_schemas"`
  Migrations                   []PicoClawAdapterMigrationRef `json:"migrations"`
}

type PicoClawAdapterVersion struct {
  Version       string   `json:"version"`
  ConfigVersion int      `json:"config_version"`
  ChannelTypes  []string `json:"channel_types,omitempty"`
}

type PicoClawAdapterMigrationRef struct {
  FromConfig int    `json:"from_config"`
  ToConfig   int    `json:"to_config"`
  Path       string `json:"path"`
}

type PicoClawConfigSchema struct {
  ConfigVersion       int    `json:"config_version"`
  ChannelsPath        string `json:"channels_path"`
  ChannelSettingsPath string `json:"channel_settings_path"`
  ModelsPath          string `json:"models_path"`
  DefaultModelPath    string `json:"default_model_path"`
  Security            struct {
    ChannelsPath        string `json:"channels_path"`
    ChannelSettingsPath string `json:"channel_settings_path"`
    ModelsPath          string `json:"models_path"`
  } `json:"security"`
  SingletonChannels []string `json:"singleton_channels"`
  ChannelTypes      []string `json:"channel_types"`
}

type PicoClawUISchema struct {
  ConfigVersion int `json:"config_version"`
  Pages         []struct {
    Key      string `json:"key"`
    Label    string `json:"label"`
    Sections []struct {
      Key    string `json:"key"`
      Label  string `json:"label"`
      Fields []struct {
        Key      string   `json:"key"`
        Label    string   `json:"label"`
        Type     string   `json:"type"`
        Storage  string   `json:"storage"`
        Path     string   `json:"path"`
        Secret   bool     `json:"secret,omitempty"`
        Required bool     `json:"required,omitempty"`
        Options  []string `json:"options,omitempty"`
        Default  any      `json:"default,omitempty"`
      } `json:"fields"`
    } `json:"sections"`
  } `json:"pages"`
}

type PicoClawConfigMigration struct {
  FromConfig int                     `json:"from_config"`
  ToConfig   int                     `json:"to_config"`
  Actions    []PicoClawMigrationRule `json:"actions"`
}

type PicoClawAdapterPackage struct {
  Root          string
  Index         PicoClawAdapterIndex
  ConfigSchemas map[int]PicoClawConfigSchema
  UISchemas     map[int]PicoClawUISchema
  Migrations    map[string]PicoClawConfigMigration
}

type SerializableAdapterContent struct {
  Index         PicoClawAdapterIndex              `json:"index"`
  ConfigSchemas map[int]PicoClawConfigSchema      `json:"config_schemas"`
  UISchemas     map[int]PicoClawUISchema          `json:"ui_schemas"`
  Migrations    map[string]PicoClawConfigMigration `json:"migrations"`
}

type PicoClawAdapterHashEntry struct {
  SHA256 string
  Path   string
}

func NewPicoClawAdapterPackage(cacheDir string) (*PicoClawAdapterPackage, error) {
  engine, err := auth.GetEngine()
  if err == nil {
    pkg, err := PicoClawAdapterPackageFromDB(engine)
    if err == nil && pkg != nil {
      return pkg, nil
    }
  }
  // DB 无数据，从 embed 加载并写入 DB
  pkg, err := NewPicoClawAdapterPackageFromEmbed()
  if err != nil {
    return nil, err
  }
  if engine != nil {
    _ = SavePicoClawAdapterPackageToDB(engine, pkg, "")
  }
  return pkg, nil
}

func ForceReleasePicoClawAdapterCache(cacheDir string) error {
  targetRoot := filepath.Join(cacheDir, picoclawAdapterDir)
  os.RemoveAll(targetRoot)
  if picoclawAdapterEmbedExists() {
    return releasePicoClawAdapterFromEmbed(targetRoot)
  }
  bundledRoot, err := findBundledPicoClawAdapterRoot()
  if err != nil {
    return err
  }
  return copyPicoClawAdapterDir(bundledRoot, targetRoot)
}

func ReleasePicoClawAdapterCacheIfValid(cacheDir string) error {
  targetRoot := filepath.Join(cacheDir, picoclawAdapterDir)
  if _, err := os.Stat(filepath.Join(targetRoot, picoclawAdapterIndexFile)); err != nil {
    return ForceReleasePicoClawAdapterCache(cacheDir)
  }
  if _, err := LoadPicoClawAdapterPackage(targetRoot); err != nil {
    return ForceReleasePicoClawAdapterCache(cacheDir)
  }
  return nil
}

func LoadPicoClawAdapterPackage(root string) (*PicoClawAdapterPackage, error) {
  indexData, err := os.ReadFile(filepath.Join(root, picoclawAdapterIndexFile))
  if err != nil {
    return nil, fmt.Errorf("读取 Picoclaw adapter index 失败: %w", err)
  }
  var index PicoClawAdapterIndex
  if err := json.Unmarshal(indexData, &index); err != nil {
    return nil, fmt.Errorf("解析 Picoclaw adapter index 失败: %w", err)
  }
  pkg := &PicoClawAdapterPackage{
    Root:          root,
    Index:         index,
    ConfigSchemas: make(map[int]PicoClawConfigSchema),
    UISchemas:     make(map[int]PicoClawUISchema),
    Migrations:    make(map[string]PicoClawConfigMigration),
  }
  if err := pkg.loadReferencedFiles(); err != nil {
    return nil, err
  }
  if err := pkg.Validate(); err != nil {
    return nil, err
  }
  return pkg, nil
}

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
  tmpRoot, err := os.MkdirTemp("", "picoaide-picoclaw-adapter-*")
  if err != nil {
    return nil, err
  }
  defer os.RemoveAll(tmpRoot)
  for _, entry := range entries {
    data, err := fetchPicoClawAdapterFile(client, base, entry.Path)
    if err != nil {
      return nil, err
    }
    if got := sha256Hex(data); got != entry.SHA256 {
      return nil, fmt.Errorf("Picoclaw adapter 文件 %s hash 不匹配: got %s want %s", entry.Path, got, entry.SHA256)
    }
    target := filepath.Join(tmpRoot, filepath.FromSlash(entry.Path))
    if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
      return nil, err
    }
    if err := os.WriteFile(target, data, 0644); err != nil {
      return nil, err
    }
  }
  if err := os.WriteFile(filepath.Join(tmpRoot, picoclawAdapterHashFile), hashData, 0644); err != nil {
    return nil, err
  }
  if _, err := LoadPicoClawAdapterPackage(tmpRoot); err != nil {
    return nil, err
  }
  activeRoot := filepath.Join(cacheDir, picoclawAdapterDir)
  if err := activatePicoClawAdapter(tmpRoot, activeRoot); err != nil {
    return nil, err
  }
  return LoadPicoClawAdapterPackage(activeRoot)
}

// RefreshPicoClawAdapterFromRemoteIfChanged 仅当远程 hash 与本地不同时执行刷新
// 返回 (pkg, true, nil) 表示已更新；(nil, false, nil) 表示无变化
// 支持多个回退 URL，按顺序尝试，成功后立即返回
func RefreshPicoClawAdapterFromRemoteIfChanged(cacheDir string, remoteBaseURLs []string, client *http.Client) (*PicoClawAdapterPackage, bool, error) {
  if len(remoteBaseURLs) == 0 {
    return nil, false, errors.New("Picoclaw adapter remote base URLs are empty")
  }
  if client == nil {
    client = &http.Client{Timeout: 20 * time.Second}
  }
  // 读取本地 hash
  localHashPath := filepath.Join(cacheDir, picoclawAdapterDir, picoclawAdapterHashFile)
  localHash, localErr := os.ReadFile(localHashPath)

  // 尝试每个远程 URL
  var lastErr error
  for _, baseURL := range remoteBaseURLs {
    baseURL = strings.TrimRight(baseURL, "/")
    if baseURL == "" {
      continue
    }
    // 获取远程 hash
    entries, remoteHash, err := fetchPicoClawAdapterHash(client, baseURL)
    if err != nil {
      lastErr = err
      continue
    }
    // 比较 hash：如果本地有 hash 文件且内容一致，跳过下载
    if localErr == nil && bytes.Equal(localHash, remoteHash) {
      return nil, false, nil
    }
    // hash 不同或本地无 hash，执行完整下载
    pkg, err := doRefreshPicoClawAdapter(cacheDir, baseURL, client, entries, remoteHash)
    if err != nil {
      lastErr = err
      continue
    }
    return pkg, true, nil
  }
  return nil, false, fmt.Errorf("所有远程 URL 均失败: %w", lastErr)
}

// doRefreshPicoClawAdapter 从指定 URL 完整下载适配器
func doRefreshPicoClawAdapter(cacheDir, base string, client *http.Client, entries []PicoClawAdapterHashEntry, hashData []byte) (*PicoClawAdapterPackage, error) {
  tmpRoot, err := os.MkdirTemp("", "picoaide-picoclaw-adapter-*")
  if err != nil {
    return nil, err
  }
  defer os.RemoveAll(tmpRoot)
  for _, entry := range entries {
    data, err := fetchPicoClawAdapterFile(client, base, entry.Path)
    if err != nil {
      return nil, err
    }
    if got := sha256Hex(data); got != entry.SHA256 {
      return nil, fmt.Errorf("Picoclaw adapter 文件 %s hash 不匹配: got %s want %s", entry.Path, got, entry.SHA256)
    }
    target := filepath.Join(tmpRoot, filepath.FromSlash(entry.Path))
    if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
      return nil, err
    }
    if err := os.WriteFile(target, data, 0644); err != nil {
      return nil, err
    }
  }
  if err := os.WriteFile(filepath.Join(tmpRoot, picoclawAdapterHashFile), hashData, 0644); err != nil {
    return nil, err
  }
  if _, err := LoadPicoClawAdapterPackage(tmpRoot); err != nil {
    return nil, err
  }
  activeRoot := filepath.Join(cacheDir, picoclawAdapterDir)
  if err := activatePicoClawAdapter(tmpRoot, activeRoot); err != nil {
    return nil, err
  }
  return LoadPicoClawAdapterPackage(activeRoot)
}

// AutoRefreshPicoClawAdapter 后台自动刷新适配器
// 阶段一：释放本地嵌入版（从 //go:embed 复制到缓存目录）
// 阶段二：远程更新检测（hash 对比，失败指数退避，成功后每 24h 检查）
func AutoRefreshPicoClawAdapter(cacheDir string, remoteBaseURLs []string) {
  // 阶段一：确保本地嵌入版已释放（含损坏检测）
  if err := ReleasePicoClawAdapterCacheIfValid(cacheDir); err != nil {
    slog.Warn("释放本地 Picoclaw 适配器失败", "error", err)
  } else {
    slog.Info("本地 Picoclaw 适配器已就绪")
  }

  if len(remoteBaseURLs) == 0 {
    return
  }

  // 阶段二：远程更新检测
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

func SavePicoClawAdapterZip(cacheDir string, data []byte) (*PicoClawAdapterPackage, error) {
  if len(data) == 0 {
    return nil, errors.New("adapter zip 为空")
  }
  reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
  if err != nil {
    return nil, fmt.Errorf("读取 adapter zip 失败: %w", err)
  }
  tmpRoot, err := os.MkdirTemp("", "picoaide-picoclaw-adapter-upload-*")
  if err != nil {
    return nil, err
  }
  defer os.RemoveAll(tmpRoot)
  tmpRootHandle, err := os.OpenRoot(tmpRoot)
  if err != nil {
    return nil, err
  }
  defer tmpRootHandle.Close()

  seen := map[string]bool{}
  for _, file := range reader.File {
    name := strings.TrimSpace(strings.ReplaceAll(file.Name, "\\", "/"))
    if name == "" {
      return nil, errors.New("adapter zip 包含空文件名")
    }
    if file.FileInfo().IsDir() {
      continue
    }
    if strings.HasPrefix(name, "/") || path.Clean(name) == "." || strings.HasPrefix(path.Clean(name), "../") || path.Clean(name) == ".." {
      return nil, fmt.Errorf("adapter zip 文件路径不合法: %s", file.Name)
    }
    clean := path.Clean(name)
    if strings.Contains(clean, "/") && strings.Split(clean, "/")[0] == picoclawAdapterDir {
      return nil, errors.New("adapter zip 不能包含 picoclaw 顶层目录，请直接压缩 rules/picoclaw/ 下的文件")
    }
    if clean != picoclawAdapterHashFile {
      if _, err := validatePicoClawAdapterRelPath(clean); err != nil {
        return nil, fmt.Errorf("adapter zip 文件 %s 不合法: %w", clean, err)
      }
    }
    if seen[clean] {
      return nil, fmt.Errorf("adapter zip 文件重复: %s", clean)
    }
    seen[clean] = true
    rc, err := file.Open()
    if err != nil {
      return nil, err
    }
    content, readErr := io.ReadAll(io.LimitReader(rc, 4<<20))
    closeErr := rc.Close()
    if readErr != nil {
      return nil, fmt.Errorf("读取 adapter zip 文件 %s 失败: %w", clean, readErr)
    }
    if closeErr != nil {
      return nil, fmt.Errorf("关闭 adapter zip 文件 %s 失败: %w", clean, closeErr)
    }
    target := filepath.FromSlash(clean)
    if err := tmpRootHandle.MkdirAll(filepath.Dir(target), 0755); err != nil {
      return nil, err
    }
    if err := tmpRootHandle.WriteFile(target, content, 0644); err != nil {
      return nil, err
    }
  }
  if !seen[picoclawAdapterIndexFile] {
    return nil, errors.New("adapter zip 缺少 index.json")
  }
  if !seen[picoclawAdapterHashFile] {
    return nil, errors.New("adapter zip 缺少 hash")
  }
  if err := VerifyPicoClawAdapterHash(tmpRoot); err != nil {
    return nil, err
  }
  if _, err := LoadPicoClawAdapterPackage(tmpRoot); err != nil {
    return nil, err
  }
  activeRoot := filepath.Join(cacheDir, picoclawAdapterDir)
  if err := activatePicoClawAdapter(tmpRoot, activeRoot); err != nil {
    return nil, err
  }
  return LoadPicoClawAdapterPackage(activeRoot)
}

func (p *PicoClawAdapterPackage) loadReferencedFiles() error {
  for key, rel := range p.Index.ConfigSchemas {
    version, err := strconv.Atoi(key)
    if err != nil {
      return fmt.Errorf("config_schemas key %q 不是数字", key)
    }
    data, err := p.readAdapterJSON(rel)
    if err != nil {
      return err
    }
    var schema PicoClawConfigSchema
    if err := json.Unmarshal(data, &schema); err != nil {
      return fmt.Errorf("解析 %s 失败: %w", rel, err)
    }
    if schema.ConfigVersion != version {
      return fmt.Errorf("%s config_version=%d 与 index key=%d 不一致", rel, schema.ConfigVersion, version)
    }
    p.ConfigSchemas[version] = schema
  }
  for key, rel := range p.Index.UISchemas {
    version, err := strconv.Atoi(key)
    if err != nil {
      return fmt.Errorf("ui_schemas key %q 不是数字", key)
    }
    data, err := p.readAdapterJSON(rel)
    if err != nil {
      return err
    }
    var schema PicoClawUISchema
    if err := json.Unmarshal(data, &schema); err != nil {
      return fmt.Errorf("解析 %s 失败: %w", rel, err)
    }
    if schema.ConfigVersion != version {
      return fmt.Errorf("%s config_version=%d 与 index key=%d 不一致", rel, schema.ConfigVersion, version)
    }
    p.UISchemas[version] = schema
  }
  for _, ref := range p.Index.Migrations {
    data, err := p.readAdapterJSON(ref.Path)
    if err != nil {
      return err
    }
    var migration PicoClawConfigMigration
    if err := json.Unmarshal(data, &migration); err != nil {
      return fmt.Errorf("解析 %s 失败: %w", ref.Path, err)
    }
    if migration.FromConfig != ref.FromConfig || migration.ToConfig != ref.ToConfig {
      return fmt.Errorf("%s from/to 与 index 不一致", ref.Path)
    }
    p.Migrations[migrationKey(ref.FromConfig, ref.ToConfig)] = migration
  }
  return nil
}

func (p *PicoClawAdapterPackage) Validate() error {
  if p.Index.AdapterSchemaVersion <= 0 {
    return errors.New("adapter 缺少 adapter_schema_version")
  }
  if p.Index.AdapterSchemaVersion > PicoAideSupportedAdapterSchemaVersion {
    return fmt.Errorf("adapter_schema_version=%d 超过当前 PicoAide 支持的 %d，请升级 PicoAide", p.Index.AdapterSchemaVersion, PicoAideSupportedAdapterSchemaVersion)
  }
  if p.Index.LatestSupportedConfigVersion <= 0 {
    return errors.New("adapter 缺少 latest_supported_config_version")
  }
  if p.Index.LatestSupportedConfigVersion > PicoAideSupportedPicoClawConfigVersion {
    return fmt.Errorf("adapter 声明 Picoclaw 配置版本最高为 %d，但当前 PicoAide 只支持到 %d，暂不允许导入。请发送 issue 催促管理员适配新配置版本：%s", p.Index.LatestSupportedConfigVersion, PicoAideSupportedPicoClawConfigVersion, PicoAideIssueURL)
  }
  if len(p.Index.PicoClawVersions) == 0 {
    return errors.New("adapter 缺少 picoclaw_versions")
  }
  seenVersions := map[string]bool{}
  for _, version := range p.Index.PicoClawVersions {
    if !validPicoClawVersion(version.Version) {
      return fmt.Errorf("Picoclaw 版本格式不合法: %s", version.Version)
    }
    if version.ConfigVersion <= 0 {
      return fmt.Errorf("Picoclaw %s 缺少 config_version", version.Version)
    }
    if version.ConfigVersion > p.Index.LatestSupportedConfigVersion {
      return fmt.Errorf("Picoclaw %s config_version=%d 超过 adapter latest_supported_config_version=%d", version.Version, version.ConfigVersion, p.Index.LatestSupportedConfigVersion)
    }
    normalized := normalizeVersion(version.Version)
    if seenVersions[normalized] {
      return fmt.Errorf("Picoclaw 版本重复: %s", version.Version)
    }
    seenVersions[normalized] = true
    if _, ok := p.ConfigSchemas[version.ConfigVersion]; !ok {
      return fmt.Errorf("Picoclaw %s 引用的 config schema v%d 不存在", version.Version, version.ConfigVersion)
    }
    if len(version.ChannelTypes) > 0 {
      schema := p.ConfigSchemas[version.ConfigVersion]
      allowed := stringSet(schema.ChannelTypes)
      for _, channelType := range version.ChannelTypes {
        if !allowed[channelType] {
          return fmt.Errorf("Picoclaw %s channel_types 包含 config v%d 未声明的渠道: %s", version.Version, version.ConfigVersion, channelType)
        }
      }
    }
  }
  for version := range p.ConfigSchemas {
    if _, ok := p.UISchemas[version]; !ok {
      return fmt.Errorf("config v%d 缺少 UI schema", version)
    }
    if err := p.validateUIChannelCoverage(version); err != nil {
      return err
    }
  }
  return nil
}

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

var picoclawAdapterTableName = (&auth.PicoclawAdapterPackage{}).TableName()

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
    AdapterVersion:               pkg.Index.AdapterVersion,
    AdapterSchemaVersion:         pkg.Index.AdapterSchemaVersion,
    LatestSupportedConfigVersion: pkg.Index.LatestSupportedConfigVersion,
    Content:                      content,
    Hash:                         hash,
    RefreshedAt:                  now,
    CreatedAt:                    now,
  }
  if _, err := engine.Where("1=1").Delete(&auth.PicoclawAdapterPackage{}); err != nil {
    return fmt.Errorf("清理旧适配器包失败: %w", err)
  }
  if _, err := engine.Insert(record); err != nil {
    return fmt.Errorf("保存适配器包到数据库失败: %w", err)
  }
  return nil
}

func (p *PicoClawAdapterPackage) validateUIChannelCoverage(version int) error {
  schema, ok := p.ConfigSchemas[version]
  if !ok {
    return fmt.Errorf("config v%d 缺少 config schema", version)
  }
  ui, ok := p.UISchemas[version]
  if !ok {
    return fmt.Errorf("config v%d 缺少 UI schema", version)
  }
  schemaChannels := stringSet(schema.ChannelTypes)
  uiChannels := map[string]bool{}
  for _, section := range findPicoClawUISections(ui, "channels") {
    uiChannels[section.Key] = true
    if !schemaChannels[section.Key] {
      return fmt.Errorf("config v%d UI schema 声明了不支持的渠道: %s", version, section.Key)
    }
  }
  for _, channelType := range schema.ChannelTypes {
    if !uiChannels[channelType] {
      return fmt.Errorf("config v%d UI schema 缺少渠道: %s", version, channelType)
    }
  }
  return nil
}

func (p *PicoClawAdapterPackage) ChannelTypesFor(configVersion int, picoclawVersion string) []string {
  if p == nil {
    return nil
  }
  if normalized := normalizeVersion(picoclawVersion); normalized != "" {
    for _, version := range p.Index.PicoClawVersions {
      if normalizeVersion(version.Version) == normalized && version.ConfigVersion == configVersion && len(version.ChannelTypes) > 0 {
        return append([]string(nil), version.ChannelTypes...)
      }
    }
  }
  if schema, ok := p.ConfigSchemas[configVersion]; ok {
    return append([]string(nil), schema.ChannelTypes...)
  }
  return nil
}

func stringSet(values []string) map[string]bool {
  out := make(map[string]bool, len(values))
  for _, value := range values {
    value = strings.TrimSpace(value)
    if value != "" {
      out[value] = true
    }
  }
  return out
}

func (p *PicoClawAdapterPackage) ToMigrationRuleSet() PicoClawMigrationRuleSet {
  adapterVersions := append([]PicoClawAdapterVersion(nil), p.Index.PicoClawVersions...)
  sort.SliceStable(adapterVersions, func(i, j int) bool {
    return compareVersionStrings(adapterVersions[i].Version, adapterVersions[j].Version) < 0
  })
  versions := make([]PicoClawMigrationVersionRule, 0, len(adapterVersions))
  prevConfigVersion := 0
  for _, version := range adapterVersions {
    rule := PicoClawMigrationVersionRule{
      Version:       version.Version,
      ConfigVersion: version.ConfigVersion,
      ConfigChanged: false,
      Actions:       []PicoClawMigrationRule{},
    }
    if prevConfigVersion > 0 && prevConfigVersion != version.ConfigVersion {
      if migration, ok := p.Migrations[migrationKey(prevConfigVersion, version.ConfigVersion)]; ok {
        rule.ConfigChanged = true
        rule.FromConfig = prevConfigVersion
        rule.ToConfig = version.ConfigVersion
        rule.Actions = migration.Actions
      }
    }
    versions = append(versions, rule)
    prevConfigVersion = version.ConfigVersion
  }
  return PicoClawMigrationRuleSet{
    LatestSupportedConfigVersion: p.Index.LatestSupportedConfigVersion,
    Versions:                     versions,
  }
}

func (p *PicoClawAdapterPackage) readAdapterJSON(rel string) ([]byte, error) {
  clean, err := validatePicoClawAdapterRelPath(rel)
  if err != nil {
    return nil, err
  }
  data, err := os.ReadFile(filepath.Join(p.Root, filepath.FromSlash(clean)))
  if err != nil {
    return nil, fmt.Errorf("读取 adapter 文件 %s 失败: %w", clean, err)
  }
  return data, nil
}

func ParsePicoClawAdapterHash(data []byte) ([]PicoClawAdapterHashEntry, error) {
  lines := strings.Split(string(data), "\n")
  var entries []PicoClawAdapterHashEntry
  seen := map[string]bool{}
  for lineNo, line := range lines {
    line = strings.TrimSpace(line)
    if line == "" || strings.HasPrefix(line, "#") {
      continue
    }
    parts := strings.Fields(line)
    if len(parts) != 2 {
      return nil, fmt.Errorf("hash 第 %d 行格式不合法", lineNo+1)
    }
    sum := strings.ToLower(parts[0])
    if len(sum) != 64 {
      return nil, fmt.Errorf("hash 第 %d 行 sha256 长度不合法", lineNo+1)
    }
    if _, err := hex.DecodeString(sum); err != nil {
      return nil, fmt.Errorf("hash 第 %d 行 sha256 不合法: %w", lineNo+1, err)
    }
    rel, err := validatePicoClawAdapterRelPath(parts[1])
    if err != nil {
      return nil, fmt.Errorf("hash 第 %d 行路径不合法: %w", lineNo+1, err)
    }
    if seen[rel] {
      return nil, fmt.Errorf("hash 文件重复声明: %s", rel)
    }
    seen[rel] = true
    entries = append(entries, PicoClawAdapterHashEntry{SHA256: sum, Path: rel})
  }
  if len(entries) == 0 {
    return nil, errors.New("hash 文件为空")
  }
  if !seen[picoclawAdapterIndexFile] {
    return nil, errors.New("hash 文件缺少 index.json")
  }
  return entries, nil
}

func VerifyPicoClawAdapterHash(root string) error {
  hashData, err := os.ReadFile(filepath.Join(root, picoclawAdapterHashFile))
  if err != nil {
    return fmt.Errorf("读取 adapter hash 失败: %w", err)
  }
  entries, err := ParsePicoClawAdapterHash(hashData)
  if err != nil {
    return err
  }
  seen := map[string]bool{}
  for _, entry := range entries {
    data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(entry.Path)))
    if err != nil {
      return fmt.Errorf("读取 adapter 文件 %s 失败: %w", entry.Path, err)
    }
    if got := sha256Hex(data); got != entry.SHA256 {
      return fmt.Errorf("Picoclaw adapter 文件 %s hash 不匹配: got %s want %s", entry.Path, got, entry.SHA256)
    }
    seen[entry.Path] = true
  }
  return filepath.WalkDir(root, func(filePath string, entry os.DirEntry, err error) error {
    if err != nil {
      return err
    }
    if entry.IsDir() {
      return nil
    }
    rel, err := filepath.Rel(root, filePath)
    if err != nil {
      return err
    }
    clean := filepath.ToSlash(rel)
    if clean == picoclawAdapterHashFile {
      return nil
    }
    if !seen[clean] {
      return fmt.Errorf("adapter 文件 %s 未在 hash 中声明", clean)
    }
    return nil
  })
}

func validatePicoClawAdapterRelPath(rel string) (string, error) {
  rel = strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
  if rel == "" {
    return "", errors.New("路径为空")
  }
  if strings.HasPrefix(rel, "/") {
    return "", errors.New("不能使用绝对路径")
  }
  clean := path.Clean(rel)
  if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
    return "", errors.New("路径不能逃逸 adapter 根目录")
  }
  if clean == picoclawAdapterHashFile {
    return "", errors.New("hash 文件不应列入自身")
  }
  if filepath.Ext(clean) != ".json" {
    return "", errors.New("adapter 只允许引用 JSON 文件")
  }
  return clean, nil
}

func fetchPicoClawAdapterHash(client *http.Client, base string) ([]PicoClawAdapterHashEntry, []byte, error) {
  data, err := fetchPicoClawAdapterURL(client, base+"/"+picoclawAdapterHashFile)
  if err != nil {
    return nil, nil, err
  }
  entries, err := ParsePicoClawAdapterHash(data)
  if err != nil {
    return nil, nil, err
  }
  return entries, data, nil
}

func fetchPicoClawAdapterFile(client *http.Client, base, rel string) ([]byte, error) {
  clean, err := validatePicoClawAdapterRelPath(rel)
  if err != nil {
    return nil, err
  }
  return fetchPicoClawAdapterURL(client, base+"/"+clean)
}

func fetchPicoClawAdapterURL(client *http.Client, url string) ([]byte, error) {
  resp, err := client.Get(url)
  if err != nil {
    return nil, err
  }
  defer resp.Body.Close()
  if resp.StatusCode < 200 || resp.StatusCode >= 300 {
    return nil, fmt.Errorf("%s HTTP %d", url, resp.StatusCode)
  }
  data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
  if err != nil {
    return nil, err
  }
  return data, nil
}

func findBundledPicoClawAdapterRoot() (string, error) {
  paths := []string{
    filepath.Join("internal", "user", picoclawRulesEmbedDir),
    filepath.Join(filepath.Dir(os.Args[0]), "internal", "user", picoclawRulesEmbedDir),
  }
  if _, file, _, ok := runtime.Caller(0); ok {
    paths = append(paths, filepath.Join(filepath.Dir(file), picoclawRulesEmbedDir))
  }
  for _, root := range paths {
    if _, err := os.Stat(filepath.Join(root, picoclawAdapterIndexFile)); err == nil {
      return root, nil
    }
  }
  return "", errors.New("未找到内置 Picoclaw adapter")
}

func copyPicoClawAdapterDir(srcRoot, dstRoot string) error {
  if err := os.MkdirAll(dstRoot, 0755); err != nil {
    return err
  }
  return filepath.WalkDir(srcRoot, func(src string, entry os.DirEntry, err error) error {
    if err != nil {
      return err
    }
    rel, err := filepath.Rel(srcRoot, src)
    if err != nil {
      return err
    }
    if rel == "." {
      return nil
    }
    dst := filepath.Join(dstRoot, rel)
    if entry.IsDir() {
      return os.MkdirAll(dst, 0755)
    }
    if filepath.Ext(entry.Name()) != ".json" && entry.Name() != picoclawAdapterHashFile {
      return nil
    }
    data, err := os.ReadFile(src)
    if err != nil {
      return err
    }
    return os.WriteFile(dst, data, 0644)
  })
}

func activatePicoClawAdapter(tmpRoot, activeRoot string) error {
  parent := filepath.Dir(activeRoot)
  if err := os.MkdirAll(parent, 0755); err != nil {
    return err
  }
  nextRoot := activeRoot + ".next"
  oldRoot := activeRoot + ".old"
  _ = os.RemoveAll(nextRoot)
  if err := copyPicoClawAdapterDir(tmpRoot, nextRoot); err != nil {
    return err
  }
  _ = os.RemoveAll(oldRoot)
  if _, err := os.Stat(activeRoot); err == nil {
    if err := os.Rename(activeRoot, oldRoot); err != nil {
      return err
    }
  }
  if err := os.Rename(nextRoot, activeRoot); err != nil {
    if _, statErr := os.Stat(oldRoot); statErr == nil {
      _ = os.Rename(oldRoot, activeRoot)
    }
    return err
  }
  _ = os.RemoveAll(oldRoot)
  return nil
}

func migrationKey(from, to int) string {
  return fmt.Sprintf("%d:%d", from, to)
}

func sha256Hex(data []byte) string {
  sum := sha256.Sum256(data)
  return hex.EncodeToString(sum[:])
}
