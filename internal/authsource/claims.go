package authsource

import "strings"

func claimString(claims map[string]interface{}, key string) string {
  if key == "" {
    return ""
  }
  switch v := claims[key].(type) {
  case string:
    return strings.TrimSpace(v)
  default:
    return ""
  }
}

func claimStrings(claims map[string]interface{}, key string) []string {
  switch v := claims[key].(type) {
  case []interface{}:
    result := make([]string, 0, len(v))
    for _, item := range v {
      if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
        result = append(result, strings.TrimSpace(s))
      }
    }
    return result
  case []string:
    result := make([]string, 0, len(v))
    for _, item := range v {
      if item = strings.TrimSpace(item); item != "" {
        result = append(result, item)
      }
    }
    return result
  case string:
    fields := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' })
    result := make([]string, 0, len(fields))
    for _, item := range fields {
      if item = strings.TrimSpace(item); item != "" {
        result = append(result, item)
      }
    }
    return result
  default:
    return nil
  }
}

func firstNonEmpty(values ...string) string {
  for _, value := range values {
    if strings.TrimSpace(value) != "" {
      return strings.TrimSpace(value)
    }
  }
  return ""
}
