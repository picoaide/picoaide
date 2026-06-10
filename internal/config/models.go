package config

// Setting 系统设置表（与 store.Setting 结构一致）
type Setting struct {
  Key       string `xorm:"pk 'key'"`
  Value     string `xorm:"notnull 'value'"`
  UpdatedAt string `xorm:"notnull 'updated_at'"`
}

func (Setting) TableName() string { return "settings" }

// SettingsHistory 设置变更历史表（与 store.SettingsHistory 结构一致）
type SettingsHistory struct {
  ID        int64  `xorm:"pk autoincr 'id'"`
  Key       string `xorm:"notnull 'key'"`
  OldValue  string `xorm:"'old_value'"`
  NewValue  string `xorm:"'new_value'"`
  ChangedBy string `xorm:"notnull 'changed_by'"`
  ChangedAt string `xorm:"notnull 'changed_at'"`
}

func (SettingsHistory) TableName() string { return "settings_history" }
