package store

import (
  "testing"
)

func TestEncryptDecryptPassword(t *testing.T) {
  SetSessionSecret("test-secret-key-for-unit-testing")
  plaintext := "my-email-password-123!@#"

  cipherHex, err := encryptPassword(plaintext)
  if err != nil {
    t.Fatalf("encryptPassword failed: %v", err)
  }
  if cipherHex == "" {
    t.Fatal("cipherHex should not be empty")
  }
  if cipherHex == plaintext {
    t.Error("cipherHex should not equal plaintext")
  }

  decrypted, err := decryptPassword(cipherHex)
  if err != nil {
    t.Fatalf("decryptPassword failed: %v", err)
  }
  if decrypted != plaintext {
    t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
  }
}

func TestEncryptDecryptWithDifferentSecret(t *testing.T) {
  SetSessionSecret("first-secret-key")
  plaintext := "sensitive-password"

  cipherHex, err := encryptPassword(plaintext)
  if err != nil {
    t.Fatalf("encryptPassword failed: %v", err)
  }

  SetSessionSecret("different-secret-key")
  _, err = decryptPassword(cipherHex)
  if err == nil {
    t.Error("decryptPassword should fail with different secret")
  }
}

func TestUpsertGetDeleteUserEmail(t *testing.T) {
  testInitDB(t)
  SetSessionSecret("test-secret-for-crud-test")

  ue := &UserEmail{
    Username:      "testuser",
    Email:         "test@example.com",
    SMTPHost:      "smtp.example.com",
    SMTPPort:      587,
    SMTPTLS:       true,
    IMAPHost:      "imap.example.com",
    IMAPPort:      993,
    IMAPTLS:       true,
    LoginUser:     "testuser@example.com",
    LoginPassword: "secret123",
    Enabled:       true,
  }

  if err := UpsertUserEmail(ue); err != nil {
    t.Fatalf("UpsertUserEmail failed: %v", err)
  }

  got, err := GetUserEmail("testuser")
  if err != nil {
    t.Fatalf("GetUserEmail failed: %v", err)
  }
  if got == nil {
    t.Fatal("GetUserEmail returned nil")
  }
  if got.Email != "test@example.com" {
    t.Errorf("Email = %q, want %q", got.Email, "test@example.com")
  }
  if got.LoginPassword == "secret123" {
    t.Error("LoginPassword should be encrypted in DB")
  }
  if got.TestResult != "" {
    t.Errorf("TestResult should be empty by default, got %q", got.TestResult)
  }

  gotDecrypted, err := GetUserEmailWithDecryptedPassword("testuser")
  if err != nil {
    t.Fatalf("GetUserEmailWithDecryptedPassword failed: %v", err)
  }
  if gotDecrypted == nil {
    t.Fatal("GetUserEmailWithDecryptedPassword returned nil")
  }
  if gotDecrypted.LoginPassword != "secret123" {
    t.Errorf("Decrypted LoginPassword = %q, want %q", gotDecrypted.LoginPassword, "secret123")
  }

  ue2 := &UserEmail{
    Username:      "testuser",
    Email:         "updated@example.com",
    SMTPHost:      "smtp2.example.com",
    SMTPPort:      465,
    SMTPTLS:       false,
    IMAPHost:      "imap2.example.com",
    IMAPPort:      143,
    IMAPTLS:       false,
    LoginUser:     "updated@example.com",
    LoginPassword: "new-password",
    Enabled:       false,
  }
  if err := UpsertUserEmail(ue2); err != nil {
    t.Fatalf("UpsertUserEmail (update) failed: %v", err)
  }

  gotUpdated, err := GetUserEmailWithDecryptedPassword("testuser")
  if err != nil {
    t.Fatalf("GetUserEmailWithDecryptedPassword after update failed: %v", err)
  }
  if gotUpdated.Email != "updated@example.com" {
    t.Errorf("Email after update = %q, want %q", gotUpdated.Email, "updated@example.com")
  }
  if gotUpdated.LoginPassword != "new-password" {
    t.Errorf("Decrypted password after update = %q, want %q", gotUpdated.LoginPassword, "new-password")
  }
  if gotUpdated.SMTPPort != 465 {
    t.Errorf("SMTPPort after update = %d, want 465", gotUpdated.SMTPPort)
  }
  if gotUpdated.SMTPTLS != false {
    t.Error("SMTPTLS after update should be false")
  }

  if err := DeleteUserEmail("testuser"); err != nil {
    t.Fatalf("DeleteUserEmail failed: %v", err)
  }

  deleted, err := GetUserEmail("testuser")
  if err != nil {
    t.Fatalf("GetUserEmail after delete failed: %v", err)
  }
  if deleted != nil {
    t.Error("GetUserEmail should return nil after delete")
  }

  deletedDecrypted, err := GetUserEmailWithDecryptedPassword("testuser")
  if err != nil {
    t.Fatalf("GetUserEmailWithDecryptedPassword after delete failed: %v", err)
  }
  if deletedDecrypted != nil {
    t.Error("GetUserEmailWithDecryptedPassword should return nil after delete")
  }
}

func TestGetUserEmailNotFound(t *testing.T) {
  testInitDB(t)
  SetSessionSecret("test-secret")

  got, err := GetUserEmail("nonexistent")
  if err != nil {
    t.Fatalf("GetUserEmail for nonexistent user failed: %v", err)
  }
  if got != nil {
    t.Error("GetUserEmail should return nil for nonexistent user")
  }

  gotDecrypted, err := GetUserEmailWithDecryptedPassword("nonexistent")
  if err != nil {
    t.Fatalf("GetUserEmailWithDecryptedPassword for nonexistent user failed: %v", err)
  }
  if gotDecrypted != nil {
    t.Error("GetUserEmailWithDecryptedPassword should return nil for nonexistent user")
  }

  if err := DeleteUserEmail("nonexistent"); err != nil {
    t.Fatalf("DeleteUserEmail for nonexistent user failed: %v", err)
  }
}
