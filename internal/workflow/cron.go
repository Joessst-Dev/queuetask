package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/robfig/cron/v3"
)

// CronScheduler starts new workflow instances on a cron schedule.
type CronScheduler struct {
	mu     sync.Mutex
	c      *cron.Cron
	jobs   map[string]cron.EntryID // key: "workflowName:index"
	engine *Engine
	repo   *Repository
}

func NewCronScheduler(engine *Engine, repo *Repository) *CronScheduler {
	return &CronScheduler{
		c:      cron.New(),
		jobs:   make(map[string]cron.EntryID),
		engine: engine,
		repo:   repo,
	}
}

func (s *CronScheduler) Start() { s.c.Start() }
func (s *CronScheduler) Stop()  { s.c.Stop() }

// Sync removes all existing cron jobs and re-registers them from the current
// definitions. Called on startup and after every registry reload.
func (s *CronScheduler) Sync(defs []*Definition) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range s.jobs {
		s.c.Remove(id)
	}
	s.jobs = make(map[string]cron.EntryID)

	for _, def := range defs {
		for i, t := range def.Triggers {
			if t.Type != InstanceTriggerCron {
				continue
			}
			name := def.Name
			var inputJSON json.RawMessage
			if t.Input != nil {
				b, err := json.Marshal(t.Input)
				if err != nil {
					slog.Error("marshalling cron trigger input", "workflow", name, "error", err)
					continue
				}
				inputJSON = b
			}
			idx := i
			var entryID cron.EntryID
			id, err := s.c.AddFunc(t.Schedule, func() {
				// Use the cron-computed fire time (Entry.Prev is set to the scheduled
				// time before the job goroutine starts) so all nodes derive an identical
				// dedup key regardless of wall-clock jitter or NTP skew.
				scheduledAt := s.c.Entry(entryID).Prev
				ctx := context.Background()
				won, err := s.repo.TryAcquireCronLock(ctx, name, idx, scheduledAt)
				if err != nil {
					slog.Warn("cron lock check failed", "workflow", name, "error", err)
					return
				}
				if !won {
					return
				}
				if _, err := s.engine.StartInstance(ctx, name, inputJSON); err != nil {
					slog.Warn("cron trigger failed to start instance", "workflow", name, "error", err)
				}
			})
			if err != nil {
				slog.Error("registering cron job", "workflow", def.Name, "schedule", t.Schedule, "error", err)
				continue
			}
			entryID = id // safe: job first fires at next schedule boundary, after this assignment
			s.jobs[fmt.Sprintf("%s:%d", def.Name, i)] = id
			slog.Info("cron trigger registered", "workflow", def.Name, "schedule", t.Schedule)
		}
	}
}
