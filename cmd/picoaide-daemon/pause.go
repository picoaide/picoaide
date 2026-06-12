package main

import (
  "crypto/sha256"
  "encoding/hex"
  "encoding/json"
  "io/fs"
  "os"
  "path/filepath"
  "strings"
  "time"

  "log/slog"

  "github.com/picoaide/picoaide/internal/daemon/store"
)

// takeFileSnapshot 在任务执行前生成文件快照（SHA256），用于暂停恢复时对比变更
func takeFileSnapshot(taskDir, workspace string) {
  snapDir := filepath.Join(taskDir, "snapshots")
  os.MkdirAll(snapDir, 0755)

  snapType := "before_task"
  files := scanWorkspaceFiles(workspace)

  var entries []store.FileEntry
  for _, f := range files {
    relPath := strings.TrimPrefix(strings.TrimPrefix(f, workspace), "/")
    hash := fileSHA256(f)
    info, err := os.Stat(f)
    if err != nil {
      continue
    }
    entries = append(entries, store.FileEntry{
      Path:    relPath,
      SHA256:  hash,
      Size:    info.Size(),
      ModTime: info.ModTime().UTC().Format(time.RFC3339),
    })
  }

  ss := store.NewSnapshotStore(snapDir)
  data, _ := json.MarshalIndent(entries, "", "  ")
  os.WriteFile(filepath.Join(snapDir, "files_"+snapType+".json"), data, 0600)
  _ = ss

  slog.Debug("taskqueue.file_snapshot_taken",
    "task_dir", taskDir,
    "file_count", len(entries),
  )
}

// scanWorkspaceFiles 扫描工作目录下的所有文件，跳过 .git 和 node_modules
func scanWorkspaceFiles(workspace string) []string {
  var files []string
  filepath.WalkDir(workspace, func(path string, d fs.DirEntry, err error) error {
    if err != nil {
      return nil
    }
    name := d.Name()
    if d.IsDir() {
      if name == ".git" || name == "node_modules" || name == "__pycache__" || name == "daemon" || name == "sessions" {
        return filepath.SkipDir
      }
      return nil
    }
    if strings.HasPrefix(name, ".") {
      return nil
    }
    files = append(files, path)
    return nil
  })
  return files
}

func fileSHA256(path string) string {
  data, err := os.ReadFile(path)
  if err != nil {
    return ""
  }
  sum := sha256.Sum256(data)
  return hex.EncodeToString(sum[:])
}

// savePauseSnapshot 保存完整的暂停快照（引擎状态 + 文件状态）
func savePauseSnapshot(taskDir string, engineSnap json.RawMessage) error {
  os.MkdirAll(taskDir, 0755)

  snapPath := filepath.Join(taskDir, "engine_snapshot.json")
  if err := os.WriteFile(snapPath, engineSnap, 0600); err != nil {
    return err
  }

  slog.Debug("taskqueue.pause_snapshot_saved", "task_dir", taskDir)
  return nil
}

// loadPauseSnapshot 加载暂停快照
func loadPauseSnapshot(taskDir string) (json.RawMessage, error) {
  snapPath := filepath.Join(taskDir, "engine_snapshot.json")
  data, err := os.ReadFile(snapPath)
  if err != nil {
    return nil, err
  }
  return data, nil
}
