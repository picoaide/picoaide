package web

import (
  "net/http"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/email"
  "github.com/picoaide/picoaide/internal/store"
)

// handleEmailGet 获取当前用户的邮件配置
// GET /api/user/email
func (s *Server) handleEmailGet(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  ue, err := store.GetUserEmail(username)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "查询邮件配置失败")
    return
  }

  if ue == nil {
    writeJSON(c, http.StatusOK, map[string]interface{}{
      "success":     true,
      "configured":  false,
      "email":       nil,
    })
    return
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success":    true,
    "configured": true,
    "email": map[string]interface{}{
      "email":     ue.Email,
      "smtpHost":  ue.SMTPHost,
      "smtpPort":  ue.SMTPPort,
      "smtpTls":   ue.SMTPTLS,
      "imapHost":  ue.IMAPHost,
      "imapPort":  ue.IMAPPort,
      "imapTls":   ue.IMAPTLS,
      "loginUser": ue.LoginUser,
    },
  })
}

type emailConfigReq struct {
  Email         string `json:"email"`
  SMTPHost      string `json:"smtpHost"`
  SMTPPort      int    `json:"smtpPort"`
  SMTPTLS       bool   `json:"smtpTls"`
  IMAPHost      string `json:"imapHost"`
  IMAPPort      int    `json:"imapPort"`
  IMAPTLS       bool   `json:"imapTls"`
  LoginUser     string `json:"loginUser"`
  LoginPassword string `json:"loginPassword"`
}

// handleEmailSave 保存当前用户的邮件配置
// POST /api/user/email
func (s *Server) handleEmailSave(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  var req emailConfigReq
  if err := c.ShouldBindJSON(&req); err != nil {
    writeError(c, http.StatusBadRequest, "无效的请求参数")
    return
  }

  if req.Email == "" || req.SMTPHost == "" || req.IMAPHost == "" || req.LoginUser == "" || req.LoginPassword == "" {
    writeError(c, http.StatusBadRequest, "所有字段均为必填")
    return
  }

  smtpPort := req.SMTPPort
  if smtpPort == 0 {
    smtpPort = 587
  }

  imapPort := req.IMAPPort
  if imapPort == 0 {
    imapPort = 993
  }

  ue := &store.UserEmail{
    Username:      username,
    Email:         req.Email,
    SMTPHost:      req.SMTPHost,
    SMTPPort:      smtpPort,
    SMTPTLS:       req.SMTPTLS,
    IMAPHost:      req.IMAPHost,
    IMAPPort:      imapPort,
    IMAPTLS:       req.IMAPTLS,
    LoginUser:     req.LoginUser,
    LoginPassword: req.LoginPassword,
    Enabled:       true,
  }

  if err := store.UpsertUserEmail(ue); err != nil {
    writeError(c, http.StatusInternalServerError, "保存邮件配置失败")
    return
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": "邮件配置已保存",
  })
}

// handleEmailTest 测试邮件连接（优先用传入参数，否则用已保存的配置）
// POST /api/user/email/test
func (s *Server) handleEmailTest(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  var cfg *email.Config
  var req emailConfigReq

  if err := c.ShouldBindJSON(&req); err == nil && req.Email != "" && req.SMTPHost != "" && req.LoginUser != "" && req.LoginPassword != "" {
    smtpPort := req.SMTPPort
    if smtpPort == 0 {
      smtpPort = 587
    }
    imapPort := req.IMAPPort
    if imapPort == 0 {
      imapPort = 993
    }
    cfg = &email.Config{
      Email:     req.Email,
      SMTPHost:  req.SMTPHost,
      SMTPPort:  smtpPort,
      SMTPTLS:   req.SMTPTLS,
      IMAPHost:  req.IMAPHost,
      IMAPPort:  imapPort,
      IMAPTLS:   req.IMAPTLS,
      LoginUser: req.LoginUser,
      LoginPass: req.LoginPassword,
    }
  } else {
    ue, err := store.GetUserEmailWithDecryptedPassword(username)
    if err != nil {
      writeError(c, http.StatusInternalServerError, "查询邮件配置失败")
      return
    }
    if ue == nil {
      writeError(c, http.StatusBadRequest, "请先填写并保存邮箱配置")
      return
    }
    cfg = &email.Config{
      Email:     ue.Email,
      SMTPHost:  ue.SMTPHost,
      SMTPPort:  ue.SMTPPort,
      SMTPTLS:   ue.SMTPTLS,
      IMAPHost:  ue.IMAPHost,
      IMAPPort:  ue.IMAPPort,
      IMAPTLS:   ue.IMAPTLS,
      LoginUser: ue.LoginUser,
      LoginPass: ue.LoginPassword,
    }
  }

  smtpOK, imapOK, testErr := email.TestConnection(cfg)
  errMsg := ""
  if testErr != nil {
    errMsg = testErr.Error()
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "smtp":    smtpOK,
    "imap":    imapOK,
    "error":   errMsg,
  })
}

// handleEmailDelete 删除当前用户的邮件配置
// POST /api/user/email/delete
func (s *Server) handleEmailDelete(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  if err := store.DeleteUserEmail(username); err != nil {
    writeError(c, http.StatusInternalServerError, "删除邮件配置失败")
    return
  }

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": "邮件配置已删除",
  })
}
