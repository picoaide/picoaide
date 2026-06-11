package main

import (
  "fmt"
  "net"
  "os"
  "os/exec"
  "path/filepath"
  "strconv"

  "github.com/picoaide/picoaide/internal/store"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/rootfs"
  "github.com/picoaide/picoaide/internal/util"
  "github.com/picoaide/picoaide/internal/web"
)

const workDir = "/data/picoaide"

func printUsage() {
  fmt.Printf(`%s - PicoClaw 批量管理工具

用法:
  %s <command> [options]

命令:
  init                    首次运行全自动初始化
  serve                   启动 Web 管理面板
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
  cmdArgs := []string{}
  if len(filteredArgs) > 0 {
    command = filteredArgs[0]
    cmdArgs = filteredArgs[1:]
  }

  switch command {
  case "init":
    runInitSilent()

  case "serve":
    // 检查 /data/picoaide 是否已初始化，未初始化则自动执行 init
    if _, err := os.Stat(filepath.Join(workDir, "picoaide.db")); os.IsNotExist(err) {
      fmt.Println("检测到未初始化，正在自动执行初始化...")
      runInitSilent()
    }
    if err := rootfs.Ensure(filepath.Join(workDir, "rootfs")); err != nil {
      fmt.Fprintf(os.Stderr, "初始化沙箱 rootfs 失败: %v\n", err)
      os.Exit(1)
    }
    store.InitDB(workDir)
    config.SetEngineProvider(store.GetEngine)
    if err := web.Serve(); err != nil {
      fmt.Fprintf(os.Stderr, "Web 服务启动失败: %v\n", err)
      os.Exit(1)
    }

  case "reset-password":
    _, positional := util.ParseFlags(cmdArgs)
    if len(positional) == 0 {
      fmt.Println("用法: picoaide reset-password <用户名>")
      os.Exit(1)
    }
    wd, _ := os.Getwd()
    if err := store.InitDB(wd); err != nil {
      os.MkdirAll(wd, 0755)
      if err := store.InitDB(wd); err != nil {
        fmt.Fprintf(os.Stderr, "初始化数据库失败: %v\n", err)
        os.Exit(1)
      }
    }
    config.SetEngineProvider(store.GetEngine)
    if err := runResetPassword(positional[0]); err != nil {
      fmt.Fprintf(os.Stderr, "重置密码失败: %v\n", err)
      os.Exit(1)
    }

  default:
    printUsage()
  }
}

// ============================================================
// reset-password
// ============================================================

func runResetPassword(username string) error {
  if !store.UserExists(username) {
    return fmt.Errorf("用户 %s 不存在", username)
  }
  if !store.IsSuperadmin(username) {
    return fmt.Errorf("用户 %s 不是超管，仅支持重置超管密码", username)
  }
  password := store.GenerateRandomPassword(16)
  if err := store.ChangePassword(username, password); err != nil {
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
  if err := store.InitDB(workDir); err != nil {
    fmt.Fprintf(os.Stderr, "初始化数据库失败: %v\n", err)
    os.Exit(1)
  }
  config.SetEngineProvider(store.GetEngine)

  if err := rootfs.Ensure(filepath.Join(workDir, "rootfs")); err != nil {
    fmt.Fprintf(os.Stderr, "初始化沙箱 rootfs 失败: %v\n", err)
    os.Exit(1)
  }

  password := store.GenerateRandomPassword(16)
  if !store.UserExists("admin") {
    if err := store.CreateUser("admin", password, "superadmin"); err != nil {
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

  cfg.Web.AuthMode = "local"
  cfg.Web.Listen = ":80"
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
  fmt.Println("执行 picoaide serve 启动 Web 管理面板")
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
  if err := util.CopyFile(absPath, "/usr/sbin/picoaide"); err != nil {
    fmt.Fprintf(os.Stderr, "复制到 /usr/sbin/picoaide 失败: %v\n", err)
    os.Exit(1)
  }
  os.Chmod("/usr/sbin/picoaide", 0755)
  fmt.Println("已安装到 /usr/sbin/picoaide")
}


func checkPort(port int) bool {
  ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
  if err != nil {
    return false
  }
  ln.Close()
  return true
}

