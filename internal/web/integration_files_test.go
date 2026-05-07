package web

import (
  "net/url"
  "strings"
  "testing"
)

func TestMCPToken_GenerateAndRetrieve(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/mcp/token", "testuser")
  assertStatus(t, resp, 200)
  var result struct {
    Success bool   `json:"success"`
    Token   string `json:"token"`
  }
  parseJSON(t, resp, &result)
  if result.Token == "" {
    t.Error("should have token")
  }

  // 再次获取应为同一个 token
  resp2 := env.get(t, "/api/mcp/token", "testuser")
  assertStatus(t, resp2, 200)
  var result2 struct {
    Token string `json:"token"`
  }
  parseJSON(t, resp2, &result2)
  if result.Token != result2.Token {
    t.Error("token should be stable")
  }
}

func TestFiles_ListRoot(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/files", "testuser")
  assertStatus(t, resp, 200)
}

func TestFiles_Mkdir(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{
    "path": {""},
    "name": {"testdir"},
  }
  resp := env.postForm(t, "/api/files/mkdir", "testuser", form)
  assertStatus(t, resp, 200)
}

func TestFiles_Mkdir_InvalidName(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{
    "path": {""},
    "name": {".."},
  }
  resp := env.postForm(t, "/api/files/mkdir", "testuser", form)
  assertStatus(t, resp, 400)
}

func TestFiles_EditReadAndSave(t *testing.T) {
  env := setupTestServer(t)
  // 创建文本文件
  form := url.Values{
    "path":    {"hello.txt"},
    "content": {"Hello World"},
  }
  resp := env.postForm(t, "/api/files/edit", "testuser", form)
  assertStatus(t, resp, 200)

  // 读取回来
  resp = env.get(t, "/api/files/edit?path=hello.txt", "testuser")
  assertStatus(t, resp, 200)
  var result struct {
    Success bool   `json:"success"`
    Content string `json:"content"`
  }
  parseJSON(t, resp, &result)
  if result.Content != "Hello World" {
    t.Errorf("content=%q, want %q", result.Content, "Hello World")
  }
}

func TestFiles_Download(t *testing.T) {
  env := setupTestServer(t)
  // 先创建文件
  form := url.Values{
    "path":    {"download.txt"},
    "content": {"download me"},
  }
  env.postForm(t, "/api/files/edit", "testuser", form)

  resp := env.get(t, "/api/files/download?path=download.txt", "testuser")
  assertStatus(t, resp, 200)
  ct := resp.Header.Get("Content-Disposition")
  if !strings.Contains(ct, "download.txt") {
    t.Errorf("Content-Disposition=%q", ct)
  }
}

func TestFiles_Delete(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{
    "path":    {"to-delete.txt"},
    "content": {"temp"},
  }
  env.postForm(t, "/api/files/edit", "testuser", form)

  delForm := url.Values{"path": {"to-delete.txt"}}
  resp := env.postForm(t, "/api/files/delete", "testuser", delForm)
  assertStatus(t, resp, 200)
}

func TestFiles_DeleteRoot_Prevented(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{"path": {""}}
  resp := env.postForm(t, "/api/files/delete", "testuser", form)
  assertStatus(t, resp, 400)
}

func TestDingTalk_GetEmpty(t *testing.T) {
  env := setupTestServer(t)
  resp := env.get(t, "/api/dingtalk", "testuser")
  assertStatus(t, resp, 200)
}

func TestDingTalk_Save(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{
    "client_id":     {"test-id"},
    "client_secret": {"test-secret"},
  }
  resp := env.postForm(t, "/api/dingtalk", "testuser", form)
  assertStatus(t, resp, 200)
}

func TestCookies_Success(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{
    "domain":  {"example.com"},
    "cookies": {"session=abc123"},
  }
  resp := env.postForm(t, "/api/cookies", "testuser", form)
  assertStatus(t, resp, 200)
}

func TestCookies_MissingFields(t *testing.T) {
  env := setupTestServer(t)
  form := url.Values{
    "domain":  {""},
    "cookies": {""},
  }
  resp := env.postForm(t, "/api/cookies", "testuser", form)
  assertStatus(t, resp, 400)
}
