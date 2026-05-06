package web

import (
  "archive/zip"
  "context"
  "encoding/json"
  "fmt"
  "io"
  "log/slog"
  "net/http"
  "os"
  "os/exec"
  "path/filepath"
  "sort"
  "strconv"
  "strings"
  "sync"
  "time"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  dockerpkg "github.com/picoaide/picoaide/internal/docker"
  "github.com/picoaide/picoaide/internal/ldap"
  "github.com/picoaide/picoaide/internal/logger"
  "github.com/picoaide/picoaide/internal/user"
  "github.com/picoaide/picoaide/internal/util"
)

var gitMutex sync.Mutex

func (s *Server) requireSuperadmin(w http.ResponseWriter, r *http.Request) string {
  username := s.requireAuth(w, r)
  if username == "" {
    return ""
  }
  if !auth.IsSuperadmin(username) {
    writeError(w, http.StatusForbidden, "仅超级管理员可访问")
    return ""
  }
  return username
}

// ============================================================
// 用户管理
// ============================================================

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  containers, err := auth.GetAllContainers()
  if err != nil {
    writeError(w, http.StatusInternalServerError, err.Error())
    return
  }

  // 本地用户（local_users 表）
  localUsers, _ := auth.GetAllLocalUsers()
  localRoleMap := make(map[string]string)
  for _, u := range localUsers {
    localRoleMap[u.Username] = u.Role
  }

  type UserInfo struct {
    Username   string `json:"username"`
    Status     string `json:"status"`
    ImageTag   string `json:"image_tag"`
    ImageReady bool   `json:"image_ready"`
    IP         string `json:"ip"`
    Role       string `json:"role"`
  }

  ctx := context.Background()

  // 按 username 索引容器记录
  containerMap := make(map[string]*auth.ContainerRecord)
  for i := range containers {
    containerMap[containers[i].Username] = &containers[i]
  }

  var list []UserInfo

  // 先输出所有有容器记录的用户
  seen := make(map[string]bool)
  for _, c := range containers {
    seen[c.Username] = true
    status := c.Status
    if c.ContainerID != "" {
      status = dockerpkg.ContainerStatus(ctx, c.ContainerID)
    }

    imageRef := c.Image
    imageReady := imageRef != "" && dockerpkg.ImageExists(ctx, imageRef)
    imageTag := imageRef
    if parts := strings.SplitN(imageRef, ":", 2); len(parts) == 2 {
      imageTag = parts[1]
    }

    role := localRoleMap[c.Username]

    list = append(list, UserInfo{
      Username:   c.Username,
      Status:     status,
      ImageTag:   imageTag,
      ImageReady: imageReady,
      IP:         c.IP,
      Role:       role,
    })
  }

  // 补上本地用户中没有容器记录的（如超管）
  for _, u := range localUsers {
    if !seen[u.Username] {
      list = append(list, UserInfo{
        Username: u.Username,
        Status:   "未初始化",
        Role:     u.Role,
      })
    }
  }

  if list == nil {
    list = []UserInfo{}
  }

  writeJSON(w, http.StatusOK, struct {
    Success     bool       `json:"success"`
    Users       []UserInfo `json:"users"`
    AuthMode    string     `json:"auth_mode"`
    UnifiedAuth bool       `json:"unified_auth"`
  }{true, list, s.cfg.AuthMode(), s.cfg.UnifiedAuthEnabled()})
}

// ============================================================
// 用户创建与删除（仅本地模式）
// ============================================================

func (s *Server) handleAdminUserCreate(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if s.cfg.UnifiedAuthEnabled() {
    writeError(w, http.StatusForbidden, "统一认证模式下不允许手动创建用户")
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  username := r.FormValue("username")
  if err := user.ValidateUsername(username); err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
  }
  if auth.UserExists(username) {
    writeError(w, http.StatusBadRequest, "用户 "+username+" 已存在")
    return
  }

  password := auth.GenerateRandomPassword(12)
  if err := auth.CreateUser(username, password, "user"); err != nil {
    writeError(w, http.StatusInternalServerError, "创建用户失败: "+err.Error())
    return
  }

  // 获取镜像标签参数，未指定时自动使用本地最新标签
  imageTag := r.FormValue("image_tag")
  if imageTag == "" && s.dockerAvailable {
    ctx := contextWithTimeout(10)
    localTags, err := dockerpkg.ListLocalTags(ctx, s.cfg.Image.Name)
    if err == nil && len(localTags) > 0 {
      imageTag = localTags[len(localTags)-1]
    }
  }
  if imageTag == "" {
    auth.DeleteUser(username)
    writeError(w, http.StatusBadRequest, "未指定镜像标签且本地无可用镜像，请先拉取镜像")
    return
  }

  // 创建用户容器目录
  if err := user.InitUser(s.cfg, username, imageTag); err != nil {
    // 回滚：删除已创建的用户记录
    auth.DeleteUser(username)
    writeError(w, http.StatusInternalServerError, "初始化用户目录失败: "+err.Error())
    return
  }

  // 异步启动容器并下发配置
  if s.dockerAvailable {
    go s.autoStartUserContainer(username)
  }

  logger.Audit("user.create", "username", username, "operator", s.getSessionUser(r))
  writeJSON(w, http.StatusOK, struct {
    Success  bool   `json:"success"`
    Message  string `json:"message"`
    Username string `json:"username"`
    Password string `json:"password"`
  }{true, "用户创建成功，容器启动中", username, password})
}

func (s *Server) handleAdminUserDelete(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if s.cfg.UnifiedAuthEnabled() {
    writeError(w, http.StatusForbidden, "统一认证模式下不允许删除用户")
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  username := r.FormValue("username")
  if username == "" {
    writeError(w, http.StatusBadRequest, "用户名不能为空")
    return
  }
  if err := user.ValidateUsername(username); err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
  }

  // 停止并移除容器
  rec, _ := auth.GetContainerByUsername(username)
  if rec != nil && rec.ContainerID != "" {
    ctx := context.Background()
    _ = dockerpkg.Remove(ctx, rec.ContainerID)
  }
  auth.DeleteContainer(username)

  // 归档用户目录
  if err := user.ArchiveUser(s.cfg, username); err != nil {
    writeError(w, http.StatusInternalServerError, "归档用户目录失败: "+err.Error())
    return
  }

  // 删除本地用户记录
  if err := auth.DeleteUser(username); err != nil {
    writeError(w, http.StatusInternalServerError, err.Error())
    return
  }

  logger.Audit("user.delete", "username", username, "operator", s.getSessionUser(r))
  writeSuccess(w, "用户 "+username+" 已删除并归档")
}

// ============================================================
// 容器操作
// ============================================================

func (s *Server) handleAdminContainerStart(w http.ResponseWriter, r *http.Request) {
  s.handleContainerAction(w, r, "start")
}
func (s *Server) handleAdminContainerStop(w http.ResponseWriter, r *http.Request) {
  s.handleContainerAction(w, r, "stop")
}
func (s *Server) handleAdminContainerRestart(w http.ResponseWriter, r *http.Request) {
  s.handleContainerAction(w, r, "restart")
}

func (s *Server) handleContainerAction(w http.ResponseWriter, r *http.Request, action string) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if !s.dockerAvailable {
    writeError(w, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  username := r.FormValue("username")
  if username == "" {
    writeError(w, http.StatusBadRequest, "用户名不能为空")
    return
  }
  if err := user.ValidateUsername(username); err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
  }

  rec, err := auth.GetContainerByUsername(username)
  if err != nil || rec == nil {
    writeError(w, http.StatusBadRequest, "用户 "+username+" 未初始化")
    return
  }

  ctx := context.Background()

  // 启动/重启前检查镜像是否存在
  if action == "start" || action == "restart" {
    if rec.Image == "" || !dockerpkg.ImageExists(ctx, rec.Image) {
      writeError(w, http.StatusBadRequest, "容器镜像 "+rec.Image+" 不存在，请先拉取镜像")
      return
    }
  }

  switch action {
  case "start":
    // 如果容器不存在，创建容器
    if rec.ContainerID == "" || !dockerpkg.ContainerExists(ctx, username) {
      ud := user.UserDir(s.cfg, username)
      cid, createErr := dockerpkg.CreateContainer(ctx, username, rec.Image, ud, rec.IP, rec.CPULimit, rec.MemoryLimit)
      if createErr != nil {
        writeError(w, http.StatusInternalServerError, "创建容器失败: "+createErr.Error())
        return
      }
      auth.UpdateContainerID(username, cid)
      rec.ContainerID = cid
    }
    if err := dockerpkg.Start(ctx, rec.ContainerID); err != nil {
      writeError(w, http.StatusInternalServerError, err.Error())
      return
    }
    auth.UpdateContainerStatus(username, "running")

    // 配置下发：确保 config.json 包含全局配置和 MCP 注入
    picoclawDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw")
    configPath := filepath.Join(picoclawDir, "config.json")
    if _, err := os.Stat(configPath); err == nil {
      if err := user.ApplyConfigToJSON(s.cfg, picoclawDir, username); err != nil {
        slog.Error("下发配置失败", "username", username, "error", err)
      }
      if err := user.ApplySecurityToYAML(s.cfg, picoclawDir); err != nil {
        slog.Error("下发安全配置失败", "username", username, "error", err)
      }
    } else {
      go s.applyConfigAsync(username, picoclawDir, rec.ContainerID)
    }

  case "stop":
    // 停止并删除容器（类似 docker compose down）
    if rec.ContainerID != "" {
      _ = dockerpkg.Stop(ctx, rec.ContainerID)
      _ = dockerpkg.Remove(ctx, rec.ContainerID)
    }
    auth.UpdateContainerID(username, "")
    auth.UpdateContainerStatus(username, "stopped")

  case "restart":
    // 停止+删除旧容器，重新创建并启动
    if rec.ContainerID != "" {
      _ = dockerpkg.Stop(ctx, rec.ContainerID)
      _ = dockerpkg.Remove(ctx, rec.ContainerID)
      auth.UpdateContainerID(username, "")
    }
    ud := user.UserDir(s.cfg, username)
    cid, createErr := dockerpkg.CreateContainer(ctx, username, rec.Image, ud, rec.IP, rec.CPULimit, rec.MemoryLimit)
    if createErr != nil {
      writeError(w, http.StatusInternalServerError, "创建容器失败: "+createErr.Error())
      return
    }
    auth.UpdateContainerID(username, cid)
    if err := dockerpkg.Start(ctx, cid); err != nil {
      writeError(w, http.StatusInternalServerError, err.Error())
      return
    }
    auth.UpdateContainerStatus(username, "running")

    // 配置下发
    picoclawDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw")
    configPath := filepath.Join(picoclawDir, "config.json")
    if _, err := os.Stat(configPath); err == nil {
      if err := user.ApplyConfigToJSON(s.cfg, picoclawDir, username); err != nil {
        slog.Error("下发配置失败", "username", username, "error", err)
      }
      if err := user.ApplySecurityToYAML(s.cfg, picoclawDir); err != nil {
        slog.Error("下发安全配置失败", "username", username, "error", err)
      }
    } else {
      go s.applyConfigAsync(username, picoclawDir, cid)
    }
  }

  logger.Audit("container."+action, "username", username, "operator", s.getSessionUser(r))
  labels := map[string]string{"start": "启动", "stop": "停止", "restart": "重启"}
  writeSuccess(w, fmt.Sprintf("容器已%s", labels[action]))
}

// autoStartUserContainer 为新创建的用户自动启动容器并下发配置
func (s *Server) autoStartUserContainer(username string) {
  rec, err := auth.GetContainerByUsername(username)
  if err != nil || rec == nil || rec.Image == "" {
    slog.Warn("无容器记录或镜像未配置，跳过自动启动", "username", username)
    return
  }

  ctx := context.Background()
  if !dockerpkg.ImageExists(ctx, rec.Image) {
    slog.Warn("镜像不存在，跳过自动启动", "username", username, "image", rec.Image)
    return
  }

  ud := user.UserDir(s.cfg, username)
  cid, createErr := dockerpkg.CreateContainer(ctx, username, rec.Image, ud, rec.IP, rec.CPULimit, rec.MemoryLimit)
  if createErr != nil {
    slog.Error("创建容器失败", "username", username, "error", createErr)
    return
  }
  auth.UpdateContainerID(username, cid)

  if err := dockerpkg.Start(ctx, cid); err != nil {
    slog.Error("启动容器失败", "username", username, "error", err)
    return
  }
  auth.UpdateContainerStatus(username, "running")
  slog.Info("容器已自动启动", "username", username)

  picoclawDir := filepath.Join(ud, ".picoclaw")
  s.applyConfigAsync(username, picoclawDir, cid)
}

// applyConfigAsync 异步等待 config.json 生成后下发配置并重启容器
func (s *Server) applyConfigAsync(username, picoclawDir, containerID string) {
  configPath := filepath.Join(picoclawDir, "config.json")
  slog.Info("等待 config.json 生成", "username", username)

  // 轮询等待 config.json 出现，最多 60 秒
  for i := 0; i < 30; i++ {
    time.Sleep(2 * time.Second)
    if _, err := os.Stat(configPath); err == nil {
      break
    }
    if i == 29 {
      slog.Warn("等待 config.json 超时", "username", username)
      return
    }
  }

  // 下发配置
  if err := user.ApplyConfigToJSON(s.cfg, picoclawDir, username); err != nil {
    slog.Error("下发配置失败", "username", username, "error", err)
    return
  }
  if err := user.ApplySecurityToYAML(s.cfg, picoclawDir); err != nil {
    slog.Error("下发安全配置失败", "username", username, "error", err)
  }
  slog.Info("配置已下发", "username", username)

  // 重启容器使配置生效
  ctx := context.Background()
  if err := dockerpkg.Restart(ctx, containerID); err != nil {
    slog.Error("重启容器失败", "username", username, "error", err)
    return
  }
  slog.Info("容器已重启，配置生效", "username", username)
}

// applyConfigToUser 向单个用户下发配置并重启容器
func (s *Server) applyConfigToUser(username string) error {
  picoclawDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw")
  configPath := filepath.Join(picoclawDir, "config.json")

  // config.json 不存在说明容器还没启动过，跳过
  if _, err := os.Stat(configPath); err != nil {
    return fmt.Errorf("config.json 不存在")
  }

  if err := user.ApplyConfigToJSON(s.cfg, picoclawDir, username); err != nil {
    return fmt.Errorf("下发配置失败: %w", err)
  }
  if err := user.ApplySecurityToYAML(s.cfg, picoclawDir); err != nil {
    slog.Error("下发安全配置失败", "username", username, "error", err)
  }

  // 重启容器使配置生效
  rec, err := auth.GetContainerByUsername(username)
  if err != nil || rec == nil || rec.ContainerID == "" {
    return fmt.Errorf("容器记录不存在")
  }
  if rec.Status != "running" {
    return nil // 容器未运行，下次启动时自动下发
  }
  ctx := context.Background()
  if err := dockerpkg.Restart(ctx, rec.ContainerID); err != nil {
    return fmt.Errorf("重启失败: %w", err)
  }
  return nil
}

// handleAdminConfigApply 下发配置到指定用户/组/全部用户并重启容器
func (s *Server) handleAdminConfigApply(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  username := r.FormValue("username")
  group := r.FormValue("group")

  var targets []string
  var err error

  switch {
  case username != "":
    if err := user.ValidateUsername(username); err != nil {
      writeError(w, http.StatusBadRequest, err.Error())
      return
    }
    targets = []string{username}
  case group != "":
    targets, err = auth.GetGroupMembersForDeploy(group)
    if err != nil {
      writeError(w, http.StatusBadRequest, "获取组成员失败: "+err.Error())
      return
    }
    if len(targets) == 0 {
      writeError(w, http.StatusBadRequest, "组 "+group+" 没有成员")
      return
    }
  default:
    // 不指定用户和组时，下发到所有用户
    targets, err = user.GetUserList(s.cfg)
    if err != nil {
      writeError(w, http.StatusInternalServerError, "获取用户列表失败: "+err.Error())
      return
    }
  }

  // 单个用户直接同步执行
  if len(targets) == 1 {
    if err := s.applyConfigToUser(targets[0]); err != nil {
      writeError(w, http.StatusInternalServerError, err.Error())
    } else {
      writeSuccess(w, "配置已下发并重启")
    }
    return
  }

  // 多个用户走队列
  applyFn := func(u string) error {
    return s.applyConfigToUser(u)
  }
  taskID, err := enqueueTask("config-apply", targets, applyFn)
  if err != nil {
    writeError(w, http.StatusConflict, err.Error())
    return
  }
  writeJSON(w, http.StatusOK, map[string]interface{}{
    "success": true,
    "message": fmt.Sprintf("已提交配置下发任务，共 %d 个用户", len(targets)),
    "task_id": taskID,
  })
}

// handleAdminTaskStatus 返回当前任务队列状态
func (s *Server) handleAdminTaskStatus(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }
  status := getTaskStatus()
  writeJSON(w, http.StatusOK, status)
}

// handleAdminContainerLogs 返回容器日志
func (s *Server) handleAdminContainerLogs(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }
  if !s.dockerAvailable {
    writeError(w, http.StatusServiceUnavailable, "Docker 服务不可用")
    return
  }

  username := r.URL.Query().Get("username")
  if username == "" {
    writeError(w, http.StatusBadRequest, "用户名不能为空")
    return
  }
  if err := user.ValidateUsername(username); err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
  }

  rec, err := auth.GetContainerByUsername(username)
  if err != nil || rec == nil {
    writeError(w, http.StatusBadRequest, "用户 "+username+" 未初始化")
    return
  }
  if rec.ContainerID == "" {
    writeError(w, http.StatusBadRequest, "容器未创建")
    return
  }

  tail := r.URL.Query().Get("tail")

  ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
  defer cancel()

  logs, err := dockerpkg.ContainerLogs(ctx, rec.ContainerID, tail)
  if err != nil {
    writeError(w, http.StatusInternalServerError, err.Error())
    return
  }

  writeJSON(w, http.StatusOK, struct {
    Success  bool   `json:"success"`
    Username string `json:"username"`
    Logs     string `json:"logs"`
  }{
    Success:  true,
    Username: username,
    Logs:     logs,
  })
}

// ============================================================
// 认证配置 — LDAP 测试 & 用户搜索
// ============================================================

func (s *Server) handleAdminAuthTestLDAP(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  host := r.FormValue("host")
  bindDN := r.FormValue("bind_dn")
  bindPassword := r.FormValue("bind_password")
  baseDN := r.FormValue("base_dn")
  filter := r.FormValue("filter")
  usernameAttr := r.FormValue("username_attribute")
  groupSearchMode := r.FormValue("group_search_mode")
  groupBaseDN := r.FormValue("group_base_dn")
  groupFilter := r.FormValue("group_filter")
  groupMemberAttr := r.FormValue("group_member_attribute")

  if host == "" || bindDN == "" || baseDN == "" {
    writeError(w, http.StatusBadRequest, "LDAP 地址、Bind DN 和 Base DN 不能为空")
    return
  }

  users, err := ldap.TestConnection(host, bindDN, bindPassword, baseDN, filter, usernameAttr)
  if err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
  }

  // 测试组查询（失败不影响用户测试结果）
  var groupError string
  groups, gerr := ldap.TestGroups(host, bindDN, bindPassword, baseDN, groupSearchMode, groupBaseDN, groupFilter, groupMemberAttr, usernameAttr)
  if gerr != nil {
    groupError = gerr.Error()
  }
  if groups == nil {
    groups = []ldap.GroupPreview{}
  }

  writeJSON(w, http.StatusOK, struct {
    Success    bool                `json:"success"`
    Message    string              `json:"message"`
    UserCount  int                 `json:"user_count"`
    Users      []string            `json:"users"`
    Groups     []ldap.GroupPreview `json:"groups"`
    GroupError string              `json:"group_error"`
  }{true, fmt.Sprintf("连接成功，找到 %d 个用户", len(users)), len(users), users, groups, groupError})
}

func (s *Server) handleAdminAuthLDAPUsers(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  users, err := ldap.FetchUsers(s.cfg)
  if err != nil {
    writeError(w, http.StatusInternalServerError, err.Error())
    return
  }

  if users == nil {
    users = []string{}
  }
  writeJSON(w, http.StatusOK, struct {
    Success bool     `json:"success"`
    Users   []string `json:"users"`
  }{true, users})
}

// ============================================================
// 白名单管理
// ============================================================

func (s *Server) handleAdminWhitelist(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }

  if r.Method == "GET" {
    whitelist, err := user.LoadWhitelist()
    if err != nil {
      writeError(w, http.StatusInternalServerError, err.Error())
      return
    }
    var users []string
    if whitelist != nil {
      for u := range whitelist {
        users = append(users, u)
      }
      sort.Strings(users)
    }
    if users == nil {
      users = []string{}
    }
    writeJSON(w, http.StatusOK, struct {
      Success bool     `json:"success"`
      Users   []string `json:"users"`
    }{true, users})
    return
  }

  if r.Method == "POST" {
    if !s.checkCSRF(r) {
      writeError(w, http.StatusForbidden, "无效请求")
      return
    }
    usersStr := r.FormValue("users")
    var users []string
    if usersStr != "" {
      for _, u := range strings.Split(usersStr, ",") {
        u = strings.TrimSpace(u)
        if u != "" {
          users = append(users, u)
        }
      }
    }
    sort.Strings(users)

    // 写入数据库
    d, err := auth.GetDB()
    if err != nil {
      writeError(w, http.StatusInternalServerError, "数据库连接失败")
      return
    }
    tx, err := d.Begin()
    if err != nil {
      writeError(w, http.StatusInternalServerError, err.Error())
      return
    }
    defer tx.Rollback()
    tx.Exec("DELETE FROM whitelist")
    for _, u := range users {
      tx.Exec("INSERT OR IGNORE INTO whitelist (username, added_by) VALUES (?, ?)", u, s.getSessionUser(r))
    }
    if err := tx.Commit(); err != nil {
      writeError(w, http.StatusInternalServerError, err.Error())
      return
    }
    writeSuccess(w, "白名单已更新")
    logger.Audit("whitelist.update", "count", len(users), "operator", s.getSessionUser(r))
    return
  }

  writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 和 POST 方法")
}

// ============================================================
// 技能库管理
// ============================================================

// skillReposDir 技能仓库克隆区路径
func skillReposDir() string {
  wd, _ := os.Getwd()
  return filepath.Join(wd, "skill-repos")
}

func (s *Server) handleAdminSkills(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  type SkillInfo struct {
    Name      string `json:"name"`
    FileCount int    `json:"file_count"`
    Size      int64  `json:"size"`
    SizeStr   string `json:"size_str"`
    ModTime   string `json:"mod_time"`
  }

  var skills []SkillInfo
  skillDir := config.SkillsDirPath()
  entries, err := os.ReadDir(skillDir)
  if err == nil {
    for _, e := range entries {
      if !e.IsDir() {
        continue
      }
      info, ierr := e.Info()
      if ierr != nil {
        continue
      }
      fullPath := filepath.Join(skillDir, e.Name())
      var fileCount int
      var totalSize int64
      filepath.WalkDir(fullPath, func(_ string, d os.DirEntry, err error) error {
        if err != nil {
          return nil
        }
        if !d.IsDir() {
          fileCount++
          if fi, e := d.Info(); e == nil {
            totalSize += fi.Size()
          }
        }
        return nil
      })
      skills = append(skills, SkillInfo{
        Name:      e.Name(),
        FileCount: fileCount,
        Size:      totalSize,
        SizeStr:   formatSize(totalSize),
        ModTime:   info.ModTime().Format("2006-01-02 15:04"),
      })
    }
  }
  if skills == nil {
    skills = []SkillInfo{}
  }

  writeJSON(w, http.StatusOK, struct {
    Success bool        `json:"success"`
    Skills  []SkillInfo `json:"skills"`
    Repos   []config.SkillRepo `json:"repos"`
  }{true, skills, s.cfg.Skills.Repos})
}

func (s *Server) handleAdminSkillsDeploy(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  skillName := strings.TrimSpace(r.FormValue("skill_name"))
  targetUser := strings.TrimSpace(r.FormValue("username"))
  targetGroup := strings.TrimSpace(r.FormValue("group_name"))

  if skillName != "" {
    if err := util.SafePathSegment(skillName); err != nil {
      writeError(w, http.StatusBadRequest, "技能名称不合法: "+err.Error())
      return
    }
  }
  if targetUser != "" {
    if err := user.ValidateUsername(targetUser); err != nil {
      writeError(w, http.StatusBadRequest, err.Error())
      return
    }
  }

  skillDir := config.SkillsDirPath()
  entries, err := os.ReadDir(skillDir)
  if err != nil {
    writeError(w, http.StatusInternalServerError, "读取技能目录失败: "+err.Error())
    return
  }

  var deploySkills []string
  for _, e := range entries {
    if !e.IsDir() {
      continue
    }
    if skillName == "" || e.Name() == skillName {
      deploySkills = append(deploySkills, e.Name())
    }
  }
  if len(deploySkills) == 0 {
    writeError(w, http.StatusBadRequest, "没有找到可部署的技能")
    return
  }

  deployFn := func(username string) error {
    targetSkillsDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw", "workspace", "skills")
    for _, sn := range deploySkills {
      srcPath := filepath.Join(skillDir, sn)
      dstPath := filepath.Join(targetSkillsDir, sn)
      if err := util.CopyDir(srcPath, dstPath); err != nil {
        return fmt.Errorf("复制技能 %s 失败: %w", sn, err)
      }
    }
    return nil
  }

  if targetUser != "" {
    // 单个用户直接执行
    if err := deployFn(targetUser); err != nil {
      writeError(w, http.StatusInternalServerError, err.Error())
      return
    }
    writeJSON(w, http.StatusOK, map[string]interface{}{
      "success":     true,
      "message":     fmt.Sprintf("已将 %d 个技能部署到 %s", len(deploySkills), targetUser),
      "skill_count": len(deploySkills),
      "user_count":  1,
    })
    return
  }

  // 组或全部用户走队列
  var targets []string
  if targetGroup != "" {
    targets, err = auth.GetGroupMembersForDeploy(targetGroup)
    if err != nil {
      writeError(w, http.StatusBadRequest, "获取组成员失败: "+err.Error())
      return
    }
  } else {
    targets, err = user.GetUserList(s.cfg)
    if err != nil {
      writeError(w, http.StatusInternalServerError, "获取用户列表失败: "+err.Error())
      return
    }
  }

  taskID, err := enqueueTask("skills-deploy", targets, deployFn)
  if err != nil {
    writeError(w, http.StatusConflict, err.Error())
    return
  }
  writeJSON(w, http.StatusOK, map[string]interface{}{
    "success":     true,
    "message":     fmt.Sprintf("已提交技能部署任务，共 %d 个用户", len(targets)),
    "task_id":     taskID,
    "skill_count": len(deploySkills),
  })
}

func (s *Server) handleAdminSkillsDownload(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }
  name := r.URL.Query().Get("name")
  if name == "" {
    writeError(w, http.StatusBadRequest, "技能名称不能为空")
    return
  }
  if err := util.SafePathSegment(name); err != nil {
    writeError(w, http.StatusBadRequest, "技能名称不合法")
    return
  }
  skillPath := filepath.Join(config.SkillsDirPath(), name)
  if _, err := os.Stat(skillPath); err != nil {
    writeError(w, http.StatusNotFound, "技能不存在")
    return
  }
  w.Header().Set("Content-Type", "application/zip")
  w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, name))
  zw := zip.NewWriter(w)
  filepath.WalkDir(skillPath, func(path string, d os.DirEntry, err error) error {
    if err != nil {
      return nil
    }
    relPath, _ := filepath.Rel(skillPath, path)
    // 安全检查：防止 zip slip — 确保相对路径不以 .. 开头
    relPath = filepath.ToSlash(relPath)
    if strings.HasPrefix(relPath, "../") || relPath == ".." {
      return nil
    }
    if d.IsDir() {
      zw.Create(relPath + "/")
      return nil
    }
    fw, err := zw.Create(relPath)
    if err != nil {
      return nil
    }
    f, err := os.Open(path)
    if err != nil {
      return nil
    }
    defer f.Close()
    io.Copy(fw, f)
    return nil
  })
  zw.Close()
}

// handleAdminSkillsRemove 从 skill/ 删除技能
func (s *Server) handleAdminSkillsRemove(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }
  name := strings.TrimSpace(r.FormValue("name"))
  if name == "" {
    writeError(w, http.StatusBadRequest, "技能名称不能为空")
    return
  }
  if err := util.SafePathSegment(name); err != nil {
    writeError(w, http.StatusBadRequest, "技能名称不合法")
    return
  }
  skillPath := filepath.Join(config.SkillsDirPath(), name)
  if err := os.RemoveAll(skillPath); err != nil {
    writeError(w, http.StatusInternalServerError, "删除失败: "+err.Error())
    return
  }
  writeSuccess(w, "技能已删除")
}

// ============================================================
// 技能仓库管理（多仓库）
// ============================================================

// handleAdminSkillsReposAdd 添加并克隆技能仓库
func (s *Server) handleAdminSkillsReposAdd(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  repoName := strings.TrimSpace(r.FormValue("name"))
  repoURL := strings.TrimSpace(r.FormValue("url"))
  if repoName == "" || repoURL == "" {
    writeError(w, http.StatusBadRequest, "仓库名称和地址不能为空")
    return
  }
  if err := util.SafePathSegment(repoName); err != nil {
    writeError(w, http.StatusBadRequest, "仓库名称不合法: "+err.Error())
    return
  }
  // 验证仓库 URL 必须是合法的 Git 远程地址
  if !strings.HasPrefix(repoURL, "https://") && !strings.HasPrefix(repoURL, "git@") && !strings.HasPrefix(repoURL, "ssh://") {
    writeError(w, http.StatusBadRequest, "仓库地址必须是 https://、git@ 或 ssh:// 开头的 Git 地址")
    return
  }

  gitMutex.Lock()
  defer gitMutex.Unlock()

  if _, err := exec.LookPath("git"); err != nil {
    writeError(w, http.StatusInternalServerError, "Git 未安装")
    return
  }

  // 检查重名
  for _, r := range s.cfg.Skills.Repos {
    if r.Name == repoName {
      writeError(w, http.StatusBadRequest, "仓库名称已存在")
      return
    }
  }

  reposDir := skillReposDir()
  targetDir := filepath.Join(reposDir, repoName)
  if _, err := os.Stat(targetDir); err == nil {
    writeError(w, http.StatusBadRequest, "仓库目录已存在")
    return
  }

  os.MkdirAll(reposDir, 0755)
  if _, err := gitCmd(reposDir, "clone", repoURL, repoName); err != nil {
    writeError(w, http.StatusInternalServerError, "Git clone 失败: "+err.Error())
    return
  }

  s.cfg.Skills.Repos = append(s.cfg.Skills.Repos, config.SkillRepo{
    Name:     repoName,
    URL:      repoURL,
    LastPull: time.Now().Format("2006-01-02 15:04:05"),
  })
  s.saveSkillsConfig()
  writeSuccess(w, "仓库已克隆")
}

// handleAdminSkillsReposPull 拉取仓库更新
func (s *Server) handleAdminSkillsReposPull(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  repoName := strings.TrimSpace(r.FormValue("name"))
  if repoName == "" {
    writeError(w, http.StatusBadRequest, "仓库名称不能为空")
    return
  }
  if err := util.SafePathSegment(repoName); err != nil {
    writeError(w, http.StatusBadRequest, "仓库名称不合法")
    return
  }

  gitMutex.Lock()
  defer gitMutex.Unlock()

  repoDir := filepath.Join(skillReposDir(), repoName)
  if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
    writeError(w, http.StatusBadRequest, "不是 Git 仓库")
    return
  }

  gitCmd(repoDir, "reset", "--hard")
  out, err := gitCmd(repoDir, "pull")
  if err != nil {
    writeError(w, http.StatusInternalServerError, "Git pull 失败: "+err.Error())
    return
  }

  for i := range s.cfg.Skills.Repos {
    if s.cfg.Skills.Repos[i].Name == repoName {
      s.cfg.Skills.Repos[i].LastPull = time.Now().Format("2006-01-02 15:04:05")
      break
    }
  }
  s.saveSkillsConfig()

  updated := !strings.Contains(out, "Already up to date")
  writeJSON(w, http.StatusOK, struct {
    Success bool   `json:"success"`
    Message string `json:"message"`
    Updated bool   `json:"updated"`
  }{true, "已拉取更新", updated})
}

// handleAdminSkillsReposRemove 删除仓库
func (s *Server) handleAdminSkillsReposRemove(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  repoName := strings.TrimSpace(r.FormValue("name"))
  if repoName == "" {
    writeError(w, http.StatusBadRequest, "仓库名称不能为空")
    return
  }
  if err := util.SafePathSegment(repoName); err != nil {
    writeError(w, http.StatusBadRequest, "仓库名称不合法")
    return
  }

  os.RemoveAll(filepath.Join(skillReposDir(), repoName))

  var filtered []config.SkillRepo
  for _, r := range s.cfg.Skills.Repos {
    if r.Name != repoName {
      filtered = append(filtered, r)
    }
  }
  s.cfg.Skills.Repos = filtered
  s.saveSkillsConfig()
  writeSuccess(w, "仓库已删除")
}

// handleAdminSkillsInstall 从仓库安装技能到 skill/ 目录
func (s *Server) handleAdminSkillsInstall(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  repoName := strings.TrimSpace(r.FormValue("repo"))
  skillName := strings.TrimSpace(r.FormValue("skill"))
  if repoName == "" {
    writeError(w, http.StatusBadRequest, "仓库名称不能为空")
    return
  }
  if err := util.SafePathSegment(repoName); err != nil {
    writeError(w, http.StatusBadRequest, "仓库名称不合法")
    return
  }
  if skillName != "" {
    if err := util.SafePathSegment(skillName); err != nil {
      writeError(w, http.StatusBadRequest, "技能名称不合法")
      return
    }
  }

  repoDir := filepath.Join(skillReposDir(), repoName)
  skillDir := config.SkillsDirPath()
  os.MkdirAll(skillDir, 0755)

  if skillName == "" {
    // 安装仓库中所有技能
    entries, err := os.ReadDir(repoDir)
    if err != nil {
      writeError(w, http.StatusInternalServerError, "读取仓库失败")
      return
    }
    count := 0
    for _, e := range entries {
      if !e.IsDir() || e.Name() == ".git" {
        continue
      }
      src := filepath.Join(repoDir, e.Name())
      dst := filepath.Join(skillDir, e.Name())
      if err := util.CopyDir(src, dst); err != nil {
        writeError(w, http.StatusInternalServerError, fmt.Sprintf("安装 %s 失败: %v", e.Name(), err))
        return
      }
      count++
    }
    writeJSON(w, http.StatusOK, struct {
      Success bool   `json:"success"`
      Message string `json:"message"`
      Count   int    `json:"count"`
    }{true, fmt.Sprintf("已安装 %d 个技能", count), count})
    return
  }

  // 安装单个技能
  src := filepath.Join(repoDir, skillName)
  if _, err := os.Stat(src); err != nil {
    writeError(w, http.StatusNotFound, "技能在仓库中不存在")
    return
  }
  dst := filepath.Join(skillDir, skillName)
  if err := util.CopyDir(src, dst); err != nil {
    writeError(w, http.StatusInternalServerError, "安装失败: "+err.Error())
    return
  }
  writeSuccess(w, "技能已安装")
}

// handleAdminSkillsReposList 列出仓库中的技能
func (s *Server) handleAdminSkillsReposList(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }

  repoName := r.URL.Query().Get("name")
  if repoName == "" {
    writeError(w, http.StatusBadRequest, "仓库名称不能为空")
    return
  }
  if err := util.SafePathSegment(repoName); err != nil {
    writeError(w, http.StatusBadRequest, "仓库名称不合法")
    return
  }

  repoDir := filepath.Join(skillReposDir(), repoName)
  entries, err := os.ReadDir(repoDir)
  if err != nil {
    writeError(w, http.StatusNotFound, "仓库不存在")
    return
  }

  type SkillEntry struct {
    Name string `json:"name"`
    Installed bool `json:"installed"`
  }
  var skills []SkillEntry
  skillDir := config.SkillsDirPath()

  for _, e := range entries {
    if !e.IsDir() || e.Name() == ".git" {
      continue
    }
    _, installed := os.Stat(filepath.Join(skillDir, e.Name()))
    skills = append(skills, SkillEntry{
      Name: e.Name(),
      Installed: installed == nil,
    })
  }
  if skills == nil {
    skills = []SkillEntry{}
  }

  writeJSON(w, http.StatusOK, struct {
    Success bool         `json:"success"`
    Skills  []SkillEntry `json:"skills"`
  }{true, skills})
}

// ============================================================
// 用户组管理
// ============================================================

func (s *Server) handleAdminGroups(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }
  groups, err := auth.ListGroups()
  if err != nil {
    writeError(w, http.StatusInternalServerError, err.Error())
    return
  }
  if groups == nil {
    groups = []auth.GroupInfo{}
  }
  writeJSON(w, http.StatusOK, struct {
    Success bool              `json:"success"`
    Groups  []auth.GroupInfo  `json:"groups"`
  }{true, groups})
}

func (s *Server) handleAdminGroupCreate(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if s.cfg.UnifiedAuthEnabled() {
    writeError(w, http.StatusForbidden, "统一认证模式下不允许手动创建组，请通过 LDAP 同步")
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }
  name := strings.TrimSpace(r.FormValue("name"))
  description := strings.TrimSpace(r.FormValue("description"))
  parentIDStr := strings.TrimSpace(r.FormValue("parent_id"))
  if name == "" {
    writeError(w, http.StatusBadRequest, "组名不能为空")
    return
  }
  var parentID *int64
  if parentIDStr != "" {
    pid, err := strconv.ParseInt(parentIDStr, 10, 64)
    if err != nil {
      writeError(w, http.StatusBadRequest, "无效的父组 ID")
      return
    }
    parentID = &pid
  }
  if err := auth.CreateGroup(name, "local", description, parentID); err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
  }
  writeSuccess(w, "组 "+name+" 创建成功")
}

func (s *Server) handleAdminGroupDelete(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }
  name := strings.TrimSpace(r.FormValue("name"))
  if name == "" {
    writeError(w, http.StatusBadRequest, "组名不能为空")
    return
  }
  if err := auth.DeleteGroup(name); err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
  }
  writeSuccess(w, "组 "+name+" 已删除")
}

func (s *Server) handleAdminGroupMembersAdd(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }
  groupName := strings.TrimSpace(r.FormValue("group_name"))
  usersStr := strings.TrimSpace(r.FormValue("usernames"))
  if groupName == "" || usersStr == "" {
    writeError(w, http.StatusBadRequest, "组名和用户名不能为空")
    return
  }
  usernames := strings.Split(usersStr, ",")
  var trimmed []string
  for _, u := range usernames {
    u = strings.TrimSpace(u)
    if u != "" {
      trimmed = append(trimmed, u)
    }
  }
  if len(trimmed) == 0 {
    writeError(w, http.StatusBadRequest, "用户名不能为空")
    return
  }
  if err := auth.AddUsersToGroup(groupName, trimmed); err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
  }
  writeSuccess(w, fmt.Sprintf("已添加 %d 个用户到组 %s", len(trimmed), groupName))
}

func (s *Server) handleAdminGroupMembersRemove(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }
  groupName := strings.TrimSpace(r.FormValue("group_name"))
  username := strings.TrimSpace(r.FormValue("username"))
  if groupName == "" || username == "" {
    writeError(w, http.StatusBadRequest, "组名和用户名不能为空")
    return
  }
  if err := auth.RemoveUserFromGroup(groupName, username); err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
  }
  writeSuccess(w, "已从组 "+groupName+" 移除 "+username)
}

func (s *Server) handleAdminGroupSkillsBind(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }
  groupName := strings.TrimSpace(r.FormValue("group_name"))
  skillName := strings.TrimSpace(r.FormValue("skill_name"))
  if groupName == "" || skillName == "" {
    writeError(w, http.StatusBadRequest, "组名和技能名不能为空")
    return
  }
  if err := util.SafePathSegment(skillName); err != nil {
    writeError(w, http.StatusBadRequest, "技能名称不合法")
    return
  }
  if err := auth.BindSkillToGroup(groupName, skillName); err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
  }

  // 绑定后立即部署到组内所有用户
  members, err := auth.GetGroupMembersForDeploy(groupName)
  if err != nil {
    writeError(w, http.StatusInternalServerError, "绑定成功但获取组成员失败: "+err.Error())
    return
  }
  skillDir := config.SkillsDirPath()
  userCount := 0
  for _, username := range members {
    targetDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw", "workspace", "skills")
    srcPath := filepath.Join(skillDir, skillName)
    dstPath := filepath.Join(targetDir, skillName)
    if err := util.CopyDir(srcPath, dstPath); err == nil {
      userCount++
    }
  }

  writeJSON(w, http.StatusOK, struct {
    Success   bool   `json:"success"`
    Message   string `json:"message"`
    UserCount int    `json:"user_count"`
  }{true, fmt.Sprintf("技能 %s 已绑定到组 %s 并部署到 %d 个用户", skillName, groupName, userCount), userCount})
}

func (s *Server) handleAdminGroupSkillsUnbind(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }
  groupName := strings.TrimSpace(r.FormValue("group_name"))
  skillName := strings.TrimSpace(r.FormValue("skill_name"))
  if groupName == "" || skillName == "" {
    writeError(w, http.StatusBadRequest, "组名和技能名不能为空")
    return
  }
  if err := util.SafePathSegment(skillName); err != nil {
    writeError(w, http.StatusBadRequest, "技能名称不合法")
    return
  }
  if err := auth.UnbindSkillFromGroup(groupName, skillName); err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
  }
  writeSuccess(w, "已从组 "+groupName+" 解绑技能 "+skillName)
}

func (s *Server) handleAdminGroupMembers(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "GET" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
    return
  }
  groupName := r.URL.Query().Get("name")
  if groupName == "" {
    writeError(w, http.StatusBadRequest, "组名不能为空")
    return
  }
  members, err := auth.GetGroupMembers(groupName)
  if err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
  }
  skills, err := auth.GetGroupSkills(groupName)
  if err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
  }
  if members == nil {
    members = []string{}
  }
  if skills == nil {
    skills = []string{}
  }
  writeJSON(w, http.StatusOK, struct {
    Success  bool     `json:"success"`
    Members  []string `json:"members"`
    Skills   []string `json:"skills"`
  }{true, members, skills})
}

// handleAdminAuthSyncGroups 手动触发 LDAP 组同步
func (s *Server) handleAdminAuthSyncGroups(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }
  if !s.cfg.LDAPEnabled() {
    writeError(w, http.StatusBadRequest, "LDAP 未启用")
    return
  }

  groupMap, err := ldap.FetchAllGroupsWithMembers(s.cfg)
  if err != nil {
    writeError(w, http.StatusInternalServerError, "获取 LDAP 组失败: "+err.Error())
    return
  }

  // 获取白名单
  whitelist, _ := user.LoadWhitelist()

  groupCount := 0
  userCount := 0
  for groupName, members := range groupMap {
    // 创建组（如果不存在）
    auth.CreateGroup(groupName, "ldap", "", nil)
    groupCount++

    // 过滤白名单用户
    var filtered []string
    for _, m := range members {
      if whitelist == nil || whitelist[m] {
        filtered = append(filtered, m)
      }
    }

    if len(filtered) > 0 {
      auth.AddUsersToGroup(groupName, filtered)
      userCount += len(filtered)
    }
  }

  writeJSON(w, http.StatusOK, struct {
    Success     bool   `json:"success"`
    Message     string `json:"message"`
    GroupCount  int    `json:"group_count"`
    MemberCount int    `json:"member_count"`
  }{true, fmt.Sprintf("同步完成，发现 %d 个组，共 %d 个组成员关系", groupCount, userCount), groupCount, userCount})
}

// ============================================================
// 辅助函数
// ============================================================

func gitCmd(dir string, args ...string) (string, error) {
  cmd := exec.Command("git", args...)
  cmd.Dir = dir
  var stdout, stderr strings.Builder
  cmd.Stdout = &stdout
  cmd.Stderr = &stderr
  if err := cmd.Run(); err != nil {
    return "", fmt.Errorf("%w\n%s", err, stderr.String())
  }
  return stdout.String(), nil
}

func (s *Server) saveSkillsConfig() {
  skillsJSON, err := json.Marshal(map[string]interface{}{
    "repos": s.cfg.Skills.Repos,
  })
  if err != nil {
    return
  }
  d, err := auth.GetDB()
  if err != nil {
    return
  }
  d.Exec("INSERT OR REPLACE INTO settings (key, value, updated_at) VALUES ('skills', ?, datetime('now','localtime'))", string(skillsJSON))
}

func formatSize(size int64) string {
  if size < 1024 {
    return fmt.Sprintf("%d B", size)
  }
  if size < 1024*1024 {
    return fmt.Sprintf("%.1f KB", float64(size)/1024)
  }
  return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
}

func minInt(a, b int) int {
  if a < b {
    return a
  }
  return b
}

// ============================================================
// 超管账户管理
// ============================================================

// handleAdminSuperadmins 返回超管列表
func (s *Server) handleAdminSuperadmins(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method == "GET" {
    list, err := auth.GetSuperadmins()
    if err != nil {
      writeError(w, http.StatusInternalServerError, err.Error())
      return
    }
    if list == nil {
      list = []string{}
    }
    writeJSON(w, http.StatusOK, struct {
      Success bool     `json:"success"`
      Admins  []string `json:"admins"`
    }{true, list})
    return
  }
  writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
}

// handleAdminSuperadminCreate 创建超管账户
func (s *Server) handleAdminSuperadminCreate(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  username := r.FormValue("username")
  if err := user.ValidateUsername(username); err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
  }
  if auth.UserExists(username) {
    writeError(w, http.StatusBadRequest, "用户 "+username+" 已存在")
    return
  }

  password := auth.GenerateRandomPassword(12)
  if err := auth.CreateUser(username, password, "superadmin"); err != nil {
    writeError(w, http.StatusInternalServerError, "创建超管失败: "+err.Error())
    return
  }

  writeJSON(w, http.StatusOK, struct {
    Success  bool   `json:"success"`
    Message  string `json:"message"`
    Username string `json:"username"`
    Password string `json:"password"`
  }{true, "超管创建成功", username, password})
  logger.Audit("superadmin.create", "username", username, "operator", s.getSessionUser(r))
}

// handleAdminSuperadminDelete 删除超管账户（至少保留一个）
func (s *Server) handleAdminSuperadminDelete(w http.ResponseWriter, r *http.Request) {
  currentUser := s.requireSuperadmin(w, r)
  if currentUser == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  username := r.FormValue("username")
  if username == "" {
    writeError(w, http.StatusBadRequest, "用户名不能为空")
    return
  }
  if username == currentUser {
    writeError(w, http.StatusBadRequest, "不能删除自己")
    return
  }
  if err := user.ValidateUsername(username); err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
  }
  if !auth.IsSuperadmin(username) {
    writeError(w, http.StatusBadRequest, username+" 不是超管")
    return
  }

  admins, _ := auth.GetSuperadmins()
  if len(admins) <= 1 {
    writeError(w, http.StatusBadRequest, "至少保留一个超管账户")
    return
  }

  if err := auth.DeleteUser(username); err != nil {
    writeError(w, http.StatusInternalServerError, "删除失败: "+err.Error())
    return
  }

  writeSuccess(w, "超管 "+username+" 已删除")
  logger.Audit("superadmin.delete", "username", username, "operator", currentUser)
}

// handleAdminSuperadminReset 重置超管密码
func (s *Server) handleAdminSuperadminReset(w http.ResponseWriter, r *http.Request) {
  if s.requireSuperadmin(w, r) == "" {
    return
  }
  if r.Method != "POST" {
    writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
    return
  }
  if !s.checkCSRF(r) {
    writeError(w, http.StatusForbidden, "无效请求")
    return
  }

  username := r.FormValue("username")
  if username == "" {
    writeError(w, http.StatusBadRequest, "用户名不能为空")
    return
  }
  if err := user.ValidateUsername(username); err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
  }
  if !auth.IsSuperadmin(username) {
    writeError(w, http.StatusBadRequest, username+" 不是超管")
    return
  }

  password := auth.GenerateRandomPassword(12)
  if err := auth.ChangePassword(username, password); err != nil {
    writeError(w, http.StatusInternalServerError, "重置密码失败: "+err.Error())
    return
  }

  writeJSON(w, http.StatusOK, struct {
    Success  bool   `json:"success"`
    Message  string `json:"message"`
    Password string `json:"password"`
  }{true, "密码已重置", password})
  logger.Audit("password.reset", "username", username, "operator", s.getSessionUser(r))
}
