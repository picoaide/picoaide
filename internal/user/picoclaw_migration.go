package user

import (
  "encoding/json"
  "errors"
  "fmt"
  "io"
  "log/slog"
  "net/http"
  "os"
  "path/filepath"
  "regexp"
  "runtime"
  "sort"
  "strconv"
  "strings"
  "time"
)

const PicoAideIssueURL = "https://github.com/picoaide/picoaide/issues"
const defaultPicoClawMigrationRulesURL = "https://raw.githubusercontent.com/picoaide/picoaide/main/rules/picoclaw/migrations.json"
const picoClawMigrationCacheFile = "picoclaw_migrations.json"
const picoClawMigrationUpdateStampFile = "picoclaw_migrations.updated_at"

type PicoClawMigrationRuleSet struct {
  Versions []PicoClawMigrationVersionRule `json:"versions"`
}

type PicoClawMigrationVersionRule struct {
  Version       string                  `json:"version"`
  ConfigChanged bool                    `json:"config_changed"`
  Actions       []PicoClawMigrationRule `json:"actions"`
}

type PicoClawMigrationRule struct {
  Op    string `json:"op"`
  Path  string `json:"path,omitempty"`
  From  string `json:"from,omitempty"`
  To    string `json:"to,omitempty"`
  Mode  string `json:"mode,omitempty"`
  Field string `json:"field,omitempty"`
  Value any    `json:"value,omitempty"`
}

type PicoClawMigrationService struct {
  cacheDir string
  rawURL   string
  rules    PicoClawMigrationRuleSet
  client   *http.Client
}

var picoclawVersionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)`)

func NewPicoClawMigrationService(cacheDir string) (*PicoClawMigrationService, error) {
  svc := &PicoClawMigrationService{
    cacheDir: cacheDir,
    rawURL:   defaultPicoClawMigrationRulesURL,
    client:   &http.Client{Timeout: 20 * time.Second},
  }
  if err := svc.ReleaseBundledRulesCache(); err != nil {
    slog.Warn("释放 Picoclaw 本地迁移规则缓存失败", "error", err)
  }
  rules, err := svc.loadRules()
  if err != nil {
    return nil, err
  }
  svc.rules = rules
  return svc, nil
}

func ReleasePicoClawMigrationRulesCache(cacheDir string) error {
  svc := &PicoClawMigrationService{cacheDir: cacheDir}
  return svc.ReleaseBundledRulesCache()
}

func RefreshPicoClawMigrationRules(cacheDir string) error {
  svc := &PicoClawMigrationService{
    cacheDir: cacheDir,
    rawURL:   defaultPicoClawMigrationRulesURL,
    client:   &http.Client{Timeout: 20 * time.Second},
  }
  return svc.Refresh()
}

func (s *PicoClawMigrationService) ReleaseBundledRulesCache() error {
  if s == nil {
    return nil
  }
  cachePath := filepath.Join(s.cacheDir, picoClawMigrationCacheFile)
  if _, err := os.Stat(cachePath); err == nil {
    return nil
  }
  data, err := readBundledPicoClawMigrationRules()
  if err != nil {
    return err
  }
  rules, err := parsePicoClawMigrationRules(data)
  if err != nil {
    return err
  }
  if err := os.MkdirAll(s.cacheDir, 0755); err != nil {
    return err
  }
  if err := os.WriteFile(cachePath, data, 0644); err != nil {
    return err
  }
  s.rules = rules
  return nil
}

func (s *PicoClawMigrationService) RefreshIfDue() error {
  if s == nil {
    return nil
  }
  stampPath := filepath.Join(s.cacheDir, picoClawMigrationUpdateStampFile)
  if st, err := os.Stat(stampPath); err == nil && time.Since(st.ModTime()) < 24*time.Hour {
    return nil
  }
  if err := s.Refresh(); err != nil {
    slog.Warn("自动更新 Picoclaw 迁移规则失败，将继续使用本地缓存规则", "error", err)
  }
  return nil
}

func (s *PicoClawMigrationService) Refresh() error {
  if s == nil {
    return nil
  }
  var lastErr error
  for i := 0; i < 5; i++ {
    if i > 0 {
      time.Sleep(time.Duration(i) * time.Second)
    }
    rules, data, err := s.fetchRemoteRules()
    if err != nil {
      lastErr = err
      continue
    }
    if err := os.MkdirAll(s.cacheDir, 0755); err != nil {
      return err
    }
    if err := os.WriteFile(filepath.Join(s.cacheDir, picoClawMigrationCacheFile), data, 0644); err != nil {
      return err
    }
    if err := os.WriteFile(filepath.Join(s.cacheDir, picoClawMigrationUpdateStampFile), []byte(time.Now().Format(time.RFC3339)), 0644); err != nil {
      return err
    }
    s.rules = rules
    return nil
  }
  return fmt.Errorf("更新 Picoclaw 迁移规则失败，已重试 5 次: %w", lastErr)
}

func (s *PicoClawMigrationService) EnsureUpgradeable(fromTag, toTag string) error {
  if s == nil || compareVersionStrings(fromTag, toTag) >= 0 {
    return nil
  }
  missing := s.rules.MissingVersions(fromTag, toTag)
  if len(missing) == 0 {
    return nil
  }
  return fmt.Errorf("仓库未适配 Picoclaw 配置升级规则（缺少 %s），暂不允许升级镜像。请发送 issue 催促管理员尽快适配：%s", strings.Join(missing, ", "), PicoAideIssueURL)
}

func (s *PicoClawMigrationService) Migrate(cfg map[string]interface{}, fromTag, toTag string) error {
  if s == nil || compareVersionStrings(fromTag, toTag) >= 0 {
    return nil
  }
  if err := s.EnsureUpgradeable(fromTag, toTag); err != nil {
    return err
  }
  for _, version := range s.rules.VersionChain(fromTag, toTag) {
    if !version.ConfigChanged {
      continue
    }
    if err := applyPicoClawMigrationVersion(cfg, version); err != nil {
      return err
    }
  }
  return nil
}

func (s *PicoClawMigrationService) loadRules() (PicoClawMigrationRuleSet, error) {
  cachePath := filepath.Join(s.cacheDir, picoClawMigrationCacheFile)
  if data, err := os.ReadFile(cachePath); err == nil {
    return parsePicoClawMigrationRules(data)
  }
  data, err := readBundledPicoClawMigrationRules()
  if err != nil {
    return PicoClawMigrationRuleSet{}, fmt.Errorf("未找到本地 Picoclaw 迁移规则缓存，请先手动更新迁移规则: %w", err)
  }
  return parsePicoClawMigrationRules(data)
}

func readBundledPicoClawMigrationRules() ([]byte, error) {
  paths := []string{
    filepath.Join("rules", "picoclaw", "migrations.json"),
    filepath.Join(filepath.Dir(os.Args[0]), "rules", "picoclaw", "migrations.json"),
  }
  if _, file, _, ok := runtime.Caller(0); ok {
    paths = append(paths, filepath.Join(filepath.Dir(file), "..", "..", "rules", "picoclaw", "migrations.json"))
  }
  var lastErr error
  for _, path := range paths {
    data, err := os.ReadFile(path)
    if err == nil {
      return data, nil
    }
    lastErr = err
  }
  return nil, lastErr
}

func (s *PicoClawMigrationService) fetchRemoteRules() (PicoClawMigrationRuleSet, []byte, error) {
  resp, err := s.client.Get(s.rawURL)
  if err != nil {
    return PicoClawMigrationRuleSet{}, nil, err
  }
  defer resp.Body.Close()
  if resp.StatusCode < 200 || resp.StatusCode >= 300 {
    return PicoClawMigrationRuleSet{}, nil, fmt.Errorf("HTTP %d", resp.StatusCode)
  }
  data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
  if err != nil {
    return PicoClawMigrationRuleSet{}, nil, err
  }
  rules, err := parsePicoClawMigrationRules(data)
  if err != nil {
    return PicoClawMigrationRuleSet{}, nil, err
  }
  return rules, data, nil
}

func parsePicoClawMigrationRules(data []byte) (PicoClawMigrationRuleSet, error) {
  var rules PicoClawMigrationRuleSet
  if err := json.Unmarshal(data, &rules); err != nil {
    return rules, fmt.Errorf("解析 Picoclaw 迁移规则失败: %w", err)
  }
  sort.SliceStable(rules.Versions, func(i, j int) bool {
    return compareVersionStrings(rules.Versions[i].Version, rules.Versions[j].Version) < 0
  })
  if err := rules.Validate(); err != nil {
    return rules, err
  }
  return rules, nil
}

func (r PicoClawMigrationRuleSet) Validate() error {
  seen := make(map[string]bool)
  for _, version := range r.Versions {
    if version.Version == "" {
      return errors.New("迁移规则版本不能为空")
    }
    if !validPicoClawVersion(version.Version) {
      return fmt.Errorf("迁移规则版本格式不合法: %s", version.Version)
    }
    if seen[version.Version] {
      return fmt.Errorf("迁移规则版本重复: %s", version.Version)
    }
    seen[version.Version] = true
  }
  return nil
}

func (r PicoClawMigrationRuleSet) MissingVersions(fromTag, toTag string) []string {
  current := normalizeVersion(fromTag)
  target := normalizeVersion(toTag)
  if current == "" || target == "" {
    return []string{toTag}
  }
  if compareVersionStrings(current, target) >= 0 {
    return nil
  }
  ruleMap := r.versionMap()
  if _, ok := ruleMap[target]; !ok {
    return []string{target}
  }
  return nil
}

func (r PicoClawMigrationRuleSet) NextVersionAfter(version string) string {
  for _, candidate := range r.Versions {
    if compareVersionStrings(candidate.Version, version) > 0 {
      return candidate.Version
    }
  }
  return ""
}

func (r PicoClawMigrationRuleSet) versionMap() map[string]PicoClawMigrationVersionRule {
  ruleMap := make(map[string]PicoClawMigrationVersionRule, len(r.Versions))
  for _, version := range r.Versions {
    ruleMap[normalizeVersion(version.Version)] = version
  }
  return ruleMap
}

func (r PicoClawMigrationRuleSet) VersionChain(fromTag, toTag string) []PicoClawMigrationVersionRule {
  var chain []PicoClawMigrationVersionRule
  for _, version := range r.Versions {
    if compareVersionStrings(version.Version, fromTag) > 0 && compareVersionStrings(version.Version, toTag) <= 0 {
      chain = append(chain, version)
    }
  }
  return chain
}

func applyPicoClawMigrationVersion(cfg map[string]interface{}, version PicoClawMigrationVersionRule) error {
  for _, action := range version.Actions {
    if err := applyPicoClawMigrationRule(cfg, action); err != nil {
      return fmt.Errorf("版本 %s: %w", version.Version, err)
    }
  }
  return nil
}

func applyPicoClawMigrationRule(cfg map[string]interface{}, rule PicoClawMigrationRule) error {
  switch strings.ToLower(strings.TrimSpace(rule.Op)) {
  case "set":
    setByPath(cfg, rule.Path, rule.Value)
    return nil
  case "rename":
    return renameByMode(cfg, rule.From, rule.To, rule.Mode)
  case "map":
    return mapNestedField(cfg, rule.Path, rule.Field, rule.To, rule.Value)
  default:
    return fmt.Errorf("不支持的迁移操作 %q", rule.Op)
  }
}

func renameByMode(cfg map[string]interface{}, fromPath, toPath, mode string) error {
  value, ok := deepGet(cfg, fromPath)
  if !ok {
    return nil
  }
  deleteByPath(cfg, fromPath)
  switch strings.ToLower(strings.TrimSpace(mode)) {
  case "channels_to_nested":
    return migrateChannelsToChannelListValue(cfg, value, toPath)
  default:
    setByPath(cfg, toPath, value)
    return nil
  }
}

func migrateChannelsToChannelListValue(cfg map[string]interface{}, value interface{}, toPath string) error {
  channelMap, ok := value.(map[string]interface{})
  if !ok {
    setByPath(cfg, toPath, value)
    return nil
  }
  for name, raw := range channelMap {
    channelCfg, ok := raw.(map[string]interface{})
    if !ok {
      continue
    }
    if _, ok := channelCfg["type"]; !ok {
      channelCfg["type"] = name
    }
    if _, ok := channelCfg["settings"]; ok {
      continue
    }
    settings := make(map[string]interface{})
    for key, val := range channelCfg {
      if isPicoClawChannelBaseField(key) {
        continue
      }
      settings[key] = val
      delete(channelCfg, key)
    }
    if len(settings) > 0 {
      channelCfg["settings"] = settings
    }
  }
  setByPath(cfg, toPath, channelMap)
  return nil
}

func mapNestedField(cfg map[string]interface{}, pathValue, field, toField string, targetValue any) error {
  root, ok := deepGet(cfg, pathValue)
  if !ok {
    return nil
  }
  rootMap, ok := root.(map[string]interface{})
  if !ok {
    return nil
  }
  for _, raw := range rootMap {
    node, ok := raw.(map[string]interface{})
    if !ok {
      continue
    }
    if old, exists := node[field]; exists {
      if _, has := node[toField]; !has {
        if targetValue != nil {
          node[toField] = targetValue
        } else {
          node[toField] = old
        }
      }
      delete(node, field)
    }
  }
  return nil
}

func deepGet(cfg map[string]interface{}, dottedPath string) (interface{}, bool) {
  if dottedPath == "" {
    return nil, false
  }
  parts := strings.Split(dottedPath, ".")
  var current interface{} = cfg
  for _, part := range parts {
    node, ok := current.(map[string]interface{})
    if !ok {
      return nil, false
    }
    next, ok := node[part]
    if !ok {
      return nil, false
    }
    current = next
  }
  return current, true
}

func setByPath(cfg map[string]interface{}, dottedPath string, value interface{}) {
  if dottedPath == "" {
    return
  }
  parts := strings.Split(dottedPath, ".")
  current := cfg
  for i := 0; i < len(parts)-1; i++ {
    next, ok := current[parts[i]].(map[string]interface{})
    if !ok {
      next = make(map[string]interface{})
      current[parts[i]] = next
    }
    current = next
  }
  current[parts[len(parts)-1]] = value
}

func deleteByPath(cfg map[string]interface{}, dottedPath string) {
  if dottedPath == "" {
    return
  }
  parts := strings.Split(dottedPath, ".")
  if len(parts) == 1 {
    delete(cfg, parts[0])
    return
  }
  current := cfg
  for i := 0; i < len(parts)-1; i++ {
    next, ok := current[parts[i]].(map[string]interface{})
    if !ok {
      return
    }
    current = next
  }
  delete(current, parts[len(parts)-1])
}

func isPicoClawChannelBaseField(key string) bool {
  switch key {
  case "enabled", "type", "allow_from", "reasoning_channel_id", "group_trigger", "typing", "placeholder":
    return true
  default:
    return false
  }
}

func validPicoClawVersion(tag string) bool {
  return normalizeVersion(tag) != ""
}

func normalizeVersion(tag string) string {
  matches := picoclawVersionPattern.FindStringSubmatch(strings.TrimSpace(tag))
  if len(matches) != 4 {
    return ""
  }
  return "v" + matches[1] + "." + matches[2] + "." + matches[3]
}

func picoclawTagAtLeast(tag string, major int, minor int, patch int) bool {
  matches := picoclawVersionPattern.FindStringSubmatch(strings.TrimSpace(tag))
  if len(matches) != 4 {
    return false
  }
  gotMajor, _ := strconv.Atoi(matches[1])
  gotMinor, _ := strconv.Atoi(matches[2])
  gotPatch, _ := strconv.Atoi(matches[3])
  if gotMajor != major {
    return gotMajor > major
  }
  if gotMinor != minor {
    return gotMinor > minor
  }
  return gotPatch >= patch
}

func compareVersionStrings(a, b string) int {
  pa := picoclawVersionPattern.FindStringSubmatch(strings.TrimSpace(a))
  pb := picoclawVersionPattern.FindStringSubmatch(strings.TrimSpace(b))
  if len(pa) != 4 || len(pb) != 4 {
    return strings.Compare(a, b)
  }
  av := []int{mustAtoi(pa[1]), mustAtoi(pa[2]), mustAtoi(pa[3])}
  bv := []int{mustAtoi(pb[1]), mustAtoi(pb[2]), mustAtoi(pb[3])}
  for i := 0; i < 3; i++ {
    if av[i] != bv[i] {
      if av[i] < bv[i] {
        return -1
      }
      return 1
    }
  }
  return 0
}

func mustAtoi(s string) int {
  n, _ := strconv.Atoi(s)
  return n
}
