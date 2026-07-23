package sandbox

import (
  "bufio"
  "bytes"
  "context"
  "encoding/json"
  "fmt"
  "io"
  "log/slog"
  "os"
  "os/exec"
  "path/filepath"
  "strings"
  "sync"
  "syscall"
  "time"

  "github.com/picoaide/picoaide/internal/store"
)

// maxSandboxDuration 沙箱最长运行时间，超时后强制终止
const maxSandboxDuration = 12 * time.Hour

// ============================================================
// 沙箱管理器 — overlayfs 快照模式
// Alpine rootfs 作为只读 lower layer，tmpfs 作为 ephemeral upper，
// 每次沙箱启动都是干净的快照，只有 workspace 持久化写入。
// ============================================================

const bridgeName = "picoaide-br"

type Manager struct {
  rootfs  string
  workDir string

  // 用户级串行队列：每个用户一个 buffered chan (cap=1)，存一个 token
  // 同一用户的多个请求排队按顺序执行，不同用户可并发
  userChs  sync.Map // map[string]chan struct{}
  userRefs sync.Map // map[string]int
  usersMu  sync.Mutex

  // 活跃沙箱的 stdin pipe，用于多轮消息追加（聊天场景）
  activeInputs sync.Map // map[string]*io.PipeWriter
}

// SendInput 向该用户的活跃沙箱发送一条 JSON 消息（多轮追加）
func (m *Manager) SendInput(username string, inputJSON []byte) error {
  v, ok := m.activeInputs.Load(username)
  if !ok {
    return fmt.Errorf("用户 %s 没有活跃沙箱", username)
  }
  pw, ok := v.(io.WriteCloser)
  if !ok {
    return fmt.Errorf("用户 %s 的沙箱 stdin 类型异常", username)
  }
  _, err := pw.Write(inputJSON)
  if err != nil {
    return fmt.Errorf("发送消息到沙箱失败: %w", err)
  }
  return nil
}

// acquireUser 获取用户串行锁，阻塞直到轮到该用户或 ctx 取消
func (m *Manager) acquireUser(ctx context.Context, username string) error {
  ch := m.getOrCreateCh(username)
  select {
  case <-ch:
    return nil
  case <-ctx.Done():
    return ctx.Err()
  }
}

// releaseUser 释放用户串行锁，唤醒该用户的下一个等待者
func (m *Manager) releaseUser(username string) {
  ch := m.getOrCreateCh(username)
  select {
  case ch <- struct{}{}:
  default:
  }
  m.decRef(username)
}

// getOrCreateCh 返回该用户的 token channel
func (m *Manager) getOrCreateCh(username string) chan struct{} {
  v, loaded := m.userChs.LoadOrStore(username, make(chan struct{}, 1))
  ch := v.(chan struct{})
  // 仅在首次创建时放入 token，已有旧 channel 时不再重复放
  // 防止同一用户在沙箱运行时通过重复获取 token 绕过串行锁
  if !loaded {
    ch <- struct{}{}
  }
  m.usersMu.Lock()
  ref, _ := m.userRefs.LoadOrStore(username, 0)
  m.userRefs.Store(username, ref.(int)+1)
  m.usersMu.Unlock()
  return ch
}

// decRef 减少引用计数，归零时从 map 中移除
func (m *Manager) decRef(username string) {
  m.usersMu.Lock()
  defer m.usersMu.Unlock()
  v, ok := m.userRefs.Load(username)
  if !ok {
    return
  }
  ref := v.(int) - 1
  if ref <= 0 {
    m.userRefs.Delete(username)
    m.userChs.Delete(username)
    return
  }
  m.userRefs.Store(username, ref)
}

type RunResult struct {
  Events []StreamEvent
  Error  string
}

type StreamEvent struct {
  Type string          `json:"type"`
  Data json.RawMessage `json:"data,omitempty"`
}

type Mount struct {
  Source string
  Target string
}

func NewManager(rootfs, workDir string) *Manager {
  return &Manager{rootfs: rootfs, workDir: workDir}
}

// prepareSandbox 设置 overlayfs、挂载、启动 picoagent，返回清理函数和输出流
func (m *Manager) prepareSandbox(ctx context.Context, token string, inputJSON []byte, workspace string, apiKeys map[string]string, mounts []Mount, username string) (func(), io.ReadCloser, *exec.Cmd, error) {
  if err := m.acquireUser(ctx, username); err != nil {
    slog.Debug("sandbox.acquire_failed", "username", username, "error", err.Error())
    return nil, nil, nil, err
  }

  // defer 确保 panic 时也释放用户锁
  var released bool
  defer func() {
    if !released {
      m.releaseUser(username)
    }
  }()

  if _, err := os.Stat(m.rootfs); err != nil {
    released = true
    m.releaseUser(username)
    return nil, nil, nil, fmt.Errorf("rootfs 不存在 %s: %w", m.rootfs, err)
  }

  mergeDir := filepath.Join(m.workDir, ".sandbox-merge")
  upperDir := filepath.Join(m.workDir, ".sandbox-upper")

  for _, d := range []string{mergeDir, upperDir} {
    syscall.Unmount(d, syscall.MNT_DETACH)
    os.RemoveAll(d)
  }

  os.MkdirAll(mergeDir, 0755)
  os.MkdirAll(upperDir, 0755)

  if err := syscall.Mount("tmpfs", upperDir, "tmpfs", 0, ""); err != nil {
    released = true
    m.releaseUser(username)
    return nil, nil, nil, fmt.Errorf("tmpfs 挂载失败: %w", err)
  }

  os.MkdirAll(filepath.Join(upperDir, "up"), 0755)
  os.MkdirAll(filepath.Join(upperDir, "wd"), 0755)

  opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s",
    m.rootfs, filepath.Join(upperDir, "up"), filepath.Join(upperDir, "wd"))
  if err := syscall.Mount("overlay", mergeDir, "overlay", 0, opts); err != nil {
    syscall.Unmount(upperDir, syscall.MNT_DETACH)
    released = true
    m.releaseUser(username)
    return nil, nil, nil, fmt.Errorf("overlay 挂载失败: %w", err)
  }

  slog.Debug("sandbox.overlay_mounted", "merge_dir", mergeDir, "upper_dir", upperDir)

  var netCleanup func()
  var cleanupOnce sync.Once
  localCleanup := func() {
    cleanupOnce.Do(func() {
      // 关闭 stdin 并移除活跃记录，通知 picoagent 退出 stdin 读取循环
      if v, ok := m.activeInputs.LoadAndDelete(username); ok {
        if pw, ok := v.(io.WriteCloser); ok {
          pw.Close()
        }
      }
      if netCleanup != nil {
        netCleanup()
      }
      for _, d := range []string{"/workspace", "/run/picoaide.sock"} {
        syscall.Unmount(filepath.Join(mergeDir, d), syscall.MNT_DETACH)
      }
      syscall.Unmount(mergeDir, syscall.MNT_DETACH)
      syscall.Unmount(upperDir, syscall.MNT_DETACH)
      os.RemoveAll(upperDir)
      os.RemoveAll(mergeDir)
      m.releaseUser(username)
      released = true
      slog.Debug("sandbox.cleanup_complete")
    })
  }

  wsTarget := filepath.Join(mergeDir, "workspace")
  os.MkdirAll(wsTarget, 0755)
  syscall.Unmount(wsTarget, syscall.MNT_DETACH)
  if err := syscall.Mount(workspace, wsTarget, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
    localCleanup()
    return nil, nil, nil, fmt.Errorf("workspace bind mount 失败: %w", err)
  }

  sockHost := filepath.Join(m.workDir, "picoaide.sock")
  sockTarget := filepath.Join(mergeDir, "run", "picoaide.sock")
  if _, err := os.Stat(sockHost); err == nil {
    os.MkdirAll(filepath.Dir(sockTarget), 0755)
    os.Remove(sockTarget)
    os.WriteFile(sockTarget, nil, 0666)
    syscall.Unmount(sockTarget, syscall.MNT_DETACH)
    if err := syscall.Mount(sockHost, sockTarget, "", syscall.MS_BIND, ""); err != nil {
      fmt.Fprintf(os.Stderr, "[SANDBOX] socket bind mount 失败: %v\n", err)
    }
  }

  for _, mnt := range mounts {
    target := filepath.Join(mergeDir, mnt.Target)
    os.MkdirAll(filepath.Dir(target), 0755)
    os.Remove(target)
    os.MkdirAll(target, 0755)
    syscall.Unmount(target, syscall.MNT_DETACH)
    if err := syscall.Mount(mnt.Source, target, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
      slog.Warn("sandbox.skill_mount_failed", "source", mnt.Source, "target", target, "error", err)
      continue
    }
    if err := syscall.Mount("", target, "", syscall.MS_REMOUNT|syscall.MS_BIND|syscall.MS_RDONLY, ""); err != nil {
      slog.Warn("sandbox.skill_remount_ro_failed", "target", target, "error", err)
    }
  }

  cmd := exec.Command("/bin/picoagent")
  cmd.SysProcAttr = &syscall.SysProcAttr{
    Chroot:     mergeDir,
    Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNET,
  }
  cmd.Dir = "/workspace"
  env := []string{
    fmt.Sprintf("PICOAGENT_TOKEN=%s", token),
    fmt.Sprintf("PICOAIDE_MCP_TOKEN=%s", token),
    "PICOAGENT_SOCKET=/run/picoaide.sock",
    "HOME=/workspace",
    "PATH=/bin:/usr/bin:/usr/local/bin",
  }

  // 注入环境变量：API key + 其他额外环境变量
  for k, v := range apiKeys {
    if k == "default" {
      env = append(env, fmt.Sprintf("PICOAGENT_API_KEY=%s", v))
    } else {
      env = append(env, fmt.Sprintf("%s=%s", k, v))
    }
  }
  cmd.Env = env

  stdin, err := cmd.StdinPipe()
  if err != nil {
    localCleanup()
    return nil, nil, nil, fmt.Errorf("创建 stdin pipe 失败: %w", err)
  }
  var stdout io.ReadCloser
  stdout, err = cmd.StdoutPipe()
  if err != nil {
    localCleanup()
    return nil, nil, nil, fmt.Errorf("创建 stdout pipe 失败: %w", err)
  }

  var stderrBuf bytes.Buffer
  stderrReader, stderrWriter := io.Pipe()
  cmd.Stderr = io.MultiWriter(&stderrBuf, stderrWriter)
  go func() {
    scanner := bufio.NewScanner(stderrReader)
    for scanner.Scan() {
      slog.Debug("sandbox.agent.stderr", "line", scanner.Text())
    }
  }()

  if err := cmd.Start(); err != nil {
    localCleanup()
    return nil, nil, nil, fmt.Errorf("启动 picoagent 失败: %w", err)
  }

  slog.Debug("sandbox.picoagent_started", "pid", cmd.Process.Pid, "username", username)

  // 网络隔离：picoaide-br 大内网 + 固定 IP + ICC=false
  m.initBridge()
  if err := setupNetNS(cmd.Process.Pid, username); err != nil {
    slog.Error("sandbox.netns_setup_failed", "error", err, "username", username)
  } else {
    netCleanup = func() { teardownNetNS(username) }
  }

  stdin.Write(inputJSON)
  // 不关闭 stdin — 聊天场景支持多轮追加，picoagent 循环读取 stdin
  // 在 cleanup 中关闭和移除
  m.activeInputs.Store(username, stdin)

  released = true
  return localCleanup, stdout, cmd, nil
}



// killOnCancel 等待 ctx 取消后先 SIGTERM 再 SIGKILL 终止沙箱进程
func killOnCancel(ctx context.Context, cmd *exec.Cmd) {
  go func() {
    defer func() {
      if r := recover(); r != nil {
        slog.Error("sandbox.kill_on_cancel_panic", "panic", r)
      }
    }()
    <-ctx.Done()
    pid := cmd.Process.Pid
    slog.Debug("sandbox.kill_on_cancel", "pid", pid)
    cmd.Process.Kill()
  }()
}

// Run 流式运行沙箱，picoagent 每输出一行事件就实时发送到 channel
func (m *Manager) Run(ctx context.Context, token string, inputJSON []byte, workspace string, apiKeys map[string]string, mounts []Mount, username string, eventCb ...func(StreamEvent)) (<-chan StreamEvent, error) {
  slog.Debug("sandbox.run_start",
    "username", username,
    "workspace", workspace,
    "api_keys_count", len(apiKeys),
    "mounts_count", len(mounts),
  )

  runCtx, cancel := context.WithTimeout(ctx, maxSandboxDuration)
  // 不能 defer cancel() — Run 返回后 runCtx 立即取消，导致 scanner goroutine 的
  // select 中 runCtx.Done() 与 events 同时就绪，Go 随机选择会丢弃事件（event_count=0）
  // 改为在 scanner goroutine 结束后显式 cancel

  // 启动进度事件
  emitEvent := func(typ string, data map[string]interface{}) {
    if len(eventCb) > 0 && eventCb[0] != nil {
      eventCb[0](StreamEvent{Type: typ, Data: mustJSON(data)})
    }
  }
  emitEvent("progress_startup", map[string]interface{}{"stage": "mounting_overlay"})

  // 启动阶段设 30 秒超时，运行阶段由 AI 控制不设限
  startupCtx, startupCancel := context.WithTimeout(runCtx, 30*time.Second)
  defer startupCancel()
  cleanup, stdout, cmd, err := m.prepareSandbox(startupCtx, token, inputJSON, workspace, apiKeys, mounts, username)
  if err != nil {
    cancel()
    return nil, err
  }
  emitEvent("progress_startup", map[string]interface{}{"stage": "picoagent_started"})

  // 同时监听调用方 ctx（用户取消）和 runCtx（maxSandboxDuration 超时）
  killOnCancel(runCtx, cmd)
  if ctx != runCtx {
    killOnCancel(ctx, cmd)
  }

  // 当 picoagent 退出后关闭 stdout pipe（即使 scanner 未读到 EOF）
  // 防止 picoagent 子进程持有 stdout fd 导致 scanner 永远阻塞
  // 同时记录 picoagent 的退出码，用于诊断无输出问题
  done := make(chan struct{})
  exitTime := make(chan time.Time, 1)
  go func() {
    defer func() {
      if r := recover(); r != nil {
        slog.Error("sandbox.wait_panic", "panic", r)
      }
    }()
    start := time.Now()
    err := cmd.Wait()
    waited := time.Since(start)
    slog.Debug("sandbox.wait_done",
      "pid", cmd.Process.Pid,
      "waited_ms", waited.Milliseconds(),
      "wait_err", err != nil,
    )
    exitTime <- time.Now()
    stdout.Close()
    close(done)
  }()

  events := make(chan StreamEvent, 100)
  go func() {
    defer func() {
      if r := recover(); r != nil {
        slog.Error("sandbox.scanner_panic", "panic", r)
      }
    }()
    defer close(events)
    defer cleanup()
    defer cancel() // Run 退出后才清理，不影响 scanner 的 select 竞争
    startTime := time.Now()

    scanner := bufio.NewScanner(stdout)
    scanBuf := make([]byte, 32*1024*1024)
    scanner.Buffer(scanBuf, 32*1024*1024)
    var eventCount int
    for scanner.Scan() {
      line := scanner.Text()
      if len(line) == 0 {
        continue
      }
      var event StreamEvent
      if err := json.Unmarshal([]byte(line), &event); err != nil {
        continue
      }
      eventCount++
      select {
      case events <- event:
      case <-ctx.Done():
        slog.Debug("sandbox.scanner_cancelled", "event_count", eventCount, "elapsed_ms", time.Since(startTime).Milliseconds())
        return
      }
    }

    <-done
    scanEnd := time.Now()
    if cmd.ProcessState != nil {
      slog.Debug("sandbox.picoagent_exited",
        "pid", cmd.Process.Pid,
        "exit_code", cmd.ProcessState.ExitCode(),
        "success", cmd.ProcessState.Success(),
        "event_count", eventCount,
        "scan_duration_ms", scanEnd.Sub(startTime).Milliseconds(),
      )
    }

    var exitTimeVal time.Time
    select {
    case exitTimeVal = <-exitTime:
    default:
    }

    slog.Debug("sandbox.run_complete",
      "username", username,
      "event_count", eventCount,
      "has_error", false,
      "total_ms", scanEnd.Sub(startTime).Milliseconds(),
      "exit_delay_ms", scanEnd.Sub(exitTimeVal).Milliseconds(),
    )
  }()

  return events, nil
}

func mustJSON(v interface{}) json.RawMessage {
  data, _ := json.Marshal(v)
  return data
}

// initBridge 确保 picoaide-br 网桥就绪（幂等，可重复调用）
func (m *Manager) initBridge() {
  bridgeOnce.Do(ensureBridge)
}

// EnsureBridge 确保 picoaide-br 网桥就绪。可被 Serve() 等调用方安全调用。
func EnsureBridge() {
  bridgeOnce.Do(ensureBridge)
}

var bridgeOnce sync.Once

func ensureBridge() {
  // 启用 IP 转发
  exec.Command("sh", "-c", "echo 1 > /proc/sys/net/ipv4/ip_forward").Run()

  // 尝试创建网桥，已存在则继续
  if err := exec.Command("ip", "link", "add", bridgeName, "type", "bridge").Run(); err != nil {
    slog.Debug("sandbox.bridge_create_skipped", "error", err)
  }
  // 禁用 STP（纯软件网桥无环路风险，避免 15s 端口转发延迟）
  exec.Command("ip", "link", "set", bridgeName, "type", "bridge", "stp_state", "0").Run()
  exec.Command("ip", "addr", "add", "100.64.0.1/16", "dev", bridgeName).Run()
  exec.Command("ip", "link", "set", bridgeName, "up").Run()

  // 放行从网桥发出的流量（出站）
  if exec.Command("iptables", "-C", "FORWARD", "-i", bridgeName, "-j", "ACCEPT").Run() != nil {
    exec.Command("iptables", "-A", "FORWARD", "-i", bridgeName, "-j", "ACCEPT").Run()
  }
  // 放行发往网桥的流量（入站，响应包）
  if exec.Command("iptables", "-C", "FORWARD", "-o", bridgeName, "-j", "ACCEPT").Run() != nil {
    exec.Command("iptables", "-A", "FORWARD", "-o", bridgeName, "-j", "ACCEPT").Run()
  }
  // 阻断沙箱间通信（ICC=false）
  exec.Command("sh", "-c",
    "echo 1 > /proc/sys/net/bridge/bridge-nf-call-iptables").Run()
  if exec.Command("iptables", "-C", "FORWARD", "-i", bridgeName, "-o", bridgeName,
    "-j", "DROP").Run() != nil {
    exec.Command("iptables", "-I", "FORWARD", "-i", bridgeName, "-o", bridgeName,
      "-j", "DROP").Run()
  }

  // NAT：沙箱访问外网（从 100.64.0.0/16 发出且不经过网桥的流量做 SNAT）
  if exec.Command("iptables", "-t", "nat", "-C", "POSTROUTING",
    "-s", "100.64.0.0/16", "!", "-o", bridgeName, "-j", "MASQUERADE").Run() != nil {
    exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING",
      "-s", "100.64.0.0/16", "!", "-o", bridgeName, "-j", "MASQUERADE").Run()
  }

  slog.Debug("sandbox.bridge_created", "bridge", bridgeName, "gateway", "100.64.0.1")
}

// userIP 从数据库分配/获取用户 IP（顺序分配，避免 CRC32 碰撞）
func userIP(username string) string {
  ip, err := store.AllocateIP(username)
  if err != nil {
    slog.Error("分配 IP 失败，使用备用 IP", "username", username, "error", err)
    // 回退：从用户名 hash 生成，仅当数据库不可用时的备选方案
    h := 0
    for _, c := range username {
      h = h*31 + int(c)
    }
    offset := h%65533 + 2
    if offset < 2 {
      offset = 2
    }
    return fmt.Sprintf("100.64.%d.%d", offset/256, offset%256)
  }
  return ip
}

// vethName 基于 username 生成唯一的 veth 接口名（不超过 IFNAMSIZ=15）
func vethName(username string) string {
  h := uint32(0)
  for _, c := range username {
    h = h*31 + uint32(c)
  }
  return fmt.Sprintf("vs-%08x", h)
}

// setupNetNS 设置沙箱网络：veth 对接 picoaide-br + 固定 IP + 默认路由
func setupNetNS(pid int, username string) error {
  veth := vethName(username)
  ip := userIP(username)

  // 清理上次残留的 veth（异常退出未清理）
  exec.Command("ip", "link", "delete", veth).Run()

  cmds := [][]string{
    {"ip", "link", "add", veth, "type", "veth", "peer", "name", veth + "-s"},
    {"ip", "link", "set", veth + "-s", "netns", fmt.Sprint(pid)},
    {"ip", "link", "set", veth, "master", bridgeName},
    {"ip", "link", "set", veth, "up"},
    {"nsenter", "-t", fmt.Sprint(pid), "-n", "ip", "addr", "add", ip + "/16", "dev", veth + "-s"},
    {"nsenter", "-t", fmt.Sprint(pid), "-n", "ip", "link", "set", veth + "-s", "up"},
    {"nsenter", "-t", fmt.Sprint(pid), "-n", "ip", "link", "set", "lo", "up"},
    {"nsenter", "-t", fmt.Sprint(pid), "-n", "ip", "route", "add", "default", "via", "100.64.0.1"},
  }

  for _, c := range cmds {
    if err := exec.Command(c[0], c[1:]...).Run(); err != nil {
      exec.Command("ip", "link", "delete", veth).Run()
      return fmt.Errorf("网络设置失败: %s: %w", strings.Join(c, " "), err)
    }
  }

  return nil
}

// teardownNetNS 清理沙箱网络：删除 veth 对（自动从网桥移除）+ 释放 IP
func teardownNetNS(username string) {
  veth := vethName(username)
  exec.Command("ip", "link", "delete", veth+"-s").Run()
  exec.Command("ip", "link", "delete", veth).Run()
  if err := store.ReleaseIP(username); err != nil {
    slog.Warn("释放 IP 失败", "username", username, "error", err)
  }
}


