package handlers

import "termux-monitor/internal/logger"

// registerCronRoutes wires the Cron tab endpoints. Every route is session-gated:
// managing the crontab of the Termux user is a privileged action.
func registerCronRoutes(rc *routeContext) {
	rc.mux.HandleFunc("/api/cron/daemon", rc.wrap(HandleCronDaemon, true))
	rc.mux.HandleFunc("/api/cron/service", rc.wrap(HandleCronService, true))
	rc.mux.HandleFunc("/api/cron/jobs", rc.wrap(HandleCronJobs, true))
	rc.mux.HandleFunc("/api/cron/jobs/add", rc.wrap(HandleCronJobAdd, true))
	rc.mux.HandleFunc("/api/cron/jobs/toggle", rc.wrap(HandleCronJobToggle, true))
	rc.mux.HandleFunc("/api/cron/jobs/delete", rc.wrap(HandleCronJobDelete, true))
	rc.mux.HandleFunc("/api/cron/jobs/run", rc.wrap(HandleCronJobRun, true))
	rc.mux.HandleFunc("/api/cron/jobs/dry-run", rc.wrap(HandleCronDryRun, true))
	rc.mux.HandleFunc("/api/cron/runs", rc.wrap(HandleCronRuns, true))
	rc.mux.HandleFunc("/api/cron/crontab", rc.wrap(HandleCronCrontab, true))
	rc.mux.HandleFunc("/api/cron/download", rc.wrap(HandleCronDownload, true))

	// Align the fallback scheduler with the observed daemon state at startup:
	// when crond already runs the server must not execute the jobs a second
	// time, and when cronie is absent the server takes over.
	cronService.SyncScheduler()
	logger.Get().Infof("cron", "routes registered (scheduler=%t)", cronService.SchedulerRunning())
}
