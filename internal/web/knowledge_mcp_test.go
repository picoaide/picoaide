package web

import (
  "testing"
)

func TestKBSearchHandlerRegistered(t *testing.T) {
  if _, ok := picoaideHandlers["kb_search"]; !ok {
    t.Error("kb_search handler not registered")
  }
}

func TestKBSearchToolDefExists(t *testing.T) {
  found := false
  for _, def := range picoaideToolDefs {
    if def.Name == "kb_search" {
      found = true
      break
    }
  }
  if !found {
    t.Error("kb_search tool def not found in picoaideToolDefs")
  }
}
