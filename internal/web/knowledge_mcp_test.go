package web

import (
  "encoding/json"
  "net/http/httptest"
  "testing"

  "github.com/gin-gonic/gin"

  "github.com/picoaide/picoaide/internal/store"
)

func TestKBSearchHandlerRegistered(t *testing.T) {
  if _, ok := picoaideHandlers["kb_search"]; !ok {
    t.Error("kb_search handler not registered")
  }
}

func TestKBSearchToolDefExists(t *testing.T) {
  found := false
  for _, def := range picoaideToolDefs {
    if def.Name == "kb_search" {
      found = true
      break
    }
  }
  if !found {
    t.Error("kb_search tool def not found in picoaideToolDefs")
  }
}

func TestKBSearch_SearchMode(t *testing.T) {
  env := setupTestServer(t)
  gin.SetMode(gin.TestMode)

  // Create KB and document
  kb, _ := store.CreateKnowledgeBase("mcp-search", "", "testadmin")
  folders, _ := store.GetFolderTree(kb.ID)
  rootID := folders[0].ID
  store.CreateDocument(kb.ID, rootID, "MCP Test Doc", "This is a searchable content for MCP testing", "manual", "md", "testadmin", 0)

  w := httptest.NewRecorder()
  c, _ := gin.CreateTestContext(w)
  handleKBSearch(env.Server, c, "1", map[string]interface{}{
    "scope": "search",
    "query": "searchable",
    "page":  float64(1),
    "page_size": float64(10),
  }, "testadmin")

  var resp struct {
    Result struct {
      Content []struct {
        Type string `json:"type"`
        Text string `json:"text"`
      } `json:"content"`
      IsError bool `json:"isError"`
    } `json:"result"`
  }
  if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
    t.Fatalf("unmarshal: %v", err)
  }
  if resp.Result.IsError {
    t.Fatalf("expected success, got error: %s", resp.Result.Content[0].Text)
  }
}

func TestKBSearch_SearchMode_MissingQuery(t *testing.T) {
  env := setupTestServer(t)
  gin.SetMode(gin.TestMode)

  w := httptest.NewRecorder()
  c, _ := gin.CreateTestContext(w)
  handleKBSearch(env.Server, c, "2", map[string]interface{}{
    "scope": "search",
    "query": "",
  }, "testadmin")

  var resp struct {
    Result struct {
      Content []struct {
        Type string `json:"type"`
        Text string `json:"text"`
      } `json:"content"`
      IsError bool `json:"isError"`
    } `json:"result"`
  }
  json.Unmarshal(w.Body.Bytes(), &resp)
  if !resp.Result.IsError {
    t.Error("expected error for missing query")
  }
}

func TestKBSearch_ReadMode(t *testing.T) {
  env := setupTestServer(t)
  gin.SetMode(gin.TestMode)

  kb, _ := store.CreateKnowledgeBase("mcp-read", "", "testadmin")
  folders, _ := store.GetFolderTree(kb.ID)
  rootID := folders[0].ID
  doc, _ := store.CreateDocument(kb.ID, rootID, "Readable Doc", "This is the content of the document", "manual", "md", "testadmin", 0)

  w := httptest.NewRecorder()
  c, _ := gin.CreateTestContext(w)
  handleKBSearch(env.Server, c, "3", map[string]interface{}{
    "scope":  "read",
    "doc_id": float64(doc.ID),
  }, "testadmin")

  var resp struct {
    Result struct {
      Content []struct {
        Type string `json:"type"`
        Text string `json:"text"`
      } `json:"content"`
      IsError bool `json:"isError"`
    } `json:"result"`
  }
  if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
    t.Fatalf("unmarshal: %v", err)
  }
  if resp.Result.IsError {
    t.Fatalf("expected success, got error: %s", resp.Result.Content[0].Text)
  }
}

func TestKBSearch_ReadMode_MissingDocID(t *testing.T) {
  env := setupTestServer(t)
  gin.SetMode(gin.TestMode)

  w := httptest.NewRecorder()
  c, _ := gin.CreateTestContext(w)
  handleKBSearch(env.Server, c, "4", map[string]interface{}{
    "scope": "read",
  }, "testadmin")

  var resp struct {
    Result struct {
      Content []struct {
        Type string `json:"type"`
        Text string `json:"text"`
      } `json:"content"`
      IsError bool `json:"isError"`
    } `json:"result"`
  }
  json.Unmarshal(w.Body.Bytes(), &resp)
  if !resp.Result.IsError {
    t.Error("expected error for missing doc_id")
  }
}

func TestKBSearch_BrowseMode(t *testing.T) {
  env := setupTestServer(t)
  gin.SetMode(gin.TestMode)

  kb, _ := store.CreateKnowledgeBase("mcp-browse", "", "testadmin")
  folders, _ := store.GetFolderTree(kb.ID)
  rootID := folders[0].ID
  store.CreateDocument(kb.ID, rootID, "Browse Doc", "browse content", "manual", "md", "testadmin", 0)

  w := httptest.NewRecorder()
  c, _ := gin.CreateTestContext(w)
  handleKBSearch(env.Server, c, "5", map[string]interface{}{
    "scope":     "browse",
    "folder_id": float64(rootID),
  }, "testadmin")

  var resp struct {
    Result struct {
      Content []struct {
        Type string `json:"type"`
        Text string `json:"text"`
      } `json:"content"`
      IsError bool `json:"isError"`
    } `json:"result"`
  }
  if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
    t.Fatalf("unmarshal: %v", err)
  }
  if resp.Result.IsError {
    t.Fatalf("expected success, got error: %s", resp.Result.Content[0].Text)
  }
}

func TestKBSearch_InvalidScope(t *testing.T) {
  env := setupTestServer(t)
  gin.SetMode(gin.TestMode)

  w := httptest.NewRecorder()
  c, _ := gin.CreateTestContext(w)
  handleKBSearch(env.Server, c, "6", map[string]interface{}{
    "scope": "invalid",
  }, "testadmin")

  var resp struct {
    Result struct {
      Content []struct {
        Type string `json:"type"`
        Text string `json:"text"`
      } `json:"content"`
      IsError bool `json:"isError"`
    } `json:"result"`
  }
  json.Unmarshal(w.Body.Bytes(), &resp)
  if !resp.Result.IsError {
    t.Error("expected error for invalid scope")
  }
}
