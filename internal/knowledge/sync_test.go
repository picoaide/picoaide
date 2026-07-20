package knowledge

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/picoaide/picoaide/internal/store"
)

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
