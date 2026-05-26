package auth

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

// UpsertUserChannel 创建或更新用户渠道记录
func UpsertUserChannel(username, channel string, enabled, configured bool) error {
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
    uc.ConfigVersion++
    if _, err := engine.ID(uc.ID).Cols("enabled", "configured", "config_version", "updated_at").Update(&uc); err != nil {
      return fmt.Errorf("更新用户渠道失败: %w", err)
    }
  } else {
    uc = UserChannel{
      Username:      username,
      Channel:       channel,
      Allowed:       true,
      Enabled:       enabled,
      Configured:    configured,
      ConfigVersion: 1,
    }
    if _, err := engine.Insert(&uc); err != nil {
      return fmt.Errorf("创建用户渠道失败: %w", err)
    }
  }

  return nil
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
