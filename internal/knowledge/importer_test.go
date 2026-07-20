package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/picoaide/picoaide/internal/llm"
	"github.com/picoaide/picoaide/internal/store"
)

func initTestDB(t *testing.T) {
	t.Helper()
	store.ResetDB()
	if err := store.InitDB(t.TempDir()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(store.ResetDB)
}

func createTestKB(t *testing.T, name, desc, user string) *store.KnowledgeBase {
	t.Helper()
	kb, err := store.CreateKnowledgeBase(name, desc, user)
	if err != nil {
		t.Fatalf("CreateKnowledgeBase: %v", err)
	}
	return kb
}

func getRootFolderID(t *testing.T, kbID int64) int64 {
	t.Helper()
	tree, err := store.GetFolderTree(kbID)
	if err != nil {
		t.Fatalf("GetFolderTree: %v", err)
	}
	for _, f := range tree {
		if f.Name == "/" {
			return f.ID
		}
	}
	t.Fatal("root folder not found")
	return 0
}

func TestImportQueue_EnqueueDequeue(t *testing.T) {
	q := NewImportQueue(10)
	task := &ImportTask{ID: "1", FileName: "test.md"}
	if err := q.Enqueue(task); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	select {
	case got := <-q.Tasks():
		if got.ID != "1" {
			t.Errorf("got ID %q, want %q", got.ID, "1")
		}
	default:
		t.Error("expected task on channel")
	}
}

func TestImportQueue_Full(t *testing.T) {
	q := NewImportQueue(1)
	if err := q.Enqueue(&ImportTask{ID: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(&ImportTask{ID: "2"}); err == nil {
		t.Error("expected error for full queue")
	}
}

func TestPipeline_Process_MD(t *testing.T) {
	initTestDB(t)
	kb := createTestKB(t, "pipeline-test", "", "alice")
	rootID := getRootFolderID(t, kb.ID)

	store.CreateImportTask("test-task-md", kb.ID, "alice")

	q := NewImportQueue(10)
	p := NewPipeline(q, nil, nil)

	p.Process(&ImportTask{
		ID:       "test-task-md",
		KbID:     kb.ID,
		FolderID: rootID,
		FileName: "test.md",
		Data:     []byte("# Hello\nWorld content"),
		Username: "alice",
	})

	docs, err := store.GetDocumentsByKB(kb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}
	if docs[0].Title != "Hello" {
		t.Errorf("title = %q, want %q", docs[0].Title, "Hello")
	}
}

func TestPipeline_Process_ParseError(t *testing.T) {
	initTestDB(t)
	kb := createTestKB(t, "parse-error", "", "alice")
	rootID := getRootFolderID(t, kb.ID)

	store.CreateImportTask("test-task-err", kb.ID, "alice")

	q := NewImportQueue(10)
	p := NewPipeline(q, nil, nil)

	p.Process(&ImportTask{
		ID:       "test-task-err",
		KbID:     kb.ID,
		FolderID: rootID,
		FileName: "test.xyz",
		Data:     []byte("unknown"),
		Username: "alice",
	})

	docs, err := store.GetDocumentsByKB(kb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 0 {
		t.Errorf("expected 0 documents, got %d", len(docs))
	}
}

func TestPipeline_Debounce(t *testing.T) {
	initTestDB(t)
	kb := createTestKB(t, "debounce", "", "alice")
	rootID := getRootFolderID(t, kb.ID)

	q := NewImportQueue(10)
	p := NewPipeline(q, nil, NewLinker())

	store.CreateImportTask("debounce-1", kb.ID, "alice")
	store.CreateImportTask("debounce-2", kb.ID, "alice")

	p.Process(&ImportTask{
		ID: "debounce-1", KbID: kb.ID, FolderID: rootID,
		FileName: "a.md", Data: []byte("# A\ncontent a"), Username: "alice",
	})
	p.Process(&ImportTask{
		ID: "debounce-2", KbID: kb.ID, FolderID: rootID,
		FileName: "b.md", Data: []byte("# B\ncontent b"), Username: "alice",
	})

	time.Sleep(500 * time.Millisecond)

	docs, err := store.GetDocumentsByKB(kb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Errorf("expected 2 docs, got %d", len(docs))
	}
}

func TestPipeline_Start_ProcessesQueue(t *testing.T) {
	initTestDB(t)
	kb := createTestKB(t, "start-test", "", "alice")
	rootID := getRootFolderID(t, kb.ID)

	store.CreateImportTask("start-task", kb.ID, "alice")

	q := NewImportQueue(10)
	p := NewPipeline(q, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Start(ctx)

	q.Enqueue(&ImportTask{
		ID: "start-task", KbID: kb.ID, FolderID: rootID,
		FileName: "hello.md", Data: []byte("# Started\nvia queue"), Username: "alice",
	})

	time.Sleep(100 * time.Millisecond)

	docs, err := store.GetDocumentsByKB(kb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}
	if docs[0].Title != "Started" {
		t.Errorf("title = %q, want %q", docs[0].Title, "Started")
	}
}

func mockLLMServer(t *testing.T, responseContent string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": responseContent,
					},
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestClassifyAndExtract(t *testing.T) {
	srv := mockLLMServer(t, `{"sections":[{"title":"API Auth","content":"Auth content","suggested_path":"/技术/API"}],"keywords":["OAuth2","JWT"]}`)
	defer srv.Close()

	client := llm.NewClient(llm.Config{BaseURL: srv.URL, APIKey: "test", Model: "test"})
	result, err := ClassifyAndExtract(client, "test content")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sections) != 1 {
		t.Errorf("expected 1 section, got %d", len(result.Sections))
	}
	if result.Sections[0].Title != "API Auth" {
		t.Errorf("title = %q, want %q", result.Sections[0].Title, "API Auth")
	}
	if result.Sections[0].SuggestedPath != "/技术/API" {
		t.Errorf("path = %q, want %q", result.Sections[0].SuggestedPath, "/技术/API")
	}
	if len(result.Keywords) != 2 {
		t.Errorf("expected 2 keywords, got %d", len(result.Keywords))
	}
	if result.Keywords[0] != "OAuth2" {
		t.Errorf("keyword[0] = %q, want %q", result.Keywords[0], "OAuth2")
	}
}

func TestClassifyAndExtract_HandlesMarkdownCodeBlock(t *testing.T) {
	srv := mockLLMServer(t, "```json\n{\"sections\":[{\"title\":\"Intro\",\"content\":\"Hello\",\"suggested_path\":\"/\"}],\"keywords\":[]}\n```")
	defer srv.Close()

	client := llm.NewClient(llm.Config{BaseURL: srv.URL, APIKey: "test", Model: "test"})
	result, err := ClassifyAndExtract(client, "content")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sections) != 1 {
		t.Errorf("expected 1 section, got %d", len(result.Sections))
	}
}

func TestClassifyAndExtract_APIFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := llm.NewClient(llm.Config{BaseURL: srv.URL, APIKey: "test", Model: "test"})
	_, err := ClassifyAndExtract(client, "content")
	if err == nil {
		t.Error("expected error from API failure")
	}
}

func TestEnsureFolders_CreatesHierarchy(t *testing.T) {
	initTestDB(t)
	kb := createTestKB(t, "folder-test", "", "alice")

	folderID, err := EnsureFolders(kb.ID, "/技术/API")
	if err != nil {
		t.Fatalf("EnsureFolders: %v", err)
	}
	if folderID == 0 {
		t.Fatal("expected non-zero folder ID")
	}

	tree, err := store.GetFolderTree(kb.ID)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range tree {
		names = append(names, f.Name)
	}
	if !contains(names, "技术") || !contains(names, "API") {
		t.Errorf("expected '技术' and 'API' in folders, got %v", names)
	}
}

func TestEnsureFolders_Idempotent(t *testing.T) {
	initTestDB(t)
	kb := createTestKB(t, "idempotent-test", "", "alice")

	fid1, err := EnsureFolders(kb.ID, "/技术/API")
	if err != nil {
		t.Fatal(err)
	}

	fid2, err := EnsureFolders(kb.ID, "/技术/API")
	if err != nil {
		t.Fatal(err)
	}

	if fid1 != fid2 {
		t.Errorf("expected same folder ID on second call, got %d vs %d", fid1, fid2)
	}
}

func TestEnsureFolders_EmptyPathReturnsRoot(t *testing.T) {
	initTestDB(t)
	kb := createTestKB(t, "root-test", "", "alice")
	rootID := getRootFolderID(t, kb.ID)

	fid, err := EnsureFolders(kb.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if fid != rootID {
		t.Errorf("expected root ID %d, got %d", rootID, fid)
	}
}

func TestPipeline_AutoClassify(t *testing.T) {
	initTestDB(t)
	kb := createTestKB(t, "classify-pipeline", "", "alice")
	rootID := getRootFolderID(t, kb.ID)
	store.CreateImportTask("classify-task", kb.ID, "alice")

	srv := mockLLMServer(t, `{"sections":[{"title":"Section 1","content":"Section 1 content","suggested_path":"/技术/A"},{"title":"Section 2","content":"Section 2 content","suggested_path":"/技术/B"}],"keywords":["KW1","KW2"]}`)
	defer srv.Close()

	client := llm.NewClient(llm.Config{BaseURL: srv.URL, APIKey: "test", Model: "test"})
	q := NewImportQueue(10)
	p := NewPipeline(q, client, nil)

	task := &ImportTask{
		ID:           "classify-task",
		KbID:         kb.ID,
		FolderID:     rootID,
		FileName:     "test.md",
		Data:         []byte("# Doc\ncontent"),
		Username:     "alice",
		AutoClassify: true,
	}
	p.Process(task)

	docs, err := store.GetDocumentsByKB(kb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(docs))
	}
	if docs[0].Title != "Section 1" {
		t.Errorf("doc[0].Title = %q, want %q", docs[0].Title, "Section 1")
	}
	if docs[1].Title != "Section 2" {
		t.Errorf("doc[1].Title = %q, want %q", docs[1].Title, "Section 2")
	}
	if len(task.ExtraKeywords) != 2 {
		t.Errorf("expected 2 keywords, got %d", len(task.ExtraKeywords))
	}
}

func TestPipeline_AutoClassify_WithError(t *testing.T) {
	initTestDB(t)
	kb := createTestKB(t, "classify-error", "", "alice")
	rootID := getRootFolderID(t, kb.ID)
	store.CreateImportTask("classify-err-task", kb.ID, "alice")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := llm.NewClient(llm.Config{BaseURL: srv.URL, APIKey: "test", Model: "test"})
	q := NewImportQueue(10)
	p := NewPipeline(q, client, nil)

	task := &ImportTask{
		ID:           "classify-err-task",
		KbID:         kb.ID,
		FolderID:     rootID,
		FileName:     "test.md",
		Data:         []byte("# Doc\ncontent"),
		Username:     "alice",
		AutoClassify: true,
	}
	p.Process(task)

	if task.Status != "error" {
		t.Errorf("expected status 'error', got %q", task.Status)
	}
	if task.ErrorMsg == "" {
		t.Error("expected non-empty error message")
	}

	docs, err := store.GetDocumentsByKB(kb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 0 {
		t.Errorf("expected 0 documents on error, got %d", len(docs))
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}


