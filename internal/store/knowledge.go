package store

import (
  "fmt"
)

type KnowledgeBase struct {
  ID          int64  `xorm:"pk autoincr 'id'" json:"id"`
  Name        string `xorm:"notnull 'name'" json:"name"`
  Description string `xorm:"default '' 'description'" json:"description"`
  CreatedBy   string `xorm:"notnull 'created_by'" json:"created_by"`
  CreatedAt   string `xorm:"created 'created_at'" json:"created_at"`
  UpdatedAt   string `xorm:"updated 'updated_at'" json:"updated_at"`
}

func (KnowledgeBase) TableName() string { return "knowledge_bases" }

type KBFolder struct {
  ID             int64  `xorm:"pk autoincr 'id'" json:"id"`
  KbID           int64  `xorm:"notnull 'kb_id'" json:"kb_id"`
  ParentID       *int64 `xorm:"'parent_id'" json:"parent_id"`
  Name           string `xorm:"notnull 'name'" json:"name"`
  PermissionsSet int    `xorm:"default 0 'permissions_set'" json:"permissions_set"`
  CreatedAt      string `xorm:"created 'created_at'" json:"created_at"`
  UpdatedAt      string `xorm:"updated 'updated_at'" json:"updated_at"`
}

func (KBFolder) TableName() string { return "kb_folders" }

type KBFolderUser struct {
  ID       int64  `xorm:"pk autoincr 'id'"`
  FolderID int64  `xorm:"notnull 'folder_id'"`
  Username string `xorm:"notnull 'username'"`
}

func (KBFolderUser) TableName() string { return "kb_folder_users" }

type KBFolderGroup struct {
  ID       int64 `xorm:"pk autoincr 'id'"`
  FolderID int64 `xorm:"notnull 'folder_id'"`
  GroupID  int64 `xorm:"notnull 'group_id'"`
}

func (KBFolderGroup) TableName() string { return "kb_folder_groups" }

type KBDocument struct {
  ID         int64  `xorm:"pk autoincr 'id'" json:"id"`
  KbID       int64  `xorm:"notnull 'kb_id'" json:"kb_id"`
  FolderID   int64  `xorm:"notnull 'folder_id'" json:"folder_id"`
  Title      string `xorm:"notnull 'title'" json:"title"`
  Content    string `xorm:"default '' 'content'" json:"content"`
  URL        string `xorm:"default '' 'url'" json:"url"`
  SourceID   string `xorm:"default '' 'source_id'" json:"source_id"`
  SourceType string `xorm:"notnull default 'manual' 'source_type'" json:"source_type"`
  FileType   string `xorm:"default 'md' 'file_type'" json:"file_type"`
  FileSize   int    `xorm:"default 0 'file_size'" json:"file_size"`
  Status     string `xorm:"notnull default 'pending' 'status'" json:"status"`
  ErrorMsg   string `xorm:"default '' 'error_msg'" json:"error_msg"`
  Checksum   string `xorm:"default '' 'checksum'" json:"checksum"`
  CreatedBy  string `xorm:"notnull 'created_by'" json:"created_by"`
  CreatedAt  string `xorm:"created 'created_at'" json:"created_at"`
  UpdatedAt  string `xorm:"updated 'updated_at'" json:"updated_at"`
}

func (KBDocument) TableName() string { return "kb_documents" }

type KBLink struct {
  ID        int64  `xorm:"pk autoincr 'id'"`
  SourceDoc int64  `xorm:"notnull 'source_doc'"`
  TargetDoc int64  `xorm:"notnull 'target_doc'"`
  Keyword   string `xorm:"notnull 'keyword'"`
  CreatedAt string `xorm:"created 'created_at'"`
}

func (KBLink) TableName() string { return "kb_links" }

type KBTag struct {
  ID    int64  `xorm:"pk autoincr 'id'"`
  DocID int64  `xorm:"notnull 'doc_id'"`
  Tag   string `xorm:"notnull 'tag'"`
}

func (KBTag) TableName() string { return "kb_tags" }

type KBImportTask struct {
  ID        string `xorm:"pk 'id'" json:"id"`
  KbID      int64  `xorm:"notnull 'kb_id'" json:"kb_id"`
  Username  string `xorm:"notnull 'username'" json:"username"`
  Status    string `xorm:"default 'pending' 'status'" json:"status"`
  Progress  int    `xorm:"default 0 'progress'" json:"progress"`
  FileCount int    `xorm:"default 0 'file_count'" json:"file_count"`
  ErrorMsg  string `xorm:"default '' 'error_msg'" json:"error_msg"`
  CreatedAt string `xorm:"created 'created_at'" json:"created_at"`
  UpdatedAt string `xorm:"updated 'updated_at'" json:"updated_at"`
}

func (KBImportTask) TableName() string { return "kb_import_tasks" }

type KBAuditLog struct {
  ID        int64  `xorm:"pk autoincr 'id'"`
  Username  string `xorm:"notnull 'username'"`
  Action    string `xorm:"notnull 'action'"`
  Detail    string `xorm:"default '' 'detail'"`
  Source    string `xorm:"default 'web' 'source'"`
  CreatedAt string `xorm:"created 'created_at'"`
}

func (KBAuditLog) TableName() string { return "kb_audit_log" }

type SearchResult struct {
  DocID   int64  `xorm:"id" json:"doc_id"`
  Title   string `xorm:"title" json:"title"`
  KbID    int64  `xorm:"kb_id" json:"kb_id"`
  Snippet string `xorm:"snippet" json:"snippet"`
}

func CreateKnowledgeBase(name, desc, createdBy string) (*KnowledgeBase, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  session := engine.NewSession()
  defer session.Close()
  if err := session.Begin(); err != nil {
    return nil, err
  }
  kb := &KnowledgeBase{Name: name, Description: desc, CreatedBy: createdBy}
  if _, err := session.Insert(kb); err != nil {
    _ = session.Rollback()
    return nil, fmt.Errorf("create knowledge base: %w", err)
  }
  folder := &KBFolder{KbID: kb.ID, Name: "/"}
  if _, err := session.Insert(folder); err != nil {
    _ = session.Rollback()
    return nil, fmt.Errorf("create root folder: %w", err)
  }
  if _, err := session.Insert(&KBFolderUser{FolderID: folder.ID, Username: createdBy}); err != nil {
    _ = session.Rollback()
    return nil, fmt.Errorf("grant creator access: %w", err)
  }
  if err := session.Commit(); err != nil {
    return nil, err
  }
  return kb, nil
}

func ListKnowledgeBases() ([]KnowledgeBase, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var kbs []KnowledgeBase
  if err := engine.OrderBy("name").Find(&kbs); err != nil {
    return nil, err
  }
  return kbs, nil
}

func GetKnowledgeBase(id int64) (*KnowledgeBase, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var kb KnowledgeBase
  has, err := engine.Where("id = ?", id).Get(&kb)
  if err != nil {
    return nil, err
  }
  if !has {
    return nil, fmt.Errorf("knowledge base %d not found", id)
  }
  return &kb, nil
}

func UpdateKnowledgeBase(id int64, name, desc string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  affected, err := engine.Where("id = ?", id).Cols("name", "description").Update(&KnowledgeBase{Name: name, Description: desc})
  if err != nil {
    return err
  }
  if affected == 0 {
    return fmt.Errorf("knowledge base %d not found", id)
  }
  return nil
}

func DeleteKnowledgeBase(id int64) error {
  if err := ensureDB(); err != nil {
    return err
  }
  affected, err := engine.Where("id = ?", id).Delete(&KnowledgeBase{})
  if err != nil {
    return err
  }
  if affected == 0 {
    return fmt.Errorf("knowledge base %d not found", id)
  }
  return nil
}

func CreateFolder(kbID int64, parentID *int64, name string) (*KBFolder, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  f := &KBFolder{KbID: kbID, ParentID: parentID, Name: name}
  if _, err := engine.Insert(f); err != nil {
    return nil, fmt.Errorf("create folder: %w", err)
  }
  return f, nil
}

func GetFolderTree(kbID int64) ([]KBFolder, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var folders []KBFolder
  if err := engine.Where("kb_id = ?", kbID).OrderBy("name").Find(&folders); err != nil {
    return nil, err
  }
  return folders, nil
}

func UpdateFolder(id int64, name string, parentID *int64) error {
  if err := ensureDB(); err != nil {
    return err
  }
  affected, err := engine.Where("id = ?", id).Cols("name", "parent_id").Update(&KBFolder{Name: name, ParentID: parentID})
  if err != nil {
    return err
  }
  if affected == 0 {
    return fmt.Errorf("folder %d not found", id)
  }
  return nil
}

func DeleteFolder(id int64) error {
  if err := ensureDB(); err != nil {
    return err
  }
  affected, err := engine.Where("id = ?", id).Delete(&KBFolder{})
  if err != nil {
    return err
  }
  if affected == 0 {
    return fmt.Errorf("folder %d not found", id)
  }
  return nil
}

func AddFolderUser(folderID int64, username string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Insert(&KBFolderUser{FolderID: folderID, Username: username})
  return err
}

func RemoveFolderUser(folderID int64, username string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Where("folder_id = ? AND username = ?", folderID, username).Delete(&KBFolderUser{})
  return err
}

func AddFolderGroup(folderID int64, groupID int64) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Insert(&KBFolderGroup{FolderID: folderID, GroupID: groupID})
  return err
}

func RemoveFolderGroup(folderID int64, groupID int64) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Where("folder_id = ? AND group_id = ?", folderID, groupID).Delete(&KBFolderGroup{})
  return err
}

func GetFolderPermissions(folderID int64) (users []string, groups []int64, err error) {
  if err := ensureDB(); err != nil {
    return nil, nil, err
  }
  var us []KBFolderUser
  if err := engine.Where("folder_id = ?", folderID).Find(&us); err != nil {
    return nil, nil, err
  }
  users = make([]string, 0, len(us))
  for _, u := range us {
    users = append(users, u.Username)
  }
  var gs []KBFolderGroup
  if err := engine.Where("folder_id = ?", folderID).Find(&gs); err != nil {
    return nil, nil, err
  }
  groups = make([]int64, 0, len(gs))
  for _, g := range gs {
    groups = append(groups, g.GroupID)
  }
  return users, groups, nil
}

func SetFolderPermissionsSet(folderID int64, val int) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Where("id = ?", folderID).Cols("permissions_set").Update(&KBFolder{PermissionsSet: val})
  return err
}

// GetAccessibleFolderIDs returns folder IDs the user can access via user grants
// or group membership, with inheritance: a folder without explicit permissions
// inherits from its nearest ancestor that has explicit permissions.
func GetAccessibleFolderIDs(username string) ([]int64, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  // Find all groups the user belongs to
  var userGroups []UserGroup
  if err := engine.Where("username = ?", username).Find(&userGroups); err != nil {
    return nil, err
  }
  groupIDs := make([]int64, 0, len(userGroups))
  for _, ug := range userGroups {
    groupIDs = append(groupIDs, ug.GroupID)
  }

  // Folders with explicit user permission
  var directUser []KBFolderUser
  cond := engine.Where("username = ?", username)
  if err := cond.Find(&directUser); err != nil {
    return nil, err
  }
  userFolderIDs := make(map[int64]bool)
  for _, fu := range directUser {
    userFolderIDs[fu.FolderID] = true
  }

  // Folders with explicit group permission
  var directGroup []KBFolderGroup
  if len(groupIDs) > 0 {
    ids := make([]interface{}, len(groupIDs))
    for i, gid := range groupIDs {
      ids[i] = gid
    }
    if err := engine.In("group_id", ids...).Find(&directGroup); err != nil {
      return nil, err
    }
  }
  for _, fg := range directGroup {
    userFolderIDs[fg.FolderID] = true
  }

  // For each explicitly granted folder, include folders that inherit from it
  // (descendants with permissions_set = 0)
  accessible := make(map[int64]bool)
  for fid := range userFolderIDs {
    accessible[fid] = true
  }
  for fid := range userFolderIDs {
    if err := addInheritedFolders(fid, accessible); err != nil {
      return nil, err
    }
  }

  result := make([]int64, 0, len(accessible))
  for fid := range accessible {
    result = append(result, fid)
  }
  return result, nil
}

func addInheritedFolders(parentID int64, acc map[int64]bool) error {
  var children []KBFolder
  if err := engine.Where("parent_id = ? AND permissions_set = 0", parentID).Find(&children); err != nil {
    return err
  }
  for _, c := range children {
    if !acc[c.ID] {
      acc[c.ID] = true
      if err := addInheritedFolders(c.ID, acc); err != nil {
        return err
      }
    }
  }
  return nil
}

func CreateDocument(kbID, folderID int64, title, content, sourceType, fileType, createdBy string) (*KBDocument, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  doc := &KBDocument{
    KbID: kbID, FolderID: folderID, Title: title, Content: content,
    SourceType: sourceType, FileType: fileType, CreatedBy: createdBy,
  }
  if _, err := engine.Insert(doc); err != nil {
    return nil, fmt.Errorf("create document: %w", err)
  }
  return doc, nil
}

func GetDocumentsByKB(kbID int64) ([]KBDocument, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var docs []KBDocument
  if err := engine.Where("kb_id = ?", kbID).OrderBy("title").Find(&docs); err != nil {
    return nil, err
  }
  return docs, nil
}

// GetDocumentByID returns a document only if the user has permission to access it.
func GetDocumentByID(username string, docID int64) (*KBDocument, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var doc KBDocument
  has, err := engine.Where("id = ?", docID).Get(&doc)
  if err != nil {
    return nil, err
  }
  if !has {
    return nil, fmt.Errorf("document %d not found", docID)
  }
  accessible, err := GetAccessibleFolderIDs(username)
  if err != nil {
    return nil, err
  }
  ok := false
  for _, fid := range accessible {
    if fid == doc.FolderID {
      ok = true
      break
    }
  }
  if !ok {
    return nil, fmt.Errorf("document %d: permission denied", docID)
  }
  return &doc, nil
}

// BrowseFolder returns sub-folders and documents for a folder, checking permission.
func BrowseFolder(username string, folderID int64) (folders []KBFolder, docs []KBDocument, err error) {
  if err := ensureDB(); err != nil {
    return nil, nil, err
  }
  accessible, err := GetAccessibleFolderIDs(username)
  if err != nil {
    return nil, nil, err
  }
  ok := false
  for _, fid := range accessible {
    if fid == folderID {
      ok = true
      break
    }
  }
  if !ok {
    return nil, nil, fmt.Errorf("folder %d: permission denied", folderID)
  }
  var fs []KBFolder
  if err := engine.Where("parent_id = ?", folderID).OrderBy("name").Find(&fs); err != nil {
    return nil, nil, err
  }
  var ds []KBDocument
  if err := engine.Where("folder_id = ?", folderID).OrderBy("title").Find(&ds); err != nil {
    return nil, nil, err
  }
  return fs, ds, nil
}

// SearchKB performs a full-text search across documents the user can access.
func SearchKB(username, query string, page, pageSize int) ([]SearchResult, int64, error) {
  if err := ensureDB(); err != nil {
    return nil, 0, err
  }
  if query == "" {
    return nil, 0, nil
  }
  if page < 1 {
    page = 1
  }
  if pageSize < 1 {
    pageSize = 20
  }
  accessible, err := GetAccessibleFolderIDs(username)
  if err != nil {
    return nil, 0, err
  }
  if len(accessible) == 0 {
    return nil, 0, nil
  }

  offset := (page - 1) * pageSize
  // Build WHERE clause with accessible folder IDs
  var results []SearchResult
  q := fmt.Sprintf(`SELECT d.id, d.title, d.kb_id, snippet(kb_documents_fts, 1, '<mark>', '</mark>', '...', 32) AS snippet
    FROM kb_documents_fts f
    JOIN kb_documents d ON f.rowid = d.id
    WHERE kb_documents_fts MATCH ?
    AND d.folder_id IN (%s)
    ORDER BY rank
    LIMIT ? OFFSET ?`, placeholders(len(accessible)))

  args := make([]interface{}, 0, len(accessible)+3)
  args = append(args, query)
  for _, fid := range accessible {
    args = append(args, fid)
  }
  args = append(args, pageSize, offset)

  if err := engine.SQL(q, args...).Find(&results); err != nil {
    return nil, 0, err
  }

  // Count total
  countQ := fmt.Sprintf(`SELECT COUNT(*) AS cnt
    FROM kb_documents_fts f
    JOIN kb_documents d ON f.rowid = d.id
    WHERE kb_documents_fts MATCH ?
    AND d.folder_id IN (%s)`, placeholders(len(accessible)))
  countArgs := make([]interface{}, 0, len(accessible)+1)
  countArgs = append(countArgs, query)
  for _, fid := range accessible {
    countArgs = append(countArgs, fid)
  }
  var cr struct {
    Cnt int64 `xorm:"cnt"`
  }
  if _, err := engine.SQL(countQ, countArgs...).Get(&cr); err != nil {
    return nil, 0, err
  }
  total := cr.Cnt

  return results, total, nil
}

// ponytail: O(n) placeholders per search call, fine for typical folder counts
func placeholders(n int) string {
  if n == 0 {
    return "NULL"
  }
  b := make([]byte, 0, n*2-1)
  for i := 0; i < n; i++ {
    if i > 0 {
      b = append(b, ',')
    }
    b = append(b, '?')
  }
  return string(b)
}

func RebuildLinksAndTags(kbID int64, links []KBLink, tags []KBTag) error {
  if err := ensureDB(); err != nil {
    return err
  }
  session := engine.NewSession()
  defer session.Close()
  if err := session.Begin(); err != nil {
    return err
  }
  if _, err := session.Exec(`DELETE FROM kb_links WHERE id IN (
    SELECT l.id FROM kb_links l JOIN kb_documents d ON l.source_doc = d.id WHERE d.kb_id = ?
  )`, kbID); err != nil {
    _ = session.Rollback()
    return err
  }
  if _, err := session.Exec(`DELETE FROM kb_tags WHERE id IN (
    SELECT t.id FROM kb_tags t JOIN kb_documents d ON t.doc_id = d.id WHERE d.kb_id = ?
  )`, kbID); err != nil {
    _ = session.Rollback()
    return err
  }
  for i := range links {
    if _, err := session.Insert(&links[i]); err != nil {
      _ = session.Rollback()
      return err
    }
  }
  for i := range tags {
    if _, err := session.Insert(&tags[i]); err != nil {
      _ = session.Rollback()
      return err
    }
  }
  return session.Commit()
}

func GetDocumentLinks(docID int64) ([]KBLink, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var links []KBLink
  if err := engine.Where("source_doc = ?", docID).Find(&links); err != nil {
    return nil, err
  }
  return links, nil
}

func GetDocumentBacklinks(docID int64) ([]KBLink, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var links []KBLink
  if err := engine.Where("target_doc = ?", docID).Find(&links); err != nil {
    return nil, err
  }
  return links, nil
}

func GetDocumentTags(docID int64) ([]KBTag, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var tags []KBTag
  if err := engine.Where("doc_id = ?", docID).Find(&tags); err != nil {
    return nil, err
  }
  return tags, nil
}

func CreateImportTask(id string, kbID int64, username string) (*KBImportTask, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  task := &KBImportTask{ID: id, KbID: kbID, Username: username}
  if _, err := engine.Insert(task); err != nil {
    return nil, fmt.Errorf("create import task: %w", err)
  }
  return task, nil
}

func UpdateImportTaskStatus(id string, status string, progress int) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Where("id = ?", id).Cols("status", "progress").Update(&KBImportTask{Status: status, Progress: progress})
  return err
}

func UpdateImportTaskError(id string, errMsg string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Where("id = ?", id).Cols("status", "error_msg").Update(&KBImportTask{Status: "error", ErrorMsg: errMsg})
  return err
}

func GetImportTask(id string) (*KBImportTask, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var task KBImportTask
  has, err := engine.Where("id = ?", id).Get(&task)
  if err != nil {
    return nil, err
  }
  if !has {
    return nil, fmt.Errorf("import task %s not found", id)
  }
  return &task, nil
}

func CreateAuditLog(username, action, detail, source string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Insert(&KBAuditLog{Username: username, Action: action, Detail: detail, Source: source})
  return err
}

type KBSyncSource struct {
  ID          int64  `xorm:"pk autoincr 'id'"`
  KbID        int64  `xorm:"notnull 'kb_id'"`
  URL         string `xorm:"notnull 'url'"`
  Description string `xorm:"default '' 'description'"`
  SyncEnabled int    `xorm:"default 0 'sync_enabled'"`
  SyncCron    string `xorm:"default '' 'sync_cron'"`
  LastSyncAt  string `xorm:"-"`
  Checksum    string `xorm:"default '' 'checksum'"`
  CreatedAt   string `xorm:"created 'created_at'"`
  UpdatedAt   string `xorm:"updated 'updated_at'"`
}

func (KBSyncSource) TableName() string { return "kb_sync_sources" }

func CreateSyncSource(kbID int64, url, desc string) (*KBSyncSource, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  src := &KBSyncSource{KbID: kbID, URL: url, Description: desc}
  if _, err := engine.Insert(src); err != nil {
    return nil, fmt.Errorf("create sync source: %w", err)
  }
  return src, nil
}

func ListSyncSources(kbID int64) ([]KBSyncSource, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var sources []KBSyncSource
  if err := engine.Where("kb_id = ?", kbID).OrderBy("url").Find(&sources); err != nil {
    return nil, err
  }
  return sources, nil
}

func UpdateSyncSource(id int64, enabled int, cron string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Where("id = ?", id).Cols("sync_enabled", "sync_cron").Update(&KBSyncSource{SyncEnabled: enabled, SyncCron: cron})
  return err
}

func DeleteSyncSource(id int64) error {
  if err := ensureDB(); err != nil {
    return err
  }
  affected, err := engine.Where("id = ?", id).Delete(&KBSyncSource{})
  if err != nil {
    return err
  }
  if affected == 0 {
    return fmt.Errorf("sync source %d not found", id)
  }
  return nil
}

// ponytail: returns all enabled sources, refine to interval-matching if scale requires
func GetDueSyncSources() ([]KBSyncSource, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var sources []KBSyncSource
  if err := engine.Where("sync_enabled = 1").Find(&sources); err != nil {
    return nil, err
  }
  return sources, nil
}

func UpdateSyncSourceResult(id int64, checksum string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Where("id = ?", id).Cols("checksum", "last_sync_at").Update(&KBSyncSource{Checksum: checksum})
  // last_sync_at updated via raw SQL since xorm skips zero-value fields
  _, _ = engine.Exec("UPDATE kb_sync_sources SET last_sync_at = datetime('now','localtime') WHERE id = ?", id)
  return err
}

func UpdateDocumentsByURL(kbID int64, url, content, checksum string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := engine.Exec(`UPDATE kb_documents SET content = ?, checksum = ?, updated_at = datetime('now','localtime'), status = 'ready' WHERE kb_id = ? AND url = ?`, content, checksum, kbID, url)
  return err
}
