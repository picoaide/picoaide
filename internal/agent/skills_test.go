package agent

import (
  "context"
  "os"
  "path/filepath"
  "strings"
  "testing"
)

// ============================================================
// Skills 系统 — 从 workspace/skills/<name>/SKILL.md 加载
// ============================================================

func TestLoadSkills_LoadsSingleSkill(t *testing.T) {
  dir := t.TempDir()
  skillDir := filepath.Join(dir, "skills", "my-skill")
  os.MkdirAll(skillDir, 0755)
  os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: my-skill
description: A test skill
license: MIT
---

## Usage

This skill helps with testing.
`), 0644)

  skills, err := LoadSkills(dir)
  if err != nil {
    t.Fatal(err)
  }
  if len(skills) != 1 {
    t.Fatalf("expected 1 skill, got %d", len(skills))
  }
  if skills[0].Name != "my-skill" {
    t.Errorf("Name = %q, want my-skill", skills[0].Name)
  }
  if skills[0].Description != "A test skill" {
    t.Errorf("Description = %q, want 'A test skill'", skills[0].Description)
  }
  if !strings.Contains(skills[0].Content, "This skill helps with testing") {
    t.Errorf("Content missing skill body: %q", skills[0].Content)
  }
}

func TestLoadSkills_NoSkillsDir(t *testing.T) {
  dir := t.TempDir()
  skills, err := LoadSkills(dir)
  if err != nil {
    t.Fatal(err)
  }
  if len(skills) != 0 {
    t.Errorf("expected 0 skills, got %d", len(skills))
  }
}

func TestLoadSkills_SkipsInvalidDir(t *testing.T) {
  dir := t.TempDir()
  os.MkdirAll(filepath.Join(dir, "skills", "empty-dir"), 0755)
  os.MkdirAll(filepath.Join(dir, "skills", "no-frontmatter"), 0755)
  os.WriteFile(filepath.Join(filepath.Join(dir, "skills", "no-frontmatter"), "SKILL.md"), []byte("just content\nno frontmatter"), 0644)

  skills, err := LoadSkills(dir)
  if err != nil {
    t.Fatal(err)
  }
  if len(skills) != 0 {
    t.Errorf("expected 0 skills for invalid dirs, got %d", len(skills))
  }
}

// ============================================================
// BuildSkillsPrompt — 技能内容转换为 system prompt
// ============================================================

func TestBuildSkillsPrompt_IncludesAllSkills(t *testing.T) {
  skills := []*Skill{
    {Name: "skill-a", Description: "First", Content: "Content A"},
    {Name: "skill-b", Description: "Second", Content: "Content B"},
  }

  prompt := BuildSkillsPrompt(skills)
  if !strings.Contains(prompt, "skill-a") {
    t.Errorf("prompt missing skill-a: %q", prompt)
  }
  if !strings.Contains(prompt, "Content A") {
    t.Errorf("prompt missing Content A: %q", prompt)
  }
  if !strings.Contains(prompt, "skill-b") {
    t.Errorf("prompt missing skill-b: %q", prompt)
  }
  if !strings.Contains(prompt, "Content B") {
    t.Errorf("prompt missing Content B: %q", prompt)
  }
}

func TestBuildSkillsPrompt_Empty(t *testing.T) {
  prompt := BuildSkillsPrompt(nil)
  if prompt != "" {
    t.Errorf("expected empty prompt for nil skills, got %q", prompt)
  }
  prompt = BuildSkillsPrompt([]*Skill{})
  if prompt != "" {
    t.Errorf("expected empty prompt for empty skills, got %q", prompt)
  }
}

// ============================================================
// Engine 集成 — Skills 应注入到 system prompt
// ============================================================

func TestEngine_UsesSkillsInSystemPrompt(t *testing.T) {
  provider := &mockSkillsProvider{
    capturedSystem: "",
  }
  tools := NewToolRegistry()
  store := NewSessionStore(t.TempDir())
  engine := NewEngine(testConfig(), provider, tools, store)

  skills := []*Skill{
    {Name: "code-review", Description: "Review code", Content: "Focus on security and performance."},
  }
  engine.SetSkills(skills)

  msg := &Message{Role: RoleUser, Content: "review this"}
  _ = engine.Process(context.Background(), "Be helpful.", nil, msg, func(_ StreamEvent) {})

  if !strings.Contains(provider.capturedSystem, "code-review") {
    t.Errorf("system prompt should contain skill name 'code-review', got: %q", provider.capturedSystem)
  }
  if !strings.Contains(provider.capturedSystem, "Focus on security") {
    t.Errorf("system prompt should contain skill content, got: %q", provider.capturedSystem)
  }
}

type mockSkillsProvider struct {
  capturedSystem string
}

func (m *mockSkillsProvider) StreamChat(_ context.Context, req *ChatRequest, cb func(event StreamEvent)) error {
  m.capturedSystem = req.System
  cb(TextDelta("ok"))
  cb(FinishEvent("ok", map[string]int{}))
  return nil
}

func TestLoadSkills_MultipleSkills(t *testing.T) {
  dir := t.TempDir()
  os.MkdirAll(filepath.Join(dir, "skills", "skill-a"), 0755)
  os.WriteFile(filepath.Join(filepath.Join(dir, "skills", "skill-a"), "SKILL.md"), []byte(`---
name: skill-a
description: First skill
---
Content A
`), 0644)

  os.MkdirAll(filepath.Join(dir, "skills", "skill-b"), 0755)
  os.WriteFile(filepath.Join(filepath.Join(dir, "skills", "skill-b"), "SKILL.md"), []byte(`---
name: skill-b
description: Second skill
---
Content B
`), 0644)

  skills, err := LoadSkills(dir)
  if err != nil {
    t.Fatal(err)
  }
  if len(skills) != 2 {
    t.Fatalf("expected 2 skills, got %d", len(skills))
  }
  names := map[string]bool{}
  for _, s := range skills {
    names[s.Name] = true
  }
  if !names["skill-a"] || !names["skill-b"] {
    t.Errorf("missing skills: got %v", names)
  }
}
