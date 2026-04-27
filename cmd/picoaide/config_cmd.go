package main

import (
  "encoding/json"
  "fmt"
  "os"
  "path/filepath"

  "gopkg.in/yaml.v3"

  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/user"
  "github.com/picoaide/picoaide/internal/util"
)

func ConfigShow(cfg *config.GlobalConfig) error {
  data, err := yaml.Marshal(cfg)
  if err != nil {
    return err
  }
  fmt.Println(string(data))
  return nil
}

func ConfigSetModel(cfg *config.GlobalConfig, configPath, jsonStr string) error {
  var models []map[string]interface{}
  if err := json.Unmarshal([]byte(jsonStr), &models); err != nil {
    return fmt.Errorf("解析 JSON 失败: %w", err)
  }

  var picoClawMap map[string]interface{}
  if m, ok := cfg.PicoClaw.(map[string]interface{}); ok {
    picoClawMap = util.DeepCopyMap(m)
  } else {
    picoClawMap = make(map[string]interface{})
  }

  modelInterfaces := make([]interface{}, len(models))
  for i, m := range models {
    modelInterfaces[i] = m
  }
  picoClawMap["model_list"] = modelInterfaces
  cfg.PicoClaw = picoClawMap

  return config.Save(cfg, configPath)
}

func ConfigSetKey(cfg *config.GlobalConfig, configPath, modelName, apiKey string) error {
  var secMap map[string]interface{}
  if m, ok := cfg.Security.(map[string]interface{}); ok {
    secMap = util.DeepCopyMap(m)
  } else {
    secMap = make(map[string]interface{})
  }

  modelList, ok := secMap["model_list"].(map[string]interface{})
  if !ok {
    modelList = make(map[string]interface{})
  }
  secMap["model_list"] = modelList

  if entry, ok := modelList[modelName].(map[string]interface{}); ok {
    keys, _ := entry["api_keys"].([]interface{})
    keys = append(keys, apiKey)
    entry["api_keys"] = keys
    modelList[modelName] = entry
  } else {
    modelList[modelName] = map[string]interface{}{
      "api_keys": []string{apiKey},
    }
  }

  cfg.Security = secMap
  return config.Save(cfg, configPath)
}

func ConfigSetChannel(cfg *config.GlobalConfig, configPath, jsonStr string) error {
  var channels map[string]interface{}
  if err := json.Unmarshal([]byte(jsonStr), &channels); err != nil {
    return fmt.Errorf("解析 JSON 失败: %w", err)
  }

  var picoClawMap map[string]interface{}
  if m, ok := cfg.PicoClaw.(map[string]interface{}); ok {
    picoClawMap = util.DeepCopyMap(m)
  } else {
    picoClawMap = make(map[string]interface{})
  }

  picoClawMap["channels"] = channels
  cfg.PicoClaw = picoClawMap

  return config.Save(cfg, configPath)
}

func ConfigApply(cfg *config.GlobalConfig, targetUser string) error {
  applyFn := func(username string) error {
    picoclawDir := filepath.Join(user.UserDir(cfg, username), ".picoclaw")

    configJSONPath := filepath.Join(picoclawDir, "config.json")
    securityYAMLPath := filepath.Join(picoclawDir, ".security.yml")

    configExists := false
    if _, err := os.Stat(configJSONPath); err == nil {
      configExists = true
    }
    securityExists := false
    if _, err := os.Stat(securityYAMLPath); err == nil {
      securityExists = true
    }

    if !configExists && !securityExists {
      return fmt.Errorf("用户 %s 的配置文件不存在，请先启动容器", username)
    }

    if configExists {
      if err := user.ApplyConfigToJSON(cfg, picoclawDir); err != nil {
        return fmt.Errorf("更新 config.json 失败: %w", err)
      }
    }

    if securityExists {
      if err := user.ApplySecurityToYAML(cfg, picoclawDir); err != nil {
        return fmt.Errorf("更新 .security.yml 失败: %w", err)
      }
    }

    fmt.Printf("  [应用配置] %s (config.json=%v, .security.yml=%v)\n",
      username, configExists, securityExists)
    return nil
  }

  if targetUser != "" {
    return applyFn(targetUser)
  }

  return user.ForEachUser(cfg, applyFn)
}
