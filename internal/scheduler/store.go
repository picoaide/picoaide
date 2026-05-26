package scheduler

import (
  "context"
  "fmt"
  "time"

  "github.com/picoaide/picoaide/internal/util"
  "xorm.io/xorm"
)

// ============================================================
// SQLCronStore — SQLite 实现的 CronStore
// ============================================================

type SQLCronStore struct {
  engine *xorm.Engine
}

func NewSQLCronStore(engine *xorm.Engine) *SQLCronStore {
  return &SQLCronStore{engine: engine}
}

func (s *SQLCronStore) InitTable() error {
  _, err := s.engine.Exec(`CREATE TABLE IF NOT EXISTS cron_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL,
    schedule TEXT NOT NULL,
    prompt TEXT NOT NULL,
    agent_id TEXT NOT NULL DEFAULT 'pico',
    channel_id TEXT DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    next_run_at TEXT,
    last_run_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now', 'localtime'))
  )`)
  return err
}

func (s *SQLCronStore) ListActiveJobs(ctx context.Context) ([]*CronJob, error) {
  var jobs []*CronJob
  err := s.engine.Where("enabled = ?", 1).Find(&jobs)
  return jobs, err
}

func (s *SQLCronStore) UpdateNextRun(ctx context.Context, job *CronJob, nextRun time.Time) error {
  job.NextRunAt = util.FormatTime(nextRun)
  _, err := s.engine.ID(job.ID).Cols("next_run_at").Update(job)
  return err
}

func (s *SQLCronStore) UpdateLastRun(ctx context.Context, job *CronJob) error {
  job.LastRunAt = util.FormatTime(time.Now())
  _, err := s.engine.ID(job.ID).Cols("last_run_at").Update(job)
  return err
}

func (s *SQLCronStore) Insert(ctx context.Context, job *CronJob) error {
  next, err := NextRunTime(job.Schedule, time.Now())
  if err != nil {
    return fmt.Errorf("计算执行时间失败: %w", err)
  }
  if next != nil {
    job.NextRunAt = util.FormatTime(*next)
  }
  job.CreatedAt = util.FormatTime(time.Now())
  _, err = s.engine.Insert(job)
  return err
}

func (s *SQLCronStore) Delete(ctx context.Context, id int64) error {
  _, err := s.engine.ID(id).Delete(&CronJob{})
  return err
}

func (s *SQLCronStore) ListByUser(ctx context.Context, userID string) ([]*CronJob, error) {
  var jobs []*CronJob
  err := s.engine.Where("user_id = ?", userID).Desc("id").Find(&jobs)
  return jobs, err
}

func (s *SQLCronStore) GetByID(ctx context.Context, id int64) (*CronJob, error) {
  var job CronJob
  has, err := s.engine.ID(id).Get(&job)
  if err != nil {
    return nil, err
  }
  if !has {
    return nil, nil
  }
  return &job, nil
}

func (s *SQLCronStore) Update(ctx context.Context, job *CronJob) error {
  next, err := NextRunTime(job.Schedule, time.Now())
  if err != nil {
    return fmt.Errorf("计算执行时间失败: %w", err)
  }
  if next != nil {
    job.NextRunAt = util.FormatTime(*next)
  } else {
    job.NextRunAt = ""
  }
  _, err = s.engine.ID(job.ID).Cols("schedule", "prompt", "agent_id", "channel_id", "enabled", "next_run_at").Update(job)
  return err
}

func (s *SQLCronStore) DisableJob(ctx context.Context, job *CronJob) error {
  job.Enabled = false
  _, err := s.engine.ID(job.ID).Cols("enabled").Update(job)
  return err
}
