package agent

import (
  "testing"
)

func TestExtractCapability_ChineseDesc(t *testing.T) {
  desc := "查询企业的新闻舆情信息，包括正面/负面/中性报道"
  cap := extractCapability(desc)
  expected := "企业的新闻舆情信息"
  if cap != expected {
    t.Errorf("extractCapability(%q) = %q, want %q", desc, cap, expected)
  }
}

func TestExtractCapability_ShortDesc(t *testing.T) {
  desc := "获取企业工商信息"
  cap := extractCapability(desc)
  expected := "企业工商信息"
  if cap != expected {
    t.Errorf("extractCapability(%q) = %q, want %q", desc, cap, expected)
  }
}

func TestExtractCapability_EnglishDesc(t *testing.T) {
  desc := "Get company information including basic info and shareholders"
  cap := extractCapability(desc)
  if cap == "" {
    t.Error("extractCapability should not return empty for English descriptions")
  }
}

func TestExtractCapability_EmptyDesc(t *testing.T) {
  cap := extractCapability("")
  if cap != "" {
    t.Errorf("expected empty for empty desc, got %q", cap)
  }
}

func TestGenerateServerSummary_WithChineseTools(t *testing.T) {
  tools := []ToolDef{
    {Name: "get_company_info", Description: "查询企业工商信息，包括股东和高管"},
    {Name: "get_news_sentiment", Description: "获取企业的新闻舆情，含正负面分析"},
    {Name: "search_court_notice", Description: "查询企业法院公告信息"},
  }
  summary := generateServerSummary("tyc-mcp", tools)
  if summary == "" {
    t.Fatal("expected non-empty summary")
  }
  if len(summary) > 150 {
    t.Errorf("summary too long (%d chars): %s", len(summary), summary)
  }
  t.Logf("got summary: %s", summary)
}

func TestGenerateServerSummary_EmptyTools(t *testing.T) {
  summary := generateServerSummary("empty-srv", nil)
  if summary != "empty-srv（0 个工具）" {
    t.Errorf("unexpected summary for empty: %s", summary)
  }
}

func TestGenerateServerSummary_Deduplication(t *testing.T) {
  tools := []ToolDef{
    {Name: "a", Description: "查询企业工商信息"},
    {Name: "b", Description: "查询企业工商信息"},
  }
  summary := generateServerSummary("srv", tools)
  if len(summary) == 0 {
    t.Fatal("expected non-empty summary")
  }
  t.Logf("summary: %s", summary)
}

func TestGenerateServerSummary_ManyTools(t *testing.T) {
  tools := make([]ToolDef, 10)
  for i := 0; i < 10; i++ {
    tools[i] = ToolDef{
      Name: "tool", Description: "测试功能",
    }
  }
  summary := generateServerSummary("srv", tools)
  if len(summary) > 80 {
    t.Logf("summary with many tools: %s", summary)
  }
}

func TestDescribeToolFromName(t *testing.T) {
  desc := describeToolFromName("get_company_info")
  if desc == "" {
    t.Error("expected non-empty description from tool name")
  }
}

func TestDescribeToolFromName_Short(t *testing.T) {
  desc := describeToolFromName("ls")
  if desc == "" {
    t.Error("expected non-empty description even for short tool names")
  }
}
