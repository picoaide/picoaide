package skill

import (
  "os"
  "path/filepath"
  "strings"
  "testing"
)

// ============================================================
// ParseMetadata 测试
// ============================================================

func writeSKILL(t *testing.T, dir, content string) {
  t.Helper()
  os.MkdirAll(dir, 0755)
  os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644)
}

func TestParseMetadata_Valid(t *testing.T) {
  dir := t.TempDir()
  writeSKILL(t, dir, "---\nname: my-skill\ndescription: A test skill\n---\n# Content\n")
  meta, err := ParseMetadata(dir)
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if meta.Name != "my-skill" {
    t.Errorf("name = %q, want %q", meta.Name, "my-skill")
  }
  if meta.Description != "A test skill" {
    t.Errorf("description = %q, want %q", meta.Description, "A test skill")
  }
}

func TestParseMetadata_ValidWithWindowsCRLF(t *testing.T) {
  dir := t.TempDir()
  writeSKILL(t, dir, "---\r\nname: win-skill\r\ndescription: Windows line endings\r\n---\r\n# Content\r\n")
  meta, err := ParseMetadata(dir)
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if meta.Name != "win-skill" {
    t.Errorf("name = %q, want %q", meta.Name, "win-skill")
  }
}

func TestParseMetadata_MissingFile(t *testing.T) {
  dir := t.TempDir()
  _, err := ParseMetadata(dir)
  if err == nil {
    t.Fatal("expected error for missing SKILL.md")
  }
}

func TestParseMetadata_NoFrontmatter(t *testing.T) {
  dir := t.TempDir()
  writeSKILL(t, dir, "# Just content\n")
  _, err := ParseMetadata(dir)
  if err == nil || !strings.Contains(err.Error(), "YAML frontmatter") {
    t.Fatalf("expected frontmatter error, got: %v", err)
  }
}

func TestParseMetadata_NoEndDelimiter(t *testing.T) {
  dir := t.TempDir()
  // content with only opening --- but no closing ---
  writeSKILL(t, dir, "---\nname: test\ndescription: desc\n")
  _, err := ParseMetadata(dir)
  if err == nil || !strings.Contains(err.Error(), "结束") {
    t.Fatalf("expected closing delimiter error, got: %v", err)
  }
}

func TestParseMetadata_InvalidYAML(t *testing.T) {
  dir := t.TempDir()
  writeSKILL(t, dir, "---\nname: [invalid yaml\n---\n#\n")
  _, err := ParseMetadata(dir)
  if err == nil {
    t.Fatal("expected error for invalid YAML")
  }
}

func TestParseMetadata_MissingName(t *testing.T) {
  dir := t.TempDir()
  writeSKILL(t, dir, "---\ndescription: no name here\n---\n#\n")
  _, err := ParseMetadata(dir)
  if err == nil || !strings.Contains(err.Error(), "name") {
    t.Fatalf("expected missing name error, got: %v", err)
  }
}

func TestParseMetadata_MissingDescription(t *testing.T) {
  dir := t.TempDir()
  writeSKILL(t, dir, "---\nname: test\n---\n#\n")
  _, err := ParseMetadata(dir)
  if err == nil || !strings.Contains(err.Error(), "description") {
    t.Fatalf("expected missing description error, got: %v", err)
  }
}

// ============================================================
// ValidateName 测试
// ============================================================

func TestValidateName_Valid(t *testing.T) {
  valid := []string{"a", "z", "my-skill", "skill123", "a-b-c", "abc"}
  for _, name := range valid {
    if err := ValidateName(name); err != nil {
      t.Errorf("ValidateName(%q) = %v, want nil", name, err)
    }
  }
}

func TestValidateName_Invalid(t *testing.T) {
  tests := []struct {
    name string
    desc string
  }{
    {"", "empty"},
    {"My-Skill", "uppercase"},
    {"-skill", "leading hyphen"},
    {"skill-", "trailing hyphen"},
    {"a-b-", "trailing hyphen 2"},
    {"a b", "space"},
    {"skill_name", "underscore"},
    {"skill.name", "dot"},
    {"123abc", "leading digit"},
    {"abcdefghijklmnopqrstuvwxyz-abcdefghijklmnopqrstuvwxyz-abcdefghijklm", "too long (64+)"},
  }
  for _, tt := range tests {
    t.Run(tt.desc, func(t *testing.T) {
      if err := ValidateName(tt.name); err == nil {
        t.Errorf("ValidateName(%q) = nil, want error", tt.name)
      }
    })
  }
}



// ============================================================
// ValidateMetadata 测试
// ============================================================

func TestValidateMetadata_Success(t *testing.T) {
  meta := &Metadata{Name: "my-skill", Description: "A skill"}
  if err := ValidateMetadata(meta, "my-skill"); err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
}

func TestValidateMetadata_NameMismatch(t *testing.T) {
  meta := &Metadata{Name: "skill-a", Description: "test"}
  err := ValidateMetadata(meta, "skill-b")
  if err == nil || !strings.Contains(err.Error(), "不一致") {
    t.Fatalf("expected name mismatch error, got: %v", err)
  }
}

func TestValidateMetadata_DescriptionTooLong(t *testing.T) {
  desc := make([]byte, 1025)
  for i := range desc {
    desc[i] = 'a'
  }
  meta := &Metadata{Name: "my-skill", Description: string(desc)}
  err := ValidateMetadata(meta, "my-skill")
  if err == nil || !strings.Contains(err.Error(), "description") {
    t.Fatalf("expected description too long error, got: %v", err)
  }
}

// ============================================================
// ParseAndValidate 集成测试
// ============================================================

func TestParseAndValidate_Success(t *testing.T) {
  dir := t.TempDir()
  skillDir := filepath.Join(dir, "my-skill")
  os.MkdirAll(skillDir, 0755)
  writeSKILL(t, skillDir, "---\nname: my-skill\ndescription: Integrated test\n---\n#\n")
  meta, err := ParseAndValidate(skillDir)
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if meta.Name != "my-skill" {
    t.Errorf("name = %q, want %q", meta.Name, "my-skill")
  }
}

func TestParseAndValidate_MissingFile(t *testing.T) {
  dir := t.TempDir()
  _, err := ParseAndValidate(dir)
  if err == nil {
    t.Fatal("expected error")
  }
}

func TestParseAndValidate_NameMismatch(t *testing.T) {
  dir := t.TempDir()
  writeSKILL(t, dir, "---\nname: other-name\ndescription: mismatch\n---\n#\n")
  _, err := ParseAndValidate(dir)
  if err == nil || !strings.Contains(err.Error(), "不一致") {
    t.Fatalf("expected name mismatch error, got: %v", err)
  }
}

// ============================================================
// formatSize 测试
// ============================================================

func TestFormatSize(t *testing.T) {
  tests := []struct {
    bytes int64
    want  string
  }{
    {0, "0 B"},
    {1, "1 B"},
    {1023, "1023 B"},
    {1024, "1.0 KB"},
    {1536, "1.5 KB"},
    {1048576, "1.0 MB"},
    {2097152, "2.0 MB"},
    {10485760, "10.0 MB"},
  }
  for _, tt := range tests {
    got := formatSize(tt.bytes)
    if got != tt.want {
      t.Errorf("formatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
    }
  }
}


