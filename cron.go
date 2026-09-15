package goserver

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"sync"
	"time"
)

type TaskFunc func(ctx context.Context) error

type task struct {
	runAt    *time.Time
	fn       TaskFunc
	name     string
	interval time.Duration
}

type CronManager struct {
	ctx     context.Context
	cancel  context.CancelFunc
	logger  *log.Logger
	tasks   []task
	wg      sync.WaitGroup
	mu      sync.Mutex
	started bool
}

func NewCronManager(logger *log.Logger) *CronManager {
	ctx, cancel := context.WithCancel(context.Background())
	if logger == nil {
		logger = log.Default()
	}
	return &CronManager{
		ctx:    ctx,
		cancel: cancel,
		logger: logger,
	}
}

func (c *CronManager) AddIntervalJob(name string, interval time.Duration, fn TaskFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()

	newJob := task{
		name:     name,
		interval: interval,
		fn:       fn,
	}

	c.tasks = append(c.tasks, newJob)

	if c.started {
		c.wg.Add(1)
		go c.runInterval(newJob)
		c.logger.Printf("[cron] dynamically started interval job: %s", name)
	}
}

func (c *CronManager) AddDailyJob(name string, timeStr string, fn TaskFunc) error {
	t, err := time.Parse("15:04", timeStr)
	if err != nil {
		return fmt.Errorf("invalid time format, expected HH:MM (e.g. 15:30): %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	newJob := task{
		name:  name,
		runAt: &t,
		fn:    fn,
	}

	c.tasks = append(c.tasks, newJob)

	if c.started {
		c.wg.Add(1)
		go c.runDaily(newJob)
		c.logger.Printf("[cron] dynamically started daily job: %s", name)
	}
	return nil
}

func (c *CronManager) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return
	}

	for _, t := range c.tasks {
		c.wg.Add(1)
		job := t
		if job.runAt != nil {
			go c.runDaily(job)
		} else {
			go c.runInterval(job)
		}
	}

	c.started = true
	c.logger.Printf("[cron] started %d background jobs", len(c.tasks))
}

func (c *CronManager) Stop() {
	c.logger.Println("[cron] stopping background jobs...")
	c.cancel()
	c.wg.Wait()
	c.logger.Println("[cron] all jobs stopped.")
}

func (c *CronManager) runInterval(t task) {
	defer c.wg.Done()
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.executeSafely(t)
		}
	}
}

func (c *CronManager) runDaily(t task) {
	defer c.wg.Done()

	for {
		now := time.Now()
		nextRun := time.Date(now.Year(), now.Month(), now.Day(), t.runAt.Hour(), t.runAt.Minute(), 0, 0, now.Location())

		if nextRun.Before(now) {
			nextRun = nextRun.Add(24 * time.Hour)
		}

		durationUntilNextRun := nextRun.Sub(now)
		timer := time.NewTimer(durationUntilNextRun)

		select {
		case <-c.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			c.executeSafely(t)
		}
	}
}

func (c *CronManager) executeSafely(t task) {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Printf("[cron] PANIC in job '%s': %v\n%s", t.name, r, string(debug.Stack()))
		}
	}()

	err := t.fn(c.ctx)
	if err != nil {
		c.logger.Printf("[cron] ERROR in job '%s': %v", t.name, err)
	}
}

func (s *Server) RunAsync(task func()) {
	if !s.beginWork() {
		return
	}
	go func() {
		defer s.backgroundWg.Done()
		defer func() {
			if rec := recover(); rec != nil {
				s.Logger.Printf("CRITICAL: Panic in background task: %v", rec)

				if s.ErrorNotifier != nil {
					s.ErrorNotifier(
						http.StatusInternalServerError,
						nil,
						fmt.Sprintf("Async Panic: %v", rec),
					)
				}
			}
		}()

		task()
	}()
}
