package config

import (
  "fmt"
  "os"
  "path/filepath"

  "gopkg.in/yaml.v3"
)

// ============================================================
// Home 配置（~/.picoaide-config.yaml）
// ============================================================

type HomeConfig struct {
  WorkDir                      string `yaml:"work_dir"`
  RuleCacheDir                 string `yaml:"rule_cache_dir,omitempty"`
  PicoClawAdapterRemoteBaseURL string `yaml:"picoclaw_adapter_remote_base_url,omitempty"`
}

func getHomeConfigPath() string {
  home, err := os.UserHomeDir()
  if err != nil {
    return ""
  }
  return filepath.Join(home, ".picoaide-config.yaml")
}

func LoadHome() (*HomeConfig, error) {
  path := getHomeConfigPath()
  if path == "" {
    return &HomeConfig{}, nil
  }

  data, err := os.ReadFile(path)
  if err != nil {
    if os.IsNotExist(err) {
      return &HomeConfig{}, nil
    }
    return nil, fmt.Errorf("读取 home 配置失败: %w", err)
  }

  var hcfg HomeConfig
  if err := yaml.Unmarshal(data, &hcfg); err != nil {
    return nil, fmt.Errorf("解析 home 配置失败: %w", err)
  }

  return &hcfg, nil
}

func SaveHome(hcfg *HomeConfig) error {
  path := getHomeConfigPath()
  if path == "" {
    return fmt.Errorf("无法确定 home 目录")
  }

  data, err := yaml.Marshal(hcfg)
  if err != nil {
    return fmt.Errorf("序列化 home 配置失败: %w", err)
  }

  return os.WriteFile(path, data, 0644)
}
