package web

import (
  "encoding/json"
  "fmt"
  "strconv"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/email"
  "github.com/picoaide/picoaide/internal/store"
)

var emailToolDefs = []ToolDef{
  {
    Name:        "send",
    Description: "发送邮件。注意：正文使用 Markdown 格式编写，系统会自动转换为 HTML 发送。请善用标题、列表、表格等使内容清晰可读。",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "to":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "收件人地址列表"},
        "subject": map[string]interface{}{"type": "string", "description": "邮件主题，应简洁明确"},
        "body":    map[string]interface{}{"type": "string", "description": "邮件正文，使用 Markdown 格式编写（系统自动转为 HTML），善用标题/列表/表格使内容清晰可读"},
        "content": map[string]interface{}{"type": "string", "description": "邮件正文，使用 Markdown 格式编写（系统自动转为 HTML），与 body 字段等效"},
        "cc":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "抄送地址列表"},
        "bcc":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "密送地址列表"},
      },
      "required": []string{"to", "subject", "body"},
    },
  },
  {
    Name:        "list",
    Description: "列出指定文件夹中的邮件。",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "folder": map[string]interface{}{"type": "string", "description": "文件夹名称", "default": "INBOX", "enum": []string{"INBOX", "SENT", "DRAFTS", "TRASH", "SPAM"}},
        "limit":  map[string]interface{}{"type": "integer", "description": "返回数量上限", "default": 20, "maximum": 100},
        "offset": map[string]interface{}{"type": "integer", "description": "偏移量", "default": 0},
      },
    },
  },
  {
    Name:        "read",
    Description: "读取指定邮件的完整内容。",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "uid":      map[string]interface{}{"type": "integer", "description": "邮件 UID"},
        "markSeen": map[string]interface{}{"type": "boolean", "description": "是否标记为已读", "default": true},
      },
      "required": []string{"uid"},
    },
  },
  {
    Name:        "reply",
    Description: "回复指定邮件。注意：正文使用 Markdown 格式编写，系统会自动转换为 HTML 发送。",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "uid":      map[string]interface{}{"type": "integer", "description": "要回复的邮件 UID"},
        "body":     map[string]interface{}{"type": "string", "description": "回复正文，使用 Markdown 格式编写（系统自动转为 HTML）"},
        "replyAll": map[string]interface{}{"type": "boolean", "description": "是否回复全部", "default": false},
      },
      "required": []string{"uid", "body"},
    },
  },
  {
    Name:        "forward",
    Description: "转发指定邮件。注意：正文使用 Markdown 格式编写，系统会自动转换为 HTML 发送。",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "uid":  map[string]interface{}{"type": "integer", "description": "要转发的邮件 UID"},
        "to":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "收件人地址列表"},
        "body": map[string]interface{}{"type": "string", "description": "附加正文，使用 Markdown 格式编写（系统自动转为 HTML）"},
      },
      "required": []string{"uid", "to"},
    },
  },
  {
    Name:        "search",
    Description: "搜索邮件（仅支持英文关键词，不支持中文）。",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "query":  map[string]interface{}{"type": "string", "description": "搜索关键词（仅支持英文）"},
        "folder": map[string]interface{}{"type": "string", "description": "文件夹名称", "default": "INBOX"},
        "limit":  map[string]interface{}{"type": "integer", "description": "返回数量上限", "default": 50},
      },
      "required": []string{"query"},
    },
  },
  {
    Name:        "delete",
    Description: "删除指定邮件。",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "uid":  map[string]interface{}{"type": "integer", "description": "要删除的邮件 UID"},
        "hard": map[string]interface{}{"type": "boolean", "description": "是否永久删除（跳过回收站）", "default": false},
      },
      "required": []string{"uid"},
    },
  },
  {
    Name:        "move",
    Description: "移动邮件到指定文件夹。",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "uid":    map[string]interface{}{"type": "integer", "description": "要移动的邮件 UID"},
        "folder": map[string]interface{}{"type": "string", "description": "目标文件夹名称"},
      },
      "required": []string{"uid", "folder"},
    },
  },
  {
    Name:        "folders",
    Description: "列出所有邮件文件夹及其未读计数。",
    InputSchema: map[string]interface{}{
      "type":       "object",
      "properties": map[string]interface{}{},
    },
  },
}

var emailHandlers = map[string]func(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string){
  "send":    handleEmailSend,
  "list":    handleEmailList,
  "read":    handleEmailRead,
  "reply":   handleEmailReply,
  "forward": handleEmailForward,
  "search":  handleEmailSearch,
  "delete":  handleEmailDelete,
  "move":    handleEmailMove,
  "folders": handleEmailFolders,
}

func emailHandleMCPToolCall(s *Server, c *gin.Context, id json.Number, name string, args map[string]interface{}, username string) {
  handler, ok := emailHandlers[name]
  if !ok {
    writeMCPError(c, id, "未知邮件工具: "+name)
    return
  }
  handler(s, c, id, args, username)
}

func getEmailConfig(username string) (*email.Config, error) {
  ue, err := store.GetUserEmailWithDecryptedPassword(username)
  if err != nil {
    return nil, fmt.Errorf("获取邮件配置失败: %w", err)
  }
  if ue == nil {
    return nil, fmt.Errorf("未配置邮件账户，请在设置中配置后再使用邮件工具")
  }
  return &email.Config{
    Email:     ue.Email,
    SMTPHost:  ue.SMTPHost,
    SMTPPort:  ue.SMTPPort,
    SMTPTLS:   ue.SMTPTLS,
    IMAPHost:  ue.IMAPHost,
    IMAPPort:  ue.IMAPPort,
    IMAPTLS:   ue.IMAPTLS,
    LoginUser: ue.LoginUser,
    LoginPass: ue.LoginPassword,
  }, nil
}

func writeMCPError(c *gin.Context, id json.Number, text string) {
  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": text},
    },
    "isError": true,
  })
}

func toStringSlice(v interface{}) []string {
  if v == nil {
    return nil
  }
  raw, ok := v.([]interface{})
  if !ok {
    return nil
  }
  result := make([]string, 0, len(raw))
  for _, item := range raw {
    s, ok := item.(string)
    if ok {
      result = append(result, s)
    }
  }
  return result
}

func parseIntArg(v interface{}, defaultVal int) int {
  if v == nil {
    return defaultVal
  }
  switch val := v.(type) {
  case float64:
    return int(val)
  case int:
    return val
  case int64:
    return int(val)
  case string:
    if n, err := strconv.Atoi(val); err == nil {
      return n
    }
  }
  return defaultVal
}

// ============================================================
// 工具 Handler 实现
// ============================================================

func handleEmailSend(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPError(c, id, err.Error())
    return
  }

  to := toStringSlice(args["to"])
  subject, _ := args["subject"].(string)
  body, _ := args["body"].(string)
  if body == "" {
    body, _ = args["content"].(string)
  }
  cc := toStringSlice(args["cc"])
  bcc := toStringSlice(args["bcc"])

  if len(to) == 0 {
    writeMCPError(c, id, "收件人列表不能为空")
    return
  }
  if subject == "" {
    writeMCPError(c, id, "邮件主题不能为空")
    return
  }

  msg := &email.OutgoingMessage{
    To:       to,
    Cc:       cc,
    Bcc:      bcc,
    Subject:  subject,
    BodyHTML: body,
  }

  messageID, err := email.SendMail(cfg, msg)
  if err != nil {
    writeMCPError(c, id, fmt.Sprintf("发送邮件失败: %s", err.Error()))
    return
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": fmt.Sprintf("邮件已发送，Message-ID: %s", messageID)},
    },
  })
}

func handleEmailList(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPError(c, id, err.Error())
    return
  }

  folder, _ := args["folder"].(string)
  if folder == "" {
    folder = "INBOX"
  }
  limit := parseIntArg(args["limit"], 20)
  if limit > 100 {
    limit = 100
  }
  offset := parseIntArg(args["offset"], 0)

  messages, total, err := email.ListMessages(cfg, folder, limit, offset)
  if err != nil {
    writeMCPError(c, id, fmt.Sprintf("列出邮件失败: %s", err.Error()))
    return
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": fmt.Sprintf("共 %d 封邮件（已显示 %d 封）", total, len(messages))},
    },
  })
}

func handleEmailRead(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPError(c, id, err.Error())
    return
  }

  uid := uint32(parseIntArg(args["uid"], 0))
  if uid == 0 {
    writeMCPError(c, id, "uid 必须为正整数")
    return
  }
  markSeen := true
  if v, ok := args["markSeen"].(bool); ok {
    markSeen = v
  }

  msg, err := email.FetchMessage(cfg, uid, markSeen)
  if err != nil {
    writeMCPError(c, id, fmt.Sprintf("读取邮件失败: %s", err.Error()))
    return
  }

  data, _ := json.Marshal(msg)
  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": string(data)},
    },
  })
}

func handleEmailReply(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPError(c, id, err.Error())
    return
  }

  uid := uint32(parseIntArg(args["uid"], 0))
  if uid == 0 {
    writeMCPError(c, id, "uid 必须为正整数")
    return
  }
  body, _ := args["body"].(string)
  replyAll := false
  if v, ok := args["replyAll"].(bool); ok {
    replyAll = v
  }

  messageID, err := email.Reply(cfg, uid, body, replyAll)
  if err != nil {
    writeMCPError(c, id, fmt.Sprintf("回复邮件失败: %s", err.Error()))
    return
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": fmt.Sprintf("回复已发送，Message-ID: %s", messageID)},
    },
  })
}

func handleEmailForward(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPError(c, id, err.Error())
    return
  }

  uid := uint32(parseIntArg(args["uid"], 0))
  if uid == 0 {
    writeMCPError(c, id, "uid 必须为正整数")
    return
  }
  to := toStringSlice(args["to"])
  if len(to) == 0 {
    writeMCPError(c, id, "收件人列表不能为空")
    return
  }
  body, _ := args["body"].(string)

  messageID, err := email.Forward(cfg, uid, to, body)
  if err != nil {
    writeMCPError(c, id, fmt.Sprintf("转发邮件失败: %s", err.Error()))
    return
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": fmt.Sprintf("邮件已转发，Message-ID: %s", messageID)},
    },
  })
}

func handleEmailSearch(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPError(c, id, err.Error())
    return
  }

  query, _ := args["query"].(string)
  if query == "" {
    writeMCPError(c, id, "搜索关键词不能为空")
    return
  }
  folder, _ := args["folder"].(string)
  if folder == "" {
    folder = "INBOX"
  }
  limit := parseIntArg(args["limit"], 50)

  messages, err := email.SearchMessages(cfg, folder, query, limit)
  if err != nil {
    writeMCPError(c, id, fmt.Sprintf("搜索邮件失败: %s", err.Error()))
    return
  }

  data, _ := json.Marshal(messages)
  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": string(data)},
    },
  })
}

func handleEmailDelete(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPError(c, id, err.Error())
    return
  }

  uid := uint32(parseIntArg(args["uid"], 0))
  if uid == 0 {
    writeMCPError(c, id, "uid 必须为正整数")
    return
  }
  hard := false
  if v, ok := args["hard"].(bool); ok {
    hard = v
  }

  if err := email.DeleteMessage(cfg, uid, hard); err != nil {
    writeMCPError(c, id, fmt.Sprintf("删除邮件失败: %s", err.Error()))
    return
  }

  action := "已移至回收站"
  if hard {
    action = "已永久删除"
  }
  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": fmt.Sprintf("邮件 %d %s", uid, action)},
    },
  })
}

func handleEmailMove(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPError(c, id, err.Error())
    return
  }

  uid := uint32(parseIntArg(args["uid"], 0))
  if uid == 0 {
    writeMCPError(c, id, "uid 必须为正整数")
    return
  }
  folder, _ := args["folder"].(string)
  if folder == "" {
    writeMCPError(c, id, "目标文件夹不能为空")
    return
  }

  if err := email.MoveMessage(cfg, uid, folder); err != nil {
    writeMCPError(c, id, fmt.Sprintf("移动邮件失败: %s", err.Error()))
    return
  }

  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": fmt.Sprintf("邮件 %d 已移至 %s", uid, folder)},
    },
  })
}

func handleEmailFolders(s *Server, c *gin.Context, id json.Number, args map[string]interface{}, username string) {
  cfg, err := getEmailConfig(username)
  if err != nil {
    writeMCPError(c, id, err.Error())
    return
  }

  folders, err := email.ListFolders(cfg)
  if err != nil {
    writeMCPError(c, id, fmt.Sprintf("列出文件夹失败: %s", err.Error()))
    return
  }

  data, _ := json.Marshal(folders)
  writeMCPResult(c.Writer, id, map[string]interface{}{
    "content": []map[string]interface{}{
      {"type": "text", "text": string(data)},
    },
  })
}
