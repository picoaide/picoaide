package auth

import (
  "crypto/rand"
  "database/sql"
  "fmt"
  "math/big"
  "path/filepath"
  "strconv"
  "strings"

  _ "modernc.org/sqlite"
  "golang.org/x/crypto/bcrypt"
)

const dbFileName = "picoaide.db"

var db *sql.DB

// InitDB 打开或创建 SQLite 数据库
func InitDB(dataDir string) error {
  dbPath := filepath.Join(dataDir, dbFileName)

  var err error
  db, err = sql.Open("sqlite", dbPath)
  if err != nil {
    return fmt.Errorf("打开数据库失败: %w", err)
  }

  // SQLite 单写优化
  db.SetMaxOpenConns(1)

  // 总是执行 CREATE TABLE IF NOT EXISTS，确保新增的表也能创建
  if err := createTables(); err != nil {
    return fmt.Errorf("创建数据表失败: %w", err)
  }

  return nil
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
    created_at DATETIME DEFAULT (datetime('now','localtime')),
    updated_at DATETIME DEFAULT (datetime('now','localtime'))
  )`)
  return err
}

// CreateUser 创建本地用户
func CreateUser(username, password, role string) error {
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
  var count int
  db.QueryRow("SELECT COUNT(*) FROM local_users WHERE username = ?", username).Scan(&count)
  return count > 0
}

// HasAnySuperadmin 检查系统中是否存在超管
func HasAnySuperadmin() bool {
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
  CreatedAt   string
  UpdatedAt   string
}

// UpsertContainer 插入或更新容器记录
func UpsertContainer(rec *ContainerRecord) error {
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
  var r ContainerRecord
  err := db.QueryRow(`SELECT id, username, container_id, image, status, ip, cpu_limit, memory_limit, created_at, updated_at
    FROM containers WHERE username = ?`, username).Scan(&r.ID, &r.Username, &r.ContainerID, &r.Image, &r.Status, &r.IP, &r.CPULimit, &r.MemoryLimit, &r.CreatedAt, &r.UpdatedAt)
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
  rows, err := db.Query(`SELECT id, username, container_id, image, status, ip, cpu_limit, memory_limit, created_at, updated_at FROM containers ORDER BY id`)
  if err != nil {
    return nil, err
  }
  defer rows.Close()
  var list []ContainerRecord
  for rows.Next() {
    var r ContainerRecord
    if err := rows.Scan(&r.ID, &r.Username, &r.ContainerID, &r.Image, &r.Status, &r.IP, &r.CPULimit, &r.MemoryLimit, &r.CreatedAt, &r.UpdatedAt); err != nil {
      return nil, err
    }
    list = append(list, r)
  }
  return list, nil
}

// DeleteContainer 删除容器记录
func DeleteContainer(username string) error {
  _, err := db.Exec("DELETE FROM containers WHERE username = ?", username)
  return err
}

// UpdateContainerStatus 更新容器状态
func UpdateContainerStatus(username, status string) error {
  _, err := db.Exec("UPDATE containers SET status = ?, updated_at = datetime('now','localtime') WHERE username = ?", status, username)
  return err
}

// UpdateContainerID 更新 Docker 容器 ID
func UpdateContainerID(username, containerID string) error {
  _, err := db.Exec("UPDATE containers SET container_id = ?, updated_at = datetime('now','localtime') WHERE username = ?", containerID, username)
  return err
}

// AllocateNextIP 分配下一个可用 IP（100.64.0.2 起）
func AllocateNextIP() (string, error) {
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
