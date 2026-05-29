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

  "github.com/picoaide/picoaide/internal/auth"
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
      continue
    }
    syscall.Mount("", target, "", syscall.MS_REMOUNT|syscall.MS_BIND|syscall.MS_RDONLY, "")
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

  // 注入 API key 为环境变量
  if key, ok := apiKeys["default"]; ok {
    env = append(env, fmt.Sprintf("PICOAGENT_API_KEY=%s", key))
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
  go parsePicoagentStderr(stderrReader, username)

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

// RunAndWait 同步运行沙箱，等待 picoagent 完成后返回所有事件
func (m *Manager) RunAndWait(ctx context.Context, token string, inputJSON []byte, workspace string, apiKeys map[string]string, mounts []Mount, username string) (*RunResult, error) {
  slog.Debug("sandbox.run_start",
    "username", username,
    "workspace", workspace,
    "api_keys_count", len(apiKeys),
    "mounts_count", len(mounts),
  )

  runCtx, cancel := context.WithTimeout(ctx, maxSandboxDuration)
  defer cancel()

  cleanup, stdout, cmd, err := m.prepareSandbox(runCtx, token, inputJSON, workspace, apiKeys, mounts, username)
  if err != nil {
    return nil, err
  }
  defer cleanup()

  killOnCancel(runCtx, cmd)

  result := &RunResult{}
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
    result.Events = append(result.Events, event)
    if event.Type == "error" {
      var errStr string
      json.Unmarshal(event.Data, &errStr)
      result.Error = errStr
      slog.Debug("sandbox.event_error", "error", errStr)
    }
    if event.Type == "task_done" {
      slog.Debug("sandbox.event_task_done", "event_count", eventCount)
    }
  }

  cmd.Wait()

  slog.Debug("sandbox.picoagent_exited",
    "pid", cmd.Process.Pid,
    "exit_code", cmd.ProcessState.ExitCode(),
    "event_count", eventCount,
  )

  slog.Debug("sandbox.run_complete",
    "username", username,
    "event_count", eventCount,
    "has_error", result.Error != "",
  )
  return result, nil
}

// killOnCancel 等待 ctx 取消后先 SIGTERM 再 SIGKILL 终止沙箱进程
func killOnCancel(ctx context.Context, cmd *exec.Cmd) {
  go func() {
    <-ctx.Done()
    pid := cmd.Process.Pid
    slog.Debug("sandbox.kill_on_cancel", "pid", pid)
    cmd.Process.Signal(syscall.SIGTERM)
    time.AfterFunc(5*time.Second, func() {
      cmd.Process.Kill()
    })
  }()
}

// Run 流式运行沙箱，picoagent 每输出一行事件就实时发送到 channel
func (m *Manager) Run(ctx context.Context, token string, inputJSON []byte, workspace string, apiKeys map[string]string, mounts []Mount, username string) (<-chan StreamEvent, error) {
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

  cleanup, stdout, cmd, err := m.prepareSandbox(runCtx, token, inputJSON, workspace, apiKeys, mounts, username)
  if err != nil {
    cancel()
    return nil, err
  }

  // 监听调用方 context，而非 runCtx（runCtx 在 Run 返回时被 defer cancel 立即取消）
  killOnCancel(ctx, cmd)

  // 当 picoagent 退出后关闭 stdout pipe（即使 scanner 未读到 EOF）
  // 防止 picoagent 子进程持有 stdout fd 导致 scanner 永远阻塞
  // 同时记录 picoagent 的退出码，用于诊断无输出问题
  done := make(chan struct{})
  exitTime := make(chan time.Time, 1)
  go func() {
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

// parsePicoagentStderr 实时解析 picoagent 的 stderr 输出，转发为结构化 debug 日志
// stderr 格式: [PICOAGENT] message
func parsePicoagentStderr(r io.Reader, username string) {
  scanner := bufio.NewScanner(r)
  for scanner.Scan() {
    line := scanner.Text()
    if line == "" {
      continue
    }

    // 提取 [PICOAGENT] 前缀
    if !strings.HasPrefix(line, "[PICOAGENT]") {
      slog.Debug("sandbox.picoagent_stderr", "username", username, "message", line)
      continue
    }

    msg := strings.TrimSpace(strings.TrimPrefix(line, "[PICOAGENT]"))
    if msg == "" {
      continue
    }

    // 解析常见消息模式，提取结构化字段
    switch {
    case msg == "starting":
      slog.Debug("sandbox.agent.starting", "username", username)

    case strings.HasPrefix(msg, "token ok:"):
      slog.Debug("sandbox.agent.token_ok", "username", username)

    case strings.HasPrefix(msg, "host:"):
      host := strings.TrimSpace(strings.TrimPrefix(msg, "host:"))
      slog.Debug("sandbox.agent.host", "username", username, "host", host)

    case msg == "fetching config":
      slog.Debug("sandbox.agent.fetching_config", "username", username)

    case strings.HasPrefix(msg, "config ok, model:"):
      model := strings.TrimSpace(strings.TrimPrefix(msg, "config ok, model:"))
      slog.Debug("sandbox.agent.config_loaded", "username", username, "model", model)

    case msg == "config fetch failed":
      slog.Debug("sandbox.agent.config_failed", "username", username)

    case msg == "initializing store":
      slog.Debug("sandbox.agent.init_store", "username", username)

    case strings.HasPrefix(msg, "store ok, workspace:"):
      ws := strings.TrimSpace(strings.TrimPrefix(msg, "store ok, workspace:"))
      slog.Debug("sandbox.agent.store_ready", "username", username, "workspace", ws)

    case msg == "building sys prompt":
      slog.Debug("sandbox.agent.building_sysprompt", "username", username)

    case strings.HasPrefix(msg, "sysprompt:"):
      // "sysprompt: 1234 chars"
      parts := strings.Fields(msg)
      if len(parts) >= 2 {
        slog.Debug("sandbox.agent.sysprompt_ready", "username", username, "length", parts[1])
      }

    case msg == "looking up API key":
      slog.Debug("sandbox.agent.lookup_apikey", "username", username)

    case strings.HasPrefix(msg, "apikey:"):
      status := strings.TrimSpace(strings.TrimPrefix(msg, "apikey:"))
      slog.Debug("sandbox.agent.apikey_status", "username", username, "status", status)

    case strings.HasPrefix(msg, "model:"):
      // "model: xxx, provider: yyy, base_url: zzz"
      slog.Debug("sandbox.agent.model_info", "username", username, "detail", msg)

    case msg == "provider ok":
      slog.Debug("sandbox.agent.provider_ready", "username", username)

    case msg == "registering tools":
      slog.Debug("sandbox.agent.registering_tools", "username", username)

    case strings.HasPrefix(msg, "MCP") && strings.Contains(msg, "连接失败"):
      slog.Debug("sandbox.agent.mcp_connect_failed", "username", username, "detail", msg)

    case strings.HasPrefix(msg, "MCP") && strings.Contains(msg, "已连接"):
      slog.Debug("sandbox.agent.mcp_connected", "username", username, "detail", msg)

    case msg == "creating engine":
      slog.Debug("sandbox.agent.creating_engine", "username", username)

    case strings.HasPrefix(msg, "loaded") && strings.Contains(msg, "skills"):
      // "loaded 5 skills"
      parts := strings.Fields(msg)
      if len(parts) >= 2 {
        slog.Debug("sandbox.agent.skills_loaded", "username", username, "count", parts[1])
      }

    case strings.HasPrefix(msg, "history:"):
      // "history: 10 msgs"
      parts := strings.Fields(msg)
      if len(parts) >= 2 {
        slog.Debug("sandbox.agent.history_loaded", "username", username, "count", parts[1])
      }

    case msg == "reading input from stdin":
      slog.Debug("sandbox.agent.reading_input", "username", username)

    case strings.HasPrefix(msg, "input:"):
      input := strings.TrimSpace(strings.TrimPrefix(msg, "input:"))
      slog.Debug("sandbox.agent.input_received",
        "username", username,
        "content_preview", truncateString(input, 100),
      )

    case msg == "no input":
      slog.Debug("sandbox.agent.no_input", "username", username)

    case msg == "starting engine.Process...":
      slog.Debug("sandbox.agent.engine_start", "username", username)

    case strings.HasPrefix(msg, "engine error:"):
      err := strings.TrimSpace(strings.TrimPrefix(msg, "engine error:"))
      slog.Debug("sandbox.agent.engine_error", "username", username, "error", err)

    case strings.HasPrefix(msg, "engine done, response:"):
      // "engine done, response: 1234 chars"
      parts := strings.Fields(msg)
      if len(parts) >= 4 {
        slog.Debug("sandbox.agent.engine_done",
          "username", username,
          "response_length", parts[3],
        )
      }

    case msg == "done":
      slog.Debug("sandbox.agent.done", "username", username)

    case strings.HasPrefix(msg, "error:"):
      err := strings.TrimSpace(strings.TrimPrefix(msg, "error:"))
      slog.Debug("sandbox.agent.error", "username", username, "error", err)

    default:
      slog.Debug("sandbox.agent.stderr", "username", username, "message", msg)
    }
  }
}

func truncateString(s string, maxLen int) string {
  if len(s) <= maxLen {
    return s
  }
  return s[:maxLen] + "..."
}

func StreamEvents(ctx context.Context, r io.Reader) (<-chan StreamEvent, error) {
  events := make(chan StreamEvent, 100)
  go func() {
    defer close(events)
    scanner := bufio.NewScanner(r)
    for scanner.Scan() {
      line := scanner.Text()
      if len(line) == 0 {
        continue
      }
      var event StreamEvent
      if err := json.Unmarshal([]byte(line), &event); err != nil {
        continue
      }
      select {
      case events <- event:
      case <-ctx.Done():
        return
      }
    }
  }()
  return events, nil
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
  ip, err := auth.AllocateIP(username)
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
  if err := auth.ReleaseIP(username); err != nil {
    slog.Warn("释放 IP 失败", "username", username, "error", err)
  }
}


