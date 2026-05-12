package web

import (
  "context"
  "fmt"
  "io"
  "net/http"
  "time"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  dockerpkg "github.com/picoaide/picoaide/internal/docker"
  "github.com/picoaide/picoaide/internal/user"
)

// ============================================================
// 配置管理 & 迁移规则 & 异步任务 & 容器日志
// ============================================================

// handleAdminConfigApply 下发配置到指定用户/组/全部用户并重启容器
func (s *Server) handleAdminConfigApply(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "POST" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  username := c.PostForm("username")
  group := c.PostForm("group")

  var targets []string
  var err error

  switch {
  case username != "":
    if err := user.ValidateUsername(username); err != nil {
      writeError(c, http.StatusBadRequest, err.Error())
      return
    }
    targets = []string{username}
  case group != "":
    targets, err = auth.GetGroupMembersForDeploy(group)
    if err != nil {
      writeError(c, http.StatusBadRequest, "获取组成员失败: "+err.Error())
      return
    }
    if len(targets) == 0 {
      writeError(c, http.StatusBadRequest, "组 "+group+" 没有成员")
      return
    }
  default:
    // 不指定用户和组时，下发到所有用户
    targets, err = user.GetUserList(s.cfg)
    if err != nil {
      writeError(c, http.StatusInternalServerError, "获取用户列表失败: "+err.Error())
      return
    }
  }

  // 单个用户直接同步执行
  if len(targets) == 1 {
    if err := s.applyConfigToUser(targets[0]); err != nil {
      writeError(c, http.StatusInternalServerError, err.Error())
    } else {
      writeSuccess(c, "配置已下发并重启")
    }
    return
  }

  // 多个用户走队列
  applyFn := func(u string) error {
    return s.applyConfigToUser(u)
  }
  taskID, err := enqueueTask("config-apply", targets, applyFn)
  if err != nil {
    writeError(c, http.StatusConflict, err.Error())
    return
  }
  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": fmt.Sprintf("已提交配置下发任务，共 %d 个用户", len(targets)),
    "task_id": taskID,
  })
}

func (s *Server) handleAdminMigrationRulesRefresh(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "POST" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }
  if err := user.RefreshPicoClawMigrationRulesFromAdapter(config.RuleCacheDir(), config.PicoClawAdapterRemoteBaseURL()); err != nil {
    writeError(c, http.StatusBadGateway, "更新迁移规则失败: "+err.Error())
    return
  }
  info, err := user.LoadPicoClawMigrationRulesInfo(config.RuleCacheDir())
  if err != nil {
    writeError(c, http.StatusInternalServerError, "迁移规则已更新，但读取本地规则失败: "+err.Error())
    return
  }
  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": "迁移规则已更新",
    "rules":   info,
  })
}

func (s *Server) handleAdminMigrationRulesGet(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "GET" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }
  info, err := user.LoadPicoClawMigrationRulesInfo(config.RuleCacheDir())
  if err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }
  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success": true,
    "rules":   info,
  })
}

func (s *Server) handleAdminMigrationRulesUpload(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "POST" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  if file, _, err := c.Request.FormFile("file"); err == nil {
    defer file.Close()
    data, err := io.ReadAll(io.LimitReader(file, 16<<20))
    if err != nil {
      writeError(c, http.StatusBadRequest, "读取上传文件失败: "+err.Error())
      return
    }
    if _, err := user.SavePicoClawAdapterZip(config.RuleCacheDir(), data); err != nil {
      writeError(c, http.StatusBadRequest, "配置适配包校验失败: "+err.Error())
      return
    }
    info, err := user.LoadPicoClawMigrationRulesInfo(config.RuleCacheDir())
    if err != nil {
      writeError(c, http.StatusInternalServerError, "配置适配包已导入，但读取本地规则失败: "+err.Error())
      return
    }
    writeJSON(c, http.StatusOK, map[string]interface{}{
      "success": true,
      "message": "配置适配包已导入",
      "rules":   info,
    })
    return
  }
  writeError(c, http.StatusBadRequest, "请上传配置适配 zip 包")
}

// handleAdminTaskStatus 返回当前任务队列状态
func (s *Server) handleAdminTaskStatus(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "GET" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }
  status := getTaskStatus()
  writeJSON(c, http.StatusOK, status)
}

// handleAdminContainerLogs 返回容器日志
func (s *Server) handleAdminContainerLogs(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if c.Request.Method != "GET" {
    writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }
  if !s.dockerAvailable {
    writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用")
    return
  }

  username := c.Query("username")
  if username == "" {
    writeError(c, http.StatusBadRequest, "用户名不能为空")
    return
  }
  if err := user.ValidateUsername(username); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  rec, err := auth.GetContainerByUsername(username)
  if err != nil || rec == nil {
    writeError(c, http.StatusBadRequest, "用户 "+username+" 未初始化")
    return
  }
  if rec.ContainerID == "" {
    writeError(c, http.StatusBadRequest, "容器未创建")
    return
  }

  tail := c.Query("tail")

  ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
  defer cancel()

  logs, err := dockerpkg.ContainerLogs(ctx, rec.ContainerID, tail)
  if err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }

  writeJSON(c, http.StatusOK, struct {
    Success  bool   `json:"success"`
    Username string `json:"username"`
    Logs     string `json:"logs"`
  }{
    Success:  true,
    Username: username,
    Logs:     logs,
  })
}
