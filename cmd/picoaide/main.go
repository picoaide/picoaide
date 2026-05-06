package main

import (
  "bufio"
  "context"
  "fmt"
  "io"
  "net"
  "os"
  "path/filepath"
  "strconv"
  "strings"
  "syscall"
  "time"

  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
  dockerpkg "github.com/picoaide/picoaide/internal/docker"
  "github.com/picoaide/picoaide/internal/ldap"
  "github.com/picoaide/picoaide/internal/user"
  "github.com/picoaide/picoaide/internal/util"
  "github.com/picoaide/picoaide/internal/web"
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
  // 检查是否为本地用户
  if !auth.UserExists(username) {
    // 尝试加载配置判断认证模式
    cfg, _ := config.LoadFromDB()
    if cfg != nil && cfg.UnifiedAuthEnabled() {
      return fmt.Errorf("用户 %s 不是本地用户，不支持修改密码，请联系管理员在公司认证中心修改", username)
    }
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

  // 环境预检
  fmt.Println("--- 环境检查 ---")

  // 1. Docker
  if err := dockerpkg.InitClient(); err != nil {
    fmt.Fprintf(os.Stderr, "[失败] Docker 未安装或未启动: %v\n", err)
    fmt.Fprintln(os.Stderr, "请先安装 Docker: https://docs.docker.com/engine/install/")
    os.Exit(1)
  }
  dockerpkg.CloseClient()
  fmt.Println("  Docker: 已安装")

  // 2. 端口 80
  if !checkPort(80) {
    fmt.Fprintf(os.Stderr, "[失败] 端口 80 已被占用，请先释放该端口\n")
    os.Exit(1)
  }
  fmt.Println("  端口 80: 可用")

  // 3. 端口 443（警告，不阻断）
  if !checkPort(443) {
    fmt.Println("  端口 443: 已被占用（如需 HTTPS 请释放）")
  } else {
    fmt.Println("  端口 443: 可用")
  }
  fmt.Println()

  // 步骤 1: 数据目录
  fmt.Println("--- 步骤 1/4: 数据目录 ---")
  fmt.Print("请输入数据目录 (默认: /data/picoaide): ")
  dataDir, _ := reader.ReadString('\n')
  dataDir = strings.TrimSpace(dataDir)
  if dataDir == "" {
    dataDir = "/data/picoaide"
  }

  // 检查目录是否为空
  if entries, err := os.ReadDir(dataDir); err == nil && len(entries) > 0 {
    fmt.Printf("[警告] 目录 %s 不为空（包含 %d 个文件），是否继续? [y/N]: ", dataDir, len(entries))
    cont, _ := reader.ReadString('\n')
    if strings.TrimSpace(strings.ToLower(cont)) != "y" {
      fmt.Println("已取消")
      os.Exit(1)
    }
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

  if err := auth.InitDB(dataDir); err != nil {
    fmt.Fprintf(os.Stderr, "初始化数据库失败: %v\n", err)
    os.Exit(1)
  }

  fmt.Printf("数据目录: %s\n", dataDir)
  fmt.Println()

  // 步骤 2: 超管账户
  fmt.Println("--- 步骤 2/4: 超管账户 ---")
  if err := setupSuperAdmin(reader, dataDir); err != nil {
    fmt.Fprintf(os.Stderr, "超管设置失败: %v\n", err)
    os.Exit(1)
  }
  fmt.Println()

  // 初始化默认配置
  if err := config.InitDBDefaults(); err != nil {
    fmt.Fprintf(os.Stderr, "初始化默认配置失败: %v\n", err)
    os.Exit(1)
  }

  cfg, err := config.LoadFromDB()
  if err != nil {
    fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
    os.Exit(1)
  }

  // 步骤 3: 监听地址
  fmt.Println("--- 步骤 3/4: 监听地址 ---")
  fmt.Print("监听地址 (默认: :80): ")
  listenAddr, _ := reader.ReadString('\n')
  listenAddr = strings.TrimSpace(listenAddr)
  if listenAddr == "" {
    listenAddr = ":80"
  }
  cfg.Web.Listen = listenAddr
  fmt.Println()

  // 步骤 4: 镜像仓库
  fmt.Println("--- 步骤 4/4: 镜像仓库 ---")
  fmt.Println("  1) GitHub (ghcr.io)")
  fmt.Println("  2) 腾讯云 (hkccr.ccs.tencentyun.com)")
  fmt.Print("请选择 [1]: ")
  registryAnswer, _ := reader.ReadString('\n')
  registryAnswer = strings.TrimSpace(registryAnswer)
  if registryAnswer == "2" {
    cfg.Image.Registry = "tencent"
    fmt.Println("已选择: 腾讯云")
  } else {
    cfg.Image.Registry = "github"
    fmt.Println("已选择: GitHub")
  }

  fmt.Print("是否立即拉取最新镜像? [Y/n]: ")
  pullAnswer, _ := reader.ReadString('\n')
  pullAnswer = strings.TrimSpace(strings.ToLower(pullAnswer))

  if pullAnswer != "n" && pullAnswer != "no" {
    if err := dockerpkg.InitClient(); err != nil {
      fmt.Fprintf(os.Stderr, "Docker 不可用，跳过镜像拉取: %v\n", err)
    } else {
      pullAndListTags(reader, cfg)
      dockerpkg.CloseClient()
    }
  }
  fmt.Println()

  // 默认本地认证模式
  cfg.Web.AuthMode = "local"
  falseVal := false
  cfg.Web.LDAPEnabled = &falseVal

  // 保存配置
  if err := config.SaveToDB(cfg, "system"); err != nil {
    fmt.Fprintf(os.Stderr, "保存配置失败: %v\n", err)
    os.Exit(1)
  }

  if err := config.InstallService(cfg); err != nil {
    fmt.Fprintf(os.Stderr, "服务安装失败: %v\n", err)
    os.Exit(1)
  }

  fmt.Println("=== 初始化完成 ===")
  fmt.Printf("数据目录: %s\n", dataDir)
  fmt.Printf("监听地址: %s\n", listenAddr)
  fmt.Println("认证模式: 本地认证")
  fmt.Println()
  fmt.Println("安装浏览器插件后，访问管理面板完成后续配置")
}

func pullAndListTags(reader *bufio.Reader, cfg *config.GlobalConfig) {
  ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
  defer cancel()

  // 获取远程标签
  tags, err := dockerpkg.ListRegistryTagsForConfig(ctx, cfg.Image.RepoName(), cfg.Image.Registry)
  if err != nil {
    fmt.Fprintf(os.Stderr, "获取远程标签失败: %v\n", err)
    return
  }
  if len(tags) == 0 {
    fmt.Println("远程仓库无可用标签")
    return
  }

  // 选最新标签
  latestTag := tags[len(tags)-1]
  pullRef := cfg.Image.PullRef(latestTag)
  unifiedRef := cfg.Image.UnifiedRef(latestTag)

  fmt.Printf("正在拉取镜像 %s ...\n", pullRef)
  pullCtx := context.Background()
  pullReader, err := dockerpkg.ImagePull(pullCtx, pullRef)
  if err != nil {
    fmt.Fprintf(os.Stderr, "拉取失败: %v\n", err)
    return
  }
  defer pullReader.Close()
  io.Copy(os.Stdout, pullReader)
  fmt.Println()

  // 腾讯云模式 retag
  if cfg.Image.IsTencent() && pullRef != unifiedRef {
    fmt.Printf("重命名镜像: %s -> %s\n", pullRef, unifiedRef)
    if err := dockerpkg.RetagImage(pullCtx, pullRef, unifiedRef); err != nil {
      fmt.Fprintf(os.Stderr, "重命名失败: %v\n", err)
    }
  }

  fmt.Printf("镜像 %s 拉取完成\n", latestTag)
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
// 辅助函数
// ============================================================

func checkPort(port int) bool {
  ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
  if err != nil {
    return false
  }
  ln.Close()
  return true
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
