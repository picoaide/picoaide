package store

import (
  "fmt"
)

// ============================================================
// 用户通讯渠道管理
// ============================================================

// ListUserChannelByUsername 获取用户的所有渠道记录
func ListUserChannelByUsername(username string) ([]UserChannel, error) {
  engine, err := GetEngine()
  if err != nil {
    return nil, fmt.Errorf("获取数据库引擎失败: %w", err)
  }

  var channels []UserChannel
  if err := engine.Where("username = ?", username).Find(&channels); err != nil {
    return nil, fmt.Errorf("查询用户渠道失败: %w", err)
  }
  return channels, nil
}

// ListAllUserChannels 获取所有用户的渠道记录（管理端用）
func ListAllUserChannels() ([]UserChannel, error) {
  engine, err := GetEngine()
  if err != nil {
    return nil, fmt.Errorf("获取数据库引擎失败: %w", err)
  }
  var channels []UserChannel
  if err := engine.Find(&channels); err != nil {
    return nil, fmt.Errorf("查询所有用户渠道失败: %w", err)
  }
  return channels, nil
}

// GetUserChannel 获取用户在指定渠道的记录
func GetUserChannel(username, channel string) (*UserChannel, error) {
  engine, err := GetEngine()
  if err != nil {
    return nil, fmt.Errorf("获取数据库引擎失败: %w", err)
  }

  var uc UserChannel
  has, err := engine.Where("username = ? AND channel = ?", username, channel).Get(&uc)
  if err != nil {
    return nil, fmt.Errorf("查询用户渠道失败: %w", err)
  }
  if !has {
    return nil, nil
  }
  return &uc, nil
}

// UpsertUserChannelWithCreds 创建或更新用户渠道记录（含凭据）
func UpsertUserChannelWithCreds(username, channel string, enabled, configured bool, credentials string) error {
  engine, err := GetEngine()
  if err != nil {
    return fmt.Errorf("获取数据库引擎失败: %w", err)
  }

  var uc UserChannel
  has, err := engine.Where("username = ? AND channel = ?", username, channel).Get(&uc)
  if err != nil {
    return fmt.Errorf("查询用户渠道失败: %w", err)
  }

  if has {
    uc.Enabled = enabled
    uc.Configured = configured
    uc.Credentials = credentials
    uc.ConfigVersion++
    if _, err := engine.ID(uc.ID).Cols("enabled", "configured", "credentials", "config_version", "updated_at").Update(&uc); err != nil {
      return fmt.Errorf("更新用户渠道失败: %w", err)
    }
  } else {
    uc = UserChannel{
      Username:      username,
      Channel:       channel,
      Allowed:       true,
      Enabled:       enabled,
      Configured:    configured,
      Credentials:   credentials,
      ConfigVersion: 1,
    }
    if _, err := engine.Insert(&uc); err != nil {
      return fmt.Errorf("创建用户渠道失败: %w", err)
    }
  }

  return nil
}

// UpsertUserChannelSimple 创建或更新用户渠道记录（仅启用/权限，不含凭据）
func UpsertUserChannelSimple(username, channel string, allowed, enabled bool) error {
  engine, err := GetEngine()
  if err != nil {
    return fmt.Errorf("获取数据库引擎失败: %w", err)
  }

  var uc UserChannel
  has, err := engine.Where("username = ? AND channel = ?", username, channel).Get(&uc)
  if err != nil {
    return fmt.Errorf("查询用户渠道失败: %w", err)
  }

  if has {
    uc.Allowed = allowed
    uc.Enabled = enabled
    uc.ConfigVersion++
    if _, err := engine.ID(uc.ID).Cols("allowed", "enabled", "config_version", "updated_at").Update(&uc); err != nil {
      return fmt.Errorf("更新用户渠道失败: %w", err)
    }
  } else {
    uc = UserChannel{
      Username:      username,
      Channel:       channel,
      Allowed:       allowed,
      Enabled:       enabled,
      ConfigVersion: 1,
    }
    if _, err := engine.Insert(&uc); err != nil {
      return fmt.Errorf("创建用户渠道失败: %w", err)
    }
  }

  return nil
}

// ============================================================
// 渠道系统级开关（存储在 settings 表）
// ============================================================

const channelEnabledKey = "channel.%s.enabled"

// setSettingValue 写入 settings 表的单个键值
func setSettingValue(key, value string) error {
  engine, err := GetEngine()
  if err != nil {
    return fmt.Errorf("获取数据库引擎失败: %w", err)
  }
  _, err = engine.Exec(
    `INSERT OR REPLACE INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now','localtime'))`,
    key, value,
  )
  return err
}

// getSettingValue 读取 settings 表的单个键值
func getSettingValue(key string) string {
  engine, err := GetEngine()
  if err != nil {
    return ""
  }
  var s Setting
  has, _ := engine.Where("key = ?", key).Get(&s)
  if !has {
    return ""
  }
  return s.Value
}

// SetChannelEnabled 设置渠道系统级启用状态
func SetChannelEnabled(chKey string, enabled bool) error {
  v := "false"
  if enabled {
    v = "true"
  }
  return setSettingValue(fmt.Sprintf(channelEnabledKey, chKey), v)
}

// GetChannelEnabled 读取渠道系统级启用状态
func GetChannelEnabled(chKey string) bool {
  return getSettingValue(fmt.Sprintf(channelEnabledKey, chKey)) == "true"
}

// ListEnabledUserChannelsByChannel 查询某渠道下所有已启用的用户
func ListEnabledUserChannelsByChannel(channel string) ([]UserChannel, error) {
  engine, err := GetEngine()
  if err != nil {
    return nil, fmt.Errorf("获取数据库引擎失败: %w", err)
  }
  var channels []UserChannel
  if err := engine.Where("channel = ? AND enabled = ? AND allowed = ?", channel, true, true).Find(&channels); err != nil {
    return nil, fmt.Errorf("查询渠道已启用用户失败: %w", err)
  }
  return channels, nil
}

// ListConfiguredUserChannelsByChannel 查询某渠道下所有已配置且启用的用户
func ListConfiguredUserChannelsByChannel(channel string) ([]UserChannel, error) {
  engine, err := GetEngine()
  if err != nil {
    return nil, fmt.Errorf("获取数据库引擎失败: %w", err)
  }
  var channels []UserChannel
  if err := engine.Where("channel = ? AND configured = ? AND enabled = ? AND allowed = ?", channel, true, true, true).Find(&channels); err != nil {
    return nil, fmt.Errorf("查询渠道已配置用户失败: %w", err)
  }
  return channels, nil
}

// ============================================================
// 用户 IM 外部身份绑定
// ============================================================

// GetUserIMBinding 获取用户在指定平台的绑定记录
func GetUserIMBinding(username, platform string) (*UserIMBinding, error) {
  engine, err := GetEngine()
  if err != nil {
    return nil, err
  }
  var b UserIMBinding
  has, err := engine.Where("username = ? AND platform = ?", username, platform).Get(&b)
  if err != nil {
    return nil, err
  }
  if !has {
    return nil, nil
  }
  return &b, nil
}

// FindUserByExternalID 根据平台外部用户 ID 查找 PicoAide 用户名
func FindUserByExternalID(platform, externalUserID string) (string, error) {
  engine, err := GetEngine()
  if err != nil {
    return "", err
  }
  var b UserIMBinding
  has, err := engine.Where("platform = ? AND external_user_id = ?", platform, externalUserID).Get(&b)
  if err != nil {
    return "", err
  }
  if !has {
    return "", nil
  }
  return b.Username, nil
}

// UpsertUserIMBinding 创建或更新用户 IM 绑定
func UpsertUserIMBinding(username, platform, externalUserID, externalChatID string) error {
  engine, err := GetEngine()
  if err != nil {
    return err
  }
  var b UserIMBinding
  has, err := engine.Where("username = ? AND platform = ?", username, platform).Get(&b)
  if err != nil {
    return err
  }
  if has {
    b.ExternalUserID = externalUserID
    b.ExternalChatID = externalChatID
    _, err = engine.ID(b.ID).Cols("external_user_id", "external_chat_id", "updated_at").Update(&b)
  } else {
    _, err = engine.Insert(&UserIMBinding{
      Username:       username,
      Platform:       platform,
      ExternalUserID: externalUserID,
      ExternalChatID: externalChatID,
    })
  }
  return err
}

// DeleteUserIMBinding 删除用户 IM 绑定
func DeleteUserIMBinding(username, platform string) error {
  engine, err := GetEngine()
  if err != nil {
    return err
  }
  _, err = engine.Where("username = ? AND platform = ?", username, platform).Delete(&UserIMBinding{})
  return err
}
