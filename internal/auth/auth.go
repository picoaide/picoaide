package auth

import (
  "crypto/rand"
  "database/sql"
  "encoding/hex"
  "fmt"
  "math/big"
  "os"
  "path/filepath"
  "strconv"
  "strings"
  "time"

  _ "modernc.org/sqlite"
  "golang.org/x/crypto/bcrypt"
)

const dbFileName = "picoaide.db"

var (
  db       *sql.DB
  dbDataDir string
)

// InitDB 打开或创建 SQLite 数据库
func InitDB(dataDir string) error {
  dbDataDir = dataDir

  if err := os.MkdirAll(dataDir, 0755); err != nil {
    return fmt.Errorf("创建数据库目录失败: %w", err)
  }

  dbPath := filepath.Join(dataDir, dbFileName)

  var err error
  db, err = sql.Open("sqlite", dbPath)
  if err != nil {
    return fmt.Errorf("打开数据库失败: %w", err)
  }

  if err := db.Ping(); err != nil {
    // 数据库损坏，备份后重建
    db.Close()
    db = nil
    backupPath := dbPath + ".broken." + time.Now().Format("20060102-150405")
    fmt.Fprintf(os.Stderr, "数据库损坏，已备份到 %s，正在重建\n", backupPath)
    os.Rename(dbPath, backupPath)

    db, err = sql.Open("sqlite", dbPath)
    if err != nil {
      return fmt.Errorf("重建数据库失败: %w", err)
    }
  }

  db.SetMaxOpenConns(1)

  if err := createTables(); err != nil {
    return fmt.Errorf("创建数据表失败: %w", err)
  }

  return nil
}

// GetDB 返回数据库连接（供其他包直接操作 DB）
func GetDB() (*sql.DB, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  return db, nil
}

// ensureDB 确保数据库连接可用，db 为 nil 时自动重连
func ensureDB() error {
  if db != nil {
    return nil
  }
  if dbDataDir == "" {
    return fmt.Errorf("数据库未初始化")
  }
  return InitDB(dbDataDir)
}

func createTables() error {
  _, err := db.Exec(`CREATE TABLE IF NOT EXISTS local_users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'user',
    created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime'))
  )`)
  if err != nil {
    return err
  }
  _, err = db.Exec(`CREATE TABLE IF NOT EXISTS containers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    container_id TEXT,
    image TEXT NOT NULL,
    status TEXT DEFAULT 'stopped',
    ip TEXT,
    cpu_limit REAL DEFAULT 0,
    memory_limit INTEGER DEFAULT 0,
    mcp_token TEXT DEFAULT '',
    created_at DATETIME DEFAULT (datetime('now','localtime')),
    updated_at DATETIME DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    return err
  }
  // 迁移：已有表添加 mcp_token 字段
  db.Exec(`ALTER TABLE containers ADD COLUMN mcp_token TEXT DEFAULT ''`)
  _, err = db.Exec(`CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    return err
  }
  _, err = db.Exec(`CREATE TABLE IF NOT EXISTS settings_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key TEXT NOT NULL,
    old_value TEXT,
    new_value TEXT,
    changed_by TEXT NOT NULL DEFAULT 'system',
    changed_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    return err
  }
  _, err = db.Exec(`CREATE TABLE IF NOT EXISTS whitelist (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    added_by TEXT NOT NULL DEFAULT 'system',
    added_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    return err
  }
  _, err = db.Exec(`CREATE TABLE IF NOT EXISTS groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    parent_id INTEGER REFERENCES groups(id) ON DELETE SET NULL,
    source TEXT NOT NULL DEFAULT 'local',
    description TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
  )`)
  if err != nil {
    return err
  }
  _, err = db.Exec(`CREATE TABLE IF NOT EXISTS user_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    UNIQUE(username, group_id)
  )`)
  if err != nil {
    return err
  }
  _, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_user_groups_username ON user_groups(username)`)
  if err != nil {
    return err
  }
  _, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_user_groups_group_id ON user_groups(group_id)`)
  if err != nil {
    return err
  }
  _, err = db.Exec(`CREATE TABLE IF NOT EXISTS group_skills (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    skill_name TEXT NOT NULL,
    UNIQUE(group_id, skill_name)
  )`)
  if err != nil {
    return err
  }

  // 迁移：旧数据库 groups 表没有 parent_id 列
  db.Exec(`ALTER TABLE groups ADD COLUMN parent_id INTEGER REFERENCES groups(id) ON DELETE SET NULL`)

  return nil
}

// CreateUser 创建本地用户
func CreateUser(username, password, role string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
  if err != nil {
    return fmt.Errorf("密码哈希失败: %w", err)
  }

  _, err = db.Exec(
    "INSERT INTO local_users (username, password_hash, role) VALUES (?, ?, ?)",
    username, string(hash), role,
  )
  if err != nil {
    return fmt.Errorf("创建用户失败: %w", err)
  }
  return nil
}

// AuthenticateLocal 校验本地用户，返回 (是否成功, 角色, 错误)
func AuthenticateLocal(username, password string) (bool, string, error) {
  if err := ensureDB(); err != nil {
    return false, "", err
  }
  var hash, role string
  err := db.QueryRow(
    "SELECT password_hash, role FROM local_users WHERE username = ?",
    username,
  ).Scan(&hash, &role)

  if err == sql.ErrNoRows {
    return false, "", nil
  }
  if err != nil {
    return false, "", fmt.Errorf("查询用户失败: %w", err)
  }

  if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
    return false, "", nil
  }

  return true, role, nil
}

// UserExists 检查本地用户是否存在
func UserExists(username string) bool {
  if ensureDB() != nil {
    return false
  }
  var count int
  db.QueryRow("SELECT COUNT(*) FROM local_users WHERE username = ?", username).Scan(&count)
  return count > 0
}

// LocalUser 记录本地用户基本信息
type LocalUser struct {
  Username string
  Role     string
}

// GetAllLocalUsers 返回所有本地用户
func GetAllLocalUsers() ([]LocalUser, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  rows, err := db.Query("SELECT username, role FROM local_users ORDER BY username")
  if err != nil {
    return nil, err
  }
  defer rows.Close()
  var list []LocalUser
  for rows.Next() {
    var u LocalUser
    if err := rows.Scan(&u.Username, &u.Role); err != nil {
      return nil, err
    }
    list = append(list, u)
  }
  return list, nil
}

// GetSuperadmins 返回所有超管列表
func GetSuperadmins() ([]string, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  rows, err := db.Query("SELECT username FROM local_users WHERE role = 'superadmin' ORDER BY username")
  if err != nil {
    return nil, err
  }
  defer rows.Close()
  var list []string
  for rows.Next() {
    var name string
    if err := rows.Scan(&name); err != nil {
      return nil, err
    }
    list = append(list, name)
  }
  return list, nil
}

// HasAnySuperadmin 检查系统中是否存在超管
func HasAnySuperadmin() bool {
  if ensureDB() != nil {
    return false
  }
  var count int
  db.QueryRow("SELECT COUNT(*) FROM local_users WHERE role = 'superadmin'").Scan(&count)
  return count > 0
}

// DB 返回数据库连接（供其他包使用）
func DB() *sql.DB {
  return db
}

// IsSuperadmin 检查指定用户是否是超管
func IsSuperadmin(username string) bool {
  if ensureDB() != nil {
    return false
  }
  var role string
  err := db.QueryRow(
    "SELECT role FROM local_users WHERE username = ?",
    username,
  ).Scan(&role)
  if err != nil {
    return false
  }
  return role == "superadmin"
}

// GetUserRole 获取用户角色
func GetUserRole(username string) string {
  if ensureDB() != nil {
    return ""
  }
  var role string
  err := db.QueryRow(
    "SELECT role FROM local_users WHERE username = ?",
    username,
  ).Scan(&role)
  if err != nil {
    return ""
  }
  return role
}

// ChangePassword 修改本地用户密码
func ChangePassword(username, newPassword string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
  if err != nil {
    return fmt.Errorf("密码哈希失败: %w", err)
  }
  result, err := db.Exec(
    "UPDATE local_users SET password_hash = ? WHERE username = ?",
    string(hash), username,
  )
  if err != nil {
    return fmt.Errorf("更新密码失败: %w", err)
  }
  n, _ := result.RowsAffected()
  if n == 0 {
    return fmt.Errorf("用户 %s 不存在", username)
  }
  return nil
}

// GenerateRandomPassword 生成指定长度的随机密码
func GenerateRandomPassword(length int) string {
  const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%&*"
  b := make([]byte, length)
  for i := range b {
    n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
    b[i] = charset[n.Int64()]
  }
  return string(b)
}

// DeleteUser 删除本地用户
func DeleteUser(username string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  result, err := db.Exec("DELETE FROM local_users WHERE username = ?", username)
  if err != nil {
    return fmt.Errorf("删除用户失败: %w", err)
  }
  n, _ := result.RowsAffected()
  if n == 0 {
    return fmt.Errorf("用户 %s 不存在", username)
  }
  return nil
}

// ============================================================
// 容器记录管理
// ============================================================

// ContainerRecord 容器数据库记录
type ContainerRecord struct {
  ID          int64
  Username    string
  ContainerID string
  Image       string
  Status      string
  IP          string
  CPULimit    float64
  MemoryLimit int64
  MCPToken    string
  CreatedAt   string
  UpdatedAt   string
}

// UpsertContainer 插入或更新容器记录
func UpsertContainer(rec *ContainerRecord) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := db.Exec(`INSERT INTO containers (username, container_id, image, status, ip, cpu_limit, memory_limit)
    VALUES (?, ?, ?, ?, ?, ?, ?)
    ON CONFLICT(username) DO UPDATE SET
      container_id = excluded.container_id,
      image = excluded.image,
      status = excluded.status,
      ip = excluded.ip,
      cpu_limit = excluded.cpu_limit,
      memory_limit = excluded.memory_limit,
      updated_at = datetime('now','localtime')`,
    rec.Username, rec.ContainerID, rec.Image, rec.Status, rec.IP, rec.CPULimit, rec.MemoryLimit)
  return err
}

// GetContainerByUsername 按用户名查询容器记录
func GetContainerByUsername(username string) (*ContainerRecord, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var r ContainerRecord
  err := db.QueryRow(`SELECT id, username, container_id, image, status, ip, cpu_limit, memory_limit, mcp_token, created_at, updated_at
    FROM containers WHERE username = ?`, username).Scan(&r.ID, &r.Username, &r.ContainerID, &r.Image, &r.Status, &r.IP, &r.CPULimit, &r.MemoryLimit, &r.MCPToken, &r.CreatedAt, &r.UpdatedAt)
  if err == sql.ErrNoRows {
    return nil, nil
  }
  if err != nil {
    return nil, err
  }
  return &r, nil
}

// GetAllContainers 返回所有容器记录
func GetAllContainers() ([]ContainerRecord, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  rows, err := db.Query(`SELECT id, username, container_id, image, status, ip, cpu_limit, memory_limit, mcp_token, created_at, updated_at FROM containers ORDER BY id`)
  if err != nil {
    return nil, err
  }
  defer rows.Close()
  var list []ContainerRecord
  for rows.Next() {
    var r ContainerRecord
    if err := rows.Scan(&r.ID, &r.Username, &r.ContainerID, &r.Image, &r.Status, &r.IP, &r.CPULimit, &r.MemoryLimit, &r.MCPToken, &r.CreatedAt, &r.UpdatedAt); err != nil {
      return nil, err
    }
    list = append(list, r)
  }
  return list, nil
}

// DeleteContainer 删除容器记录
func DeleteContainer(username string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := db.Exec("DELETE FROM containers WHERE username = ?", username)
  return err
}

// UpdateContainerStatus 更新容器状态
func UpdateContainerStatus(username, status string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := db.Exec("UPDATE containers SET status = ?, updated_at = datetime('now','localtime') WHERE username = ?", status, username)
  return err
}

// UpdateContainerID 更新 Docker 容器 ID
func UpdateContainerID(username, containerID string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := db.Exec("UPDATE containers SET container_id = ?, updated_at = datetime('now','localtime') WHERE username = ?", containerID, username)
  return err
}

// UpdateContainerImage 更新用户容器镜像引用
func UpdateContainerImage(username, imageRef string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := db.Exec("UPDATE containers SET image = ?, updated_at = datetime('now','localtime') WHERE username = ?", imageRef, username)
  return err
}

// AllocateNextIP 分配下一个可用 IP（100.64.0.2 起）
func AllocateNextIP() (string, error) {
  if err := ensureDB(); err != nil {
    return "", err
  }
  var maxIP string
  db.QueryRow("SELECT ip FROM containers WHERE ip IS NOT NULL ORDER BY id DESC LIMIT 1").Scan(&maxIP)
  if maxIP == "" {
    return "100.64.0.2", nil
  }
  // 解析最后一段递增
  parts := strings.SplitN(maxIP, ".", 4)
  if len(parts) != 4 {
    return "100.64.0.2", nil
  }
  last, err := strconv.Atoi(parts[3])
  if err != nil {
    return "100.64.0.2", nil
  }
  last++
  return fmt.Sprintf("%s.%s.%s.%d", parts[0], parts[1], parts[2], last), nil
}

// ============================================================
// 用户组管理
// ============================================================

// GroupInfo 组信息（包含成员数和绑定技能数）
type GroupInfo struct {
  ID          int64  `json:"id"`
  Name        string `json:"name"`
  ParentID    *int64 `json:"parent_id"`
  Source      string `json:"source"`
  Description string `json:"description"`
  MemberCount int    `json:"member_count"`
  SkillCount  int    `json:"skill_count"`
}

// CreateGroup 创建组，parentID 为 nil 表示顶级组
func CreateGroup(name, source, description string, parentID *int64) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := db.Exec("INSERT INTO groups (name, parent_id, source, description) VALUES (?, ?, ?, ?)", name, parentID, source, description)
  if err != nil {
    return fmt.Errorf("创建组失败: %w", err)
  }
  return nil
}

// DeleteGroup 删除组（级联删除成员和技能绑定）
func DeleteGroup(name string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  result, err := db.Exec("DELETE FROM groups WHERE name = ?", name)
  if err != nil {
    return fmt.Errorf("删除组失败: %w", err)
  }
  n, _ := result.RowsAffected()
  if n == 0 {
    return fmt.Errorf("组 %s 不存在", name)
  }
  return nil
}

// ListGroups 列出所有组及其统计
func ListGroups() ([]GroupInfo, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  rows, err := db.Query(`SELECT g.id, g.name, g.parent_id, g.source, g.description,
    (SELECT COUNT(*) FROM user_groups ug WHERE ug.group_id = g.id) AS member_count,
    (SELECT COUNT(*) FROM group_skills gs WHERE gs.group_id = g.id) AS skill_count
    FROM groups g ORDER BY g.name`)
  if err != nil {
    return nil, err
  }
  defer rows.Close()
  var list []GroupInfo
  for rows.Next() {
    var g GroupInfo
    if err := rows.Scan(&g.ID, &g.Name, &g.ParentID, &g.Source, &g.Description, &g.MemberCount, &g.SkillCount); err != nil {
      return nil, err
    }
    list = append(list, g)
  }
  return list, nil
}

// GetGroupID 根据组名获取组 ID
func GetGroupID(name string) (int64, error) {
  if err := ensureDB(); err != nil {
    return 0, err
  }
  var id int64
  err := db.QueryRow("SELECT id FROM groups WHERE name = ?", name).Scan(&id)
  if err == sql.ErrNoRows {
    return 0, fmt.Errorf("组 %s 不存在", name)
  }
  return id, err
}

// AddUsersToGroup 批量添加用户到组
func AddUsersToGroup(groupName string, usernames []string) error {
  gid, err := GetGroupID(groupName)
  if err != nil {
    return err
  }
  for _, u := range usernames {
    db.Exec("INSERT OR IGNORE INTO user_groups (username, group_id) VALUES (?, ?)", u, gid)
  }
  return nil
}

// RemoveUserFromGroup 从组移除用户
func RemoveUserFromGroup(groupName, username string) error {
  gid, err := GetGroupID(groupName)
  if err != nil {
    return err
  }
  _, err = db.Exec("DELETE FROM user_groups WHERE group_id = ? AND username = ?", gid, username)
  return err
}

// GetGroupMembers 获取组成员列表
func GetGroupMembers(groupName string) ([]string, error) {
  gid, err := GetGroupID(groupName)
  if err != nil {
    return nil, err
  }
  rows, err := db.Query("SELECT username FROM user_groups WHERE group_id = ? ORDER BY username", gid)
  if err != nil {
    return nil, err
  }
  defer rows.Close()
  var list []string
  for rows.Next() {
    var u string
    if err := rows.Scan(&u); err != nil {
      return nil, err
    }
    list = append(list, u)
  }
  return list, nil
}

// GetGroupsForUser 获取用户所属的组名列表
func GetGroupsForUser(username string) ([]string, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  rows, err := db.Query(`SELECT g.name FROM groups g JOIN user_groups ug ON g.id = ug.group_id WHERE ug.username = ? ORDER BY g.name`, username)
  if err != nil {
    return nil, err
  }
  defer rows.Close()
  var list []string
  for rows.Next() {
    var n string
    if err := rows.Scan(&n); err != nil {
      return nil, err
    }
    list = append(list, n)
  }
  return list, nil
}

// SyncUserGroups 差量更新用户的组关系（传入用户应属于的组名列表）
func SyncUserGroups(username string, groupNames []string) error {
  if err := ensureDB(); err != nil {
    return err
  }
  tx, err := db.Begin()
  if err != nil {
    return err
  }
  defer tx.Rollback()

  // 确保所有组存在
  for _, name := range groupNames {
    tx.Exec("INSERT OR IGNORE INTO groups (name, source) VALUES (?, 'ldap')", name)
  }

  // 删除用户当前所有组关系
  tx.Exec("DELETE FROM user_groups WHERE username = ?", username)

  // 添加新的组关系
  for _, name := range groupNames {
    var gid int64
    if err := tx.QueryRow("SELECT id FROM groups WHERE name = ?", name).Scan(&gid); err != nil {
      continue
    }
    tx.Exec("INSERT OR IGNORE INTO user_groups (username, group_id) VALUES (?, ?)", username, gid)
  }

  return tx.Commit()
}

// BindSkillToGroup 绑定技能到组
func BindSkillToGroup(groupName, skillName string) error {
  gid, err := GetGroupID(groupName)
  if err != nil {
    return err
  }
  _, err = db.Exec("INSERT OR IGNORE INTO group_skills (group_id, skill_name) VALUES (?, ?)", gid, skillName)
  return err
}

// UnbindSkillFromGroup 解绑技能
func UnbindSkillFromGroup(groupName, skillName string) error {
  gid, err := GetGroupID(groupName)
  if err != nil {
    return err
  }
  _, err = db.Exec("DELETE FROM group_skills WHERE group_id = ? AND skill_name = ?", gid, skillName)
  return err
}

// GetGroupSkills 获取组绑定的技能列表
func GetGroupSkills(groupName string) ([]string, error) {
  gid, err := GetGroupID(groupName)
  if err != nil {
    return nil, err
  }
  rows, err := db.Query("SELECT skill_name FROM group_skills WHERE group_id = ? ORDER BY skill_name", gid)
  if err != nil {
    return nil, err
  }
  defer rows.Close()
  var list []string
  for rows.Next() {
    var s string
    if err := rows.Scan(&s); err != nil {
      return nil, err
    }
    list = append(list, s)
  }
  return list, nil
}

// GetGroupMembersForDeploy 获取组成员的用户名列表（递归包含所有子组成员）
func GetGroupMembersForDeploy(groupName string) ([]string, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }

  var groupID int64
  if err := db.QueryRow("SELECT id FROM groups WHERE name = ?", groupName).Scan(&groupID); err != nil {
    return nil, fmt.Errorf("组 %s 不存在", groupName)
  }

  // 收集目标组及所有子组 ID
  ids := []int64{groupID}
  subIDs, err := GetSubGroupIDs(groupID)
  if err != nil {
    return nil, err
  }
  ids = append(ids, subIDs...)

  // 批量查询所有组的成员
  seen := make(map[string]bool)
  var members []string
  for _, gid := range ids {
    rows, err := db.Query("SELECT username FROM user_groups WHERE group_id = ?", gid)
    if err != nil {
      continue
    }
    for rows.Next() {
      var u string
      if rows.Scan(&u) == nil && !seen[u] {
        seen[u] = true
        members = append(members, u)
      }
    }
    rows.Close()
  }
  return members, nil
}

// GetSubGroupIDs 递归获取所有子组 ID
func GetSubGroupIDs(groupID int64) ([]int64, error) {
  if err := ensureDB(); err != nil {
    return nil, err
  }
  var result []int64
  var walk func(pid int64) error
  walk = func(pid int64) error {
    rows, err := db.Query("SELECT id FROM groups WHERE parent_id = ?", pid)
    if err != nil {
      return err
    }
    defer rows.Close()
    for rows.Next() {
      var id int64
      if err := rows.Scan(&id); err != nil {
        continue
      }
      result = append(result, id)
      walk(id)
    }
    return nil
  }
  if err := walk(groupID); err != nil {
    return nil, err
  }
  return result, nil
}

// SetGroupParent 设置组的父组
func SetGroupParent(groupName string, parentID *int64) error {
  if err := ensureDB(); err != nil {
    return err
  }
  _, err := db.Exec("UPDATE groups SET parent_id = ? WHERE name = ?", parentID, groupName)
  return err
}

// GetGroupIDByName 根据组名获取 ID
func GetGroupIDByName(name string) (int64, error) {
  if err := ensureDB(); err != nil {
    return 0, err
  }
  var id int64
  err := db.QueryRow("SELECT id FROM groups WHERE name = ?", name).Scan(&id)
  return id, err
}

// GenerateMCPToken 为用户生成 MCP token（用户名:随机hex）并存入 DB
func GenerateMCPToken(username string) (string, error) {
  if err := ensureDB(); err != nil {
    return "", err
  }
  b := make([]byte, 32)
  if _, err := rand.Read(b); err != nil {
    return "", fmt.Errorf("生成随机数失败: %w", err)
  }
  token := username + ":" + hex.EncodeToString(b)

  // 先尝试 UPDATE，如果无匹配行则 INSERT
  result, err := db.Exec(`UPDATE containers SET mcp_token = ? WHERE username = ?`, token, username)
  if err != nil {
    return "", fmt.Errorf("保存 MCP token 失败: %w", err)
  }
  affected, _ := result.RowsAffected()
  if affected == 0 {
    _, err = db.Exec(`INSERT INTO containers (username, image, status, mcp_token) VALUES (?, '', 'stopped', ?)`, username, token)
    if err != nil {
      return "", fmt.Errorf("创建容器记录失败: %w", err)
    }
  }
  return token, nil
}

// GetMCPToken 获取用户的 MCP token
func GetMCPToken(username string) (string, error) {
  if err := ensureDB(); err != nil {
    return "", err
  }
  var token string
  err := db.QueryRow(`SELECT mcp_token FROM containers WHERE username = ?`, username).Scan(&token)
  if err == sql.ErrNoRows {
    return "", nil
  }
  return token, err
}

// ValidateMCPToken 验证 MCP token，返回用户名
func ValidateMCPToken(token string) (string, bool) {
  if token == "" {
    return "", false
  }
  parts := strings.SplitN(token, ":", 2)
  if len(parts) != 2 {
    return "", false
  }
  username := parts[0]
  stored, err := GetMCPToken(username)
  if err != nil || stored != token {
    return "", false
  }
  return username, true
}
