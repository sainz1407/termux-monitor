<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import {
    Timer,
    RefreshCw,
    X,
    Check,
    AlertCircle,
    Play,
    Plus,
    Power,
    Trash2,
    FlaskConical,
    FileText,
    Search,
    History,
    CalendarClock,
  } from '@lucide/svelte'
  import { api } from '../../../api/client'
  import { toastStore } from '../../../stores/toast.svelte'
  import type {
    CronDaemonResponse,
    CronDryRunResult,
    CronJob,
    CronJobsResponse,
    CronRun,
    CronRunsResponse,
  } from '../../../types/cron'
  import Card from '../../ui/Card.svelte'
  import Button from '../../ui/Button.svelte'
  import Badge from '../../ui/Badge.svelte'

  let daemon = $state<CronDaemonResponse | null>(null)
  let jobs = $state<CronJob[]>([])
  let runs = $state<CronRun[]>([])
  let isLoading = $state(false)
  let serviceBusy = $state(false)
  let busyJob = $state<string | null>(null)

  // Filters.
  let query = $state('')
  let filter = $state<'all' | 'enabled' | 'disabled'>('all')

  // Add-job form.
  let showAdd = $state(false)
  let formName = $state('')
  let formSchedule = $state('*/5 * * * *')
  let formCommand = $state('')
  let adding = $state(false)

  // Dry-run modal.
  let dryRunOpen = $state(false)
  let dryRunBusy = $state(false)
  let dryRunResults = $state<CronDryRunResult[]>([])

  // Crontab viewer modal.
  let crontabOpen = $state(false)
  let crontabBusy = $state(false)
  let crontabText = $state('')

  let pollTimer: ReturnType<typeof setInterval> | null = null
  let tickerTimer: ReturnType<typeof setInterval> | null = null
  // Bumped every second so the countdown `$derived` values recompute.
  let nowMs = $state(Date.now())

  const PRESETS: { label: string; value: string }[] = [
    { label: 'Every minute', value: '* * * * *' },
    { label: 'Every 5 min', value: '*/5 * * * *' },
    { label: 'Every 15 min', value: '*/15 * * * *' },
    { label: 'Hourly', value: '0 * * * *' },
    { label: 'Daily 03:00', value: '0 3 * * *' },
    { label: 'Weekly Sun', value: '0 4 * * 0' },
    { label: 'Monthly 1st', value: '0 5 1 * *' },
  ]

  onMount(async () => {
    await refreshAll()
    pollTimer = setInterval(() => void refreshDaemon(), 5000)
    tickerTimer = setInterval(() => {
      nowMs = Date.now()
    }, 1000)
  })

  onDestroy(() => {
    if (pollTimer) clearInterval(pollTimer)
    if (tickerTimer) clearInterval(tickerTimer)
  })

  async function refreshDaemon() {
    try {
      daemon = await api.get<CronDaemonResponse>('/api/cron/daemon')
    } catch {
      // Transient polling failures stay silent; the next tick retries.
    }
  }

  async function refreshJobs() {
    try {
      const res = await api.get<CronJobsResponse>('/api/cron/jobs')
      jobs = Array.isArray(res.jobs) ? res.jobs : []
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to load cron jobs')
    }
  }

  async function refreshRuns() {
    try {
      const res = await api.get<CronRunsResponse>('/api/cron/runs?limit=50')
      runs = Array.isArray(res.runs) ? res.runs : []
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to load run history')
    }
  }

  async function refreshAll() {
    isLoading = true
    try {
      await Promise.all([refreshDaemon(), refreshJobs(), refreshRuns()])
    } finally {
      isLoading = false
    }
  }

  async function setService(action: 'start' | 'stop') {
    if (serviceBusy) return
    try {
      serviceBusy = true
      const res = await api.post<CronDaemonResponse & { ok: boolean }>('/api/cron/service', { action })
      daemon = {
        running: res.running,
        pid: res.pid,
        schedulerRunning: res.schedulerRunning,
        available: daemon?.available ?? true,
      }
      toastStore.success(action === 'start' ? 'crond started.' : 'crond stopped.')
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : `Failed to ${action} crond`)
    } finally {
      serviceBusy = false
    }
  }

  async function addJob() {
    if (adding) return
    const command = formCommand.trim()
    if (!command) {
      toastStore.error('Command is required.')
      return
    }
    try {
      adding = true
      await api.post('/api/cron/jobs/add', {
        name: formName.trim(),
        schedule: formSchedule.trim(),
        command,
      })
      toastStore.success('Job added.')
      showAdd = false
      formName = ''
      formCommand = ''
      formSchedule = '*/5 * * * *'
      await refreshJobs()
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to add job')
    } finally {
      adding = false
    }
  }

  async function toggleJob(job: CronJob) {
    if (busyJob) return
    try {
      busyJob = job.id
      await api.post('/api/cron/jobs/toggle', { id: job.id })
      await refreshJobs()
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to toggle job')
    } finally {
      busyJob = null
    }
  }

  async function deleteJob(job: CronJob) {
    if (busyJob) return
    if (!confirm(`Delete job "${job.name}"?`)) return
    try {
      busyJob = job.id
      await api.post('/api/cron/jobs/delete', { id: job.id })
      toastStore.success('Job deleted.')
      await refreshJobs()
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to delete job')
    } finally {
      busyJob = null
    }
  }

  async function runJob(job: CronJob) {
    if (busyJob) return
    try {
      busyJob = job.id
      const res = await api.post<{ ok: boolean; run: CronRun }>('/api/cron/jobs/run', { id: job.id })
      if (res.run?.exitCode === 0) {
        toastStore.success(`"${job.name}" exited 0 in ${res.run.latencyMs} ms.`)
      } else if (res.run?.timedOut) {
        toastStore.error(`"${job.name}" timed out.`)
      } else {
        toastStore.error(`"${job.name}" exited with code ${res.run?.exitCode ?? 'unknown'}.`)
      }
      await refreshRuns()
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to run job')
    } finally {
      busyJob = null
    }
  }

  async function openDryRun() {
    dryRunOpen = true
    dryRunBusy = true
    try {
      const res = await api.post<{ results: CronDryRunResult[] }>('/api/cron/jobs/dry-run', {})
      dryRunResults = Array.isArray(res.results) ? res.results : []
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Dry run failed')
      dryRunOpen = false
    } finally {
      dryRunBusy = false
    }
  }

  async function openCrontab() {
    crontabOpen = true
    crontabBusy = true
    try {
      const res = await api.get<{ text: string }>('/api/cron/crontab')
      crontabText = res.text || ''
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to read crontab')
      crontabOpen = false
    } finally {
      crontabBusy = false
    }
  }

  /** Human readable countdown to a next-run timestamp. */
  function countdown(target: number | null): string {
    if (!target) return '—'
    const diff = target - nowMs
    if (diff <= 0) return 'due now'
    const total = Math.floor(diff / 1000)
    const d = Math.floor(total / 86400)
    const h = Math.floor((total % 86400) / 3600)
    const m = Math.floor((total % 3600) / 60)
    const s = total % 60
    if (d > 0) return `${d}d ${h}h`
    if (h > 0) return `${h}h ${m}m`
    if (m > 0) return `${m}m ${s}s`
    return `${s}s`
  }

  function formatTime(ms: number): string {
    if (!ms) return '—'
    return new Date(ms).toLocaleString()
  }

  const filteredJobs = $derived(
    jobs.filter((job) => {
      if (filter === 'enabled' && !job.enabled) return false
      if (filter === 'disabled' && job.enabled) return false
      const q = query.trim().toLowerCase()
      if (!q) return true
      return (
        job.name.toLowerCase().includes(q) ||
        job.command.toLowerCase().includes(q) ||
        job.schedule.toLowerCase().includes(q)
      )
    }),
  )

  const enabledCount = $derived(jobs.filter((j) => j.enabled).length)

  const last24h = $derived.by(() => {
    const cutoff = nowMs - 24 * 3600 * 1000
    const window = runs.filter((r) => r.finishedAt >= cutoff)
    const ok = window.filter((r) => !r.timedOut && r.exitCode === 0).length
    return { total: window.length, ok, failed: window.length - ok }
  })
</script>

<div class="space-y-3 sm:space-y-6">
  <!-- Top Bar -->
  <div class="flex flex-wrap items-center justify-between gap-2">
    <div class="flex items-center gap-2 min-w-0">
      <Timer class="w-5 h-5 text-accent shrink-0" />
      <h2 class="text-base sm:text-lg font-mono font-bold uppercase tracking-wider text-foreground truncate">
        Scheduled Jobs
      </h2>
      <Badge variant="default">{enabledCount}/{jobs.length} Active</Badge>
    </div>

    <div class="flex items-center gap-2 flex-wrap">
      <Button variant="secondary" size="sm" disabled={isLoading} onclick={openCrontab}>
        <FileText class="w-3.5 h-3.5 mr-1.5" />
        <span>Crontab</span>
      </Button>
      <Button variant="secondary" size="sm" disabled={isLoading} onclick={openDryRun}>
        <FlaskConical class="w-3.5 h-3.5 mr-1.5" />
        <span>Dry Run</span>
      </Button>
      <Button variant="secondary" size="sm" disabled={isLoading} onclick={refreshAll}>
        <RefreshCw class="w-3.5 h-3.5 mr-1.5 {isLoading ? 'animate-spin' : ''}" />
        <span>Refresh</span>
      </Button>
      <Button variant="primary" size="sm" onclick={() => (showAdd = !showAdd)}>
        <Plus class="w-3.5 h-3.5 mr-1.5" />
        <span>New Job</span>
      </Button>
    </div>
  </div>

  <!-- Environment notice -->
  {#if daemon && !daemon.available}
    <div class="flex items-start gap-2 p-3 border-2 border-border rounded-lg bg-card-sub font-mono text-xs text-foreground">
      <AlertCircle class="w-4 h-4 text-amber-400 shrink-0 mt-0.5" />
      <div>
        <span class="font-bold block">Termux environment unavailable</span>
        <span class="text-muted">Cron jobs cannot be read or written.</span>
      </div>
    </div>
  {/if}

  <!-- Daemon status -->
  <Card title="Cron Daemon" subtitle="Who executes the scheduled jobs" tone="mint">
    <div class="grid grid-cols-1 sm:grid-cols-2 gap-3 font-mono text-xs">
      <div class="flex items-center gap-2 flex-wrap">
        {#if daemon?.running}
          <Badge variant="success">
            <Check class="w-3 h-3 mr-0.5" />
            crond running
          </Badge>
          {#if daemon.pid > 0}
            <Badge variant="default">PID {daemon.pid}</Badge>
          {/if}
        {:else if daemon?.schedulerRunning}
          <Badge variant="warning">
            <Power class="w-3 h-3 mr-0.5" />
            Fallback scheduler
          </Badge>
        {:else}
          <Badge variant="danger">stopped</Badge>
        {/if}
      </div>

      <div class="flex items-center gap-2 sm:justify-end">
        {#if daemon?.running}
          <Button variant="stop" size="sm" disabled={serviceBusy} onclick={() => setService('stop')}>
            {#if serviceBusy}
              <RefreshCw class="w-3.5 h-3.5 mr-1.5 animate-spin" />
            {:else}
              <Power class="w-3.5 h-3.5 mr-1.5" />
            {/if}
            <span>Stop crond</span>
          </Button>
        {:else}
          <Button variant="start" size="sm" disabled={serviceBusy} onclick={() => setService('start')}>
            {#if serviceBusy}
              <RefreshCw class="w-3.5 h-3.5 mr-1.5 animate-spin" />
            {:else}
              <Power class="w-3.5 h-3.5 mr-1.5" />
            {/if}
            <span>Start crond</span>
          </Button>
        {/if}
      </div>
    </div>

    <p class="text-[11px] text-muted font-mono mt-2 leading-relaxed">
      {#if daemon?.running}
        Cronie is handling the schedule. Jobs fire inside Termux even when this panel is closed.
      {:else if daemon?.schedulerRunning}
        Cronie is not running, so this server executes tagged jobs itself. It stops when the panel exits.
      {:else}
        No scheduler is active. Start crond or install Cronie from the Store.
      {/if}
    </p>
  </Card>

  <!-- Add job form -->
  {#if showAdd}
    <Card title="New Job" subtitle="Five-field cron expression" tone="lavender">
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3 font-mono text-xs">
        <label class="block">
          <span class="text-muted block mb-1">Name</span>
          <input
            type="text"
            bind:value={formName}
            placeholder="Backup nightly"
            class="w-full px-2.5 py-1.5 bg-card-sub border border-border rounded text-foreground text-xs font-mono focus:outline-none focus:border-accent"
          />
        </label>
        <label class="block">
          <span class="text-muted block mb-1">Schedule</span>
          <input
            type="text"
            bind:value={formSchedule}
            placeholder="*/5 * * * *"
            class="w-full px-2.5 py-1.5 bg-card-sub border border-border rounded text-foreground text-xs font-mono focus:outline-none focus:border-accent"
          />
        </label>
        <label class="block sm:col-span-2">
          <span class="text-muted block mb-1">Command (runs inside Termux)</span>
          <input
            type="text"
            bind:value={formCommand}
            placeholder="bash ~/backup.sh"
            class="w-full px-2.5 py-1.5 bg-card-sub border border-border rounded text-foreground text-xs font-mono focus:outline-none focus:border-accent"
          />
        </label>
      </div>

      <div class="flex flex-wrap gap-1.5 mt-3">
        {#each PRESETS as preset (preset.value)}
          <button
            type="button"
            onclick={() => (formSchedule = preset.value)}
            class="px-2 py-0.5 text-[10px] font-mono font-bold uppercase tracking-wider border rounded transition-colors {formSchedule === preset.value
              ? 'bg-accent text-accent-text border-border'
              : 'bg-card-sub text-muted border-border hover:text-foreground'}"
          >
            {preset.label}
          </button>
        {/each}
      </div>

      <p class="text-[10px] text-muted font-mono mt-3">
        Format: minute hour day-of-month month day-of-week. Names like <span class="text-foreground">MON</span> and
        <span class="text-foreground">JAN</span> are accepted.
      </p>

      <div class="flex items-center gap-2 mt-3">
        <Button variant="primary" size="sm" disabled={adding} onclick={addJob}>
          {#if adding}
            <RefreshCw class="w-3.5 h-3.5 mr-1.5 animate-spin" />
          {:else}
            <Plus class="w-3.5 h-3.5 mr-1.5" />
          {/if}
          <span>Add Job</span>
        </Button>
        <Button variant="ghost" size="sm" onclick={() => (showAdd = false)}>Cancel</Button>
      </div>
    </Card>
  {/if}

  <div class="grid grid-cols-1 xl:grid-cols-3 gap-3 sm:gap-6">
    <!-- Jobs -->
    <div class="xl:col-span-2 space-y-3 sm:space-y-6">
      <Card title="Jobs" subtitle="{filteredJobs.length} shown" tone="peach">
        <div class="flex flex-wrap items-center gap-2 mb-3">
          <div class="relative flex-1 min-w-[10rem]">
            <Search class="w-3.5 h-3.5 absolute left-2 top-1/2 -translate-y-1/2 text-muted" />
            <input
              type="text"
              bind:value={query}
              placeholder="Search jobs..."
              class="w-full pl-7 pr-2.5 py-1.5 bg-card-sub border border-border rounded text-foreground text-xs font-mono focus:outline-none focus:border-accent"
            />
          </div>
          <div class="flex gap-1">
            {#each [{ id: 'all', label: 'All' }, { id: 'enabled', label: 'On' }, { id: 'disabled', label: 'Off' }] as opt (opt.id)}
              <button
                type="button"
                onclick={() => (filter = opt.id as 'all' | 'enabled' | 'disabled')}
                class="px-2 py-1 text-[10px] font-mono font-bold uppercase tracking-wider border rounded transition-colors {filter === opt.id
                  ? 'bg-accent text-accent-text border-border'
                  : 'bg-card-sub text-muted border-border hover:text-foreground'}"
              >
                {opt.label}
              </button>
            {/each}
          </div>
        </div>

        {#if isLoading && jobs.length === 0}
          <div class="p-8 text-center text-muted font-mono text-xs">
            <RefreshCw class="w-6 h-6 animate-spin mx-auto mb-2 text-accent" />
            Loading jobs...
          </div>
        {:else if filteredJobs.length === 0}
          <div class="p-8 text-center text-muted font-mono text-xs">
            <Timer class="w-6 h-6 mx-auto mb-2 opacity-50" />
            {jobs.length === 0 ? 'No scheduled jobs yet.' : 'No jobs match the filter.'}
          </div>
        {:else}
          <div class="space-y-2.5 font-mono text-xs">
            {#each filteredJobs as job (job.id)}
              <div class="p-3 bg-card-sub border border-border rounded-lg">
                <div class="flex items-start justify-between gap-3">
                  <div class="min-w-0">
                    <div class="flex items-center gap-2 flex-wrap">
                      <span class="font-bold text-foreground text-sm truncate">{job.name}</span>
                      {#if job.enabled}
                        <Badge variant="success">
                          <Check class="w-3 h-3 mr-0.5" />
                          Active
                        </Badge>
                      {:else}
                        <Badge variant="warning">Paused</Badge>
                      {/if}
                      <Badge variant="default">{job.schedule}</Badge>
                    </div>
                    <p class="text-muted text-[11px] mt-1 break-all">{job.command}</p>
                    <div class="flex items-center gap-3 mt-1 text-[10px] text-muted">
                      <span class="inline-flex items-center gap-1">
                        <CalendarClock class="w-3 h-3" />
                        {#if job.enabled}
                          next in {countdown(job.nextRun)}
                        {:else}
                          paused
                        {/if}
                      </span>
                      <span class="text-accent">{job.id}</span>
                    </div>
                  </div>

                  <div class="flex items-center gap-1.5 shrink-0">
                    <Button
                      variant="secondary"
                      size="sm"
                      title="Run now"
                      disabled={busyJob === job.id}
                      onclick={() => runJob(job)}
                    >
                      {#if busyJob === job.id}
                        <RefreshCw class="w-3.5 h-3.5 animate-spin" />
                      {:else}
                        <Play class="w-3.5 h-3.5" />
                      {/if}
                    </Button>
                    <Button
                      variant={job.enabled ? 'restart' : 'start'}
                      size="sm"
                      title={job.enabled ? 'Pause job' : 'Resume job'}
                      disabled={busyJob === job.id}
                      onclick={() => toggleJob(job)}
                    >
                      <Power class="w-3.5 h-3.5" />
                    </Button>
                    <Button
                      variant="danger"
                      size="sm"
                      title="Delete job"
                      disabled={busyJob === job.id}
                      onclick={() => deleteJob(job)}
                    >
                      <Trash2 class="w-3.5 h-3.5" />
                    </Button>
                  </div>
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </Card>
    </div>

    <!-- Run history -->
    <div class="xl:col-span-1 space-y-3 sm:space-y-6">
      <Card title="Last 24 Hours" tone="butter">
        <div class="grid grid-cols-3 gap-2 font-mono text-center">
          <div class="p-2 bg-card-sub border border-border rounded">
            <span class="block text-lg font-bold text-foreground">{last24h.total}</span>
            <span class="text-[10px] text-muted uppercase tracking-wider">Runs</span>
          </div>
          <div class="p-2 bg-card-sub border border-border rounded">
            <span class="block text-lg font-bold text-emerald-400">{last24h.ok}</span>
            <span class="text-[10px] text-muted uppercase tracking-wider">OK</span>
          </div>
          <div class="p-2 bg-card-sub border border-border rounded">
            <span class="block text-lg font-bold text-red-400">{last24h.failed}</span>
            <span class="text-[10px] text-muted uppercase tracking-wider">Failed</span>
          </div>
        </div>
      </Card>

      <Card title="Run History" subtitle="Manual runs recorded here" tone="ice">
        {#if runs.length === 0}
          <div class="p-6 text-center text-muted font-mono text-xs">
            <History class="w-6 h-6 mx-auto mb-2 opacity-50" />
            No runs recorded yet.
          </div>
        {:else}
          <div class="space-y-2 font-mono text-xs max-h-[30rem] overflow-y-auto">
            {#each runs as run (run.id)}
              <div class="p-2.5 bg-card-sub border border-border rounded-lg">
                <div class="flex items-center justify-between gap-2">
                  <span class="font-bold text-foreground truncate">{run.name}</span>
                  {#if run.timedOut}
                    <Badge variant="warning">Timeout</Badge>
                  {:else if run.exitCode === 0}
                    <Badge variant="success">0</Badge>
                  {:else}
                    <Badge variant="danger">{run.exitCode ?? '?'}</Badge>
                  {/if}
                </div>
                <div class="flex items-center justify-between gap-2 text-[10px] text-muted mt-1">
                  <span>{formatTime(run.startedAt)}</span>
                  <span>{run.latencyMs} ms</span>
                </div>
                {#if run.output}
                  <pre class="mt-1.5 bg-black/80 border border-border rounded p-2 text-[10px] text-emerald-400 whitespace-pre-wrap max-h-32 overflow-y-auto break-all">{run.output}</pre>
                {/if}
              </div>
            {/each}
          </div>
        {/if}
      </Card>
    </div>
  </div>
</div>

<!-- Dry run modal -->
{#if dryRunOpen}
  <div class="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/70" role="presentation">
    <div class="w-full max-w-2xl max-h-[80vh] overflow-y-auto bg-card border-2 border-border rounded-lg p-4 font-mono text-xs">
      <div class="flex items-center justify-between border-b border-border pb-2 mb-3">
        <h3 class="text-sm font-bold uppercase tracking-wider text-foreground">Dry Run</h3>
        <Button variant="ghost" size="sm" onclick={() => (dryRunOpen = false)}>
          <X class="w-3.5 h-3.5" />
        </Button>
      </div>

      {#if dryRunBusy}
        <div class="p-6 text-center text-muted">
          <RefreshCw class="w-5 h-5 animate-spin mx-auto mb-2 text-accent" />
          Validating jobs...
        </div>
      {:else if dryRunResults.length === 0}
        <div class="p-6 text-center text-muted">No jobs to validate.</div>
      {:else}
        <div class="space-y-2">
          {#each dryRunResults as result (result.id)}
            <div class="p-2.5 bg-card-sub border border-border rounded-lg">
              <div class="flex items-center justify-between gap-2">
                <span class="font-bold text-foreground truncate">{result.name}</span>
                {#if result.ok}
                  <Badge variant="success">
                    <Check class="w-3 h-3 mr-0.5" />
                    OK
                  </Badge>
                {:else}
                  <Badge variant="danger">Problem</Badge>
                {/if}
              </div>
              <div class="text-[10px] text-muted mt-1">
                <span class="block">schedule: <span class="text-foreground">{result.nextRun ? formatTime(result.nextRun) : '—'}</span></span>
                {#if result.error}
                  <span class="block text-red-400">{result.error}</span>
                {/if}
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </div>
{/if}

<!-- Crontab viewer modal -->
{#if crontabOpen}
  <div class="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/70" role="presentation">
    <div class="w-full max-w-2xl max-h-[80vh] overflow-y-auto bg-card border-2 border-border rounded-lg p-4 font-mono text-xs">
      <div class="flex items-center justify-between border-b border-border pb-2 mb-3">
        <h3 class="text-sm font-bold uppercase tracking-wider text-foreground">Raw Crontab</h3>
        <div class="flex items-center gap-1.5">
          <a href="/api/cron/download" download>
            <Button variant="secondary" size="sm">Download</Button>
          </a>
          <Button variant="ghost" size="sm" onclick={() => (crontabOpen = false)}>
            <X class="w-3.5 h-3.5" />
          </Button>
        </div>
      </div>

      {#if crontabBusy}
        <div class="p-6 text-center text-muted">
          <RefreshCw class="w-5 h-5 animate-spin mx-auto mb-2 text-accent" />
          Reading crontab...
        </div>
      {:else}
        <pre class="bg-black/80 border border-border rounded p-2.5 text-[11px] text-emerald-400 whitespace-pre-wrap max-h-[60vh] overflow-y-auto break-all">{crontabText || 'Crontab is empty.'}</pre>
      {/if}
    </div>
  </div>
{/if}
