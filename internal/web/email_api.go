package web

import (
  "net/http"
  "strconv"

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

// handleEmailSave 保存当前用户的邮件配置
// POST /api/user/email
func (s *Server) handleEmailSave(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  emailAddr := c.PostForm("email")
  smtpHost := c.PostForm("smtpHost")
  smtpPortStr := c.PostForm("smtpPort")
  smtpTlsStr := c.PostForm("smtpTls")
  imapHost := c.PostForm("imapHost")
  imapPortStr := c.PostForm("imapPort")
  imapTlsStr := c.PostForm("imapTls")
  loginUser := c.PostForm("loginUser")
  loginPassword := c.PostForm("loginPassword")

  if emailAddr == "" || smtpHost == "" || imapHost == "" || loginUser == "" || loginPassword == "" {
    writeError(c, http.StatusBadRequest, "所有字段均为必填")
    return
  }

  smtpPort := 587
  if smtpPortStr != "" {
    if p, err := strconv.Atoi(smtpPortStr); err == nil {
      smtpPort = p
    }
  }

  imapPort := 993
  if imapPortStr != "" {
    if p, err := strconv.Atoi(imapPortStr); err == nil {
      imapPort = p
    }
  }

  smtpTls := smtpTlsStr == "true"
  imapTls := imapTlsStr == "true"

  cfg := &email.Config{
    Email:     emailAddr,
    SMTPHost:  smtpHost,
    SMTPPort:  smtpPort,
    SMTPTLS:   smtpTls,
    IMAPHost:  imapHost,
    IMAPPort:  imapPort,
    IMAPTLS:   imapTls,
    LoginUser: loginUser,
    LoginPass: loginPassword,
  }

  smtpOK, _, testErr := email.TestConnection(cfg)
  if testErr != nil || !smtpOK {
    errMsg := "SMTP 连接测试失败"
    if testErr != nil {
      errMsg = testErr.Error()
    }
    writeError(c, http.StatusBadRequest, errMsg)
    return
  }

  ue := &store.UserEmail{
    Username:      username,
    Email:         emailAddr,
    SMTPHost:      smtpHost,
    SMTPPort:      smtpPort,
    SMTPTLS:       smtpTls,
    IMAPHost:      imapHost,
    IMAPPort:      imapPort,
    IMAPTLS:       imapTls,
    LoginUser:     loginUser,
    LoginPassword: loginPassword,
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

// handleEmailTest 测试当前用户的邮件连接
// POST /api/user/email/test
func (s *Server) handleEmailTest(c *gin.Context) {
  username := s.requireRegularUser(c)
  if username == "" {
    return
  }

  ue, err := store.GetUserEmailWithDecryptedPassword(username)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "查询邮件配置失败")
    return
  }

  if ue == nil {
    writeError(c, http.StatusBadRequest, "请先保存邮件配置")
    return
  }

  cfg := &email.Config{
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
