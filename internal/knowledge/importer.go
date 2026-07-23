package knowledge

import (
  "context"
  "fmt"
  "os"
  "sync"
  "time"

  "github.com/picoaide/picoaide/internal/llm"
  "github.com/picoaide/picoaide/internal/store"
)

type ImportTask struct {
  ID            string
  KbID          int64
  FolderID      int64
  Username      string
  FilePath      string
  FileName      string
  Data          []byte
  URL           string
  AutoClassify  bool
  ExtraKeywords []string
  Status        string
  ErrorMsg      string
}

var GlobalImportQueue = NewImportQueue(100)

type ImportQueue struct {
  ch chan *ImportTask
}

func NewImportQueue(buffer int) *ImportQueue {
  return &ImportQueue{
    ch: make(chan *ImportTask, buffer),
  }
}

func (q *ImportQueue) Enqueue(task *ImportTask) error {
  select {
  case q.ch <- task:
    return nil
  default:
    return fmt.Errorf("queue full")
  }
}

func (q *ImportQueue) Tasks() <-chan *ImportTask {
  return q.ch
}

type rebuildDebouncer struct {
  mu     sync.Mutex
  timers map[int64]*time.Timer
}

func (d *rebuildDebouncer) Trigger(kbID int64, fn func(int64)) {
  d.mu.Lock()
  defer d.mu.Unlock()
  if t, ok := d.timers[kbID]; ok {
    t.Stop()
  }
  d.timers[kbID] = time.AfterFunc(200*time.Millisecond, func() {
    fn(kbID)
    d.mu.Lock()
    delete(d.timers, kbID)
    d.mu.Unlock()
  })
}

type Pipeline struct {
  queue     *ImportQueue
  llm       *llm.Client
  linker    *Linker
  debouncer *rebuildDebouncer
}

func NewPipeline(queue *ImportQueue, llmClient *llm.Client, linker *Linker) *Pipeline {
  return &Pipeline{
    queue:     queue,
    llm:       llmClient,
    linker:    linker,
    debouncer: &rebuildDebouncer{timers: make(map[int64]*time.Timer)},
  }
}

func (p *Pipeline) SetLinker(l *Linker) { p.linker = l }

func (p *Pipeline) Start(ctx context.Context) {
  for {
    select {
    case <-ctx.Done():
      return
    case task := <-p.queue.Tasks():
      p.Process(task)
    }
  }
}

func (p *Pipeline) Process(task *ImportTask) {
  store.UpdateImportTaskStatus(task.ID, "parsing", 10)

  result, err := Parse(task.FileName, task.Data)
  if err != nil {
    store.UpdateImportTaskStatus(task.ID, "error", 0)
    store.UpdateImportTaskError(task.ID, err.Error())
    task.Status = "error"
    task.ErrorMsg = err.Error()
    return
  }

  store.UpdateImportTaskStatus(task.ID, "indexing", 50)

  if p.llm != nil && task.AutoClassify {
    classifyResult, classifyErr := ClassifyAndExtract(p.llm, result.Content)
    if classifyErr != nil {
      task.Status = "error"
      task.ErrorMsg = classifyErr.Error()
      store.UpdateImportTaskStatus(task.ID, "error", 0)
      store.UpdateImportTaskError(task.ID, classifyErr.Error())
      return
    }

    for _, section := range classifyResult.Sections {
      folderID := task.FolderID
      if section.SuggestedPath != "" {
        fid, err := EnsureFolders(task.KbID, section.SuggestedPath)
        if err == nil {
          folderID = fid
        }
      }
      content := section.Content
      if content == "" {
        content = section.Title
      }
      store.CreateDocument(task.KbID, folderID, section.Title, content, "upload", result.FileType, task.Username)
    }

    task.ExtraKeywords = classifyResult.Keywords
  } else {
    store.CreateDocument(task.KbID, task.FolderID, result.Title, result.Content, "upload", result.FileType, task.Username)
  }

  p.debouncer.Trigger(task.KbID, func(kbID int64) {
    if p.linker != nil {
      p.linker.RebuildAndStore(kbID)
    }
  })

  store.UpdateImportTaskStatus(task.ID, "ready", 100)
  task.Status = "ready"

  if task.FilePath != "" {
    os.Remove(task.FilePath)
  }
}
