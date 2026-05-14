package web

import (
  "context"
  "fmt"
  "net/http"
  "sort"
  "strings"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  dockerpkg "github.com/picoaide/picoaide/internal/docker"
  "github.com/picoaide/picoaide/internal/user"
)

// ============================================================
// 镜像迁移与升级 Handler
// ============================================================

// handleAdminImageMigrate 将用户从旧镜像迁移到新镜像（更新 DB + 重建容器）
func (s *Server) handleAdminImageMigrate(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if !s.dockerAvailable {
    writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  oldImage := c.PostForm("image")
  newImage := c.PostForm("target")
  if oldImage == "" || newImage == "" {
    writeError(c, http.StatusBadRequest, "必须指定旧镜像和新镜像")
    return
  }
  if oldImage == newImage {
    writeError(c, http.StatusBadRequest, "新旧镜像不能相同")
    return
  }
  migrator, err := user.NewPicoClawMigrationService(config.RuleCacheDir())
  if err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }
  if err := migrator.EnsureUpgradeable(imageTagFromRef(oldImage), imageTagFromRef(newImage)); err != nil {
    writeError(c, http.StatusBadRequest, err.Error())
    return
  }

  // 检查新镜像是否存在
  ctx := context.Background()
  if !dockerpkg.ImageExists(ctx, newImage) {
    writeError(c, http.StatusBadRequest, "新镜像 "+newImage+" 不存在，请先拉取")
    return
  }

  // 找出依赖旧镜像的用户
  containers, err := auth.GetAllContainers()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "查询用户列表失败")
    return
  }

  // 支持指定特定用户列表
  userFilter := c.PostForm("users")
  var targetUsers []string
  for _, ctr := range containers {
    if ctr.Image != oldImage {
      continue
    }
    if userFilter != "" {
      found := false
      for _, u := range strings.Split(userFilter, ",") {
        if strings.TrimSpace(u) == ctr.Username {
          found = true
          break
        }
      }
      if !found {
        continue
      }
    }
    targetUsers = append(targetUsers, ctr.Username)
  }

  if len(targetUsers) == 0 {
    writeError(c, http.StatusBadRequest, "没有用户使用旧镜像 "+oldImage)
    return
  }

  var success []string
  var failed []string

  for _, username := range targetUsers {
    fromTag := imageTagFromRef(oldImage)
    // 更新 DB 中的镜像引用
    if err := auth.UpdateContainerImage(username, newImage); err != nil {
      failed = append(failed, username+"(更新失败)")
      continue
    }

    // 如果容器正在运行，重建容器以使用新镜像
    rec, _ := auth.GetContainerByUsername(username)
    if rec == nil {
      failed = append(failed, username+"(记录不存在)")
      continue
    }
    if rec.ContainerID != "" {
      _ = dockerpkg.Stop(ctx, rec.ContainerID)
      _ = dockerpkg.Remove(ctx, rec.ContainerID)
      auth.UpdateContainerID(username, "")
    }
    // 只有原来是 running 状态才重新启动
    if rec.Status == "running" {
      ud := user.UserDir(s.cfg, username)
      cid, createErr := dockerpkg.CreateContainer(ctx, username, newImage, ud, rec.IP, rec.CPULimit, rec.MemoryLimit, rec.MCPToken)
      if createErr != nil {
        failed = append(failed, username+"(创建失败)")
        continue
      }
      auth.UpdateContainerID(username, cid)
      if err := s.applyConfigForUpgrade(username, fromTag, imageTagFromRef(newImage)); err != nil {
        failed = append(failed, username+"(配置失败)")
        continue
      }
      if err := dockerpkg.Start(ctx, cid); err != nil {
        failed = append(failed, username+"(启动失败)")
        continue
      }
      auth.UpdateContainerStatus(username, "running")
    }
    success = append(success, username)
  }

  msg := fmt.Sprintf("迁移完成：%d 成功", len(success))
  if len(failed) > 0 {
    msg += fmt.Sprintf("，%d 失败：%s", len(failed), strings.Join(failed, ", "))
  }
  writeSuccess(c, msg)
}

// handleAdminImageUpgradeCandidates 查询可升级到指定版本的用户和分组
func (s *Server) handleAdminImageUpgradeCandidates(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }

  targetTag := c.Query("tag")
  if targetTag == "" {
    writeError(c, http.StatusBadRequest, "必须指定目标版本标签")
    return
  }
  newImage := s.cfg.Image.UnifiedRef(targetTag)

  containers, err := auth.GetAllContainers()
  if err != nil {
    writeError(c, http.StatusInternalServerError, "查询用户列表失败")
    return
  }

  type userInfo struct {
    Username string `json:"username"`
    Image    string `json:"image"`
    Status   string `json:"status"`
    Groups   string `json:"groups"`
  }

  var users []userInfo
  for _, ctr := range containers {
    if ctr.Image == newImage {
      continue
    }
    if !strings.Contains(ctr.Image, s.cfg.Image.RepoName()) {
      continue
    }
    groups, _ := auth.GetGroupsForUser(ctr.Username)
    groupStr := strings.Join(groups, ", ")
    users = append(users, userInfo{
      Username: ctr.Username,
      Image:    ctr.Image,
      Status:   ctr.Status,
      Groups:   groupStr,
    })
  }
  sort.Slice(users, func(i, j int) bool {
    return users[i].Username < users[j].Username
  })

  allGroups, _ := auth.ListGroups()
  type groupInfo struct {
    Name  string `json:"name"`
    Count int    `json:"count"`
  }
  groupCount := map[string]int{}
  for _, u := range users {
    gs, _ := auth.GetGroupsForUser(u.Username)
    seen := map[string]bool{}
    for _, g := range gs {
      if !seen[g] {
        groupCount[g]++
        seen[g] = true
      }
    }
  }
  var groups []groupInfo
  for _, g := range allGroups {
    if groupCount[g.Name] > 0 {
      groups = append(groups, groupInfo{Name: g.Name, Count: groupCount[g.Name]})
    }
  }
  sort.Slice(groups, func(i, j int) bool {
    return groups[i].Name < groups[j].Name
  })

  pager := parsePagination(c, 50, 500)
  if pager.Search != "" {
    filtered := users[:0]
    for _, u := range users {
      if strings.Contains(strings.ToLower(u.Username), pager.Search) ||
        strings.Contains(strings.ToLower(u.Groups), pager.Search) ||
        strings.Contains(strings.ToLower(u.Image), pager.Search) ||
        strings.Contains(strings.ToLower(u.Status), pager.Search) {
        filtered = append(filtered, u)
      }
    }
    users = filtered
  }
  users, total, totalPages, page, pageSize := paginateSlice(users, pager)

  writeJSON(c, http.StatusOK, map[string]interface{}{
    "success":     true,
    "target":      newImage,
    "users":       users,
    "groups":      groups,
    "page":        page,
    "page_size":   pageSize,
    "total":       total,
    "total_pages": totalPages,
  })
}

// handleAdminImageUpgrade 拉取新版本镜像，然后用任务队列逐步升级用户
func (s *Server) handleAdminImageUpgrade(c *gin.Context) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if !s.dockerAvailable {
    writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用")
    return
  }
  if !s.checkCSRF(c) {
    writeError(c, http.StatusForbidden, "无效请求")
    return
  }

  targetTag := c.PostForm("tag")
  if targetTag == "" {
    writeError(c, http.StatusBadRequest, "必须指定目标版本标签")
    return
  }
  newImage := s.cfg.Image.UnifiedRef(targetTag)

  // 解析目标用户列表
  var targetUsers []string
  if userList := c.PostForm("users"); userList != "" {
    for _, u := range strings.Split(userList, ",") {
      if v := strings.TrimSpace(u); v != "" {
        targetUsers = append(targetUsers, v)
      }
    }
  }

  if len(targetUsers) == 0 {
    writeError(c, http.StatusBadRequest, "必须指定要升级的用户")
    return
  }

  // 过滤出真正需要升级的用户
  var upgradeUsers []string
  fromTags := make(map[string]string)
  for _, username := range targetUsers {
    rec, _ := auth.GetContainerByUsername(username)
    if rec == nil {
      continue
    }
    if rec.Image == newImage {
      continue
    }
    if !strings.Contains(rec.Image, s.cfg.Image.RepoName()) {
      continue
    }
    fromTags[username] = imageTagFromRef(rec.Image)
    upgradeUsers = append(upgradeUsers, username)
  }

  if len(upgradeUsers) == 0 {
    writeError(c, http.StatusBadRequest, "没有需要升级的用户")
    return
  }
  migrator, err := user.NewPicoClawMigrationService(config.RuleCacheDir())
  if err != nil {
    writeError(c, http.StatusInternalServerError, err.Error())
    return
  }
  for _, username := range upgradeUsers {
    if err := migrator.EnsureUpgradeable(fromTags[username], targetTag); err != nil {
      writeError(c, http.StatusBadRequest, username+": "+err.Error())
      return
    }
  }

  // SSE 响应
  c.Writer.Header().Set("Content-Type", "text/event-stream")
  c.Writer.Header().Set("Cache-Control", "no-cache")
  c.Writer.Header().Set("Connection", "keep-alive")
  flush := func() {
    c.Writer.Flush()
  }
  sendStatus := func(status string) {
    fmt.Fprintf(c.Writer, "data: {\"status\":%q}\n\n", status)
    flush()
  }

  // 1. 同步拉取新镜像
  sendStatus("正在拉取镜像 " + newImage + " ...")
  pullRef := s.cfg.Image.PullRef(targetTag)
  ctx := context.Background()
  reader, err := dockerpkg.ImagePull(ctx, pullRef)
  if err != nil {
    fmt.Fprintf(c.Writer, "data: {\"status\":\"error\",\"error\":\"拉取失败: %s\"}\n\n", err.Error())
    flush()
    return
  }
  buf := make([]byte, 4096)
  for {
    n, err := reader.Read(buf)
    if n > 0 {
      c.Writer.Write(buf[:n])
      flush()
    }
    if err != nil {
      break
    }
  }

  // 腾讯云模式 retag
  if s.cfg.Image.IsTencent() && pullRef != newImage {
    sendStatus("重命名镜像 " + pullRef + " -> " + newImage)
    if err := dockerpkg.RetagImage(ctx, pullRef, newImage); err != nil {
      fmt.Fprintf(c.Writer, "data: {\"status\":\"error\",\"error\":\"重命名失败: %s\"}\n\n", err.Error())
      flush()
      return
    }
  }

  // 2. 提交到任务队列逐步执行
  img := newImage // 闭包捕获
  taskFn := func(username string) error {
    fromTag := fromTags[username]
    if err := auth.UpdateContainerImage(username, img); err != nil {
      return fmt.Errorf("更新失败: %w", err)
    }
    rec, _ := auth.GetContainerByUsername(username)
    if rec == nil {
      return fmt.Errorf("记录不存在")
    }
    if rec.ContainerID != "" {
      _ = dockerpkg.Stop(ctx, rec.ContainerID)
      _ = dockerpkg.Remove(ctx, rec.ContainerID)
      auth.UpdateContainerID(username, "")
    }
    if rec.Status == "running" {
      ud := user.UserDir(s.cfg, username)
      cid, createErr := dockerpkg.CreateContainer(ctx, username, img, ud, rec.IP, rec.CPULimit, rec.MemoryLimit, rec.MCPToken)
      if createErr != nil {
        return fmt.Errorf("创建失败: %w", createErr)
      }
      auth.UpdateContainerID(username, cid)
      if err := s.applyConfigForUpgrade(username, fromTag, targetTag); err != nil {
        return fmt.Errorf("配置失败: %w", err)
      }
      if err := dockerpkg.Start(ctx, cid); err != nil {
        return fmt.Errorf("启动失败: %w", err)
      }
      auth.UpdateContainerStatus(username, "running")
    }
    return nil
  }

  taskID, err := enqueueTask("upgrade", upgradeUsers, taskFn)
  if err != nil {
    fmt.Fprintf(c.Writer, "data: {\"status\":\"error\",\"error\":\"%s\"}\n\n", err.Error())
    flush()
    return
  }

  sendStatus(fmt.Sprintf("镜像就绪，已提交升级任务 %s，共 %d 个用户排队中（每 2 秒处理一个）", taskID, len(upgradeUsers)))
  fmt.Fprintf(c.Writer, "data: {\"status\":\"done\",\"message\":\"镜像拉取完成，%d 个用户已进入升级队列\",\"task_id\":%q,\"total\":%d}\n\n", len(upgradeUsers), taskID, len(upgradeUsers))
  flush()
}
