package logger

import (
  "context"
  "io"
  "log/slog"
  "os"
  "path/filepath"
  "sync"

  "gopkg.in/natefinch/lumberjack.v2"
)

var (
  once     sync.Once
  instance *logManager
)

type logManager struct {
  writer      *lumberjack.Logger
  debugWriter *lumberjack.Logger
  isDev       bool
  debugMode   *bool // 指针，支持运行时切换
  handler     slog.Handler
}

// RetentionDays 将配置值转为保留天数，0 表示永久
func RetentionDays(val string) int {
  switch val {
  case "1m":
    return 30
  case "3m":
    return 90
  case "6m":
    return 180
  case "1y":
    return 365
  case "3y":
    return 1095
  case "5y":
    return 1825
  case "forever":
    return 0
  default:
    return 180
  }
}

// debugHandler 自定义 handler：所有日志写入主日志，debug 模式下同时写入 debug.log
type debugHandler struct {
  mainHandler  slog.Handler
  debugHandler slog.Handler
  debugMode    *bool // 指针，支持运行时切换
}

func (h *debugHandler) Enabled(ctx context.Context, level slog.Level) bool {
  // debug 模式下放行 DEBUG 级别
  if h.debugMode != nil && *h.debugMode && level >= slog.LevelDebug {
    return true
  }
  return h.mainHandler.Enabled(ctx, level)
}

func (h *debugHandler) Handle(ctx context.Context, r slog.Record) error {
  // 仅当日志级别满足 mainHandler 的 Enabled 条件时才写入主日志
  if h.mainHandler.Enabled(ctx, r.Level) {
    _ = h.mainHandler.Handle(ctx, r)
  }
  // debug 模式下同时写入 debug.log（记录所有级别，包括 DEBUG）
  if h.debugMode == nil || *h.debugMode {
    _ = h.debugHandler.Handle(ctx, r)
  }
  return nil
}

func (h *debugHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
  return &debugHandler{
    mainHandler:  h.mainHandler.WithAttrs(attrs),
    debugHandler: h.debugHandler.WithAttrs(attrs),
    debugMode:    h.debugMode,
  }
}

func (h *debugHandler) WithGroup(name string) slog.Handler {
  return &debugHandler{
    mainHandler:  h.mainHandler.WithGroup(name),
    debugHandler: h.debugHandler.WithGroup(name),
    debugMode:    h.debugMode,
  }
}

// Init 初始化日志系统。dataDir 为数据目录，retention 为保留策略，isDev 为开发者模式，level 为日志级别，debugMode 为调试模式。
func Init(dataDir string, retention string, isDev bool, level string, debugMode bool) {
  once.Do(func() {
    days := RetentionDays(retention)
    logsDir := filepath.Join(dataDir, "logs")
    os.MkdirAll(logsDir, 0755)

    isDev = isDev || os.Getenv("PICOAIDE_DEV") == "1"

    // 主日志写入器
    lw := &lumberjack.Logger{
      Filename:   filepath.Join(logsDir, "picoaide.log"),
      MaxSize:    100, // MB
      MaxBackups: 0,   // 不限制备份数（由 MaxAge 控制）
      MaxAge:     days,
      Compress:   true,
      LocalTime:  true,
    }

    multiWriter := io.MultiWriter(lw, os.Stdout)

    logLevel := parseLevel(level)
    if isDev && logLevel > slog.LevelDebug {
      logLevel = slog.LevelDebug
    }

    mainHandler := slog.NewJSONHandler(multiWriter, &slog.HandlerOptions{
      Level:     logLevel,
      AddSource: true,
      ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
        if a.Key == slog.SourceKey {
          if s, ok := a.Value.Any().(*slog.Source); ok {
            s.Function = ""
            s.File = filepath.Base(s.File)
          }
        }
        return a
      },
    })

    // debug.log 写入器：始终创建，支持运行时切换
    dw := &lumberjack.Logger{
      Filename:   filepath.Join(logsDir, "debug.log"),
      MaxSize:    200, // debug 日志更大，记录更多细节
      MaxBackups: 0,
      MaxAge:     days,
      Compress:   true,
      LocalTime:  true,
    }
    debugWriter := io.MultiWriter(dw, os.Stdout)
    debugHdlr := slog.NewJSONHandler(debugWriter, &slog.HandlerOptions{
      Level:     slog.LevelDebug, // debug.log 记录所有级别
      AddSource: true,
      ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
        if a.Key == slog.SourceKey {
          if s, ok := a.Value.Any().(*slog.Source); ok {
            s.Function = ""
            s.File = filepath.Base(s.File)
          }
        }
        return a
      },
    })

    // 组合 handler，debugMode 使用指针支持运行时切换
    dm := debugMode
    combinedHandler := &debugHandler{
      mainHandler:  mainHandler,
      debugHandler: debugHdlr,
      debugMode:    &dm,
    }

    instance = &logManager{
      writer:      lw,
      debugWriter: dw,
      isDev:       isDev,
      debugMode:   &dm,
      handler:     combinedHandler,
    }

    slog.SetDefault(slog.New(combinedHandler))
    slog.Info("日志系统已初始化",
      "dir", logsDir,
      "retention", retention,
      "dev", isDev,
      "debug_mode", dm,
    )
  })
}

// EnableDebug 启用调试模式（运行时切换）
func EnableDebug() {
  if instance != nil && instance.debugMode != nil {
    *instance.debugMode = true
  }
}

// DisableDebug 禁用调试模式（运行时切换）
func DisableDebug() {
  if instance != nil && instance.debugMode != nil {
    *instance.debugMode = false
  }
}

// IsDebug 返回当前调试模式状态
func IsDebug() bool {
  if instance != nil && instance.debugMode != nil {
    return *instance.debugMode
  }
  return false
}

// Close 关闭日志写入器
func Close() {
  if instance != nil {
    if instance.writer != nil {
      instance.writer.Close()
    }
    if instance.debugWriter != nil {
      instance.debugWriter.Close()
    }
  }
}

func parseLevel(val string) slog.Level {
  switch val {
  case "debug":
    return slog.LevelDebug
  case "warn":
    return slog.LevelWarn
  case "error":
    return slog.LevelError
  default:
    return slog.LevelInfo
  }
}

// Audit 记录审计日志
func Audit(action string, args ...any) {
  allArgs := []any{"type", "AUDIT", "action", action}
  allArgs = append(allArgs, args...)
  slog.Info("audit", allArgs...)
}

// Debug 记录调试日志（仅在 debug 模式下有意义，会同时写入 debug.log）
func Debug(msg string, args ...any) {
  slog.Debug(msg, args...)
}

// DebugOp 记录操作调试日志，自动添加 "op" 标签标识操作类型
func DebugOp(operation string, args ...any) {
  allArgs := []any{"op", operation}
  allArgs = append(allArgs, args...)
  slog.Debug("operation", allArgs...)
}

// DebugRecv 记录接收到的请求调试日志
func DebugRecv(method, path string, args ...any) {
  allArgs := []any{"event", "recv", "method", method, "path", path}
  allArgs = append(allArgs, args...)
  slog.Debug("request", allArgs...)
}

// DebugSend 记录发送响应调试日志
func DebugSend(method, path string, status int, args ...any) {
  allArgs := []any{"event", "send", "method", method, "path", path, "status", status}
  allArgs = append(allArgs, args...)
  slog.Debug("response", allArgs...)
}

// DebugProcess 记录处理过程中的调试日志
func DebugProcess(phase string, args ...any) {
  allArgs := []any{"event", "process", "phase", phase}
  allArgs = append(allArgs, args...)
  slog.Debug("process", allArgs...)
}
