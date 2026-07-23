package knowledge

import (
  "net/http"
  "net/http/httptest"
  "testing"
  "time"

  "github.com/picoaide/picoaide/internal/store"
)

func TestSyncChecker_NoMatchingDocument(t *testing.T) {
  tmpDir := t.TempDir()
  store.ResetDB()
  if err := store.InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB: %v", err)
  }
  defer store.ResetDB()

  kb, err := store.CreateKnowledgeBase("sync-nodoc", "", "admin")
  if err != nil {
    t.Fatalf("CreateKnowledgeBase: %v", err)
  }

  ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("content"))
  }))
  defer ts.Close()

  src, _ := store.CreateSyncSource(kb.ID, ts.URL, "no matching doc")
  store.UpdateSyncSource(src.ID, 1, "")

  checker := NewSyncChecker()
  checker.interval = 100 * time.Millisecond
  checker.runOnce()

  // Should not panic or error when no matching document exists
  engine, _ := store.GetEngine()
  var stored store.KBSyncSource
  has, _ := engine.Where("id = ?", src.ID).Get(&stored)
  if !has {
    t.Fatal("sync source should exist")
  }
  if stored.Checksum != "" {
    t.Logf("checksum = %q (set even without matching doc)", stored.Checksum)
  }
}

func TestSyncChecker_NetworkError(t *testing.T) {
  tmpDir := t.TempDir()
  store.ResetDB()
  if err := store.InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB: %v", err)
  }
  defer store.ResetDB()

  kb, _ := store.CreateKnowledgeBase("sync-err", "", "admin")
  var root store.KBFolder
  engine, _ := store.GetEngine()
  engine.Where("kb_id = ? AND name = '/'", kb.ID).Get(&root)
  store.CreateDocument(kb.ID, root.ID, "error doc", "old", "web", "md", "admin")
  engine.Exec("UPDATE kb_documents SET url = ? WHERE kb_id = ?", "http://127.0.0.1:1/nonexistent", kb.ID)
  src, _ := store.CreateSyncSource(kb.ID, "http://127.0.0.1:1/nonexistent", "broken source")
  store.UpdateSyncSource(src.ID, 1, "")

  checker := NewSyncChecker()
  checker.runOnce()

  var doc store.KBDocument
  engine.Where("kb_id = ?", kb.ID).Get(&doc)
  if doc.Content != "old" {
    t.Errorf("content should remain unchanged after network error, got %q", doc.Content)
  }
}

func TestSyncChecker_Non200Response(t *testing.T) {
  tmpDir := t.TempDir()
  store.ResetDB()
  if err := store.InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB: %v", err)
  }
  defer store.ResetDB()

  kb, _ := store.CreateKnowledgeBase("sync-500", "", "admin")
  var root store.KBFolder
  engine, _ := store.GetEngine()
  engine.Where("kb_id = ? AND name = '/'", kb.ID).Get(&root)
  store.CreateDocument(kb.ID, root.ID, "error doc", "old content", "web", "md", "admin")

  ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusInternalServerError)
    w.Write([]byte("error page"))
  }))
  defer ts.Close()

  engine.Exec("UPDATE kb_documents SET url = ? WHERE kb_id = ?", ts.URL, kb.ID)
  src, _ := store.CreateSyncSource(kb.ID, ts.URL, "500 source")
  store.UpdateSyncSource(src.ID, 1, "")

  checker := NewSyncChecker()
  checker.runOnce()

  var doc store.KBDocument
  engine.Where("kb_id = ?", kb.ID).Get(&doc)
  // Sync doesn't check HTTP status, so content gets updated anyway
  if doc.Content != "error page" {
    t.Errorf("content = %q, want 'error page' (sync reads body regardless of status)", doc.Content)
  }
}

func TestSyncChecker_MultipleSources(t *testing.T) {
  tmpDir := t.TempDir()
  store.ResetDB()
  if err := store.InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB: %v", err)
  }
  defer store.ResetDB()

  kb, _ := store.CreateKnowledgeBase("multi-sync", "", "admin")
  var root store.KBFolder
  engine, _ := store.GetEngine()
  engine.Where("kb_id = ? AND name = '/'", kb.ID).Get(&root)

  ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("source1 content"))
  }))
  defer ts1.Close()

  ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("source2 content"))
  }))
  defer ts2.Close()

  store.CreateDocument(kb.ID, root.ID, "doc1", "old1", "web", "md", "admin")
  engine.Exec("UPDATE kb_documents SET url = ? WHERE kb_id = ?", ts1.URL, kb.ID)
  src1, _ := store.CreateSyncSource(kb.ID, ts1.URL, "source 1")
  store.UpdateSyncSource(src1.ID, 1, "")

  store.CreateDocument(kb.ID, root.ID, "doc2", "old2", "web", "md", "admin")
  engine.Exec("UPDATE kb_documents SET url = ? WHERE kb_id = ? AND title = 'doc2'", ts2.URL, kb.ID)
  src2, _ := store.CreateSyncSource(kb.ID, ts2.URL, "source 2")
  store.UpdateSyncSource(src2.ID, 1, "")

  checker := NewSyncChecker()
  checker.runOnce()

  // Both documents should be updated
  var docs []store.KBDocument
  engine.Where("kb_id = ?", kb.ID).Find(&docs)
  contentMap := make(map[string]string)
  for _, d := range docs {
    contentMap[d.Title] = d.Content
  }
  if contentMap["doc1"] != "source1 content" {
    t.Errorf("doc1 content = %q, want 'source1 content'", contentMap["doc1"])
  }
  if contentMap["doc2"] != "source2 content" {
    t.Errorf("doc2 content = %q, want 'source2 content'", contentMap["doc2"])
  }
}

func TestSyncChecker(t *testing.T) {
  // Init DB with KB tables
  tmpDir := t.TempDir()
  store.ResetDB()
  if err := store.InitDB(tmpDir); err != nil {
    t.Fatalf("InitDB: %v", err)
  }
  defer store.ResetDB()

  kb, err := store.CreateKnowledgeBase("sync-test", "", "admin")
  if err != nil {
    t.Fatalf("CreateKnowledgeBase: %v", err)
  }

  var root store.KBFolder
  engine, _ := store.GetEngine()
  engine.Where("kb_id = ? AND name = '/'", kb.ID).Get(&root)

  // Start a test server: returns same content for first 2 calls, changes on 3rd
  callCount := 0
  ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    callCount++
    if callCount >= 3 {
      w.Write([]byte("content v2"))
    } else {
      w.Write([]byte("content v1"))
    }
  }))
  defer ts.Close()

  // Create a document with matching URL
  _, _ = store.CreateDocument(kb.ID, root.ID, "external doc", "old content", "web", "md", "admin")
  engine.Exec("UPDATE kb_documents SET url = ? WHERE kb_id = ?", ts.URL, kb.ID)

  src, err := store.CreateSyncSource(kb.ID, ts.URL, "test source")
  if err != nil {
    t.Fatalf("CreateSyncSource: %v", err)
  }
  store.UpdateSyncSource(src.ID, 1, "")

  // Run sync
  checker := NewSyncChecker()
  checker.interval = 100 * time.Millisecond
  checker.runOnce()

  // Verify document was updated
  var doc store.KBDocument
  engine.Where("kb_id = ?", kb.ID).Get(&doc)
  if doc.Content != "content v1" {
    t.Errorf("doc content = %q, want 'content v1'", doc.Content)
  }

  // Re-read source checksum
  var src2 store.KBSyncSource
  engine.Where("id = ?", src.ID).Get(&src2)
  if src2.Checksum == "" {
    t.Error("expected checksum to be set")
  }
  rows, _ := engine.Query("SELECT last_sync_at FROM kb_sync_sources WHERE id = ?", src.ID)
  if len(rows) == 0 || string(rows[0]["last_sync_at"]) == "" {
    t.Error("expected last_sync_at in DB")
  }

  // Run again - no content change
  checker.runOnce()
  var doc2 store.KBDocument
  engine.Where("kb_id = ?", kb.ID).Get(&doc2)
  if doc2.Content != "content v1" {
    t.Errorf("doc content should be unchanged, got %q", doc2.Content)
  }

  // Run third time - content changed
  checker.runOnce()
  var doc3 store.KBDocument
  engine.Where("kb_id = ?", kb.ID).Get(&doc3)
  if doc3.Content != "content v2" {
    t.Errorf("doc content = %q, want 'content v2'", doc3.Content)
  }
}
