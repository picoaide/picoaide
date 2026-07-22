package web

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/picoaide/picoaide/internal/store"
)

// ============================================================
// 认证检查
// ============================================================

func TestUserKBList_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{secret: "test", csrfKey: "test-csrf-key"}
	r := gin.New()
	r.GET("/user/knowledge-bases", s.handleUserKBList)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/user/knowledge-bases", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestUserKBOverview_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{secret: "test", csrfKey: "test-csrf-key"}
	r := gin.New()
	r.GET("/user/knowledge-bases/:id", s.handleUserKBOverview)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/user/knowledge-bases/1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestUserKBNavigate_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{secret: "test", csrfKey: "test-csrf-key"}
	r := gin.New()
	r.GET("/user/knowledge-bases/:id/navigate", s.handleUserKBNavigate)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/user/knowledge-bases/1/navigate", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestUserKBRead_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{secret: "test", csrfKey: "test-csrf-key"}
	r := gin.New()
	r.GET("/user/knowledge-bases/documents/:id", s.handleUserKBRead)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/user/knowledge-bases/documents/1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestUserKBSearch_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{secret: "test", csrfKey: "test-csrf-key"}
	r := gin.New()
	r.GET("/user/knowledge-bases/search", s.handleUserKBSearch)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/user/knowledge-bases/search?q=test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestUserKBImportUpload_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{secret: "test", csrfKey: "test-csrf-key"}
	r := gin.New()
	r.POST("/user/knowledge-bases/:id/import/upload", s.handleUserKBImportUpload)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/user/knowledge-bases/1/import/upload", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestUserKBImportProgress_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{secret: "test", csrfKey: "test-csrf-key"}
	r := gin.New()
	r.GET("/user/knowledge-bases/imports/:task_id", s.handleUserKBImportProgress)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/user/knowledge-bases/imports/task-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// ============================================================
// 集成测试
// ============================================================

func TestUserKBList_Success(t *testing.T) {
	env := setupTestServer(t)

	resp := env.get(t, "/api/user/knowledge-bases", "testuser")
	assertStatus(t, resp, 200)
	var body struct {
		Success bool                  `json:"success"`
		Data    []store.KnowledgeBase `json:"data"`
	}
	parseJSON(t, resp, &body)
	if !body.Success {
		t.Fatal("expected success")
	}
}

func TestUserKBSearch_EmptyQuery(t *testing.T) {
	env := setupTestServer(t)

	resp := env.get(t, "/api/user/knowledge-bases/search?q=", "testuser")
	assertStatus(t, resp, 400)
}

func TestUserKBSearch_ValidQuery(t *testing.T) {
	env := setupTestServer(t)

	resp := env.get(t, "/api/user/knowledge-bases/search?q=test", "testuser")
	assertStatus(t, resp, 200)
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Results []store.SearchResult `json:"results"`
			Total   int64                `json:"total"`
		} `json:"data"`
	}
	parseJSON(t, resp, &body)
	if !body.Success {
		t.Fatal("expected success")
	}
}

func TestUserKBOverview_NotFound(t *testing.T) {
	env := setupTestServer(t)

	resp := env.get(t, "/api/user/knowledge-bases/999", "testuser")
	assertStatus(t, resp, 404)
}

func TestUserKBRead_NotFound(t *testing.T) {
	env := setupTestServer(t)

	resp := env.get(t, "/api/user/knowledge-bases/documents/999", "testuser")
	assertStatus(t, resp, 404)
}

func TestUserKBImportUpload_Success(t *testing.T) {
	env := setupTestServer(t)

	createBody := map[string]interface{}{"name": "import-test-kb", "description": ""}
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

	content := []byte("# Test Document\n\nThis is a test.")
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fw, _ := writer.CreateFormFile("file", "test.md")
	fw.Write(content)
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
		t.Fatal("import should succeed")
	}
	if importResp.TaskID == "" {
		t.Fatal("should have task_id")
	}
}

func TestUserKBImportUpload_MissingFile(t *testing.T) {
	env := setupTestServer(t)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writer.WriteField("kb_id", "1")
	writer.Close()

	req, _ := http.NewRequest("POST", env.HTTP.URL+"/api/admin/knowledge-bases/1/import/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-CSRF-Token", env.Server.csrfToken("testadmin"))
	req.AddCookie(&http.Cookie{Name: "session", Value: env.Server.createSessionToken("testadmin")})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	assertStatus(t, resp, 400)
}

func TestUserKBImportProgress_NotFound(t *testing.T) {
	env := setupTestServer(t)

	resp := env.get(t, "/api/user/knowledge-bases/imports/nonexistent", "testuser")
	assertStatus(t, resp, 404)
}

func TestUserKBOverview_WithKB(t *testing.T) {
	env := setupTestServer(t)

	createBody := map[string]interface{}{"name": "overview-test-kb", "description": "desc"}
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

	resp = env.get(t, "/api/user/knowledge-bases/"+itoa64(kbID), "testuser")
	assertStatus(t, resp, 200)
	var overviewResp struct {
		Success bool `json:"success"`
		Data    struct {
			KB      *store.KnowledgeBase `json:"kb"`
			Folders []store.KBFolder     `json:"folders"`
		} `json:"data"`
	}
	parseJSON(t, resp, &overviewResp)
	if !overviewResp.Success {
		t.Fatal("overview should succeed")
	}
	if overviewResp.Data.KB == nil || overviewResp.Data.KB.Name != "overview-test-kb" {
		t.Fatalf("KB mismatch: %+v", overviewResp.Data.KB)
	}
	if len(overviewResp.Data.Folders) == 0 {
		t.Fatal("should have at least root folder")
	}
}

func TestUserKBOverview_NoPermission(t *testing.T) {
	env := setupTestServer(t)

	// Create a KB as admin
	createBody := map[string]interface{}{"name": "restricted-kb", "description": ""}
	resp := env.postJSON(t, "/api/admin/knowledge-bases", "testadmin", createBody)
	var createResp struct {
		Success bool                  `json:"success"`
		Data    *store.KnowledgeBase  `json:"data"`
	}
	parseJSON(t, resp, &createResp)
	kbID := createResp.Data.ID

	// Remove testuser from root folder permissions
	resp = env.get(t, "/api/admin/knowledge-bases/"+itoa64(kbID)+"/folders", "testadmin")
	var treeResp struct {
		Success bool             `json:"success"`
		Data    []store.KBFolder `json:"data"`
	}
	parseJSON(t, resp, &treeResp)
	rootID := treeResp.Data[0].ID

	// Overwrite permissions: only "otheruser" (not testuser)
	setBody := map[string]interface{}{
		"users":           []string{"otheruser"},
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

	// testuser should be able to see KB overview (KB-level access isn't restricted)
	resp = env.get(t, "/api/user/knowledge-bases/"+itoa64(kbID), "testuser")
	assertStatus(t, resp, 200)
}

func TestUserKBSearch_Pagination(t *testing.T) {
	env := setupTestServer(t)

	resp := env.get(t, "/api/user/knowledge-bases/search?q=test&page=1&size=5", "testuser")
	assertStatus(t, resp, 200)
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Results []store.SearchResult `json:"results"`
			Total   int64               `json:"total"`
		} `json:"data"`
	}
	parseJSON(t, resp, &body)
	if !body.Success {
		t.Fatal("expected success")
	}
}

func TestUserKBSearch_NoResults(t *testing.T) {
	env := setupTestServer(t)

	resp := env.get(t, "/api/user/knowledge-bases/search?q=ZZZZNONEXISTENT", "testuser")
	assertStatus(t, resp, 200)
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Results []store.SearchResult `json:"results"`
			Total   int64               `json:"total"`
		} `json:"data"`
	}
	parseJSON(t, resp, &body)
	if !body.Success {
		t.Fatal("expected success")
	}
	if body.Data.Total != 0 {
		t.Errorf("expected total=0, got %d", body.Data.Total)
	}
	if len(body.Data.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(body.Data.Results))
	}
}

func TestUserKBNavigate_Pagination(t *testing.T) {
	env := setupTestServer(t)

	createBody := map[string]interface{}{"name": "nav-page-kb", "description": ""}
	resp := env.postJSON(t, "/api/admin/knowledge-bases", "testadmin", createBody)
	var createResp struct {
		Success bool                  `json:"success"`
		Data    *store.KnowledgeBase  `json:"data"`
	}
	parseJSON(t, resp, &createResp)
	kbID := createResp.Data.ID

	// Give testuser access by setting folder permissions
	resp = env.get(t, "/api/admin/knowledge-bases/"+itoa64(kbID)+"/folders", "testadmin")
	var treeResp struct {
		Success bool             `json:"success"`
		Data    []store.KBFolder `json:"data"`
	}
	parseJSON(t, resp, &treeResp)
	rootID := treeResp.Data[0].ID

	setBody := map[string]interface{}{
		"users":           []string{"testadmin", "testuser"},
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

	// Now testuser can navigate
	resp = env.get(t, "/api/user/knowledge-bases/"+itoa64(kbID)+"/navigate?folder=0&page=1&size=5", "testuser")
	assertStatus(t, resp, 200)
}

func TestUserKBNavigate_NoPermission(t *testing.T) {
	env := setupTestServer(t)

	createBody := map[string]interface{}{"name": "restricted-nav", "description": ""}
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

	// Set permissions to exclude testuser
	setBody := map[string]interface{}{
		"users":           []string{"otheruser"},
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

	resp = env.get(t, "/api/user/knowledge-bases/"+itoa64(kbID)+"/navigate?folder="+itoa64(rootID), "testuser")
	assertStatus(t, resp, 403)
}

func TestUserKBRead_WithLinks(t *testing.T) {
	env := setupTestServer(t)

	createBody := map[string]interface{}{"name": "read-links-kb", "description": ""}
	resp := env.postJSON(t, "/api/admin/knowledge-bases", "testadmin", createBody)
	var createResp struct {
		Success bool                  `json:"success"`
		Data    *store.KnowledgeBase  `json:"data"`
	}
	parseJSON(t, resp, &createResp)
	kbID := createResp.Data.ID

	content := []byte("# API Guide\n参考 [[OAuth2 Guide]]\n标签: #api #security")
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fw, _ := writer.CreateFormFile("file", "api-guide.md")
	fw.Write(content)
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
}

func TestUserKBImportUpload_LargeFile(t *testing.T) {
	env := setupTestServer(t)

	createBody := map[string]interface{}{"name": "large-import", "description": ""}
	resp := env.postJSON(t, "/api/admin/knowledge-bases", "testadmin", createBody)
	var createResp struct {
		Success bool                  `json:"success"`
		Data    *store.KnowledgeBase  `json:"data"`
	}
	parseJSON(t, resp, &createResp)
	kbID := createResp.Data.ID

	content := make([]byte, 100*1024)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fw, _ := writer.CreateFormFile("file", "large.md")
	fw.Write(content)
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
}

func writeFormField(writer *multipart.Writer, name, value string) {
	fw, _ := writer.CreateFormField(name)
	io.WriteString(fw, value)
}
