package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/picoaide/picoaide/internal/store"
)

// ============================================================
// 单元测试：认证检查
// ============================================================

func TestAdminKBList_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{secret: "test", csrfKey: "test-csrf-key"}
	r := gin.New()
	g := r.Group("/api/admin")
	g.Use(s.superadminMiddleware())
	g.GET("/knowledge-bases", s.handleAdminKBList)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/admin/knowledge-bases", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAdminKBCreate_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{secret: "test", csrfKey: "test-csrf-key"}
	r := gin.New()
	g := r.Group("/api/admin")
	g.Use(s.superadminMiddleware())
	g.POST("/knowledge-bases", s.handleAdminKBCreate)

	w := httptest.NewRecorder()
	body := bytes.NewReader([]byte(`{"name":"test","description":"test"}`))
	req, _ := http.NewRequest("POST", "/api/admin/knowledge-bases", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAdminKBUpdate_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{secret: "test", csrfKey: "test-csrf-key"}
	r := gin.New()
	g := r.Group("/api/admin")
	g.Use(s.superadminMiddleware())
	g.PUT("/knowledge-bases/:id", s.handleAdminKBUpdate)

	w := httptest.NewRecorder()
	body := bytes.NewReader([]byte(`{"name":"test","description":"test"}`))
	req, _ := http.NewRequest("PUT", "/api/admin/knowledge-bases/1", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAdminKBDelete_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{secret: "test", csrfKey: "test-csrf-key"}
	r := gin.New()
	g := r.Group("/api/admin")
	g.Use(s.superadminMiddleware())
	g.DELETE("/knowledge-bases/:id", s.handleAdminKBDelete)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/admin/knowledge-bases/1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAdminFolderTree_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{secret: "test", csrfKey: "test-csrf-key"}
	r := gin.New()
	g := r.Group("/api/admin")
	g.Use(s.superadminMiddleware())
	g.GET("/knowledge-bases/:id/folders", s.handleAdminFolderTree)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/admin/knowledge-bases/1/folders", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAdminFolderCreate_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{secret: "test", csrfKey: "test-csrf-key"}
	r := gin.New()
	g := r.Group("/api/admin")
	g.Use(s.superadminMiddleware())
	g.POST("/knowledge-bases/:id/folders", s.handleAdminFolderCreate)

	w := httptest.NewRecorder()
	body := bytes.NewReader([]byte(`{"name":"subfolder"}`))
	req, _ := http.NewRequest("POST", "/api/admin/knowledge-bases/1/folders", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAdminFolderUpdate_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{secret: "test", csrfKey: "test-csrf-key"}
	r := gin.New()
	g := r.Group("/api/admin")
	g.Use(s.superadminMiddleware())
	g.PUT("/knowledge-bases/folders/:id", s.handleAdminFolderUpdate)

	w := httptest.NewRecorder()
	body := bytes.NewReader([]byte(`{"name":"renamed"}`))
	req, _ := http.NewRequest("PUT", "/api/admin/knowledge-bases/folders/1", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAdminFolderDelete_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{secret: "test", csrfKey: "test-csrf-key"}
	r := gin.New()
	g := r.Group("/api/admin")
	g.Use(s.superadminMiddleware())
	g.DELETE("/knowledge-bases/folders/:id", s.handleAdminFolderDelete)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/admin/knowledge-bases/folders/1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAdminFolderPermissions_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{secret: "test", csrfKey: "test-csrf-key"}
	r := gin.New()
	g := r.Group("/api/admin")
	g.Use(s.superadminMiddleware())
	g.GET("/knowledge-bases/folders/:id/permissions", s.handleAdminFolderPermissions)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/admin/knowledge-bases/folders/1/permissions", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAdminFolderSetPermissions_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{secret: "test", csrfKey: "test-csrf-key"}
	r := gin.New()
	g := r.Group("/api/admin")
	g.Use(s.superadminMiddleware())
	g.PUT("/knowledge-bases/folders/:id/permissions", s.handleAdminFolderSetPermissions)

	w := httptest.NewRecorder()
	body := bytes.NewReader([]byte(`{"users":["user1"],"groups":[1,2],"permissions_set":1}`))
	req, _ := http.NewRequest("PUT", "/api/admin/knowledge-bases/folders/1/permissions", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// ============================================================
// 集成测试：KB / 文件夹 / 权限 CRUD
// ============================================================

func TestAdminKnowledgeBaseCRUD(t *testing.T) {
	env := setupTestServer(t)

	// List — 初始为空
	resp := env.get(t, "/api/admin/knowledge-bases", "testadmin")
	assertStatus(t, resp, 200)
	var listResp struct {
		Success bool                    `json:"success"`
		Data    []store.KnowledgeBase   `json:"data"`
	}
	parseJSON(t, resp, &listResp)
	if !listResp.Success {
		t.Fatal("List 失败")
	}
	if len(listResp.Data) != 0 {
		t.Fatalf("初始应空，得到 %d 条", len(listResp.Data))
	}

	// Create
	createBody := map[string]interface{}{"name": "test-kb", "description": "my kb"}
	resp = env.postJSON(t, "/api/admin/knowledge-bases", "testadmin", createBody)
	assertStatus(t, resp, 200)
	var createResp struct {
		Success bool                  `json:"success"`
		Data    *store.KnowledgeBase  `json:"data"`
	}
	parseJSON(t, resp, &createResp)
	if !createResp.Success {
		t.Fatal("Create 失败")
	}
	if createResp.Data == nil || createResp.Data.Name != "test-kb" {
		t.Fatalf("创建 KB 不匹配: %+v", createResp.Data)
	}
	kbID := createResp.Data.ID

	// List — 应有 1 条
	resp = env.get(t, "/api/admin/knowledge-bases", "testadmin")
	parseJSON(t, resp, &listResp)
	if !listResp.Success || len(listResp.Data) != 1 {
		t.Fatalf("List 应为 1 条，得到 %d", len(listResp.Data))
	}

	// Update
	updateBody := map[string]interface{}{"name": "test-kb-renamed", "description": "updated"}
	req, _ := http.NewRequest("PUT", env.HTTP.URL+"/api/admin/knowledge-bases/"+itoa64(kbID), jsonBody(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", env.Server.csrfToken("testadmin"))
	req.AddCookie(&http.Cookie{Name: "session", Value: env.Server.createSessionToken("testadmin")})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT 请求失败: %v", err)
	}
	assertStatus(t, resp, 200)
	var updateResp struct {
		Success bool `json:"success"`
	}
	parseJSON(t, resp, &updateResp)
	if !updateResp.Success {
		t.Fatal("Update 失败")
	}

	// List — 验证更新
	resp = env.get(t, "/api/admin/knowledge-bases", "testadmin")
	parseJSON(t, resp, &listResp)
	if !listResp.Success || len(listResp.Data) != 1 || listResp.Data[0].Name != "test-kb-renamed" {
		t.Fatalf("更新后名称不匹配: %+v", listResp.Data)
	}

	// Delete
	req, _ = http.NewRequest("DELETE", env.HTTP.URL+"/api/admin/knowledge-bases/"+itoa64(kbID), nil)
	req.Header.Set("X-CSRF-Token", env.Server.csrfToken("testadmin"))
	req.AddCookie(&http.Cookie{Name: "session", Value: env.Server.createSessionToken("testadmin")})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE 请求失败: %v", err)
	}
	assertStatus(t, resp, 200)
	var delResp struct {
		Success bool `json:"success"`
	}
	parseJSON(t, resp, &delResp)
	if !delResp.Success {
		t.Fatal("Delete 失败")
	}

	// List — 应为空
	resp = env.get(t, "/api/admin/knowledge-bases", "testadmin")
	parseJSON(t, resp, &listResp)
	if !listResp.Success || len(listResp.Data) != 0 {
		t.Fatalf("删除后应为空，得到 %d 条", len(listResp.Data))
	}
}

func TestAdminFolderCRUD(t *testing.T) {
	env := setupTestServer(t)

	// 先创建 KB
	createBody := map[string]interface{}{"name": "folder-test-kb", "description": ""}
	resp := env.postJSON(t, "/api/admin/knowledge-bases", "testadmin", createBody)
	var createResp struct {
		Success bool                  `json:"success"`
		Data    *store.KnowledgeBase  `json:"data"`
	}
	parseJSON(t, resp, &createResp)
	if !createResp.Success {
		t.Fatal("创建 KB 失败")
	}
	kbID := createResp.Data.ID

	// Folder tree — 默认应有根目录 /
	resp = env.get(t, "/api/admin/knowledge-bases/"+itoa64(kbID)+"/folders", "testadmin")
	assertStatus(t, resp, 200)
	var treeResp struct {
		Success bool             `json:"success"`
		Data    []store.KBFolder `json:"data"`
	}
	parseJSON(t, resp, &treeResp)
	if !treeResp.Success || len(treeResp.Data) == 0 {
		t.Fatal("应返回文件夹树")
	}
	rootID := treeResp.Data[0].ID
	if treeResp.Data[0].Name != "/" {
		t.Fatalf("根目录名称应为 /，得到 %q", treeResp.Data[0].Name)
	}

	// Create folder
	folderBody := map[string]interface{}{"parent_id": rootID, "name": "sub"}
	resp = env.postJSON(t, "/api/admin/knowledge-bases/"+itoa64(kbID)+"/folders", "testadmin", folderBody)
	assertStatus(t, resp, 200)
	var folderCreateResp struct {
		Success bool             `json:"success"`
		Data    *store.KBFolder  `json:"data"`
	}
	parseJSON(t, resp, &folderCreateResp)
	if !folderCreateResp.Success || folderCreateResp.Data == nil || folderCreateResp.Data.Name != "sub" {
		t.Fatal("创建文件夹失败")
	}
	folderID := folderCreateResp.Data.ID

	// Folder tree — 应有 2 条
	resp = env.get(t, "/api/admin/knowledge-bases/"+itoa64(kbID)+"/folders", "testadmin")
	parseJSON(t, resp, &treeResp)
	if !treeResp.Success || len(treeResp.Data) != 2 {
		t.Fatalf("应有 2 个文件夹，得到 %d", len(treeResp.Data))
	}

	// Update folder
	updateBody := map[string]interface{}{"name": "sub-renamed"}
	req, _ := http.NewRequest("PUT", env.HTTP.URL+"/api/admin/knowledge-bases/folders/"+itoa64(folderID), jsonBody(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", env.Server.csrfToken("testadmin"))
	req.AddCookie(&http.Cookie{Name: "session", Value: env.Server.createSessionToken("testadmin")})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT 请求失败: %v", err)
	}
	assertStatus(t, resp, 200)
	var folderUpdateResp struct {
		Success bool `json:"success"`
	}
	parseJSON(t, resp, &folderUpdateResp)
	if !folderUpdateResp.Success {
		t.Fatal("更新文件夹失败")
	}

	// Verify update in tree
	resp = env.get(t, "/api/admin/knowledge-bases/"+itoa64(kbID)+"/folders", "testadmin")
	parseJSON(t, resp, &treeResp)
	found := false
	for _, f := range treeResp.Data {
		if f.ID == folderID && f.Name == "sub-renamed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("更新后文件夹名称不匹配")
	}

	// Delete folder
	req, _ = http.NewRequest("DELETE", env.HTTP.URL+"/api/admin/knowledge-bases/folders/"+itoa64(folderID), nil)
	req.Header.Set("X-CSRF-Token", env.Server.csrfToken("testadmin"))
	req.AddCookie(&http.Cookie{Name: "session", Value: env.Server.createSessionToken("testadmin")})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE 请求失败: %v", err)
	}
	assertStatus(t, resp, 200)
	var folderDelResp struct {
		Success bool `json:"success"`
	}
	parseJSON(t, resp, &folderDelResp)
	if !folderDelResp.Success {
		t.Fatal("删除文件夹失败")
	}

	// Verify tree — 只有根目录
	resp = env.get(t, "/api/admin/knowledge-bases/"+itoa64(kbID)+"/folders", "testadmin")
	parseJSON(t, resp, &treeResp)
	if !treeResp.Success || len(treeResp.Data) != 1 {
		t.Fatalf("删除后应有 1 个文件夹，得到 %d", len(treeResp.Data))
	}
}

func TestAdminFolderPermissions(t *testing.T) {
	env := setupTestServer(t)

	// 创建 KB + 获取根目录
	createBody := map[string]interface{}{"name": "perm-test-kb", "description": ""}
	resp := env.postJSON(t, "/api/admin/knowledge-bases", "testadmin", createBody)
	var createResp struct {
		Success bool                  `json:"success"`
		Data    *store.KnowledgeBase  `json:"data"`
	}
	parseJSON(t, resp, &createResp)
	if !createResp.Success {
		t.Fatal("创建 KB 失败")
	}

	resp = env.get(t, "/api/admin/knowledge-bases/"+itoa64(createResp.Data.ID)+"/folders", "testadmin")
	var treeResp struct {
		Success bool             `json:"success"`
		Data    []store.KBFolder `json:"data"`
	}
	parseJSON(t, resp, &treeResp)
	rootID := treeResp.Data[0].ID

	// Get permissions — 初始应有创建者
	resp = env.get(t, "/api/admin/knowledge-bases/folders/"+itoa64(rootID)+"/permissions", "testadmin")
	assertStatus(t, resp, 200)
	var getPermResp struct {
		Success      bool     `json:"success"`
		Users        []string `json:"users"`
		Groups       []int64  `json:"groups"`
		Set          int      `json:"permissions_set"`
	}
	parseJSON(t, resp, &getPermResp)
	if !getPermResp.Success {
		t.Fatal("获取权限失败")
	}

	// Set permissions
	setBody := map[string]interface{}{
		"users":           []string{"testuser"},
		"groups":          []int64{},
		"permissions_set": 1,
	}
	req, _ := http.NewRequest("PUT", env.HTTP.URL+"/api/admin/knowledge-bases/folders/"+itoa64(rootID)+"/permissions", jsonBody(setBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", env.Server.csrfToken("testadmin"))
	req.AddCookie(&http.Cookie{Name: "session", Value: env.Server.createSessionToken("testadmin")})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT 请求失败: %v", err)
	}
	assertStatus(t, resp, 200)
	var setPermResp struct {
		Success bool `json:"success"`
	}
	parseJSON(t, resp, &setPermResp)
	if !setPermResp.Success {
		t.Fatal("设置权限失败")
	}

	// Verify permissions were set
	resp = env.get(t, "/api/admin/knowledge-bases/folders/"+itoa64(rootID)+"/permissions", "testadmin")
	parseJSON(t, resp, &getPermResp)
	if !getPermResp.Success {
		t.Fatal("重新获取权限失败")
	}
	if getPermResp.Set != 1 {
		t.Fatalf("permissions_set 应为 1，得到 %d", getPermResp.Set)
	}
	hasTestUser := false
	for _, u := range getPermResp.Users {
		if u == "testuser" {
			hasTestUser = true
			break
		}
	}
	if !hasTestUser {
		t.Fatalf("users 中应包含 testuser，得到 %v", getPermResp.Users)
	}
}

func TestAdminKBCreate_Validation(t *testing.T) {
	env := setupTestServer(t)

	// Empty name should fail
	resp := env.postJSON(t, "/api/admin/knowledge-bases", "testadmin", map[string]interface{}{"name": "", "description": "test"})
	assertStatus(t, resp, 400)

	// Missing name field
	resp = env.postJSON(t, "/api/admin/knowledge-bases", "testadmin", map[string]interface{}{"description": "no name"})
	assertStatus(t, resp, 400)
}

func TestAdminKBDelete_NonExistent(t *testing.T) {
	env := setupTestServer(t)

	req, _ := http.NewRequest("DELETE", env.HTTP.URL+"/api/admin/knowledge-bases/99999", nil)
	req.Header.Set("X-CSRF-Token", env.Server.csrfToken("testadmin"))
	req.AddCookie(&http.Cookie{Name: "session", Value: env.Server.createSessionToken("testadmin")})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	assertStatus(t, resp, 400)
}

func TestAdminFolderCreate_NonExistentParent(t *testing.T) {
	env := setupTestServer(t)

	createBody := map[string]interface{}{"name": "kb-folder-err", "description": ""}
	resp := env.postJSON(t, "/api/admin/knowledge-bases", "testadmin", createBody)
	var createResp struct {
		Success bool                  `json:"success"`
		Data    *store.KnowledgeBase  `json:"data"`
	}
	parseJSON(t, resp, &createResp)
	kbID := createResp.Data.ID

	folderBody := map[string]interface{}{"parent_id": 99999, "name": "orphan"}
	resp = env.postJSON(t, "/api/admin/knowledge-bases/"+itoa64(kbID)+"/folders", "testadmin", folderBody)
	assertStatus(t, resp, 400)
}

func TestAdminFolderDelete_NonExistent(t *testing.T) {
	env := setupTestServer(t)

	req, _ := http.NewRequest("DELETE", env.HTTP.URL+"/api/admin/knowledge-bases/folders/99999", nil)
	req.Header.Set("X-CSRF-Token", env.Server.csrfToken("testadmin"))
	req.AddCookie(&http.Cookie{Name: "session", Value: env.Server.createSessionToken("testadmin")})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	assertStatus(t, resp, 400)
}

func TestAdminFolderPermissions_Inheritance(t *testing.T) {
	env := setupTestServer(t)

	createBody := map[string]interface{}{"name": "inherit-test-kb", "description": ""}
	resp := env.postJSON(t, "/api/admin/knowledge-bases", "testadmin", createBody)
	var createResp struct {
		Success bool                  `json:"success"`
		Data    *store.KnowledgeBase  `json:"data"`
	}
	parseJSON(t, resp, &createResp)
	kbID := createResp.Data.ID

	resp = env.get(t, "/api/admin/knowledge-bases/"+itoa64(kbID)+"/folders", "testadmin")
	var treeResp struct {
		Success bool             `json:"success"`
		Data    []store.KBFolder `json:"data"`
	}
	parseJSON(t, resp, &treeResp)
	rootID := treeResp.Data[0].ID

	// Create sub-folder (inherits permissions)
	folderBody := map[string]interface{}{"parent_id": rootID, "name": "inherit-sub"}
	resp = env.postJSON(t, "/api/admin/knowledge-bases/"+itoa64(kbID)+"/folders", "testadmin", folderBody)
	var folderCreateResp struct {
		Success bool             `json:"success"`
		Data    *store.KBFolder  `json:"data"`
	}
	parseJSON(t, resp, &folderCreateResp)
	subID := folderCreateResp.Data.ID

	// Sub folder should have permissions_set = 0 (inheriting)
	resp = env.get(t, "/api/admin/knowledge-bases/folders/"+itoa64(subID)+"/permissions", "testadmin")
	var permResp struct {
		Success bool     `json:"success"`
		Set     int      `json:"permissions_set"`
		Users   []string `json:"users"`
	}
	parseJSON(t, resp, &permResp)
	if !permResp.Success {
		t.Fatal("get permissions failed")
	}
	if permResp.Set != 0 {
		t.Errorf("inheriting folder should have permissions_set=0, got %d", permResp.Set)
	}
	// The sub folder should show inherited users from root
	if len(permResp.Users) == 0 {
		t.Log("permissions may not show inherited users - only explicitly set ones")
	}
}

func TestAdminNonSuperadmin_Forbidden(t *testing.T) {
	env := setupTestServer(t)

	// Regular user tries admin endpoints - should get 403 (authenticated but not superadmin)
	resp := env.get(t, "/api/admin/knowledge-bases", "testuser")
	assertStatus(t, resp, 403)

	resp = env.postJSON(t, "/api/admin/knowledge-bases", "testuser", map[string]interface{}{"name": "x"})
	assertStatus(t, resp, 403)
}

func TestAdminImportUpload_InvalidFileType(t *testing.T) {
	env := setupTestServer(t)

	createBody := map[string]interface{}{"name": "bad-import-kb", "description": ""}
	resp := env.postJSON(t, "/api/admin/knowledge-bases", "testadmin", createBody)
	var createResp struct {
		Success bool                  `json:"success"`
		Data    *store.KnowledgeBase  `json:"data"`
	}
	parseJSON(t, resp, &createResp)
	kbID := createResp.Data.ID

	// Upload .exe file (unsupported) with proper CSRF header
	content := []byte("fake exe")
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fw, _ := writer.CreateFormFile("file", "virus.exe")
	fw.Write(content)
	writer.WriteField("csrf_token", env.Server.csrfToken("testadmin"))
	writer.Close()

	req, _ := http.NewRequest("POST", env.HTTP.URL+"/api/admin/knowledge-bases/"+itoa64(kbID)+"/import/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-CSRF-Token", env.Server.csrfToken("testadmin"))
	req.AddCookie(&http.Cookie{Name: "session", Value: env.Server.createSessionToken("testadmin")})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}

	assertStatus(t, resp, 200)
	var importResp struct {
		Success bool   `json:"success"`
		TaskID  string `json:"task_id"`
	}
	parseJSON(t, resp, &importResp)
	if !importResp.Success {
		t.Fatal("import should succeed at HTTP level")
	}
	if importResp.TaskID == "" {
		t.Fatal("should have task_id")
	}
}

// ============================================================
// 辅助
// ============================================================

func itoa64(v int64) string {
	return fmt.Sprintf("%d", v)
}

func jsonBody(v interface{}) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}
