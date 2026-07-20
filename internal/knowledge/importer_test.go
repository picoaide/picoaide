package knowledge

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

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

	var rebuildCount int64
	linker := &Linker{
		onRebuild: func(kbID int64) {
			atomic.AddInt64(&rebuildCount, 1)
		},
	}

	q := NewImportQueue(10)
	p := NewPipeline(q, nil, linker)

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

	if n := atomic.LoadInt64(&rebuildCount); n != 1 {
		t.Errorf("expected 1 rebuild, got %d", n)
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
