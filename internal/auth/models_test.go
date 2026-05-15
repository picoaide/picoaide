package auth

import (
  "testing"
)

func TestPicoclawAdapterPackageTableName(t *testing.T) {
  p := PicoclawAdapterPackage{}
  expected := "picoclaw_adapter_packages"
  if got := p.TableName(); got != expected {
    t.Fatalf("TableName() = %q, 应等于 %q", got, expected)
  }
}
