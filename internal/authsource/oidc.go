package authsource

import (
	"context"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/picoaide/picoaide/internal/config"
)

type OIDCProvider struct{}

func init() {
	Register("oidc", OIDCProvider{})
}

func (OIDCProvider) AuthURL(cfg *config.GlobalConfig, state string) (string, error) {
	_, oauthCfg, err := buildOIDCConfig(cfg)
	if err != nil {
		return "", err
	}
	return oauthCfg.AuthCodeURL(state), nil
}

func (OIDCProvider) CompleteLogin(ctx context.Context, cfg *config.GlobalConfig, code string) (*Identity, error) {
	provider, oauthCfg, err := buildOIDCConfig(cfg)
	if err != nil {
		return nil, err
	}
	token, err := oauthCfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("OIDC 授权码交换失败: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, fmt.Errorf("OIDC 响应缺少 id_token")
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: oauthCfg.ClientID}).Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("OIDC id_token 校验失败: %w", err)
	}
	var claims map[string]interface{}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("OIDC claims 解析失败: %w", err)
	}

	username := firstNonEmpty(
		claimString(claims, strings.TrimSpace(cfg.OIDC.UsernameClaim)),
		claimString(claims, "preferred_username"),
		claimString(claims, "email"),
		claimString(claims, "sub"),
	)
	if username == "" {
		return nil, fmt.Errorf("OIDC claims 缺少可用用户名")
	}

	groups := claimStrings(claims, firstNonEmpty(strings.TrimSpace(cfg.OIDC.GroupsClaim), "groups"))
	return &Identity{Username: username, Groups: groups}, nil
}

func OIDCAuthURL(cfg *config.GlobalConfig, state string) (string, error) {
	provider, err := browserProvider("oidc")
	if err != nil {
		return "", err
	}
	return provider.AuthURL(cfg, state)
}

func OIDCCompleteLogin(ctx context.Context, cfg *config.GlobalConfig, code string) (*Identity, error) {
	provider, err := browserProvider("oidc")
	if err != nil {
		return nil, err
	}
	return provider.CompleteLogin(ctx, cfg, code)
}

func buildOIDCConfig(cfg *config.GlobalConfig) (*oidc.Provider, *oauth2.Config, error) {
	issuer := strings.TrimSpace(cfg.OIDC.IssuerURL)
	clientID := strings.TrimSpace(cfg.OIDC.ClientID)
	clientSecret := strings.TrimSpace(cfg.OIDC.ClientSecret)
	redirectURL := strings.TrimSpace(cfg.OIDC.RedirectURL)
	if issuer == "" || clientID == "" || clientSecret == "" || redirectURL == "" {
		return nil, nil, fmt.Errorf("OIDC Issuer、Client ID、Client Secret 和 Redirect URL 不能为空")
	}
	provider, err := oidc.NewProvider(context.Background(), issuer)
	if err != nil {
		return nil, nil, err
	}
	scopes := strings.Fields(cfg.OIDC.Scopes)
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}
	hasOpenID := false
	for _, scope := range scopes {
		if scope == oidc.ScopeOpenID {
			hasOpenID = true
			break
		}
	}
	if !hasOpenID {
		scopes = append([]string{oidc.ScopeOpenID}, scopes...)
	}
	return provider, &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes,
	}, nil
}
