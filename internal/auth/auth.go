package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

const dbFileName = "picoaide.db"

const argon2idHashPrefix = "$argon2id$"

var passwordHashParams = struct {
	memory  uint32
	time    uint32
	threads uint8
	keyLen  uint32
	saltLen int
}{
	memory:  4 * 1024,
	time:    1,
	threads: 1,
	keyLen:  32,
	saltLen: 16,
}

var (
	engine    *xorm.Engine
	dbDataDir string
)

// ============================================================
// ORM 模型定义
// ============================================================

// LocalUser 本地用户表
type LocalUser struct {
	ID           int64  `xorm:"pk autoincr 'id'"`
	Username     string `xorm:"unique notnull 'username'"`
	PasswordHash string `xorm:"notnull 'password_hash'"`
	Role         string `xorm:"notnull 'role'"`
	CreatedAt    string `xorm:"notnull 'created_at'"`
}

func (LocalUser) TableName() string {
	return "local_users"
}

// ContainerRecord 容器数据库记录
type ContainerRecord struct {
	ID          int64   `xorm:"pk autoincr 'id'"`
	Username    string  `xorm:"unique notnull 'username'"`
	ContainerID string  `xorm:"'container_id'"`
	Image       string  `xorm:"notnull 'image'"`
	Status      string  `xorm:"'status'"`
	IP          string  `xorm:"'ip'"`
	CPULimit    float64 `xorm:"'cpu_limit'"`
	MemoryLimit int64   `xorm:"'memory_limit'"`
	MCPToken    string  `xorm:"'mcp_token'"`
	CreatedAt   string  `xorm:"'created_at'"`
	UpdatedAt   string  `xorm:"'updated_at'"`
}

func (ContainerRecord) TableName() string {
	return "containers"
}

// Setting 系统设置表
type Setting struct {
	Key       string `xorm:"pk 'key'"`
	Value     string `xorm:"notnull 'value'"`
	UpdatedAt string `xorm:"notnull 'updated_at'"`
}

func (Setting) TableName() string {
	return "settings"
}

// SettingsHistory 设置变更历史表
type SettingsHistory struct {
	ID        int64  `xorm:"pk autoincr 'id'"`
	Key       string `xorm:"notnull 'key'"`
	OldValue  string `xorm:"'old_value'"`
	NewValue  string `xorm:"'new_value'"`
	ChangedBy string `xorm:"notnull 'changed_by'"`
	ChangedAt string `xorm:"notnull 'changed_at'"`
}

func (SettingsHistory) TableName() string {
	return "settings_history"
}

// WhitelistEntry 白名单表
type WhitelistEntry struct {
	ID       int64  `xorm:"pk autoincr 'id'"`
	Username string `xorm:"unique notnull 'username'"`
	AddedBy  string `xorm:"notnull 'added_by'"`
	AddedAt  string `xorm:"notnull 'added_at'"`
}

func (WhitelistEntry) TableName() string {
	return "whitelist"
}

// Group 用户组表
type Group struct {
	ID          int64  `xorm:"pk autoincr 'id'"`
	Name        string `xorm:"unique notnull 'name'"`
	ParentID    *int64 `xorm:"'parent_id'"`
	Source      string `xorm:"notnull 'source'"`
	Description string `xorm:"notnull 'description'"`
	CreatedAt   string `xorm:"notnull 'created_at'"`
}

func (Group) TableName() string {
	return "groups"
}

// UserGroup 用户-组关联表
type UserGroup struct {
	ID       int64  `xorm:"pk autoincr 'id'"`
	Username string `xorm:"notnull 'username'"`
	GroupID  int64  `xorm:"notnull 'group_id'"`
}

func (UserGroup) TableName() string {
	return "user_groups"
}

// GroupSkill 组-技能关联表
type GroupSkill struct {
	ID        int64  `xorm:"pk autoincr 'id'"`
	GroupID   int64  `xorm:"notnull 'group_id'"`
	SkillName string `xorm:"notnull 'skill_name'"`
}

func (GroupSkill) TableName() string {
	return "group_skills"
}

// UserChannel 记录用户可见渠道和启用状态。具体密钥仍存放在用户自己的 .security.yml。
type UserChannel struct {
	ID            int64  `xorm:"pk autoincr 'id'"`
	Username      string `xorm:"notnull 'username'"`
	Channel       string `xorm:"notnull 'channel'"`
	Allowed       bool   `xorm:"notnull 'allowed'"`
	Enabled       bool   `xorm:"notnull 'enabled'"`
	Configured    bool   `xorm:"notnull 'configured'"`
	ConfigVersion int    `xorm:"notnull 'config_version'"`
	UpdatedAt     string `xorm:"notnull 'updated_at'"`
}

func (UserChannel) TableName() string {
	return "user_channels"
}

// GroupInfo 组信息（包含成员数和绑定技能数），非数据库模型，仅用于查询结果
type GroupInfo struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ParentID    *int64 `json:"parent_id"`
	Source      string `json:"source"`
	Description string `json:"description"`
	MemberCount int    `json:"member_count"`
	SkillCount  int    `json:"skill_count"`
}

// ============================================================
// 数据库初始化
// ============================================================

// InitDB 打开或创建 SQLite 数据库
func InitDB(dataDir string) error {
	dbDataDir = dataDir

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("创建数据库目录失败: %w", err)
	}

	dbPath := filepath.Join(dataDir, dbFileName)

	var err error
	engine, err = xorm.NewEngine("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}

	if err := engine.Ping(); err != nil {
		// 数据库损坏，备份后重建
		engine.Close()
		engine = nil
		backupPath := dbPath + ".broken." + time.Now().Format("20060102-150405")
		fmt.Fprintf(os.Stderr, "数据库损坏，已备份到 %s，正在重建\n", backupPath)
		os.Rename(dbPath, backupPath)

		engine, err = xorm.NewEngine("sqlite", dbPath)
		if err != nil {
			return fmt.Errorf("重建数据库失败: %w", err)
		}
	}

	engine.SetMaxOpenConns(1)
	// 禁用 xorm 缓存，避免与手动 SQL 操作产生不一致
	engine.SetDefaultCacher(nil)

	if err := syncSchema(); err != nil {
		return fmt.Errorf("创建数据表失败: %w", err)
	}

	return nil
}

// ResetDB 关闭当前数据库连接并重置全局状态（测试用）
func ResetDB() {
	if engine != nil {
		engine.Close()
	}
	engine = nil
	dbDataDir = ""
}

// GetDB 返回底层 *sql.DB 连接（供其他包直接操作 DB，向后兼容）
func GetDB() (*sql.DB, error) {
	if err := ensureDB(); err != nil {
		return nil, err
	}
	return engine.DB().DB, nil
}

// GetEngine 返回 xorm 引擎（供新代码使用）
func GetEngine() (*xorm.Engine, error) {
	if err := ensureDB(); err != nil {
		return nil, err
	}
	return engine, nil
}

// DB 返回底层 *sql.DB 连接（供其他包使用，向后兼容）
func DB() *sql.DB {
	if engine == nil {
		return nil
	}
	return engine.DB().DB
}

// ensureDB 确保数据库连接可用，engine 为 nil 时自动重连
func ensureDB() error {
	if engine != nil {
		return nil
	}
	if dbDataDir == "" {
		return fmt.Errorf("数据库未初始化")
	}
	return InitDB(dbDataDir)
}

// syncSchema 使用原始 SQL 创建表结构（保留 SQLite datetime 默认值），并做必要的迁移
func syncSchema() error {
	_, err := engine.Exec(`CREATE TABLE IF NOT EXISTS local_users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'user',
    created_at DATETIME NOT NULL DEFAULT (datetime('now', 'localtime'))
  )`)
	if err != nil {
		return err
	}
	_, err = engine.Exec(`CREATE TABLE IF NOT EXISTS containers (
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
	engine.Exec(`ALTER TABLE containers ADD COLUMN mcp_token TEXT DEFAULT ''`)
	_, err = engine.Exec(`CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
  )`)
	if err != nil {
		return err
	}
	_, err = engine.Exec(`CREATE TABLE IF NOT EXISTS settings_history (
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
	_, err = engine.Exec(`CREATE TABLE IF NOT EXISTS whitelist (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    added_by TEXT NOT NULL DEFAULT 'system',
    added_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
  )`)
	if err != nil {
		return err
	}
	_, err = engine.Exec(`CREATE TABLE IF NOT EXISTS groups (
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
	_, err = engine.Exec(`CREATE TABLE IF NOT EXISTS user_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    UNIQUE(username, group_id)
  )`)
	if err != nil {
		return err
	}
	_, err = engine.Exec(`CREATE INDEX IF NOT EXISTS idx_user_groups_username ON user_groups(username)`)
	if err != nil {
		return err
	}
	_, err = engine.Exec(`CREATE INDEX IF NOT EXISTS idx_user_groups_group_id ON user_groups(group_id)`)
	if err != nil {
		return err
	}
	_, err = engine.Exec(`CREATE TABLE IF NOT EXISTS group_skills (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    skill_name TEXT NOT NULL,
    UNIQUE(group_id, skill_name)
  )`)
	if err != nil {
		return err
	}
	_, err = engine.Exec(`CREATE TABLE IF NOT EXISTS user_channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL,
    channel TEXT NOT NULL,
    allowed INTEGER NOT NULL DEFAULT 1,
    enabled INTEGER NOT NULL DEFAULT 0,
    configured INTEGER NOT NULL DEFAULT 0,
    config_version INTEGER NOT NULL DEFAULT 0,
    updated_at DATETIME NOT NULL DEFAULT (datetime('now','localtime')),
    UNIQUE(username, channel)
  )`)
	if err != nil {
		return err
	}
	_, err = engine.Exec(`CREATE INDEX IF NOT EXISTS idx_user_channels_username ON user_channels(username)`)
	if err != nil {
		return err
	}

	// 迁移：旧数据库 groups 表没有 parent_id 列
	engine.Exec(`ALTER TABLE groups ADD COLUMN parent_id INTEGER REFERENCES groups(id) ON DELETE SET NULL`)

	return nil
}

// ============================================================
// 用户认证管理
// ============================================================

func hashPassword(password string) (string, error) {
	salt := make([]byte, passwordHashParams.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, passwordHashParams.time, passwordHashParams.memory, passwordHashParams.threads, passwordHashParams.keyLen)
	return fmt.Sprintf("%sv=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2idHashPrefix,
		argon2.Version,
		passwordHashParams.memory,
		passwordHashParams.time,
		passwordHashParams.threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func verifyPassword(storedHash, password string) (ok bool, needsUpgrade bool, err error) {
	if strings.HasPrefix(storedHash, argon2idHashPrefix) {
		ok, err := verifyArgon2idPassword(storedHash, password)
		return ok, false, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)); err != nil {
		return false, false, nil
	}
	return true, true, nil
}

func verifyArgon2idPassword(storedHash, password string) (bool, error) {
	parts := strings.Split(storedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, nil
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, nil
	}

	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false, nil
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, nil
	}
	expectedKey, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, nil
	}
	actualKey := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(expectedKey)))
	return subtle.ConstantTimeCompare(actualKey, expectedKey) == 1, nil
}

// CreateUser 创建本地用户
func CreateUser(username, password, role string) error {
	if err := ensureDB(); err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return fmt.Errorf("密码哈希失败: %w", err)
	}

	user := &LocalUser{
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
	}
	_, err = engine.Insert(user)
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
	var user LocalUser
	has, err := engine.Where("username = ?", username).Get(&user)
	if err != nil {
		return false, "", fmt.Errorf("查询用户失败: %w", err)
	}
	if !has {
		return false, "", nil
	}

	ok, needsUpgrade, err := verifyPassword(user.PasswordHash, password)
	if err != nil {
		return false, "", fmt.Errorf("校验密码失败: %w", err)
	}
	if !ok {
		return false, "", nil
	}
	if needsUpgrade {
		if hash, err := hashPassword(password); err == nil {
			_, _ = engine.ID(user.ID).Cols("password_hash").Update(&LocalUser{PasswordHash: hash})
		}
	}

	return true, user.Role, nil
}

// UserExists 检查本地用户是否存在
func UserExists(username string) bool {
	if ensureDB() != nil {
		return false
	}
	has, _ := engine.Where("username = ?", username).Exist(&LocalUser{})
	return has
}

// GetAllLocalUsers 返回所有本地用户
func GetAllLocalUsers() ([]LocalUser, error) {
	if err := ensureDB(); err != nil {
		return nil, err
	}
	var users []LocalUser
	err := engine.OrderBy("username").Find(&users)
	if err != nil {
		return nil, err
	}
	// 转换为只含 Username/Role 的结构（保持原返回类型）
	result := make([]LocalUser, 0, len(users))
	for _, u := range users {
		result = append(result, LocalUser{Username: u.Username, Role: u.Role})
	}
	return result, nil
}

// GetSuperadmins 返回所有超管列表
func GetSuperadmins() ([]string, error) {
	if err := ensureDB(); err != nil {
		return nil, err
	}
	var users []LocalUser
	err := engine.Where("role = ?", "superadmin").OrderBy("username").Find(&users)
	if err != nil {
		return nil, err
	}
	list := make([]string, 0, len(users))
	for _, u := range users {
		list = append(list, u.Username)
	}
	return list, nil
}

// HasAnySuperadmin 检查系统中是否存在超管
func HasAnySuperadmin() bool {
	if ensureDB() != nil {
		return false
	}
	count, _ := engine.Where("role = ?", "superadmin").Count(&LocalUser{})
	return count > 0
}

// IsSuperadmin 检查指定用户是否是超管
func IsSuperadmin(username string) bool {
	if ensureDB() != nil {
		return false
	}
	var user LocalUser
	has, err := engine.Where("username = ?", username).Get(&user)
	if err != nil || !has {
		return false
	}
	return user.Role == "superadmin"
}

// GetUserRole 获取用户角色
func GetUserRole(username string) string {
	if ensureDB() != nil {
		return ""
	}
	var user LocalUser
	has, err := engine.Where("username = ?", username).Get(&user)
	if err != nil || !has {
		return ""
	}
	return user.Role
}

// ChangePassword 修改本地用户密码
func ChangePassword(username, newPassword string) error {
	if err := ensureDB(); err != nil {
		return err
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("密码哈希失败: %w", err)
	}
	affected, err := engine.Where("username = ?", username).
		Cols("password_hash").
		Update(&LocalUser{PasswordHash: string(hash)})
	if err != nil {
		return fmt.Errorf("更新密码失败: %w", err)
	}
	if affected == 0 {
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
	affected, err := engine.Where("username = ?", username).Delete(&LocalUser{})
	if err != nil {
		return fmt.Errorf("删除用户失败: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("用户 %s 不存在", username)
	}
	return nil
}

// ============================================================
// 容器记录管理
// ============================================================

// UpsertContainer 插入或更新容器记录
func UpsertContainer(rec *ContainerRecord) error {
	if err := ensureDB(); err != nil {
		return err
	}
	// SQLite 的 ON CONFLICT 语句需要原始 SQL
	_, err := engine.Exec(`INSERT INTO containers (username, container_id, image, status, ip, cpu_limit, memory_limit)
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
	var rec ContainerRecord
	has, err := engine.Where("username = ?", username).Get(&rec)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}
	return &rec, nil
}

// GetAllContainers 返回所有容器记录
func GetAllContainers() ([]ContainerRecord, error) {
	if err := ensureDB(); err != nil {
		return nil, err
	}
	var list []ContainerRecord
	err := engine.OrderBy("id").Find(&list)
	if err != nil {
		return nil, err
	}
	return list, nil
}

// DeleteContainer 删除容器记录
func DeleteContainer(username string) error {
	if err := ensureDB(); err != nil {
		return err
	}
	_, err := engine.Where("username = ?", username).Delete(&ContainerRecord{})
	return err
}

// UpdateContainerStatus 更新容器状态
func UpdateContainerStatus(username, status string) error {
	if err := ensureDB(); err != nil {
		return err
	}
	_, err := engine.Where("username = ?", username).
		Cols("status", "updated_at").
		Update(&ContainerRecord{Status: status, UpdatedAt: time.Now().Format("2006-01-02 15:04:05")})
	return err
}

// UpdateContainerID 更新 Docker 容器 ID
func UpdateContainerID(username, containerID string) error {
	if err := ensureDB(); err != nil {
		return err
	}
	_, err := engine.Where("username = ?", username).
		Cols("container_id", "updated_at").
		Update(&ContainerRecord{ContainerID: containerID, UpdatedAt: time.Now().Format("2006-01-02 15:04:05")})
	return err
}

// UpdateContainerImage 更新用户容器镜像引用
func UpdateContainerImage(username, imageRef string) error {
	if err := ensureDB(); err != nil {
		return err
	}
	_, err := engine.Where("username = ?", username).
		Cols("image", "updated_at").
		Update(&ContainerRecord{Image: imageRef, UpdatedAt: time.Now().Format("2006-01-02 15:04:05")})
	return err
}

// UpsertUserChannelStatus 写入用户渠道可见性和启用状态。
func UpsertUserChannelStatus(username, channel string, allowed, enabled, configured bool, configVersion int) error {
	if err := ensureDB(); err != nil {
		return err
	}
	_, err := engine.Exec(`INSERT INTO user_channels (username, channel, allowed, enabled, configured, config_version, updated_at)
    VALUES (?, ?, ?, ?, ?, ?, datetime('now','localtime'))
    ON CONFLICT(username, channel) DO UPDATE SET
      allowed = excluded.allowed,
      enabled = excluded.enabled,
      configured = excluded.configured,
      config_version = excluded.config_version,
      updated_at = datetime('now','localtime')`,
		username, channel, boolInt(allowed), boolInt(enabled), boolInt(configured), configVersion)
	return err
}

// GetUserChannelStatus 返回用户渠道状态记录，不存在时返回 nil。
func GetUserChannelStatus(username, channel string) (*UserChannel, error) {
	if err := ensureDB(); err != nil {
		return nil, err
	}
	var rec UserChannel
	has, err := engine.Where("username = ? AND channel = ?", username, channel).Get(&rec)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}
	return &rec, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// AllocateNextIP 分配下一个可用 IP（100.64.0.2 起）
func AllocateNextIP() (string, error) {
	if err := ensureDB(); err != nil {
		return "", err
	}
	var rec ContainerRecord
	has, _ := engine.Where("ip IS NOT NULL AND ip != ''").OrderBy("id DESC").Limit(1).Get(&rec)
	if !has || rec.IP == "" {
		return "100.64.0.2", nil
	}
	// 解析最后一段递增
	parts := strings.SplitN(rec.IP, ".", 4)
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

// CreateGroup 创建组，parentID 为 nil 表示顶级组。
func CreateGroup(name, source, description string, parentID *int64) error {
	if err := ensureDB(); err != nil {
		return err
	}
	group := &Group{
		Name:        name,
		ParentID:    parentID,
		Source:      source,
		Description: description,
	}
	_, err := engine.Insert(group)
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
	affected, err := engine.Where("name = ?", name).Delete(&Group{})
	if err != nil {
		return fmt.Errorf("删除组失败: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("组 %s 不存在", name)
	}
	return nil
}

// ListGroups 列出所有组及其统计
func ListGroups() ([]GroupInfo, error) {
	if err := ensureDB(); err != nil {
		return nil, err
	}
	rows, err := engine.DB().Query(`SELECT g.id, g.name, g.parent_id, g.source, g.description,
    (SELECT COUNT(*) FROM user_groups ug WHERE ug.group_id = g.id) AS member_count,
    (SELECT COUNT(*) FROM group_skills gs WHERE gs.group_id = g.id) AS skill_count
    FROM groups g ORDER BY g.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []GroupInfo
	for rows.Next() {
		var group GroupInfo
		var parentID sql.NullInt64
		if err := rows.Scan(
			&group.ID,
			&group.Name,
			&parentID,
			&group.Source,
			&group.Description,
			&group.MemberCount,
			&group.SkillCount,
		); err != nil {
			return nil, err
		}
		if parentID.Valid {
			group.ParentID = &parentID.Int64
		}
		list = append(list, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

// GetGroupID 根据组名获取组 ID
func GetGroupID(name string) (int64, error) {
	if err := ensureDB(); err != nil {
		return 0, err
	}
	var group Group
	has, err := engine.Where("name = ?", name).Get(&group)
	if err != nil {
		return 0, err
	}
	if !has {
		return 0, fmt.Errorf("组 %s 不存在", name)
	}
	return group.ID, nil
}

// AddUsersToGroup 批量添加用户到组
func AddUsersToGroup(groupName string, usernames []string) error {
	gid, err := GetGroupID(groupName)
	if err != nil {
		return err
	}
	for _, u := range usernames {
		// INSERT OR IGNORE 避免重复插入
		engine.Exec("INSERT OR IGNORE INTO user_groups (username, group_id) VALUES (?, ?)", u, gid)
	}
	return nil
}

// RemoveUserFromGroup 从组移除用户
func RemoveUserFromGroup(groupName, username string) error {
	gid, err := GetGroupID(groupName)
	if err != nil {
		return err
	}
	_, err = engine.Where("group_id = ? AND username = ?", gid, username).Delete(&UserGroup{})
	return err
}

// GetGroupMembers 获取组成员列表
func GetGroupMembers(groupName string) ([]string, error) {
	gid, err := GetGroupID(groupName)
	if err != nil {
		return nil, err
	}
	var userGroups []UserGroup
	err = engine.Where("group_id = ?", gid).OrderBy("username").Find(&userGroups)
	if err != nil {
		return nil, err
	}
	list := make([]string, 0, len(userGroups))
	for _, ug := range userGroups {
		list = append(list, ug.Username)
	}
	return list, nil
}

// GetGroupMembersWithSubGroups 获取组的直接成员和子组成员。
func GetGroupMembersWithSubGroups(groupName string) ([]string, []string, error) {
	directMembers, err := GetGroupMembers(groupName)
	if err != nil {
		return nil, nil, err
	}
	allMembers, err := GetGroupMembersForDeploy(groupName)
	if err != nil {
		return nil, nil, err
	}
	direct := make(map[string]bool, len(directMembers))
	for _, member := range directMembers {
		direct[member] = true
	}
	var inherited []string
	for _, member := range allMembers {
		if !direct[member] {
			inherited = append(inherited, member)
		}
	}
	sort.Strings(inherited)
	return directMembers, inherited, nil
}

// GetGroupsForUser 获取用户所属的组名列表
func GetGroupsForUser(username string) ([]string, error) {
	if err := ensureDB(); err != nil {
		return nil, err
	}
	var results []struct {
		Name string `xorm:"name"`
	}
	err := engine.SQL(`SELECT g.name FROM groups g JOIN user_groups ug ON g.id = ug.group_id WHERE ug.username = ? ORDER BY g.name`, username).Find(&results)
	if err != nil {
		return nil, err
	}
	list := make([]string, 0, len(results))
	for _, r := range results {
		list = append(list, r.Name)
	}
	return list, nil
}

// SyncUserGroups 差量更新用户的组关系（传入用户应属于的组名列表）
func SyncUserGroups(username string, groupNames []string) error {
	if err := ensureDB(); err != nil {
		return err
	}
	session := engine.NewSession()
	defer session.Close()

	if err := session.Begin(); err != nil {
		return err
	}

	// 确保所有组存在
	for _, name := range groupNames {
		session.Exec("INSERT OR IGNORE INTO groups (name, source) VALUES (?, 'ldap')", name)
	}

	// 删除用户当前所有组关系
	session.Where("username = ?", username).Delete(&UserGroup{})

	// 添加新的组关系
	for _, name := range groupNames {
		var group Group
		has, err := session.Where("name = ?", name).Get(&group)
		if err != nil || !has {
			continue
		}
		session.Exec("INSERT OR IGNORE INTO user_groups (username, group_id) VALUES (?, ?)", username, group.ID)
	}

	return session.Commit()
}

// BindSkillToGroup 绑定技能到组
func BindSkillToGroup(groupName, skillName string) error {
	gid, err := GetGroupID(groupName)
	if err != nil {
		return err
	}
	_, err = engine.Exec("INSERT OR IGNORE INTO group_skills (group_id, skill_name) VALUES (?, ?)", gid, skillName)
	return err
}

// UnbindSkillFromGroup 解绑技能
func UnbindSkillFromGroup(groupName, skillName string) error {
	gid, err := GetGroupID(groupName)
	if err != nil {
		return err
	}
	_, err = engine.Where("group_id = ? AND skill_name = ?", gid, skillName).Delete(&GroupSkill{})
	return err
}

// GetGroupSkills 获取组绑定的技能列表
func GetGroupSkills(groupName string) ([]string, error) {
	gid, err := GetGroupID(groupName)
	if err != nil {
		return nil, err
	}
	var skills []GroupSkill
	err = engine.Where("group_id = ?", gid).OrderBy("skill_name").Find(&skills)
	if err != nil {
		return nil, err
	}
	list := make([]string, 0, len(skills))
	for _, s := range skills {
		list = append(list, s.SkillName)
	}
	return list, nil
}

// GetGroupMembersForDeploy 获取组成员的用户名列表（包含子组成员）。
func GetGroupMembersForDeploy(groupName string) ([]string, error) {
	if err := ensureDB(); err != nil {
		return nil, err
	}

	var group Group
	has, err := engine.Where("name = ?", groupName).Get(&group)
	if err != nil || !has {
		return nil, fmt.Errorf("组 %s 不存在", groupName)
	}

	ids := []int64{group.ID}
	subIDs, err := GetSubGroupIDs(group.ID)
	if err != nil {
		return nil, err
	}
	ids = append(ids, subIDs...)

	seen := make(map[string]bool)
	var members []string
	for _, gid := range ids {
		var userGroups []UserGroup
		if err := engine.Where("group_id = ?", gid).OrderBy("username").Find(&userGroups); err != nil {
			return nil, err
		}
		for _, ug := range userGroups {
			if !seen[ug.Username] {
				seen[ug.Username] = true
				members = append(members, ug.Username)
			}
		}
	}
	return members, nil
}

// GetSubGroupIDs 递归获取所有子组 ID。
func GetSubGroupIDs(groupID int64) ([]int64, error) {
	if err := ensureDB(); err != nil {
		return nil, err
	}
	var result []int64
	var walk func(pid int64) error
	walk = func(pid int64) error {
		var children []Group
		if err := engine.Where("parent_id = ?", pid).OrderBy("name").Find(&children); err != nil {
			return err
		}
		for _, child := range children {
			result = append(result, child.ID)
			if err := walk(child.ID); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(groupID); err != nil {
		return nil, err
	}
	return result, nil
}

// SetGroupParent 设置组的父组。
func SetGroupParent(groupName string, parentID *int64) error {
	if err := ensureDB(); err != nil {
		return err
	}
	_, err := engine.Where("name = ?", groupName).
		Cols("parent_id").
		Update(&Group{ParentID: parentID})
	return err
}

// GetGroupIDByName 根据组名获取 ID
func GetGroupIDByName(name string) (int64, error) {
	if err := ensureDB(); err != nil {
		return 0, err
	}
	var group Group
	has, err := engine.Where("name = ?", name).Get(&group)
	if err != nil {
		return 0, err
	}
	if !has {
		return 0, fmt.Errorf("组 %s 不存在", name)
	}
	return group.ID, nil
}

// ============================================================
// MCP Token 管理
// ============================================================

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
	affected, err := engine.Where("username = ?", username).
		Cols("mcp_token").
		Update(&ContainerRecord{MCPToken: token})
	if err != nil {
		return "", fmt.Errorf("保存 MCP token 失败: %w", err)
	}
	if affected == 0 {
		// 无匹配行，执行 INSERT
		_, err = engine.Insert(&ContainerRecord{
			Username: username,
			Image:    "",
			Status:   "stopped",
			MCPToken: token,
		})
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
	var rec ContainerRecord
	has, err := engine.Where("username = ?", username).Get(&rec)
	if err != nil {
		return "", err
	}
	if !has {
		return "", nil
	}
	return rec.MCPToken, nil
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

// ensure interface compatibility: core.DB embeds *sql.DB
var _ = func() *sql.DB {
	var e *xorm.Engine
	if e != nil {
		return e.DB().DB
	}
	return nil
}
