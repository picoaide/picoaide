package web

import (
  "fmt"
  "net/http"
  "strconv"
  "strings"

  "github.com/gin-gonic/gin"
  "github.com/picoaide/picoaide/internal/scheduler"
  "github.com/picoaide/picoaide/internal/logger"
)

// ============================================================
// 定时任务管理（用户端）
// ============================================================

func (s *Server) getCronStore() *scheduler.SQLCronStore {
  if s.agentIntegration == nil {
    return nil
  }
  return s.agentIntegration.cronStore
}

func (s *Server) handleCronList(c *gin.Context) {
  username := s.getSessionUser(c)
  if username == "" {
    writeError(c, http.StatusUnauthorized, "未登录")
    return
  }
  store := s.getCronStore()
  if store == nil {
    writeError(c, http.StatusInternalServerError, "定时任务服务未就绪")
    return
  }
  jobs, err := store.ListByUser(c.Request.Context(), username)
  if err != nil {
    writeError(c, http.StatusInternalServerError, "查询失败")
    return
  }
  if jobs == nil {
    jobs = []*scheduler.CronJob{}
  }
  writeJSON(c, http.StatusOK, gin.H{"success": true, "jobs": jobs})
}

func (s *Server) handleCronCreate(c *gin.Context) {
  username := s.getSessionUser(c)
  if username == "" {
    writeError(c, http.StatusUnauthorized, "未登录")
    return
  }
  store := s.getCronStore()
  if store == nil {
    writeError(c, http.StatusInternalServerError, "定时任务服务未就绪")
    return
  }
  schedule := strings.TrimSpace(c.PostForm("schedule"))
  prompt := strings.TrimSpace(c.PostForm("prompt"))
  channelID := strings.TrimSpace(c.PostForm("channel_id"))

  if schedule == "" || prompt == "" {
    writeError(c, http.StatusBadRequest, "schedule 和 prompt 不能为空")
    return
  }

  if _, err := scheduler.ParseSchedule(schedule); err != nil {
    writeError(c, http.StatusBadRequest, fmt.Sprintf("调度格式错误: %s", err.Error()))
    return
  }

  job := &scheduler.CronJob{
    UserID:    username,
    Schedule:  schedule,
    Prompt:    prompt,
    AgentID:   "pico",
    ChannelID: channelID,
    Enabled:   true,
  }

  if err := store.Insert(c.Request.Context(), job); err != nil {
    writeError(c, http.StatusInternalServerError, "创建失败")
    return
  }

  logger.DebugProcess("cron_create", "user", username, "job_id", job.ID, "schedule", schedule)
  writeJSON(c, http.StatusOK, gin.H{"success": true, "job": job})
}

func (s *Server) handleCronUpdate(c *gin.Context) {
  username := s.getSessionUser(c)
  if username == "" {
    writeError(c, http.StatusUnauthorized, "未登录")
    return
  }
  store := s.getCronStore()
  if store == nil {
    writeError(c, http.StatusInternalServerError, "定时任务服务未就绪")
    return
  }

  idStr := strings.TrimSpace(c.PostForm("id"))
  id, err := strconv.ParseInt(idStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的任务 ID")
    return
  }

  existing, err := store.GetByID(c.Request.Context(), id)
  if err != nil || existing == nil {
    writeError(c, http.StatusNotFound, "任务不存在")
    return
  }
  if existing.UserID != username {
    writeError(c, http.StatusForbidden, "无权操作该任务")
    return
  }

  schedule := strings.TrimSpace(c.PostForm("schedule"))
  prompt := strings.TrimSpace(c.PostForm("prompt"))
  channelID := strings.TrimSpace(c.PostForm("channel_id"))
  if schedule == "" {
    schedule = existing.Schedule
  }
  if prompt == "" {
    prompt = existing.Prompt
  }
  if channelID == "" {
    channelID = existing.ChannelID
  }

  if _, err := scheduler.ParseSchedule(schedule); err != nil {
    writeError(c, http.StatusBadRequest, fmt.Sprintf("调度格式错误: %s", err.Error()))
    return
  }

  existing.Schedule = schedule
  existing.Prompt = prompt
  existing.ChannelID = channelID

  if err := store.Update(c.Request.Context(), existing); err != nil {
    writeError(c, http.StatusInternalServerError, "更新失败")
    return
  }

  logger.DebugProcess("cron_update", "user", username, "job_id", id)
  writeJSON(c, http.StatusOK, gin.H{"success": true, "job": existing})
}

func (s *Server) handleCronDelete(c *gin.Context) {
  username := s.getSessionUser(c)
  if username == "" {
    writeError(c, http.StatusUnauthorized, "未登录")
    return
  }
  store := s.getCronStore()
  if store == nil {
    writeError(c, http.StatusInternalServerError, "定时任务服务未就绪")
    return
  }

  idStr := strings.TrimSpace(c.PostForm("id"))
  id, err := strconv.ParseInt(idStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的任务 ID")
    return
  }

  existing, err := store.GetByID(c.Request.Context(), id)
  if err != nil || existing == nil {
    writeError(c, http.StatusNotFound, "任务不存在")
    return
  }
  if existing.UserID != username {
    writeError(c, http.StatusForbidden, "无权操作该任务")
    return
  }

  if err := store.Delete(c.Request.Context(), id); err != nil {
    writeError(c, http.StatusInternalServerError, "删除失败")
    return
  }

  logger.DebugProcess("cron_delete", "user", username, "job_id", id)
  writeJSON(c, http.StatusOK, gin.H{"success": true})
}

func (s *Server) handleCronToggle(c *gin.Context) {
  username := s.getSessionUser(c)
  if username == "" {
    writeError(c, http.StatusUnauthorized, "未登录")
    return
  }
  store := s.getCronStore()
  if store == nil {
    writeError(c, http.StatusInternalServerError, "定时任务服务未就绪")
    return
  }

  idStr := strings.TrimSpace(c.PostForm("id"))
  id, err := strconv.ParseInt(idStr, 10, 64)
  if err != nil {
    writeError(c, http.StatusBadRequest, "无效的任务 ID")
    return
  }

  existing, err := store.GetByID(c.Request.Context(), id)
  if err != nil || existing == nil {
    writeError(c, http.StatusNotFound, "任务不存在")
    return
  }
  if existing.UserID != username {
    writeError(c, http.StatusForbidden, "无权操作该任务")
    return
  }

  existing.Enabled = !existing.Enabled
  if err := store.Update(c.Request.Context(), existing); err != nil {
    writeError(c, http.StatusInternalServerError, "更新失败")
    return
  }

  logger.DebugProcess("cron_toggle", "user", username, "job_id", id, "enabled", existing.Enabled)
  writeJSON(c, http.StatusOK, gin.H{"success": true, "enabled": existing.Enabled})
}
