package authsource

import "testing"

func TestClaimStringTrimsConfiguredClaim(t *testing.T) {
  claims := map[string]interface{}{
    "preferred_username": "  alice  ",
  }
  if got := claimString(claims, "preferred_username"); got != "alice" {
    t.Fatalf("claimString = %q, want alice", got)
  }
}

func TestClaimStringsAcceptsArrayAndDelimitedString(t *testing.T) {
  claims := map[string]interface{}{
    "groups_array": []interface{}{"dev", " ops ", 12, ""},
    "groups_text":  "dev,ops qa",
  }
  arrayGroups := claimStrings(claims, "groups_array")
  if len(arrayGroups) != 2 || arrayGroups[0] != "dev" || arrayGroups[1] != "ops" {
    t.Fatalf("array groups = %v, want [dev ops]", arrayGroups)
  }
  textGroups := claimStrings(claims, "groups_text")
  if len(textGroups) != 3 || textGroups[0] != "dev" || textGroups[1] != "ops" || textGroups[2] != "qa" {
    t.Fatalf("text groups = %v, want [dev ops qa]", textGroups)
  }
}

func TestFirstNonEmpty(t *testing.T) {
  if got := firstNonEmpty("", " ", "email", "sub"); got != "email" {
    t.Fatalf("firstNonEmpty = %q, want email", got)
  }
}
