package web

import (
  "context"
  "fmt"
  "log/slog"
  "net/http"
  "os"
  "path/filepath"
  "time"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/auth"
  dockerpkg "github.com/picoaide/picoaide/internal/docker"
  "github.com/picoaide/picoaide/internal/logger"
  "github.com/picoaide/picoaide/internal/user"
)

// ============================================================
// 容器操作
// ============================================================

func (s *Server) handleAdminContainerStart(c *gin.Context) {
  s.handleContainerAction(c, "start")
}
func (s *Server) handleAdminContainerStop(c *gin.Context) {
  s.handleContainerAction(c, "stop")
}
func (s *Server) handleAdminContainerRestart(c *gin.Context) {
  s.handleContainerAction(c, "restart")
}
func (s *Server) handleAdminContainerDebug(c *gin.Context) {
  s.handleContainerAction(c, "debug")
}

func (s *Server) handleContainerAction(c *gin.Context, action string) {
  if s.requireSuperadmin(c) == "" {
    return
  }
  if !s.dockerAvailable {
    writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
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

  ctx := context.Background()

  // 启动/重启前检查镜像是否存在
  if action == "start" || action == "restart" || action == "debug" {
    if rec.Image == "" || !dockerpkg.ImageExists(ctx, rec.Image) {
      if rec.Image == "" {
        writeError(c, http.StatusBadRequest, "用户未绑定容器镜像，请先在认证配置中同步 LDAP 账号")
      } else {
        writeError(c, http.StatusBadRequest, "容器镜像 "+rec.Image+" 不存在，请先拉取镜像")
      }
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
        writeError(c, http.StatusInternalServerError, "创建容器失败: "+createErr.Error())
        return
      }
      auth.UpdateContainerID(username, cid)
      rec.ContainerID = cid
    }
    picoclawDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw")
    configPath := filepath.Join(picoclawDir, "config.json")
    shouldApplyAfterStart := false
    if _, err := os.Stat(configPath); err == nil {
      if err := s.applyConfigForImage(username, rec.Image); err != nil {
        writeError(c, http.StatusInternalServerError, "下发配置失败: "+err.Error())
        return
      }
    } else {
      shouldApplyAfterStart = true
    }
    if err := dockerpkg.Start(ctx, rec.ContainerID); err != nil {
      writeError(c, http.StatusInternalServerError, err.Error())
      return
    }
    auth.UpdateContainerStatus(username, "running")
    if shouldApplyAfterStart {
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
      writeError(c, http.StatusInternalServerError, "创建容器失败: "+createErr.Error())
      return
    }
    auth.UpdateContainerID(username, cid)
    picoclawDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw")
    configPath := filepath.Join(picoclawDir, "config.json")
    shouldApplyAfterStart := false
    if _, err := os.Stat(configPath); err == nil {
      if err := s.applyConfigForImage(username, rec.Image); err != nil {
        writeError(c, http.StatusInternalServerError, "下发配置失败: "+err.Error())
        return
      }
    } else {
      shouldApplyAfterStart = true
    }
    if err := dockerpkg.Start(ctx, cid); err != nil {
      writeError(c, http.StatusInternalServerError, err.Error())
      return
    }
    auth.UpdateContainerStatus(username, "running")
    if shouldApplyAfterStart {
      go s.applyConfigAsync(username, picoclawDir, cid)
    }

  case "debug":
    // 停止+删除旧容器，重新创建并以 Picoclaw debug 模式启动
    if rec.ContainerID != "" {
      _ = dockerpkg.Stop(ctx, rec.ContainerID)
      _ = dockerpkg.Remove(ctx, rec.ContainerID)
      auth.UpdateContainerID(username, "")
    }
    ud := user.UserDir(s.cfg, username)
    cid, createErr := dockerpkg.CreateContainerWithOptions(ctx, username, rec.Image, ud, rec.IP, rec.CPULimit, rec.MemoryLimit, true, nil)
    if createErr != nil {
      writeError(c, http.StatusInternalServerError, "创建调试容器失败: "+createErr.Error())
      return
    }
    auth.UpdateContainerID(username, cid)
    picoclawDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw")
    configPath := filepath.Join(picoclawDir, "config.json")
    shouldApplyAfterStart := false
    if _, err := os.Stat(configPath); err == nil {
      if err := s.applyConfigForImage(username, rec.Image); err != nil {
        writeError(c, http.StatusInternalServerError, "下发配置失败: "+err.Error())
        return
      }
    } else {
      shouldApplyAfterStart = true
    }
    if err := dockerpkg.Start(ctx, cid); err != nil {
      writeError(c, http.StatusInternalServerError, err.Error())
      return
    }
    auth.UpdateContainerStatus(username, "running")
    if shouldApplyAfterStart {
      go s.applyConfigAsync(username, picoclawDir, cid)
    }
  }

  logger.Audit("container."+action, "username", username, "operator", s.getSessionUser(c))
  labels := map[string]string{"start": "启动", "stop": "停止", "restart": "重启", "debug": "调试启动"}
  writeSuccess(c, fmt.Sprintf("容器已%s", labels[action]))
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
  rec, _ := auth.GetContainerByUsername(username)
  targetTag := ""
  if rec != nil {
    targetTag = imageTagFromRef(rec.Image)
  }
  if err := user.ApplyConfigToJSONForTag(s.cfg, picoclawDir, username, targetTag); err != nil {
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

func (s *Server) applyConfigForImage(username string, imageRef string) error {
  picoclawDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw")
  if err := os.MkdirAll(picoclawDir, 0755); err != nil {
    return fmt.Errorf("创建配置目录失败: %w", err)
  }
  if err := user.ApplyConfigToJSONForTag(s.cfg, picoclawDir, username, imageTagFromRef(imageRef)); err != nil {
    return fmt.Errorf("下发配置失败: %w", err)
  }
  if err := user.ApplySecurityToYAML(s.cfg, picoclawDir); err != nil {
    slog.Error("下发安全配置失败", "username", username, "error", err)
  }
  return nil
}

func (s *Server) applyConfigForUpgrade(username string, fromTag string, targetTag string) error {
  picoclawDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw")
  if err := os.MkdirAll(picoclawDir, 0755); err != nil {
    return fmt.Errorf("创建配置目录失败: %w", err)
  }
  if err := user.ApplyConfigToJSONWithMigration(s.cfg, picoclawDir, username, fromTag, targetTag); err != nil {
    return fmt.Errorf("下发配置失败: %w", err)
  }
  if err := user.ApplySecurityToYAML(s.cfg, picoclawDir); err != nil {
    slog.Error("下发安全配置失败", "username", username, "error", err)
  }
  return nil
}

// applyConfigToUser 向单个用户下发配置并重启容器
func (s *Server) applyConfigToUser(username string) error {
  picoclawDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw")
  configPath := filepath.Join(picoclawDir, "config.json")

  // config.json 不存在说明容器还没启动过，跳过
  if _, err := os.Stat(configPath); err != nil {
    return fmt.Errorf("config.json 不存在")
  }

  rec, err := auth.GetContainerByUsername(username)
  targetTag := ""
  if rec != nil {
    targetTag = imageTagFromRef(rec.Image)
  }
  if err := user.ApplyConfigToJSONForTag(s.cfg, picoclawDir, username, targetTag); err != nil {
    return fmt.Errorf("下发配置失败: %w", err)
  }
  if err := user.ApplySecurityToYAML(s.cfg, picoclawDir); err != nil {
    slog.Error("下发安全配置失败", "username", username, "error", err)
  }

  // 重启容器使配置生效
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
