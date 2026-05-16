package authsource

import (
  "github.com/picoaide/picoaide/internal/config"
)

func testConfig(authMode string) *config.GlobalConfig {
  return &config.GlobalConfig{
    Web: config.WebConfig{
      AuthMode: authMode,
    },
  }
}
