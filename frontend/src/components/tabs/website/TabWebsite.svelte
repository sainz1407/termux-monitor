<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import {
    Globe,
    RefreshCw,
    X,
    Check,
    AlertCircle,
    Play,
    Plus,
    Power,
    Trash2,
    FileText,
    Search,
    RotateCw,
    ExternalLink,
    Terminal,
  } from '@lucide/svelte'
  import { api } from '../../../api/client'
  import { toastStore } from '../../../stores/toast.svelte'
  import type {
    WebsiteAddResponse,
    WebsiteEngine,
    WebsiteEngineInfo,
    WebsiteListResponse,
    WebsiteLogsResponse,
    WebsiteProbeResponse,
    WebsiteSiteView,
  } from '../../../types/website'
  import Card from '../../ui/Card.svelte'
  import Button from '../../ui/Button.svelte'
  import Badge from '../../ui/Badge.svelte'

  let available = $state(true)
  let reason = $state('')
  let sites = $state<WebsiteSiteView[]>([])
  let engines = $state<WebsiteEngine[]>([])
  let engineInfo = $state<WebsiteEngineInfo[]>([])
  let isLoading = $state(false)
  let busySite = $state<string | null>(null)

  // Filters.
  let query = $state('')
  let engineFilter = $state<'all' | WebsiteEngine>('all')
  let statusFilter = $state<'all' | 'running' | 'stopped'>('all')

  // Add-site form.
  let showAdd = $state(false)
  let formName = $state('')
  let formEngine = $state<WebsiteEngine>('static')
  let formPath = $state('')
  let formPort = $state('8080')
  let adding = $state(false)

  // Log modal.
  let logOpen = $state(false)
  let logSite = $state<WebsiteSiteView | null>(null)
  let logText = $state('')
  let logBusy = $state(false)

  let pollTimer: ReturnType<typeof setInterval> | null = null
  // Bumped every second so the uptime `$derived` values recompute.
  let nowMs = $state(Date.now())

  const ENGINE_LABELS: Record<WebsiteEngine, string> = {
    node: 'Node.js',
    php: 'PHP',
    static: 'Static (python3)',
  }

  const ENGINE_HINTS: Record<WebsiteEngine, string> = {
    node: 'Path must point to a .js file inside Termux.',
    php: 'Path must point to a directory. Served with `php -S`.',
    static: 'Path must point to a directory. Served with `python3 -m http.server`.',
  }

  onMount(async () => {
    await refreshAll()
    pollTimer = setInterval(() => {
      nowMs = Date.now()
      void refreshSites()
    }, 5000)
  })

  onDestroy(() => {
    if (pollTimer) clearInterval(pollTimer)
  })

  async function refreshSites() {
    try {
      const res = await api.get<WebsiteListResponse>('/api/website/list')
      available = res.available
      reason = res.reason || ''
      sites = Array.isArray(res.sites) ? res.sites : []
      engines = Array.isArray(res.engines) ? res.engines : []
    } catch {
      // Transient polling failures stay silent; the next tick retries.
    }
  }

  async function refreshProbe() {
    try {
      const res = await api.get<WebsiteProbeResponse>('/api/website/probe')
      engineInfo = Array.isArray(res.engines) ? res.engines : []
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to probe engines')
    }
  }

  async function refreshAll() {
    isLoading = true
    try {
      await Promise.all([refreshSites(), refreshProbe()])
    } finally {
      isLoading = false
    }
  }

  async function addSite() {
    if (adding) return
    const name = formName.trim()
    const path = formPath.trim()
    const port = Number.parseInt(formPort, 10)
    if (!name) {
      toastStore.error('Name is required.')
      return
    }
    if (!path) {
      toastStore.error('Path is required.')
      return
    }
    if (!Number.isFinite(port)) {
      toastStore.error('Port must be a number.')
      return
    }
    try {
      adding = true
      await api.post<WebsiteAddResponse>('/api/website/add', {
        name,
        engine: formEngine,
        path,
        port,
      })
      toastStore.success('Site added.')
      showAdd = false
      formName = ''
      formPath = ''
      formPort = '8080'
      await refreshAll()
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to add site')
    } finally {
      adding = false
    }
  }

  async function siteAction(site: WebsiteSiteView, action: 'start' | 'stop' | 'restart') {
    if (busySite) return
    try {
      busySite = site.id
      await api.post(`/api/website/${action}`, { id: site.id })
      toastStore.success(`"${site.name}" ${action === 'stop' ? 'stopped' : action + 'ed'}.`)
      await refreshSites()
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : `Failed to ${action} site`)
    } finally {
      busySite = null
    }
  }

  async function deleteSite(site: WebsiteSiteView) {
    if (busySite) return
    if (!confirm(`Delete site "${site.name}"? Its process will be stopped.`)) return
    try {
      busySite = site.id
      await api.post('/api/website/delete', { id: site.id })
      toastStore.success('Site deleted.')
      await refreshSites()
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to delete site')
    } finally {
      busySite = null
    }
  }

  async function openLogs(site: WebsiteSiteView) {
    logOpen = true
    logSite = site
    logText = ''
    await loadLogs()
  }

  async function loadLogs() {
    if (!logSite) return
    logBusy = true
    try {
      const res = await api.get<WebsiteLogsResponse>(`/api/website/logs?id=${encodeURIComponent(logSite.id)}`)
      logText = res.logs || ''
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to read site log')
      logOpen = false
    } finally {
      logBusy = false
    }
  }

  /** Human readable uptime from a millisecond duration. */
  function formatUptime(ms: number): string {
    if (!ms || ms <= 0) return '—'
    const total = Math.floor(ms / 1000)
    const d = Math.floor(total / 86400)
    const h = Math.floor((total % 86400) / 3600)
    const m = Math.floor((total % 3600) / 60)
    const s = total % 60
    if (d > 0) return `${d}d ${h}h`
    if (h > 0) return `${h}h ${m}m`
    if (m > 0) return `${m}m ${s}s`
    return `${s}s`
  }

  const filteredSites = $derived(
    sites.filter((site) => {
      if (engineFilter !== 'all' && site.engine !== engineFilter) return false
      if (statusFilter === 'running' && !site.running) return false
      if (statusFilter === 'stopped' && site.running) return false
      const q = query.trim().toLowerCase()
      if (!q) return true
      return (
        site.name.toLowerCase().includes(q) ||
        site.path.toLowerCase().includes(q) ||
        site.engine.toLowerCase().includes(q)
      )
    }),
  )

  const runningCount = $derived(sites.filter((s) => s.running).length)

  // Engines offered in the add-site form: the live list from the API when
  // available, otherwise a static fallback so the form still works offline.
  const engineOptions = $derived(
    engines.length ? engines : (['node', 'php', 'static'] as WebsiteEngine[]),
  )
</script>

<div class="space-y-3 sm:space-y-6">
  <!-- Top Bar -->
  <div class="flex flex-wrap items-center justify-between gap-2">
    <div class="flex items-center gap-2 min-w-0">
      <Globe class="w-5 h-5 text-accent shrink-0" />
      <h2 class="text-base sm:text-lg font-mono font-bold uppercase tracking-wider text-foreground truncate">
        Sites
      </h2>
      <Badge variant="default">{runningCount}/{sites.length} Running</Badge>
    </div>

    <div class="flex items-center gap-2 flex-wrap">
      <Button variant="secondary" size="sm" disabled={isLoading} onclick={refreshAll}>
        <RefreshCw class="w-3.5 h-3.5 mr-1.5 {isLoading ? 'animate-spin' : ''}" />
        <span>Refresh</span>
      </Button>
      <Button variant="primary" size="sm" onclick={() => (showAdd = !showAdd)}>
        <Plus class="w-3.5 h-3.5 mr-1.5" />
        <span>New Site</span>
      </Button>
    </div>
  </div>

  <!-- Environment notice -->
  {#if !available}
    <div class="flex items-start gap-2 p-3 border-2 border-border rounded-lg bg-card-sub font-mono text-xs text-foreground">
      <AlertCircle class="w-4 h-4 text-amber-400 shrink-0 mt-0.5" />
      <div>
        <span class="font-bold block">Termux environment unavailable</span>
        <span class="text-muted">{reason || 'Sites cannot be started.'}</span>
      </div>
    </div>
  {/if}

  <!-- Engine inventory -->
  <Card title="Engines" subtitle="Runtime availability inside Termux" tone="mint">
    <div class="grid grid-cols-1 sm:grid-cols-3 gap-2 font-mono text-xs">
      {#each engineInfo as info (info.engine)}
        <div class="p-2.5 bg-card-sub border border-border rounded-lg flex items-center justify-between gap-2">
          <div class="min-w-0">
            <span class="block font-bold text-foreground truncate">{ENGINE_LABELS[info.engine] ?? info.engine}</span>
            <span class="text-[10px] text-muted">{info.bin}</span>
          </div>
          {#if info.installed}
            <Badge variant="success">
              <Check class="w-3 h-3 mr-0.5" />
              ready
            </Badge>
          {:else}
            <Badge variant="danger">missing</Badge>
          {/if}
        </div>
      {/each}
    </div>
    <p class="text-[11px] text-muted font-mono mt-2 leading-relaxed">
      Sites bind to <span class="text-foreground">127.0.0.1</span> only. Missing engines can be installed from the Store tab.
    </p>
  </Card>

  <!-- Add site form -->
  {#if showAdd}
    <Card title="New Site" subtitle="Binds to the loopback interface" tone="lavender">
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3 font-mono text-xs">
        <label class="block">
          <span class="text-muted block mb-1">Name</span>
          <input
            type="text"
            bind:value={formName}
            placeholder="My blog"
            class="w-full px-2.5 py-1.5 bg-card-sub border border-border rounded text-foreground text-xs font-mono focus:outline-none focus:border-accent"
          />
        </label>
        <label class="block">
          <span class="text-muted block mb-1">Engine</span>
          <select
            bind:value={formEngine}
            class="w-full px-2.5 py-1.5 bg-card-sub border border-border rounded text-foreground text-xs font-mono focus:outline-none focus:border-accent"
          >
            {#each engineOptions as engine (engine)}
              <option value={engine}>{ENGINE_LABELS[engine] ?? engine}</option>
            {/each}
          </select>
        </label>
        <label class="block">
          <span class="text-muted block mb-1">Port</span>
          <input
            type="number"
            min="1024"
            max="65535"
            bind:value={formPort}
            placeholder="8080"
            class="w-full px-2.5 py-1.5 bg-card-sub border border-border rounded text-foreground text-xs font-mono focus:outline-none focus:border-accent"
          />
        </label>
        <label class="block">
          <span class="text-muted block mb-1">Path (inside Termux)</span>
          <input
            type="text"
            bind:value={formPath}
            placeholder="~/mysite"
            class="w-full px-2.5 py-1.5 bg-card-sub border border-border rounded text-foreground text-xs font-mono focus:outline-none focus:border-accent"
          />
        </label>
      </div>

      <p class="text-[10px] text-muted font-mono mt-3">{ENGINE_HINTS[formEngine]}</p>

      <div class="flex items-center gap-2 mt-3">
        <Button variant="primary" size="sm" disabled={adding} onclick={addSite}>
          {#if adding}
            <RefreshCw class="w-3.5 h-3.5 mr-1.5 animate-spin" />
          {:else}
            <Plus class="w-3.5 h-3.5 mr-1.5" />
          {/if}
          <span>Add Site</span>
        </Button>
        <Button variant="ghost" size="sm" onclick={() => (showAdd = false)}>Cancel</Button>
      </div>
    </Card>
  {/if}

  <!-- Sites -->
  <Card title="Managed Sites" subtitle="{filteredSites.length} shown" tone="peach">
    <div class="flex flex-wrap items-center gap-2 mb-3">
      <div class="relative flex-1 min-w-[10rem]">
        <Search class="w-3.5 h-3.5 absolute left-2 top-1/2 -translate-y-1/2 text-muted" />
        <input
          type="text"
          bind:value={query}
          placeholder="Search sites..."
          class="w-full pl-7 pr-2.5 py-1.5 bg-card-sub border border-border rounded text-foreground text-xs font-mono focus:outline-none focus:border-accent"
        />
      </div>

      <div class="flex gap-1">
        {#each [{ id: 'all', label: 'All' }, { id: 'node', label: 'Node' }, { id: 'php', label: 'PHP' }, { id: 'static', label: 'Static' }] as opt (opt.id)}
          <button
            type="button"
            onclick={() => (engineFilter = opt.id as 'all' | WebsiteEngine)}
            class="px-2 py-1 text-[10px] font-mono font-bold uppercase tracking-wider border rounded transition-colors {engineFilter === opt.id
              ? 'bg-accent text-accent-text border-border'
              : 'bg-card-sub text-muted border-border hover:text-foreground'}"
          >
            {opt.label}
          </button>
        {/each}
      </div>

      <div class="flex gap-1">
        {#each [{ id: 'all', label: 'Any' }, { id: 'running', label: 'Up' }, { id: 'stopped', label: 'Down' }] as opt (opt.id)}
          <button
            type="button"
            onclick={() => (statusFilter = opt.id as 'all' | 'running' | 'stopped')}
            class="px-2 py-1 text-[10px] font-mono font-bold uppercase tracking-wider border rounded transition-colors {statusFilter === opt.id
              ? 'bg-accent text-accent-text border-border'
              : 'bg-card-sub text-muted border-border hover:text-foreground'}"
          >
            {opt.label}
          </button>
        {/each}
      </div>
    </div>

    {#if isLoading && sites.length === 0}
      <div class="p-8 text-center text-muted font-mono text-xs">
        <RefreshCw class="w-6 h-6 animate-spin mx-auto mb-2 text-accent" />
        Loading sites...
      </div>
    {:else if filteredSites.length === 0}
      <div class="p-8 text-center text-muted font-mono text-xs">
        <Globe class="w-6 h-6 mx-auto mb-2 opacity-50" />
        {sites.length === 0 ? 'No sites yet. Add one to serve content from Termux.' : 'No sites match the filter.'}
      </div>
    {:else}
      <div class="grid grid-cols-1 lg:grid-cols-2 gap-2.5 font-mono text-xs">
        {#each filteredSites as site (site.id)}
          <div class="p-3 bg-card-sub border border-border rounded-lg">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="flex items-center gap-2 flex-wrap">
                  <span class="font-bold text-foreground text-sm truncate">{site.name}</span>
                  {#if site.running}
                    <Badge variant="success">
                      <Check class="w-3 h-3 mr-0.5" />
                      Running
                    </Badge>
                  {:else}
                    <Badge variant="danger">Stopped</Badge>
                  {/if}
                  <Badge variant="default">{ENGINE_LABELS[site.engine] ?? site.engine}</Badge>
                  {#if !site.engineInstalled}
                    <Badge variant="warning">engine missing</Badge>
                  {/if}
                </div>
                <p class="text-muted text-[11px] mt-1 break-all">{site.path}</p>
                <div class="flex flex-wrap items-center gap-3 mt-1 text-[10px] text-muted">
                  <span class="text-accent">:{site.port}</span>
                  {#if site.running}
                    <span>PID {site.pid}</span>
                    <span>up {formatUptime(site.uptimeMs)}</span>
                  {/if}
                  {#if site.engineVersion}
                    <span class="truncate max-w-[12rem]">{site.engineVersion}</span>
                  {/if}
                </div>
              </div>

              <div class="flex items-center gap-1.5 shrink-0">
                {#if site.running}
                  <a href={site.url} target="_blank" rel="noopener noreferrer" title="Open site">
                    <Button variant="secondary" size="sm">
                      <ExternalLink class="w-3.5 h-3.5" />
                    </Button>
                  </a>
                {/if}
                <Button
                  variant="secondary"
                  size="sm"
                  title="View log"
                  disabled={busySite === site.id}
                  onclick={() => openLogs(site)}
                >
                  <Terminal class="w-3.5 h-3.5" />
                </Button>
                {#if site.running}
                  <Button
                    variant="restart"
                    size="sm"
                    title="Restart site"
                    disabled={busySite === site.id}
                    onclick={() => siteAction(site, 'restart')}
                  >
                    {#if busySite === site.id}
                      <RefreshCw class="w-3.5 h-3.5 animate-spin" />
                    {:else}
                      <RotateCw class="w-3.5 h-3.5" />
                    {/if}
                  </Button>
                  <Button
                    variant="stop"
                    size="sm"
                    title="Stop site"
                    disabled={busySite === site.id}
                    onclick={() => siteAction(site, 'stop')}
                  >
                    <Power class="w-3.5 h-3.5" />
                  </Button>
                {:else}
                  <Button
                    variant="start"
                    size="sm"
                    title="Start site"
                    disabled={busySite === site.id || !site.engineInstalled}
                    onclick={() => siteAction(site, 'start')}
                  >
                    {#if busySite === site.id}
                      <RefreshCw class="w-3.5 h-3.5 animate-spin" />
                    {:else}
                      <Play class="w-3.5 h-3.5" />
                    {/if}
                  </Button>
                {/if}
                <Button
                  variant="danger"
                  size="sm"
                  title="Delete site"
                  disabled={busySite === site.id}
                  onclick={() => deleteSite(site)}
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

<!-- Log modal -->
{#if logOpen}
  <div class="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/70" role="presentation">
    <div class="w-full max-w-2xl max-h-[80vh] overflow-y-auto bg-card border-2 border-border rounded-lg p-4 font-mono text-xs">
      <div class="flex items-center justify-between border-b border-border pb-2 mb-3">
        <div class="flex items-center gap-2 min-w-0">
          <FileText class="w-4 h-4 text-accent shrink-0" />
          <h3 class="text-sm font-bold uppercase tracking-wider text-foreground truncate">
            {logSite?.name ?? 'Site'} log
          </h3>
        </div>
        <div class="flex items-center gap-1.5">
          <Button variant="secondary" size="sm" disabled={logBusy} onclick={loadLogs}>
            <RefreshCw class="w-3.5 h-3.5 {logBusy ? 'animate-spin' : ''}" />
          </Button>
          <Button variant="ghost" size="sm" onclick={() => (logOpen = false)}>
            <X class="w-3.5 h-3.5" />
          </Button>
        </div>
      </div>

      {#if logBusy && !logText}
        <div class="p-6 text-center text-muted">
          <RefreshCw class="w-5 h-5 animate-spin mx-auto mb-2 text-accent" />
          Reading log...
        </div>
      {:else}
        <pre class="bg-black/80 border border-border rounded p-2.5 text-[11px] text-emerald-400 whitespace-pre-wrap max-h-[60vh] overflow-y-auto break-all">{logText || 'No output captured yet.'}</pre>
      {/if}
    </div>
  </div>
{/if}
