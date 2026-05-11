package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/picoaide/picoaide/internal/auth"
	"github.com/picoaide/picoaide/internal/authsource"
	"github.com/picoaide/picoaide/internal/config"
	dockerpkg "github.com/picoaide/picoaide/internal/docker"
	"github.com/picoaide/picoaide/internal/logger"
	"github.com/picoaide/picoaide/internal/user"
)

// ============================================================
// JSON 响应辅助
// ============================================================

// apiResponse 是统一的 JSON 响应结构
type apiResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// apiError 是带 error 字段的 JSON 响应
type apiError struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

// writeJSON 将 v 序列化为 JSON 并写入响应
func writeJSON(c *gin.Context, statusCode int, v interface{}) {
	c.JSON(statusCode, v)
}

// writeSuccess 返回成功响应
func writeSuccess(c *gin.Context, message string) {
	writeJSON(c, http.StatusOK, apiResponse{Success: true, Message: message})
}

// writeError 返回错误响应
func writeError(c *gin.Context, statusCode int, errMsg string) {
	writeJSON(c, statusCode, apiError{Success: false, Error: errMsg})
}

// requireAuth 检查登录状态，返回用户名；未登录时自动返回 401
func (s *Server) requireAuth(c *gin.Context) string {
	username := s.getSessionUser(c)
	if username == "" {
		writeError(c, http.StatusUnauthorized, "未登录")
		return ""
	}
	return username
}

func (s *Server) requireNonSuperadmin(c *gin.Context) string {
	username := s.requireAuth(c)
	if username == "" {
		return ""
	}
	if auth.IsSuperadmin(username) {
		writeError(c, http.StatusForbidden, "超管用户不允许登录插件，使用普通用户登录")
		return ""
	}
	return username
}

func (s *Server) requireRegularUser(c *gin.Context) string {
	username := s.requireAuth(c)
	if username == "" {
		return ""
	}
	if auth.IsSuperadmin(username) {
		writeError(c, http.StatusForbidden, "管理员没有普通用户配置权限，请进入管理后台")
		return ""
	}
	return username
}

func (s *Server) isExtensionRequest(c *gin.Context) bool {
	origin := c.GetHeader("Origin")
	return strings.HasPrefix(origin, "chrome-extension://") || strings.HasPrefix(origin, "moz-extension://")
}

// ============================================================
// 健康检查 Handler
// ============================================================

func (s *Server) handleHealth(c *gin.Context) {
	writeJSON(c, http.StatusOK, struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}{
		Status:  "ok",
		Version: config.Version,
	})
}

// ============================================================
// 认证 Handler
// ============================================================

func (s *Server) handleLoginMode(c *gin.Context) {
	writeJSON(c, http.StatusOK, struct {
		Success  bool   `json:"success"`
		AuthMode string `json:"auth_mode"`
	}{true, s.cfg.AuthMode()})
}

// handleLogin 处理用户名密码登录请求（本地超管或 LDAP）
func (s *Server) handleLogin(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")
	if username == "" || password == "" {
		writeError(c, http.StatusBadRequest, "请输入用户名和密码")
		return
	}

	isLocalSuperadmin := auth.IsSuperadmin(username)
	if s.isExtensionRequest(c) && isLocalSuperadmin {
		writeError(c, http.StatusForbidden, "超管用户不允许登录插件，使用普通用户登录")
		return
	}

	// 1. 本地认证（LDAP 模式下仅允许超管）
	if ok, _, err := auth.AuthenticateLocal(username, password); err == nil && ok {
		if s.cfg.UnifiedAuthEnabled() && !isLocalSuperadmin {
			// 统一认证模式下，非超管本地用户禁止登录
		} else {
			// 首次登录自动初始化（创建容器记录，分配 IP）
			// 超管不需要容器
			if rec, _ := auth.GetContainerByUsername(username); rec == nil {
				if !isLocalSuperadmin {
					go user.InitUser(s.cfg, username, "")
				}
			}

			s.setSessionCookie(c, s.createSessionToken(username), 86400)
			logger.Audit("user.login", "username", username, "method", "local")
			writeJSON(c, http.StatusOK, struct {
				Success  bool   `json:"success"`
				Username string `json:"username"`
			}{
				Success:  true,
				Username: username,
			})
			return
		}
	}

	// 2. 外部用户名密码认证只支持 LDAP；OIDC 走浏览器授权码流程。
	if s.cfg.AuthMode() != "ldap" {
		logger.Audit("user.login_failed", "username", username, "reason", "invalid_credentials")
		writeError(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}

	if isLocalSuperadmin {
		logger.Audit("user.login_failed", "username", username, "reason", "invalid_local_superadmin_password")
		writeError(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}

	if !authsource.LDAPAuthenticate(s.cfg, username, password) {
		writeError(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}

	if !user.AllowedByWhitelist(s.cfg, "ldap", username) {
		writeError(c, http.StatusForbidden, "请联系管理员添加白名单")
		return
	}
	if err := auth.EnsureExternalUser(username, "user", "ldap"); err != nil {
		writeError(c, http.StatusInternalServerError, "同步本地用户失败: "+err.Error())
		return
	}

	// 首次登录自动初始化（创建容器记录，分配 IP）
	initializing := false
	if rec, _ := auth.GetContainerByUsername(username); rec == nil {
		initializing = true
		if err := s.initLDAPUser(username); err != nil {
			logger.Audit("user.init_failed", "username", username, "method", "ldap", "error", err.Error())
			writeError(c, http.StatusInternalServerError, "LDAP 登录成功，但初始化账号失败: "+err.Error())
			return
		}
	} else if !s.userEnvironmentReady(username) {
		initializing = true
	}

	// 异步同步用户的 LDAP 组
	if s.cfg.LDAP.GroupSearchMode != "" {
		go func() {
			if groups, err := authsource.LDAPFetchUserGroups(s.cfg, username); err == nil && len(groups) > 0 {
				auth.SyncUserGroups(username, groups)
			}
		}()
	}

	s.setSessionCookie(c, s.createSessionToken(username), 86400)
	logger.Audit("user.login", "username", username, "method", "ldap")
	writeJSON(c, http.StatusOK, struct {
		Success      bool   `json:"success"`
		Username     string `json:"username"`
		Initializing bool   `json:"initializing"`
	}{
		Success:      true,
		Username:     username,
		Initializing: initializing,
	})
}

// handleLogout 处理登出请求
func (s *Server) handleLogout(c *gin.Context) {
	username := s.getSessionUser(c)
	s.setSessionCookie(c, "", -1)
	if username != "" {
		logger.Audit("user.logout", "username", username)
	}
	writeSuccess(c, "已登出")
}

// handleCSRF 返回当前用户的 CSRF token
func (s *Server) handleCSRF(c *gin.Context) {
	username := s.requireAuth(c)
	if username == "" {
		return
	}
	writeJSON(c, http.StatusOK, struct {
		Success   bool   `json:"success"`
		CSRFToken string `json:"csrf_token"`
	}{
		Success:   true,
		CSRFToken: s.csrfToken(username),
	})
}

// ============================================================
// Cookie 同步 Handler
// ============================================================

// handleCookies 将当前页面的 Cookie 写入用户的 .security.yml
func (s *Server) handleCookies(c *gin.Context) {
	username := s.requireNonSuperadmin(c)
	if username == "" {
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	domain := strings.TrimSpace(c.PostForm("domain"))
	cookieStr := strings.TrimSpace(c.PostForm("cookies"))

	if domain == "" || cookieStr == "" {
		writeError(c, http.StatusBadRequest, "域名和 Cookie 不能为空")
		return
	}

	if err := user.SyncCookies(s.cfg, username, domain, cookieStr); err != nil {
		writeError(c, http.StatusInternalServerError, "同步失败: "+err.Error())
		return
	}

	writeSuccess(c, "已同步 "+domain+" 的登录状态")
}

// ============================================================
// 钉钉配置 Handler
// ============================================================

// handleDingTalkGet 返回当前用户的钉钉配置
func (s *Server) handleDingTalkGet(c *gin.Context) {
	username := s.requireRegularUser(c)
	if username == "" {
		return
	}

	clientID, clientSecret := user.GetDingTalkConfig(s.cfg, username)
	writeJSON(c, http.StatusOK, struct {
		Success      bool   `json:"success"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}{
		Success:      true,
		ClientID:     clientID,
		ClientSecret: clientSecret,
	})
}

// handleDingTalkSave 保存钉钉配置并重启容器
func (s *Server) handleDingTalkSave(c *gin.Context) {
	username := s.requireRegularUser(c)
	if username == "" {
		return
	}

	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	clientID := strings.TrimSpace(c.PostForm("client_id"))
	clientSecret := strings.TrimSpace(c.PostForm("client_secret"))

	if clientID == "" || clientSecret == "" {
		writeError(c, http.StatusBadRequest, "Client ID 和 Client Secret 不能为空")
		return
	}

	if err := user.SaveDingTalkConfig(s.cfg, username, clientID, clientSecret); err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}

	// 重启容器
	rec, _ := auth.GetContainerByUsername(username)
	if rec != nil && rec.ContainerID != "" {
		_ = dockerpkg.Restart(c.Request.Context(), rec.ContainerID)
	}

	writeSuccess(c, "配置已保存，容器正在重启中，请稍候片刻即可使用。")
}

func (s *Server) handlePicoClawConfigFieldsGet(c *gin.Context) {
	username := s.requireRegularUser(c)
	if username == "" {
		return
	}
	configVersion, _ := strconv.Atoi(c.Query("config_version"))
	section := strings.TrimSpace(c.Query("section"))
	values, err := user.GetPicoClawConfigFields(s.cfg, username, configVersion, section)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(c, http.StatusOK, map[string]interface{}{
		"success": true,
		"fields":  values,
	})
}

func (s *Server) handlePicoClawChannelsGet(c *gin.Context) {
	username := s.requireRegularUser(c)
	if username == "" {
		return
	}
	configVersion, _ := strconv.Atoi(c.Query("config_version"))
	channels, err := user.ListPicoClawUserChannels(s.cfg, username, configVersion)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(c, http.StatusOK, map[string]interface{}{
		"success":  true,
		"channels": nonNilChannels(channels),
	})
}

func (s *Server) handlePicoClawAdminChannelsGet(c *gin.Context) {
	if s.requireSuperadmin(c) == "" {
		return
	}
	configVersion, _ := strconv.Atoi(c.Query("config_version"))
	if configVersion <= 0 {
		configVersion = user.PicoAideSupportedPicoClawConfigVersion
	}
	channels, err := user.ListPicoClawAdminChannels(config.RuleCacheDir(), configVersion)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(c, http.StatusOK, map[string]interface{}{
		"success":  true,
		"channels": nonNilChannels(channels),
	})
}

func nonNilChannels(channels []user.PicoClawChannelInfo) []user.PicoClawChannelInfo {
	if channels == nil {
		return []user.PicoClawChannelInfo{}
	}
	return channels
}

func (s *Server) handlePicoClawConfigFieldsSave(c *gin.Context) {
	username := s.requireRegularUser(c)
	if username == "" {
		return
	}
	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}
	configVersion, _ := strconv.Atoi(c.PostForm("config_version"))
	section := strings.TrimSpace(c.PostForm("section"))
	valuesText := strings.TrimSpace(c.PostForm("values"))
	if valuesText == "" {
		writeError(c, http.StatusBadRequest, "values 不能为空")
		return
	}
	var values map[string]interface{}
	if err := json.Unmarshal([]byte(valuesText), &values); err != nil {
		writeError(c, http.StatusBadRequest, "values JSON 格式错误: "+err.Error())
		return
	}
	if err := user.SavePicoClawConfigSectionFields(s.cfg, username, configVersion, section, values); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	rec, _ := auth.GetContainerByUsername(username)
	if rec != nil && rec.ContainerID != "" {
		_ = dockerpkg.Restart(c.Request.Context(), rec.ContainerID)
	}
	writeSuccess(c, "配置已保存，容器正在重启中，请稍候片刻即可使用。")
}

// ============================================================
// 用户信息 & 配置管理 Handler
// ============================================================

// handleUserInfo 返回当前登录用户的信息（角色等）
func (s *Server) handleUserInfo(c *gin.Context) {
	username := s.requireAuth(c)
	if username == "" {
		return
	}

	role := auth.GetUserRole(username)
	if role == "" {
		role = "user"
	}

	writeJSON(c, http.StatusOK, struct {
		Success      bool   `json:"success"`
		Username     string `json:"username"`
		Role         string `json:"role"`
		AuthMode     string `json:"auth_mode"`
		UnifiedAuth  bool   `json:"unified_auth"`
		Initializing bool   `json:"initializing"`
	}{
		Success:      true,
		Username:     username,
		Role:         role,
		AuthMode:     s.cfg.AuthMode(),
		UnifiedAuth:  s.cfg.UnifiedAuthEnabled(),
		Initializing: role != "superadmin" && auth.IsExternalUser(username) && !s.userEnvironmentReady(username),
	})
}

func (s *Server) userEnvironmentReady(username string) bool {
	rec, _ := auth.GetContainerByUsername(username)
	if rec == nil || rec.Image == "" {
		return false
	}
	configPath := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw", "config.json")
	if _, err := os.Stat(configPath); err != nil {
		return false
	}
	if !s.dockerAvailable {
		return true
	}
	if rec.ContainerID == "" {
		return false
	}
	return dockerpkg.ContainerStatus(contextWithTimeout(5), rec.ContainerID) == "running"
}

func (s *Server) handleUserInitStatus(c *gin.Context) {
	username := s.requireRegularUser(c)
	if username == "" {
		return
	}

	rec, _ := auth.GetContainerByUsername(username)
	status := "未初始化"
	imageReady := false
	hasConfig := false
	if rec != nil {
		status = rec.Status
		if s.dockerAvailable && rec.ContainerID != "" {
			status = dockerpkg.ContainerStatus(contextWithTimeout(5), rec.ContainerID)
		}
		imageReady = rec.Image != ""
		configPath := filepath.Join(user.UserDir(s.cfg, username), ".picoclaw", "config.json")
		_, err := os.Stat(configPath)
		hasConfig = err == nil
	}

	writeJSON(c, http.StatusOK, struct {
		Success    bool   `json:"success"`
		Ready      bool   `json:"ready"`
		Status     string `json:"status"`
		ImageReady bool   `json:"image_ready"`
		HasConfig  bool   `json:"has_config"`
	}{
		Success:    true,
		Ready:      s.userEnvironmentReady(username),
		Status:     status,
		ImageReady: imageReady,
		HasConfig:  hasConfig,
	})
}

// handleChangePassword 处理用户修改密码（仅本地模式）
func (s *Server) handleChangePassword(c *gin.Context) {
	username := s.requireRegularUser(c)
	if username == "" {
		return
	}

	if s.cfg.UnifiedAuthEnabled() {
		writeError(c, http.StatusForbidden, "非本地用户不支持修改密码，请联系管理员在公司认证中心修改")
		return
	}

	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	oldPassword := c.PostForm("old_password")
	newPassword := c.PostForm("new_password")
	if oldPassword == "" || newPassword == "" {
		writeError(c, http.StatusBadRequest, "请输入旧密码和新密码")
		return
	}
	if len(newPassword) < 6 {
		writeError(c, http.StatusBadRequest, "新密码至少 6 个字符")
		return
	}

	// 验证旧密码
	ok, _, err := auth.AuthenticateLocal(username, oldPassword)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "验证密码失败")
		return
	}
	if !ok {
		writeError(c, http.StatusUnauthorized, "旧密码错误")
		return
	}

	if err := auth.ChangePassword(username, newPassword); err != nil {
		writeError(c, http.StatusInternalServerError, "修改密码失败: "+err.Error())
		return
	}

	writeSuccess(c, "密码修改成功")
	logger.Audit("password.change", "username", username)
}

// handleConfigGet 从数据库读取配置并返回为 JSON
func (s *Server) handleConfigGet(c *gin.Context) {
	username := s.requireAuth(c)
	if username == "" {
		return
	}

	// 检查超管权限
	if !auth.IsSuperadmin(username) {
		writeError(c, http.StatusForbidden, "仅超级管理员可访问")
		return
	}

	raw, err := config.LoadRawFromDB()
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
}

// handleConfigSave 从 JSON 保存配置到数据库
func (s *Server) handleConfigSave(c *gin.Context) {
	username := s.requireAuth(c)
	if username == "" {
		return
	}

	// 检查超管权限
	if !auth.IsSuperadmin(username) {
		writeError(c, http.StatusForbidden, "仅超级管理员可访问")
		return
	}

	if !s.checkCSRF(c) {
		writeError(c, http.StatusForbidden, "无效请求")
		return
	}

	jsonStr := c.PostForm("config")
	if jsonStr == "" {
		writeError(c, http.StatusBadRequest, "配置内容不能为空")
		return
	}

	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		writeError(c, http.StatusBadRequest, fmt.Sprintf("JSON 格式错误: %v", err))
		return
	}

	oldMode := s.cfg.AuthMode()
	oldCfg := *s.cfg
	newMode := authModeFromRaw(raw, oldMode)
	if !validAuthMode(newMode) {
		writeError(c, http.StatusBadRequest, "不支持的认证方式: "+newMode)
		return
	}
	normalizeAuthModeInRaw(raw, newMode)

	changedBy := s.getSessionUser(c)
	if changedBy == "" {
		changedBy = "admin"
	}
	if err := config.SaveRawToDB(raw, changedBy); err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}

	// 重新加载内存配置，确保后续下发操作使用最新值
	if newCfg, err := config.LoadFromDB(); err == nil {
		s.cfg = newCfg
	}

	var cleanup *authProviderSwitchCleanupResult
	if oldMode != newMode {
		var err error
		cleanup, err = s.purgeOrdinaryAuthProviderStateForConfig(&oldCfg)
		if err != nil {
			writeError(c, http.StatusInternalServerError, "认证方式已保存，但清理旧认证数据失败: "+err.Error())
			return
		}
		logger.Audit("auth.provider_switch", "operator", changedBy, "old_mode", oldMode, "new_mode", newMode, "users_removed", cleanup.UsersRemoved, "container_records", cleanup.ContainerRecords)
	}

	if cleanup != nil {
		writeJSON(c, http.StatusOK, struct {
			Success bool                             `json:"success"`
			Message string                           `json:"message"`
			Cleanup *authProviderSwitchCleanupResult `json:"cleanup"`
		}{true, "配置已保存，认证方式已切换并清空旧认证数据", cleanup})
	} else {
		writeSuccess(c, "配置已保存")
	}
	logger.Audit("config.save", "operator", changedBy)
}

func authModeFromRaw(raw map[string]interface{}, fallback string) string {
	web, ok := raw["web"].(map[string]interface{})
	if !ok {
		return fallback
	}
	if mode, ok := web["auth_mode"].(string); ok && strings.TrimSpace(mode) != "" {
		return strings.ToLower(strings.TrimSpace(mode))
	}
	if enabled, ok := web["ldap_enabled"].(bool); ok {
		if enabled {
			return "ldap"
		}
		return "local"
	}
	if enabled, ok := web["ldap_enabled"].(string); ok {
		b, _ := strconv.ParseBool(enabled)
		if b {
			return "ldap"
		}
		return "local"
	}
	return fallback
}

func validAuthMode(mode string) bool {
	switch mode {
	case "local", "ldap", "oidc":
		return true
	default:
		return false
	}
}

func normalizeAuthModeInRaw(raw map[string]interface{}, mode string) {
	web, ok := raw["web"].(map[string]interface{})
	if !ok {
		web = map[string]interface{}{}
		raw["web"] = web
	}
	web["auth_mode"] = mode
	web["ldap_enabled"] = mode == "ldap"
}
