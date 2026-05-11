package authsource

import (
	"context"
	"fmt"
	"sync"

	"github.com/picoaide/picoaide/internal/config"
)

type Identity struct {
	Username string
	Groups   []string
}

type PasswordProvider interface {
	Authenticate(cfg *config.GlobalConfig, username, password string) bool
}

type BrowserProvider interface {
	AuthURL(cfg *config.GlobalConfig, state string) (string, error)
	CompleteLogin(ctx context.Context, cfg *config.GlobalConfig, code string) (*Identity, error)
}

type DirectoryProvider interface {
	FetchUsers(cfg *config.GlobalConfig) ([]string, error)
	FetchUserGroups(cfg *config.GlobalConfig, username string) ([]string, error)
	FetchGroups(cfg *config.GlobalConfig) (map[string]GroupHierarchy, error)
}

var (
	providersMu sync.RWMutex
	providers   = map[string]any{}
)

func Register(name string, provider any) {
	providersMu.Lock()
	defer providersMu.Unlock()
	providers[name] = provider
}

func Provider(name string) (any, bool) {
	providersMu.RLock()
	defer providersMu.RUnlock()
	provider, ok := providers[name]
	return provider, ok
}

func passwordProvider(name string) (PasswordProvider, error) {
	provider, ok := Provider(name)
	if !ok {
		return nil, fmt.Errorf("认证源 %s 未注册", name)
	}
	typed, ok := provider.(PasswordProvider)
	if !ok {
		return nil, fmt.Errorf("认证源 %s 不支持用户名密码认证", name)
	}
	return typed, nil
}

func browserProvider(name string) (BrowserProvider, error) {
	provider, ok := Provider(name)
	if !ok {
		return nil, fmt.Errorf("认证源 %s 未注册", name)
	}
	typed, ok := provider.(BrowserProvider)
	if !ok {
		return nil, fmt.Errorf("认证源 %s 不支持浏览器认证", name)
	}
	return typed, nil
}

func directoryProvider(name string) (DirectoryProvider, error) {
	provider, ok := Provider(name)
	if !ok {
		return nil, fmt.Errorf("认证源 %s 未注册", name)
	}
	typed, ok := provider.(DirectoryProvider)
	if !ok {
		return nil, fmt.Errorf("认证源 %s 不支持目录同步", name)
	}
	return typed, nil
}
