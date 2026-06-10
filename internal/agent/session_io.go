package agent

import (
  "bufio"
  "encoding/json"
  "fmt"
  "os"
  "path/filepath"
  "strings"
  "time"
)

// ============================================================
// 双层会话存储
// archive.jsonl — 永久完整记录（只追加，不压缩，永不删除）
// live.jsonl    — LLM 视角（压缩后含摘要消息）
// live.meta.json — 元数据（摘要文本、token计数）
// ============================================================

type SessionStore struct {
  workspace string
}

func NewSessionStore(workspace string) *SessionStore {
  return &SessionStore{workspace: workspace}
}

func (s *SessionStore) sessionDir(key string) string {
  return filepath.Join(s.workspace, "sessions", SanitizeKey(key))
}

func (s *SessionStore) archivePath(key string, date string) string {
  return filepath.Join(s.sessionDir(key), fmt.Sprintf("archive-%s.jsonl", date))
}

func (s *SessionStore) livePath(key string) string {
  return filepath.Join(s.sessionDir(key), "live.jsonl")
}

func (s *SessionStore) metaPath(key string) string {
  return filepath.Join(s.sessionDir(key), "live.meta.json")
}

// todayArchive 返回今天的归档路径
func (s *SessionStore) todayArchive(key string) string {
  return s.archivePath(key, time.Now().UTC().Format("2006-01-02"))
}

func (s *SessionStore) EnsureDir(key string) error {
  return os.MkdirAll(s.sessionDir(key), 0755)
}

// AppendMessage 追加到 archive（永久）和 live（LLM视角）
func (s *SessionStore) AppendMessage(key string, msg *Message) error {
  if err := s.EnsureDir(key); err != nil {
    return err
  }
  data, err := json.Marshal(msg)
  if err != nil {
    return err
  }
  line := append(data, '\n')

  // archive: 永久保存（按日切割）
  if err := appendFile(s.todayArchive(key), line); err != nil {
    return fmt.Errorf("写入 archive 失败: %w", err)
  }

  // live: LLM 视角
  if err := appendFile(s.livePath(key), line); err != nil {
    return fmt.Errorf("写入 live 失败: %w", err)
  }
  return nil
}

// LoadLive 加载 LLM 视角的会话（已压缩）
func (s *SessionStore) LoadLive(key string) ([]*Message, error) {
  return readJSONL(s.livePath(key))
}

// LoadArchive 加载归档消息。date 为空时加载今天，否则加载指定日期
func (s *SessionStore) LoadArchive(key string, date ...string) ([]*Message, error) {
  path := s.todayArchive(key)
  if len(date) > 0 && date[0] != "" {
    path = s.archivePath(key, date[0])
  }
  msgs, err := readJSONL(path)
  if err != nil {
    if os.IsNotExist(err) && len(date) == 0 {
      // 兼容旧版单文件 archive.jsonl
      oldPath := filepath.Join(s.sessionDir(key), "archive.jsonl")
      return readJSONL(oldPath)
    }
    return nil, err
  }
  return msgs, nil
}

// ListArchives 列出所有归档日期（按文件名中的日期排序）
func (s *SessionStore) ListArchives(key string) ([]string, error) {
  dir := s.sessionDir(key)
  entries, err := os.ReadDir(dir)
  if err != nil {
    if os.IsNotExist(err) {
      return nil, nil
    }
    return nil, err
  }
  var dates []string
  for _, e := range entries {
    name := e.Name()
    if strings.HasPrefix(name, "archive-") && strings.HasSuffix(name, ".jsonl") {
      dates = append(dates, name[8:18])
    }
  }
  // 也检查旧版单文件
  if _, err := os.Stat(filepath.Join(dir, "archive.jsonl")); err == nil {
    dates = append(dates, "legacy")
  }
  return dates, nil
}

// ArchiveSize 返回归档消息数（所有日期合计）
func (s *SessionStore) ArchiveSize(key string) int {
  dates, err := s.ListArchives(key)
  if err != nil {
    return 0
  }
  total := 0
  for _, d := range dates {
    p := s.archivePath(key, d)
    if d == "legacy" {
      p = filepath.Join(s.sessionDir(key), "archive.jsonl")
    }
    f, err := os.Open(p)
    if err != nil {
      continue
    }
    defer f.Close()
    scanner := bufio.NewScanner(f)
    scanner.Buffer(make([]byte, 512*1024), 512*1024)
    for scanner.Scan() {
      if len(scanner.Bytes()) > 0 {
        total++
      }
    }
  }
  return total
}

// ReplaceLive 替换 live 内容（压缩后调用）
func (s *SessionStore) ReplaceLive(key string, msgs []*Message) error {
  if err := s.EnsureDir(key); err != nil {
    return err
  }
  f, err := os.Create(s.livePath(key))
  if err != nil {
    return fmt.Errorf("创建 live 文件失败: %w", err)
  }
  defer f.Close()
  for _, msg := range msgs {
    data, _ := json.Marshal(msg)
    if _, err := f.Write(append(data, '\n')); err != nil {
      return err
    }
  }
  return nil
}

// LoadMeta 读取元数据
func (s *SessionStore) LoadMeta(key string) (*SessionMeta, error) {
  f, err := os.Open(s.metaPath(key))
  if err != nil {
    if os.IsNotExist(err) {
      return nil, nil
    }
    return nil, err
  }
  defer f.Close()
  var meta SessionMeta
  if err := json.NewDecoder(f).Decode(&meta); err != nil {
    return nil, err
  }
  return &meta, nil
}

// SaveMeta 保存元数据
func (s *SessionStore) SaveMeta(key string, meta *SessionMeta) error {
  if err := s.EnsureDir(key); err != nil {
    return err
  }
  meta.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
  meta.Key = key
  data, _ := json.MarshalIndent(meta, "", "  ")
  return os.WriteFile(s.metaPath(key), data, 0644)
}

// ============================================================
// 内部辅助
// ============================================================

func appendFile(path string, data []byte) (err error) {
  f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
  if err != nil {
    return err
  }
  defer func() {
    if cerr := f.Close(); cerr != nil && err == nil {
      err = cerr
    }
  }()
  _, err = f.Write(data)
  return
}

func readJSONL(path string) ([]*Message, error) {
  f, err := os.Open(path)
  if err != nil {
    if os.IsNotExist(err) {
      return nil, nil
    }
    return nil, err
  }
  defer f.Close()
  var msgs []*Message
  scanner := bufio.NewScanner(f)
  scanner.Buffer(make([]byte, 512*1024), 512*1024)
  for scanner.Scan() {
    line := scanner.Bytes()
    if len(line) == 0 {
      continue
    }
    var msg Message
    if err := json.Unmarshal(line, &msg); err != nil {
      return nil, fmt.Errorf("解析消息失败: %w", err)
    }
    msgs = append(msgs, &msg)
  }
  return msgs, scanner.Err()
}
