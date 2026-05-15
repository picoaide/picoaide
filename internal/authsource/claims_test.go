package authsource

import "testing"

func TestClaimStringWithEmptyKey(t *testing.T) {
  claims := map[string]interface{}{"key": "value"}
  if got := claimString(claims, ""); got != "" {
    t.Fatalf("claimString with empty key = %q, want empty", got)
  }
}

func TestClaimStringWithNonExistentKey(t *testing.T) {
  claims := map[string]interface{}{"key": "value"}
  if got := claimString(claims, "nonexistent"); got != "" {
    t.Fatalf("claimString with nonexistent key = %q, want empty", got)
  }
}

func TestClaimStringWithNonStringValue(t *testing.T) {
  claims := map[string]interface{}{"key": 42}
  if got := claimString(claims, "key"); got != "" {
    t.Fatalf("claimString with int value = %q, want empty", got)
  }
}

func TestClaimStringWithArrayValue(t *testing.T) {
  claims := map[string]interface{}{"key": []string{"a", "b"}}
  if got := claimString(claims, "key"); got != "" {
    t.Fatalf("claimString with array value = %q, want empty", got)
  }
}

func TestClaimStringWithNilClaims(t *testing.T) {
  if got := claimString(nil, "key"); got != "" {
    t.Fatalf("claimString with nil claims = %q, want empty", got)
  }
}

func TestClaimStringsWithDefaultCase(t *testing.T) {
  claims := map[string]interface{}{"key": 42}
  if got := claimStrings(claims, "key"); got != nil {
    t.Fatal("claimStrings with int value should return nil")
  }
}

func TestClaimStringsWithStringSliceType(t *testing.T) {
  claims := map[string]interface{}{"key": []string{"dev", " ops ", ""}}
  got := claimStrings(claims, "key")
  if len(got) != 2 || got[0] != "dev" || got[1] != "ops" {
    t.Fatalf("claimStrings with []string = %v, want [dev ops]", got)
  }
}

func TestClaimStringsWithNilValue(t *testing.T) {
  claims := map[string]interface{}{"key": nil}
  if got := claimStrings(claims, "key"); got != nil {
    t.Fatal("claimStrings with nil value should return nil")
  }
}

func TestClaimStringsWithNonExistentKey(t *testing.T) {
  claims := map[string]interface{}{}
  if got := claimStrings(claims, "nonexistent"); got != nil {
    t.Fatal("claimStrings with nonexistent key should return nil")
  }
}

func TestClaimStringsWithNilClaims(t *testing.T) {
  if got := claimStrings(nil, "key"); got != nil {
    t.Fatal("claimStrings with nil claims should return nil")
  }
}

func TestClaimStringsWithAllEmptyArray(t *testing.T) {
  claims := map[string]interface{}{"key": []interface{}{"", " ", 0}}
  got := claimStrings(claims, "key")
  if len(got) != 0 {
    t.Fatalf("claimStrings with empty-ish array = %v, want empty", got)
  }
}

func TestFirstNonEmptyAllEmpty(t *testing.T) {
  if got := firstNonEmpty("", "", ""); got != "" {
    t.Fatalf("firstNonEmpty all empty = %q, want empty", got)
  }
}

func TestFirstNonEmptySingleValue(t *testing.T) {
  if got := firstNonEmpty("hello"); got != "hello" {
    t.Fatalf("firstNonEmpty single = %q, want hello", got)
  }
}

func TestFirstNonEmptyFirstNonEmpty(t *testing.T) {
  if got := firstNonEmpty("", "first"); got != "first" {
    t.Fatalf("firstNonEmpty = %q, want first", got)
  }
}

func TestFirstNonEmptyWithWhitespace(t *testing.T) {
  if got := firstNonEmpty(" ", "\t", "valid"); got != "valid" {
    t.Fatalf("firstNonEmpty whitespace = %q, want valid", got)
  }
}

func TestFirstNonEmptyEmptyArgs(t *testing.T) {
  if got := firstNonEmpty(); got != "" {
    t.Fatalf("firstNonEmpty no args = %q, want empty", got)
  }
}
