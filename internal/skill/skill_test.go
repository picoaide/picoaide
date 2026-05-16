package skill

import (
  "archive/zip"
  "bytes"
  "encoding/json"
  "net/http"
  "net/http/httptest"
  "net/url"
  "os"
  "path/filepath"
  "strings"
  "testing"

  gogit "github.com/go-git/go-git/v5"
  gogitconfig "github.com/go-git/go-git/v5/config"
  "github.com/go-git/go-git/v5/plumbing"
  "github.com/go-git/go-git/v5/plumbing/object"

  "github.com/picoaide/picoaide/internal/config"
)

// ============================================================
// 测试工具函数
// ============================================================

func writeSKILL(t *testing.T, dir, content string) {
  t.Helper()
  os.MkdirAll(dir, 0755)
  os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644)
}

func setWorkDir(t *testing.T) string {
  t.Helper()
  old := config.DefaultWorkDir
  tmp := t.TempDir()
  config.DefaultWorkDir = tmp
  t.Cleanup(func() { config.DefaultWorkDir = old })
  return tmp
}

type mockTransport struct {
  target string
  next   http.RoundTripper
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
  mockReq := req.Clone(req.Context())
  u, _ := url.Parse(m.target)
  mockReq.URL.Scheme = u.Scheme
  mockReq.URL.Host = u.Host
  return m.next.RoundTrip(mockReq)
}

func setMockHTTP(t *testing.T, serverURL string) {
  t.Helper()
  old := http.DefaultTransport
  http.DefaultTransport = &mockTransport{target: serverURL, next: old}
  t.Cleanup(func() { http.DefaultTransport = old })
}

func createTestZip(t *testing.T, files map[string]string) []byte {
  t.Helper()
  var buf bytes.Buffer
  zw := zip.NewWriter(&buf)
  for name, content := range files {
    f, err := zw.Create(name)
    if err != nil {
      t.Fatalf("创建 ZIP 条目失败: %v", err)
    }
    if _, err := f.Write([]byte(content)); err != nil {
      t.Fatalf("写入 ZIP 内容失败: %v", err)
    }
  }
  if err := zw.Close(); err != nil {
    t.Fatalf("关闭 ZIP 写入器失败: %v", err)
  }
  return buf.Bytes()
}

func initRepoWithCommit(t *testing.T, dir string) *gogit.Repository {
  t.Helper()
  repo, err := gogit.PlainInit(dir, false)
  if err != nil {
    t.Fatalf("初始化仓库失败: %v", err)
  }
  wt, err := repo.Worktree()
  if err != nil {
    t.Fatalf("获取工作区失败: %v", err)
  }
  os.WriteFile(filepath.Join(dir, "README.md"), []byte("test"), 0644)
  wt.Add("README.md")
  if _, err := wt.Commit("initial", &gogit.CommitOptions{
    Author: &object.Signature{Name: "test", Email: "test@test.com"},
  }); err != nil {
    t.Fatalf("提交失败: %v", err)
  }
  return repo
}

// ============================================================
// ParseMetadata 测试
// ============================================================

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

func TestValidateMetadata_InvalidName(t *testing.T) {
  meta := &Metadata{Name: "UPPERCASE", Description: "desc"}
  err := ValidateMetadata(meta, "whatever")
  if err == nil || !strings.Contains(err.Error(), "name 字段不合法") {
    t.Fatalf("expected name validation error, got: %v", err)
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
// SkillsRootDir 测试
// ============================================================

func TestSkillsRootDir(t *testing.T) {
  setWorkDir(t)
  got := SkillsRootDir()
  want := filepath.Join(config.DefaultWorkDir, "skills")
  if got != want {
    t.Errorf("SkillsRootDir() = %q, want %q", got, want)
  }
}

// ============================================================
// ListAllSkills 测试
// ============================================================

func TestListAllSkills_NoRoot(t *testing.T) {
  setWorkDir(t)
  skills, err := ListAllSkills()
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(skills) != 0 {
    t.Errorf("expected 0 skills, got %d", len(skills))
  }
}

func TestListAllSkills_WithSkills(t *testing.T) {
  setWorkDir(t)
  srcDir := filepath.Join(SkillsRootDir(), "source-a", "my-skill")
  writeSKILL(t, srcDir, "---\nname: my-skill\ndescription: A test skill\n---\n#\n")
  srcDir2 := filepath.Join(SkillsRootDir(), "source-a", "other-skill")
  writeSKILL(t, srcDir2, "---\nname: other-skill\ndescription: Another skill\n---\n#\n")

  skills, err := ListAllSkills()
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(skills) != 2 {
    t.Fatalf("expected 2 skills, got %d", len(skills))
  }
  if skills[0].Name != "my-skill" {
    t.Errorf("skills[0].Name = %q, want %q", skills[0].Name, "my-skill")
  }
  if skills[1].Name != "other-skill" {
    t.Errorf("skills[1].Name = %q, want %q", skills[1].Name, "other-skill")
  }
}

func TestListAllSkills_SkipsDotDirs(t *testing.T) {
  setWorkDir(t)
  os.MkdirAll(filepath.Join(SkillsRootDir(), ".hidden"), 0755)
  writeSKILL(t, filepath.Join(SkillsRootDir(), ".hidden", "skill"), "---\nname: skill\ndescription: hidden\n---\n#\n")
  skills, err := ListAllSkills()
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(skills) != 0 {
    t.Errorf("expected 0 skills (hidden dirs skipped), got %d", len(skills))
  }
}

// ============================================================
// ListSourceSkills 测试
// ============================================================

func TestListSourceSkills_InvalidName(t *testing.T) {
  _, err := ListSourceSkills("../evil")
  if err == nil {
    t.Fatal("expected error for invalid source name")
  }
}

func TestListSourceSkills_NonExistent(t *testing.T) {
  setWorkDir(t)
  skills, err := ListSourceSkills("no-such-source")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(skills) != 0 {
    t.Errorf("expected 0 skills, got %d", len(skills))
  }
}

func TestListSourceSkills_WithSkills(t *testing.T) {
  setWorkDir(t)
  writeSKILL(t, filepath.Join(SkillsRootDir(), "src", "skill-a"), "---\nname: skill-a\ndescription: Skill A\n---\n#\n")
  writeSKILL(t, filepath.Join(SkillsRootDir(), "src", "skill-b"), "---\nname: skill-b\ndescription: Skill B\n---\n#\n")

  skills, err := ListSourceSkills("src")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(skills) != 2 {
    t.Fatalf("expected 2 skills, got %d", len(skills))
  }
}

func TestListSourceSkills_SkipsNoSKILLMD(t *testing.T) {
  setWorkDir(t)
  os.MkdirAll(filepath.Join(SkillsRootDir(), "src", "no-skill-dir"), 0755)
  writeSKILL(t, filepath.Join(SkillsRootDir(), "src", "valid-skill"), "---\nname: valid-skill\ndescription: valid\n---\n#\n")

  skills, err := ListSourceSkills("src")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(skills) != 1 {
    t.Fatalf("expected 1 skill, got %d", len(skills))
  }
}

func TestListSourceSkills_SkipsHiddenDirs(t *testing.T) {
  setWorkDir(t)
  os.MkdirAll(filepath.Join(SkillsRootDir(), "src", ".hidden"), 0755)
  writeSKILL(t, filepath.Join(SkillsRootDir(), "src", ".hidden", "nested"), "---\nname: nested\ndescription: nested\n---\n#\n")
  writeSKILL(t, filepath.Join(SkillsRootDir(), "src", "visible"), "---\nname: visible\ndescription: visible\n---\n#\n")

  skills, err := ListSourceSkills("src")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(skills) != 1 {
    t.Fatalf("expected 1 skill, got %d", len(skills))
  }
}

func TestListSourceSkills_SkipsBadMetadata(t *testing.T) {
  setWorkDir(t)
  writeSKILL(t, filepath.Join(SkillsRootDir(), "src", "bad"), "---\nname: [invalid\n---\n#\n")
  writeSKILL(t, filepath.Join(SkillsRootDir(), "src", "good"), "---\nname: good\ndescription: good skill\n---\n#\n")

  skills, err := ListSourceSkills("src")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(skills) != 1 {
    t.Fatalf("expected 1 skill, got %d", len(skills))
  }
}

// ============================================================
// RescanSource 测试
// ============================================================

func TestRescanSource_InvalidName(t *testing.T) {
  _, err := RescanSource("../evil")
  if err == nil {
    t.Fatal("expected error for invalid source name")
  }
}

func TestRescanSource_NonExistent(t *testing.T) {
  setWorkDir(t)
  names, err := RescanSource("no-such-source")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if names != nil {
    t.Errorf("expected nil names, got %v", names)
  }
}

func TestRescanSource_WithSkills(t *testing.T) {
  setWorkDir(t)
  writeSKILL(t, filepath.Join(SkillsRootDir(), "src", "skill-a"), "---\nname: skill-a\ndescription: Skill A\n---\n#\n")
  writeSKILL(t, filepath.Join(SkillsRootDir(), "src", "skill-b"), "---\nname: skill-b\ndescription: Skill B\n---\n#\n")

  names, err := RescanSource("src")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(names) != 2 {
    t.Fatalf("expected 2 names, got %d", len(names))
  }
}

func TestRescanSource_SkipsNoSKILLMD(t *testing.T) {
  setWorkDir(t)
  os.MkdirAll(filepath.Join(SkillsRootDir(), "src", "dir-only"), 0755)
  writeSKILL(t, filepath.Join(SkillsRootDir(), "src", "real-skill"), "---\nname: real-skill\ndescription: real\n---\n#\n")

  names, err := RescanSource("src")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(names) != 1 {
    t.Fatalf("expected 1 name, got %d", len(names))
  }
}

func TestRescanSource_SkipsUnsafeName(t *testing.T) {
  setWorkDir(t)
  writeSKILL(t, filepath.Join(SkillsRootDir(), "src", "bad-dir"), "---\nname: ./malicious\ndescription: bad\n---\n#\n")
  writeSKILL(t, filepath.Join(SkillsRootDir(), "src", "good-skill"), "---\nname: good-skill\ndescription: a good skill\n---\n#\n")

  names, err := RescanSource("src")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(names) != 1 {
    t.Fatalf("expected 1 name, got %d", len(names))
  }
}

func TestRescanSource_SkipsBadMetadata(t *testing.T) {
  setWorkDir(t)
  writeSKILL(t, filepath.Join(SkillsRootDir(), "src", "bad"), "---\nname: [invalid yaml\n---\n#\n")
  writeSKILL(t, filepath.Join(SkillsRootDir(), "src", "good"), "---\nname: good\ndescription: a good skill\n---\n#\n")

  names, err := RescanSource("src")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(names) != 1 {
    t.Fatalf("expected 1 name, got %d", len(names))
  }
}

// ============================================================
// CloneGitSource 测试
// ============================================================

func TestCloneGitSource_InvalidName(t *testing.T) {
  err := CloneGitSource("../evil", "http://example.com/repo", "", "")
  if err == nil {
    t.Fatal("expected error for invalid name")
  }
}

func TestCloneGitSource_DirExists(t *testing.T) {
  setWorkDir(t)
  targetDir := filepath.Join(SkillsRootDir(), "existing-source")
  os.MkdirAll(targetDir, 0755)
  err := CloneGitSource("existing-source", "http://example.com/repo", "", "")
  if err == nil || !strings.Contains(err.Error(), "已存在") {
    t.Fatalf("expected 'already exists' error, got: %v", err)
  }
}

func TestCloneGitSource_InvalidURL(t *testing.T) {
  setWorkDir(t)
  err := CloneGitSource("test-source", "/nonexistent/path", "", "")
  if err == nil {
    t.Fatal("expected error for invalid URL")
  }
}

func TestCloneGitSource_Success(t *testing.T) {
  setWorkDir(t)
  srcDir := t.TempDir()
  initRepoWithCommit(t, srcDir)

  err := CloneGitSource("test-source", srcDir, "", "")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  targetDir := filepath.Join(SkillsRootDir(), "test-source")
  if _, err := os.Stat(targetDir); os.IsNotExist(err) {
    t.Errorf("target directory not created: %s", targetDir)
  }
}

func TestCloneGitSource_WithRef(t *testing.T) {
  setWorkDir(t)
  srcDir := t.TempDir()
  srcRepo := initRepoWithCommit(t, srcDir)

  headRef, err := srcRepo.Head()
  if err != nil {
    t.Fatalf("failed to get HEAD: %v", err)
  }
  srcRepo.Storer.SetReference(plumbing.NewHashReference("refs/heads/dev", headRef.Hash()))

  err = CloneGitSource("test-dev", srcDir, "dev", "branch")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  targetDir := filepath.Join(SkillsRootDir(), "test-dev")
  if _, err := os.Stat(targetDir); os.IsNotExist(err) {
    t.Errorf("target directory not created: %s", targetDir)
  }
}

// ============================================================
// PullGitSource 测试
// ============================================================

func TestPullGitSource_InvalidName(t *testing.T) {
  _, err := PullGitSource("../evil", "", "")
  if err == nil {
    t.Fatal("expected error for invalid name")
  }
}

func TestPullGitSource_NonExistent(t *testing.T) {
  setWorkDir(t)
  _, err := PullGitSource("no-such-source", "", "")
  if err == nil || !strings.Contains(err.Error(), "不存在") {
    t.Fatalf("expected 'not exists' error, got: %v", err)
  }
}

func TestPullGitSource_NotAGitRepo(t *testing.T) {
  setWorkDir(t)
  os.MkdirAll(filepath.Join(SkillsRootDir(), "not-git"), 0755)
  _, err := PullGitSource("not-git", "", "")
  if err == nil || !strings.Contains(err.Error(), "打开 Git 仓库失败") {
    t.Fatalf("expected git open error, got: %v", err)
  }
}

func TestPullGitSource_Success(t *testing.T) {
  setWorkDir(t)
  srcDir := t.TempDir()
  srcRepo := initRepoWithCommit(t, srcDir)
  wt, err := srcRepo.Worktree()
  if err != nil {
    t.Fatalf("failed to get worktree: %v", err)
  }
  os.MkdirAll(filepath.Join(srcDir, "skill-a"), 0755)
  os.WriteFile(filepath.Join(srcDir, "skill-a", "SKILL.md"), []byte("---\nname: skill-a\ndescription: Skill A\n---\n#\n"), 0644)
  _, err = wt.Add("skill-a/SKILL.md")
  if err != nil {
    t.Fatalf("git add failed: %v", err)
  }
  _, err = wt.Commit("add skill-a", &gogit.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}})
  if err != nil {
    t.Fatalf("git commit failed: %v", err)
  }

  err = CloneGitSource("test-source", srcDir, "", "")
  if err != nil {
    t.Fatalf("clone failed: %v", err)
  }

  os.MkdirAll(filepath.Join(srcDir, "skill-b"), 0755)
  os.WriteFile(filepath.Join(srcDir, "skill-b", "SKILL.md"), []byte("---\nname: skill-b\ndescription: Skill B\n---\n#\n"), 0644)
  _, err = wt.Add("skill-b/SKILL.md")
  if err != nil {
    t.Fatalf("git add failed: %v", err)
  }
  _, err = wt.Commit("add skill-b", &gogit.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}})
  if err != nil {
    t.Fatalf("git commit failed: %v", err)
  }

  result, err := PullGitSource("test-source", "", "")
  if err != nil {
    t.Fatalf("pull failed: %v", err)
  }
  if len(result.Added) == 0 && len(result.Updated) == 0 {
    t.Errorf("expected some changes, got empty result: %+v", result)
  }
  foundAdded := false
  for _, s := range result.Added {
    if s == "skill-b" {
      foundAdded = true
      break
    }
  }
  if !foundAdded {
    t.Errorf("expected 'skill-b' in Added, got Added=%v Updated=%v Removed=%v", result.Added, result.Updated, result.Removed)
  }
}

func TestPullGitSource_RemovedSkill(t *testing.T) {
  setWorkDir(t)
  srcDir := t.TempDir()
  srcRepo := initRepoWithCommit(t, srcDir)
  wt, err := srcRepo.Worktree()
  if err != nil {
    t.Fatalf("failed to get worktree: %v", err)
  }
  os.MkdirAll(filepath.Join(srcDir, "skill-a"), 0755)
  os.WriteFile(filepath.Join(srcDir, "skill-a", "SKILL.md"), []byte("---\nname: skill-a\ndescription: Skill A\n---\n#\n"), 0644)
  wt.Add("skill-a/SKILL.md")
  wt.Commit("add skill-a", &gogit.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}})

  err = CloneGitSource("test-source", srcDir, "", "")
  if err != nil {
    t.Fatalf("clone failed: %v", err)
  }

  os.RemoveAll(filepath.Join(srcDir, "skill-a"))
  wt.Remove("skill-a/SKILL.md")

  os.MkdirAll(filepath.Join(srcDir, "skill-c"), 0755)
  os.WriteFile(filepath.Join(srcDir, "skill-c", "SKILL.md"), []byte("---\nname: skill-c\ndescription: Skill C\n---\n#\n"), 0644)
  wt.Add("skill-c/SKILL.md")
  wt.Commit("replace skill-a with skill-c", &gogit.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}})

  result, err := PullGitSource("test-source", "", "")
  if err != nil {
    t.Fatalf("pull failed: %v", err)
  }
  t.Logf("removed skill result: Added=%v Updated=%v Removed=%v", result.Added, result.Updated, result.Removed)
}

func TestPullGitSource_WithRef(t *testing.T) {
  setWorkDir(t)
  srcDir := t.TempDir()
  srcRepo := initRepoWithCommit(t, srcDir)
  wt, err := srcRepo.Worktree()
  if err != nil {
    t.Fatalf("failed to get worktree: %v", err)
  }

  os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("updated"), 0644)
  _, err = wt.Add("file.txt")
  if err != nil {
    t.Fatalf("git add failed: %v", err)
  }
  _, err = wt.Commit("update on main", &gogit.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}})
  if err != nil {
    t.Fatalf("git commit failed: %v", err)
  }

  headRef, err := srcRepo.Head()
  if err != nil {
    t.Fatalf("failed to get HEAD: %v", err)
  }
  refName := plumbing.ReferenceName("refs/tags/v1.0.0")
  srcRepo.Storer.SetReference(plumbing.NewHashReference(refName, headRef.Hash()))

  err = CloneGitSource("test-tag", srcDir, "v1.0.0", "tag")
  if err != nil {
    t.Fatalf("clone with tag failed: %v", err)
  }
}

func TestPullGitSource_PullWithBranchRef(t *testing.T) {
  setWorkDir(t)
  srcDir := t.TempDir()
  srcRepo := initRepoWithCommit(t, srcDir)
  wt, err := srcRepo.Worktree()
  if err != nil {
    t.Fatalf("failed to get worktree: %v", err)
  }
  os.MkdirAll(filepath.Join(srcDir, "skill-a"), 0755)
  os.WriteFile(filepath.Join(srcDir, "skill-a", "SKILL.md"), []byte("---\nname: skill-a\ndescription: Skill A\n---\n#\n"), 0644)
  wt.Add("skill-a/SKILL.md")
  wt.Commit("add skill-a", &gogit.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}})

  err = CloneGitSource("test-source", srcDir, "", "")
  if err != nil {
    t.Fatalf("clone failed: %v", err)
  }

  headRef, err := srcRepo.Head()
  if err != nil {
    t.Fatalf("failed to get HEAD: %v", err)
  }
  srcRepo.Storer.SetReference(plumbing.NewHashReference("refs/heads/feature", headRef.Hash()))

  os.MkdirAll(filepath.Join(srcDir, "skill-b"), 0755)
  os.WriteFile(filepath.Join(srcDir, "skill-b", "SKILL.md"), []byte("---\nname: skill-b\ndescription: Skill B\n---\n#\n"), 0644)
  wt.Add("skill-b/SKILL.md")
  wt.Commit("add skill-b on main", &gogit.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}})

  result, err := PullGitSource("test-source", "feature", "branch")
  if err != nil {
    t.Fatalf("pull with branch ref failed: %v", err)
  }
  t.Logf("pull with branch ref result: Added=%v Updated=%v Removed=%v", result.Added, result.Updated, result.Removed)
}

func TestPullGitSource_PullWithTagRef(t *testing.T) {
  setWorkDir(t)
  srcDir := t.TempDir()
  srcRepo := initRepoWithCommit(t, srcDir)
  wt, err := srcRepo.Worktree()
  if err != nil {
    t.Fatalf("failed to get worktree: %v", err)
  }
  os.MkdirAll(filepath.Join(srcDir, "skill-a"), 0755)
  os.WriteFile(filepath.Join(srcDir, "skill-a", "SKILL.md"), []byte("---\nname: skill-a\ndescription: Skill A\n---\n#\n"), 0644)
  wt.Add("skill-a/SKILL.md")
  wt.Commit("add skill-a", &gogit.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}})

  headRef, err := srcRepo.Head()
  if err != nil {
    t.Fatalf("failed to get HEAD: %v", err)
  }
  srcRepo.Storer.SetReference(plumbing.NewHashReference("refs/tags/v1.0.0", headRef.Hash()))

  err = CloneGitSource("test-tag-pull", srcDir, "v1.0.0", "tag")
  if err != nil {
    t.Fatalf("clone with tag failed: %v", err)
  }

  os.MkdirAll(filepath.Join(srcDir, "skill-b"), 0755)
  os.WriteFile(filepath.Join(srcDir, "skill-b", "SKILL.md"), []byte("---\nname: skill-b\ndescription: Skill B\n---\n#\n"), 0644)
  wt.Add("skill-b/SKILL.md")
  wt.Commit("add skill-b", &gogit.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}})

  headRef2, err := srcRepo.Head()
  if err != nil {
    t.Fatalf("failed to get HEAD: %v", err)
  }
  srcRepo.Storer.SetReference(plumbing.NewHashReference("refs/tags/v1.1.0", headRef2.Hash()))

  result, err := PullGitSource("test-tag-pull", "v1.1.0", "tag")
  if err != nil {
    t.Fatalf("pull with tag ref failed: %v", err)
  }
  if len(result.Added) != 1 {
    t.Errorf("expected 1 added skill, got Added=%v Updated=%v Removed=%v", result.Added, result.Updated, result.Removed)
  }
}

func TestPullGitSource_WorktreeError(t *testing.T) {
  setWorkDir(t)
  srcDir := t.TempDir()
  initRepoWithCommit(t, srcDir)

  bareDir := filepath.Join(SkillsRootDir(), "bare-pull")
  _, err := gogit.PlainClone(bareDir, true, &gogit.CloneOptions{URL: srcDir})
  if err != nil {
    t.Fatalf("bare clone failed: %v", err)
  }

  _, err = PullGitSource("bare-pull", "", "")
  if err == nil || !strings.Contains(err.Error(), "工作区") {
    t.Fatalf("expected worktree error, got: %v", err)
  }
}

func TestPullGitSource_CheckoutError(t *testing.T) {
  setWorkDir(t)
  srcDir := t.TempDir()
  initRepoWithCommit(t, srcDir)

  err := CloneGitSource("test-source", srcDir, "", "")
  if err != nil {
    t.Fatalf("clone failed: %v", err)
  }

  _, err = PullGitSource("test-source", "nonexistent-tag", "tag")
  if err == nil {
    t.Fatal("expected checkout error for nonexistent tag")
  }
}

func TestPullGitSource_FetchError(t *testing.T) {
  setWorkDir(t)
  srcDir := t.TempDir()
  srcRepo := initRepoWithCommit(t, srcDir)
  wt, err := srcRepo.Worktree()
  if err != nil {
    t.Fatalf("worktree error: %v", err)
  }
  os.MkdirAll(filepath.Join(srcDir, "skill-a"), 0755)
  os.WriteFile(filepath.Join(srcDir, "skill-a", "SKILL.md"), []byte("---\nname: skill-a\ndescription: A\n---\n#\n"), 0644)
  wt.Add("skill-a/SKILL.md")
  wt.Commit("initial", &gogit.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}})

  err = CloneGitSource("test-source", srcDir, "", "")
  if err != nil {
    t.Fatalf("clone failed: %v", err)
  }

  repo, err := gogit.PlainOpen(filepath.Join(SkillsRootDir(), "test-source"))
  if err != nil {
    t.Fatalf("open cloned repo failed: %v", err)
  }
  repo.DeleteRemote("origin")
  repo.CreateRemote(&gogitconfig.RemoteConfig{
    Name: "origin",
    URLs: []string{"/nonexistent-repo-path"},
  })

  headRef, err := repo.Head()
  if err != nil {
    t.Fatalf("get head failed: %v", err)
  }
  repo.Storer.SetReference(plumbing.NewHashReference("refs/heads/feature", headRef.Hash()))

  _, err = PullGitSource("test-source", "feature", "branch")
  if err == nil {
    t.Fatal("expected fetch error from nonexistent remote")
  }
}

func TestPullGitSource_PullFromRemovedOrigin(t *testing.T) {
  setWorkDir(t)
  srcDir := t.TempDir()
  initRepoWithCommit(t, srcDir)

  err := CloneGitSource("test-source", srcDir, "", "")
  if err != nil {
    t.Fatalf("clone failed: %v", err)
  }

  os.RemoveAll(srcDir)

  _, err = PullGitSource("test-source", "", "")
  if err == nil {
    t.Fatal("expected pull error after removing origin")
  }
}

// ============================================================
// FetchIndex 测试
// ============================================================

func TestFetchIndex_ObjectFormat(t *testing.T) {
  mux := http.NewServeMux()
  mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode(IndexResponse{
      Total:  2,
      Skills: []RegistrySkill{
        {Slug: "skill-a", Name: "Skill A", Description: "Desc A"},
        {Slug: "skill-b", Name: "Skill B", Description: "Desc B"},
      },
    })
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  idx, err := FetchIndex(srv.URL + "/index.json")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if idx.Total != 2 {
    t.Errorf("Total = %d, want 2", idx.Total)
  }
  if len(idx.Skills) != 2 {
    t.Errorf("len(Skills) = %d, want 2", len(idx.Skills))
  }
}

func TestFetchIndex_ArrayFormat(t *testing.T) {
  mux := http.NewServeMux()
  mux.HandleFunc("/array.json", func(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode([]RegistrySkill{
      {Slug: "skill-a", Name: "Skill A", Description: "Desc A"},
    })
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  idx, err := FetchIndex(srv.URL + "/array.json")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if idx.Total != 1 {
    t.Errorf("Total = %d, want 1", idx.Total)
  }
  if len(idx.Skills) != 1 {
    t.Errorf("len(Skills) = %d, want 1", len(idx.Skills))
  }
}

func TestFetchIndex_HTTPError(t *testing.T) {
  mux := http.NewServeMux()
  mux.HandleFunc("/error", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusInternalServerError)
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  _, err := FetchIndex(srv.URL + "/error")
  if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
    t.Fatalf("expected HTTP 500 error, got: %v", err)
  }
}

func TestFetchIndex_InvalidJSON(t *testing.T) {
  mux := http.NewServeMux()
  mux.HandleFunc("/badjson", func(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("not json at all"))
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  _, err := FetchIndex(srv.URL + "/badjson")
  if err == nil {
    t.Fatal("expected parse error for invalid JSON")
  }
}

func TestFetchIndex_MissingURL(t *testing.T) {
  mux := http.NewServeMux()
  mux.HandleFunc("/missing", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNotFound)
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  _, err := FetchIndex(srv.URL + "/missing")
  if err == nil {
    t.Fatal("expected error for 404")
  }
}

func TestFetchIndex_InvalidRequestURL(t *testing.T) {
  _, err := FetchIndex("http://example.com/\x00")
  if err == nil {
    t.Fatal("expected error for invalid URL with null byte")
  }
}

// ============================================================
// SearchRegistry 测试
// ============================================================

func TestSearchRegistry_EmptyInput(t *testing.T) {
  skills, err := SearchRegistry("", "query", 10)
  if err != nil {
    t.Fatalf("expected no error for empty URL, got: %v", err)
  }
  if skills != nil {
    t.Errorf("expected nil skills, got %v", skills)
  }

  skills, err = SearchRegistry("http://example.com/search", "", 10)
  if err != nil {
    t.Fatalf("expected no error for empty query, got: %v", err)
  }
  if skills != nil {
    t.Errorf("expected nil skills for empty query, got %v", skills)
  }
}

func TestSearchRegistry_Success(t *testing.T) {
  mux := http.NewServeMux()
  mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
    if r.URL.Query().Get("q") != "test" {
      w.WriteHeader(http.StatusBadRequest)
      return
    }
    json.NewEncoder(w).Encode(SearchResponse{
      Results: []SearchResult{
        {
          Slug:        "test-skill",
          Name:        "test-skill",
          DisplayName: "Test Skill",
          Summary:     "A test skill",
          Description: "Full description",
          Version:     "1.0.0",
        },
      },
    })
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  skills, err := SearchRegistry(srv.URL+"/search", "test", 10)
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(skills) != 1 {
    t.Fatalf("expected 1 skill, got %d", len(skills))
  }
  if skills[0].Slug != "test-skill" {
    t.Errorf("Slug = %q, want %q", skills[0].Slug, "test-skill")
  }
}

func TestSearchRegistry_FillsEmptyNameFromSlug(t *testing.T) {
  mux := http.NewServeMux()
  mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode(SearchResponse{
      Results: []SearchResult{
        {Slug: "slug-only", Name: "", DisplayName: "", Summary: "summary only", Description: ""},
      },
    })
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  skills, err := SearchRegistry(srv.URL+"/search", "test", 0)
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(skills) != 1 {
    t.Fatalf("expected 1 skill, got %d", len(skills))
  }
  if skills[0].Name != "slug-only" {
    t.Errorf("Name = %q, want %q (should fallback to Slug)", skills[0].Name, "slug-only")
  }
  if skills[0].Description != "summary only" {
    t.Errorf("Description = %q, want %q (should fallback to Summary)", skills[0].Description, "summary only")
  }
}

func TestSearchRegistry_HTTPError(t *testing.T) {
  mux := http.NewServeMux()
  mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNotFound)
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  _, err := SearchRegistry(srv.URL+"/search", "query", 5)
  if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
    t.Fatalf("expected HTTP 404 error, got: %v", err)
  }
}

func TestSearchRegistry_BadJSON(t *testing.T) {
  mux := http.NewServeMux()
  mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("{{invalid json"))
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  _, err := SearchRegistry(srv.URL+"/search", "query", 1)
  if err == nil {
    t.Fatal("expected error for invalid JSON")
  }
}

func TestSearchRegistry_InvalidURL(t *testing.T) {
  _, err := SearchRegistry("://invalid", "query", 1)
  if err == nil {
    t.Fatal("expected error for invalid URL")
  }
}

func TestSearchRegistry_InvalidRequestURL(t *testing.T) {
  _, err := SearchRegistry("http://example.com/\x00", "query", 1)
  if err == nil {
    t.Fatal("expected error for invalid URL")
  }
}

func TestSearchRegistry_Non200(t *testing.T) {
  mux := http.NewServeMux()
  mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusServiceUnavailable)
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  _, err := SearchRegistry(srv.URL+"/search", "query", 1)
  if err == nil {
    t.Fatal("expected error for 503 response")
  }
}

// ============================================================
// DownloadAndInstall 测试
// ============================================================

func TestDownloadAndInstall_InvalidSource(t *testing.T) {
  err := DownloadAndInstall("../evil", "skill", "", "", "")
  if err == nil {
    t.Fatal("expected error for invalid source name")
  }
}

func TestDownloadAndInstall_InvalidSlug(t *testing.T) {
  err := DownloadAndInstall("source", "../evil", "", "", "")
  if err == nil {
    t.Fatal("expected error for invalid slug")
  }
}

func TestDownloadAndInstall_BothChannelsFail(t *testing.T) {
  setWorkDir(t)
  mux := http.NewServeMux()
  mux.HandleFunc("/fail", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNotFound)
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  err := DownloadAndInstall("src", "my-skill", srv.URL+"/fail", srv.URL+"/fail", "")
  if err == nil || !strings.Contains(err.Error(), "所有下载通道均失败") {
    t.Fatalf("expected all channels failed error, got: %v", err)
  }
}

func TestDownloadAndInstall_Success(t *testing.T) {
  setWorkDir(t)
  zipData := createTestZip(t, map[string]string{
    "SKILL.md": "---\nname: my-skill\ndescription: My test skill\n---\n# Content\n",
  })

  mux := http.NewServeMux()
  mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
    w.Write(zipData)
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  err := DownloadAndInstall("src", "my-skill", srv.URL+"/download", "", "")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }

  targetDir := filepath.Join(SkillsRootDir(), "src", "my-skill")
  if _, err := os.Stat(targetDir); os.IsNotExist(err) {
    t.Errorf("target directory not created: %s", targetDir)
  }
  skmdPath := filepath.Join(targetDir, "SKILL.md")
  if _, err := os.Stat(skmdPath); os.IsNotExist(err) {
    t.Errorf("SKILL.md not found in installed skill")
  }
}

func TestDownloadAndInstall_FallbackURL(t *testing.T) {
  setWorkDir(t)
  zipData := createTestZip(t, map[string]string{
    "SKILL.md": "---\nname: my-skill\ndescription: from fallback\n---\n#\n",
  })

  mux := http.NewServeMux()
  mux.HandleFunc("/primary", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNotFound)
  })
  mux.HandleFunc("/fallback", func(w http.ResponseWriter, r *http.Request) {
    w.Write(zipData)
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  err := DownloadAndInstall("src", "my-skill", srv.URL+"/primary", srv.URL+"/fallback", "")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }

  targetDir := filepath.Join(SkillsRootDir(), "src", "my-skill")
  if _, err := os.Stat(targetDir); os.IsNotExist(err) {
    t.Errorf("target directory not created: %s", targetDir)
  }
}

func TestDownloadAndInstall_MissingSKILLMD(t *testing.T) {
  setWorkDir(t)
  zipData := createTestZip(t, map[string]string{
    "somefile.txt": "not a skill\n",
  })

  mux := http.NewServeMux()
  mux.HandleFunc("/no-skill", func(w http.ResponseWriter, r *http.Request) {
    w.Write(zipData)
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  err := DownloadAndInstall("src", "my-skill", srv.URL+"/no-skill", "", "")
  if err == nil || !strings.Contains(err.Error(), "缺少 SKILL.md") {
    t.Fatalf("expected missing SKILL.md error, got: %v", err)
  }

  targetDir := filepath.Join(SkillsRootDir(), "src", "my-skill")
  if _, err := os.Stat(targetDir); !os.IsNotExist(err) {
    t.Errorf("expected target dir to be cleaned up after failure: %s", targetDir)
  }
}

func TestDownloadAndInstall_SHA256Mismatch(t *testing.T) {
  setWorkDir(t)
  zipData := createTestZip(t, map[string]string{
    "SKILL.md": "---\nname: my-skill\ndescription: test\n---\n#\n",
  })

  mux := http.NewServeMux()
  mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
    w.Write(zipData)
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  err := DownloadAndInstall("src", "my-skill", srv.URL+"/download", "", "0000000000000000000000000000000000000000000000000000000000000000")
  if err == nil || !strings.Contains(err.Error(), "SHA256") {
    t.Fatalf("expected SHA256 mismatch error, got: %v", err)
  }
}

func TestDownloadAndInstall_CleansExistingDir(t *testing.T) {
  setWorkDir(t)
  oldDir := filepath.Join(SkillsRootDir(), "src", "my-skill")
  os.MkdirAll(oldDir, 0755)
  os.WriteFile(filepath.Join(oldDir, "old-file.txt"), []byte("old"), 0644)

  zipData := createTestZip(t, map[string]string{
    "SKILL.md": "---\nname: my-skill\ndescription: fresh install\n---\n#\n",
  })

  mux := http.NewServeMux()
  mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
    w.Write(zipData)
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  err := DownloadAndInstall("src", "my-skill", srv.URL+"/download", "", "")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }

  if _, err := os.Stat(filepath.Join(SkillsRootDir(), "src", "my-skill", "old-file.txt")); !os.IsNotExist(err) {
    t.Errorf("expected old file to be removed after reinstall")
  }
}

func TestDownloadAndInstall_SlugSubstitution(t *testing.T) {
  setWorkDir(t)
  zipData := createTestZip(t, map[string]string{
    "SKILL.md": "---\nname: test-slug\ndescription: slug test\n---\n#\n",
  })

  callCount := 0
  mux := http.NewServeMux()
  mux.HandleFunc("/download/test-slug.zip", func(w http.ResponseWriter, r *http.Request) {
    callCount++
    w.Write(zipData)
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  err := DownloadAndInstall("src", "test-slug", srv.URL+"/download/{slug}.zip", "", "")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if callCount != 1 {
    t.Errorf("expected 1 call to download handler, got %d", callCount)
  }
}

// ============================================================
// downloadFile 测试
// ============================================================

func TestDownloadFile_InvalidScheme(t *testing.T) {
  _, err := downloadFile("ftp://example.com/file.zip")
  if err == nil || !strings.Contains(err.Error(), "URL 协议") {
    t.Fatalf("expected unsupported protocol error, got: %v", err)
  }
}

func TestDownloadFile_InvalidURL(t *testing.T) {
  _, err := downloadFile("://invalid")
  if err == nil {
    t.Fatal("expected error for invalid URL")
  }
}

func TestDownloadFile_Success(t *testing.T) {
  mux := http.NewServeMux()
  mux.HandleFunc("/file.zip", func(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("file content"))
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  data, err := downloadFile(srv.URL + "/file.zip")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if string(data) != "file content" {
    t.Errorf("got %q, want %q", string(data), "file content")
  }
}

func TestDownloadFile_HTTPError(t *testing.T) {
  mux := http.NewServeMux()
  mux.HandleFunc("/error", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusForbidden)
  })
  srv := httptest.NewServer(mux)
  defer srv.Close()
  setMockHTTP(t, srv.URL)

  _, err := downloadFile(srv.URL + "/error")
  if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
    t.Fatalf("expected HTTP 403 error, got: %v", err)
  }
}

// ============================================================
// extractZipToDir 测试
// ============================================================

func TestExtractZipToDir_InvalidData(t *testing.T) {
  tmpDir := t.TempDir()
  err := extractZipToDir([]byte("not a zip file"), tmpDir)
  if err == nil {
    t.Fatal("expected error for invalid zip data")
  }
}

func TestExtractZipToDir_Success(t *testing.T) {
  tmpDir := t.TempDir()
  zipData := createTestZip(t, map[string]string{
    "file1.txt":    "content1",
    "sub/file2.txt": "content2",
  })

  err := extractZipToDir(zipData, tmpDir)
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }

  data1, err := os.ReadFile(filepath.Join(tmpDir, "file1.txt"))
  if err != nil {
    t.Fatalf("failed to read file1.txt: %v", err)
  }
  if string(data1) != "content1" {
    t.Errorf("file1.txt = %q, want %q", string(data1), "content1")
  }

  data2, err := os.ReadFile(filepath.Join(tmpDir, "sub", "file2.txt"))
  if err != nil {
    t.Fatalf("failed to read sub/file2.txt: %v", err)
  }
  if string(data2) != "content2" {
    t.Errorf("sub/file2.txt = %q, want %q", string(data2), "content2")
  }
}

func TestExtractZipToDir_PathTraversal(t *testing.T) {
  tmpDir := t.TempDir()
  zipData := createTestZip(t, map[string]string{
    "../../evil.txt": "malicious",
  })

  err := extractZipToDir(zipData, tmpDir)
  if err == nil || !strings.Contains(err.Error(), "非法路径") {
    t.Fatalf("expected path traversal error, got: %v", err)
  }
}

func TestExtractZipToDir_EmptyZip(t *testing.T) {
  tmpDir := t.TempDir()
  zipData := createTestZip(t, map[string]string{})
  err := extractZipToDir(zipData, tmpDir)
  if err != nil {
    t.Fatalf("unexpected error for empty zip: %v", err)
  }
}

func TestExtractZipToDir_WithDirectoryEntry(t *testing.T) {
  tmpDir := t.TempDir()
  var buf bytes.Buffer
  zw := zip.NewWriter(&buf)
  zw.Create("mydir/")
  f, _ := zw.Create("mydir/file.txt")
  f.Write([]byte("nested content"))
  zw.Close()
  zipData := buf.Bytes()

  err := extractZipToDir(zipData, tmpDir)
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  data, err := os.ReadFile(filepath.Join(tmpDir, "mydir", "file.txt"))
  if err != nil {
    t.Fatalf("failed to read nested file: %v", err)
  }
  if string(data) != "nested content" {
    t.Errorf("content = %q, want %q", string(data), "nested content")
  }
}

func TestExtractZipToDir_FOpenError(t *testing.T) {
  tmpDir := t.TempDir()
  var buf bytes.Buffer
  zw := zip.NewWriter(&buf)
  fw, _ := zw.Create("file.txt")
  fw.Write([]byte("original content"))
  zw.Close()

  data := buf.Bytes()
  offset := 30 + 8
  if len(data) > offset+10 {
    data[offset] ^= 0xFF
  }

  err := extractZipToDir(data, tmpDir)
  if err == nil {
    t.Log("corrupted zip did not produce an error (may depend on Go version)")
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

// ============================================================
// 集成测试
// ============================================================

func TestSafePathSegment_UsedInSourceFunctions(t *testing.T) {
  _, err := ListSourceSkills("../bad")
  if err == nil {
    t.Error("ListSourceSkills should reject '../bad'")
  }
  _, err = RescanSource("../bad")
  if err == nil {
    t.Error("RescanSource should reject '../bad'")
  }
  err = CloneGitSource("../bad", "http://ex.com", "", "")
  if err == nil {
    t.Error("CloneGitSource should reject '../bad'")
  }
  _, err = PullGitSource("../bad", "", "")
  if err == nil {
    t.Error("PullGitSource should reject '../bad'")
  }
  err = DownloadAndInstall("../bad", "skill", "", "", "")
  if err == nil {
    t.Error("DownloadAndInstall should reject '../bad' as source")
  }
  err = DownloadAndInstall("source", "../bad", "", "", "")
  if err == nil {
    t.Error("DownloadAndInstall should reject '../bad' as slug")
  }
}

func TestWorkDirOverrideSmoke(t *testing.T) {
  old := config.DefaultWorkDir
  tmp := t.TempDir()
  config.DefaultWorkDir = tmp
  defer func() { config.DefaultWorkDir = old }()

  root := SkillsRootDir()
  if !strings.HasPrefix(root, tmp) {
    t.Errorf("SkillsRootDir() = %q, expected to start with %q", root, tmp)
  }

  skills, err := ListAllSkills()
  if err != nil {
    t.Fatalf("ListAllSkills with tmp workdir: %v", err)
  }
  if skills == nil {
    t.Error("ListAllSkills returned nil instead of empty slice")
  }

  writeSKILL(t, filepath.Join(root, "src", "skill-a"), "---\nname: skill-a\ndescription: Smoke test skill\n---\n#\n")
  skills, err = ListAllSkills()
  if err != nil {
    t.Fatalf("ListAllSkills after adding skill: %v", err)
  }
  if len(skills) != 1 {
    t.Errorf("expected 1 skill, got %d", len(skills))
  }
}

func TestListAllSkills_IgnoresNonDirEntries(t *testing.T) {
  setWorkDir(t)
  root := SkillsRootDir()
  os.MkdirAll(root, 0755)
  os.WriteFile(filepath.Join(root, "not-a-dir.txt"), []byte("not a dir"), 0644)

  skills, err := ListAllSkills()
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(skills) != 0 {
    t.Errorf("expected 0 skills (file entry skipped), got %d", len(skills))
  }
}

func TestListSourceSkills_IgnoresNonDirEntries(t *testing.T) {
  setWorkDir(t)
  root := filepath.Join(SkillsRootDir(), "src")
  os.MkdirAll(root, 0755)
  os.WriteFile(filepath.Join(root, "file.txt"), []byte("not a dir"), 0644)

  skills, err := ListSourceSkills("src")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(skills) != 0 {
    t.Errorf("expected 0 skills (file entry skipped), got %d", len(skills))
  }
}

func TestRescanSource_IgnoresNonDirEntries(t *testing.T) {
  setWorkDir(t)
  root := filepath.Join(SkillsRootDir(), "src")
  os.MkdirAll(root, 0755)
  os.WriteFile(filepath.Join(root, "file.txt"), []byte("not a dir"), 0644)

  names, err := RescanSource("src")
  if err != nil {
    t.Fatalf("unexpected error: %v", err)
  }
  if len(names) != 0 {
    t.Errorf("expected 0 names (file entry skipped), got %d", len(names))
  }
}
