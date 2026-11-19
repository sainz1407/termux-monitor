package cron

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
)

// randomHex returns n cryptographically random bytes as lowercase hex. Job and
// run ids use it so two panels writing to the same crontab never collide.
func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand does not fail on Android; a panic here is preferable to
		// silently handing out a predictable, colliding id.
		panic("cron: random source unavailable: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

// Errors surfaced to the API. The handler maps them to 400 responses, matching
// the OLP contract where thrown errors become `{error}` bodies.
var (
	ErrScheduleFields = errors.New("Schedule must have 5 cron fields")
	ErrCommandMissing = errors.New("Command is required")
	ErrNoCrontab      = errors.New("crontab is not available on this system")
	ErrUnknownJob     = errors.New("Unknown job")
)

// validateSchedule enforces exactly five non-empty whitespace separated fields.
func validateSchedule(schedule string) error {
	if len(strings.Fields(schedule)) != 5 {
		return ErrScheduleFields
	}
	// Fields must also parse, otherwise a job could be stored that never fires.
	if _, ok := ParseCronSchedule(schedule); !ok {
		return errors.New("Invalid schedule expression")
	}
	return nil
}

// Service is the cron domain logic the HTTP handlers call into. It owns the
// backend, the run history and the fallback scheduler.
type Service struct {
	backend   Backend
	runs      *RunStore
	scheduler *Scheduler
}

// NewService wires the domain layer. The fallback scheduler is started lazily
// by SyncScheduler once the daemon state is known.
func NewService(backend Backend, runs *RunStore) *Service {
	return &Service{
		backend:   backend,
		runs:      runs,
		scheduler: NewScheduler(backend, runs),
	}
}

// Jobs returns every panel job, parsed from the crontab in order.
func (s *Service) Jobs() []Job {
	lines := s.backend.ReadLines()
	jobs := make([]Job, 0, len(lines))
	for _, line := range lines {
		if job := ParseLine(line); job != nil {
			jobs = append(jobs, *job)
		}
	}
	return jobs
}

// FindJob returns the job with the given id, or false.
func (s *Service) FindJob(id string) (Job, bool) {
	for _, job := range s.Jobs() {
		if job.ID == id {
			return job, true
		}
	}
	return Job{}, false
}

// Add validates and appends a new job to the crontab. The crontab binary must
// exist, because without it the write would silently be lost.
func (s *Service) Add(name, schedule, command string) (Job, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "job"
	}
	schedule = strings.Join(strings.Fields(schedule), " ")
	command = strings.TrimSpace(command)
	if err := validateSchedule(schedule); err != nil {
		return Job{}, err
	}
	if command == "" {
		return Job{}, ErrCommandMissing
	}
	if !s.backend.HasBin("crontab") {
		return Job{}, ErrNoCrontab
	}
	job := Job{
		ID:       randomHex(4),
		Name:     name,
		Schedule: schedule,
		Command:  command,
		Enabled:  true,
	}
	lines := s.backend.ReadLines()
	lines = append(lines, JobLine(job.ID, job.Name, job.Schedule, job.Command))
	if err := s.backend.WriteLines(lines); err != nil {
		return Job{}, err
	}
	return job, nil
}

// Toggle flips a job's enabled state. Unknown ids are a no-op success so a
// double tap from two browser tabs cannot fail.
func (s *Service) Toggle(id string) error {
	lines := s.backend.ReadLines()
	for i, line := range lines {
		lines[i] = ToggleLine(line, id)
	}
	return s.backend.WriteLines(lines)
}

// Delete removes the job tagged with id, leaving foreign lines untouched.
func (s *Service) Delete(id string) error {
	lines := s.backend.ReadLines()
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		job := ParseLine(line)
		if job != nil && job.ID == id {
			continue
		}
		kept = append(kept, line)
	}
	return s.backend.WriteLines(kept)
}

// RunNow executes a job immediately and records the result.
func (s *Service) RunNow(id string) (RunRecord, error) {
	job, ok := s.FindJob(id)
	if !ok {
		return RunRecord{}, ErrUnknownJob
	}
	startedAt := timeNowMillis()
	result := s.backend.RunCommand(job.Command)
	record := RunRecord{
		ID:         randomHex(4),
		JobID:      job.ID,
		Name:       job.Name,
		Command:    job.Command,
		StartedAt:  startedAt,
		FinishedAt: timeNowMillis(),
		ExitCode:   result.ExitCode,
		TimedOut:   result.TimedOut,
		LatencyMs:  result.LatencyMs,
		Output:     result.Output,
	}
	s.runs.Append(record)
	return record, nil
}

// Runs returns the recorded history, newest first.
func (s *Service) Runs(limit int) []RunRecord {
	return s.runs.List(limit)
}

// Daemon reports whether crond is running and its PID when known.
func (s *Service) Daemon() (bool, int) {
	pid := s.backend.DaemonPID()
	return pid > 0, pid
}

// StartDaemon launches crond, then parks the fallback scheduler so jobs are not
// executed twice (once by crond, once by the server).
func (s *Service) StartDaemon() error {
	if !s.backend.HasBin("crond") {
		return errors.New("crond is not installed. Install Cronie via the Store menu.")
	}
	if err := s.backend.SpawnDetached(); err != nil {
		return err
	}
	s.scheduler.Stop()
	return nil
}

// StopDaemon stops crond and starts the fallback scheduler so scheduled jobs
// keep firing without cronie running.
func (s *Service) StopDaemon() error {
	err := s.backend.StopDaemon()
	s.scheduler.Start()
	return err
}

// SyncScheduler aligns the fallback scheduler with the observed daemon state.
// It is called once at startup: when crond is already running the fallback must
// stay off, and when it is absent the fallback must take over.
func (s *Service) SyncScheduler() {
	if running, _ := s.Daemon(); running {
		s.scheduler.Stop()
		return
	}
	s.scheduler.Start()
}

// SchedulerRunning reports whether the fallback scheduler is active, so the UI
// can explain who is executing the jobs.
func (s *Service) SchedulerRunning() bool {
	return s.scheduler.Running()
}

// Available reports whether the Termux environment is usable at all. When it is
// not, every cron endpoint would fail on the first crontab call, so the UI shows
// a single explanatory banner instead.
func (s *Service) Available() bool {
	ok, _ := s.backend.Available()
	return ok
}

// AvailableReason explains why Available reports false; empty when usable.
func (s *Service) AvailableReason() string {
	_, reason := s.backend.Available()
	return reason
}

// DryRun inspects every job without touching the crontab: an invalid schedule
// or a command whose binary is missing is reported per job.
type DryRunResult struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	OK       bool   `json:"ok"`
	NextRun  *int64 `json:"nextRun"`
	Resolved bool   `json:"resolved"`
	Error    string `json:"error"`
}

// DryRun validates each job and resolves the first non-assignment token of its
// command against $PREFIX.
func (s *Service) DryRun() []DryRunResult {
	now := timeNowMillis()
	jobs := s.Jobs()
	results := make([]DryRunResult, 0, len(jobs))
	for _, job := range jobs {
		base := DryRunResult{ID: job.ID, Name: job.Name, Enabled: job.Enabled}
		next, ok := NextRunMs(job.Schedule, now)
		if !ok {
			base.Error = "Invalid schedule"
			results = append(results, base)
			continue
		}
		token := firstCommandToken(job.Command)
		if token == "" {
			base.Error = "Empty command"
			results = append(results, base)
			continue
		}
		resolved := s.backend.HasBin(token)
		base.NextRun = &next
		base.Resolved = resolved
		base.OK = resolved
		if !resolved {
			base.Error = "Command not found: " + token
		}
		results = append(results, base)
	}
	return results
}

// CrontabText returns the raw crontab for the read-only viewer. An absent
// crontab yields the empty string.
func (s *Service) CrontabText() string {
	lines := s.backend.ReadLines()
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// firstCommandToken returns the first shell word that is not an environment
// assignment (VAR=value), mirroring the OLP dry-run resolver.
func firstCommandToken(command string) string {
	for _, token := range strings.Fields(command) {
		if isAssignment(token) {
			continue
		}
		return token
	}
	return ""
}

// isAssignment reports whether token looks like a `NAME=value` prefix.
func isAssignment(token string) bool {
	eq := strings.IndexByte(token, '=')
	if eq <= 0 {
		return false
	}
	name := token[:eq]
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
