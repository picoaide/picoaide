package web

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/picoaide/picoaide/internal/knowledge"
	"github.com/picoaide/picoaide/internal/store"
)

// handleUserKBList 列出用户可访问的知识库
func (s *Server) handleUserKBList(c *gin.Context) {
	if s.requireRegularUser(c) == "" {
		return
	}
	kbs, err := store.ListKnowledgeBases()
	if err != nil {
		writeError(c, 500, "获取知识库列表失败")
		return
	}
	writeJSON(c, 200, gin.H{"success": true, "data": kbs})
}

// handleUserKBOverview 知识库概览 + 文件夹树
func (s *Server) handleUserKBOverview(c *gin.Context) {
	if s.requireRegularUser(c) == "" {
		return
	}
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "无效的 ID")
		return
	}
	kb, err := store.GetKnowledgeBase(id)
	if err != nil {
		writeError(c, http.StatusNotFound, "知识库不存在")
		return
	}
	folders, err := store.GetFolderTree(id)
	if err != nil {
		writeError(c, 500, "获取文件夹树失败")
		return
	}
	writeJSON(c, 200, gin.H{"success": true, "data": gin.H{"kb": kb, "folders": folders}})
}

// handleUserKBNavigate 浏览文件夹内容
func (s *Server) handleUserKBNavigate(c *gin.Context) {
	username := s.requireRegularUser(c)
	if username == "" {
		return
	}
	idStr := c.Param("id")
	kbID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "无效的 ID")
		return
	}

	folderStr := c.DefaultQuery("folder", "0")
	folderID, err := strconv.ParseInt(folderStr, 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "无效的文件夹 ID")
		return
	}

	if folderID == 0 {
		folders, err := store.GetFolderTree(kbID)
		if err != nil {
			writeError(c, 500, "获取文件夹失败")
			return
		}
		for _, f := range folders {
			if f.ParentID == nil {
				folderID = f.ID
				break
			}
		}
		if folderID == 0 {
			writeError(c, http.StatusNotFound, "知识库无根目录")
			return
		}
	}

	subFolders, docs, err := store.BrowseFolder(username, folderID)
	if err != nil {
		writeError(c, http.StatusForbidden, err.Error())
		return
	}

	// ponytail: pagination params accepted but not applied; add when BrowseFolder supports it
	writeJSON(c, 200, gin.H{"success": true, "data": gin.H{"folders": subFolders, "documents": docs}})
}

// handleUserKBRead 读取文档
func (s *Server) handleUserKBRead(c *gin.Context) {
	username := s.requireRegularUser(c)
	if username == "" {
		return
	}
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "无效的文档 ID")
		return
	}
	doc, err := store.GetDocumentByID(username, id)
	if err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(c, 200, gin.H{"success": true, "data": doc})
}

// handleUserKBSearch FTS5 搜索
func (s *Server) handleUserKBSearch(c *gin.Context) {
	username := s.requireRegularUser(c)
	if username == "" {
		return
	}
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		writeError(c, http.StatusBadRequest, "搜索关键词不能为空")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	results, total, err := store.SearchKB(username, query, page, pageSize)
	if err != nil {
		writeError(c, 500, "搜索失败")
		return
	}
	writeJSON(c, 200, gin.H{"success": true, "data": results, "total": total})
}

// handleUserKBImportUpload 文件上传导入
func (s *Server) handleUserKBImportUpload(c *gin.Context) {
	username := s.requireRegularUser(c)
	if username == "" {
		return
	}
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		writeError(c, http.StatusBadRequest, "文件上传失败")
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		writeError(c, http.StatusBadRequest, "请上传文件")
		return
	}
	defer file.Close()

	idStr := c.Param("id")
	kbID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "无效的知识库 ID")
		return
	}

	folderIDStr := c.Request.FormValue("folder_id")
	var folderID int64
	if folderIDStr != "" {
		folderID, _ = strconv.ParseInt(folderIDStr, 10, 64)
	}
	if folderID == 0 {
		folders, _ := store.GetFolderTree(kbID)
		for _, f := range folders {
			if f.ParentID == nil {
				folderID = f.ID
				break
			}
		}
	}

	autoClassify := c.Request.FormValue("auto_classify") == "true"

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(c, 500, "读取文件失败")
		return
	}

	tmpDir := os.TempDir()
	tmpFile := filepath.Join(tmpDir, header.Filename)
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		writeError(c, 500, "保存文件失败")
		return
	}

	taskID := uuid.New().String()
	if _, err := store.CreateImportTask(taskID, kbID, username); err != nil {
		os.Remove(tmpFile)
		writeError(c, 500, "创建导入任务失败")
		return
	}

	task := &knowledge.ImportTask{
		ID:           taskID,
		KbID:         kbID,
		FolderID:     folderID,
		Username:     username,
		FilePath:     tmpFile,
		FileName:     header.Filename,
		Data:         data,
		AutoClassify: autoClassify,
	}
	if err := knowledge.GlobalImportQueue.Enqueue(task); err != nil {
		os.Remove(tmpFile)
		writeError(c, 500, "导入队列已满")
		return
	}

	writeJSON(c, 200, gin.H{"success": true, "task_id": taskID})
}

// handleUserKBImportWeb URL 导入
func (s *Server) handleUserKBImportWeb(c *gin.Context) {
	username := s.requireRegularUser(c)
	if username == "" {
		return
	}

	var req struct {
		URL          string `json:"url" binding:"required"`
		KbID         int64  `json:"kb_id"`
		FolderID     int64  `json:"folder_id"`
		AutoClassify bool   `json:"auto_classify"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, 400, "参数错误: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "GET", req.URL, nil)
	if err != nil {
		writeError(c, 400, "无效的 URL")
		return
	}
	httpReq.Header.Set("User-Agent", "PicoAide-KB/1.0")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		writeError(c, 400, "抓取页面失败: "+err.Error())
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeError(c, 500, "读取响应失败")
		return
	}

	idStr := c.Param("id")
	kbID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "无效的知识库 ID")
		return
	}

	taskID := uuid.New().String()
	if _, err := store.CreateImportTask(taskID, kbID, username); err != nil {
		writeError(c, 500, "创建导入任务失败")
		return
	}

	urlStr := req.URL
	task := &knowledge.ImportTask{
		ID:           taskID,
		KbID:         kbID,
		FolderID:     req.FolderID,
		Username:     username,
		FileName:     extractURLFilename(urlStr, "page.html"),
		Data:         body,
		URL:          urlStr,
		AutoClassify: req.AutoClassify,
	}
	if err := knowledge.GlobalImportQueue.Enqueue(task); err != nil {
		store.UpdateImportTaskError(taskID, "队列已满")
		writeError(c, 429, "导入队列已满，请稍后重试")
		return
	}

	writeJSON(c, 200, gin.H{"success": true, "data": gin.H{"task_id": taskID}})
}

func extractURLFilename(rawURL, fallback string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fallback
	}
	p := u.Path
	if p == "" || p == "/" {
		return "index.html"
	}
	return path.Base(p)
}

// handleUserKBImportProgress 查询导入进度
func (s *Server) handleUserKBImportProgress(c *gin.Context) {
	if s.requireRegularUser(c) == "" {
		return
	}
	taskID := c.Param("task_id")
	task, err := store.GetImportTask(taskID)
	if err != nil {
		writeError(c, http.StatusNotFound, "导入任务不存在")
		return
	}
	writeJSON(c, 200, gin.H{"success": true, "data": task})
}
