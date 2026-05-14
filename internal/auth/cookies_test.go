package auth

import (
  "testing"
  "time"
)

func TestSetAndGetCookie(t *testing.T) {
  testInitDB(t)
  username := "testuser"
  domain := "example.com"
  cookies := "session=abc; token=xyz"

  if err := SetCookie(username, domain, cookies); err != nil {
    t.Fatalf("SetCookie failed: %v", err)
  }

  got, err := GetCookie(username, domain)
  if err != nil {
    t.Fatalf("GetCookie failed: %v", err)
  }
  if got != cookies {
    t.Errorf("GetCookie = %q, want %q", got, cookies)
  }
}

func TestGetCookie_NotFound(t *testing.T) {
  testInitDB(t)
  got, err := GetCookie("nobody", "nowhere.com")
  if err != nil {
    t.Fatalf("GetCookie failed: %v", err)
  }
  if got != "" {
    t.Errorf("GetCookie = %q, want empty string", got)
  }
}

func TestSetCookie_Overwrites(t *testing.T) {
  testInitDB(t)
  username := "testuser"
  domain := "example.com"

  if err := SetCookie(username, domain, "old=value"); err != nil {
    t.Fatal(err)
  }
  if err := SetCookie(username, domain, "new=value"); err != nil {
    t.Fatal(err)
  }

  got, _ := GetCookie(username, domain)
  if got != "new=value" {
    t.Errorf("after overwrite, GetCookie = %q, want %q", got, "new=value")
  }
}

func TestSetCookie_UpdatedAtChanges(t *testing.T) {
  testInitDB(t)
  username := "testuser"
  domain := "example.com"

  if err := SetCookie(username, domain, "first"); err != nil {
    t.Fatal(err)
  }

  time.Sleep(2 * time.Second)

  if err := SetCookie(username, domain, "second"); err != nil {
    t.Fatal(err)
  }

  var rec UserCookie
  has, err := engine.Where("username = ? AND domain = ?", username, domain).Get(&rec)
  if err != nil {
    t.Fatal(err)
  }
  if !has {
    t.Fatal("record not found")
  }
  if rec.UpdatedAt == "" {
    t.Error("UpdatedAt is empty")
  }
  if rec.Cookies != "second" {
    t.Errorf("Cookies = %q, want %q", rec.Cookies, "second")
  }
}

func TestGetAllCookies(t *testing.T) {
  testInitDB(t)
  username := "testuser"
  if err := SetCookie(username, "a.com", "a=1"); err != nil {
    t.Fatal(err)
  }
  if err := SetCookie(username, "b.com", "b=2"); err != nil {
    t.Fatal(err)
  }

  all, err := GetAllCookies(username)
  if err != nil {
    t.Fatalf("GetAllCookies failed: %v", err)
  }
  if len(all) != 2 {
    t.Errorf("got %d cookies, want 2", len(all))
  }
  if all["a.com"] != "a=1" {
    t.Errorf("a.com = %q, want %q", all["a.com"], "a=1")
  }
  if all["b.com"] != "b=2" {
    t.Errorf("b.com = %q, want %q", all["b.com"], "b=2")
  }
}

func TestGetAllCookies_Empty(t *testing.T) {
  testInitDB(t)
  all, err := GetAllCookies("nobody")
  if err != nil {
    t.Fatalf("GetAllCookies failed: %v", err)
  }
  if len(all) != 0 {
    t.Errorf("got %d cookies, want 0", len(all))
  }
}

func TestDeleteCookie(t *testing.T) {
  testInitDB(t)
  username := "testuser"
  domain := "example.com"
  if err := SetCookie(username, domain, "session=abc"); err != nil {
    t.Fatal(err)
  }

  if err := DeleteCookie(username, domain); err != nil {
    t.Fatalf("DeleteCookie failed: %v", err)
  }

  got, _ := GetCookie(username, domain)
  if got != "" {
    t.Errorf("after delete, GetCookie = %q, want empty", got)
  }
}

func TestDeleteCookie_NotFound(t *testing.T) {
  testInitDB(t)
  if err := DeleteCookie("nobody", "nowhere.com"); err != nil {
    t.Errorf("DeleteCookie on non-existent should succeed: %v", err)
  }
}

func TestListCookieDomains(t *testing.T) {
  testInitDB(t)
  username := "testuser"
  if err := SetCookie(username, "a.com", "a=1"); err != nil {
    t.Fatal(err)
  }
  if err := SetCookie(username, "b.com", "b=2"); err != nil {
    t.Fatal(err)
  }

  entries, err := ListCookieDomains(username)
  if err != nil {
    t.Fatalf("ListCookieDomains failed: %v", err)
  }
  if len(entries) != 2 {
    t.Errorf("got %d entries, want 2", len(entries))
  }
  domains := map[string]bool{}
  for _, e := range entries {
    domains[e.Domain] = true
    if e.UpdatedAt == "" {
      t.Errorf("entry %q has empty UpdatedAt", e.Domain)
    }
  }
  if !domains["a.com"] || !domains["b.com"] {
    t.Errorf("missing domains: %v", domains)
  }
}

func TestListCookieDomains_Empty(t *testing.T) {
  testInitDB(t)
  entries, err := ListCookieDomains("nobody")
  if err != nil {
    t.Fatalf("ListCookieDomains failed: %v", err)
  }
  if len(entries) != 0 {
    t.Errorf("got %d entries, want 0", len(entries))
  }
}

func TestCookieIsolation_BetweenUsers(t *testing.T) {
  testInitDB(t)
  if err := SetCookie("alice", "example.com", "alice_cookie"); err != nil {
    t.Fatal(err)
  }
  if err := SetCookie("bob", "example.com", "bob_cookie"); err != nil {
    t.Fatal(err)
  }

  alice, _ := GetCookie("alice", "example.com")
  bob, _ := GetCookie("bob", "example.com")

  if alice != "alice_cookie" {
    t.Errorf("alice = %q", alice)
  }
  if bob != "bob_cookie" {
    t.Errorf("bob = %q", bob)
  }

  allAlice, _ := GetAllCookies("alice")
  if len(allAlice) != 1 {
    t.Errorf("alice has %d cookies, want 1", len(allAlice))
  }
}
