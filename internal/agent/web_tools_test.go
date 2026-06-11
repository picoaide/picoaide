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
  if len(result.Data) > 50100 {
    t.Errorf("output too long: %d chars (expected ~50000)", len(result.Data))
  }
}
