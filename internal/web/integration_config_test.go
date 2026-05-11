package web

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/picoaide/picoaide/internal/auth"
	"github.com/picoaide/picoaide/internal/user"
)

func TestConfig_Get_Superadmin(t *testing.T) {
	env := setupTestServer(t)
	resp := env.get(t, "/api/config", "testadmin")
	assertStatus(t, resp, 200)
}

func TestConfig_Get_RegularUser(t *testing.T) {
	env := setupTestServer(t)
	resp := env.get(t, "/api/config", "testuser")
	assertStatus(t, resp, 403)
}

func TestConfig_Get_Unauthenticated(t *testing.T) {
	env := setupTestServer(t)
	resp, _ := http.Get(env.HTTP.URL + "/api/config")
	assertStatus(t, resp, 401)
}

func TestConfig_SaveAndGet(t *testing.T) {
	env := setupTestServer(t)
	// 获取当前配置
	resp := env.get(t, "/api/config", "testadmin")
	assertStatus(t, resp, 200)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// 修改并保存
	var cfg map[string]interface{}
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}
	cfg["users_root"] = "./custom-users"

	configJSON, _ := json.Marshal(cfg)
	form := url.Values{"config": {string(configJSON)}}
	resp = env.postForm(t, "/api/config", "testadmin", form)
	assertStatus(t, resp, 200)

	// 重新加载并验证
	resp = env.get(t, "/api/config", "testadmin")
	assertStatus(t, resp, 200)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}
	if cfg["users_root"] != "./custom-users" {
		t.Errorf("users_root=%v", cfg["users_root"])
	}
}

func TestConfig_AuthModeSwitchPurgesOrdinaryUserState(t *testing.T) {
	env := setupTestServer(t)
	userFile := filepath.Join(user.UserDir(env.Cfg, "testuser"), "leftover.txt")
	archiveFile := filepath.Join(user.ResolveArchiveRoot(env.Cfg), "old", "leftover.txt")
	if err := os.WriteFile(userFile, []byte("user"), 0644); err != nil {
		t.Fatalf("write user leftover: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(archiveFile), 0755); err != nil {
		t.Fatalf("mkdir archive leftover: %v", err)
	}
	if err := os.WriteFile(archiveFile, []byte("archive"), 0644); err != nil {
		t.Fatalf("write archive leftover: %v", err)
	}

	resp := env.get(t, "/api/config", "testadmin")
	assertStatus(t, resp, 200)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var cfg map[string]interface{}
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}
	cfg["web"].(map[string]interface{})["auth_mode"] = "oidc"
	configJSON, _ := json.Marshal(cfg)
	resp = env.postForm(t, "/api/config", "testadmin", url.Values{"config": {string(configJSON)}})
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(body))
	}
	assertStatus(t, resp, 200)

	if auth.UserExists("testuser") {
		t.Fatal("ordinary local user should be removed")
	}
	if rec, _ := auth.GetContainerByUsername("testuser"); rec != nil {
		t.Fatal("container record should be removed")
	}
	if _, err := os.Stat(userFile); !os.IsNotExist(err) {
		t.Fatalf("user leftover should be removed, err=%v", err)
	}
	if _, err := os.Stat(archiveFile); !os.IsNotExist(err) {
		t.Fatalf("archive leftover should be removed, err=%v", err)
	}
	if !auth.IsSuperadmin("testadmin") {
		t.Fatal("local superadmin should be kept")
	}
}

func TestAdminMigrationRulesGet(t *testing.T) {
	env := setupTestServer(t)
	resp := env.get(t, "/api/admin/migration-rules", "testadmin")
	assertStatus(t, resp, 200)
	body := getJSON(t, resp)
	rules := body["rules"].(map[string]interface{})
	if rules["picoaide_supported_config_version"].(float64) != 3 {
		t.Fatalf("picoaide_supported_config_version = %v", rules["picoaide_supported_config_version"])
	}
	if len(rules["versions"].([]interface{})) == 0 {
		t.Fatal("versions should not be empty")
	}
}

func TestAdminMigrationRulesUploadRejectsUnsupportedConfigVersion(t *testing.T) {
	env := setupTestServer(t)
	resp := env.postMultipartFile(t, "/api/admin/migration-rules/upload", "testadmin", "file", "picoclaw-adapter.zip", buildTestAdapterZip(t, map[string]string{
		"index.json": `{
  "adapter_schema_version": 1,
  "adapter_version": "unsupported",
  "latest_supported_config_version": 4,
  "picoclaw_versions": [{"version":"0.2.10","config_version":4}],
  "config_schemas": {"4":"schemas/config-v4.json"},
  "ui_schemas": {"4":"ui/ui-v4.json"},
  "migrations": []
}`,
		"schemas/config-v4.json": `{
  "config_version": 4,
  "channels_path": "channel_list",
  "channel_settings_path": "channel_list.*.settings",
  "models_path": "model_list",
  "default_model_path": "agents.defaults.model_name",
  "security": {"channels_path":"channel_list","channel_settings_path":"channel_list.*.settings","models_path":"model_list"},
  "singleton_channels": [],
  "channel_types": ["dingtalk"]
}`,
		"ui/ui-v4.json": `{"config_version":4,"pages":[]}`,
	}))
	assertStatus(t, resp, 400)
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(data), "只支持到 3") {
		t.Fatalf("response = %s, want supported config version message", string(data))
	}
}

func buildTestAdapterZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	hash := ""
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	for _, name := range names {
		hash += sha256Text(files[name]) + "  " + name + "\n"
	}
	files["hash"] = hash
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func sha256Text(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func TestUserPicoClawConfigFields(t *testing.T) {
	env := setupTestServer(t)
	cfgMap := env.Cfg.PicoClaw.(map[string]interface{})
	cfgMap["channel_list"] = map[string]interface{}{
		"dingtalk": map[string]interface{}{"enabled": true, "type": "dingtalk"},
	}
	form := url.Values{
		"section": {"dingtalk"},
		"values":  {`{"enabled":true,"client_id":"user-client","client_secret":"user-secret"}`},
	}
	resp := env.postForm(t, "/api/picoclaw/config-fields", "testuser", form)
	assertStatus(t, resp, 200)

	resp = env.get(t, "/api/picoclaw/config-fields?section=dingtalk", "testuser")
	assertStatus(t, resp, 200)
	body := getJSON(t, resp)
	fields := body["fields"].([]interface{})
	got := map[string]interface{}{}
	for _, raw := range fields {
		item := raw.(map[string]interface{})
		field := item["field"].(map[string]interface{})
		got[field["key"].(string)] = item["value"]
	}
	if got["client_id"] != "user-client" || got["client_secret"] != "user-secret" {
		t.Fatalf("fields = %+v", got)
	}
}

func TestUserPicoClawChannelsFollowAdminPolicy(t *testing.T) {
	env := setupTestServer(t)
	cfgMap := env.Cfg.PicoClaw.(map[string]interface{})
	cfgMap["channel_list"] = map[string]interface{}{
		"dingtalk": map[string]interface{}{"enabled": true, "type": "dingtalk"},
		"feishu":   map[string]interface{}{"enabled": false, "type": "feishu"},
	}

	resp := env.get(t, "/api/picoclaw/channels", "testuser")
	assertStatus(t, resp, 200)
	body := getJSON(t, resp)
	channels := body["channels"].([]interface{})
	if len(channels) != 1 || channels[0].(map[string]interface{})["key"] != "dingtalk" {
		t.Fatalf("channels = %+v, want only dingtalk", channels)
	}

	form := url.Values{
		"section": {"feishu"},
		"values":  {`{"enabled":true,"app_id":"app","app_secret":"secret"}`},
	}
	resp = env.postForm(t, "/api/picoclaw/config-fields", "testuser", form)
	assertStatus(t, resp, 400)
}

func TestUserPicoClawChannelsEmptyAdminPolicyAllowsNone(t *testing.T) {
	env := setupTestServer(t)
	cfgMap := env.Cfg.PicoClaw.(map[string]interface{})
	cfgMap["channel_list"] = map[string]interface{}{}

	resp := env.get(t, "/api/picoclaw/channels", "testuser")
	assertStatus(t, resp, 200)
	body := getJSON(t, resp)
	channels := body["channels"].([]interface{})
	if len(channels) != 0 {
		t.Fatalf("channels = %+v, want none when admin policy has no selected channels", channels)
	}

	form := url.Values{
		"section": {"dingtalk"},
		"values":  {`{"enabled":true,"client_id":"app","client_secret":"secret"}`},
	}
	resp = env.postForm(t, "/api/picoclaw/config-fields", "testuser", form)
	assertStatus(t, resp, 400)
}
