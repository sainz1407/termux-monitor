package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"termux-monitor/internal/config"
	"termux-monitor/internal/cron"
	"termux-monitor/internal/logger"
)

// cronService is the process-wide cron domain instance. The backend talks to the
// Termux user's crontab, and the history lives beside the other persistent data
// files so it survives a module reinstall.
var cronService = cron.NewService(
	cron.NewBackend(),
	cron.NewRunStore(config.GetPersistentFilePath("cron_runs.json")),
)

// cronErrorStatus maps the domain sentinels to HTTP status codes. Anything
// unrecognised is a 500 because the request itself was well formed.
func cronErrorStatus(err error) (int, string) {
	switch {
	case err == nil:
		return http.StatusOK, ""
	case errors.Is(err, cron.ErrScheduleFields),
		errors.Is(err, cron.ErrCommandMissing),
		errors.Is(err, cron.ErrNoCrontab):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, cron.ErrUnknownJob):
		return http.StatusNotFound, err.Error()
	default:
		return http.StatusInternalServerError, err.Error()
	}
}

// decodeCronBody decodes a JSON body into v, reporting a 400 on malformed input.
func decodeCronBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeStoreJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return false
	}
	return true
}

// requirePost rejects any method other than POST.
func requirePost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// requireGet rejects any method other than GET.
func requireGet(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// cronJobView augments a job with its computed next run so the UI does not have
// to reimplement the schedule math.
type cronJobView struct {
	cron.Job
	NextRun *int64 `json:"nextRun"`
}

// HandleCronDaemon reports the crond state and who is executing the jobs.
// GET /api/cron/daemon
func HandleCronDaemon(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	running, pid := cronService.Daemon()
	writeStoreJSON(w, http.StatusOK, map[string]any{
		"running":          running,
		"pid":              pid,
		"schedulerRunning": cronService.SchedulerRunning(),
		"available":        cronService.Available(),
	})
}

// HandleCronService starts or stops the crond daemon.
// POST /api/cron/service  {"action": "start"|"stop"}
func HandleCronService(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		Action string `json:"action"`
	}
	if !decodeCronBody(w, r, &req) {
		return
	}
	var err error
	switch strings.TrimSpace(req.Action) {
	case "start":
		err = cronService.StartDaemon()
	case "stop":
		err = cronService.StopDaemon()
	default:
		writeStoreJSON(w, http.StatusBadRequest, map[string]any{"error": "unknown action"})
		return
	}
	if err != nil {
		code, msg := cronErrorStatus(err)
		writeStoreJSON(w, code, map[string]any{"error": msg})
		return
	}
	logger.Get().Infof("cron", "daemon action: %s", req.Action)
	running, pid := cronService.Daemon()
	writeStoreJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"running":          running,
		"pid":              pid,
		"schedulerRunning": cronService.SchedulerRunning(),
	})
}

// HandleCronJobs lists the panel jobs with their next run.
// GET /api/cron/jobs
func HandleCronJobs(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	now := cron.TimeNowMillis()
	jobs := cronService.Jobs()
	views := make([]cronJobView, 0, len(jobs))
	for _, job := range jobs {
		view := cronJobView{Job: job}
		if next, ok := cron.NextRunMs(job.Schedule, now); ok {
			view.NextRun = &next
		}
		views = append(views, view)
	}
	writeStoreJSON(w, http.StatusOK, map[string]any{"jobs": views})
}

// HandleCronJobAdd creates a job.
// POST /api/cron/jobs  {"name": "...", "schedule": "...", "command": "..."}
func HandleCronJobAdd(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Command  string `json:"command"`
	}
	if !decodeCronBody(w, r, &req) {
		return
	}
	job, err := cronService.Add(req.Name, req.Schedule, req.Command)
	if err != nil {
		code, msg := cronErrorStatus(err)
		writeStoreJSON(w, code, map[string]any{"error": msg})
		return
	}
	logger.Get().Infof("cron", "job added: %s (%s)", job.ID, job.Name)
	view := cronJobView{Job: job}
	if next, ok := cron.NextRunMs(job.Schedule, cron.TimeNowMillis()); ok {
		view.NextRun = &next
	}
	writeStoreJSON(w, http.StatusOK, map[string]any{"ok": true, "job": view})
}

// HandleCronJobToggle enables or disables a job.
// POST /api/cron/jobs/toggle  {"id": "..."}
func HandleCronJobToggle(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !decodeCronBody(w, r, &req) {
		return
	}
	if err := cronService.Toggle(strings.TrimSpace(req.ID)); err != nil {
		code, msg := cronErrorStatus(err)
		writeStoreJSON(w, code, map[string]any{"error": msg})
		return
	}
	writeStoreJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// HandleCronJobDelete removes a job.
// POST /api/cron/jobs/delete  {"id": "..."}
func HandleCronJobDelete(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !decodeCronBody(w, r, &req) {
		return
	}
	if err := cronService.Delete(strings.TrimSpace(req.ID)); err != nil {
		code, msg := cronErrorStatus(err)
		writeStoreJSON(w, code, map[string]any{"error": msg})
		return
	}
	logger.Get().Infof("cron", "job deleted: %s", req.ID)
	writeStoreJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// HandleCronJobRun executes a job immediately and records the result.
// POST /api/cron/jobs/run  {"id": "..."}
func HandleCronJobRun(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !decodeCronBody(w, r, &req) {
		return
	}
	record, err := cronService.RunNow(strings.TrimSpace(req.ID))
	if err != nil {
		code, msg := cronErrorStatus(err)
		writeStoreJSON(w, code, map[string]any{"error": msg})
		return
	}
	logger.Get().Infof("cron", "manual run: %s (%s)", record.JobID, record.Name)
	writeStoreJSON(w, http.StatusOK, map[string]any{"ok": true, "run": record})
}

// HandleCronRuns returns the recorded execution history.
// GET /api/cron/runs?limit=N
func HandleCronRuns(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeStoreJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid limit"})
			return
		}
		limit = n
	}
	writeStoreJSON(w, http.StatusOK, map[string]any{"runs": cronService.Runs(limit)})
}

// HandleCronDryRun validates every job without touching the crontab.
// POST /api/cron/jobs/dry-run
func HandleCronDryRun(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	writeStoreJSON(w, http.StatusOK, map[string]any{"results": cronService.DryRun()})
}

// HandleCronDownload returns the raw crontab as text.
// GET /api/cron/download
func HandleCronDownload(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="crontab.txt"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(cronService.CrontabText()))
}

// HandleCronCrontab returns the raw crontab for the read-only viewer.
// GET /api/cron/crontab
func HandleCronCrontab(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	writeStoreJSON(w, http.StatusOK, map[string]any{"text": cronService.CrontabText()})
}
