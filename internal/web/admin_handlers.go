package web

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/picoaide/picoaide/internal/auth"
	"github.com/picoaide/picoaide/internal/config"
	dockerpkg "github.com/picoaide/picoaide/internal/docker"
	"github.com/picoaide/picoaide/internal/ldap"
	"github.com/picoaide/picoaide/internal/logger"
	"github.com/picoaide/picoaide/internal/user"
	"github.com/picoaide/picoaide/internal/util"
)

var gitMutex sync.Mutex

type skillRepoCredentialInput struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Mode     string `json:"mode"`
	Username string `json:"username"`
	Secret   string `json:"secret"`
}

type skillRepoInput struct {
	Name        string                     `json:"name"`
	URL         string                     `json:"url"`
	Ref         string                     `json:"ref"`
	RefType     string                     `json:"ref_type"`
	Public      bool                       `json:"public"`
	Credentials []skillRepoCredentialInput `json:"credentials"`
}

func normalizeSkillRepoCredentialInput(input skillRepoCredentialInput, fallbackName string) config.SkillRepoCredential {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = fallbackName
	}
	return config.SkillRepoCredential{
		Name:     name,
		Provider: strings.TrimSpace(input.Provider),
		Mode:     strings.ToLower(strings.TrimSpace(input.Mode)),
		Username: strings.TrimSpace(input.Username),
		Secret:   strings.TrimSpace(input.Secret),
	}
}

func normalizeSkillRepoRef(ref, refType string) (string, string) {
	ref = strings.TrimSpace(ref)
	refType = strings.ToLower(strings.TrimSpace(refType))
	if ref == "" {
		return "", ""
	}
	if refType != "tag" {
		refType = "branch"
	}
	return ref, refType
}

func inferSkillRepoCredentialMode(repoURL string) string {
	lower := strings.ToLower(strings.TrimSpace(repoURL))
	if strings.HasPrefix(lower, "git@") || strings.HasPrefix(lower, "ssh://") {
		return "ssh"
	}
	if strings.HasPrefix(lower, "http://") {
		return "http"
	}
	return "https"
}

func skillRepoDefaultUsername(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "gitlab":
		return "oauth2"
	case "gitee":
		return "git"
	default:
		return "x-access-token"
	}
}

func skillRepoFromInput(input skillRepoInput) (config.SkillRepo, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return config.SkillRepo{}, fmt.Errorf("仓库名称不能为空")
	}
	if err := util.SafePathSegment(name); err != nil {
		return config.SkillRepo{}, fmt.Errorf("仓库名称不合法: %w", err)
	}
	url := strings.TrimSpace(input.URL)
	if url == "" {
		return config.SkillRepo{}, fmt.Errorf("仓库地址不能为空")
	}
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "git@") && !strings.HasPrefix(url, "ssh://") {
		return config.SkillRepo{}, fmt.Errorf("仓库地址必须是 http://、https://、git@ 或 ssh:// 开头的 Git 地址")
	}
	ref, refType := normalizeSkillRepoRef(input.Ref, input.RefType)
	var creds []config.SkillRepoCredential
	for i, raw := range input.Credentials {
		cred := normalizeSkillRepoCredentialInput(raw, fmt.Sprintf("%s-%d", name, i+1))
		if cred.Name == "" {
			continue
		}
		cred.Mode = inferSkillRepoCredentialMode(url)
		if cred.Mode != "ssh" && cred.Mode != "http" && cred.Mode != "https" {
			return config.SkillRepo{}, fmt.Errorf("凭据 %s 的方式必须是 ssh/http/https", cred.Name)
		}
		if cred.Mode == "http" || cred.Mode == "https" {
			if cred.Username == "" {
				cred.Username = skillRepoDefaultUsername(cred.Provider)
			}
		}
		creds = append(creds, cred)
	}
	if !input.Public && len(creds) == 0 {
		return config.SkillRepo{}, fmt.Errorf("私有仓库至少需要配置一个凭据")
	}
	return config.SkillRepo{
		Name:        name,
		URL:         url,
		Ref:         ref,
		RefType:     refType,
		Public:      input.Public,
		Credentials: creds,
	}, nil
}

func skillRepoByName(repos []config.SkillRepo, name string) (config.SkillRepo, int, bool) {
	for i, repo := range repos {
		if repo.Name == name {
			return repo, i, true
		}
	}
	return config.SkillRepo{}, -1, false
}

func cloneGitRepoWithCredentials(reposDir, repoName, repoURL string, repo config.SkillRepo) (string, error) {
	destBase := filepath.Join(reposDir, repoName)
	if err := os.RemoveAll(destBase); err != nil {
		return "", err
	}
	cleanups := []func(){}
	defer func() {
		for _, fn := range cleanups {
			fn()
		}
	}()

	attempts := repo.Credentials
	if repo.Public || len(attempts) == 0 {
		attempts = []config.SkillRepoCredential{{Name: "public", Mode: "https"}}
	}

	var lastErr error
	for idx, cred := range attempts {
		tempDest := destBase
		if len(attempts) > 1 {
			tempDest = filepath.Join(reposDir, fmt.Sprintf("%s.tmp-%d-%d", repoName, time.Now().UnixNano(), idx))
			if err := os.RemoveAll(tempDest); err != nil {
				return "", err
			}
		}
		args := []string{"clone"}
		if repo.Ref != "" {
			args = append(args, "--branch", repo.Ref, "--single-branch")
		}
		args = append(args, repoURL, tempDest)
		out, err := gitCmdWithCredential(reposDir, cred, args...)
		if err == nil {
			if tempDest != destBase {
				if err := os.RemoveAll(destBase); err != nil {
					return "", err
				}
				if err := os.Rename(tempDest, destBase); err != nil {
					return "", err
				}
			}
			return out, nil
		}
		lastErr = err
		if tempDest != destBase {
			cleanups = append(cleanups, func() { _ = os.RemoveAll(tempDest) })
		}
	}
	return "", lastErr
}

func pullGitRepoWithCredentials(repoDir string, repo config.SkillRepo) (string, error) {
	attempts := repo.Credentials
	if repo.Public || len(attempts) == 0 {
		attempts = []config.SkillRepoCredential{{Name: "public", Mode: "https"}}
	}

	var lastErr error
	for _, cred := range attempts {
		var args []string
		if repo.RefType == "tag" && repo.Ref != "" {
			args = []string{"fetch", "--tags", "origin"}
			if _, err := gitCmdWithCredential(repoDir, cred, args...); err != nil {
				lastErr = err
				continue
			}
			if _, err := gitCmdWithCredential(repoDir, cred, "checkout", "-f", repo.Ref); err != nil {
				lastErr = err
				continue
			}
			if _, err := gitCmdWithCredential(repoDir, cred, "reset", "--hard", repo.Ref); err != nil {
				lastErr = err
				continue
			}
			return "tag refreshed", nil
		}
		if repo.Ref != "" {
			if _, err := gitCmdWithCredential(repoDir, cred, "fetch", "origin", repo.Ref); err != nil {
				lastErr = err
				continue
			}
			if _, err := gitCmdWithCredential(repoDir, cred, "checkout", "-B", repo.Ref, "origin/"+repo.Ref); err != nil {
				lastErr = err
				continue
			}
			if _, err := gitCmdWithCredential(repoDir, cred, "reset", "--hard", "origin/"+repo.Ref); err != nil {
				lastErr = err
				continue
			}
			return "branch refreshed", nil
		}
		args = []string{"pull", "--ff-only"}
		out, err := gitCmdWithCredential(repoDir, cred, args...)
		if err == nil {
			return out, nil
		}
		lastErr = err
	}
	return "", lastErr
}

func gitCmdWithCredential(dir string, cred config.SkillRepoCredential, args ...string) (string, error) {
	env := os.Environ()
	env = append(env, "GIT_TERMINAL_PROMPT=0")

	var cleanup func()
	switch cred.Mode {
	case "ssh":
		keyFile, err := os.CreateTemp("", "picoaide-git-key-*")
		if err != nil {
			return "", err
		}
		if _, err := keyFile.WriteString(cred.Secret); err != nil {
			keyFile.Close()
			os.Remove(keyFile.Name())
			return "", err
		}
		if err := keyFile.Chmod(0600); err != nil {
			keyFile.Close()
			os.Remove(keyFile.Name())
			return "", err
		}
		if err := keyFile.Close(); err != nil {
			os.Remove(keyFile.Name())
			return "", err
		}
		env = append(env, "GIT_SSH_COMMAND=ssh -i "+keyFile.Name()+" -o IdentitiesOnly=yes -o StrictHostKeyChecking=no")
		cleanup = func() { _ = os.Remove(keyFile.Name()) }
	default:
		script, err := os.CreateTemp("", "picoaide-git-askpass-*")
		if err != nil {
			return "", err
		}
		content := "#!/bin/sh\ncase \"$1\" in\n*Username*) printf '%s' \"${PICOAIDE_GIT_USERNAME:-" + skillRepoDefaultUsername(cred.Provider) + "}\" ;;\n*) printf '%s' \"${PICOAIDE_GIT_PASSWORD:-}\" ;;\nesac\n"
		if _, err := script.WriteString(content); err != nil {
			script.Close()
			os.Remove(script.Name())
			return "", err
		}
		if err := script.Chmod(0700); err != nil {
			script.Close()
			os.Remove(script.Name())
			return "", err
		}
		if err := script.Close(); err != nil {
			os.Remove(script.Name())
			return "", err
		}
		env = append(env, "GIT_ASKPASS="+script.Name())
		env = append(env, "PICOAIDE_GIT_USERNAME="+cred.Username)
		env = append(env, "PICOAIDE_GIT_PASSWORD="+cred.Secret)
		cleanup = func() { _ = os.Remove(script.Name()) }
	}
	if cleanup != nil {
		defer cleanup()
	}
	return gitCmdWithEnv(dir, env, args...)
}

func gitCmdWithEnv(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w\n%s", err, stderr.String())
	}
	return stdout.String(), nil
}

func cleanPathSegment(value string) (string, error) {
	cleaned := filepath.Base(strings.TrimSpace(value))
	if cleaned != strings.TrimSpace(value) {
		return "", fmt.Errorf("名称不合法")
	}
	if err := util.SafePathSegment(cleaned); err != nil {
		return "", err
	}
	return cleaned, nil
}

func copyDirBetweenRoots(source *os.Root, sourceDir string, target *os.Root, targetDir string) error {
	return fs.WalkDir(source.FS(), sourceDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(targetDir, relPath)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return target.MkdirAll(targetPath, info.Mode())
		}
		in, err := source.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := target.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return err
		}
		out, err := target.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

func copySkillZipContents(reader *zip.Reader, skillName string) error {
	skillDir := config.SkillsDirPath()
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return err
	}
	targetDir := filepath.Join(skillDir, skillName)
	if err := os.RemoveAll(targetDir); err != nil {
		return err
	}

	tempDir, err := os.MkdirTemp(filepath.Dir(skillDir), ".skill-upload-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	prefix := commonZipRootDir(reader.File)
	for _, file := range reader.File {
		name := strings.TrimPrefix(filepath.ToSlash(file.Name), "/")
		if name == "" || strings.HasSuffix(name, "/") {
			continue
		}
		if prefix != "" {
			name = strings.TrimPrefix(name, prefix+"/")
		}
		if name == "" {
			continue
		}
		cleanName := filepath.Clean(filepath.FromSlash(name))
		if cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) || filepath.IsAbs(cleanName) {
			return fmt.Errorf("zip 包包含不安全路径: %s", file.Name)
		}
		targetPath := filepath.Join(tempDir, cleanName)
		if !strings.HasPrefix(targetPath, tempDir+string(os.PathSeparator)) && targetPath != tempDir {
			return fmt.Errorf("zip 包包含不安全路径: %s", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, file.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, file.Mode())
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		closeErr := dst.Close()
		src.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err := os.Rename(tempDir, targetDir); err != nil {
		return err
	}
	return nil
}

func commonZipRootDir(files []*zip.File) string {
	root := ""
	for _, file := range files {
		name := strings.Trim(filepath.ToSlash(file.Name), "/")
		if name == "" {
			continue
		}
		first := strings.SplitN(name, "/", 2)[0]
		if root == "" {
			root = first
			continue
		}
		if root != first {
			return ""
		}
	}
	return root
}

func syncGitRepoToSkill(repoName string) error {
	repoDir := filepath.Join(skillReposDir(), repoName)
	skillDir := config.SkillsDirPath()
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return err
	}
	targetDir := filepath.Join(skillDir, repoName)
	if err := os.RemoveAll(targetDir); err != nil {
		return err
	}

	tempDir, err := os.MkdirTemp(filepath.Dir(skillDir), ".skill-git-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	err = filepath.WalkDir(repoDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(repoDir, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}
		relSlash := filepath.ToSlash(relPath)
		if relSlash == ".git" || strings.HasPrefix(relSlash, ".git/") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		targetPath := filepath.Join(tempDir, relPath)
		if entry.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		closeErr := dst.Close()
		src.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return err
	}
	return os.Rename(tempDir, targetDir)
}

func (s *Server) requireSuperadmin(c *gin.Context) string {
	username := s.requireAuth(c)
	if username == "" {
		return ""
	}
	if !auth.IsSuperadmin(username) {
		writeError(c, http.StatusForbidden, "仅超级管理员可访问")
		return ""
	}
	return username
}

// ============================================================
// 用户管理
// ============================================================

func (s *Server) handleAdminUsers(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "GET" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
		return
	}

	containers, err := auth.GetAllContainers()
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}

	// 本地用户（local_users 表）
	localUsers, _ := auth.GetAllLocalUsers()
	localRoleMap := make(map[string]string)
	for _, u := range localUsers {
		localRoleMap[u.Username] = u.Role
	}

	type UserInfo struct {
		Username   string `json:"username"`
		Status     string `json:"status"`
		ImageTag   string `json:"image_tag"`
		ImageReady bool   `json:"image_ready"`
		IP         string `json:"ip"`
		Role       string `json:"role"`
	}

	ctx := context.Background()

	// 按 username 索引容器记录
	containerMap := make(map[string]*auth.ContainerRecord)
	for i := range containers {
		containerMap[containers[i].Username] = &containers[i]
	}

	var list []UserInfo

	// 先输出所有有容器记录的用户
	seen := make(map[string]bool)
	for _, c := range containers {
		seen[c.Username] = true
		status := c.Status
		if c.ContainerID != "" {
			status = dockerpkg.ContainerStatus(ctx, c.ContainerID)
		}

		imageRef := c.Image
		imageReady := imageRef != "" && dockerpkg.ImageExists(ctx, imageRef)
		imageTag := imageRef
		if parts := strings.SplitN(imageRef, ":", 2); len(parts) == 2 {
			imageTag = parts[1]
		}

		role := localRoleMap[c.Username]

		list = append(list, UserInfo{
			Username:   c.Username,
			Status:     status,
			ImageTag:   imageTag,
			ImageReady: imageReady,
			IP:         c.IP,
			Role:       role,
		})
	}

	// 补上本地用户中没有容器记录的（如超管）
	for _, u := range localUsers {
		if !seen[u.Username] {
			list = append(list, UserInfo{
				Username: u.Username,
				Status:   "未初始化",
				Role:     u.Role,
			})
		}
	}

	if list == nil {
		list = []UserInfo{}
	}

	writeJSON(c, http.StatusOK, struct {
		Success     bool       `json:"success"`
		Users       []UserInfo `json:"users"`
		AuthMode    string     `json:"auth_mode"`
		UnifiedAuth bool       `json:"unified_auth"`
	}{true, list, s.cfg.AuthMode(), s.cfg.UnifiedAuthEnabled()})
}

// ============================================================
// 用户创建与删除（仅本地模式）
// ============================================================

func (s *Server) handleAdminUserCreate(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if s.cfg.UnifiedAuthEnabled() {
		writeError(c, http.StatusForbidden, "统一认证模式下不允许手动创建用户")
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	username := c.PostForm("username")
	if err := user.ValidateUsername(username); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if auth.UserExists(username) {
		writeError(c, http.StatusBadRequest, "用户 "+username+" 已存在")
		return
	}

	password := auth.GenerateRandomPassword(12)
	if err := auth.CreateUser(username, password, "user"); err != nil {
		writeError(c, http.StatusInternalServerError, "创建用户失败: "+err.Error())
		return
	}

	// 获取镜像标签参数，未指定时自动使用本地最新标签
	imageTag := c.PostForm("image_tag")
	if imageTag == "" && s.dockerAvailable {
		ctx := contextWithTimeout(10)
		localTags, err := dockerpkg.ListLocalTags(ctx, s.cfg.Image.Name)
		if err == nil && len(localTags) > 0 {
			imageTag = localTags[len(localTags)-1]
		}
	}
	if imageTag == "" {
		auth.DeleteUser(username)
		writeError(c, http.StatusBadRequest, "未指定镜像标签且本地无可用镜像，请先拉取镜像")
		return
	}

	// 创建用户容器目录
	if err := user.InitUser(s.cfg, username, imageTag); err != nil {
		// 回滚：删除已创建的用户记录
		auth.DeleteUser(username)
		writeError(c, http.StatusInternalServerError, "初始化用户目录失败: "+err.Error())
		return
	}

	// 异步启动容器并下发配置
	if s.dockerAvailable {
		go s.autoStartUserContainer(username)
	}

	logger.Audit("user.create", "username", username, "operator", s.getSessionUser(c))
	writeJSON(c, http.StatusOK, struct {
		Success  bool   `json:"success"`
		Message  string `json:"message"`
		Username string `json:"username"`
		Password string `json:"password"`
	}{true, "用户创建成功，容器启动中", username, password})
}

func (s *Server) handleAdminUserDelete(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if s.cfg.UnifiedAuthEnabled() {
		writeError(c, http.StatusForbidden, "统一认证模式下不允许删除用户")
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	username := c.PostForm("username")
	if username == "" {
		writeError(c, http.StatusBadRequest, "用户名不能为空")
		return
	}
	if err := user.ValidateUsername(username); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 停止并移除容器
	rec, _ := auth.GetContainerByUsername(username)
	if rec != nil && rec.ContainerID != "" {
		ctx := context.Background()
		_ = dockerpkg.Remove(ctx, rec.ContainerID)
	}
	auth.DeleteContainer(username)

	// 归档用户目录
	if err := user.ArchiveUser(s.cfg, username); err != nil {
		writeError(c, http.StatusInternalServerError, "归档用户目录失败: "+err.Error())
		return
	}

	// 删除本地用户记录
	if err := auth.DeleteUser(username); err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Audit("user.delete", "username", username, "operator", s.getSessionUser(c))
	writeSuccess(c, "用户 "+username+" 已删除并归档")
}

// ============================================================
// 容器操作
// ============================================================

func (s *Server) handleAdminContainerStart(c *gin.Context) {
	s.handleContainerAction(c, "start")
}
func (s *Server) handleAdminContainerStop(c *gin.Context) {
	s.handleContainerAction(c, "stop")
}
func (s *Server) handleAdminContainerRestart(c *gin.Context) {
	s.handleContainerAction(c, "restart")
}
func (s *Server) handleAdminContainerDebug(c *gin.Context) {
	s.handleContainerAction(c, "debug")
}

func (s *Server) handleContainerAction(c *gin.Context, action string) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if !s.dockerAvailable {
		writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用，请联系管理员")
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	username := c.PostForm("username")
	if username == "" {
		writeError(c, http.StatusBadRequest, "用户名不能为空")
		return
	}
	if err := user.ValidateUsername(username); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	rec, err := auth.GetContainerByUsername(username)
	if err != nil || rec == nil {
		writeError(c, http.StatusBadRequest, "用户 "+username+" 未初始化")
		return
	}

	ctx := context.Background()

	// 启动/重启前检查镜像是否存在
	if action == "start" || action == "restart" || action == "debug" {
		if rec.Image == "" || !dockerpkg.ImageExists(ctx, rec.Image) {
			writeError(c, http.StatusBadRequest, "容器镜像 "+rec.Image+" 不存在，请先拉取镜像")
			return
		}
	}

	switch action {
	case "start":
		// 如果容器不存在，创建容器
		if rec.ContainerID == "" || !dockerpkg.ContainerExists(ctx, username) {
			ud := user.UserDir(s.cfg, username)
			cid, createErr := dockerpkg.CreateContainer(ctx, username, rec.Image, ud, rec.IP, rec.CPULimit, rec.MemoryLimit)
			if createErr != nil {
				writeError(c, http.StatusInternalServerError, "创建容器失败: "+createErr.Error())
				return
			}
			auth.UpdateContainerID(username, cid)
			rec.ContainerID = cid
		}
		picoclawDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw")
		configPath := filepath.Join(picoclawDir, "config.json")
		shouldApplyAfterStart := false
		if _, err := os.Stat(configPath); err == nil {
			if err := s.applyConfigForImage(username, rec.Image); err != nil {
				writeError(c, http.StatusInternalServerError, "下发配置失败: "+err.Error())
				return
			}
		} else {
			shouldApplyAfterStart = true
		}
		if err := dockerpkg.Start(ctx, rec.ContainerID); err != nil {
			writeError(c, http.StatusInternalServerError, err.Error())
			return
		}
		auth.UpdateContainerStatus(username, "running")
		if shouldApplyAfterStart {
			go s.applyConfigAsync(username, picoclawDir, rec.ContainerID)
		}

	case "stop":
		// 停止并删除容器（类似 docker compose down）
		if rec.ContainerID != "" {
			_ = dockerpkg.Stop(ctx, rec.ContainerID)
			_ = dockerpkg.Remove(ctx, rec.ContainerID)
		}
		auth.UpdateContainerID(username, "")
		auth.UpdateContainerStatus(username, "stopped")

	case "restart":
		// 停止+删除旧容器，重新创建并启动
		if rec.ContainerID != "" {
			_ = dockerpkg.Stop(ctx, rec.ContainerID)
			_ = dockerpkg.Remove(ctx, rec.ContainerID)
			auth.UpdateContainerID(username, "")
		}
		ud := user.UserDir(s.cfg, username)
		cid, createErr := dockerpkg.CreateContainer(ctx, username, rec.Image, ud, rec.IP, rec.CPULimit, rec.MemoryLimit)
		if createErr != nil {
			writeError(c, http.StatusInternalServerError, "创建容器失败: "+createErr.Error())
			return
		}
		auth.UpdateContainerID(username, cid)
		picoclawDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw")
		configPath := filepath.Join(picoclawDir, "config.json")
		shouldApplyAfterStart := false
		if _, err := os.Stat(configPath); err == nil {
			if err := s.applyConfigForImage(username, rec.Image); err != nil {
				writeError(c, http.StatusInternalServerError, "下发配置失败: "+err.Error())
				return
			}
		} else {
			shouldApplyAfterStart = true
		}
		if err := dockerpkg.Start(ctx, cid); err != nil {
			writeError(c, http.StatusInternalServerError, err.Error())
			return
		}
		auth.UpdateContainerStatus(username, "running")
		if shouldApplyAfterStart {
			go s.applyConfigAsync(username, picoclawDir, cid)
		}

	case "debug":
		// 停止+删除旧容器，重新创建并以 Picoclaw debug 模式启动
		if rec.ContainerID != "" {
			_ = dockerpkg.Stop(ctx, rec.ContainerID)
			_ = dockerpkg.Remove(ctx, rec.ContainerID)
			auth.UpdateContainerID(username, "")
		}
		ud := user.UserDir(s.cfg, username)
		cid, createErr := dockerpkg.CreateContainerWithOptions(ctx, username, rec.Image, ud, rec.IP, rec.CPULimit, rec.MemoryLimit, true)
		if createErr != nil {
			writeError(c, http.StatusInternalServerError, "创建调试容器失败: "+createErr.Error())
			return
		}
		auth.UpdateContainerID(username, cid)
		picoclawDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw")
		configPath := filepath.Join(picoclawDir, "config.json")
		shouldApplyAfterStart := false
		if _, err := os.Stat(configPath); err == nil {
			if err := s.applyConfigForImage(username, rec.Image); err != nil {
				writeError(c, http.StatusInternalServerError, "下发配置失败: "+err.Error())
				return
			}
		} else {
			shouldApplyAfterStart = true
		}
		if err := dockerpkg.Start(ctx, cid); err != nil {
			writeError(c, http.StatusInternalServerError, err.Error())
			return
		}
		auth.UpdateContainerStatus(username, "running")
		if shouldApplyAfterStart {
			go s.applyConfigAsync(username, picoclawDir, cid)
		}
	}

	logger.Audit("container."+action, "username", username, "operator", s.getSessionUser(c))
	labels := map[string]string{"start": "启动", "stop": "停止", "restart": "重启", "debug": "调试启动"}
	writeSuccess(c, fmt.Sprintf("容器已%s", labels[action]))
}

// autoStartUserContainer 为新创建的用户自动启动容器并下发配置
func (s *Server) autoStartUserContainer(username string) {
	rec, err := auth.GetContainerByUsername(username)
	if err != nil || rec == nil || rec.Image == "" {
		slog.Warn("无容器记录或镜像未配置，跳过自动启动", "username", username)
		return
	}

	ctx := context.Background()
	if !dockerpkg.ImageExists(ctx, rec.Image) {
		slog.Warn("镜像不存在，跳过自动启动", "username", username, "image", rec.Image)
		return
	}

	ud := user.UserDir(s.cfg, username)
	cid, createErr := dockerpkg.CreateContainer(ctx, username, rec.Image, ud, rec.IP, rec.CPULimit, rec.MemoryLimit)
	if createErr != nil {
		slog.Error("创建容器失败", "username", username, "error", createErr)
		return
	}
	auth.UpdateContainerID(username, cid)

	if err := dockerpkg.Start(ctx, cid); err != nil {
		slog.Error("启动容器失败", "username", username, "error", err)
		return
	}
	auth.UpdateContainerStatus(username, "running")
	slog.Info("容器已自动启动", "username", username)

	picoclawDir := filepath.Join(ud, ".picoclaw")
	s.applyConfigAsync(username, picoclawDir, cid)
}

// applyConfigAsync 异步等待 config.json 生成后下发配置并重启容器
func (s *Server) applyConfigAsync(username, picoclawDir, containerID string) {
	configPath := filepath.Join(picoclawDir, "config.json")
	slog.Info("等待 config.json 生成", "username", username)

	// 轮询等待 config.json 出现，最多 60 秒
	for i := 0; i < 30; i++ {
		time.Sleep(2 * time.Second)
		if _, err := os.Stat(configPath); err == nil {
			break
		}
		if i == 29 {
			slog.Warn("等待 config.json 超时", "username", username)
			return
		}
	}

	// 下发配置
	rec, _ := auth.GetContainerByUsername(username)
	targetTag := ""
	if rec != nil {
		targetTag = imageTagFromRef(rec.Image)
	}
	if err := user.ApplyConfigToJSONForTag(s.cfg, picoclawDir, username, targetTag); err != nil {
		slog.Error("下发配置失败", "username", username, "error", err)
		return
	}
	if err := user.ApplySecurityToYAML(s.cfg, picoclawDir); err != nil {
		slog.Error("下发安全配置失败", "username", username, "error", err)
	}
	slog.Info("配置已下发", "username", username)

	// 重启容器使配置生效
	ctx := context.Background()
	if err := dockerpkg.Restart(ctx, containerID); err != nil {
		slog.Error("重启容器失败", "username", username, "error", err)
		return
	}
	slog.Info("容器已重启，配置生效", "username", username)
}

func (s *Server) applyConfigForImage(username string, imageRef string) error {
	picoclawDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw")
	if err := os.MkdirAll(picoclawDir, 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	if err := user.ApplyConfigToJSONForTag(s.cfg, picoclawDir, username, imageTagFromRef(imageRef)); err != nil {
		return fmt.Errorf("下发配置失败: %w", err)
	}
	if err := user.ApplySecurityToYAML(s.cfg, picoclawDir); err != nil {
		slog.Error("下发安全配置失败", "username", username, "error", err)
	}
	return nil
}

func (s *Server) applyConfigForUpgrade(username string, fromTag string, targetTag string) error {
	picoclawDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw")
	if err := os.MkdirAll(picoclawDir, 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	if err := user.ApplyConfigToJSONWithMigration(s.cfg, picoclawDir, username, fromTag, targetTag); err != nil {
		return fmt.Errorf("下发配置失败: %w", err)
	}
	if err := user.ApplySecurityToYAML(s.cfg, picoclawDir); err != nil {
		slog.Error("下发安全配置失败", "username", username, "error", err)
	}
	return nil
}

// applyConfigToUser 向单个用户下发配置并重启容器
func (s *Server) applyConfigToUser(username string) error {
	picoclawDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw")
	configPath := filepath.Join(picoclawDir, "config.json")

	// config.json 不存在说明容器还没启动过，跳过
	if _, err := os.Stat(configPath); err != nil {
		return fmt.Errorf("config.json 不存在")
	}

	rec, err := auth.GetContainerByUsername(username)
	targetTag := ""
	if rec != nil {
		targetTag = imageTagFromRef(rec.Image)
	}
	if err := user.ApplyConfigToJSONForTag(s.cfg, picoclawDir, username, targetTag); err != nil {
		return fmt.Errorf("下发配置失败: %w", err)
	}
	if err := user.ApplySecurityToYAML(s.cfg, picoclawDir); err != nil {
		slog.Error("下发安全配置失败", "username", username, "error", err)
	}

	// 重启容器使配置生效
	if err != nil || rec == nil || rec.ContainerID == "" {
		return fmt.Errorf("容器记录不存在")
	}
	if rec.Status != "running" {
		return nil // 容器未运行，下次启动时自动下发
	}
	ctx := context.Background()
	if err := dockerpkg.Restart(ctx, rec.ContainerID); err != nil {
		return fmt.Errorf("重启失败: %w", err)
	}
	return nil
}

// handleAdminConfigApply 下发配置到指定用户/组/全部用户并重启容器
func (s *Server) handleAdminConfigApply(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	username := c.PostForm("username")
	group := c.PostForm("group")

	var targets []string
	var err error

	switch {
	case username != "":
		if err := user.ValidateUsername(username); err != nil {
			writeError(c, http.StatusBadRequest, err.Error())
			return
		}
		targets = []string{username}
	case group != "":
		targets, err = auth.GetGroupMembersForDeploy(group)
		if err != nil {
			writeError(c, http.StatusBadRequest, "获取组成员失败: "+err.Error())
			return
		}
		if len(targets) == 0 {
			writeError(c, http.StatusBadRequest, "组 "+group+" 没有成员")
			return
		}
	default:
		// 不指定用户和组时，下发到所有用户
		targets, err = user.GetUserList(s.cfg)
		if err != nil {
			writeError(c, http.StatusInternalServerError, "获取用户列表失败: "+err.Error())
			return
		}
	}

	// 单个用户直接同步执行
	if len(targets) == 1 {
		if err := s.applyConfigToUser(targets[0]); err != nil {
			writeError(c, http.StatusInternalServerError, err.Error())
		} else {
			writeSuccess(c, "配置已下发并重启")
		}
		return
	}

	// 多个用户走队列
	applyFn := func(u string) error {
		return s.applyConfigToUser(u)
	}
	taskID, err := enqueueTask("config-apply", targets, applyFn)
	if err != nil {
		writeError(c, http.StatusConflict, err.Error())
		return
	}
	writeJSON(c, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("已提交配置下发任务，共 %d 个用户", len(targets)),
		"task_id": taskID,
	})
}

func (s *Server) handleAdminMigrationRulesRefresh(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}
	if err := user.RefreshPicoClawMigrationRulesFromAdapter(config.RuleCacheDir(), config.PicoClawAdapterRemoteBaseURL()); err != nil {
		writeError(c, http.StatusBadGateway, "更新迁移规则失败: "+err.Error())
		return
	}
	info, err := user.LoadPicoClawMigrationRulesInfo(config.RuleCacheDir())
	if err != nil {
		writeError(c, http.StatusInternalServerError, "迁移规则已更新，但读取本地规则失败: "+err.Error())
		return
	}
	writeJSON(c, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "迁移规则已更新",
		"rules":   info,
	})
}

func (s *Server) handleAdminMigrationRulesGet(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "GET" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
		return
	}
	info, err := user.LoadPicoClawMigrationRulesInfo(config.RuleCacheDir())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(c, http.StatusOK, map[string]interface{}{
		"success": true,
		"rules":   info,
	})
}

func (s *Server) handleAdminMigrationRulesUpload(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	if file, _, err := c.Request.FormFile("file"); err == nil {
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, 16<<20))
		if err != nil {
			writeError(c, http.StatusBadRequest, "读取上传文件失败: "+err.Error())
			return
		}
		if _, err := user.SavePicoClawAdapterZip(config.RuleCacheDir(), data); err != nil {
			writeError(c, http.StatusBadRequest, "配置适配包校验失败: "+err.Error())
			return
		}
		info, err := user.LoadPicoClawMigrationRulesInfo(config.RuleCacheDir())
		if err != nil {
			writeError(c, http.StatusInternalServerError, "配置适配包已导入，但读取本地规则失败: "+err.Error())
			return
		}
		writeJSON(c, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "配置适配包已导入",
			"rules":   info,
		})
		return
	}
	writeError(c, http.StatusBadRequest, "请上传配置适配 zip 包")
}

// handleAdminTaskStatus 返回当前任务队列状态
func (s *Server) handleAdminTaskStatus(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "GET" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
		return
	}
	status := getTaskStatus()
	writeJSON(c, http.StatusOK, status)
}

// handleAdminContainerLogs 返回容器日志
func (s *Server) handleAdminContainerLogs(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "GET" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
		return
	}
	if !s.dockerAvailable {
		writeError(c, http.StatusServiceUnavailable, "Docker 服务不可用")
		return
	}

	username := c.Query("username")
	if username == "" {
		writeError(c, http.StatusBadRequest, "用户名不能为空")
		return
	}
	if err := user.ValidateUsername(username); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	rec, err := auth.GetContainerByUsername(username)
	if err != nil || rec == nil {
		writeError(c, http.StatusBadRequest, "用户 "+username+" 未初始化")
		return
	}
	if rec.ContainerID == "" {
		writeError(c, http.StatusBadRequest, "容器未创建")
		return
	}

	tail := c.Query("tail")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logs, err := dockerpkg.ContainerLogs(ctx, rec.ContainerID, tail)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(c, http.StatusOK, struct {
		Success  bool   `json:"success"`
		Username string `json:"username"`
		Logs     string `json:"logs"`
	}{
		Success:  true,
		Username: username,
		Logs:     logs,
	})
}

// ============================================================
// 认证配置 — LDAP 测试 & 用户搜索
// ============================================================

func (s *Server) handleAdminAuthTestLDAP(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	host := c.PostForm("host")
	bindDN := c.PostForm("bind_dn")
	bindPassword := c.PostForm("bind_password")
	baseDN := c.PostForm("base_dn")
	filter := c.PostForm("filter")
	usernameAttr := c.PostForm("username_attribute")
	groupSearchMode := c.PostForm("group_search_mode")
	groupBaseDN := c.PostForm("group_base_dn")
	groupFilter := c.PostForm("group_filter")
	groupMemberAttr := c.PostForm("group_member_attribute")

	if host == "" || bindDN == "" || baseDN == "" {
		writeError(c, http.StatusBadRequest, "LDAP 地址、Bind DN 和 Base DN 不能为空")
		return
	}

	users, err := ldap.TestConnection(host, bindDN, bindPassword, baseDN, filter, usernameAttr)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 测试组查询（失败不影响用户测试结果）
	var groupError string
	groups, gerr := ldap.TestGroups(host, bindDN, bindPassword, baseDN, groupSearchMode, groupBaseDN, groupFilter, groupMemberAttr, usernameAttr)
	if gerr != nil {
		groupError = gerr.Error()
	}
	if groups == nil {
		groups = []ldap.GroupPreview{}
	}

	writeJSON(c, http.StatusOK, struct {
		Success    bool                `json:"success"`
		Message    string              `json:"message"`
		UserCount  int                 `json:"user_count"`
		Users      []string            `json:"users"`
		Groups     []ldap.GroupPreview `json:"groups"`
		GroupError string              `json:"group_error"`
	}{true, fmt.Sprintf("连接成功，找到 %d 个用户", len(users)), len(users), users, groups, groupError})
}

func (s *Server) handleAdminAuthLDAPUsers(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "GET" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
		return
	}

	users, err := ldap.FetchUsers(s.cfg)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if users == nil {
		users = []string{}
	}
	writeJSON(c, http.StatusOK, struct {
		Success bool     `json:"success"`
		Users   []string `json:"users"`
	}{true, users})
}

// ============================================================
// 白名单管理
// ============================================================

// handleAdminWhitelistGet 返回白名单列表
func (s *Server) handleAdminWhitelistGet(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	whitelist, err := user.LoadWhitelist()
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	var users []string
	if whitelist != nil {
		for u := range whitelist {
			users = append(users, u)
		}
		sort.Strings(users)
	}
	if users == nil {
		users = []string{}
	}
	writeJSON(c, http.StatusOK, struct {
		Success bool     `json:"success"`
		Users   []string `json:"users"`
	}{true, users})
}

// handleAdminWhitelistPost 更新白名单
func (s *Server) handleAdminWhitelistPost(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}
	usersStr := c.PostForm("users")
	var users []string
	if usersStr != "" {
		for _, u := range strings.Split(usersStr, ",") {
			u = strings.TrimSpace(u)
			if u != "" {
				users = append(users, u)
			}
		}
	}
	sort.Strings(users)

	// 写入数据库（使用 xorm）
	engine, err := auth.GetEngine()
	if err != nil {
		writeError(c, http.StatusInternalServerError, "数据库连接失败")
		return
	}
	session := engine.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	session.Exec("DELETE FROM whitelist")
	for _, u := range users {
		session.Exec("INSERT OR IGNORE INTO whitelist (username, added_by) VALUES (?, ?)", u, s.getSessionUser(c))
	}
	if err := session.Commit(); err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(c, "白名单已更新")
	logger.Audit("whitelist.update", "count", len(users), "operator", s.getSessionUser(c))
}

// ============================================================
// 技能库管理
// ============================================================

// skillReposDir 技能仓库克隆区路径
func skillReposDir() string {
	wd, _ := os.Getwd()
	return filepath.Join(wd, "skill-repos")
}

func (s *Server) handleAdminSkills(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "GET" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
		return
	}

	type SkillInfo struct {
		Name      string `json:"name"`
		FileCount int    `json:"file_count"`
		Size      int64  `json:"size"`
		SizeStr   string `json:"size_str"`
		ModTime   string `json:"mod_time"`
	}

	var skills []SkillInfo
	skillDir := config.SkillsDirPath()
	entries, err := os.ReadDir(skillDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			info, ierr := e.Info()
			if ierr != nil {
				continue
			}
			fullPath := filepath.Join(skillDir, e.Name())
			var fileCount int
			var totalSize int64
			filepath.WalkDir(fullPath, func(_ string, d os.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if !d.IsDir() {
					fileCount++
					if fi, e := d.Info(); e == nil {
						totalSize += fi.Size()
					}
				}
				return nil
			})
			skills = append(skills, SkillInfo{
				Name:      e.Name(),
				FileCount: fileCount,
				Size:      totalSize,
				SizeStr:   formatSize(totalSize),
				ModTime:   info.ModTime().Format("2006-01-02 15:04"),
			})
		}
	}
	if skills == nil {
		skills = []SkillInfo{}
	}

	writeJSON(c, http.StatusOK, struct {
		Success bool               `json:"success"`
		Skills  []SkillInfo        `json:"skills"`
		Repos   []config.SkillRepo `json:"repos"`
	}{true, skills, s.cfg.Skills.Repos})
}

func (s *Server) handleAdminSkillsDeploy(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	skillName := strings.TrimSpace(c.PostForm("skill_name"))
	targetUser := strings.TrimSpace(c.PostForm("username"))
	targetGroup := strings.TrimSpace(c.PostForm("group_name"))

	if skillName != "" {
		if err := util.SafePathSegment(skillName); err != nil {
			writeError(c, http.StatusBadRequest, "技能名称不合法: "+err.Error())
			return
		}
	}
	if targetUser != "" {
		if err := user.ValidateUsername(targetUser); err != nil {
			writeError(c, http.StatusBadRequest, err.Error())
			return
		}
	}

	skillDir := config.SkillsDirPath()
	entries, err := os.ReadDir(skillDir)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "读取技能目录失败: "+err.Error())
		return
	}

	var deploySkills []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if skillName == "" || e.Name() == skillName {
			deploySkills = append(deploySkills, e.Name())
		}
	}
	if len(deploySkills) == 0 {
		writeError(c, http.StatusBadRequest, "没有找到可部署的技能")
		return
	}

	deployFn := func(username string) error {
		targetSkillsDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw", "workspace", "skills")
		for _, sn := range deploySkills {
			srcPath := filepath.Join(skillDir, sn)
			dstPath := filepath.Join(targetSkillsDir, sn)
			if err := util.CopyDir(srcPath, dstPath); err != nil {
				return fmt.Errorf("复制技能 %s 失败: %w", sn, err)
			}
		}
		return nil
	}

	if targetUser != "" {
		// 单个用户直接执行
		if err := deployFn(targetUser); err != nil {
			writeError(c, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(c, http.StatusOK, map[string]interface{}{
			"success":     true,
			"message":     fmt.Sprintf("已将 %d 个技能部署到 %s", len(deploySkills), targetUser),
			"skill_count": len(deploySkills),
			"user_count":  1,
		})
		return
	}

	// 组或全部用户走队列
	var targets []string
	if targetGroup != "" {
		targets, err = auth.GetGroupMembersForDeploy(targetGroup)
		if err != nil {
			writeError(c, http.StatusBadRequest, "获取组成员失败: "+err.Error())
			return
		}
	} else {
		targets, err = user.GetUserList(s.cfg)
		if err != nil {
			writeError(c, http.StatusInternalServerError, "获取用户列表失败: "+err.Error())
			return
		}
	}

	taskID, err := enqueueTask("skills-deploy", targets, deployFn)
	if err != nil {
		writeError(c, http.StatusConflict, err.Error())
		return
	}
	writeJSON(c, http.StatusOK, map[string]interface{}{
		"success":     true,
		"message":     fmt.Sprintf("已提交技能部署任务，共 %d 个用户", len(targets)),
		"task_id":     taskID,
		"skill_count": len(deploySkills),
	})
}

func (s *Server) handleAdminSkillsDownload(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "GET" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
		return
	}
	name := c.Query("name")
	if name == "" {
		writeError(c, http.StatusBadRequest, "技能名称不能为空")
		return
	}
	if err := util.SafePathSegment(name); err != nil {
		writeError(c, http.StatusBadRequest, "技能名称不合法")
		return
	}
	skillPath := filepath.Join(config.SkillsDirPath(), name)
	if _, err := os.Stat(skillPath); err != nil {
		writeError(c, http.StatusNotFound, "技能不存在")
		return
	}
	w := c.Writer
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, name))
	zw := zip.NewWriter(w)
	filepath.WalkDir(skillPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		relPath, _ := filepath.Rel(skillPath, path)
		// 安全检查：防止 zip slip — 确保相对路径不以 .. 开头
		relPath = filepath.ToSlash(relPath)
		if strings.HasPrefix(relPath, "../") || relPath == ".." {
			return nil
		}
		if d.IsDir() {
			zw.Create(relPath + "/")
			return nil
		}
		fw, err := zw.Create(relPath)
		if err != nil {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		io.Copy(fw, f)
		return nil
	})
	zw.Close()
}

// handleAdminSkillsRemove 从 skill/ 删除技能
func (s *Server) handleAdminSkillsRemove(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		writeError(c, http.StatusBadRequest, "技能名称不能为空")
		return
	}
	if err := util.SafePathSegment(name); err != nil {
		writeError(c, http.StatusBadRequest, "技能名称不合法")
		return
	}
	skillPath := filepath.Join(config.SkillsDirPath(), name)
	if err := os.RemoveAll(skillPath); err != nil {
		writeError(c, http.StatusInternalServerError, "删除失败: "+err.Error())
		return
	}
	writeSuccess(c, "技能已删除")
}

// handleAdminSkillsUpload 从 zip 包安装一个技能
func (s *Server) handleAdminSkillsUpload(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	skillName, err := cleanPathSegment(c.PostForm("name"))
	if skillName == "" {
		writeError(c, http.StatusBadRequest, "技能名称不能为空")
		return
	}
	if err != nil {
		writeError(c, http.StatusBadRequest, "技能名称不合法")
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		writeError(c, http.StatusBadRequest, "请上传技能 zip 包")
		return
	}
	defer file.Close()
	if header != nil && !strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
		writeError(c, http.StatusBadRequest, "仅支持 zip 压缩包")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, 128<<20))
	if err != nil {
		writeError(c, http.StatusBadRequest, "读取上传文件失败: "+err.Error())
		return
	}
	if len(data) == 0 {
		writeError(c, http.StatusBadRequest, "上传文件为空")
		return
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		writeError(c, http.StatusBadRequest, "zip 包格式错误: "+err.Error())
		return
	}
	if err := copySkillZipContents(reader, skillName); err != nil {
		writeError(c, http.StatusBadRequest, "安装技能失败: "+err.Error())
		return
	}
	writeSuccess(c, "技能已上传并安装")
}

// ============================================================
// 技能仓库管理（多仓库）
// ============================================================

// handleAdminSkillsReposAdd 添加并克隆技能仓库
func (s *Server) handleAdminSkillsReposAdd(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	var input skillRepoInput
	if err := json.Unmarshal([]byte(c.PostForm("repo")), &input); err != nil {
		input.Name = c.PostForm("name")
		input.URL = c.PostForm("url")
		input.Ref = c.PostForm("ref")
		input.RefType = c.PostForm("ref_type")
		input.Public, _ = strconv.ParseBool(c.PostForm("public"))
	}
	repo, err := skillRepoFromInput(input)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	gitMutex.Lock()
	defer gitMutex.Unlock()

	if _, err := exec.LookPath("git"); err != nil {
		writeError(c, http.StatusInternalServerError, "Git 未安装")
		return
	}

	if _, _, ok := skillRepoByName(s.cfg.Skills.Repos, repo.Name); ok {
		writeError(c, http.StatusBadRequest, "仓库名称已存在")
		return
	}

	reposDir := skillReposDir()
	targetDir := filepath.Join(reposDir, repo.Name)
	if _, err := os.Stat(targetDir); err == nil {
		writeError(c, http.StatusBadRequest, "仓库目录已存在")
		return
	}

	os.MkdirAll(reposDir, 0755)
	if _, err := cloneGitRepoWithCredentials(reposDir, repo.Name, repo.URL, repo); err != nil {
		writeError(c, http.StatusInternalServerError, "Git clone 失败: "+err.Error())
		return
	}
	if err := syncGitRepoToSkill(repo.Name); err != nil {
		writeError(c, http.StatusInternalServerError, "安装技能失败: "+err.Error())
		return
	}

	repo.LastPull = time.Now().Format("2006-01-02 15:04:05")
	s.cfg.Skills.Repos = append(s.cfg.Skills.Repos, repo)
	s.saveSkillsConfig()
	writeSuccess(c, "技能已从 Git 导入")
}

// handleAdminSkillsReposPull 拉取仓库更新
func (s *Server) handleAdminSkillsReposPull(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	repoName := strings.TrimSpace(c.PostForm("name"))
	if repoName == "" {
		writeError(c, http.StatusBadRequest, "仓库名称不能为空")
		return
	}
	if err := util.SafePathSegment(repoName); err != nil {
		writeError(c, http.StatusBadRequest, "仓库名称不合法")
		return
	}

	gitMutex.Lock()
	defer gitMutex.Unlock()

	repo, idx, ok := skillRepoByName(s.cfg.Skills.Repos, repoName)
	if !ok {
		writeError(c, http.StatusNotFound, "仓库不存在")
		return
	}

	repoDir := filepath.Join(skillReposDir(), repoName)
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
		writeError(c, http.StatusBadRequest, "不是 Git 仓库")
		return
	}

	out, err := pullGitRepoWithCredentials(repoDir, repo)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Git pull 失败: "+err.Error())
		return
	}
	if err := syncGitRepoToSkill(repo.Name); err != nil {
		writeError(c, http.StatusInternalServerError, "同步技能失败: "+err.Error())
		return
	}

	s.cfg.Skills.Repos[idx].LastPull = time.Now().Format("2006-01-02 15:04:05")
	s.saveSkillsConfig()

	updated := !strings.Contains(out, "Already up to date")
	writeJSON(c, http.StatusOK, struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Updated bool   `json:"updated"`
	}{true, "技能已更新", updated})
}

// handleAdminSkillsReposRemove 删除仓库
func (s *Server) handleAdminSkillsReposRemove(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	repoName := strings.TrimSpace(c.PostForm("name"))
	if repoName == "" {
		writeError(c, http.StatusBadRequest, "仓库名称不能为空")
		return
	}
	if err := util.SafePathSegment(repoName); err != nil {
		writeError(c, http.StatusBadRequest, "仓库名称不合法")
		return
	}

	os.RemoveAll(filepath.Join(skillReposDir(), repoName))

	var filtered []config.SkillRepo
	for _, r := range s.cfg.Skills.Repos {
		if r.Name != repoName {
			filtered = append(filtered, r)
		}
	}
	s.cfg.Skills.Repos = filtered
	s.saveSkillsConfig()
	writeSuccess(c, "仓库已删除")
}

// handleAdminSkillsReposSave 保存仓库配置
func (s *Server) handleAdminSkillsReposSave(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	var input skillRepoInput
	if err := json.Unmarshal([]byte(c.PostForm("repo")), &input); err != nil {
		writeError(c, http.StatusBadRequest, "仓库配置格式错误")
		return
	}
	repo, err := skillRepoFromInput(input)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	gitMutex.Lock()
	defer gitMutex.Unlock()

	oldRepo, idx, ok := skillRepoByName(s.cfg.Skills.Repos, repo.Name)
	if !ok {
		writeError(c, http.StatusNotFound, "仓库不存在")
		return
	}
	repo.LastPull = oldRepo.LastPull
	s.cfg.Skills.Repos[idx] = repo
	s.saveSkillsConfig()
	writeSuccess(c, "仓库配置已保存")
}

// handleAdminSkillsInstall 从仓库安装技能到 skill/ 目录
func (s *Server) handleAdminSkillsInstall(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	repoName, err := cleanPathSegment(c.PostForm("repo"))
	skillName := strings.TrimSpace(c.PostForm("skill"))
	if repoName == "" {
		writeError(c, http.StatusBadRequest, "仓库名称不能为空")
		return
	}
	if err != nil {
		writeError(c, http.StatusBadRequest, "仓库名称不合法")
		return
	}
	if skillName != "" {
		cleanedSkillName, err := cleanPathSegment(skillName)
		if err != nil {
			writeError(c, http.StatusBadRequest, "技能名称不合法")
			return
		}
		skillName = cleanedSkillName
	}

	repoDir := filepath.Join(skillReposDir(), repoName)
	skillDir := config.SkillsDirPath()
	os.MkdirAll(skillDir, 0755)
	repoRoot, err := os.OpenRoot(repoDir)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "读取仓库失败")
		return
	}
	defer repoRoot.Close()
	skillRoot, err := os.OpenRoot(skillDir)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "读取技能目录失败")
		return
	}
	defer skillRoot.Close()

	if skillName == "" {
		// 安装仓库中所有技能
		entries, err := fs.ReadDir(repoRoot.FS(), ".")
		if err != nil {
			writeError(c, http.StatusInternalServerError, "读取仓库失败")
			return
		}
		count := 0
		for _, e := range entries {
			if !e.IsDir() || e.Name() == ".git" {
				continue
			}
			if err := copyDirBetweenRoots(repoRoot, e.Name(), skillRoot, e.Name()); err != nil {
				writeError(c, http.StatusInternalServerError, fmt.Sprintf("安装 %s 失败: %v", e.Name(), err))
				return
			}
			count++
		}
		writeJSON(c, http.StatusOK, struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
			Count   int    `json:"count"`
		}{true, fmt.Sprintf("已安装 %d 个技能", count), count})
		return
	}

	// 安装单个技能
	info, err := repoRoot.Stat(skillName)
	if err != nil || !info.IsDir() {
		writeError(c, http.StatusNotFound, "技能在仓库中不存在")
		return
	}
	if err := copyDirBetweenRoots(repoRoot, skillName, skillRoot, skillName); err != nil {
		writeError(c, http.StatusInternalServerError, "安装失败: "+err.Error())
		return
	}
	writeSuccess(c, "技能已安装")
}

// handleAdminSkillsReposList 列出仓库中的技能
func (s *Server) handleAdminSkillsReposList(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "GET" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
		return
	}

	repoName := c.Query("name")
	if repoName == "" {
		writeError(c, http.StatusBadRequest, "仓库名称不能为空")
		return
	}
	if err := util.SafePathSegment(repoName); err != nil {
		writeError(c, http.StatusBadRequest, "仓库名称不合法")
		return
	}

	repoDir := filepath.Join(skillReposDir(), repoName)
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		writeError(c, http.StatusNotFound, "仓库不存在")
		return
	}

	type SkillEntry struct {
		Name      string `json:"name"`
		Installed bool   `json:"installed"`
	}
	var skills []SkillEntry
	skillDir := config.SkillsDirPath()

	for _, e := range entries {
		if !e.IsDir() || e.Name() == ".git" {
			continue
		}
		_, installed := os.Stat(filepath.Join(skillDir, e.Name()))
		skills = append(skills, SkillEntry{
			Name:      e.Name(),
			Installed: installed == nil,
		})
	}
	if skills == nil {
		skills = []SkillEntry{}
	}

	writeJSON(c, http.StatusOK, struct {
		Success bool         `json:"success"`
		Skills  []SkillEntry `json:"skills"`
	}{true, skills})
}

// ============================================================
// 用户组管理
// ============================================================

func (s *Server) handleAdminGroups(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "GET" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
		return
	}
	groups, err := auth.ListGroups()
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if groups == nil {
		groups = []auth.GroupInfo{}
	}
	writeJSON(c, http.StatusOK, struct {
		Success bool             `json:"success"`
		Groups  []auth.GroupInfo `json:"groups"`
	}{true, groups})
}

func (s *Server) handleAdminGroupCreate(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if s.cfg.UnifiedAuthEnabled() {
		writeError(c, http.StatusForbidden, "统一认证模式下不允许手动创建组，请通过 LDAP 同步")
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}
	name := strings.TrimSpace(c.PostForm("name"))
	description := strings.TrimSpace(c.PostForm("description"))
	parentIDStr := strings.TrimSpace(c.PostForm("parent_id"))
	if name == "" {
		writeError(c, http.StatusBadRequest, "组名不能为空")
		return
	}
	var parentID *int64
	if parentIDStr != "" {
		pid, err := strconv.ParseInt(parentIDStr, 10, 64)
		if err != nil {
			writeError(c, http.StatusBadRequest, "无效的父组 ID")
			return
		}
		parentID = &pid
	}
	if err := auth.CreateGroup(name, "local", description, parentID); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	writeSuccess(c, "组 "+name+" 创建成功")
}

func (s *Server) handleAdminGroupDelete(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		writeError(c, http.StatusBadRequest, "组名不能为空")
		return
	}
	if err := auth.DeleteGroup(name); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	writeSuccess(c, "组 "+name+" 已删除")
}

func (s *Server) handleAdminGroupMembersAdd(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}
	groupName := strings.TrimSpace(c.PostForm("group_name"))
	usersStr := strings.TrimSpace(c.PostForm("usernames"))
	if groupName == "" || usersStr == "" {
		writeError(c, http.StatusBadRequest, "组名和用户名不能为空")
		return
	}
	usernames := strings.Split(usersStr, ",")
	var trimmed []string
	for _, u := range usernames {
		u = strings.TrimSpace(u)
		if u != "" {
			trimmed = append(trimmed, u)
		}
	}
	if len(trimmed) == 0 {
		writeError(c, http.StatusBadRequest, "用户名不能为空")
		return
	}
	if err := auth.AddUsersToGroup(groupName, trimmed); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	writeSuccess(c, fmt.Sprintf("已添加 %d 个用户到组 %s", len(trimmed), groupName))
}

func (s *Server) handleAdminGroupMembersRemove(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}
	groupName := strings.TrimSpace(c.PostForm("group_name"))
	username := strings.TrimSpace(c.PostForm("username"))
	if groupName == "" || username == "" {
		writeError(c, http.StatusBadRequest, "组名和用户名不能为空")
		return
	}
	if err := auth.RemoveUserFromGroup(groupName, username); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	writeSuccess(c, "已从组 "+groupName+" 移除 "+username)
}

func (s *Server) handleAdminGroupSkillsBind(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}
	groupName := strings.TrimSpace(c.PostForm("group_name"))
	skillName := strings.TrimSpace(c.PostForm("skill_name"))
	if groupName == "" || skillName == "" {
		writeError(c, http.StatusBadRequest, "组名和技能名不能为空")
		return
	}
	if err := util.SafePathSegment(skillName); err != nil {
		writeError(c, http.StatusBadRequest, "技能名称不合法")
		return
	}
	if err := auth.BindSkillToGroup(groupName, skillName); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 绑定后立即部署到组内所有用户
	members, err := auth.GetGroupMembersForDeploy(groupName)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "绑定成功但获取组成员失败: "+err.Error())
		return
	}
	skillDir := config.SkillsDirPath()
	userCount := 0
	for _, username := range members {
		targetDir := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw", "workspace", "skills")
		srcPath := filepath.Join(skillDir, skillName)
		dstPath := filepath.Join(targetDir, skillName)
		if err := util.CopyDir(srcPath, dstPath); err == nil {
			userCount++
		}
	}

	writeJSON(c, http.StatusOK, struct {
		Success   bool   `json:"success"`
		Message   string `json:"message"`
		UserCount int    `json:"user_count"`
	}{true, fmt.Sprintf("技能 %s 已绑定到组 %s 并部署到 %d 个用户", skillName, groupName, userCount), userCount})
}

func (s *Server) handleAdminGroupSkillsUnbind(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}
	groupName := strings.TrimSpace(c.PostForm("group_name"))
	skillName := strings.TrimSpace(c.PostForm("skill_name"))
	if groupName == "" || skillName == "" {
		writeError(c, http.StatusBadRequest, "组名和技能名不能为空")
		return
	}
	if err := util.SafePathSegment(skillName); err != nil {
		writeError(c, http.StatusBadRequest, "技能名称不合法")
		return
	}
	if err := auth.UnbindSkillFromGroup(groupName, skillName); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	writeSuccess(c, "已从组 "+groupName+" 解绑技能 "+skillName)
}

func (s *Server) handleAdminGroupMembers(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "GET" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
		return
	}
	groupName := c.Query("name")
	if groupName == "" {
		writeError(c, http.StatusBadRequest, "组名不能为空")
		return
	}
	members, err := auth.GetGroupMembers(groupName)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	skills, err := auth.GetGroupSkills(groupName)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if members == nil {
		members = []string{}
	}
	if skills == nil {
		skills = []string{}
	}
	writeJSON(c, http.StatusOK, struct {
		Success bool     `json:"success"`
		Members []string `json:"members"`
		Skills  []string `json:"skills"`
	}{true, members, skills})
}

// handleAdminAuthSyncGroups 手动触发 LDAP 组同步
func (s *Server) handleAdminAuthSyncGroups(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}
	if !s.cfg.LDAPEnabled() {
		writeError(c, http.StatusBadRequest, "LDAP 未启用")
		return
	}

	groupMap, err := ldap.FetchAllGroupsWithMembers(s.cfg)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "获取 LDAP 组失败: "+err.Error())
		return
	}

	// 获取白名单
	whitelist, _ := user.LoadWhitelist()

	groupCount := 0
	userCount := 0
	for groupName, members := range groupMap {
		// 创建组（如果不存在）
		auth.CreateGroup(groupName, "ldap", "", nil)
		groupCount++

		// 过滤白名单用户
		var filtered []string
		for _, m := range members {
			if whitelist == nil || whitelist[m] {
				filtered = append(filtered, m)
			}
		}

		if len(filtered) > 0 {
			auth.AddUsersToGroup(groupName, filtered)
			userCount += len(filtered)
		}
	}

	writeJSON(c, http.StatusOK, struct {
		Success     bool   `json:"success"`
		Message     string `json:"message"`
		GroupCount  int    `json:"group_count"`
		MemberCount int    `json:"member_count"`
	}{true, fmt.Sprintf("同步完成，发现 %d 个组，共 %d 个组成员关系", groupCount, userCount), groupCount, userCount})
}

// ============================================================
// 辅助函数
// ============================================================

func (s *Server) saveSkillsConfig() {
	skillsJSON, err := json.Marshal(map[string]interface{}{
		"repos": s.cfg.Skills.Repos,
	})
	if err != nil {
		return
	}
	engine, err := auth.GetEngine()
	if err != nil {
		return
	}
	engine.Exec("INSERT OR REPLACE INTO settings (key, value, updated_at) VALUES ('skills', ?, datetime('now','localtime'))", string(skillsJSON))
}

func formatSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ============================================================
// 超管账户管理
// ============================================================

// handleAdminSuperadmins 返回超管列表
func (s *Server) handleAdminSuperadmins(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method == "GET" {
		list, err := auth.GetSuperadmins()
		if err != nil {
			writeError(c, http.StatusInternalServerError, err.Error())
			return
		}
		if list == nil {
			list = []string{}
		}
		writeJSON(c, http.StatusOK, struct {
			Success bool     `json:"success"`
			Admins  []string `json:"admins"`
		}{true, list})
		return
	}
	writeError(c, http.StatusMethodNotAllowed, "仅支持 GET 方法")
}

// handleAdminSuperadminCreate 创建超管账户
func (s *Server) handleAdminSuperadminCreate(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	username := c.PostForm("username")
	if err := user.ValidateUsername(username); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if auth.UserExists(username) {
		writeError(c, http.StatusBadRequest, "用户 "+username+" 已存在")
		return
	}

	password := auth.GenerateRandomPassword(12)
	if err := auth.CreateUser(username, password, "superadmin"); err != nil {
		writeError(c, http.StatusInternalServerError, "创建超管失败: "+err.Error())
		return
	}

	writeJSON(c, http.StatusOK, struct {
		Success  bool   `json:"success"`
		Message  string `json:"message"`
		Username string `json:"username"`
		Password string `json:"password"`
	}{true, "超管创建成功", username, password})
	logger.Audit("superadmin.create", "username", username, "operator", s.getSessionUser(c))
}

// handleAdminSuperadminDelete 删除超管账户（至少保留一个）
func (s *Server) handleAdminSuperadminDelete(c *gin.Context) {
	currentUser := s.requireSuperadmin(c)
	if currentUser == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	username := c.PostForm("username")
	if username == "" {
		writeError(c, http.StatusBadRequest, "用户名不能为空")
		return
	}
	if username == currentUser {
		writeError(c, http.StatusBadRequest, "不能删除自己")
		return
	}
	if err := user.ValidateUsername(username); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if !auth.IsSuperadmin(username) {
		writeError(c, http.StatusBadRequest, username+" 不是超管")
		return
	}

	admins, _ := auth.GetSuperadmins()
	if len(admins) <= 1 {
		writeError(c, http.StatusBadRequest, "至少保留一个超管账户")
		return
	}

	if err := auth.DeleteUser(username); err != nil {
		writeError(c, http.StatusInternalServerError, "删除失败: "+err.Error())
		return
	}

	writeSuccess(c, "超管 "+username+" 已删除")
	logger.Audit("superadmin.delete", "username", username, "operator", currentUser)
}

// handleAdminSuperadminReset 重置超管密码
func (s *Server) handleAdminSuperadminReset(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	if c.Request.Method != "POST" {
		writeError(c, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	username := c.PostForm("username")
	if username == "" {
		writeError(c, http.StatusBadRequest, "用户名不能为空")
		return
	}
	if err := user.ValidateUsername(username); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if !auth.IsSuperadmin(username) {
		writeError(c, http.StatusBadRequest, username+" 不是超管")
		return
	}

	password := auth.GenerateRandomPassword(12)
	if err := auth.ChangePassword(username, password); err != nil {
		writeError(c, http.StatusInternalServerError, "重置密码失败: "+err.Error())
		return
	}

	writeJSON(c, http.StatusOK, struct {
		Success  bool   `json:"success"`
		Message  string `json:"message"`
		Password string `json:"password"`
	}{true, "密码已重置", password})
	logger.Audit("password.reset", "username", username, "operator", s.getSessionUser(c))
}
