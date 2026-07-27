package web

import (
  "encoding/json"
  "fmt"
  "net/http"
  "github.com/gin-gonic/gin"

  "github.com/picoaide/picoaide/internal/im"
  "github.com/picoaide/picoaide/internal/store"
)

// ============================================================
// 通讯渠道管理 Handler
// ============================================================

type channelDef struct {
  Key      string         `json:"key"`
  Label    string         `json:"label"`
  GuideURL string         `json:"guide_url,omitempty"`
  Fields   []channelField `json:"fields,omitempty"`
}

type channelField struct {
  Key   string `json:"key"`
  Label string `json:"label"`
  Type  string `json:"type"`
  Hint  string `json:"hint,omitempty"`
}

var channelDefs = []channelDef{
  {
    Key:      "dingtalk",
    Label:    "钉钉",
    GuideURL: "https://open.dingtalk.com/document/development/build-dingtalk-ai-employees",
    Fields: []channelField{
      {Key: "client_id", Label: "Client ID", Type: "text", Hint: "钉钉应用的 AppKey"},
      {Key: "client_secret", Label: "Client Secret", Type: "password", Hint: "钉钉应用的 AppSecret"},
      {Key: "stream", Label: "分段回复", Type: "boolean", Hint: "开启后每步执行结果实时推送"},
    },
  },
  {
    Key:      "feishu",
    Label:    "飞书",
    GuideURL: "https://open.feishu.cn/document/home/develop-a-bot-in-5-minutes",
    Fields: []channelField{
      {Key: "app_id", Label: "App ID", Type: "text", Hint: "飞书应用的 App ID"},
      {Key: "app_secret", Label: "App Secret", Type: "password", Hint: "飞书应用的 App Secret"},
      {Key: "stream", Label: "分段回复", Type: "boolean", Hint: "开启后每步执行结果实时推送"},
    },
  },
  {
    Key:      "wechat",
    Label:    "微信",
    GuideURL: "https://developer.work.weixin.qq.com/document/path/101039",
    Fields: []channelField{
      {Key: "bot_id", Label: "Bot ID", Type: "text", Hint: "微信机器人的 Bot ID"},
      {Key: "secret", Label: "Secret", Type: "password", Hint: "微信机器人的 Secret"},
    },
  },
  {
    Key:      "wecom",
    Label:    "企业微信",
    GuideURL: "https://open.work.weixin.qq.com/help2/pc/21661",
    Fields: []channelField{
      {Key: "bot_id", Label: "Bot ID", Type: "text", Hint: "企业微信智能机器人的 Bot ID"},
      {Key: "secret", Label: "Secret", Type: "password", Hint: "企业微信智能机器人的 Secret"},
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

func channelEnabled(chKey string) bool {
  return store.GetChannelEnabled(chKey)
}

func userChannelCreds(username, channel string) map[string]string {
  uc, err := store.GetUserChannel(username, channel)
  if err != nil || uc == nil || uc.Credentials == "" {
    return nil
  }
  var creds map[string]string
  if json.Unmarshal([]byte(uc.Credentials), &creds) != nil {
    return nil
  }
  return creds
}

// disconnectChannelUser 断开用户在某个 provider 的连接
func disconnectChannelUser(ai *AgentIntegration, username, chKey string) {
  if ai == nil {
    return
  }
  p := ai.imGateway.GetProvider(chKey)
  if p == nil {
    return
  }
  switch pp := p.(type) {
  case *im.DingTalkProvider:
    pp.RemoveUser(username)
  case *im.FeishuProvider:
    pp.RemoveUser(username)
  case *im.WeChatProvider:
    pp.RemoveUser(username)
  case *im.WeComProvider:
    pp.RemoveUser(username)
  }
}

// addChannelUser 在某个 provider 上建立用户连接
func addChannelUser(ai *AgentIntegration, username, chKey string, creds map[string]string) {
  if ai == nil || creds == nil {
    return
  }
  p := ai.imGateway.GetProvider(chKey)
  if p == nil {
    return
  }
  switch pp := p.(type) {
  case *im.DingTalkProvider:
    pp.AddUser(username, creds["client_id"], creds["client_secret"], creds["default_chat"])
  case *im.FeishuProvider:
    pp.AddUser(username, creds["app_id"], creds["app_secret"], creds["default_chat"])
  case *im.WeChatProvider:
    pp.AddUser(username, creds["bot_id"], creds["secret"], creds["default_chat"])
  case *im.WeComProvider:
    pp.AddUser(username, creds["bot_id"], creds["secret"], creds["default_chat"])
  }
}

// ============================================================
// 管理员：获取渠道列表
// GET /api/admin/channels
// ============================================================

func (s *Server) handleAdminChannelsGet(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  items := make([]map[string]interface{}, 0, len(channelDefs))
  for _, d := range channelDefs {
    items = append(items, map[string]interface{}{
      "key":       d.Key,
      "label":     d.Label,
      "guide_url": d.GuideURL,
      "fields":    d.Fields,
      "enabled":   channelEnabled(d.Key),
    })
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success":  true,
    "channels": items,
  })
}

// ============================================================
// 管理员：切换系统级渠道启用状态
// POST /api/admin/channels/toggle
// ============================================================

func (s *Server) handleAdminChannelsToggle(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  var req struct {
    Section string `json:"section"`
    Enabled bool   `json:"enabled"`
  }
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "无效的请求参数")
    return
  }
  if _, ok := channelKeyMap[req.Section]; !ok {
    writeError(c, http.StatusNotFound, "未知渠道: "+req.Section)
    return
  }

  // 关闭时：断开该渠道所有已启用用户的连接
  if !req.Enabled && s.agentIntegration != nil {
    users, _ := store.ListEnabledUserChannelsByChannel(req.Section)
    for _, uc := range users {
      disconnectChannelUser(s.agentIntegration, uc.Username, req.Section)
    }
  }

  if err := store.SetChannelEnabled(req.Section, req.Enabled); err != nil {
    writeError(c, http.StatusInternalServerError, "设置渠道状态失败: "+err.Error())
    return
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": "渠道状态已更新",
  })
}

// ============================================================
// 管理员：获取用户渠道权限列表
// GET /api/admin/channels/users
// ============================================================

func (s *Server) handleAdminChannelsUsersGet(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  channelFilter := c.Query("channel")
  usernameFilter := c.Query("username")

  all, err := store.ListAllUserChannels()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "查询失败: "+err.Error())
    return
  }

  items := make([]map[string]interface{}, 0)
  seen := make(map[string]bool)
  for _, uc := range all {
    if channelFilter != "" && uc.Channel != channelFilter {
      continue
    }
    if usernameFilter != "" && uc.Username != usernameFilter {
      continue
    }
    key := uc.Username + ":" + uc.Channel
    if seen[key] {
      continue
    }
    seen[key] = true
    items = append(items, map[string]interface{}{
      "id":        uc.ID,
      "username":  uc.Username,
      "channel":   uc.Channel,
      "allowed":   uc.Allowed,
      "enabled":   uc.Enabled,
      "configured": uc.Configured,
    })
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "items":   items,
  })
}

// ============================================================
// 管理员：设置单条用户渠道权限
// POST /api/admin/channels/users/permission
// ============================================================

func (s *Server) handleAdminChannelsUsersPermission(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  var req struct {
    Username string `json:"username"`
    Channel  string `json:"channel"`
    Allowed  bool   `json:"allowed"`
  }
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "无效的请求参数")
    return
  }
  if req.Username == "" || req.Channel == "" {
    writeError(c, http.StatusBadRequest, "缺少参数")
    return
  }
  if _, ok := channelKeyMap[req.Channel]; !ok {
    writeError(c, http.StatusNotFound, "未知渠道: "+req.Channel)
    return
  }

  // 取消权限时断开连接
  if !req.Allowed && s.agentIntegration != nil {
    disconnectChannelUser(s.agentIntegration, req.Username, req.Channel)
  }

  if err := store.UpsertUserChannelSimple(req.Username, req.Channel, req.Allowed, false); err != nil {
    writeError(c, http.StatusInternalServerError, "设置权限失败: "+err.Error())
    return
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": "权限已更新",
  })
}

// ============================================================
// 管理员：批量设置用户渠道权限
// POST /api/admin/channels/users/permissions
// ============================================================

func (s *Server) handleAdminChannelsUsersPermissions(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  var req struct {
    Username string          `json:"username"`
    Channels map[string]bool `json:"channels"`
  }
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "无效的请求参数")
    return
  }
  if req.Username == "" || len(req.Channels) == 0 {
    writeError(c, http.StatusBadRequest, "缺少参数")
    return
  }

  // 获取用户当前权限
  oldChannels, _ := store.ListUserChannelByUsername(req.Username)
  oldMap := make(map[string]bool)
  for _, uc := range oldChannels {
    oldMap[uc.Channel] = uc.Allowed
  }

  for chKey, allowed := range req.Channels {
    if _, ok := channelKeyMap[chKey]; !ok {
      continue
    }

    oldAllowed, existed := oldMap[chKey]

    // 如果是取消权限且之前有权限→断连
    if existed && oldAllowed && !allowed && s.agentIntegration != nil {
      disconnectChannelUser(s.agentIntegration, req.Username, chKey)
    }

    if err := store.UpsertUserChannelSimple(req.Username, chKey, allowed, false); err != nil {
      writeError(c, http.StatusInternalServerError, fmt.Sprintf("设置渠道 %s 权限失败: %s", chKey, err.Error()))
      return
    }
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": "权限已更新",
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

  userChannels, _ := store.ListUserChannelByUsername(username)
  userChanMap := make(map[string]*store.UserChannel)
  for i := range userChannels {
    userChanMap[userChannels[i].Channel] = &userChannels[i]
  }

  items := make([]map[string]interface{}, 0)
  for _, d := range channelDefs {
    if !channelEnabled(d.Key) {
      continue
    }

    uc, hasUC := userChanMap[d.Key]
    if !hasUC || !uc.Allowed {
      continue
    }

    creds := userChannelCreds(username, d.Key)
    configured := uc.Configured || creds != nil

    items = append(items, map[string]interface{}{
      "key":        d.Key,
      "label":      d.Label,
      "enabled":    uc.Enabled,
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

  uc, _ := store.GetUserChannel(username, section)
  if uc == nil || !uc.Allowed {
    writeError(c, http.StatusForbidden, "您没有该渠道的访问权限")
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

  enabled := uc != nil && uc.Enabled

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success":   true,
    "fields":    fields,
    "enabled":   enabled,
    "guide_url": def.GuideURL,
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

  var req struct {
    Section string                 `json:"section"`
    Values  map[string]interface{} `json:"values"`
  }
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "无效的请求参数")
    return
  }
  section := req.Section
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

  existing, _ := store.GetUserChannel(username, section)
  if existing == nil || !existing.Allowed {
    writeError(c, http.StatusForbidden, "您没有该渠道的访问权限")
    return
  }

  values := req.Values
  if values == nil {
    values = make(map[string]interface{})
  }

  enabled := existing.Enabled
  if v, ok := values["enabled"]; ok {
    switch vv := v.(type) {
    case bool:
      enabled = vv
    case string:
      enabled = vv == "true"
    }
  }

  // 从现有凭据中保留非字段键（如 default_chat、webhook 等）
  creds := make(map[string]string)
  if existingCreds := userChannelCreds(username, section); existingCreds != nil {
    for k, v := range existingCreds {
      creds[k] = v
    }
  }
  // 用新提交的字段值覆写
  for _, f := range def.Fields {
    if v, ok := values[f.Key]; ok {
      switch vv := v.(type) {
      case string:
        creds[f.Key] = vv
      default:
        bs, _ := json.Marshal(v)
        creds[f.Key] = string(bs)
      }
    } else {
      // 字段未在请求中，保留旧值
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

  if err := store.UpsertUserChannelWithCreds(username, section, enabled, configured, string(credsJSON)); err != nil {
    writeError(c, http.StatusInternalServerError, fmt.Sprintf("保存渠道配置失败: %s", err.Error()))
    return
  }

  // 实时更新 IM 连接
  if enabled && configured {
    addChannelUser(s.agentIntegration, username, section, creds)
  } else {
    disconnectChannelUser(s.agentIntegration, username, section)
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": "渠道配置已保存",
  })
}
