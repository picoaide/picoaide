package main

import (
  "crypto/rand"
  "fmt"
  "io"
  "net"
  "os"
  "os/exec"
  "path/filepath"
  "strconv"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  dockerpkg "github.com/picoaide/picoaide/internal/docker"
  "github.com/picoaide/picoaide/internal/logger"
  "github.com/picoaide/picoaide/internal/user"
  "github.com/picoaide/picoaide/internal/util"
  "github.com/picoaide/picoaide/internal/web"
)

const workDir = "/data/picoaide"

func printUsage() {
  fmt.Printf(`%s - PicoClaw 批量管理工具

用法:
  %s <command> [options]

命令:
  init                    静默全自动初始化
  reset-password <用户>   重置超管密码

全局选项:
  -h, --help        显示帮助信息
`, config.AppName, config.AppName)
}

func main() {
  if os.Geteuid() != 0 {
    fmt.Fprintln(os.Stderr, "错误: picoaide 必须以 root 用户运行")
    os.Exit(1)
  }

  if err := os.Chdir(workDir); err != nil {
    os.MkdirAll(workDir, 0755)
    os.Chdir(workDir)
  }

  filteredArgs := os.Args[1:]
  for _, arg := range filteredArgs {
    if arg == "-h" || arg == "--help" {
      printUsage()
      os.Exit(0)
    }
  }

  command := ""
  if len(filteredArgs) > 0 {
    command = filteredArgs[0]
  }
  cmdArgs := filteredArgs[1:]

  switch command {
  case "init":
    runInitSilent()

  case "reset-password":
    _, positional := util.ParseFlags(cmdArgs)
    if len(positional) == 0 {
      fmt.Println("用法: picoaide reset-password <用户名>")
      os.Exit(1)
    }
    wd, _ := os.Getwd()
    if err := auth.InitDB(wd); err != nil {
      os.MkdirAll(wd, 0755)
      if err := auth.InitDB(wd); err != nil {
        fmt.Fprintf(os.Stderr, "初始化数据库失败: %v\n", err)
        os.Exit(1)
      }
    }
    if err := runResetPassword(positional[0]); err != nil {
      fmt.Fprintf(os.Stderr, "重置密码失败: %v\n", err)
      os.Exit(1)
    }

  default:
    wd, _ := os.Getwd()
    if err := auth.InitDB(wd); err != nil {
      os.MkdirAll(wd, 0755)
      if err := auth.InitDB(wd); err != nil {
        fmt.Fprintf(os.Stderr, "初始化数据库失败: %v\n", err)
        os.Exit(1)
      }
    }

    count, _ := config.SettingsCount()
    if count == 0 {
      if err := config.InitDBDefaults(); err != nil {
        fmt.Fprintf(os.Stderr, "初始化默认配置失败: %v\n", err)
        os.Exit(1)
      }
    }

  cfg, err := config.LoadFromDB()
  if err != nil {
    fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
    os.Exit(1)
  }
  if err := user.ReleasePicoClawMigrationRulesCacheIfValid(config.RuleCacheDir()); err != nil {
    fmt.Fprintf(os.Stderr, "警告: 初始化迁移规则缓存失败: %v\n", err)
  }

  wd, _ = os.Getwd()
    retention := cfg.Web.LogRetention
    if retention == "" {
      retention = "6m"
    }
    logger.Init(wd, retention, false, cfg.Web.LogLevel)
    defer logger.Close()

    if err := web.Serve(cfg); err != nil {
      fmt.Fprintf(os.Stderr, "Web 服务启动失败: %v\n", err)
      os.Exit(1)
    }
  }
}

// ============================================================
// reset-password（非交互式，仅超管）
// ============================================================

func runResetPassword(username string) error {
  if !auth.UserExists(username) {
    return fmt.Errorf("用户 %s 不存在", username)
  }
  if !auth.IsSuperadmin(username) {
    return fmt.Errorf("用户 %s 不是超管，仅支持重置超管密码", username)
  }

  password := generatePassword(16)
  if err := auth.ChangePassword(username, password); err != nil {
    return err
  }
  fmt.Fprintf(os.Stderr, "用户 %s 密码已重置: %s\n", username, password)
  return nil
}

// ============================================================
// 静默初始化
// ============================================================

func runInitSilent() {
  fmt.Println("=== PicoAide 静默初始化 ===")

  if _, err := exec.LookPath("systemctl"); err != nil {
    fmt.Fprintf(os.Stderr, "错误: 未找到 systemctl，请确认 systemd 可用: %v\n", err)
    os.Exit(1)
  }

  if err := dockerpkg.InitClient(); err != nil {
    fmt.Fprintf(os.Stderr, "错误: Docker 不可用: %v\n", err)
    os.Exit(1)
  }
  dockerpkg.CloseClient()

  if !checkPort(80) {
    fmt.Fprintf(os.Stderr, "错误: 端口 80 已被占用，请先释放\n")
    os.Exit(1)
  }

  if entries, err := os.ReadDir(workDir); err == nil && len(entries) > 0 {
    fmt.Fprintf(os.Stderr, "错误: %s 已存在且非空，请清理后重试\n", workDir)
    os.Exit(1)
  }

  ensureBinaryInstalled()

  os.MkdirAll(filepath.Join(workDir, "users"), 0755)
  os.MkdirAll(filepath.Join(workDir, "archive"), 0755)
  os.MkdirAll(filepath.Join(workDir, "rules"), 0755)

  os.Chdir(workDir)
  if err := auth.InitDB(workDir); err != nil {
    fmt.Fprintf(os.Stderr, "初始化数据库失败: %v\n", err)
    os.Exit(1)
  }

  password := generatePassword(16)
  if !auth.UserExists("admin") {
    if err := auth.CreateUser("admin", password, "superadmin"); err != nil {
      fmt.Fprintf(os.Stderr, "创建超管失败: %v\n", err)
      os.Exit(1)
    }
  }

  secretPath := filepath.Join(workDir, "secret")
  if err := os.WriteFile(secretPath, []byte(password), 0600); err != nil {
    fmt.Fprintf(os.Stderr, "写入 secret 文件失败: %v\n", err)
    os.Exit(1)
  }
  fmt.Printf("超管密码已写入: %s\n", secretPath)

  if err := config.InitDBDefaults(); err != nil {
    fmt.Fprintf(os.Stderr, "初始化默认配置失败: %v\n", err)
    os.Exit(1)
  }
  cfg, err := config.LoadFromDB()
  if err != nil {
    fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
    os.Exit(1)
  }

  if err := user.ReleasePicoClawMigrationRulesCache(config.RuleCacheDir()); err != nil {
    fmt.Fprintf(os.Stderr, "警告: 初始化迁移规则缓存失败: %v\n", err)
  }

  cfg.Web.AuthMode = "local"
  cfg.Web.Listen = ":80"
  cfg.Image.Registry = "tencent"
  if err := config.SaveToDB(cfg, "system"); err != nil {
    fmt.Fprintf(os.Stderr, "保存配置失败: %v\n", err)
    os.Exit(1)
  }

  if err := config.InstallService(cfg); err != nil {
    fmt.Fprintf(os.Stderr, "服务安装失败: %v\n", err)
    os.Exit(1)
  }

  fmt.Println("=== 初始化完成 ===")
  fmt.Printf("数据目录: %s\n", workDir)
  fmt.Printf("监听地址: %s\n", cfg.Web.Listen)
  fmt.Println("超管首次登录成功后，secret 文件将被自动删除")
  fmt.Println("执行 picoaide 启动 Web 管理面板")
}

func ensureBinaryInstalled() {
  selfPath, err := os.Executable()
  if err != nil {
    fmt.Fprintf(os.Stderr, "获取自身路径失败: %v\n", err)
    os.Exit(1)
  }
  absPath, _ := filepath.Abs(selfPath)
  if absPath == "/usr/sbin/picoaide" {
    return
  }
  if err := copyFile(absPath, "/usr/sbin/picoaide"); err != nil {
    fmt.Fprintf(os.Stderr, "复制到 /usr/sbin/picoaide 失败: %v\n", err)
    os.Exit(1)
  }
  os.Chmod("/usr/sbin/picoaide", 0755)
  fmt.Println("已安装到 /usr/sbin/picoaide")
}

func copyFile(src, dst string) error {
  in, err := os.Open(src)
  if err != nil {
    return err
  }
  defer in.Close()
  out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
  if err != nil {
    return err
  }
  _, err = io.Copy(out, in)
  if cerr := out.Close(); cerr != nil && err == nil {
    err = cerr
  }
  return err
}

func generatePassword(length int) string {
  b := make([]byte, length)
  if _, err := rand.Read(b); err != nil {
    fmt.Fprintf(os.Stderr, "生成密码失败: %v\n", err)
    os.Exit(1)
  }
  chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
  for i := range b {
    b[i] = chars[int(b[i])%len(chars)]
  }
  return string(b)
}

func checkPort(port int) bool {
  ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
  if err != nil {
    return false
  }
  ln.Close()
  return true
}
