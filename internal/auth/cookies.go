package auth

import (
  "fmt"
  "time"
)

// ============================================================
// Cookie CRUD
// ============================================================

// SetCookie 写入/覆盖某个域名的 Cookie（UPSERT）
func SetCookie(username, domain, cookies string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  now := time.Now().Format(time.RFC3339)

  // 先尝试 UPDATE
  affected, err := engine.Where("username = ? AND domain = ?", username, domain).
    Cols("cookies", "updated_at").
    Update(&UserCookie{Cookies: cookies, UpdatedAt: now})
  if err != nil {
    return fmt.Errorf("更新 Cookie 失败: %w", err)
  }
  if affected == 0 {
    _, err = engine.Insert(&UserCookie{
      Username:  username,
      Domain:    domain,
      Cookies:   cookies,
      UpdatedAt: now,
    })
    if err != nil {
      return fmt.Errorf("插入 Cookie 失败: %w", err)
    }
  }
  return nil
}

// GetCookie 读取某个域名的 Cookie
func GetCookie(username, domain string) (string, error) {
  if err := ensureDB(); err != nil {
    return "", err
  }
  var rec UserCookie
  has, err := engine.Where("username = ? AND domain = ?", username, domain).Get(&rec)
  if err != nil {
    return "", fmt.Errorf("查询 Cookie 失败: %w", err)
  }
  if !has {
    return "", nil
  }
  return rec.Cookies, nil
}

// GetAllCookies 读取用户所有域名的 Cookie
func GetAllCookies(username string) (map[string]string, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var records []UserCookie
  if err := engine.Where("username = ?", username).Find(&records); err != nil {
    return nil, fmt.Errorf("查询 Cookie 列表失败: %w", err)
  }
  result := make(map[string]string, len(records))
  for _, r := range records {
    result[r.Domain] = r.Cookies
  }
  return result, nil
}

// DeleteCookie 删除某个域名的 Cookie
func DeleteCookie(username, domain string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Where("username = ? AND domain = ?", username, domain).Delete(&UserCookie{})
  if err != nil {
    return fmt.Errorf("删除 Cookie 失败: %w", err)
  }
  return nil
}

// ListCookieDomains 列出用户所有已授权的域名及时间
func ListCookieDomains(username string) ([]CookieEntry, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var records []UserCookie
  if err := engine.Where("username = ?", username).Find(&records); err != nil {
    return nil, fmt.Errorf("查询 Cookie 列表失败: %w", err)
  }
  entries := make([]CookieEntry, len(records))
  for i, r := range records {
    entries[i] = CookieEntry{
      Domain:    r.Domain,
      Cookies:   r.Cookies,
      UpdatedAt: r.UpdatedAt,
    }
  }
  return entries, nil
}
