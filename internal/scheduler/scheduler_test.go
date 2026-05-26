package scheduler

import (
  "encoding/json"
  "strings"
  "testing"
  "time"

  "github.com/robfig/cron/v3"
)

// ============================================================
// CronJob.String
// ============================================================

func TestCronJobString(t *testing.T) {
  job := &CronJob{
    ID:       1,
    UserID:   "alice",
    Prompt:   "早安问候",
    Schedule: "every 3600000",
  }
  s := job.String()
  if !strings.Contains(s, "#1") || !strings.Contains(s, "alice") || !strings.Contains(s, "早安问候") {
    t.Errorf("unexpected string: %s", s)
  }
}

// ============================================================
// ParseSchedule
// ============================================================

func TestParseSchedule(t *testing.T) {
  tests := []struct {
    input   string
    want    string
    wantErr bool
  }{
    {"every 3600000", "间隔 3600000 毫秒", false},
    {"cron 0 9 * * *", "cron: 0 9 * * *", false},
    {"at 1700000000000", "一次性: 1700000000000", false},
    {"invalid", "", true},
    {"unknown foo", "", true},
    {"cron bad-expr", "", true},
    {"", "", true},
  }
  for _, tt := range tests {
    t.Run(tt.input, func(t *testing.T) {
      got, err := ParseSchedule(tt.input)
      if tt.wantErr {
        if err == nil {
          t.Errorf("expected error, got %q", got)
        }
        return
      }
      if err != nil {
        t.Fatalf("unexpected error: %v", err)
      }
      if got != tt.want {
        t.Errorf("got %q, want %q", got, tt.want)
      }
    })
  }
}

// ============================================================
// NextRunTime
// ============================================================

func TestNextRunTime(t *testing.T) {
  t.Run("every", func(t *testing.T) {
    from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
    next, err := NextRunTime("every 3600000", from)
    if err != nil {
      t.Fatal(err)
    }
    if next == nil {
      t.Fatal("nil result")
    }
    expected := from.Add(time.Hour)
    if !next.Equal(expected) {
      t.Errorf("expected %v, got %v", expected, next)
    }
  })

  t.Run("every_invalid_ms", func(t *testing.T) {
    _, err := NextRunTime("every abc", time.Now())
    if err == nil {
      t.Errorf("expected error for non-numeric interval")
    }
  })

  t.Run("cron", func(t *testing.T) {
    from := time.Date(2025, 1, 1, 8, 30, 0, 0, time.UTC)
    next, err := NextRunTime("cron 0 9 * * *", from)
    if err != nil {
      t.Fatal(err)
    }
    if next == nil {
      t.Fatal("nil result")
    }
    expected := time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC)
    if !next.Equal(expected) {
      t.Errorf("expected %v, got %v", expected, next)
    }
  })

  t.Run("cron_invalid", func(t *testing.T) {
    _, err := NextRunTime("cron bad-expr", time.Now())
    if err == nil {
      t.Errorf("expected error for bad cron expression")
    }
  })

  t.Run("at_future", func(t *testing.T) {
    from := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
    next, err := NextRunTime("at 9999999999999", from)
    if err != nil {
      t.Fatal(err)
    }
    if next == nil {
      t.Fatal("nil result")
    }
  })

  t.Run("at_past", func(t *testing.T) {
    from := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
    next, err := NextRunTime("at 1000000000", from)
    if err != nil {
      t.Fatal(err)
    }
    if next != nil {
      t.Errorf("expected nil for past timestamp, got %v", next)
    }
  })

  t.Run("at_invalid", func(t *testing.T) {
    _, err := NextRunTime("at abc", time.Now())
    if err == nil {
      t.Errorf("expected error for non-numeric timestamp")
    }
  })

  t.Run("invalid_schedule", func(t *testing.T) {
    _, err := NextRunTime("unsupported xxx", time.Now())
    if err == nil {
      t.Errorf("expected error for unsupported schedule type")
    }
  })

  t.Run("invalid_format", func(t *testing.T) {
    _, err := NextRunTime("no-space", time.Now())
    if err == nil {
      t.Errorf("expected error")
    }
  })
}

// ============================================================
// CronParser integration (robfig/cron)
// ============================================================

func TestCronParseStandard(t *testing.T) {
  t.Run("valid", func(t *testing.T) {
    _, err := cron.ParseStandard("0 9 * * *")
    if err != nil {
      t.Fatal(err)
    }
  })

  t.Run("invalid", func(t *testing.T) {
    _, err := cron.ParseStandard("bad")
    if err == nil {
      t.Errorf("expected parse error")
    }
  })
}

func TestJSONSerialization(t *testing.T) {
  job := &CronJob{
    ID:        1,
    UserID:    "test",
    Schedule:  "every 3600000",
    Prompt:    "test prompt",
    AgentID:   "pico",
    Enabled:   true,
    NextRunAt: "2026-05-25T12:00:00Z",
  }
  data, err := json.Marshal(job)
  if err != nil {
    t.Fatal(err)
  }
  var decoded CronJob
  if err := json.Unmarshal(data, &decoded); err != nil {
    t.Fatal(err)
  }
  if decoded.ID != 1 || decoded.UserID != "test" || decoded.NextRunAt != "2026-05-25T12:00:00Z" {
    t.Errorf("unexpected decoded values: %+v", decoded)
  }
}
