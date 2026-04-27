package main

import (
  "bufio"
  "context"
  "fmt"
  "os"
  "path/filepath"
  "strings"
  "syscall"

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

推荐工作流:
  1. init          初始化（首次运行引导，创建配置和服务）
  2. start         启动容器（PicoClaw 自动生成默认配置）
  3. config apply  合并全局配置到用户配置文件

命令:
  init                                    首次运行引导 / 初始化用户目录
  init -user <name>                       初始化单个用户目录
  start [-user <name>]                    启动容器
  stop [-user <name>]                     停止容器
  down [-user <name>]                     彻底停止并清理容器
  restart [-user <name>]                  重启容器
  sync                                    LDAP 同步 + 镜像更新 + 滚动重启
  upgrade [-tag <version>] [-user <name>] 升级镜像版本
  list                                    列出所有用户及容器状态
  config show                             查看全局配置
  config set-model <json>                 设置模型列表
  config set-key <model> <key>           设置 API Key
  config set-channel <json>               设置渠道配置
  config apply [-user <name>]             合并全局配置到用户配置文件（不覆盖已有配置）
  skills deploy [-user <name>]            分发技能到用户目录
  serve [-listen :80]                     启动 Web 管理面板

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

  // 初始化数据库（失败时重试一次）
  wd, _ := os.Getwd()
  if err := auth.InitDB(wd); err != nil {
    fmt.Fprintf(os.Stderr, "警告: 数据库初始化失败，正在重试: %v\n", err)
    os.MkdirAll(wd, 0755)
    if err := auth.InitDB(wd); err != nil {
      fmt.Fprintf(os.Stderr, "初始化数据库失败: %v\n", err)
      os.Exit(1)
    }
  }

  // 配置初始化：检查 DB 是否有配置，没有则迁移或初始化
  count, err := config.SettingsCount()
  if err != nil {
    fmt.Fprintf(os.Stderr, "检查配置失败: %v\n", err)
    os.Exit(1)
  }
  if count == 0 {
    if configPath != "" {
      // 从指定 config.yaml 迁移
      if _, err := os.Stat(configPath); err == nil {
        fmt.Printf("检测到配置文件 %s，正在迁移到数据库...\n", configPath)
        if err := config.MigrateFromYAML(configPath); err != nil {
          fmt.Fprintf(os.Stderr, "迁移配置失败: %v\n", err)
          os.Exit(1)
        }
      }
    } else if _, err := os.Stat("config.yaml"); err == nil {
      // 自动检测当前目录的 config.yaml
      fmt.Println("检测到 config.yaml，正在迁移到数据库...")
      if err := config.MigrateFromYAML("config.yaml"); err != nil {
        fmt.Fprintf(os.Stderr, "迁移配置失败: %v\n", err)
        os.Exit(1)
      }
    } else {
      // 全新安装：用默认配置初始化
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

  // 初始化 Docker 客户端（容器操作需要）
  needDocker := map[string]bool{
    "start": true, "stop": true, "down": true, "restart": true,
    "sync": true, "upgrade": true,
  }
  canRunWithoutDocker := map[string]bool{"list": true}
  if !canRunWithoutDocker[command] {
    needDocker[command] = true
  }
  if needDocker[command] || canRunWithoutDocker[command] {
    if err := dockerpkg.InitClient(); err != nil {
      if canRunWithoutDocker[command] {
        fmt.Fprintf(os.Stderr, "警告: Docker 不可用，将以降级模式运行\n")
      } else {
        fmt.Fprintf(os.Stderr, "Docker 初始化失败: %v\n", err)
        os.Exit(1)
      }
    } else {
      defer dockerpkg.CloseClient()
      ctx := context.Background()
      if err := dockerpkg.EnsureNetwork(ctx); err != nil {
        fmt.Fprintf(os.Stderr, "警告: 网络初始化失败: %v\n", err)
      }
    }
  }

  switch command {
  case "start":
    config.PreflightChecks()
    flags, _ := util.ParseFlags(cmdArgs)
    targetUser := flags["-user"]
    if targetUser != "" {
      if err := startUser(cfg, targetUser); err != nil {
        fmt.Fprintf(os.Stderr, "启动失败: %v\n", err)
        os.Exit(1)
      }
    } else {
      if err := user.ForEachUser(cfg, func(u string) error {
        return startUser(cfg, u)
      }); err != nil {
        fmt.Fprintf(os.Stderr, "启动失败: %v\n", err)
        os.Exit(1)
      }
    }

  case "stop":
    flags, _ := util.ParseFlags(cmdArgs)
    targetUser := flags["-user"]
    if targetUser != "" {
      if err := stopUser(targetUser); err != nil {
        fmt.Fprintf(os.Stderr, "停止失败: %v\n", err)
        os.Exit(1)
      }
    } else {
      if err := user.ForEachUser(cfg, func(u string) error {
        return stopUser(u)
      }); err != nil {
        fmt.Fprintf(os.Stderr, "停止失败: %v\n", err)
        os.Exit(1)
      }
    }

  case "down":
    flags, _ := util.ParseFlags(cmdArgs)
    targetUser := flags["-user"]
    if targetUser != "" {
      if err := downUser(targetUser); err != nil {
        fmt.Fprintf(os.Stderr, "彻底停止失败: %v\n", err)
        os.Exit(1)
      }
    } else {
      if err := user.ForEachUser(cfg, func(u string) error {
        return downUser(u)
      }); err != nil {
        fmt.Fprintf(os.Stderr, "彻底停止失败: %v\n", err)
        os.Exit(1)
      }
    }

  case "restart":
    flags, _ := util.ParseFlags(cmdArgs)
    targetUser := flags["-user"]
    if targetUser != "" {
      if err := restartUser(cfg, targetUser); err != nil {
        fmt.Fprintf(os.Stderr, "重启失败: %v\n", err)
        os.Exit(1)
      }
    } else {
      if err := user.ForEachUser(cfg, func(u string) error {
        return restartUser(cfg, u)
      }); err != nil {
        fmt.Fprintf(os.Stderr, "重启失败: %v\n", err)
        os.Exit(1)
      }
    }

  case "sync":
    config.PreflightChecks()
    if err := Sync(cfg); err != nil {
      fmt.Fprintf(os.Stderr, "同步失败: %v\n", err)
      os.Exit(1)
    }

  case "upgrade":
    flags, _ := util.ParseFlags(cmdArgs)
    newTag := flags["-tag"]
    targetUser := flags["-user"]
    if err := Upgrade(cfg, configPath, newTag, targetUser); err != nil {
      fmt.Fprintf(os.Stderr, "升级失败: %v\n", err)
      os.Exit(1)
    }

  case "list":
    if err := List(); err != nil {
      fmt.Fprintf(os.Stderr, "列表失败: %v\n", err)
      os.Exit(1)
    }

  case "skills":
    if len(cmdArgs) == 0 {
      fmt.Println("用法: skills <deploy> [options]")
      fmt.Println("  skills deploy [-user <name>]  分发技能到用户目录")
      os.Exit(1)
    }
    skillsCmd := cmdArgs[0]
    skillsCmdArgs := cmdArgs[1:]
    switch skillsCmd {
    case "deploy":
      flags, _ := util.ParseFlags(skillsCmdArgs)
      targetUser := flags["-user"]
      if err := SkillsDeploy(cfg, targetUser); err != nil {
        fmt.Fprintf(os.Stderr, "技能分发失败: %v\n", err)
        os.Exit(1)
      }
      fmt.Println("技能分发完成")
    default:
      fmt.Printf("未知技能命令: %s\n", skillsCmd)
      os.Exit(1)
    }

  case "serve":
    flags, _ := util.ParseFlags(cmdArgs)
    listenAddr := flags["-listen"]
    if err := web.Serve(cfg, listenAddr); err != nil {
      fmt.Fprintf(os.Stderr, "Web 服务启动失败: %v\n", err)
      os.Exit(1)
    }

  case "config":
    if len(cmdArgs) == 0 {
      fmt.Println("用法: config <show|set-model|set-key|set-channel|apply> [args]")
      os.Exit(1)
    }
    configCmd := cmdArgs[0]
    configCmdArgs := cmdArgs[1:]

    switch configCmd {
    case "show":
      if err := ConfigShow(cfg); err != nil {
        fmt.Fprintf(os.Stderr, "错误: %v\n", err)
        os.Exit(1)
      }
    case "set-model":
      if len(configCmdArgs) < 1 {
        fmt.Println("用法: config set-model '<json-array>'")
        fmt.Println(`示例: config set-model '[{"model_name":"gpt-5.4","model":"openai/gpt-5.4","api_base":"https://api.openai.com/v1"}]'`)
        os.Exit(1)
      }
      _, positional := util.ParseFlags(configCmdArgs)
      if len(positional) == 0 {
        fmt.Println("用法: config set-model '<json-array>'")
        os.Exit(1)
      }
      if err := ConfigSetModel(cfg, configPath, positional[0]); err != nil {
        fmt.Fprintf(os.Stderr, "错误: %v\n", err)
        os.Exit(1)
      }
      fmt.Println("模型列表已更新")
    case "set-key":
      if len(configCmdArgs) < 2 {
        fmt.Println("用法: config set-key <model-name> <api-key>")
        fmt.Println("示例: config set-key gpt-5.4:0 sk-new-key")
        os.Exit(1)
      }
      _, positional := util.ParseFlags(configCmdArgs)
      if len(positional) < 2 {
        fmt.Println("用法: config set-key <model-name> <api-key>")
        os.Exit(1)
      }
      if err := ConfigSetKey(cfg, configPath, positional[0], positional[1]); err != nil {
        fmt.Fprintf(os.Stderr, "错误: %v\n", err)
        os.Exit(1)
      }
      fmt.Printf("API Key 已更新: %s\n", positional[0])
    case "set-channel":
      if len(configCmdArgs) < 1 {
        fmt.Println("用法: config set-channel '<json>'")
        fmt.Println(`示例: config set-channel '{"telegram":{"enabled":true}}'`)
        os.Exit(1)
      }
      if err := ConfigSetChannel(cfg, configPath, configCmdArgs[0]); err != nil {
        fmt.Fprintf(os.Stderr, "错误: %v\n", err)
        os.Exit(1)
      }
      fmt.Println("渠道配置已更新")
    case "apply":
      flags, _ := util.ParseFlags(configCmdArgs)
      targetUser := flags["-user"]
      if err := ConfigApply(cfg, targetUser); err != nil {
        fmt.Fprintf(os.Stderr, "错误: %v\n", err)
        os.Exit(1)
      }
      fmt.Println("配置已应用")
    default:
      fmt.Printf("未知配置命令: %s\n", configCmd)
      os.Exit(1)
    }

  default:
    fmt.Printf("未知命令: %s\n", command)
    printUsage()
    os.Exit(1)
  }
}

// ============================================================
// Docker 容器操作辅助函数
// ============================================================

// ensureContainerCreated 确保容器已创建（未创建则创建）
func ensureContainerCreated(cfg *config.GlobalConfig, username string) (string, error) {
  rec, err := auth.GetContainerByUsername(username)
  if err != nil {
    return "", fmt.Errorf("查询用户记录失败: %w", err)
  }
  if rec == nil {
    return "", fmt.Errorf("用户 %s 未初始化", username)
  }

  // 已有 containerID 且容器存在
  if rec.ContainerID != "" {
    if dockerpkg.ContainerExists(context.Background(), username) {
      return rec.ContainerID, nil
    }
  }

  // 需要创建容器
  userDir := user.UserDir(cfg, username)
  imageRef := rec.Image
  if imageRef == "" {
    return "", fmt.Errorf("用户 %s 未设置镜像标签，请先拉取镜像并在创建用户时指定标签", username)
  }

  ctx := context.Background()
  cid, err := dockerpkg.CreateContainer(ctx, username, imageRef, userDir, rec.IP, rec.CPULimit, rec.MemoryLimit)
  if err != nil {
    return "", fmt.Errorf("创建容器失败: %w", err)
  }

  // 更新 DB 记录
  auth.UpdateContainerID(username, cid)
  return cid, nil
}

func startUser(cfg *config.GlobalConfig, username string) error {
  cid, err := ensureContainerCreated(cfg, username)
  if err != nil {
    return err
  }
  if err := dockerpkg.Start(context.Background(), cid); err != nil {
    return err
  }
  auth.UpdateContainerStatus(username, "running")
  fmt.Printf("  [启动] %s\n", username)
  return nil
}

func stopUser(username string) error {
  rec, err := auth.GetContainerByUsername(username)
  if err != nil || rec == nil || rec.ContainerID == "" {
    fmt.Printf("  [跳过] %s (无容器)\n", username)
    return nil
  }
  if err := dockerpkg.Stop(context.Background(), rec.ContainerID); err != nil {
    return err
  }
  auth.UpdateContainerStatus(username, "stopped")
  fmt.Printf("  [停止] %s\n", username)
  return nil
}

func downUser(username string) error {
  rec, err := auth.GetContainerByUsername(username)
  if err != nil || rec == nil || rec.ContainerID == "" {
    fmt.Printf("  [跳过] %s (无容器)\n", username)
    return nil
  }
  if err := dockerpkg.Remove(context.Background(), rec.ContainerID); err != nil {
    return err
  }
  auth.UpdateContainerID(username, "")
  auth.UpdateContainerStatus(username, "stopped")
  fmt.Printf("  [彻底停止] %s\n", username)
  return nil
}

func restartUser(cfg *config.GlobalConfig, username string) error {
  cid, err := ensureContainerCreated(cfg, username)
  if err != nil {
    return err
  }
  if err := dockerpkg.Restart(context.Background(), cid); err != nil {
    return err
  }
  auth.UpdateContainerStatus(username, "running")
  fmt.Printf("  [重启] %s\n", username)
  return nil
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
      fmt.Fprintf(os.Stderr, "LDAP 连接失败，请检查 config.yaml 中的 LDAP 配置\n")
      os.Exit(1)
    }
    fmt.Printf("成功（获取到 %d 个用户）\n", len(users))
  }

  wd, _ := os.Getwd()
  if err := auth.InitDB(wd); err != nil {
    fmt.Fprintf(os.Stderr, "初始化数据库失败: %v\n", err)
    os.Exit(1)
  }

  if err := config.InstallService(cfg); err != nil {
    fmt.Fprintf(os.Stderr, "警告: 服务安装失败: %v\n", err)
  }

  config.PreflightChecks()

  // 获取本地最新镜像标签
  var initTag string
  if tags, err := dockerpkg.ListLocalTags(context.Background(), cfg.Image.Name); err == nil && len(tags) > 0 {
    initTag = tags[0]
  }
  if err := user.InitAll(cfg, initTag); err != nil {
    fmt.Fprintf(os.Stderr, "初始化失败: %v\n", err)
    os.Exit(1)
  }
}

// ============================================================
// 超管设置
// ============================================================

func setupSuperAdmin(reader *bufio.Reader, dataDir string) error {
  if err := auth.InitDB(dataDir); err != nil {
    return fmt.Errorf("初始化数据库失败: %w", err)
  }

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
