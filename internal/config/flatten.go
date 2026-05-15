package config

import (
  "encoding/json"
  "fmt"
  "strconv"
  "strings"
)

// flattenConfig 将嵌套配置展平为点分隔的键值映射
// 规则：
//   - 字符串/数字/布尔值 → 直接存储为字符串
//   - 嵌套 map → 递归展平，键用点连接
//   - 切片/数组 → 序列化为 JSON 字符串，存储在父键下
//   - 特殊键 picoclaw、security、skills → 整体序列化为 JSON
func flattenConfig(data map[string]interface{}) map[string]string {
  result := make(map[string]string)
  flattenRecursive(data, "", result)
  return result
}

// flattenConfig 内部递归实现
func flattenRecursive(data map[string]interface{}, prefix string, result map[string]string) {
  // 需要整体存储为 JSON 的顶层键
  jsonBlobKeys := map[string]bool{
    "picoclaw": true,
    "security": true,
    "skills":   true,
  }

  for key, val := range data {
    fullKey := key
    if prefix != "" {
      fullKey = prefix + "." + key
    }

    // 顶层特殊键：整体序列化为 JSON
    if prefix == "" && jsonBlobKeys[key] {
      b, err := json.Marshal(val)
      if err == nil {
        result[key] = string(b)
      }
      continue
    }

    switch v := val.(type) {
    case map[string]interface{}:
      // 嵌套 map → 递归展平
      flattenRecursive(v, fullKey, result)
    case []interface{}:
      // 切片 → 序列化为 JSON
      b, err := json.Marshal(v)
      if err == nil {
        result[fullKey] = string(b)
      }
    case nil:
      result[fullKey] = ""
    default:
      // 字符串、数字、布尔值等 → 转为字符串
      result[fullKey] = fmt.Sprintf("%v", v)
    }
  }
}

// buildNested 将展平的键值映射重建为嵌套 JSON 结构
func buildNested(flat map[string]string) map[string]interface{} {
  // 需要从 JSON 反序列化的顶层键
  jsonBlobKeys := map[string]bool{
    "picoclaw": true,
    "security": true,
    "skills":   true,
  }

  // 需要作为 bool 返回的键
  boolKeys := map[string]bool{
    "web.ldap_enabled":       true,
    "web.tls.enabled":        true,
    "ldap.whitelist_enabled": true,
    "oidc.whitelist_enabled": true,
  }

  result := make(map[string]interface{})

  for key, value := range flat {
    // 特殊键直接从 JSON 解析
    if jsonBlobKeys[key] {
      var parsed interface{}
      if err := json.Unmarshal([]byte(value), &parsed); err == nil {
        result[key] = parsed
      }
      continue
    }

    // 类型转换
    var typedVal interface{} = value
    if boolKeys[key] {
      if b, err := strconv.ParseBool(value); err == nil {
        typedVal = b
      }
    } else if iv, err := strconv.ParseInt(value, 10, 64); err == nil && strconv.FormatInt(iv, 10) == value {
      typedVal = iv
    }

    // 按点分隔逐层构建嵌套 map
    parts := strings.Split(key, ".")
    current := result
    for i := 0; i < len(parts)-1; i++ {
      part := parts[i]
      if _, ok := current[part]; !ok {
        current[part] = make(map[string]interface{})
      }
      if m, ok := current[part].(map[string]interface{}); ok {
        current = m
      }
    }
    current[parts[len(parts)-1]] = typedVal
  }

  return result
}
