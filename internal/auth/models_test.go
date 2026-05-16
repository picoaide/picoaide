package auth

import (
  "testing"
)

func TestModelTableNames(t *testing.T) {
  tests := []struct {
    model    interface{ TableName() string }
    expected string
  }{
    {LocalUser{}, "local_users"},
    {ContainerRecord{}, "containers"},
    {Setting{}, "settings"},
    {SettingsHistory{}, "settings_history"},
    {WhitelistEntry{}, "whitelist"},
    {Group{}, "groups"},
    {UserGroup{}, "user_groups"},
    {UserChannel{}, "user_channels"},
    {SharedFolder{}, "shared_folders"},
    {SharedFolderGroup{}, "shared_folder_groups"},
    {SharedFolderMount{}, "shared_folder_mounts"},
    {SkillRecord{}, "skills"},
    {UserSkill{}, "user_skills"},
    {UserCookie{}, "user_cookies"},
    {PicoclawAdapterPackage{}, "picoclaw_adapter_packages"},
  }
  for _, tc := range tests {
    if got := tc.model.TableName(); got != tc.expected {
      t.Errorf("%T TableName() = %q, want %q", tc.model, got, tc.expected)
    }
  }
}
