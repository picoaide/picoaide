package scheduler

import (
  "context"
  "fmt"
  "log"
  "strings"
  "sync"
  "time"

  "github.com/picoaide/picoaide/internal/util"
  "github.com/robfig/cron/v3"
)

// ============================================================
// CronJob 定时任务模型
// ============================================================

type CronJob struct {
  ID        int64  `json:"id" xorm:"pk autoincr 'id'"`
  UserID    string `json:"user_id" xorm:"notnull 'user_id'"`
  Schedule  string `json:"schedule" xorm:"notnull 'schedule'"`
  Prompt    string `json:"prompt" xorm:"notnull 'prompt'"`
  AgentID   string `json:"agent_id" xorm:"notnull 'agent_id'"`
  ChannelID string `json:"channel_id" xorm:"'channel_id'"`
  Enabled   bool   `json:"enabled" xorm:"notnull 'enabled'"`
  NextRunAt string `json:"next_run_at" xorm:"TEXT 'next_run_at'"`
  LastRunAt string `json:"last_run_at" xorm:"TEXT 'last_run_at'"`
  CreatedAt string `json:"created_at" xorm:"notnull 'created_at'"`
}

func (CronJob) TableName() string {
  return "cron_jobs"
}

func (j *CronJob) String() string {
  desc, _ := ParseSchedule(j.Schedule)
  return fmt.Sprintf("#%d [%s] %s — %s", j.ID, j.UserID, j.Prompt, desc)
}

// ============================================================
// Scheduling 解析
// ============================================================

func ParseSchedule(schedule string) (string, error) {
  parts := strings.SplitN(schedule, " ", 2)
  if len(parts) < 2 {
    return "", fmt.Errorf("无效的调度格式: %s", schedule)
  }
  switch parts[0] {
  case "every":
    return fmt.Sprintf("间隔 %s 毫秒", parts[1]), nil
  case "cron":
    _, err := cron.ParseStandard(parts[1])
    if err != nil {
      return "", fmt.Errorf("无效的 cron 表达式: %w", err)
    }
    return fmt.Sprintf("cron: %s", parts[1]), nil
  case "at":
    return fmt.Sprintf("一次性: %s", parts[1]), nil
  default:
    return "", fmt.Errorf("不支持的调度类型: %s", parts[0])
  }
}

func NextRunTime(schedule string, from time.Time) (*time.Time, error) {
  parts := strings.SplitN(schedule, " ", 2)
  if len(parts) < 2 {
    return nil, fmt.Errorf("无效的调度格式: %s", schedule)
  }
  switch parts[0] {
  case "every":
    var ms int64
    if _, err := fmt.Sscanf(parts[1], "%d", &ms); err != nil {
      return nil, fmt.Errorf("无效的间隔: %s", parts[1])
    }
    next := from.Add(time.Duration(ms) * time.Millisecond)
    return &next, nil
  case "cron":
    parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
    expr, err := parser.Parse(parts[1])
    if err != nil {
      return nil, fmt.Errorf("无效的 cron: %w", err)
    }
    next := expr.Next(from)
    return &next, nil
  case "at":
    var ts int64
    if _, err := fmt.Sscanf(parts[1], "%d", &ts); err != nil {
      return nil, fmt.Errorf("无效的时间戳: %s", parts[1])
    }
    t := time.UnixMilli(ts)
    if t.Before(from) {
      return nil, nil
    }
    return &t, nil
  default:
    return nil, fmt.Errorf("不支持的调度: %s", parts[0])
  }
}

// ============================================================
// CronStore 接口
// ============================================================

type CronStore interface {
  ListActiveJobs(ctx context.Context) ([]*CronJob, error)
  UpdateNextRun(ctx context.Context, job *CronJob, nextRun time.Time) error
  UpdateLastRun(ctx context.Context, job *CronJob) error
  DisableJob(ctx context.Context, job *CronJob) error
}

// ============================================================
// CronScheduler 调度器
// ============================================================

type CronScheduler struct {
  store  CronStore
  execFn func(ctx context.Context, job *CronJob) error
  ticker *time.Ticker
  stopCh chan struct{}
  wg     sync.WaitGroup
}

func NewCronScheduler(store CronStore, execFn func(ctx context.Context, job *CronJob) error) *CronScheduler {
  return &CronScheduler{
    store:  store,
    execFn: execFn,
    ticker: time.NewTicker(10 * time.Second),
    stopCh: make(chan struct{}),
  }
}

func (s *CronScheduler) Start(ctx context.Context) {
  s.wg.Add(1)
  go func() {
    defer s.wg.Done()
    s.poll(ctx)
    for {
      select {
      case <-s.ticker.C:
        s.poll(ctx)
      case <-s.stopCh:
        return
      case <-ctx.Done():
        return
      }
    }
  }()
  log.Println("[cron] 定时任务调度器已启动")
}

func (s *CronScheduler) Stop() {
  close(s.stopCh)
  s.ticker.Stop()
  s.wg.Wait()
}

func (s *CronScheduler) poll(ctx context.Context) {
  jobs, err := s.store.ListActiveJobs(ctx)
  if err != nil {
    log.Printf("[cron] 查询任务失败: %v", err)
    return
  }

  now := time.Now()
  for _, job := range jobs {
    nextRun, err := util.ParseTime(job.NextRunAt)
    if err != nil || nextRun.After(now) {
      continue
    }

    // 立即将 next_run_at 设为远期，避免下一次轮询重复执行
    farFuture := now.Add(100 * 365 * 24 * time.Hour)
    if err := s.store.UpdateNextRun(ctx, job, farFuture); err != nil {
      log.Printf("[cron] 任务 #%d 更新 next_run_at 失败: %v", job.ID, err)
      continue
    }

    j := job
    s.wg.Add(1)
    go func() {
      defer s.wg.Done()
      runCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
      defer cancel()

      log.Printf("[cron] 执行任务 %d (用户: %s)", j.ID, j.UserID)
      if err := s.execFn(runCtx, j); err != nil {
        log.Printf("[cron] 任务 #%d 执行失败: %v", j.ID, err)
      }

      s.store.UpdateLastRun(ctx, j)
      next, err := NextRunTime(j.Schedule, time.Now())
      if err != nil {
        log.Printf("[cron] 计算下次时间失败: %v", err)
        return
      }
      if next != nil {
        s.store.UpdateNextRun(ctx, j, *next)
      } else {
        s.store.DisableJob(ctx, j)
      }
    }()
  }
}
