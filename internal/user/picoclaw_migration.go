package user

import (
  "fmt"
  "net/http"
  "os"
  "path/filepath"
  "regexp"
  "strconv"
  "strings"
  "time"
)

const PicoAideIssueURL = "https://github.com/picoaide/picoaide/issues"
const PicoAideSupportedPicoClawConfigVersion = 3

type PicoClawMigrationRuleSet struct {
  LatestSupportedConfigVersion int                            `json:"latest_supported_config_version"`
  Versions                     []PicoClawMigrationVersionRule `json:"versions"`
}

type PicoClawMigrationVersionRule struct {
  Version       string                  `json:"version"`
  ConfigVersion int                     `json:"config_version"`
  ConfigChanged bool                    `json:"config_changed"`
  FromConfig    int                     `json:"from_config,omitempty"`
  ToConfig      int                     `json:"to_config,omitempty"`
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

type PicoClawMigrationRulesInfo struct {
  LatestSupportedConfigVersion   int                            `json:"latest_supported_config_version"`
  PicoAideSupportedConfigVersion int                            `json:"picoaide_supported_config_version"`
  AdapterSchemaVersion           int                            `json:"adapter_schema_version,omitempty"`
  AdapterVersion                 string                         `json:"adapter_version,omitempty"`
  Versions                       []PicoClawMigrationVersionRule `json:"versions"`
  CachePath                      string                         `json:"cache_path"`
  UpdatedAt                      string                         `json:"updated_at,omitempty"`
}

type PicoClawMigrationService struct {
  cacheDir string
  rules    PicoClawMigrationRuleSet
}

var picoclawVersionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)`)

func NewPicoClawMigrationService(cacheDir string) (*PicoClawMigrationService, error) {
  svc := &PicoClawMigrationService{
    cacheDir: cacheDir,
  }
  if err := svc.ReleaseBundledRulesCache(); err != nil {
    return nil, err
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

func ForceReleasePicoClawMigrationRulesCache(cacheDir string) error {
  return ForceReleasePicoClawAdapterCache(cacheDir)
}

func ReleasePicoClawMigrationRulesCacheIfValid(cacheDir string) error {
  return ReleasePicoClawAdapterCacheIfValid(cacheDir)
}

func RefreshPicoClawMigrationRulesFromAdapter(cacheDir, remoteBaseURL string) error {
  _, err := RefreshPicoClawAdapterFromRemote(cacheDir, remoteBaseURL, &http.Client{Timeout: 20 * time.Second})
  return err
}

func RefreshPicoClawMigrationRulesFromURLs(cacheDir string, remoteBaseURLs []string) error {
  _, _, err := RefreshPicoClawAdapterFromRemoteIfChanged(cacheDir, remoteBaseURLs, &http.Client{Timeout: 20 * time.Second})
  return err
}

func RefreshPicoClawMigrationRulesFromURLsCheck(cacheDir string, remoteBaseURLs []string) (bool, error) {
  _, changed, err := RefreshPicoClawAdapterFromRemoteIfChanged(cacheDir, remoteBaseURLs, &http.Client{Timeout: 20 * time.Second})
  return changed, err
}

func AutoRefreshPicoClawMigrationRules(cacheDir string, remoteBaseURLs []string) {
  AutoRefreshPicoClawAdapter(cacheDir, remoteBaseURLs)
}

func LoadPicoClawMigrationRulesInfo(cacheDir string) (PicoClawMigrationRulesInfo, error) {
  pkg, err := NewPicoClawAdapterPackage(cacheDir)
  if err != nil {
    return PicoClawMigrationRulesInfo{}, err
  }
  rules := pkg.ToMigrationRuleSet()
  info := PicoClawMigrationRulesInfo{
    LatestSupportedConfigVersion:   rules.LatestSupportedConfigVersion,
    PicoAideSupportedConfigVersion: rules.LatestSupportedConfigVersion,
    AdapterSchemaVersion:           pkg.Index.AdapterSchemaVersion,
    AdapterVersion:                 pkg.Index.AdapterVersion,
    Versions:                       rules.Versions,
    CachePath:                      filepath.Join(cacheDir, picoclawAdapterDir, picoclawAdapterIndexFile),
  }
  if st, err := os.Stat(info.CachePath); err == nil {
    info.UpdatedAt = st.ModTime().Format(time.RFC3339)
  }
  return info, nil
}

func (s *PicoClawMigrationService) ReleaseBundledRulesCache() error {
  if s == nil {
    return nil
  }
  return ReleasePicoClawAdapterCacheIfValid(s.cacheDir)
}

func (s *PicoClawMigrationService) EnsureUpgradeable(fromTag, toTag string) error {
  if s == nil {
    return nil
  }
  if err := s.rules.EnsureSupportedByPicoAide(); err != nil {
    return err
  }
  if missing := s.rules.UnsupportedEndpointVersions(fromTag, toTag); len(missing) > 0 {
    return fmt.Errorf("Picoclaw 版本未在当前适配包中声明（%s），默认只支持已适配版本间升级/降级。请更新配置适配包或发送 issue 催促管理员适配：%s", strings.Join(missing, ", "), PicoAideIssueURL)
  }
  if compareVersionStrings(fromTag, toTag) >= 0 {
    return nil
  }
  missing := s.rules.MissingVersions(fromTag, toTag)
  if len(missing) == 0 {
    return nil
  }
  return fmt.Errorf("仓库未适配 Picoclaw 镜像/配置升级规则（缺少 %s），暂不允许升级镜像。请发送 issue 催促管理员尽快适配：%s", strings.Join(missing, ", "), PicoAideIssueURL)
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
  pkg, err := NewPicoClawAdapterPackage(s.cacheDir)
  if err != nil {
    return PicoClawMigrationRuleSet{}, fmt.Errorf("未找到本地 Picoclaw adapter，请先手动更新配置适配包: %w", err)
  }
  return pkg.ToMigrationRuleSet(), nil
}

func (r PicoClawMigrationRuleSet) EnsureSupportedByPicoAide() error {
  if r.LatestSupportedConfigVersion > PicoAideSupportedPicoClawConfigVersion {
    return fmt.Errorf("迁移规则声明 Picoclaw 配置版本最高为 %d，但当前 PicoAide 只支持到 %d，暂不允许升级镜像。请发送 issue 催促管理员适配新配置版本：%s", r.LatestSupportedConfigVersion, PicoAideSupportedPicoClawConfigVersion, PicoAideIssueURL)
  }
  for _, version := range r.Versions {
    if version.ConfigVersion > PicoAideSupportedPicoClawConfigVersion {
      return fmt.Errorf("Picoclaw %s 使用配置版本 %d，但当前 PicoAide 只支持到 %d，暂不允许升级镜像。请发送 issue 催促管理员适配新配置版本：%s", version.Version, version.ConfigVersion, PicoAideSupportedPicoClawConfigVersion, PicoAideIssueURL)
    }
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
  var missing []string
  if _, ok := ruleMap[current]; !ok {
    missing = append(missing, current)
  }
  for _, version := range r.Versions {
    normalized := normalizeVersion(version.Version)
    if compareVersionStrings(normalized, current) > 0 && compareVersionStrings(normalized, target) <= 0 {
      if _, ok := ruleMap[normalized]; !ok {
        missing = append(missing, normalized)
      }
    }
  }
  if _, ok := ruleMap[target]; !ok {
    missing = append(missing, target)
  }
  return uniqueStrings(missing)
}

func (r PicoClawMigrationRuleSet) UnsupportedEndpointVersions(fromTag, toTag string) []string {
  ruleMap := r.versionMap()
  var missing []string
  for _, tag := range []string{fromTag, toTag} {
    normalized := normalizeVersion(tag)
    if normalized == "" {
      missing = append(missing, tag)
      continue
    }
    if _, ok := ruleMap[normalized]; !ok {
      missing = append(missing, normalized)
    }
  }
  return uniqueStrings(missing)
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
  case "delete":
    deleteByPath(cfg, rule.Path)
    return nil
  case "rename":
    return renameByMode(cfg, rule.From, rule.To, rule.Mode)
  case "move":
    return moveByPattern(cfg, rule.Path, rule.To)
  case "map":
    return mapNestedField(cfg, rule.Path, rule.Field, rule.To, rule.Value)
  case "infer_model_enabled":
    inferModelEnabled(cfg)
    return nil
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

func moveByPattern(cfg map[string]interface{}, fromPattern, toPattern string) error {
  fromParts := strings.Split(fromPattern, ".")
  toParts := strings.Split(toPattern, ".")
  if len(fromParts) != len(toParts) || len(fromParts) == 0 {
    return fmt.Errorf("move pattern 不合法: %s -> %s", fromPattern, toPattern)
  }
  wildcardIndex := -1
  for i, part := range fromParts {
    if part == "*" {
      wildcardIndex = i
      break
    }
  }
  if wildcardIndex < 0 || toParts[wildcardIndex] != "*" {
    value, ok := deepGet(cfg, fromPattern)
    if !ok {
      return nil
    }
    deleteByPath(cfg, fromPattern)
    setByPath(cfg, toPattern, value)
    return nil
  }
  rootPath := strings.Join(fromParts[:wildcardIndex], ".")
  root, ok := deepGet(cfg, rootPath)
  if !ok {
    return nil
  }
  rootMap, ok := root.(map[string]interface{})
  if !ok {
    return nil
  }
  for key := range rootMap {
    concreteFrom := strings.ReplaceAll(fromPattern, "*", key)
    concreteTo := strings.ReplaceAll(toPattern, "*", key)
    value, ok := deepGet(cfg, concreteFrom)
    if !ok {
      continue
    }
    deleteByPath(cfg, concreteFrom)
    setByPath(cfg, concreteTo, value)
  }
  return nil
}

func inferModelEnabled(cfg map[string]interface{}) {
  modelList, ok := cfg["model_list"].([]interface{})
  if !ok {
    return
  }
  for _, raw := range modelList {
    model, ok := raw.(map[string]interface{})
    if !ok {
      continue
    }
    if apiKey, ok := model["api_key"].(string); ok && apiKey != "" {
      if _, hasAPIKeys := model["api_keys"]; !hasAPIKeys {
        model["api_keys"] = []interface{}{apiKey}
      }
      delete(model, "api_key")
    }
    if _, hasEnabled := model["enabled"]; hasEnabled {
      continue
    }
    if hasNonEmptyAPIKeys(model["api_keys"]) || model["model_name"] == "local-model" {
      model["enabled"] = true
    }
  }
}

func hasNonEmptyAPIKeys(value interface{}) bool {
  switch keys := value.(type) {
  case []interface{}:
    return len(keys) > 0
  case []string:
    return len(keys) > 0
  default:
    return false
  }
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

func uniqueStrings(values []string) []string {
  if len(values) == 0 {
    return nil
  }
  seen := make(map[string]bool, len(values))
  out := make([]string, 0, len(values))
  for _, value := range values {
    if value == "" || seen[value] {
      continue
    }
    seen[value] = true
    out = append(out, value)
  }
  return out
}
