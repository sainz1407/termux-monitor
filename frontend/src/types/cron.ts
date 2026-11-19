// Types for the Cron tab. They mirror the JSON emitted by
// internal/handlers/cron_handler.go.

/** One cron job parsed from the Termux user's crontab. */
export interface CronJob {
  id: string
  name: string
  /** Five-field cron expression: "min hour dom month dow". */
  schedule: string
  command: string
  enabled: boolean
  /** Epoch milliseconds of the next fire time, or null when none is reachable. */
  nextRun: number | null
}

/** One recorded execution (manual Run Now or a fallback-scheduler run). */
export interface CronRun {
  id: string
  jobId: string
  name: string
  command: string
  startedAt: number
  finishedAt: number
  /** null when the run timed out or was killed by a signal. */
  exitCode: number | null
  timedOut: boolean
  latencyMs: number
  /** Tail of combined stdout+stderr, capped server-side. */
  output: string
}

/** Result of GET /api/cron/daemon. */
export interface CronDaemonResponse {
  running: boolean
  /** 0 when the daemon PID is unknown. */
  pid: number
  /** True when the in-process fallback scheduler is executing the jobs. */
  schedulerRunning: boolean
  available: boolean
}

/** Result of GET /api/cron/jobs. */
export interface CronJobsResponse {
  jobs: CronJob[]
}

/** Per-job verdict from POST /api/cron/jobs/dry-run. */
export interface CronDryRunResult {
  id: string
  name: string
  enabled: boolean
  ok: boolean
  nextRun: number | null
  /** True when the command's binary resolved inside $PREFIX. */
  resolved: boolean
  error: string
}

/** Result of GET /api/cron/runs. */
export interface CronRunsResponse {
  runs: CronRun[]
}

/** Result of GET /api/cron/crontab. */
export interface CronCrontabResponse {
  text: string
}
