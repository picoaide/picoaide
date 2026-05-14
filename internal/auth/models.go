package auth

// ============================================================
// ORM 模型定义
// ============================================================

// LocalUser 本地用户表
type LocalUser struct {
  ID           int64  `xorm:"pk autoincr 'id'"`
  Username     string `xorm:"unique notnull 'username'"`
  PasswordHash string `xorm:"notnull 'password_hash'"`
  Role         string `xorm:"notnull 'role'"`
  Source       string `xorm:"notnull 'source'"`
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

// SharedFolder 共享文件夹
type SharedFolder struct {
  ID          int64  `xorm:"pk autoincr 'id'" json:"id"`
  Name        string `xorm:"unique notnull 'name'" json:"name"`
  Description string `xorm:"notnull 'description'" json:"description"`
  IsPublic    bool   `xorm:"notnull 'is_public'" json:"is_public"`
  CreatedBy   string `xorm:"notnull 'created_by'" json:"created_by"`
  CreatedAt   string `xorm:"notnull 'created_at'" json:"created_at"`
  UpdatedAt   string `xorm:"notnull 'updated_at'" json:"updated_at"`
}

func (SharedFolder) TableName() string {
  return "shared_folders"
}

// SharedFolderGroup 共享文件夹—用户组关联
type SharedFolderGroup struct {
  ID       int64 `xorm:"pk autoincr 'id'" json:"id"`
  FolderID int64 `xorm:"notnull unique(folder_group) 'folder_id'" json:"folder_id"`
  GroupID  int64 `xorm:"notnull unique(folder_group) 'group_id'" json:"group_id"`
}

func (SharedFolderGroup) TableName() string {
  return "shared_folder_groups"
}

// SharedFolderMount 共享文件夹挂载状态（缓存）
type SharedFolderMount struct {
  ID        int64  `xorm:"pk autoincr 'id'" json:"id"`
  FolderID  int64  `xorm:"notnull unique(folder_user) 'folder_id'" json:"folder_id"`
  Username  string `xorm:"notnull unique(folder_user) 'username'" json:"username"`
  Mounted   bool   `xorm:"notnull 'mounted'" json:"mounted"`
  CheckedAt string `xorm:"notnull 'checked_at'" json:"checked_at"`
}

func (SharedFolderMount) TableName() string {
  return "shared_folder_mounts"
}

// ShareMount 共享文件夹挂载规范（auth 包内部结构，供 web 层转换为 docker.Mount）
type ShareMount struct {
  Source string
  Target string
}

// SkillRecord 技能元数据表
type SkillRecord struct {
  ID          int64  `xorm:"pk autoincr 'id'"`
  Name        string `xorm:"unique notnull 'name'"`
  Description string `xorm:"notnull 'description'"`
  UpdatedAt   string `xorm:"notnull 'updated_at'"`
}

func (SkillRecord) TableName() string {
  return "skills"
}

// UserSkill 用户-技能直接绑定表
type UserSkill struct {
  ID        int64  `xorm:"pk autoincr 'id'"`
  Username  string `xorm:"notnull unique(username, skill_name) 'username'"`
  SkillName string `xorm:"notnull 'skill_name'"`
  Source    string `xorm:"notnull default '' 'source'"`
  UpdatedAt string `xorm:"updated 'updated_at'"`
}

func (UserSkill) TableName() string {
  return "user_skills"
}

// UserCookie 用户 Cookie 表
type UserCookie struct {
  ID        int64  `xorm:"pk autoincr 'id'"`
  Username  string `xorm:"notnull unique(username_domain) 'username'"`
  Domain    string `xorm:"notnull 'domain'"`
  Cookies   string `xorm:"notnull 'cookies'"`
  UpdatedAt string `xorm:"notnull 'updated_at'"`
}

func (UserCookie) TableName() string {
  return "user_cookies"
}

// CookieEntry 前端展示用的 Cookie 元数据
type CookieEntry struct {
  Domain    string `json:"domain"`
  Cookies   string `json:"-"` // 不暴露给前端
  UpdatedAt string `json:"updated_at"`
}
// GroupInfo 组信息（包含成员数），非数据库模型，仅用于查询结果
type GroupInfo struct {
  ID          int64  `json:"id"`
  Name        string `json:"name"`
  ParentID    *int64 `json:"parent_id"`
  Source      string `json:"source"`
  Description string `json:"description"`
  MemberCount int    `json:"member_count"`
}
