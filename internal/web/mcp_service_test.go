package web

import (
  "encoding/json"
  "testing"
)

func TestToolToMap(t *testing.T) {
  td := ToolDef{
    Name:        "browser_navigate",
    Description: "Navigate to URL",
    InputSchema: map[string]interface{}{
      "type": "object",
      "properties": map[string]interface{}{
        "url": map[string]interface{}{"type": "string"},
      },
    },
  }
  m := toolToMap(td)
  if m["name"] != "browser_navigate" {
    t.Errorf("name = %v, want browser_navigate", m["name"])
  }
  if m["description"] != "Navigate to URL" {
    t.Errorf("description = %v, want Navigate to URL", m["description"])
  }
}

func TestAgentToolsList_IncludesPicoaideTools(t *testing.T) {
  info, ok := serviceRegistry["agent"]
  if !ok {
    t.Fatal("agent service not registered")
  }

  // Verify the agent service has picoaide tools
  if len(info.Tools) != len(picoaideToolDefs) {
    t.Errorf("agent tools count = %d, want %d", len(info.Tools), len(picoaideToolDefs))
  }
}

func TestPicoaideToolDefsNotEmpty(t *testing.T) {
  if len(picoaideToolDefs) == 0 {
    t.Error("picoaideToolDefs should not be empty")
  }
}

func TestBrowserToolDefsNotEmpty(t *testing.T) {
  if len(browserToolDefs) == 0 {
    t.Error("browserToolDefs should not be empty")
  }
}

func TestComputerToolDefsNotEmpty(t *testing.T) {
  if len(computerToolDefs) == 0 {
    t.Error("computerToolDefs should not be empty")
  }
}

func TestToolsListAggregation(t *testing.T) {
  // Test that tools/list for "agent" service aggregates all sources
  // This test verifies the structure expected after integration
  info, ok := serviceRegistry["agent"]
  if !ok {
    t.Fatal("agent service not registered")
  }
  if info.Hub != nil {
    t.Errorf("agent service Hub should be nil (server-side)")
  }
  if info.ServerName != "picoaide-agent" {
    t.Errorf("ServerName = %q, want picoaide-agent", info.ServerName)
  }
}

func TestAgentServiceToolsListJSON(t *testing.T) {
  // Simulate the tools/list response structure
  tools := make([]map[string]interface{}, 0)
  for _, td := range picoaideToolDefs {
    tools = append(tools, toolToMap(td))
  }

  resp := map[string]interface{}{
    "tools": tools,
  }
  data, err := json.Marshal(resp)
  if err != nil {
    t.Fatalf("JSON marshal failed: %v", err)
  }

  var decoded map[string]interface{}
  if err := json.Unmarshal(data, &decoded); err != nil {
    t.Fatalf("JSON unmarshal failed: %v", err)
  }

  toolsArr, ok := decoded["tools"].([]interface{})
  if !ok {
    t.Fatal("tools should be an array")
  }
  if len(toolsArr) != len(picoaideToolDefs) {
    t.Errorf("tools count = %d, want %d", len(toolsArr), len(picoaideToolDefs))
  }
}
