package cron

import (
	"sync"
	"time"

	"termux-monitor/internal/logger"
)

// SchedulerTick is how often the fallback scheduler revisits the crontab. One
// minute is the cron resolution, so the tick only needs to be frequent enough
// to not miss the start of a minute under load.
const SchedulerTick = 30 * time.Second

// Scheduler is the in-process fallback used when cronie is not installed. It
// polls the same crontab the panel writes and fires due jobs itself, so the
// Cron tab keeps working on a device without the cronie package.
//
// It only ever fires jobs that carry this panel's tag: a foreign crontab line
// must not be executed by the server on the user's behalf.
type Scheduler struct {
	backend Backend
	runs    *RunStore

	mu      sync.Mutex
	lastRun map[string]int64 // job id -> last fired minute (unix seconds, truncated)
	stop    chan struct{}
	done    chan struct{}
	started bool
}

// NewScheduler builds a fallback scheduler over the given backend and history.
func NewScheduler(backend Backend, runs *RunStore) *Scheduler {
	return &Scheduler{
		backend: backend,
		runs:    runs,
		lastRun: map[string]int64{},
	}
}

// Start launches the polling loop. It is a no-op when the loop already runs.
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return
	}
	s.started = true
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	go s.loop(s.stop, s.done)
	logger.Get().Infof("cron", "fallback scheduler started (tick %s)", SchedulerTick)
}

// Stop halts the polling loop and waits for the current tick to finish.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	stop, done := s.stop, s.done
	s.started = false
	s.mu.Unlock()

	close(stop)
	<-done
	logger.Get().Infof("cron", "fallback scheduler stopped")
}

// Running reports whether the polling loop is active.
func (s *Scheduler) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

// loop polls until stop closes.
func (s *Scheduler) loop(stop, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(SchedulerTick)
	defer ticker.Stop()
	// Evaluate immediately so a job whose minute is already current still fires
	// on the first tick after the service is enabled.
	s.tick()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

// tick fires every enabled, due job exactly once per minute.
func (s *Scheduler) tick() {
	now := time.Now()
	minute := now.Truncate(time.Minute).Unix()

	for _, job := range s.readJobs() {
		if !job.Enabled {
			continue
		}
		if !dueThisMinute(job.Schedule, now) {
			continue
		}
		s.mu.Lock()
		last := s.lastRun[job.ID]
		if last == minute {
			s.mu.Unlock()
			continue
		}
		s.lastRun[job.ID] = minute
		s.mu.Unlock()

		go s.fire(job)
	}
}

// fire executes one job and records the result in the shared history so the
// WebUI shows fallback runs exactly like manual ones.
func (s *Scheduler) fire(job Job) {
	startedAt := time.Now()
	result := s.backend.RunCommand(job.Command)
	record := RunRecord{
		ID:         randomHex(4),
		JobID:      job.ID,
		Name:       job.Name,
		Command:    job.Command,
		StartedAt:  startedAt.UnixMilli(),
		FinishedAt: time.Now().UnixMilli(),
		ExitCode:   result.ExitCode,
		TimedOut:   result.TimedOut,
		LatencyMs:  result.LatencyMs,
		Output:     result.Output,
	}
	s.runs.Append(record)
	code := -1
	if result.ExitCode != nil {
		code = *result.ExitCode
	}
	logger.Get().Infof("cron", "fallback run %s (%s) exit=%d timeout=%t", job.ID, job.Name, code, result.TimedOut)
}

// readJobs parses every tagged line of the crontab.
func (s *Scheduler) readJobs() []Job {
	lines := s.backend.ReadLines()
	jobs := make([]Job, 0, len(lines))
	for _, line := range lines {
		if job := ParseLine(line); job != nil {
			jobs = append(jobs, *job)
		}
	}
	return jobs
}

// dueThisMinute reports whether schedule fires during now's minute. The check
// reuses the schedule math: a fire time whose minute equals now's minute is
// due now, which keeps the fallback and crond interpretations identical.
func dueThisMinute(schedule string, now time.Time) bool {
	// NextRun is strictly after its argument, so looking back one minute makes
	// a fire time inside the current minute resolve to that minute.
	next, ok := NextRun(schedule, now.Add(-time.Minute))
	if !ok {
		return false
	}
	return next.Truncate(time.Minute).Equal(now.Truncate(time.Minute))
}
