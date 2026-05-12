package auth

import (
  "fmt"
  "strconv"
  "strings"
  "time"
)

// ============================================================
// 容器记录管理
// ============================================================

// UpsertContainer 插入或更新容器记录
func UpsertContainer(rec *ContainerRecord) error {
  if err := ensureDB(); err != nil {
    return err
  }
  // SQLite 的 ON CONFLICT 语句需要原始 SQL
  _, err := engine.Exec(`INSERT INTO containers (username, container_id, image, status, ip, cpu_limit, memory_limit)
    VALUES (?, ?, ?, ?, ?, ?, ?)
    ON CONFLICT(username) DO UPDATE SET
      container_id = excluded.container_id,
      image = excluded.image,
      status = excluded.status,
      ip = excluded.ip,
      cpu_limit = excluded.cpu_limit,
      memory_limit = excluded.memory_limit,
      updated_at = datetime('now','localtime')`,
    rec.Username, rec.ContainerID, rec.Image, rec.Status, rec.IP, rec.CPULimit, rec.MemoryLimit)
  return err
}

// GetContainerByUsername 按用户名查询容器记录
func GetContainerByUsername(username string) (*ContainerRecord, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var rec ContainerRecord
  has, err := engine.Where("username = ?", username).Get(&rec)
  if err != nil {
    return nil, err
  }
  if !has {
    return nil, nil
  }
  return &rec, nil
}

// GetAllContainers 返回所有容器记录
func GetAllContainers() ([]ContainerRecord, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var list []ContainerRecord
  err := engine.OrderBy("id").Find(&list)
  if err != nil {
    return nil, err
  }
  return list, nil
}

// DeleteContainer 删除容器记录
func DeleteContainer(username string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Where("username = ?", username).Delete(&ContainerRecord{})
  return err
}

// UpdateContainerStatus 更新容器状态
func UpdateContainerStatus(username, status string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Where("username = ?", username).
    Cols("status", "updated_at").
    Update(&ContainerRecord{Status: status, UpdatedAt: time.Now().Format("2006-01-02 15:04:05")})
  return err
}

// UpdateContainerID 更新 Docker 容器 ID
func UpdateContainerID(username, containerID string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Where("username = ?", username).
    Cols("container_id", "updated_at").
    Update(&ContainerRecord{ContainerID: containerID, UpdatedAt: time.Now().Format("2006-01-02 15:04:05")})
  return err
}

// UpdateContainerImage 更新用户容器镜像引用
func UpdateContainerImage(username, imageRef string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Where("username = ?", username).
    Cols("image", "updated_at").
    Update(&ContainerRecord{Image: imageRef, UpdatedAt: time.Now().Format("2006-01-02 15:04:05")})
  return err
}

// UpsertUserChannelStatus 写入用户渠道可见性和启用状态。
func UpsertUserChannelStatus(username, channel string, allowed, enabled, configured bool, configVersion int) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Exec(`INSERT INTO user_channels (username, channel, allowed, enabled, configured, config_version, updated_at)
    VALUES (?, ?, ?, ?, ?, ?, datetime('now','localtime'))
    ON CONFLICT(username, channel) DO UPDATE SET
      allowed = excluded.allowed,
      enabled = excluded.enabled,
      configured = excluded.configured,
      config_version = excluded.config_version,
      updated_at = datetime('now','localtime')`,
    username, channel, boolInt(allowed), boolInt(enabled), boolInt(configured), configVersion)
  return err
}

// GetUserChannelStatus 返回用户渠道状态记录，不存在时返回 nil。
func GetUserChannelStatus(username, channel string) (*UserChannel, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var rec UserChannel
  has, err := engine.Where("username = ? AND channel = ?", username, channel).Get(&rec)
  if err != nil {
    return nil, err
  }
  if !has {
    return nil, nil
  }
  return &rec, nil
}

func boolInt(value bool) int {
  if value {
    return 1
  }
  return 0
}

// AllocateNextIP 分配下一个可用 IP（100.64.0.2 起）
func AllocateNextIP() (string, error) {
  if err := ensureDB(); err != nil {
    return "", err
  }
  var rec ContainerRecord
  has, _ := engine.Where("ip IS NOT NULL AND ip != ''").OrderBy("id DESC").Limit(1).Get(&rec)
  if !has || rec.IP == "" {
    return "100.64.0.2", nil
  }
  // 解析最后一段递增
  parts := strings.SplitN(rec.IP, ".", 4)
  if len(parts) != 4 {
    return "100.64.0.2", nil
  }
  last, err := strconv.Atoi(parts[3])
  if err != nil {
    return "100.64.0.2", nil
  }
  last++
  return fmt.Sprintf("%s.%s.%s.%d", parts[0], parts[1], parts[2], last), nil
}
