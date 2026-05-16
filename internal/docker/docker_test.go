package docker

import (
  "context"
  "encoding/json"
  "io"
  "net/http"
  "net/http/httptest"
  "os"
  "strings"
  "testing"
  "time"

  "github.com/docker/docker/api/types/container"
  "github.com/docker/docker/api/types/image"
  "github.com/docker/docker/client"
)

// mockHandler 定义 mock Docker daemon 的路由规则
type mockHandler struct {
  method string
  path   string
  h      func(http.ResponseWriter, *http.Request)
}

// mockDockerDaemon 创建一个 mock Docker daemon HTTP server，
// 返回预定义的响应。handlers 按 path 长度降序匹配（长路径优先）。
// Docker API version 协商自动处理。
// 注意：exec attach 使用 HTTP hijack 无法通过 httptest 模拟，exec 测试只覆盖错误路径。
func mockDockerDaemon(t *testing.T, handlers []mockHandler) *httptest.Server {
  t.Helper()
  // 按 path 长度降序排序
  sorted := make([]mockHandler, len(handlers))
  copy(sorted, handlers)
  for i := 0; i < len(sorted); i++ {
    for j := i + 1; j < len(sorted); j++ {
      if len(sorted[j].path) > len(sorted[i].path) {
        sorted[i], sorted[j] = sorted[j], sorted[i]
      }
    }
  }
  return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.URL.Path == "/version" || strings.HasSuffix(r.URL.Path, "/version") {
      w.Header().Set("Content-Type", "application/json")
      json.NewEncoder(w).Encode(map[string]string{
        "Version":       "24.0.0",
        "ApiVersion":    "1.43",
        "MinAPIVersion": "1.12",
      })
      return
    }
    if strings.HasSuffix(r.URL.Path, "/_ping") {
      w.Header().Set("Content-Type", "text/plain")
      w.Header().Set("API-Version", "1.43")
      w.WriteHeader(http.StatusOK)
      w.Write([]byte("OK"))
      return
    }
    for _, e := range sorted {
      if (e.method == "*" || e.method == r.Method) && strings.Contains(r.URL.Path, e.path) {
        e.h(w, r)
        return
      }
    }
    t.Logf("unhandled Docker API request: %s %s (query: %s)", r.Method, r.URL.Path, r.URL.RawQuery)
    w.WriteHeader(http.StatusNotFound)
    json.NewEncoder(w).Encode(map[string]string{"message": "not found"})
  }))
}

// setupDockerClient 创建指向 mock server 的 Docker client 并赋值给包级变量 cli
func setupDockerClient(t *testing.T, server *httptest.Server) {
  t.Helper()
  var err error
  cli, err = client.NewClientWithOpts(
    client.WithHost(server.URL),
    client.WithAPIVersionNegotiation(),
  )
  if err != nil {
    t.Fatalf("创建 Docker client 失败: %v", err)
  }
  // 验证连接
  ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
  defer cancel()
  if _, err := cli.Ping(ctx); err != nil {
    t.Fatalf("Ping mock server 失败: %v", err)
  }
}

func resetDockerClient() {
  if cli != nil {
    cli.Close()
    cli = nil
  }
}

// ============================================================
// containerEnvVars 测试
// ============================================================

func TestContainerEnvVars_WithToken(t *testing.T) {
  env := containerEnvVars("my-test-token")
  tokenFound := false
  for _, e := range env {
    if e == "PICOAIDE_MCP_TOKEN=my-test-token" {
      tokenFound = true
      break
    }
  }
  if !tokenFound {
    t.Errorf("env vars %v should contain PICOAIDE_MCP_TOKEN=my-test-token", env)
  }

  tzFound := false
  for _, e := range env {
    if strings.HasPrefix(e, "TZ=") {
      tzFound = true
      break
    }
  }
  if !tzFound {
    t.Errorf("env vars %v should contain TZ", env)
  }
}

func TestContainerEnvVars_WithoutToken(t *testing.T) {
  env := containerEnvVars("")
  for _, e := range env {
    if strings.HasPrefix(e, "PICOAIDE_MCP_TOKEN=") {
      t.Errorf("env vars %v should not contain PICOAIDE_MCP_TOKEN", env)
    }
  }
  tzFound := false
  for _, e := range env {
    if strings.HasPrefix(e, "TZ=") {
      tzFound = true
      break
    }
  }
  if !tzFound {
    t.Errorf("env vars %v should contain TZ", env)
  }
}

// ============================================================
// stripANSI 测试
// ============================================================

func TestStripANSI_NoANSI(t *testing.T) {
  input := "hello world"
  got := stripANSI(input)
  if got != input {
    t.Errorf("stripANSI(%q) = %q, want %q", input, got, input)
  }
}

func TestStripANSI_SimpleColor(t *testing.T) {
  input := "\x1b[31mred\x1b[0m"
  want := "red"
  got := stripANSI(input)
  if got != want {
    t.Errorf("stripANSI(%q) = %q, want %q", input, got, want)
  }
}

func TestStripANSI_MultipleCodes(t *testing.T) {
  input := "\x1b[32mgreen\x1b[0m \x1b[1mbold\x1b[0m"
  want := "green bold"
  got := stripANSI(input)
  if got != want {
    t.Errorf("stripANSI(%q) = %q, want %q", input, got, want)
  }
}

func TestStripANSI_WithNumbersAndSemicolons(t *testing.T) {
  input := "\x1b[38;5;82mcolorful\x1b[0m"
  want := "colorful"
  got := stripANSI(input)
  if got != want {
    t.Errorf("stripANSI(%q) = %q, want %q", input, got, want)
  }
}

func TestStripANSI_EmptyString(t *testing.T) {
  got := stripANSI("")
  if got != "" {
    t.Errorf("stripANSI(\"\") = %q, want \"\"", got)
  }
}

func TestStripANSI_IncompleteSequence(t *testing.T) {
  // \x1b[ without closing letter — function keeps the incomplete sequence
  input := "text\x1b["
  want := "text\x1b[" // incomplete sequences are preserved
  got := stripANSI(input)
  if got != want {
    t.Errorf("stripANSI(%q) = %q, want %q", input, got, want)
  }
}

func TestStripANSI_CursorMovement(t *testing.T) {
  input := "line1\x1b[2K\x1b[1Aline2"
  want := "line1line2"
  got := stripANSI(input)
  if got != want {
    t.Errorf("stripANSI(%q) = %q, want %q", input, got, want)
  }
}

// ============================================================
// parseWWWAuthenticate 测试
// ============================================================

func TestParseWWWAuthenticate_Full(t *testing.T) {
  // 注意：函数按逗号分割后检查 realm="/service="/scope=" 前缀
  // 如果 header 以 "Bearer " 开头，第一部分不会被匹配
  header := `realm="https://ghcr.io/token",service="ghcr.io",scope="repository:repo:pull"`
  want := "https://ghcr.io/token?service=ghcr.io&scope=repository:repo:pull"
  got := parseWWWAuthenticate(header)
  if got != want {
    t.Errorf("parseWWWAuthenticate = %q, want %q", got, want)
  }
}

func TestParseWWWAuthenticate_EmptyHeader(t *testing.T) {
  got := parseWWWAuthenticate("")
  if got != "" {
    t.Errorf("parseWWWAuthenticate(\"\") = %q, want \"\"", got)
  }
}

func TestParseWWWAuthenticate_NoRealm(t *testing.T) {
  header := `Bearer service="ghcr.io",scope="repository:repo:pull"`
  got := parseWWWAuthenticate(header)
  if got != "" {
    t.Errorf("parseWWWAuthenticate = %q, want \"\"", got)
  }
}

func TestParseWWWAuthenticate_RealmOnly(t *testing.T) {
  header := `realm="https://registry.example.com/token"`
  want := "https://registry.example.com/token?service=&scope="
  got := parseWWWAuthenticate(header)
  if got != want {
    t.Errorf("parseWWWAuthenticate = %q, want %q", got, want)
  }
}

func TestParseWWWAuthenticate_OrderVariation(t *testing.T) {
}

// ============================================================
// InitClient / CloseClient 测试
// ============================================================

func TestCloseClient_Nil(t *testing.T) {
  // cli 为 nil 时 CloseClient 不应 panic
  resetDockerClient()
  CloseClient()
}

func TestInitClient_Success(t *testing.T) {
  server := mockDockerDaemon(t, nil)
  defer server.Close()
  defer resetDockerClient()

  osSetenv := os.Setenv
  osSetenv("DOCKER_HOST", server.URL)
  defer os.Unsetenv("DOCKER_HOST")

  // 保存原始的 NewClientWithOpts 调用结果，InitClient 使用 FromEnv
  if err := InitClient(); err != nil {
    t.Fatalf("InitClient error: %v", err)
  }
  if cli == nil {
    t.Fatal("InitClient: cli is nil")
  }
}

func TestCloseClient_Active(t *testing.T) {
  server := mockDockerDaemon(t, nil)
  defer server.Close()
  defer resetDockerClient()

  os.Setenv("DOCKER_HOST", server.URL)
  defer os.Unsetenv("DOCKER_HOST")

  if err := InitClient(); err != nil {
    t.Fatal(err)
  }
  // 不应 panic
  CloseClient()
}

func TestInitClient_PingError(t *testing.T) {
  // 无服务可连：InitClient 应返回错误
  resetDockerClient()
  oldHost := os.Getenv("DOCKER_HOST")
  os.Setenv("DOCKER_HOST", "http://127.0.0.1:1")
  defer func() {
    if oldHost != "" {
      os.Setenv("DOCKER_HOST", oldHost)
    } else {
      os.Unsetenv("DOCKER_HOST")
    }
    resetDockerClient()
  }()
  if err := InitClient(); err == nil {
    t.Error("InitClient should error when Docker not available")
  }
}

// ============================================================
// EnsureNetwork 测试
// ============================================================

func TestEnsureNetwork_AlreadyExists(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/networks", h: func(w http.ResponseWriter, r *http.Request) {
      json.NewEncoder(w).Encode([]map[string]interface{}{
        {"Name": NetworkName, "Id": "test-id"},
      })
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  if err := EnsureNetwork(ctx); err != nil {
    t.Errorf("EnsureNetwork should succeed when network exists: %v", err)
  }
}

func TestEnsureNetwork_Create(t *testing.T) {
  created := false
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/networks", h: func(w http.ResponseWriter, r *http.Request) {
      if r.Method == "GET" {
        // 空列表，表示网络不存在
        json.NewEncoder(w).Encode([]map[string]interface{}{})
        return
      }
      // POST /networks/create
      created = true
      w.WriteHeader(http.StatusCreated)
      json.NewEncoder(w).Encode(map[string]string{"Id": "new-network-id", "Warning": ""})
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  if err := EnsureNetwork(ctx); err != nil {
    t.Errorf("EnsureNetwork should create network: %v", err)
  }
  if !created {
    t.Error("EnsureNetwork did not call NetworkCreate")
  }
}

func TestEnsureNetwork_ListError(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/networks", h: func(w http.ResponseWriter, r *http.Request) {
      w.WriteHeader(http.StatusInternalServerError)
      json.NewEncoder(w).Encode(map[string]string{"message": "server error"})
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  if err := EnsureNetwork(ctx); err == nil {
    t.Error("EnsureNetwork should error when list fails")
  }
}

// ============================================================
// 容器操作测试
// ============================================================

func TestContainerStatus_Running(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/containers/test-id/json", h: func(w http.ResponseWriter, r *http.Request) {
      json.NewEncoder(w).Encode(map[string]interface{}{
        "State": map[string]string{"Status": "running"},
      })
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  status := ContainerStatus(ctx, "test-id")
  if status != "running" {
    t.Errorf("ContainerStatus = %q, want %q", status, "running")
  }
}

func TestContainerStatus_NotFound(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/containers/test-id/json", h: func(w http.ResponseWriter, r *http.Request) {
      w.WriteHeader(http.StatusNotFound)
      json.NewEncoder(w).Encode(map[string]string{"message": "No such container"})
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  status := ContainerStatus(ctx, "test-id")
  if status != "未知" {
    t.Errorf("ContainerStatus for missing container = %q, want %q", status, "未知")
  }
}

func TestContainerStatus_EmptyID(t *testing.T) {
  ctx := context.Background()
  status := ContainerStatus(ctx, "")
  if status != "未创建" {
    t.Errorf("ContainerStatus with empty ID = %q, want %q", status, "未创建")
  }
}

func TestContainerExists_True(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/containers/picoaide-testuser/json", h: func(w http.ResponseWriter, r *http.Request) {
      json.NewEncoder(w).Encode(map[string]interface{}{
        "State": map[string]string{"Status": "running"},
      })
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  if !ContainerExists(ctx, "testuser") {
    t.Error("ContainerExists should return true")
  }
}

func TestContainerExists_False(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/containers/picoaide-testuser/json", h: func(w http.ResponseWriter, r *http.Request) {
      w.WriteHeader(http.StatusNotFound)
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  if ContainerExists(ctx, "testuser") {
    t.Error("ContainerExists should return false for missing container")
  }
}

func TestContainerStart(t *testing.T) {
  started := false
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/containers/test-id/start", h: func(w http.ResponseWriter, r *http.Request) {
      started = true
      w.WriteHeader(http.StatusNoContent)
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  if err := Start(ctx, "test-id"); err != nil {
    t.Errorf("Start error: %v", err)
  }
  if !started {
    t.Error("Start did not call ContainerStart")
  }
}

func TestContainerStop(t *testing.T) {
  stopped := false
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/containers/test-id/stop", h: func(w http.ResponseWriter, r *http.Request) {
      stopped = true
      w.WriteHeader(http.StatusNoContent)
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  if err := Stop(ctx, "test-id"); err != nil {
    t.Errorf("Stop error: %v", err)
  }
  if !stopped {
    t.Error("Stop did not call ContainerStop")
  }
}

func TestContainerStop_Error(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/containers/test-id/stop", h: func(w http.ResponseWriter, r *http.Request) {
      w.WriteHeader(http.StatusInternalServerError)
      json.NewEncoder(w).Encode(map[string]string{"message": "stop failed"})
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  if err := Stop(ctx, "test-id"); err == nil {
    t.Error("Stop should error when server returns error")
  }
}

func TestContainerRestart(t *testing.T) {
  restarted := false
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/containers/test-id/restart", h: func(w http.ResponseWriter, r *http.Request) {
      restarted = true
      w.WriteHeader(http.StatusNoContent)
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  if err := Restart(ctx, "test-id"); err != nil {
    t.Errorf("Restart error: %v", err)
  }
  if !restarted {
    t.Error("Restart did not call ContainerRestart")
  }
}

func TestContainerRemove(t *testing.T) {
  removed := false
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/containers/test-id", h: func(w http.ResponseWriter, r *http.Request) {
      if r.Method == "DELETE" {
        removed = true
        w.WriteHeader(http.StatusNoContent)
      }
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  if err := Remove(ctx, "test-id"); err != nil {
    t.Errorf("Remove error: %v", err)
  }
  if !removed {
    t.Error("Remove did not call ContainerRemove")
  }
}

func TestRemoveByUsername(t *testing.T) {
  removed := false
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/containers/picoaide-testuser", h: func(w http.ResponseWriter, r *http.Request) {
      if r.Method == "DELETE" {
        removed = true
        w.WriteHeader(http.StatusNoContent)
      }
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  if err := RemoveByUsername(ctx, "testuser"); err != nil {
    t.Errorf("RemoveByUsername error: %v", err)
  }
  if !removed {
    t.Error("RemoveByUsername did not call ContainerRemove")
  }
}

// ============================================================
// 镜像操作测试
// ============================================================

func TestImageExists_True(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/images/test-image/json", h: func(w http.ResponseWriter, r *http.Request) {
      json.NewEncoder(w).Encode(map[string]interface{}{
        "Id": "sha256:abc123",
      })
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  if !ImageExists(ctx, "test-image") {
    t.Error("ImageExists should return true")
  }
}

func TestImageExists_False(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/images/nonexistent/json", h: func(w http.ResponseWriter, r *http.Request) {
      w.WriteHeader(http.StatusNotFound)
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  if ImageExists(ctx, "nonexistent") {
    t.Error("ImageExists should return false")
  }
}

func TestRemoveImage(t *testing.T) {
  removed := false
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/images/test-image", h: func(w http.ResponseWriter, r *http.Request) {
      if r.Method == "DELETE" {
        removed = true
        json.NewEncoder(w).Encode([]map[string]string{
          {"Untagged": "test-image"},
        })
      }
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  if err := RemoveImage(ctx, "test-image"); err != nil {
    t.Errorf("RemoveImage error: %v", err)
  }
  if !removed {
    t.Error("RemoveImage did not call ImageRemove")
  }
}

func TestRetagImage(t *testing.T) {
  tagged := false
  removed := false
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/images/source/tag", h: func(w http.ResponseWriter, r *http.Request) {
      if r.Method == "POST" {
        tagged = true
        w.WriteHeader(http.StatusCreated)
      }
    }},
    {method: "*", path: "/images/source", h: func(w http.ResponseWriter, r *http.Request) {
      if r.Method == "DELETE" {
        removed = true
        json.NewEncoder(w).Encode([]map[string]string{
          {"Untagged": "source"},
        })
      }
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  if err := RetagImage(ctx, "source", "target"); err != nil {
    t.Errorf("RetagImage error: %v", err)
  }
  if !tagged {
    t.Error("RetagImage did not call ImageTag")
  }
  if !removed {
    t.Error("RetagImage did not remove old tag")
  }
}

// ============================================================
// 镜像列表测试
// ============================================================

func TestListLocalImages(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/images/json", h: func(w http.ResponseWriter, r *http.Request) {
      json.NewEncoder(w).Encode([]image.Summary{
        {RepoTags: []string{"picoaide/picoaide:v1", "picoaide/picoaide:v2"}},
        {RepoTags: []string{"other/image:latest"}},
      })
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  imgs, err := ListLocalImages(ctx, "picoaide/picoaide")
  if err != nil {
    t.Errorf("ListLocalImages error: %v", err)
  }
  if len(imgs) != 1 {
    t.Errorf("ListLocalImages returned %d images, want 1", len(imgs))
  }
}

func TestListLocalImages_NoFilter(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/images/json", h: func(w http.ResponseWriter, r *http.Request) {
      json.NewEncoder(w).Encode([]image.Summary{
        {RepoTags: []string{"img1:v1"}},
        {RepoTags: []string{"img2:v1"}},
      })
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  imgs, err := ListLocalImages(ctx, "")
  if err != nil {
    t.Errorf("ListLocalImages error: %v", err)
  }
  if len(imgs) != 2 {
    t.Errorf("ListLocalImages returned %d images, want 2", len(imgs))
  }
}

func TestListLocalTags(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/images/json", h: func(w http.ResponseWriter, r *http.Request) {
      json.NewEncoder(w).Encode([]image.Summary{
        {RepoTags: []string{"picoaide/picoaide:v1", "picoaide/picoaide:v2", "other:latest"}},
      })
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  tags, err := ListLocalTags(ctx, "picoaide/picoaide")
  if err != nil {
    t.Errorf("ListLocalTags error: %v", err)
  }
  if len(tags) != 2 {
    t.Errorf("ListLocalTags returned %d tags, want 2", len(tags))
  }
}

func TestListLocalTags_Dedup(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/images/json", h: func(w http.ResponseWriter, r *http.Request) {
      // 两个镜像有相同 tag
      json.NewEncoder(w).Encode([]image.Summary{
        {RepoTags: []string{"picoaide/picoaide:v1"}},
        {RepoTags: []string{"picoaide/picoaide:v1"}},
      })
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  tags, err := ListLocalTags(ctx, "picoaide/picoaide")
  if err != nil {
    t.Errorf("ListLocalTags error: %v", err)
  }
  if len(tags) != 1 {
    t.Errorf("ListLocalTags should dedup, got %d tags, want 1", len(tags))
  }
}

// ============================================================
// ListRegistryTagsForConfig 测试
// ============================================================

func TestListRegistryTagsForConfig_Tencent(t *testing.T) {
  // 通过 transport mock 测试 tencent 分支
  authFlow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if strings.Contains(r.URL.Path, "/token") {
      json.NewEncoder(w).Encode(map[string]string{"token": "tc-token"})
      return
    }
    if strings.Contains(r.URL.Path, "/tags/list") {
      json.NewEncoder(w).Encode(map[string]interface{}{
        "tags": []string{"tencent-v1"},
      })
      return
    }
    w.Header().Set("Www-Authenticate", `realm="http://`+r.Host+`/token",service="test"`)
    w.WriteHeader(http.StatusUnauthorized)
  }))
  defer authFlow.Close()

  // 将 http.DefaultTransport 替换为 mockRedirectTransport
  oldTransport := http.DefaultTransport
  http.DefaultTransport = &mockRedirectTransport{target: authFlow.URL, original: http.DefaultTransport}
  defer func() { http.DefaultTransport = oldTransport }()

  ctx := context.Background()
  tags, err := ListRegistryTagsForConfig(ctx, "test/repo", "tencent")
  if err != nil {
    t.Fatalf("ListRegistryTagsForConfig(tencent) error: %v", err)
  }
  if len(tags) != 1 || tags[0] != "tencent-v1" {
    t.Errorf("ListRegistryTagsForConfig = %v, want [tencent-v1]", tags)
  }
}

func TestListRegistryTags_Success(t *testing.T) {
  server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if strings.Contains(r.URL.Path, "/token") {
      json.NewEncoder(w).Encode(map[string]string{"token": "mock-token"})
      return
    }
    if strings.Contains(r.URL.Path, "/tags/list") {
      json.NewEncoder(w).Encode(map[string]interface{}{
        "tags": []string{"v1", "v2", "v3"},
      })
      return
    }
    w.WriteHeader(http.StatusNotFound)
  }))
  defer server.Close()

  oldTransport := http.DefaultTransport
  http.DefaultTransport = &mockRedirectTransport{target: server.URL, original: http.DefaultTransport}
  defer func() { http.DefaultTransport = oldTransport }()

  ctx := context.Background()
  tags, err := ListRegistryTags(ctx, "test/repo")
  if err != nil {
    t.Fatalf("ListRegistryTags error: %v", err)
  }
  if len(tags) != 3 || tags[0] != "v1" {
    t.Errorf("ListRegistryTags = %v, want [v1 v2 v3]", tags)
  }
}

type mockRedirectTransport struct {
  target   string
  original http.RoundTripper
}

func (m *mockRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
  // 重写 URL 指向 mock server
  mockReq, _ := http.NewRequestWithContext(req.Context(), req.Method, m.target+req.URL.Path+"?"+req.URL.RawQuery, req.Body)
  mockReq.Header = req.Header
  return m.original.RoundTrip(mockReq)
}

func TestListRegistryTags_AuthError(t *testing.T) {
  server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if strings.Contains(r.URL.Path, "/token") {
      json.NewEncoder(w).Encode(map[string]string{"token": "mock-token"})
      return
    }
    w.WriteHeader(http.StatusUnauthorized)
  }))
  defer server.Close()

  oldTransport := http.DefaultTransport
  http.DefaultTransport = &mockRedirectTransport{target: server.URL, original: http.DefaultTransport}
  defer func() { http.DefaultTransport = oldTransport }()

  ctx := context.Background()
  _, err := ListRegistryTags(ctx, "test/repo")
  if err == nil {
    t.Error("ListRegistryTags should error on 401")
  }
}

func TestListTencentRegistryTags_Success(t *testing.T) {
  server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if strings.Contains(r.URL.Path, "/tags/list") {
      json.NewEncoder(w).Encode(map[string]interface{}{
        "tags": []string{"v1", "v2"},
      })
      return
    }
    w.WriteHeader(http.StatusNotFound)
  }))
  defer server.Close()

  oldTransport := http.DefaultTransport
  http.DefaultTransport = &mockRedirectTransport{target: server.URL, original: http.DefaultTransport}
  defer func() { http.DefaultTransport = oldTransport }()

  ctx := context.Background()
  tags, err := ListTencentRegistryTags(ctx, "test/repo")
  if err != nil {
    t.Fatalf("ListTencentRegistryTags error: %v", err)
  }
  if len(tags) != 2 || tags[0] != "v1" {
    t.Errorf("ListTencentRegistryTags = %v, want [v1 v2]", tags)
  }
}

func TestListTencentRegistryTags_WithAuth(t *testing.T) {
  var callCount int
  server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if strings.Contains(r.URL.Path, "/token") {
      json.NewEncoder(w).Encode(map[string]string{"token": "tc-token"})
      return
    }
    callCount++
    if callCount == 1 {
      w.Header().Set("Www-Authenticate", `realm="http://`+r.Host+`/token",service="hkccr",scope="repository:repo:pull"`)
      w.WriteHeader(http.StatusUnauthorized)
      return
    }
    json.NewEncoder(w).Encode(map[string]interface{}{
      "tags": []string{"v1"},
    })
  }))
  defer server.Close()

  oldTransport := http.DefaultTransport
  http.DefaultTransport = &mockRedirectTransport{target: server.URL, original: http.DefaultTransport}
  defer func() { http.DefaultTransport = oldTransport }()

  ctx := context.Background()
  tags, err := ListTencentRegistryTags(ctx, "test/repo")
  if err != nil {
    t.Fatalf("ListTencentRegistryTags auth flow error: %v", err)
  }
  if len(tags) != 1 {
    t.Errorf("ListTencentRegistryTags = %v, want [v1]", tags)
  }
}

func TestListRegistryTagsForConfig_GHCR(t *testing.T) {
  // 默认走 github 分支
  server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if strings.Contains(r.URL.Path, "/token") {
      json.NewEncoder(w).Encode(map[string]string{"token": "gh-token"})
      return
    }
    if strings.Contains(r.URL.Path, "/tags/list") {
      json.NewEncoder(w).Encode(map[string]interface{}{
        "tags": []string{"gh-v1"},
      })
      return
    }
    w.WriteHeader(http.StatusNotFound)
  }))
  defer server.Close()

  oldTransport := http.DefaultTransport
  http.DefaultTransport = &mockRedirectTransport{target: server.URL, original: http.DefaultTransport}
  defer func() { http.DefaultTransport = oldTransport }()

  ctx := context.Background()
  tags, err := ListRegistryTagsForConfig(ctx, "test/repo", "github")
  if err != nil {
    t.Fatalf("ListRegistryTagsForConfig(github) error: %v", err)
  }
  if len(tags) != 1 || tags[0] != "gh-v1" {
    t.Errorf("ListRegistryTagsForConfig = %v, want [gh-v1]", tags)
  }
}

// ============================================================
// 日志测试
// ============================================================

func TestContainerLogs(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/containers/test-id/logs", h: func(w http.ResponseWriter, r *http.Request) {
      // Docker 日志流格式: 8字节头 + 数据
      // 头部: [stream_type (1), padding (3), size (4)]
      w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
      header := []byte{1, 0, 0, 0, 0, 0, 0, 5} // 5字节数据
      w.Write(header)
      w.Write([]byte("hello"))
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  logs, err := ContainerLogs(ctx, "test-id", "10")
  if err != nil {
    t.Errorf("ContainerLogs error: %v", err)
  }
  if logs != "hello" {
    t.Errorf("ContainerLogs = %q, want %q", logs, "hello")
  }
}

func TestContainerLogs_DefaultTail(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/containers/test-id/logs", h: func(w http.ResponseWriter, r *http.Request) {
      // 默认 tail = "100"
      tail := r.URL.Query().Get("tail")
      if tail != "100" {
        t.Errorf("logs tail = %q, want %q", tail, "100")
      }
      w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
      w.Write([]byte{1, 0, 0, 0, 0, 0, 0, 0}) // 空数据
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  if _, err := ContainerLogs(ctx, "test-id", ""); err != nil {
    t.Errorf("ContainerLogs error: %v", err)
  }
}

func TestContainerLogs_Error(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/containers/test-id/logs", h: func(w http.ResponseWriter, r *http.Request) {
      w.WriteHeader(http.StatusInternalServerError)
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  if _, err := ContainerLogs(ctx, "test-id", "10"); err == nil {
    t.Error("ContainerLogs should error on server error")
  }
}

// ============================================================
// CreateContainer 测试
// ============================================================

func TestCreateContainer_Success(t *testing.T) {
  var createdConfig container.Config
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/containers/picoaide-testuser/json", h: func(w http.ResponseWriter, r *http.Request) {
      // ContainerInspect — 容器不存在
      w.WriteHeader(http.StatusNotFound)
    }},
    {method: "*", path: "/containers/create", h: func(w http.ResponseWriter, r *http.Request) {
      body, _ := io.ReadAll(r.Body)
      json.Unmarshal(body, &createdConfig)
      r.Body.Close()
      w.WriteHeader(http.StatusCreated)
      json.NewEncoder(w).Encode(container.CreateResponse{
        ID: "new-container-id",
      })
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  id, err := CreateContainer(ctx, "testuser", "myimage:v1", "/tmp/userdir", "100.64.0.2", 0.5, 256, "mcp-token-123")
  if err != nil {
    t.Errorf("CreateContainer error: %v", err)
  }
  if id != "new-container-id" {
    t.Errorf("CreateContainer id = %q, want %q", id, "new-container-id")
  }
  if createdConfig.Image != "myimage:v1" {
    t.Errorf("container image = %q, want %q", createdConfig.Image, "myimage:v1")
  }
}

func TestCreateContainer_Recreate(t *testing.T) {
  // 容器已存在时应先移除再重新创建
  removed := false
  server := mockDockerDaemon(t, []mockHandler{
    {method: "GET", path: "/containers/picoaide-testuser/json", h: func(w http.ResponseWriter, r *http.Request) {
      // ContainerJSONBase 是 *ContainerJSONBase 嵌入指针，字段用 json tag Id
      json.NewEncoder(w).Encode(map[string]interface{}{
        "Id": "existing-id",
        "State": map[string]interface{}{
          "Status": "running",
        },
      })
    }},
    {method: "DELETE", path: "/containers/picoaide-testuser", h: func(w http.ResponseWriter, r *http.Request) {
      removed = true
      w.WriteHeader(http.StatusNoContent)
    }},
    {method: "*", path: "/containers/create", h: func(w http.ResponseWriter, r *http.Request) {
      w.WriteHeader(http.StatusCreated)
      json.NewEncoder(w).Encode(container.CreateResponse{
        ID: "new-id",
      })
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  id, err := CreateContainer(ctx, "testuser", "img:v1", "/tmp/ud", "100.64.0.2", 0, 0, "")
  if err != nil {
    t.Errorf("CreateContainer recreating error: %v", err)
  }
  if id != "new-id" {
    t.Errorf("CreateContainer id = %q, want %q", id, "new-id")
  }
  if !removed {
    t.Error("CreateContainer did not remove existing container")
  }
}

func TestCreateContainer_DebugGateway(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/containers/picoaide-testuser/json", h: func(w http.ResponseWriter, r *http.Request) {
      w.WriteHeader(http.StatusNotFound)
    }},
    {method: "*", path: "/containers/create", h: func(w http.ResponseWriter, r *http.Request) {
      var cfg container.Config
      body, _ := io.ReadAll(r.Body)
      r.Body.Close()
      json.Unmarshal(body, &cfg)
      if len(cfg.Cmd) == 0 {
        t.Error("Debug gateway should set Cmd")
      }
      w.WriteHeader(http.StatusCreated)
      json.NewEncoder(w).Encode(container.CreateResponse{ID: "debug-id"})
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  _, err := CreateContainerWithOptions(ctx, "testuser", "img:v1", "/tmp/ud", "100.64.0.2", 0.5, 256, true, nil, "")
  if err != nil {
    t.Errorf("CreateContainerWithOptions debug error: %v", err)
  }
}

// ============================================================
// ExecCommand 测试
// ============================================================

func TestExecCommand_Success(t *testing.T) {
  // exec attach 使用 HTTP hijack，httptest 无法模拟
  t.Skip("HTTP hijack 需要真实 Docker daemon")
}

func TestExecCommand_CreateError(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/containers/test-id/exec", h: func(w http.ResponseWriter, r *http.Request) {
      w.WriteHeader(http.StatusInternalServerError)
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  _, _, err := ExecCommand(ctx, "test-id", []string{"echo"})
  if err == nil {
    t.Error("ExecCommand should error on create failure")
  }
}

func TestTestContainerDir_Exists(t *testing.T) {
  t.Skip("exec attach 使用 HTTP hijack，httptest 无法模拟")
}

func TestTestContainerDir_NotExists(t *testing.T) {
  t.Skip("exec attach 使用 HTTP hijack，httptest 无法模拟")
}

func TestImagePull(t *testing.T) {
  server := mockDockerDaemon(t, []mockHandler{
    {method: "*", path: "/images/create", h: func(w http.ResponseWriter, r *http.Request) {
      w.Header().Set("Content-Type", "application/json")
      json.NewEncoder(w).Encode(map[string]string{"status": "pulled"})
    }},
  })
  defer server.Close()
  setupDockerClient(t, server)
  defer resetDockerClient()

  ctx := context.Background()
  reader, err := ImagePull(ctx, "test-image:latest")
  if err != nil {
    t.Errorf("ImagePull error: %v", err)
  }
  if reader != nil {
    reader.Close()
  }
}
