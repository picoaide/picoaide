package knowledge

import (
  "context"
  "crypto/sha256"
  "fmt"
  "io"
  "net/http"
  "time"

  "github.com/picoaide/picoaide/internal/store"
)

type SyncChecker struct {
  interval time.Duration
  stopCh   chan struct{}
}

func NewSyncChecker() *SyncChecker {
  return &SyncChecker{
    interval: 60 * time.Second,
    stopCh:   make(chan struct{}),
  }
}

func (s *SyncChecker) Start(ctx context.Context) {
  ticker := time.NewTicker(s.interval)
  defer ticker.Stop()
  for {
    select {
    case <-ctx.Done():
      return
    case <-s.stopCh:
      return
    case <-ticker.C:
      s.runOnce()
    }
  }
}

func (s *SyncChecker) Stop() {
  close(s.stopCh)
}

func (s *SyncChecker) runOnce() {
  sources, err := store.GetDueSyncSources()
  if err != nil {
    return
  }
  for _, src := range sources {
    s.syncSource(src)
  }
}

func (s *SyncChecker) syncSource(src store.KBSyncSource) {
  ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
  defer cancel()

  req, err := http.NewRequestWithContext(ctx, "GET", src.URL, nil)
  if err != nil {
    return
  }
  req.Header.Set("User-Agent", "PicoAide-KB-Sync/1.0")

  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    return
  }
  defer resp.Body.Close()

  body, err := io.ReadAll(resp.Body)
  if err != nil {
    return
  }

  newChecksum := fmt.Sprintf("%x", sha256.Sum256(body))
  if newChecksum == src.Checksum {
    // ponytail: no change, just update last_sync_at
    store.UpdateSyncSourceResult(src.ID, newChecksum)
    return
  }

  store.UpdateDocumentsByURL(src.KbID, src.URL, string(body), newChecksum)
  store.UpdateSyncSourceResult(src.ID, newChecksum)
}
