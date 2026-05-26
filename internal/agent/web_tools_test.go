package agent

import (
  "context"
  "encoding/json"
  "net/http"
  "net/http/httptest"
  "strings"
  "testing"
)

// ============================================================
// WebSearchTool
// ============================================================

func TestWebSearchTool_Name(t *testing.T) {
  tool := &WebSearchTool{}
  if tool.Name() != "web_search" {
    t.Errorf("Name = %q, want web_search", tool.Name())
  }
}

func TestWebSearchTool_Description(t *testing.T) {
  tool := &WebSearchTool{}
  if tool.Description() == "" {
    t.Error("Description should not be empty")
  }
}

func TestWebSearchTool_Schema(t *testing.T) {
  tool := &WebSearchTool{}
  schema := tool.Schema()
  if schema == nil {
    t.Fatal("Schema should not be nil")
  }
  props, ok := schema["properties"].(map[string]interface{})
  if !ok {
    t.Fatal("schema missing properties")
  }
  if _, ok := props["query"]; !ok {
    t.Error("schema missing 'query' property")
  }
}

func TestWebSearchTool_Success(t *testing.T) {
  mockDDG := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte(`<html><body>
      <a rel="nofollow" class="result__a" href="https://example.com">Example Title</a>
      <a rel="nofollow" class="result__a" href="https://test.org">Test <b>Site</b></a>
    </body></html>`))
  }))
  defer mockDDG.Close()

  tool := &WebSearchTool{baseURL: mockDDG.URL, client: mockDDG.Client()}
  args, _ := json.Marshal(map[string]interface{}{"query": "test"})
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }
  if !strings.Contains(result.Data, "Example Title") {
    t.Errorf("missing 'Example Title' in:\n%s", result.Data)
  }
  if !strings.Contains(result.Data, "https://example.com") {
    t.Errorf("missing URL in:\n%s", result.Data)
  }
  if !strings.Contains(result.Data, "Test Site") {
    t.Errorf("missing 'Test Site' (with stripped tags) in:\n%s", result.Data)
  }
}

func TestWebSearchTool_NoResults(t *testing.T) {
  mockDDG := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte(`<html><body>No results found.</body></html>`))
  }))
  defer mockDDG.Close()

  tool := &WebSearchTool{baseURL: mockDDG.URL, client: mockDDG.Client()}
  args, _ := json.Marshal(map[string]interface{}{"query": "xyzzy"})
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }
  if !strings.Contains(result.Data, "无搜索") {
    t.Errorf("expected '无搜索' message, got: %s", result.Data)
  }
}

func TestWebSearchTool_EmptyQuery(t *testing.T) {
  tool := &WebSearchTool{}
  args, _ := json.Marshal(map[string]interface{}{"query": ""})
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Errorf("expected failure for empty query, got success: %s", result.Data)
  }
  if !strings.Contains(result.Data, "不能为空") {
    t.Errorf("expected '不能为空' message, got: %s", result.Data)
  }
}

func TestWebSearchTool_HTTPError(t *testing.T) {
  mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusInternalServerError)
  }))
  defer mockServer.Close()

  tool := &WebSearchTool{baseURL: mockServer.URL, client: mockServer.Client()}
  args, _ := json.Marshal(map[string]interface{}{"query": "test"})
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Errorf("expected graceful error handling, got: %s", result.Data)
  }
  if !strings.Contains(result.Data, "搜索失败") {
    t.Errorf("expected '搜索失败' in message, got: %s", result.Data)
  }
}

// ============================================================
// WebFetchTool
// ============================================================

func TestWebFetchTool_Name(t *testing.T) {
  tool := &WebFetchTool{}
  if tool.Name() != "web_fetch" {
    t.Errorf("Name = %q, want web_fetch", tool.Name())
  }
}

func TestWebFetchTool_Description(t *testing.T) {
  tool := &WebFetchTool{}
  if tool.Description() == "" {
    t.Error("Description should not be empty")
  }
}

func TestWebFetchTool_Schema(t *testing.T) {
  tool := &WebFetchTool{}
  schema := tool.Schema()
  if schema == nil {
    t.Fatal("Schema should not be nil")
  }
  props, ok := schema["properties"].(map[string]interface{})
  if !ok {
    t.Fatal("schema missing properties")
  }
  if _, ok := props["url"]; !ok {
    t.Error("schema missing 'url' property")
  }
}

func TestWebFetchTool_Success(t *testing.T) {
  mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("hello world"))
  }))
  defer mockServer.Close()

  tool := &WebFetchTool{client: mockServer.Client()}
  args, _ := json.Marshal(map[string]interface{}{"url": mockServer.URL})
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }
  if !strings.Contains(result.Data, "hello world") {
    t.Errorf("missing content in: %s", result.Data)
  }
}

func TestWebFetchTool_EmptyURL(t *testing.T) {
  tool := &WebFetchTool{}
  args, _ := json.Marshal(map[string]interface{}{"url": ""})
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if result.Success {
    t.Errorf("expected failure for empty URL, got success: %s", result.Data)
  }
  if !strings.Contains(result.Data, "不能为空") {
    t.Errorf("expected '不能为空' message, got: %s", result.Data)
  }
}

func TestWebFetchTool_HTTPError(t *testing.T) {
  mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNotFound)
    w.Write([]byte("not found"))
  }))
  defer mockServer.Close()

  tool := &WebFetchTool{client: mockServer.Client()}
  args, _ := json.Marshal(map[string]interface{}{"url": mockServer.URL + "/nonexistent"})
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Errorf("expected graceful handling, got: %s", result.Data)
  }
}

func TestWebFetchTool_LargeResponseBody(t *testing.T) {
  largeContent := strings.Repeat("ABCD", 128*1024) // 512KB
  mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte(largeContent))
  }))
  defer mockServer.Close()

  tool := &WebFetchTool{client: mockServer.Client()}
  args, _ := json.Marshal(map[string]interface{}{"url": mockServer.URL})
  result, err := tool.Execute(context.Background(), args)
  if err != nil {
    t.Fatal(err)
  }
  if !result.Success {
    t.Fatalf("expected success, got: %s", result.Data)
  }
  // Output should be truncated to 50000 chars
  if len(result.Data) > 50100 {
    t.Errorf("output too long: %d chars (expected ~50000)", len(result.Data))
  }
}
