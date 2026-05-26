package web

import (
  "encoding/json"
  "fmt"
  "net/http"

  "github.com/gin-gonic/gin"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/im"
)

// ============================================================
// 通讯渠道管理 Handler
// ============================================================

// channelDef 渠道定义
type channelDef struct {
  Key    string        `json:"key"`
  Label  string        `json:"label"`
  Fields []channelField `json:"fields,omitempty"`
}

// channelField 渠道配置字段定义
type channelField struct {
  Key     string `json:"key"`
  Label   string `json:"label"`
  Type    string `json:"type"`
  Hint    string `json:"hint,omitempty"`
}

var channelDefs = []channelDef{
  {
    Key:   "dingtalk",
    Label: "钉钉",
    Fields: []channelField{
      {Key: "client_id", Label: "Client ID", Type: "text", Hint: "钉钉应用的 AppKey"},
      {Key: "client_secret", Label: "Client Secret", Type: "password", Hint: "钉钉应用的 AppSecret"},
      {Key: "stream", Label: "分段回复", Type: "boolean", Hint: "开启后每步执行结果实时推送"},
    },
  },
  {
    Key:   "feishu",
    Label: "飞书",
    Fields: []channelField{
      {Key: "app_id", Label: "App ID", Type: "text", Hint: "飞书应用的 App ID"},
      {Key: "app_secret", Label: "App Secret", Type: "password", Hint: "飞书应用的 App Secret"},
      {Key: "stream", Label: "分段回复", Type: "boolean", Hint: "开启后每步执行结果实时推送"},
    },
  },
  {
    Key:   "wecom",
    Label: "企业微信",
    Fields: []channelField{
      {Key: "bot_id", Label: "Bot ID", Type: "text", Hint: "企业微信机器人的 Bot ID"},
      {Key: "secret", Label: "Secret", Type: "password", Hint: "企业微信机器人的 Secret"},
      {Key: "stream", Label: "分段回复", Type: "boolean", Hint: "开启后每步执行结果实时推送"},
    },
  },
}

var channelKeyMap = func() map[string]channelDef {
  m := make(map[string]channelDef, len(channelDefs))
  for _, d := range channelDefs {
    m[d.Key] = d
  }
  return m
}()

// channelEnabled 检查渠道是否在系统设置中开启
func channelEnabled(chKey string) bool {
  engine, err := auth.GetEngine()
  if err != nil {
    return false
  }
  var s auth.Setting
  has, _ := engine.Where("key = ? AND value = 'true'", "channel."+chKey+".enabled").Get(&s)
  return has
}

// userChannelCreds 从 user_channels 表获取用户渠道凭据（JSON map）
func userChannelCreds(username, channel string) map[string]string {
  uc, err := auth.GetUserChannel(username, channel)
  if err != nil || uc == nil || uc.Credentials == "" {
    return nil
  }
  var creds map[string]string
  if json.Unmarshal([]byte(uc.Credentials), &creds) != nil {
    return nil
  }
  return creds
}

// ============================================================
// 管理员：获取渠道列表
// GET /api/admin/channels
// ============================================================

func (s *Server) handleAdminChannelsGet(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  items := make([]channelDef, 0)
  for _, d := range channelDefs {
    items = append(items, d)
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success":  true,
    "channels": items,
  })
}

// ============================================================
// 用户：获取可用渠道列表
// GET /api/channels
// ============================================================

func (s *Server) handleUserChannelsGet(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  userChannels, _ := auth.ListUserChannelByUsername(username)
  userChanMap := make(map[string]*auth.UserChannel)
  for i := range userChannels {
    userChanMap[userChannels[i].Channel] = &userChannels[i]
  }

  items := make([]map[string]interface{}, 0)
  for _, d := range channelDefs {
    if !channelEnabled(d.Key) {
      continue
    }

    uc, hasUC := userChanMap[d.Key]
    creds := userChannelCreds(username, d.Key)
    configured := creds != nil
    enabled := false
    if hasUC {
      enabled = uc.Enabled
      if uc.Configured {
        configured = true
      }
    }

    items = append(items, map[string]interface{}{
      "key":        d.Key,
      "label":      d.Label,
      "enabled":    enabled,
      "configured": configured,
    })
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success":  true,
    "channels": items,
  })
}

// ============================================================
// 用户：获取渠道配置字段
// GET /api/channels/config-fields?section=<channel>
// ============================================================

func (s *Server) handleChannelConfigFieldsGet(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  section := c.Query("section")
  if section == "" {
    writeError(c, http.StatusBadRequest, "缺少 section 参数")
    return
  }

  def, ok := channelKeyMap[section]
  if !ok {
    writeError(c, http.StatusNotFound, fmt.Sprintf("未知渠道: %s", section))
    return
  }

  if !channelEnabled(section) {
    writeError(c, http.StatusForbidden, "该渠道未开放")
    return
  }

  creds := userChannelCreds(username, section)

  fields := make([]map[string]interface{}, 0)
  for _, f := range def.Fields {
    value := ""
    if creds != nil {
      if v, ok := creds[f.Key]; ok {
        value = v
      }
    }
    fields = append(fields, map[string]interface{}{
      "field": map[string]interface{}{
        "key":   f.Key,
        "label": f.Label,
        "type":  f.Type,
        "hint":  f.Hint,
      },
      "value": value,
    })
  }

  userChan, _ := auth.GetUserChannel(username, section)
  enabled := userChan != nil && userChan.Enabled

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "fields":  fields,
    "enabled": enabled,
  })
}

// ============================================================
// 用户：保存渠道配置
// POST /api/channels/config-fields
// ============================================================

func (s *Server) handleChannelConfigFieldsSave(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  section := c.PostForm("section")
  if section == "" {
    writeError(c, http.StatusBadRequest, "缺少 section 参数")
    return
  }

  def, ok := channelKeyMap[section]
  if !ok {
    writeError(c, http.StatusNotFound, fmt.Sprintf("未知渠道: %s", section))
    return
  }

  if !channelEnabled(section) {
    writeError(c, http.StatusForbidden, "该渠道未开放")
    return
  }

  valuesStr := c.PostForm("values")
  var values map[string]interface{}
  if valuesStr != "" {
    if err := json.Unmarshal([]byte(valuesStr), &values); err != nil {
      writeError(c, http.StatusBadRequest, "values 格式错误")
      return
    }
  }

  // 检查用户是否启用该渠道
  enabled := false
  if v, ok := values["enabled"]; ok {
    switch vv := v.(type) {
    case bool:
      enabled = vv
    case string:
      enabled = vv == "true"
    }
  }

  // 收集凭据（只保存定义的字段）
  creds := make(map[string]string)
  for _, f := range def.Fields {
    if v, ok := values[f.Key]; ok {
      switch vv := v.(type) {
      case string:
        creds[f.Key] = vv
      default:
        bs, _ := json.Marshal(v)
        creds[f.Key] = string(bs)
      }
    }
  }

  credsJSON, _ := json.Marshal(creds)
  configured := len(creds) > 0
  for _, f := range def.Fields {
    if creds[f.Key] == "" {
      configured = false
      break
    }
  }

  // 没有传 enabled 时，从现有记录读取
  if values["enabled"] == nil {
    existing, _ := auth.GetUserChannel(username, section)
    if existing != nil {
      enabled = existing.Enabled
    }
  }

  if err := auth.UpsertUserChannelWithCreds(username, section, enabled, configured, string(credsJSON)); err != nil {
    writeError(c, http.StatusInternalServerError, fmt.Sprintf("保存渠道配置失败: %s", err.Error()))
    return
  }

  // 渠道保存后实时更新连接
  if s.agentIntegration != nil {
    dtProvider := s.agentIntegration.imGateway.GetProvider("dingtalk")
    if dt, ok := dtProvider.(*im.DingTalkProvider); ok {
      if section == "dingtalk" {
        if enabled && configured && creds["client_id"] != "" && creds["client_secret"] != "" {
          dt.AddUser(username, creds["client_id"], creds["client_secret"], creds["default_chat"])
        } else {
          dt.RemoveUser(username)
        }
      }
    }
    fsProvider := s.agentIntegration.imGateway.GetProvider("feishu")
    if fs, ok := fsProvider.(*im.FeishuProvider); ok {
      if section == "feishu" {
        if enabled && configured && creds["app_id"] != "" && creds["app_secret"] != "" {
          fs.AddUser(username, creds["app_id"], creds["app_secret"], creds["default_chat"])
        } else {
          fs.RemoveUser(username)
        }
      }
    }
    wcProvider := s.agentIntegration.imGateway.GetProvider("wecom")
    if wc, ok := wcProvider.(*im.WeComProvider); ok {
      if section == "wecom" {
        if enabled && configured && creds["bot_id"] != "" && creds["secret"] != "" {
          wc.AddUser(username, creds["bot_id"], creds["secret"], creds["default_chat"])
        } else {
          wc.RemoveUser(username)
        }
      }
    }
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": "渠道配置已保存",
  })
}
