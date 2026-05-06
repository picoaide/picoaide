package logger

import (
  "io"
  "log/slog"
  "os"
  "path/filepath"
  "runtime"
  "strings"
  "sync"
  "time"

  "gopkg.in/natefinch/lumberjack.v2"
)

var (
  once     sync.Once
  instance *logManager
)

type logManager struct {
  writer  *lumberjack.Logger
  isDev   bool
  handler slog.Handler
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

// Init 初始化日志系统。dataDir 为数据目录，retention 为保留策略，isDev 为开发者模式。
func Init(dataDir string, retention string, isDev bool) {
  once.Do(func() {
    days := RetentionDays(retention)
    logsDir := filepath.Join(dataDir, "logs")
    os.MkdirAll(logsDir, 0755)

    isDev = isDev || os.Getenv("PICOAIDE_DEV") == "1"

    lw := &lumberjack.Logger{
      Filename:   filepath.Join(logsDir, "picoaide.log"),
      MaxSize:    100, // MB
      MaxBackups: 0,   // 不限制备份数（由 MaxAge 控制）
      MaxAge:     days,
      Compress:   true,
      LocalTime:  true,
    }

    // 同时写文件和控制台
    multiWriter := io.MultiWriter(lw, os.Stdout)

    var handler slog.Handler
    if isDev {
      handler = slog.NewJSONHandler(multiWriter, &slog.HandlerOptions{
        Level: slog.LevelDebug,
        ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
          if a.Key == slog.SourceKey {
            s := a.Value.Any().(*slog.Source)
            s.Function = ""
            s.File = filepath.Base(s.File)
          }
          return a
        },
      })
    } else {
      handler = slog.NewJSONHandler(multiWriter, &slog.HandlerOptions{
        Level: slog.LevelInfo,
      })
    }

    instance = &logManager{
      writer:  lw,
      isDev:   isDev,
      handler: handler,
    }

    slog.SetDefault(slog.New(handler))
    slog.Info("日志系统已初始化",
      "dir", logsDir,
      "retention", retention,
      "dev", isDev,
    )
  })
}

// Close 关闭日志写入器
func Close() {
  if instance != nil && instance.writer != nil {
    instance.writer.Close()
  }
}

// IsDev 返回是否为开发者模式
func IsDev() bool {
  return instance != nil && instance.isDev
}

// ErrorStack 记录错误日志，dev 模式附带调用栈
func ErrorStack(msg string, args ...any) {
  if IsDev() {
    buf := make([]byte, 4096)
    n := runtime.Stack(buf, false)
    args = append(args, "stack", strings.TrimSpace(string(buf[:n])))
  }
  slog.Error(msg, args...)
}

// Audit 记录审计日志
func Audit(action string, args ...any) {
  allArgs := []any{"type", "AUDIT", "action", action}
  allArgs = append(allArgs, args...)
  slog.Info("audit", allArgs...)
}

// Access 记录访问日志
func Access(method, path string, status int, duration time.Duration, ip, user string) {
  slog.Info("access",
    "type", "ACCESS",
    "method", method,
    "path", path,
    "status", status,
    "duration", duration.String(),
    "ip", ip,
    "user", user,
  )
}
