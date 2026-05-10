package config

import (
	"testing"
)

func TestFlattenConfig(t *testing.T) {
	input := map[string]interface{}{
		"ldap": map[string]interface{}{
			"host":    "ldap://example.com",
			"bind_dn": "cn=admin,dc=example",
			"nested": map[string]interface{}{
				"key": "value",
			},
		},
		"users_root": "./users",
	}

	result := flattenConfig(input)

	if result["ldap.host"] != "ldap://example.com" {
		t.Errorf("ldap.host = %q, want %q", result["ldap.host"], "ldap://example.com")
	}
	if result["ldap.bind_dn"] != "cn=admin,dc=example" {
		t.Errorf("ldap.bind_dn = %q", result["ldap.bind_dn"])
	}
	if result["ldap.nested.key"] != "value" {
		t.Errorf("ldap.nested.key = %q", result["ldap.nested.key"])
	}
	if result["users_root"] != "./users" {
		t.Errorf("users_root = %q", result["users_root"])
	}
}

func TestFlattenConfigJsonBlobs(t *testing.T) {
	input := map[string]interface{}{
		"picoclaw": map[string]interface{}{
			"agents": map[string]interface{}{
				"model": "gpt-4",
			},
		},
		"security": map[string]interface{}{
			"api_key": "secret",
		},
		"skills": map[string]interface{}{
			"repos": []interface{}{"repo1"},
		},
	}

	result := flattenConfig(input)

	// picoclaw/security/skills 应整体存为 JSON
	if _, ok := result["picoclaw.agents.model"]; ok {
		t.Error("picoclaw should not be flattened, should be stored as JSON blob")
	}
	if result["picoclaw"] == "" {
		t.Error("picoclaw should be stored as JSON blob")
	}
	if result["security"] == "" {
		t.Error("security should be stored as JSON blob")
	}
	if result["skills"] == "" {
		t.Error("skills should be stored as JSON blob")
	}
}

func TestFlattenConfigNil(t *testing.T) {
	input := map[string]interface{}{
		"key": nil,
	}
	result := flattenConfig(input)
	if result["key"] != "" {
		t.Errorf("nil value should be empty string, got %q", result["key"])
	}
}

func TestFlattenConfigSlice(t *testing.T) {
	input := map[string]interface{}{
		"model_list": []interface{}{
			map[string]interface{}{"name": "gpt-4"},
		},
	}
	result := flattenConfig(input)
	if result["model_list"] != `[{"name":"gpt-4"}]` {
		t.Errorf("slice should be JSON, got %q", result["model_list"])
	}
}

func TestBuildNested(t *testing.T) {
	flat := map[string]string{
		"ldap.host":    "ldap://example.com",
		"ldap.bind_dn": "cn=admin",
		"users_root":   "./users",
	}

	result := buildNested(flat)

	ldap, ok := result["ldap"].(map[string]interface{})
	if !ok {
		t.Fatal("ldap should be a map")
	}
	if ldap["host"] != "ldap://example.com" {
		t.Errorf("ldap.host = %q", ldap["host"])
	}
	if ldap["bind_dn"] != "cn=admin" {
		t.Errorf("ldap.bind_dn = %q", ldap["bind_dn"])
	}
	if result["users_root"] != "./users" {
		t.Errorf("users_root = %q", result["users_root"])
	}
}

func TestBuildNestedJsonBlobs(t *testing.T) {
	flat := map[string]string{
		"picoclaw": `{"agents":{"model":"gpt-4"}}`,
		"security": `{"api_key":"secret"}`,
	}

	result := buildNested(flat)

	pico, ok := result["picoclaw"].(map[string]interface{})
	if !ok {
		t.Fatal("picoclaw should be parsed from JSON")
	}
	agents, ok := pico["agents"].(map[string]interface{})
	if !ok {
		t.Fatal("picoclaw.agents should be a map")
	}
	if agents["model"] != "gpt-4" {
		t.Errorf("picoclaw.agents.model = %q", agents["model"])
	}
}

func TestBuildNestedBoolKeys(t *testing.T) {
	flat := map[string]string{
		"web.ldap_enabled": "true",
		"web.tls.enabled":  "false",
	}

	result := buildNested(flat)

	web := result["web"].(map[string]interface{})
	if web["ldap_enabled"] != true {
		t.Errorf("web.ldap_enabled should be bool true, got %v (%T)", web["ldap_enabled"], web["ldap_enabled"])
	}
	tls := web["tls"].(map[string]interface{})
	if tls["enabled"] != false {
		t.Errorf("web.tls.enabled should be bool false, got %v", tls["enabled"])
	}
}

func TestBuildNestedIntConversion(t *testing.T) {
	flat := map[string]string{
		"port": "8080",
	}

	result := buildNested(flat)

	if result["port"] != int64(8080) {
		t.Errorf("port should be int64(8080), got %v (%T)", result["port"], result["port"])
	}
}

func TestFlattenBuildRoundTrip(t *testing.T) {
	// 展平再重建应保持简单值不变（不含 JSON blob 键）
	input := map[string]interface{}{
		"ldap": map[string]interface{}{
			"host":    "ldap://example.com",
			"bind_dn": "cn=admin",
		},
		"users_root": "./users",
		"web": map[string]interface{}{
			"listen": ":80",
		},
	}

	flat := flattenConfig(input)
	rebuilt := buildNested(flat)

	ldap := rebuilt["ldap"].(map[string]interface{})
	if ldap["host"] != "ldap://example.com" {
		t.Errorf("roundtrip ldap.host = %q", ldap["host"])
	}
	if rebuilt["users_root"] != "./users" {
		t.Errorf("roundtrip users_root = %q", rebuilt["users_root"])
	}
}

func TestRemoveFixedConfigFields(t *testing.T) {
	input := map[string]interface{}{
		"web": map[string]interface{}{
			"listen":             ":80",
			"container_base_url": "http://172.17.0.1:8080",
		},
	}

	removeFixedConfigFields(input)

	web := input["web"].(map[string]interface{})
	if _, ok := web["container_base_url"]; ok {
		t.Fatal("container_base_url should be removed from raw config")
	}
	if web["listen"] != ":80" {
		t.Fatalf("listen should be preserved, got %v", web["listen"])
	}
}
