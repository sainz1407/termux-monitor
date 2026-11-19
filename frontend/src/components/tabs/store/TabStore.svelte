<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import {
    Package,
    RefreshCw,
    Download,
    Terminal,
    X,
    Check,
    AlertCircle,
    Database,
    Code,
    Globe,
    Wrench,
    Network,
    Server,
    RotateCcw,
    Save,
    TriangleAlert,
  } from '@lucide/svelte'
  import { api } from '../../../api/client'
  import { toastStore } from '../../../stores/toast.svelte'
  import type {
    StoreItem,
    StoreListResponse,
    StoreLogResponse,
    StoreMirror,
    StoreMirrorResponse,
    StoreMirrorWriteResponse,
    StoreStatusResponse,
  } from '../../../types/store'
  import Card from '../../ui/Card.svelte'
  import Button from '../../ui/Button.svelte'
  import Badge from '../../ui/Badge.svelte'

  let status = $state<StoreStatusResponse | null>(null)
  let items = $state<StoreItem[]>([])
  let isLoading = $state(false)

  // Log panel state. `logKey` identifies the follow-up endpoint used for
  // polling: "install:<id>" for a package, "op:<name>" for manager ops.
  let logTitle = $state('')
  let logText = $state('')
  let logRunning = $state(false)
  let logKey = $state<string | null>(null)
  let pollTimer: ReturnType<typeof setInterval> | null = null

  // Busy markers so buttons reflect exactly what is running.
  let busyItem = $state<string | null>(null)
  let busyOp = $state<string | null>(null)

  // Level 1: a failed manager operation is surfaced as a dedicated banner with
  // a retry action, instead of a transient toast. The raw log line is kept so
  // the user can still read the original package-manager wording.
  type LogFailure = {
    title: string
    exitCode: number
    friendly: string
    hint: string
    raw: string
    /** Manager op to re-run, when the failure came from one. */
    op: 'update' | 'upgrade' | 'purge' | null
  }
  let logFailure = $state<LogFailure | null>(null)

  // Level 3: mirror management state.
  let mirror = $state<StoreMirrorResponse | null>(null)
  let mirrorLoading = $state(false)
  let mirrorBusy = $state(false)
  let selectedMirror = $state<string>('')
  let showMirrorPanel = $state(false)

  const GROUPS: { id: string; label: string; icon: typeof Package }[] = [
    { id: 'runtime', label: 'Runtime & Languages', icon: Code },
    { id: 'database', label: 'Databases', icon: Database },
    { id: 'web', label: 'Web & Tunnel', icon: Globe },
    { id: 'tools', label: 'Tooling', icon: Wrench },
    { id: 'network', label: 'Network & System', icon: Network },
  ]

  onMount(async () => {
    await Promise.all([fetchStatus(), fetchCatalog(), fetchMirror()])
  })

  onDestroy(() => {
    if (pollTimer) clearInterval(pollTimer)
  })

  async function fetchStatus() {
    try {
      status = await api.get<StoreStatusResponse>('/api/store/status')
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to query Termux status')
    }
  }

  async function fetchCatalog() {
    try {
      isLoading = true
      const res = await api.get<StoreListResponse>('/api/store/list')
      items = Array.isArray(res.items) ? res.items : []
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to load package catalog')
    } finally {
      isLoading = false
    }
  }

  function groupItems(groupId: string): StoreItem[] {
    return items.filter((it) => it.group === groupId)
  }

  /**
   * Classifies a package-manager failure into a friendly explanation. The raw
   * wording comes from `pkg`/apt (for example "Error: None of the mirrors are
   * accessible"), which is accurate but not actionable on its own.
   */
  function classifyFailure(
    title: string,
    exitCode: number,
    raw: string,
    op: 'update' | 'upgrade' | 'purge' | null,
  ): LogFailure {
    const text = raw.toLowerCase()
    let friendly = `${title} failed (exit code ${exitCode}).`
    let hint = 'Open the raw log below for the full output.'

    if (text.includes('none of the mirrors are accessible') || text.includes('unable to locate package') ||
        text.includes('failed to fetch') || text.includes('temporary failure in name resolution') ||
        text.includes('could not resolve')) {
      friendly = 'No package repository could be reached.'
      hint =
        'The active mirror is unreachable or cannot be resolved. Check the device connection, or switch the mirror in the Repository panel below, then run Update again.'
    } else if (text.includes('hash sum mismatch') || text.includes('signature') || text.includes('no_pubkey')) {
      friendly = 'Repository index integrity check failed.'
      hint = 'The cached index is stale. Run Update to refresh it before installing.'
    } else if (text.includes('not enough space') || text.includes('no space left')) {
      friendly = 'Not enough free storage to complete the operation.'
      hint = 'Free space on the device, then retry.'
    } else if (text.includes('permission denied') || text.includes('operation not permitted')) {
      friendly = 'The package manager was denied file access.'
      hint = 'This usually means a broken Termux installation; reinstalling Termux restores it.'
    } else if (text.includes('is another process') || text.includes('unable to lock') || text.includes('lock file')) {
      friendly = 'Another package-manager process holds the lock.'
      hint = 'Wait for the running operation to finish, then retry.'
    } else if (text.includes('404') || text.includes('not found')) {
      friendly = 'The repository does not have that package.'
      hint = 'Run Update first, then try again.'
    }

    // Keep the last meaningful line so the banner can quote the real error.
    const lines = raw
      .split('\n')
      .map((l) => l.trim())
      .filter((l) => l.length > 0 && !l.startsWith('---') && !l.startsWith('$ '))
    const lastLine = lines.length > 0 ? lines[lines.length - 1] : ''

    return { title, exitCode, friendly, hint, raw: lastLine, op }
  }

  /** Starts polling one log endpoint until the run reports a finished state. */
  function startPolling(
    key: string,
    title: string,
    fetchLog: () => Promise<StoreLogResponse>,
    op: 'update' | 'upgrade' | 'purge' | null = null,
  ) {
    if (pollTimer) clearInterval(pollTimer)
    logKey = key
    logTitle = title
    logText = ''
    logRunning = true
    logFailure = null

    const tick = async () => {
      try {
        const res = await fetchLog()
        logText = res.logs || ''
        logRunning = res.running
        if (!res.running && res.exitCode !== null && res.exitCode !== undefined) {
          stopPolling()
          if (res.exitCode === 0) {
            logFailure = null
            toastStore.success(`${title} finished successfully.`)
          } else {
            logFailure = classifyFailure(title, res.exitCode, res.logs || '', op)
            toastStore.error(logFailure.friendly)
          }
          await Promise.all([fetchCatalog(), fetchStatus()])
        }
      } catch (err: unknown) {
        stopPolling()
        toastStore.error(err instanceof Error ? err.message : 'Log stream failed')
      }
    }

    void tick()
    pollTimer = setInterval(tick, 1200)
  }

  function stopPolling() {
    if (pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
    logRunning = false
  }

  async function installItem(item: StoreItem) {
    if (busyItem || busyOp) return
    try {
      busyItem = item.id
      await api.post('/api/store/install', { id: item.id })
      toastStore.success(`Installing ${item.name}...`)
      startPolling(
        `install:${item.id}`,
        `Install ${item.name}`,
        () => api.get<StoreLogResponse>(`/api/store/install/log?id=${encodeURIComponent(item.id)}`),
        null,
      )
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : `Failed to install ${item.name}`)
    } finally {
      busyItem = null
    }
  }

  async function runOp(op: 'update' | 'upgrade' | 'purge', title: string) {
    if (busyOp || busyItem) return
    try {
      busyOp = op
      await api.post('/api/store/op', { op })
      toastStore.success(`${title} started...`)
      startPolling(
        `op:${op}`,
        title,
        () => api.get<StoreLogResponse>(`/api/store/op/log?op=${encodeURIComponent(op)}`),
        op,
      )
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : `${title} failed to start`)
    } finally {
      busyOp = null
    }
  }

  /** Re-runs the manager operation that produced the current failure banner. */
  async function retryFailure() {
    const failure = logFailure
    if (!failure) return
    if (failure.op) {
      await runOp(failure.op, failure.title)
      return
    }
    // A package install failure only records the item name, so replay it by
    // resolving the item again and calling the install path.
    const match = items.find((it) => failure.title === `Install ${it.name}`)
    if (match) {
      await installItem(match)
      return
    }
    toastStore.error('Nothing to retry; open Update to refresh the repositories.')
  }

  /** Loads the active sources.list and the curated mirror choices. */
  async function fetchMirror() {
    try {
      mirrorLoading = true
      const res = await api.get<StoreMirrorResponse>('/api/store/mirror')
      mirror = res
      selectedMirror = res.activeId || (res.mirrors.length > 0 ? res.mirrors[0].id : '')
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to read the repository configuration')
    } finally {
      mirrorLoading = false
    }
  }

  /**
   * Rewrites the active sources.list. The write is destructive (a wrong URL
   * leaves the package manager without a usable index), so it requires an
   * explicit confirmation; the backend stores a timestamped backup first.
   */
  async function applyMirror() {
    if (mirrorBusy || !mirror) return
    const target = mirror.mirrors.find((m) => m.id === selectedMirror)
    if (!target) {
      toastStore.error('Select a repository first.')
      return
    }
    if (target.id === mirror.activeId) {
      toastStore.info('That repository is already active.')
      return
    }
    const ok = window.confirm(
      `Rewrite the Termux package repository to "${target.name}"?\n\n` +
        `File: ${mirror.path}\n` +
        `A timestamped backup is saved first, so the change can be reverted.`,
    )
    if (!ok) return

    try {
      mirrorBusy = true
      const res = await api.post<StoreMirrorWriteResponse>('/api/store/mirror', {
        id: target.id,
        confirmed: true,
      })
      toastStore.success(
        res.backup
          ? `Repository switched to ${target.name}. Backup: ${res.backup}`
          : `Repository switched to ${target.name}.`,
      )
      await Promise.all([fetchMirror(), fetchStatus()])
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to rewrite sources.list')
    } finally {
      mirrorBusy = false
    }
  }

  /** Current active mirror label, falling back to the raw id or "custom". */
  const activeMirrorLabel = $derived.by(() => {
    if (!mirror) return ''
    const match = mirror.mirrors.find((m) => m.id === mirror!.activeId)
    if (match) return match.name
    return mirror.activeId || 'Unrecognised / custom'
  })

  function closeLog() {
    stopPolling()
    logKey = null
    logText = ''
  }

  const installedCount = $derived(items.filter((it) => it.installed).length)
</script>

<div class="space-y-3 sm:space-y-6">
  <!-- Top Bar -->
  <div class="flex flex-wrap items-center justify-between gap-2">
    <div class="flex items-center gap-2 min-w-0">
      <Package class="w-5 h-5 text-accent shrink-0" />
      <h2 class="text-base sm:text-lg font-mono font-bold uppercase tracking-wider text-foreground truncate">
        Termux Package Store
      </h2>
      <Badge variant="default">
        {installedCount}/{items.length} Installed
      </Badge>
    </div>

    <div class="flex items-center gap-2">
      <Button
        variant="secondary"
        size="sm"
        disabled={isLoading || !!busyOp}
        onclick={() => runOp('update', 'Repository update')}
      >
        <RefreshCw class="w-3.5 h-3.5 mr-1.5 {busyOp === 'update' ? 'animate-spin' : ''}" />
        <span>Update</span>
      </Button>
      <Button
        variant="secondary"
        size="sm"
        disabled={isLoading || !!busyOp}
        onclick={() => runOp('upgrade', 'Batch upgrade')}
      >
        <Download class="w-3.5 h-3.5 mr-1.5" />
        <span>Upgrade</span>
      </Button>
      <Button
        variant="secondary"
        size="sm"
        disabled={isLoading}
        onclick={fetchCatalog}
      >
        <RefreshCw class="w-3.5 h-3.5 mr-1.5 {isLoading ? 'animate-spin' : ''}" />
        <span>Refresh</span>
      </Button>
    </div>
  </div>

  <!-- Environment Notice -->
  {#if status && !status.available}
    <div class="flex items-start gap-2 p-3 border-2 border-border rounded-lg bg-card-sub font-mono text-xs text-foreground">
      <AlertCircle class="w-4 h-4 text-amber-400 shrink-0 mt-0.5" />
      <div>
        <span class="font-bold block">Termux environment unavailable</span>
        <span class="text-muted">{status.reason || 'Unknown reason'}</span>
      </div>
    </div>
  {/if}

  <!-- Level 1: failure banner with a retry action -->
  {#if logFailure}
    <div class="p-3 border-2 border-red-500/60 rounded-lg bg-red-500/5 font-mono text-xs space-y-2">
      <div class="flex items-start gap-2">
        <TriangleAlert class="w-4 h-4 text-red-400 shrink-0 mt-0.5" />
        <div class="min-w-0">
          <span class="font-bold block text-foreground">{logFailure.friendly}</span>
          <span class="text-muted block mt-0.5 leading-relaxed">{logFailure.hint}</span>
          {#if logFailure.raw}
            <span class="text-red-300/80 block mt-1 break-all">{logFailure.raw}</span>
          {/if}
        </div>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <Button
          variant="primary"
          size="sm"
          disabled={!!busyOp || !!busyItem}
          onclick={retryFailure}
        >
          <RotateCcw class="w-3.5 h-3.5 mr-1.5 {busyOp || busyItem ? 'animate-spin' : ''}" />
          <span>{logFailure.op ? 'Retry' : 'Run Update'}</span>
        </Button>
        <Button variant="secondary" size="sm" onclick={() => runOp('update', 'Repository update')}>
          <RefreshCw class="w-3.5 h-3.5 mr-1.5" />
          <span>Update</span>
        </Button>
        <Button variant="ghost" size="sm" onclick={() => (logFailure = null)}>
          <X class="w-3.5 h-3.5 mr-1.5" />
          <span>Dismiss</span>
        </Button>
      </div>
    </div>
  {/if}

  <!-- Level 3: repository (sources.list) panel -->
  <Card
    title="Repository"
    subtitle={activeMirrorLabel ? `Active: ${activeMirrorLabel}` : 'Termux apt sources.list'}
    tone="mint"
  >
    <div class="font-mono text-xs space-y-3">
      {#if mirrorLoading && !mirror}
        <div class="py-4 text-center text-muted">
          <RefreshCw class="w-5 h-5 animate-spin mx-auto mb-1.5 text-accent" />
          Reading sources.list...
        </div>
      {:else if !mirror}
        <div class="text-muted">Repository information is unavailable.</div>
      {:else}
        <div class="flex flex-wrap items-center gap-2">
          <Badge variant={mirror.activeId ? 'success' : 'warning'}>
            <Server class="w-3 h-3 mr-0.5" />
            {mirror.activeId ? 'Curated mirror active' : 'Unrecognised source'}
          </Badge>
          {#if mirror.exists === false}
            <Badge variant="danger">sources.list missing</Badge>
          {/if}
          {#if mirror.readError}
            <Badge variant="warning">{mirror.readError}</Badge>
          {/if}
          <span class="text-[10px] text-muted break-all">{mirror.path}</span>
        </div>

        <div class="flex flex-wrap items-center gap-2">
          <Button variant="secondary" size="sm" onclick={() => (showMirrorPanel = !showMirrorPanel)}>
            <Server class="w-3.5 h-3.5 mr-1.5" />
            <span>{showMirrorPanel ? 'Hide mirrors' : 'Change mirror'}</span>
          </Button>
          <Button variant="ghost" size="sm" disabled={mirrorLoading} onclick={fetchMirror}>
            <RefreshCw class="w-3.5 h-3.5 mr-1.5 {mirrorLoading ? 'animate-spin' : ''}" />
            <span>Reload</span>
          </Button>
          {#if mirror.backup}
            <span class="text-[10px] text-muted break-all">Backup: {mirror.backup}</span>
          {/if}
        </div>

        {#if showMirrorPanel}
          <div class="space-y-2">
            {#each mirror.mirrors as m (m.id)}
              <label
                class="flex items-start gap-2.5 p-2.5 bg-card-sub border-2 rounded-lg cursor-pointer transition-colors {selectedMirror === m.id
                  ? 'border-accent'
                  : 'border-border hover:border-accent/50'}"
              >
                <input
                  type="radio"
                  name="tmon-mirror"
                  class="mt-0.5 accent-current"
                  value={m.id}
                  bind:group={selectedMirror}
                />
                <div class="min-w-0">
                  <div class="flex items-center gap-2 flex-wrap">
                    <span class="font-bold text-foreground text-sm">{m.name}</span>
                    {#if mirror.activeId === m.id}
                      <Badge variant="success">
                        <Check class="w-3 h-3 mr-0.5" />
                        Active
                      </Badge>
                    {/if}
                  </div>
                  <p class="text-muted text-[11px] mt-0.5 leading-relaxed">{m.description}</p>
                  <span class="text-[10px] text-accent block mt-0.5 break-all">{m.host}</span>
                </div>
              </label>
            {/each}

            <div class="flex flex-wrap items-center gap-2 pt-1">
              <Button
                variant="primary"
                size="sm"
                disabled={mirrorBusy || selectedMirror === mirror.activeId}
                onclick={applyMirror}
              >
                <Save class="w-3.5 h-3.5 mr-1.5 {mirrorBusy ? 'animate-spin' : ''}" />
                <span>{mirrorBusy ? 'Writing' : 'Save mirror'}</span>
              </Button>
              <span class="text-[10px] text-muted">
                A timestamped backup of sources.list is stored before every change.
              </span>
            </div>
          </div>
        {/if}

        {#if mirror.content}
          <details>
            <summary class="cursor-pointer text-muted text-[11px]">sources.list content</summary>
            <pre class="mt-1.5 bg-black/80 border border-border rounded p-2.5 text-[10px] text-emerald-400 whitespace-pre-wrap max-h-56 overflow-y-auto break-all">{mirror.content}</pre>
          </details>
        {/if}
      {/if}
    </div>
  </Card>

  <div class="grid grid-cols-1 xl:grid-cols-3 gap-3 sm:gap-6">
    <!-- Catalog -->
    <div class="xl:col-span-2 space-y-3 sm:space-y-6">
      {#if isLoading && items.length === 0}
        <Card title="Package Catalog" subtitle="Available Termux packages" tone="lavender">
          <div class="p-8 text-center text-muted font-mono text-xs">
            <RefreshCw class="w-6 h-6 animate-spin mx-auto mb-2 text-accent" />
            Probing installed packages...
          </div>
        </Card>
      {:else if items.length === 0}
        <Card title="Package Catalog" subtitle="Available Termux packages" tone="lavender">
          <div class="p-8 text-center text-muted font-mono text-xs">
            <Package class="w-6 h-6 mx-auto mb-2 opacity-50" />
            No packages to display.
          </div>
        </Card>
      {:else}
        {#each GROUPS as group (group.id)}
          {@const list = groupItems(group.id)}
          {#if list.length > 0}
            <Card title={group.label} subtitle="{list.length} packages" tone="lavender">
              <div class="space-y-2.5 font-mono text-xs">
                {#each list as item (item.id)}
                  <div class="p-3 bg-card-sub border border-border rounded-lg flex items-start justify-between gap-3">
                    <div class="min-w-0">
                      <div class="flex items-center gap-2 flex-wrap">
                        <span class="font-bold text-foreground text-sm truncate">{item.name}</span>
                        {#if item.installed}
                          <Badge variant="success">
                            <Check class="w-3 h-3 mr-0.5" />
                            Installed
                          </Badge>
                        {/if}
                      </div>
                      <p class="text-muted text-[11px] mt-0.5 leading-relaxed">{item.desc}</p>
                      {#if item.conf}
                        <span class="text-[10px] text-accent block mt-0.5 break-all">{item.conf}</span>
                      {/if}
                      {#if item.installed && item.version}
                        <span class="text-[10px] text-muted block mt-0.5 break-all">{item.version}</span>
                      {/if}
                    </div>

                    <Button
                      variant={item.installed ? 'secondary' : 'primary'}
                      size="sm"
                      class="shrink-0"
                      disabled={!!busyItem || !!busyOp}
                      onclick={() => installItem(item)}
                    >
                      {#if busyItem === item.id}
                        <RefreshCw class="w-3.5 h-3.5 mr-1.5 animate-spin" />
                        <span>Starting</span>
                      {:else if item.installed}
                        <RefreshCw class="w-3.5 h-3.5 mr-1.5" />
                        <span>Reinstall</span>
                      {:else}
                        <Download class="w-3.5 h-3.5 mr-1.5" />
                        <span>Install</span>
                      {/if}
                    </Button>
                  </div>
                {/each}
              </div>
            </Card>
          {/if}
        {/each}
      {/if}
    </div>

    <!-- Live Log Panel -->
    <Card
      title="Installer Log"
      subtitle={logTitle || 'No active operation'}
      tone="peach"
      class="xl:col-span-1 xl:sticky xl:top-4 self-start"
    >
      {#if logKey === null}
        <div class="p-8 text-center text-muted font-mono text-xs">
          <Terminal class="w-6 h-6 mx-auto mb-2 opacity-50" />
          Run an install or manager operation to stream its output here.
        </div>
      {:else}
        <div class="space-y-2 font-mono text-xs">
          <div class="flex items-center justify-between">
            <Badge variant={logRunning ? 'info' : 'default'}>
              {logRunning ? 'Running' : 'Finished'}
            </Badge>
            <Button variant="ghost" size="sm" onclick={closeLog}>
              <X class="w-3.5 h-3.5" />
            </Button>
          </div>
          <pre class="bg-black/80 border border-border rounded p-2.5 text-[11px] text-emerald-400 whitespace-pre-wrap max-h-[28rem] overflow-y-auto break-all">{logText || 'Waiting for output...'}</pre>
        </div>
      {/if}

      {#if status?.available}
        <div class="mt-3 pt-3 border-t border-border font-mono text-[10px] text-muted space-y-0.5">
          <span class="block">Termux UID: <span class="text-foreground">{status.uid}</span></span>
          <span class="block break-all">PREFIX: {status.prefix}</span>
        </div>
      {/if}
    </Card>
  </div>
</div>
