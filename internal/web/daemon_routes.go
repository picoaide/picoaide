package web

import "github.com/gin-gonic/gin"

// ============================================================
// Daemon 路由注册
// ============================================================

// registerDaemonRoutes 注册普通用户 daemon 相关路由
// 调用方应将 r 绑定在 /api/user 路径下
func (s *Server) registerDaemonRoutes(r *gin.RouterGroup) {
  // 任务管理
  r.POST("/task/submit", s.handleTaskSubmit)
  r.POST("/task/pause", s.handleTaskPause)
  r.POST("/task/resume", s.handleTaskResume)
  r.POST("/task/cancel", s.handleTaskCancel)
  r.POST("/task/message", s.handleTaskMessage)
  r.GET("/task/detail", s.handleTaskDetail)
  r.GET("/task/events", s.handleTaskEvents)
  r.GET("/task/list", s.handleTaskList)

  // 事件流
  r.GET("/events/stream", s.handleTaskEventStream)

  // Daemon 控制
  r.GET("/daemon/status", s.handleDaemonStatus)
  r.POST("/daemon/restart", s.handleDaemonRestart)
  r.POST("/daemon/stop", s.handleDaemonStop)
}

// registerAdminDaemonRoutes 注册超管 daemon 相关路由
// 调用方应将 r 绑定在 /api/admin 路径下
func (s *Server) registerAdminDaemonRoutes(r *gin.RouterGroup) {
  r.GET("/daemons", s.handleAdminListDaemons)
  r.GET("/tasks", s.handleAdminListTasks)
  r.GET("/tasks/stats", s.handleAdminTaskStats)
}
