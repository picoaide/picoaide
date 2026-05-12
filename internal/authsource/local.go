package authsource

import (
  "github.com/picoaide/picoaide/internal/auth"
  "github.com/picoaide/picoaide/internal/config"
)

type LocalProvider struct{}

func init() {
  Register("local", LocalProvider{})
}

func (LocalProvider) Authenticate(cfg *config.GlobalConfig, username, password string) bool {
  ok, _, err := auth.AuthenticateLocal(username, password)
  return err == nil && ok
}

func (LocalProvider) DisplayName() string {
  return "本地用户"
}
