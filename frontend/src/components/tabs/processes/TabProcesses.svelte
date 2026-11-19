<script lang="ts">
  import { onMount } from 'svelte'
  import {
    Workflow,
    RefreshCw,
    Search,
    X,
    Power,
    Zap,
    Cpu,
    HardDrive,
    Clock,
    AlertTriangle,
  } from '@lucide/svelte'
  import { api } from '../../../api/client'
  import { toastStore } from '../../../stores/toast.svelte'
  import type { ProcessInfo, ProcessesResponse } from '../../../types/processes'
  import Card from '../../ui/Card.svelte'
  import Button from '../../ui/Button.svelte'
  import Badge from '../../ui/Badge.svelte'
  import Input from '../../ui/Input.svelte'
  import Modal from '../../ui/Modal.svelte'

  let processes = $state<ProcessInfo[]>([])
  let isLoading = $state(false)
  let query = $state('')
  let sortBy = $state('cpu')
  let autoRefresh = $state(true)
  let lastUpdated = $state('')
  let killTarget = $state<ProcessInfo | null>(null)
  let killOpen = $state(false)
  let isKilling = $state(false)

  const SORTS = [
    { id: 'cpu', label: 'CPU' },
    { id: 'mem', label: 'Memory' },
    { id: 'elapsed', label: 'Uptime' },
    { id: 'pid', label: 'PID' },
    { id: 'name', label: 'Name' },
  ]

  const totalCpu = $derived(processes.reduce((sum, proc) => sum + proc.cpu, 0))
  const totalMem = $derived(processes.reduce((sum, proc) => sum + proc.mem_pct, 0))

  // The runtime label decides the colour of the badge, so the interpreters are
  // easy to spot in a long list.
  const RUNTIME_VARIANTS: Record<string, 'default' | 'success' | 'warning' | 'danger' | 'info' | 'pop-pink' | 'pop-lime' | 'pop-yellow' | 'pop-cyan' | 'pop-purple'> = {
    Python: 'pop-yellow',
    'Node.js': 'pop-lime',
    Java: 'pop-pink',
    PHP: 'pop-purple',
    Ruby: 'pop-pink',
    Shell: 'pop-cyan',
    Go: 'pop-purple',
    '.NET': 'pop-purple',
    Perl: 'pop-cyan',
    Lua: 'pop-cyan',
    Termux: 'pop-lime',
    Kernel: 'default',
    Native: 'default',
    Unknown: 'default',
  }

  function runtimeVariant(runtime: string) {
    return RUNTIME_VARIANTS[runtime] ?? 'default'
  }

  function stateVariant(state: string) {
    if (state === 'running') return 'success'
    if (state === 'zombie' || state === 'dead') return 'danger'
    if (state === 'stopped') return 'warning'
    return 'default'
  }

  onMount(async () => {
    await fetchProcesses()
  })

  $effect(() => {
    if (!autoRefresh) return
    const handle = setInterval(fetchProcesses, 3000)
    return () => clearInterval(handle)
  })

  async function fetchProcesses() {
    try {
      isLoading = true
      const params = new URLSearchParams()
      if (query.trim()) params.set('q', query.trim())
      params.set('sort', sortBy)
      const res = await api.get<ProcessesResponse>(`/api/processes?${params.toString()}`)
      if (res && Array.isArray(res.processes)) {
        processes = res.processes
        lastUpdated = new Date().toLocaleTimeString()
      }
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to load processes')
    } finally {
      isLoading = false
    }
  }

  let searchTimer: ReturnType<typeof setTimeout> | null = null

  function handleSearch() {
    if (searchTimer) clearTimeout(searchTimer)
    searchTimer = setTimeout(fetchProcesses, 350)
  }

  function changeSort(next: string) {
    sortBy = next
    fetchProcesses()
  }

  function clearSearch() {
    query = ''
    fetchProcesses()
  }

  function openKill(proc: ProcessInfo) {
    killTarget = proc
    killOpen = true
  }

  async function killProcess(force: boolean) {
    if (!killTarget) return
    try {
      isKilling = true
      await api.post('/api/processes/kill', { pid: killTarget.pid, force })
      toastStore.success(`Process ${killTarget.name} (${killTarget.pid}) stopped`)
      killTarget = null
      killOpen = false
      await fetchProcesses()
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to stop the process')
    } finally {
      isKilling = false
    }
  }
</script>

<div class="space-y-3 sm:space-y-6">
  <!-- Top Bar Controls -->
  <div class="flex items-center justify-between gap-3">
    <div class="flex items-center gap-2 min-w-0">
      <Workflow class="w-5 h-5 text-accent shrink-0" />
      <h2 class="text-base sm:text-lg font-mono font-bold uppercase tracking-wider text-foreground truncate">
        Process Monitor
      </h2>
      <Badge variant="default">{processes.length}</Badge>
    </div>

    <div class="flex items-center gap-2 shrink-0">
      <Button
        variant={autoRefresh ? 'cyan' : 'secondary'}
        size="sm"
        title="Toggle the automatic refresh"
        onclick={() => (autoRefresh = !autoRefresh)}
      >
        <Zap class="w-3.5 h-3.5 mr-1.5" />
        <span>Auto</span>
      </Button>
      <Button variant="secondary" size="sm" disabled={isLoading} onclick={fetchProcesses}>
        <RefreshCw class="w-3.5 h-3.5 mr-1.5 {isLoading ? 'animate-spin' : ''}" />
        <span>Refresh</span>
      </Button>
    </div>
  </div>

  <!-- Search & Sort -->
  <Card title="Filters" subtitle="Search by name, command line or runtime" tone="ice">
    <div class="space-y-3">
      <div class="flex items-center gap-2">
        <div class="relative flex-1">
          <Search class="w-4 h-4 absolute left-2.5 top-1/2 -translate-y-1/2 text-muted pointer-events-none" />
          <Input
            value={query}
            placeholder="Search processes..."
            oninput={handleSearch}
            class="pl-8"
          />
        </div>
        {#if query}
          <Button variant="ghost" size="sm" onclick={clearSearch} title="Clear the search">
            <X class="w-3.5 h-3.5" />
          </Button>
        {/if}
      </div>

      <div class="flex items-center gap-2 overflow-x-auto no-scrollbar">
        <span class="text-[10px] font-mono font-bold uppercase text-muted tracking-wider shrink-0">
          Sort
        </span>
        {#each SORTS as sort (sort.id)}
          <button
            type="button"
            class="px-2.5 py-1 rounded text-[10px] font-mono font-bold uppercase tracking-wider border transition-colors shrink-0 cursor-pointer
              {sortBy === sort.id
                ? 'bg-accent text-accent-text border-border'
                : 'bg-card-sub text-muted border-border hover:text-foreground'}"
            onclick={() => changeSort(sort.id)}
          >
            {sort.label}
          </button>
        {/each}
      </div>

      <div class="flex items-center gap-4 pt-2 border-t border-border text-[11px] font-mono text-muted">
        <span class="flex items-center gap-1.5">
          <Cpu class="w-3.5 h-3.5 text-accent" />
          CPU load <span class="text-foreground font-bold">{totalCpu.toFixed(1)}%</span>
        </span>
        <span class="flex items-center gap-1.5">
          <HardDrive class="w-3.5 h-3.5 text-accent" />
          Memory <span class="text-foreground font-bold">{totalMem.toFixed(1)}%</span>
        </span>
        {#if lastUpdated}
          <span class="ml-auto flex items-center gap-1.5">
            <Clock class="w-3.5 h-3.5" />
            {lastUpdated}
          </span>
        {/if}
      </div>
    </div>
  </Card>

  <!-- Process List -->
  <Card
    title="Running Processes"
    subtitle="Live list of every process on the device"
    tone="mint"
  >
    {#if isLoading && processes.length === 0}
      <div class="p-8 text-center text-muted font-mono text-xs">
        <RefreshCw class="w-6 h-6 animate-spin mx-auto mb-2 text-accent" />
        Reading the process table...
      </div>
    {:else if processes.length === 0}
      <div class="p-8 text-center text-muted font-mono text-xs">
        <Workflow class="w-6 h-6 mx-auto mb-2 opacity-50" />
        No process matches the current filter.
      </div>
    {:else}
      <!-- Mobile Card View -->
      <div class="space-y-2 sm:hidden">
        {#each processes as proc (proc.pid)}
          <div class="p-3 bg-card-sub border border-border rounded-lg space-y-2">
            <div class="flex items-start justify-between gap-2">
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-2 flex-wrap">
                  <span class="font-bold text-foreground text-sm truncate">{proc.name}</span>
                  <Badge variant={runtimeVariant(proc.runtime)} class="text-[9px]">
                    {proc.runtime}
                  </Badge>
                  <Badge variant={stateVariant(proc.state)} class="text-[9px]">
                    {proc.state}
                  </Badge>
                </div>
                <div class="font-mono text-[10px] text-muted mt-1 break-all">
                  {proc.cmd}
                </div>
              </div>
              <button
                type="button"
                class="shrink-0 p-1.5 rounded border border-border text-red-400 hover:bg-red-950/50 cursor-pointer"
                title="Stop process"
                onclick={() => openKill(proc)}
              >
                <Power class="w-3.5 h-3.5" />
              </button>
            </div>
            <div class="flex items-center gap-3 text-[10px] font-mono text-muted border-t border-border/60 pt-1.5">
              <span class="text-accent font-bold">PID {proc.pid}</span>
              <span>CPU {proc.cpu.toFixed(1)}%</span>
              <span>MEM {proc.mem_text}</span>
              <span>{proc.elapsed}</span>
            </div>
            <div class="text-[10px] font-mono text-muted truncate">
              {proc.user} &middot; {proc.exe_path || proc.name}
            </div>
          </div>
        {/each}
      </div>

      <!-- Desktop Table View -->
      <div class="hidden sm:block overflow-x-auto">
        <table class="w-full text-left font-mono text-xs">
          <thead>
            <tr class="border-b border-border text-muted uppercase text-[10px]">
              <th class="py-2.5 px-3">PID</th>
              <th class="py-2.5 px-3">Process</th>
              <th class="py-2.5 px-3">Runtime</th>
              <th class="py-2.5 px-3">User</th>
              <th class="py-2.5 px-3 text-right">CPU</th>
              <th class="py-2.5 px-3 text-right">Memory</th>
              <th class="py-2.5 px-3 text-right">Uptime</th>
              <th class="py-2.5 px-3 text-right">Thr</th>
              <th class="py-2.5 px-3"></th>
            </tr>
          </thead>
          <tbody class="divide-y divide-border">
            {#each processes as proc (proc.pid)}
              <tr class="hover:bg-card-sub transition-colors">
                <td class="py-2.5 px-3 text-accent font-bold">{proc.pid}</td>
                <td class="py-2.5 px-3 max-w-[16rem]">
                  <div class="font-bold text-foreground truncate">{proc.name}</div>
                  <div class="text-[10px] text-muted truncate">{proc.cmd}</div>
                </td>
                <td class="py-2.5 px-3">
                  <Badge variant={runtimeVariant(proc.runtime)} class="text-[9px]">
                    {proc.runtime}
                  </Badge>
                </td>
                <td class="py-2.5 px-3 text-muted">
                  <div class="truncate max-w-[9rem]">{proc.user}</div>
                  <div class="text-[10px] text-muted/70 truncate max-w-[9rem]">{proc.exe_path || '—'}</div>
                </td>
                <td class="py-2.5 px-3 text-right">
                  <span class={proc.cpu > 10 ? 'text-red-400 font-bold' : 'text-foreground'}>{proc.cpu.toFixed(1)}%</span>
                </td>
                <td class="py-2.5 px-3 text-right">
                  <div class="text-foreground">{proc.mem_text}</div>
                  <div class="text-[10px] text-muted">{proc.mem_pct.toFixed(1)}%</div>
                </td>
                <td class="py-2.5 px-3 text-right text-muted">{proc.elapsed}</td>
                <td class="py-2.5 px-3 text-right text-muted">{proc.threads}</td>
                <td class="py-2.5 px-3 text-right">
                  <button
                    type="button"
                    class="p-1.5 rounded border border-border text-red-400 hover:bg-red-950/50 cursor-pointer"
                    title="Stop process {proc.pid}"
                    onclick={() => openKill(proc)}
                  >
                    <Power class="w-3.5 h-3.5" />
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </Card>
</div>

<!-- Kill confirmation -->
<Modal
  bind:open={killOpen}
  title="Stop Process"
  size="sm"
  onclose={() => (killTarget = null)}
>
  {#if killTarget}
    <div class="space-y-3 font-mono text-xs">
      <div class="flex items-start gap-2 p-3 bg-red-950/30 border border-red-900 rounded-lg">
        <AlertTriangle class="w-4 h-4 text-red-400 shrink-0 mt-0.5" />
        <div class="min-w-0">
          <p class="text-foreground font-bold break-all">
            {killTarget.name} (PID {killTarget.pid})
          </p>
          <p class="text-muted mt-1 break-all">
            {killTarget.cmd}
          </p>
          <p class="text-muted mt-1">
            Owner: {killTarget.user} &middot; Runtime: {killTarget.runtime}
          </p>
        </div>
      </div>
      <p class="text-muted leading-relaxed">
        The process is stopped with a graceful termination signal. Use the force option when the process
        ignores the request.
      </p>
    </div>

    {#snippet footer()}
      <Button variant="secondary" size="sm" onclick={() => (killTarget = null)}>
        Cancel
      </Button>
      <Button variant="danger" size="sm" disabled={isKilling} onclick={() => killProcess(true)}>
        <Zap class="w-3.5 h-3.5 mr-1.5" />
        <span>Force Kill</span>
      </Button>
      <Button variant="primary" size="sm" disabled={isKilling} onclick={() => killProcess(false)}>
        <Power class="w-3.5 h-3.5 mr-1.5" />
        <span>Stop Process</span>
      </Button>
    {/snippet}
  {/if}
</Modal>
