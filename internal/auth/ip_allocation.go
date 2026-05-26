package auth

import (
  "fmt"
  "log/slog"
)

// AllocateIP 为指定用户分配一个 100.64.0.0/16 内的唯一 IP
// IP 直接存储在 local_users.ip 列，分配逻辑为顺序递增加一
func AllocateIP(username string) (string, error) {
  engine, err := GetEngine()
  if err != nil {
    return "", fmt.Errorf("获取数据库连接失败: %w", err)
  }

  // 检查是否已分配
  rows, err := engine.Query("SELECT ip FROM local_users WHERE username = ? LIMIT 1", username)
  if err != nil {
    return "", fmt.Errorf("查询用户 IP 失败: %w", err)
  }
  if len(rows) > 0 {
    ip := string(rows[0]["ip"])
    if ip != "" {
      return ip, nil
    }
  }

  // 查询当前最大 IP offset
  rows, err = engine.Query("SELECT ip FROM local_users WHERE ip != '' ORDER BY id DESC LIMIT 1")
  if err != nil {
    return "", fmt.Errorf("查询最大 IP 失败: %w", err)
  }

  var offset int64 = 1
  if len(rows) > 0 {
    ip := string(rows[0]["ip"])
    var a, b int64
    if n, _ := fmt.Sscanf(ip, "100.64.%d.%d", &a, &b); n == 2 {
      offset = a*256 + b
    }
  }

  // 跳过 .0.0 和 .0.1（网关），最多 65533 个 IP
  if offset < 1 {
    offset = 1
  }
  if offset >= 65533 {
    return "", fmt.Errorf("IP 地址池已耗尽")
  }

  next := offset + 1
  ip := fmt.Sprintf("100.64.%d.%d", next/256, next%256)

  // 写入用户记录
  _, err = engine.Exec("UPDATE local_users SET ip = ? WHERE username = ?", ip, username)
  if err != nil {
    return "", fmt.Errorf("保存 IP 失败: %w", err)
  }

  slog.Debug("IP 分配", "username", username, "ip", ip)
  return ip, nil
}

// ReleaseIP 释放用户占用的 IP（清空 local_users.ip）
func ReleaseIP(username string) error {
  engine, err := GetEngine()
  if err != nil {
    return fmt.Errorf("获取数据库连接失败: %w", err)
  }
  _, err = engine.Exec("UPDATE local_users SET ip = '' WHERE username = ?", username)
  if err != nil {
    return fmt.Errorf("释放 IP 失败: %w", err)
  }
  return nil
}
