package logger

import (
  "net/http"
  "net/http/httptest"
  "testing"
)

func TestRetentionDays(t *testing.T) {
  tests := []struct {
    input string
    want  int
  }{
    {"1m", 30},
    {"3m", 90},
    {"6m", 180},
    {"1y", 365},
    {"3y", 1095},
    {"5y", 1825},
    {"forever", 0},
    {"", 180},     // 默认
    {"invalid", 180},
  }
  for _, tt := range tests {
    got := RetentionDays(tt.input)
    if got != tt.want {
      t.Errorf("RetentionDays(%q) = %d, want %d", tt.input, got, tt.want)
    }
  }
}

func TestExtractUser(t *testing.T) {
  tests := []struct {
    name   string
    setup  func(r *http.Request)
    want   string
  }{
    {
      name:  "no auth info",
      setup: func(r *http.Request) {},
      want:  "-",
    },
    {
      name: "token query param",
      setup: func(r *http.Request) {
        r.URL.RawQuery = "token=admin:secret123"
      },
      want: "admin",
    },
    {
      name: "bearer token",
      setup: func(r *http.Request) {
        r.Header.Set("Authorization", "Bearer user1:abc")
      },
      want: "user1",
    },
    {
      name: "token without colon",
      setup: func(r *http.Request) {
        r.URL.RawQuery = "token=nothinghere"
      },
      want: "-",
    },
    {
      name: "bearer without colon",
      setup: func(r *http.Request) {
        r.Header.Set("Authorization", "Bearer nothinghere")
      },
      want: "-",
    },
  }

  for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
      req := httptest.NewRequest("GET", "/test", nil)
      tt.setup(req)
      got := extractUser(req)
      if got != tt.want {
        t.Errorf("extractUser() = %q, want %q", got, tt.want)
      }
    })
  }
}

func TestClientIP(t *testing.T) {
  tests := []struct {
    name  string
    setup func(r *http.Request)
    want  string
  }{
    {
      name: "X-Real-IP header",
      setup: func(r *http.Request) {
        r.Header.Set("X-Real-IP", "1.2.3.4")
      },
      want: "1.2.3.4",
    },
    {
      name: "X-Forwarded-For header",
      setup: func(r *http.Request) {
        r.Header.Set("X-Forwarded-For", "5.6.7.8")
      },
      want: "5.6.7.8",
    },
    {
      name: "X-Real-IP takes priority",
      setup: func(r *http.Request) {
        r.Header.Set("X-Real-IP", "1.2.3.4")
        r.Header.Set("X-Forwarded-For", "5.6.7.8")
      },
      want: "1.2.3.4",
    },
    {
      name:  "RemoteAddr fallback",
      setup: func(r *http.Request) {},
      want:  "192.0.2.1:1234", // httptest 默认 RemoteAddr
    },
  }

  for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
      req := httptest.NewRequest("GET", "/test", nil)
      req.RemoteAddr = "192.0.2.1:1234"
      tt.setup(req)
      got := clientIP(req)
      if got != tt.want {
        t.Errorf("clientIP() = %q, want %q", got, tt.want)
      }
    })
  }
}

func TestStringsIndex(t *testing.T) {
  tests := []struct {
    s      string
    substr string
    want   int
  }{
    {"admin:token", ":", 5},
    {"noColon", ":", -1},
    {"abc", "bc", 1},
    {"abc", "d", -1},
    {"", ":", -1},
    {"a", "a", 0},
  }
  for _, tt := range tests {
    got := stringsIndex(tt.s, tt.substr)
    if got != tt.want {
      t.Errorf("stringsIndex(%q, %q) = %d, want %d", tt.s, tt.substr, got, tt.want)
    }
  }
}
