package logger

import (
  "bufio"
  "bytes"
  "net"
  "net/http"
  "net/http/httptest"
  "os"
  "sync"
  "testing"
)

func resetLogger() {
  if instance != nil && instance.writer != nil {
    instance.writer.Close()
  }
  once = sync.Once{}
  instance = nil
}

type nonFlushWriter struct {
  code int
  h    http.Header
  buf  bytes.Buffer
}

func (w *nonFlushWriter) Header() http.Header {
  if w.h == nil {
    w.h = make(http.Header)
  }
  return w.h
}

func (w *nonFlushWriter) Write(b []byte) (int, error) {
  return w.buf.Write(b)
}

func (w *nonFlushWriter) WriteHeader(code int) {
  w.code = code
}

type hijackableWriter struct {
  inner *httptest.ResponseRecorder
}

func (w *hijackableWriter) Header() http.Header     { return w.inner.Header() }
func (w *hijackableWriter) Write(b []byte) (int, error) { return w.inner.Write(b) }
func (w *hijackableWriter) WriteHeader(code int)        { w.inner.WriteHeader(code) }
func (w *hijackableWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
  return nil, nil, nil
}

func TestResponseRecorderWriteHeader(t *testing.T) {
  w := httptest.NewRecorder()
  r := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}

  r.WriteHeader(http.StatusNotFound)

  if r.statusCode != http.StatusNotFound {
    t.Errorf("statusCode = %d, want %d", r.statusCode, http.StatusNotFound)
  }
  if w.Code != http.StatusNotFound {
    t.Errorf("underlying code = %d, want %d", w.Code, http.StatusNotFound)
  }
}

func TestResponseRecorderFlush(t *testing.T) {
  t.Run("underlying supports Flusher", func(t *testing.T) {
    w := httptest.NewRecorder()
    r := &responseRecorder{ResponseWriter: w}
    r.Flush()
  })

  t.Run("underlying does not support Flusher", func(t *testing.T) {
    w := &nonFlushWriter{}
    r := &responseRecorder{ResponseWriter: w}
    r.Flush()
  })
}

func TestResponseRecorderHijack(t *testing.T) {
  w := &hijackableWriter{httptest.NewRecorder()}
  r := &responseRecorder{ResponseWriter: w}

  conn, rw, err := r.Hijack()
  if err != nil {
    t.Fatal(err)
  }
  if conn != nil || rw != nil {
    t.Error("expected nil from mock")
  }
}

func TestAccessMiddleware(t *testing.T) {
  inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
  })
  handler := AccessMiddleware(inner)

  t.Run("normal path", func(t *testing.T) {
    req := httptest.NewRequest("GET", "/api/test", nil)
    w := httptest.NewRecorder()
    handler.ServeHTTP(w, req)
    if w.Code != http.StatusOK {
      t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
    }
  })

  t.Run("favicon.ico skipped", func(t *testing.T) {
    req := httptest.NewRequest("GET", "/favicon.ico", nil)
    w := httptest.NewRecorder()
    handler.ServeHTTP(w, req)
    if w.Code != http.StatusOK {
      t.Errorf("status = %d", w.Code)
    }
  })

  t.Run("robots.txt skipped", func(t *testing.T) {
    req := httptest.NewRequest("GET", "/robots.txt", nil)
    w := httptest.NewRecorder()
    handler.ServeHTTP(w, req)
    if w.Code != http.StatusOK {
      t.Errorf("status = %d", w.Code)
    }
  })

  t.Run("with token in query", func(t *testing.T) {
    req := httptest.NewRequest("GET", "/api/test?token=user1:abc123", nil)
    w := httptest.NewRecorder()
    handler.ServeHTTP(w, req)
  })

  t.Run("with bearer token", func(t *testing.T) {
    req := httptest.NewRequest("GET", "/api/test", nil)
    req.Header.Set("Authorization", "Bearer admin:secret")
    w := httptest.NewRecorder()
    handler.ServeHTTP(w, req)
  })

  t.Run("non-200 status code", func(t *testing.T) {
    inner500 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
      w.WriteHeader(http.StatusInternalServerError)
      w.Write([]byte("error"))
    })
    h := AccessMiddleware(inner500)
    req := httptest.NewRequest("POST", "/api/error", nil)
    w := httptest.NewRecorder()
    h.ServeHTTP(w, req)
    if w.Code != http.StatusInternalServerError {
      t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
    }
  })
}

func TestParseLevel(t *testing.T) {
  tests := []struct {
    input string
    want  int
  }{
    {"debug", -4},
    {"warn", 4},
    {"error", 8},
    {"info", 0},
    {"unknown", 0},
    {"", 0},
  }
  for _, tt := range tests {
    got := parseLevel(tt.input)
    if int(got) != tt.want {
      t.Errorf("parseLevel(%q) = %d, want %d", tt.input, got, tt.want)
    }
  }
}

func TestInit(t *testing.T) {
  t.Run("basic init", func(t *testing.T) {
    resetLogger()
    defer resetLogger()

    tmpDir := t.TempDir()
    Init(tmpDir, "1m", false, "info")
    Close()
  })

  t.Run("dev mode downgrades level", func(t *testing.T) {
    resetLogger()
    defer resetLogger()

    tmpDir := t.TempDir()
    Init(tmpDir, "forever", true, "warn")
    Close()
  })

  t.Run("PICOAIDE_DEV env var", func(t *testing.T) {
    resetLogger()
    defer resetLogger()

    old := os.Getenv("PICOAIDE_DEV")
    os.Setenv("PICOAIDE_DEV", "1")
    defer os.Setenv("PICOAIDE_DEV", old)

    tmpDir := t.TempDir()
    Init(tmpDir, "6m", false, "error")
    Close()
  })

  t.Run("dev mode debug level unchanged", func(t *testing.T) {
    resetLogger()
    defer resetLogger()

    tmpDir := t.TempDir()
    Init(tmpDir, "3m", true, "debug")
    Close()
  })

  t.Run("retention 1y", func(t *testing.T) {
    resetLogger()
    defer resetLogger()

    tmpDir := t.TempDir()
    Init(tmpDir, "1y", false, "info")
    Close()
  })

  t.Run("retention 3y", func(t *testing.T) {
    resetLogger()
    defer resetLogger()

    tmpDir := t.TempDir()
    Init(tmpDir, "3y", false, "info")
    Close()
  })

  t.Run("retention 5y", func(t *testing.T) {
    resetLogger()
    defer resetLogger()

    tmpDir := t.TempDir()
    Init(tmpDir, "5y", false, "info")
    Close()
  })

  t.Run("once do no-op on second call", func(t *testing.T) {
    resetLogger()
    defer resetLogger()

    tmpDir := t.TempDir()
    Init(tmpDir, "1m", false, "info")
    Init(tmpDir, "3m", true, "debug") // second call should be no-op
    Close()
  })
}

func TestClose(t *testing.T) {
  t.Run("before init", func(t *testing.T) {
    resetLogger()
    Close()
  })

  t.Run("after init", func(t *testing.T) {
    resetLogger()
    defer resetLogger()

    tmpDir := t.TempDir()
    Init(tmpDir, "3m", false, "debug")
    Close()
  })
}

func TestAudit(t *testing.T) {
  Audit("test_action", "key1", "value1")
  Audit("test_action_no_args")
}
