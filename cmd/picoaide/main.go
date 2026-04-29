package main

import (
  "bufio"
  "fmt"
  "os"
  "path/filepath"
  "strings"
  "syscall"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  "github.com/picoaide/picoaide/internal/ldap"
  "github.com/picoaide/picoaide/internal/user"
  "github.com/picoaide/picoaide/internal/web"
  "github.com/picoaide/picoaide/internal/util"
  "golang.org/x/term"
)

// ============================================================
// CLI 入口
// ============================================================

func printUsage() {
  fmt.Printf(`%s - PicoClaw 批量管理工具

用法:
  %s <command> [options]

命令:
  init [-user <name>]         首次运行引导 / 初始化用户目录
  reset-password <username>   重置本地用户密码
  serve [-listen :80]         启动 Web 管理面板

全局选项:
  -config <path>    指定配置文件路径
  -h, --help        显示帮助信息
`, config.AppName, config.AppName)
}

func main() {
  if os.Geteuid() != 0 {
    fmt.Fprintln(os.Stderr, "错误: picoaide 必须以 root 用户运行")
    os.Exit(1)
  }

  if len(os.Args) < 2 {
    printUsage()
    os.Exit(1)
  }

  hcfg, err := config.LoadHome()
  if err != nil {
    fmt.Fprintf(os.Stderr, "警告: %v\n", err)
  }
  if hcfg != nil && hcfg.WorkDir != "" {
    if err := os.Chdir(hcfg.WorkDir); err != nil {
      os.MkdirAll(hcfg.WorkDir, 0755)
      if err := os.Chdir(hcfg.WorkDir); err != nil {
        fmt.Fprintf(os.Stderr, "警告: 无法切换到工作目录 %s: %v\n", hcfg.WorkDir, err)
      }
    }
  }

  var configPathOverride string
  filteredArgs := os.Args[1:]
  for i, arg := range filteredArgs {
    if arg == "-config" && i+1 < len(filteredArgs) {
      configPathOverride = filteredArgs[i+1]
      filteredArgs = append(filteredArgs[:i], filteredArgs[i+2:]...)
      break
    }
  }

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

  if command == "init" {
    runInitEarly(cmdArgs)
    return
  }

  var configPath string
  if configPathOverride != "" {
    configPath = configPathOverride
  }

  // 初始化数据库
  wd, _ := os.Getwd()
  if err := auth.InitDB(wd); err != nil {
    fmt.Fprintf(os.Stderr, "警告: 数据库初始化失败，正在重试: %v\n", err)
    os.MkdirAll(wd, 0755)
    if err := auth.InitDB(wd); err != nil {
      fmt.Fprintf(os.Stderr, "初始化数据库失败: %v\n", err)
      os.Exit(1)
    }
  }

  // 配置初始化
  count, err := config.SettingsCount()
  if err != nil {
    fmt.Fprintf(os.Stderr, "检查配置失败: %v\n", err)
    os.Exit(1)
  }
  if count == 0 {
    if configPath != "" {
      if _, err := os.Stat(configPath); err == nil {
        fmt.Printf("检测到配置文件 %s，正在迁移到数据库...\n", configPath)
        if err := config.MigrateFromYAML(configPath); err != nil {
          fmt.Fprintf(os.Stderr, "迁移配置失败: %v\n", err)
          os.Exit(1)
        }
      }
    } else if _, err := os.Stat("config.yaml"); err == nil {
      fmt.Println("检测到 config.yaml，正在迁移到数据库...")
      if err := config.MigrateFromYAML("config.yaml"); err != nil {
        fmt.Fprintf(os.Stderr, "迁移配置失败: %v\n", err)
        os.Exit(1)
      }
    } else {
      fmt.Println("首次运行，初始化默认配置...")
      if err := config.InitDBDefaults(); err != nil {
        fmt.Fprintf(os.Stderr, "初始化默认配置失败: %v\n", err)
        os.Exit(1)
      }
    }
  }

  cfg, err := config.LoadFromDB()
  if err != nil {
    fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
    os.Exit(1)
  }

  switch command {
  case "serve":
    flags, _ := util.ParseFlags(cmdArgs)
    listenAddr := flags["-listen"]
    if err := web.Serve(cfg, listenAddr); err != nil {
      fmt.Fprintf(os.Stderr, "Web 服务启动失败: %v\n", err)
      os.Exit(1)
    }

  case "reset-password":
    _, positional := util.ParseFlags(cmdArgs)
    if len(positional) == 0 {
      fmt.Println("用法: picoaide reset-password <username>")
      os.Exit(1)
    }
    if err := runResetPassword(positional[0]); err != nil {
      fmt.Fprintf(os.Stderr, "重置密码失败: %v\n", err)
      os.Exit(1)
    }

  default:
    fmt.Printf("未知命令: %s\n", command)
    printUsage()
    os.Exit(1)
  }
}

// ============================================================
// reset-password 命令
// ============================================================

func runResetPassword(username string) error {
  if !auth.UserExists(username) {
    return fmt.Errorf("用户 %s 不存在", username)
  }

  for {
    fmt.Print("新密码: ")
    pwdBytes, err := term.ReadPassword(int(syscall.Stdin))
    if err != nil {
      return fmt.Errorf("读取密码失败: %w", err)
    }
    fmt.Println()

    if len(pwdBytes) < 6 {
      fmt.Println("密码至少 6 位，请重新输入")
      continue
    }

    fmt.Print("确认密码: ")
    confirmBytes, err := term.ReadPassword(int(syscall.Stdin))
    if err != nil {
      return fmt.Errorf("读取密码失败: %w", err)
    }
    fmt.Println()

    if string(pwdBytes) != string(confirmBytes) {
      fmt.Println("两次密码不一致，请重新输入")
      continue
    }

    if err := auth.ChangePassword(username, string(pwdBytes)); err != nil {
      return err
    }

    fmt.Printf("用户 %s 密码已重置\n", username)
    return nil
  }
}

// ============================================================
// init 命令逻辑
// ============================================================

func runInitEarly(cmdArgs []string) {
  flags, _ := util.ParseFlags(cmdArgs)
  targetUser := flags["-user"]

  if targetUser != "" {
    wd, _ := os.Getwd()
    if err := auth.InitDB(wd); err != nil {
      fmt.Fprintf(os.Stderr, "初始化数据库失败: %v\n", err)
      os.Exit(1)
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
    if err := user.InitUser(cfg, targetUser, flags["-tag"]); err != nil {
      fmt.Fprintf(os.Stderr, "初始化用户 %s 失败: %v\n", targetUser, err)
      os.Exit(1)
    }
    fmt.Printf("用户 %s 初始化完成\n", targetUser)
    return
  }

  hcfg, _ := config.LoadHome()
  isFirstRun := hcfg == nil || hcfg.WorkDir == ""

  if isFirstRun {
    reader := bufio.NewReader(os.Stdin)
    runFirstRun(reader)
    return
  }

  wd, _ := os.Getwd()
  if err := auth.InitDB(wd); err != nil {
    fmt.Fprintf(os.Stderr, "初始化数据库失败: %v\n", err)
    os.Exit(1)
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
  runInitExisting(cfg)
}

func runFirstRun(reader *bufio.Reader) {
  fmt.Println("=== PicoAide 首次运行引导 ===")
  fmt.Println()

  fmt.Print("请输入数据目录 (默认: /data/picoaide): ")
  dataDir, _ := reader.ReadString('\n')
  dataDir = strings.TrimSpace(dataDir)
  if dataDir == "" {
    dataDir = "/data/picoaide"
  }

  if err := os.MkdirAll(dataDir, 0755); err != nil {
    fmt.Fprintf(os.Stderr, "创建数据目录失败: %v\n", err)
    os.Exit(1)
  }
  os.MkdirAll(filepath.Join(dataDir, "users"), 0755)
  os.MkdirAll(filepath.Join(dataDir, "archive"), 0755)

  hcfg, _ := config.LoadHome()
  if hcfg == nil {
    hcfg = &config.HomeConfig{}
  }
  hcfg.WorkDir = dataDir
  if err := config.SaveHome(hcfg); err != nil {
    fmt.Fprintf(os.Stderr, "保存 home 配置失败: %v\n", err)
    os.Exit(1)
  }

  if err := os.Chdir(dataDir); err != nil {
    fmt.Fprintf(os.Stderr, "切换目录失败: %v\n", err)
    os.Exit(1)
  }

  fmt.Printf("数据目录: %s\n", dataDir)
  fmt.Println()

  if err := auth.InitDB(dataDir); err != nil {
    fmt.Fprintf(os.Stderr, "初始化数据库失败: %v\n", err)
    os.Exit(1)
  }

  fmt.Print("是否启用员工统一登录 (LDAP)? [Y/n]: ")
  ldapAnswer, _ := reader.ReadString('\n')
  ldapAnswer = strings.TrimSpace(strings.ToLower(ldapAnswer))

  if ldapAnswer == "n" || ldapAnswer == "no" {
    if err := config.InitDBDefaults(); err != nil {
      fmt.Fprintf(os.Stderr, "初始化默认配置失败: %v\n", err)
      os.Exit(1)
    }

    cfg, err := config.LoadFromDB()
    if err != nil {
      fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
      os.Exit(1)
    }

    falseVal := false
    cfg.Web.LDAPEnabled = &falseVal

    if err := config.SaveToDB(cfg, "system"); err != nil {
      fmt.Fprintf(os.Stderr, "保存配置失败: %v\n", err)
      os.Exit(1)
    }

    if err := setupSuperAdmin(reader, dataDir); err != nil {
      fmt.Fprintf(os.Stderr, "超管设置失败: %v\n", err)
      os.Exit(1)
    }

    if err := config.InstallService(cfg); err != nil {
      fmt.Fprintf(os.Stderr, "服务安装失败: %v\n", err)
      os.Exit(1)
    }

    fmt.Println()
    fmt.Println("=== 初始化完成 ===")
    fmt.Printf("数据目录: %s\n", dataDir)
    fmt.Println("服务已启动，可通过浏览器访问 API")
  } else {
    if err := config.InitDBDefaults(); err != nil {
      fmt.Fprintf(os.Stderr, "初始化默认配置失败: %v\n", err)
      os.Exit(1)
    }

    fmt.Println()
    fmt.Println("已初始化默认配置。")
    fmt.Println("请通过管理面板修改 LDAP 连接信息后，重新运行: picoaide init")
  }
}

func runInitExisting(cfg *config.GlobalConfig) {
  fmt.Println("=== PicoAide 初始化 ===")

  if cfg.LDAPEnabled() {
    fmt.Print("验证 LDAP 连接... ")
    users, err := ldap.FetchUsers(cfg)
    if err != nil {
      fmt.Printf("失败: %v\n", err)
      fmt.Fprintf(os.Stderr, "LDAP 连接失败，请检查配置\n")
      os.Exit(1)
    }
    fmt.Printf("成功（获取到 %d 个用户）\n", len(users))
  }

  if err := config.InstallService(cfg); err != nil {
    fmt.Fprintf(os.Stderr, "警告: 服务安装失败: %v\n", err)
  }

  fmt.Println("初始化完成")
}

// ============================================================
// 超管设置
// ============================================================

func setupSuperAdmin(reader *bufio.Reader, dataDir string) error {
  if auth.HasAnySuperadmin() {
    fmt.Println("系统中已存在超管账户，跳过创建")
    return nil
  }

  fmt.Println("=== 设置超级管理员 ===")

  fmt.Print("管理员用户名 (默认: admin): ")
  username, _ := reader.ReadString('\n')
  username = strings.TrimSpace(username)
  if username == "" {
    username = "admin"
  }

  if auth.UserExists(username) {
    return fmt.Errorf("用户 %s 已存在", username)
  }

  for {
    fmt.Print("密码: ")
    pwdBytes, err := term.ReadPassword(int(syscall.Stdin))
    if err != nil {
      return fmt.Errorf("读取密码失败: %w", err)
    }
    fmt.Println()

    if len(pwdBytes) < 6 {
      fmt.Println("密码至少 6 位，请重新输入")
      continue
    }

    fmt.Print("确认密码: ")
    confirmBytes, err := term.ReadPassword(int(syscall.Stdin))
    if err != nil {
      return fmt.Errorf("读取密码失败: %w", err)
    }
    fmt.Println()

    if string(pwdBytes) != string(confirmBytes) {
      fmt.Println("两次密码不一致，请重新输入")
      continue
    }

    if err := auth.CreateUser(username, string(pwdBytes), "superadmin"); err != nil {
      return err
    }

    fmt.Printf("超管账户 %s 创建成功\n", username)
    return nil
  }
}
