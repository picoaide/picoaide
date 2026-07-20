package knowledge

import (
	"sync"
	"testing"
	"time"

	"github.com/picoaide/picoaide/internal/store"
)

func TestExtractWikiLinks(t *testing.T) {
	links := ExtractWikiLinks("参考 [[OAuth2]] 和 [[JWT]]")
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
	if links[0] != "OAuth2" {
		t.Errorf("links[0] = %q, want %q", links[0], "OAuth2")
	}
	if links[1] != "JWT" {
		t.Errorf("links[1] = %q, want %q", links[1], "JWT")
	}
}

func TestExtractWikiLinks_NoMatches(t *testing.T) {
	links := ExtractWikiLinks("plain text without brackets")
	if len(links) != 0 {
		t.Errorf("expected 0 links, got %d", len(links))
	}
}

func TestExtractWikiLinks_Dedup(t *testing.T) {
	links := ExtractWikiLinks("[[OAuth2]] 和 [[OAuth2]]")
	if len(links) != 1 {
		t.Errorf("expected 1 link, got %d", len(links))
	}
}

func TestExtractTags(t *testing.T) {
	tags := ExtractTags("#api #安全")
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
	if tags[0] != "api" {
		t.Errorf("tags[0] = %q, want %q", tags[0], "api")
	}
	if tags[1] != "安全" {
		t.Errorf("tags[1] = %q, want %q", tags[1], "安全")
	}
}

func TestExtractTags_ExcludesHeadings(t *testing.T) {
	tags := ExtractTags("# Title\n #tag")
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	if tags[0] != "tag" {
		t.Errorf("tag = %q, want %q", tags[0], "tag")
	}
}

func TestExtractTags_Dedup(t *testing.T) {
	tags := ExtractTags("#api text #api more")
	if len(tags) != 1 {
		t.Errorf("expected 1 tag, got %d", len(tags))
	}
}

func TestLinker_Rebuild(t *testing.T) {
	initTestDB(t)
	kb := createTestKB(t, "rebuild-test", "", "admin")
	rootID := getRootFolderID(t, kb.ID)

	store.CreateDocument(kb.ID, rootID, "OAuth2 Guide", "参考 [[JWT Guide]]", "manual", "md", "admin")
	store.CreateDocument(kb.ID, rootID, "JWT Guide", "用于 [[OAuth2 Guide]]", "manual", "md", "admin")

	linker := NewLinker()
	links, tags, err := linker.Rebuild(kb.ID)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
	if len(tags) != 0 {
		t.Errorf("expected 0 tags, got %d", len(tags))
	}
}

func TestLinker_RebuildAndStore(t *testing.T) {
	initTestDB(t)
	kb := createTestKB(t, "rebuild-store-test", "", "admin")
	rootID := getRootFolderID(t, kb.ID)

	d1, _ := store.CreateDocument(kb.ID, rootID, "Doc A", "参考 [[Doc B]]", "manual", "md", "admin")
	store.CreateDocument(kb.ID, rootID, "Doc B", "参考 [[Doc A]]", "manual", "md", "admin")

	linker := NewLinker()
	if err := linker.RebuildAndStore(kb.ID); err != nil {
		t.Fatalf("RebuildAndStore: %v", err)
	}

	links, _ := store.GetDocumentLinks(d1.ID)
	if len(links) != 1 {
		t.Errorf("expected 1 link from Doc A, got %d", len(links))
	}
}

func TestLinker_MatchKeywords(t *testing.T) {
	initTestDB(t)
	kb := createTestKB(t, "keyword-test", "", "admin")
	rootID := getRootFolderID(t, kb.ID)

	store.CreateDocument(kb.ID, rootID, "OAuth2 Guide", "content", "manual", "md", "admin")
	store.CreateDocument(kb.ID, rootID, "JWT Guide", "content", "manual", "md", "admin")

	linker := NewLinker()
	links, err := linker.MatchKeywords(kb.ID, []string{"OAuth2 Guide", "Unknown"})
	if err != nil {
		t.Fatalf("MatchKeywords: %v", err)
	}

	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].Keyword != "OAuth2 Guide" {
		t.Errorf("keyword = %q, want %q", links[0].Keyword, "OAuth2 Guide")
	}
}

func TestRebuildDebouncer_TriggersOnce(t *testing.T) {
	var mu sync.Mutex
	var count int
	d := &rebuildDebouncer{timers: make(map[int64]*time.Timer)}
	d.Trigger(1, func(kbID int64) {
		mu.Lock()
		count++
		mu.Unlock()
	})
	d.Trigger(1, func(kbID int64) {
		mu.Lock()
		count++
		mu.Unlock()
	})
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	if count != 1 {
		t.Errorf("expected 1 callback, got %d", count)
	}
	mu.Unlock()
}
