<script lang="ts">
  import { onMount } from 'svelte'
  import {
    Database,
    Folder,
    FileCode2,
    RefreshCw,
    Play,
    Table2,
    Columns3,
    AlertCircle,
    CheckCircle2,
    ChevronRight,
    Loader2,
    ShieldAlert,
  } from '@lucide/svelte'
  import { api, ApiError } from '../../../api/client'
  import { toastStore } from '../../../stores/toast.svelte'
  import type {
    DatabaseCrumb,
    DatabaseEntry,
    DatabaseListing,
    DatabaseQueryResult,
    DatabaseSchema,
    DatabaseStatus,
    DatabaseTable,
    DatabaseTablesResponse,
  } from '../../../types/database'
  import Card from '../../ui/Card.svelte'
  import Button from '../../ui/Button.svelte'
  import Badge from '../../ui/Badge.svelte'

  // Environment status.
  let status = $state<DatabaseStatus | null>(null)
  let statusLoading = $state(true)

  // File picker.
  let pickupOpen = $state(false)
  let browserPath = $state('')
  let listing = $state<DatabaseListing | null>(null)
  let listingLoading = $state(false)

  // Selected database.
  let dbPath = $state('')
  let tables = $state<DatabaseTable[]>([])
  let tablesLoading = $state(false)
  let activeTable = $state<string | null>(null)
  let schema = $state<DatabaseSchema | null>(null)
  let schemaLoading = $state(false)

  // SQL editor.
  let sqlText = $state('SELECT * FROM sqlite_master;')
  let rowLimit = $state(200)
  let running = $state(false)
  let result = $state<DatabaseQueryResult | null>(null)
  let lastError = $state('')
  // Statement and path waiting for a write confirmation.
  let pendingWrite = $state<{ sql: string; message: string } | null>(null)

  const hasSQLite = $derived(status?.sqliteInstalled ?? false)
  const canBrowse = $derived((status?.available ?? false) && hasSQLite)

  onMount(() => {
    void refreshStatus()
  })

  async function refreshStatus() {
    statusLoading = true
    try {
      const res = await api.get<DatabaseStatus>('/api/database/status')
      status = res
      if (res.available && res.sqliteInstalled && !browserPath) {
        browserPath = res.home
      }
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to load database status')
    } finally {
      statusLoading = false
    }
  }

  function formatSize(bytes: number): string {
    if (bytes <= 0) return '—'
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1048576).toFixed(1)} MB`
    return `${(bytes / 1073741824).toFixed(2)} GB`
  }

  function formatTime(ms: number): string {
    if (!ms) return '—'
    const d = new Date(ms)
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  }

  async function openPicker() {
    pickupOpen = true
    if (!listing && browserPath) await browse(browserPath)
  }

  async function browse(path: string) {
    listingLoading = true
    lastError = ''
    try {
      const res = await api.get<DatabaseListing>(`/api/database/ls?path=${encodeURIComponent(path)}`)
      listing = res
      browserPath = res.path
    } catch (err: unknown) {
      lastError = err instanceof Error ? err.message : 'Failed to list directory'
    } finally {
      listingLoading = false
    }
  }

  function goCrumb(crumb: DatabaseCrumb) {
    void browse(crumb.path)
  }

  function pickEntry(entry: DatabaseEntry) {
    if (entry.isDir) {
      void browse(entry.path)
      return
    }
    pickupOpen = false
    void selectDatabase(entry.path)
  }

  async function selectDatabase(path: string) {
    dbPath = path
    tables = []
    schema = null
    activeTable = null
    result = null
    lastError = ''
    await refreshTables()
  }

  async function refreshTables() {
    if (!dbPath) return
    tablesLoading = true
    try {
      const res = await api.get<DatabaseTablesResponse>(
        `/api/database/tables?path=${encodeURIComponent(dbPath)}`
      )
      tables = Array.isArray(res.tables) ? res.tables : []
    } catch (err: unknown) {
      lastError = err instanceof Error ? err.message : 'Failed to read tables'
      tables = []
    } finally {
      tablesLoading = false
    }
  }

  async function openTable(name: string) {
    activeTable = name
    schema = null
    schemaLoading = true
    try {
      schema = await api.get<DatabaseSchema>(
        `/api/database/schema?path=${encodeURIComponent(dbPath)}&table=${encodeURIComponent(name)}`
      )
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to read schema')
    } finally {
      schemaLoading = false
    }
  }

  function selectTableQuery() {
    if (!activeTable) return
    sqlText = `SELECT * FROM ${quoteIdent(activeTable)} LIMIT ${rowLimit};`
  }

  function quoteIdent(name: string): string {
    return `"${name.replace(/"/g, '""')}"`
  }

  async function executeSql(confirm = false) {
    if (running) return
    if (!dbPath) {
      toastStore.error('Select a database file first')
      return
    }
    if (!sqlText.trim()) {
      toastStore.error('SQL statement is required')
      return
    }
    running = true
    lastError = ''
    try {
      const res = await api.post<DatabaseQueryResult>('/api/database/query', {
        path: dbPath,
        sql: sqlText,
        limit: rowLimit,
        confirm,
      })
      result = res
      pendingWrite = null
      if (res.changes > 0) {
        toastStore.success(`${res.changes} row(s) changed`)
        await refreshTables()
        if (activeTable) await openTable(activeTable)
      }
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 409) {
        pendingWrite = {
          sql: sqlText,
          message: 'This statement can modify the database. A backup is not created automatically.',
        }
        return
      }
      lastError = err instanceof Error ? err.message : 'Query failed'
      result = null
    } finally {
      running = false
    }
  }

  function confirmWrite() {
    void executeSql(true)
  }

  function cellText(v: string | null): string {
    return v === null ? 'NULL' : v
  }
</script>

<div class="space-y-4">
  <!-- Header -->
  <Card title="SQLite Database" subtitle="Inspect and query databases inside Termux" tone="lavender">
    <div class="flex flex-wrap items-center gap-2">
      <Badge variant={hasSQLite ? 'success' : 'warning'}>
        {hasSQLite ? 'sqlite3 ready' : 'sqlite3 missing'}
      </Badge>
      {#if status?.version}
        <Badge variant="info">{status.version.split(' ')[0]}</Badge>
      {/if}
      <Badge variant="default">{status?.home ?? '—'}</Badge>
      <div class="ml-auto">
        <Button variant="outline" size="sm" onclick={refreshStatus} disabled={statusLoading}>
          {#if statusLoading}
            <Loader2 class="w-3.5 h-3.5 mr-1.5 animate-spin" />
          {:else}
            <RefreshCw class="w-3.5 h-3.5 mr-1.5" />
          {/if}
          <span>Refresh</span>
        </Button>
      </div>
    </div>

    {#if status && !status.available}
      <p class="mt-3 text-xs font-mono text-danger flex items-center gap-1.5">
        <AlertCircle class="w-3.5 h-3.5" />
        <span>{status.reason || 'Termux environment unavailable'}</span>
      </p>
    {:else if status && !status.sqliteInstalled}
      <p class="mt-3 text-xs font-mono text-warning flex items-center gap-1.5">
        <AlertCircle class="w-3.5 h-3.5" />
        <span>Install it from the Store tab or run <code>pkg install sqlite</code> inside Termux.</span>
      </p>
    {/if}
  </Card>

  <!-- Database selection -->
  <Card title="Database File" subtitle={dbPath || 'No file selected'} tone="ice">
    <div class="flex flex-wrap items-center gap-2">
      <Button variant="primary" size="sm" onclick={openPicker} disabled={!canBrowse}>
        <Database class="w-3.5 h-3.5 mr-1.5" />
        <span>Choose File</span>
      </Button>
      <Button variant="outline" size="sm" onclick={refreshTables} disabled={!dbPath || tablesLoading}>
        <RefreshCw class="w-3.5 h-3.5 mr-1.5 {tablesLoading ? 'animate-spin' : ''}" />
        <span>Reload Tables</span>
      </Button>
    </div>
    {#if lastError}
      <p class="mt-3 text-[11px] font-mono text-danger">{lastError}</p>
    {/if}
  </Card>

  <div class="grid grid-cols-1 lg:grid-cols-3 gap-4">
    <!-- Tables -->
    <Card title="Tables" subtitle="{tables.length} object(s)" tone="peach">
      {#if tablesLoading}
        <div class="flex items-center gap-2 text-xs font-mono text-muted py-4">
          <Loader2 class="w-3.5 h-3.5 animate-spin" />
          <span>Reading schema…</span>
        </div>
      {:else if !dbPath}
        <p class="text-xs font-mono text-muted py-4">Select a database to list its tables.</p>
      {:else if tables.length === 0}
        <p class="text-xs font-mono text-muted py-4">No tables found.</p>
      {:else}
        <ul class="space-y-1 max-h-[26rem] overflow-auto">
          {#each tables as t (t.name)}
            <li>
              <button
                type="button"
                onclick={() => openTable(t.name)}
                class="w-full flex items-center gap-2 px-2 py-1.5 rounded border text-left transition-colors {activeTable === t.name
                  ? 'bg-accent text-accent-text border-border'
                  : 'bg-card-sub border-border text-foreground hover:border-accent'}"
              >
                {#if t.type === 'view'}
                  <FileCode2 class="w-3.5 h-3.5 shrink-0" />
                {:else}
                  <Table2 class="w-3.5 h-3.5 shrink-0" />
                {/if}
                <span class="flex-1 truncate font-mono text-xs">{t.name}</span>
                <span class="text-[10px] font-mono opacity-70">{t.rows >= 0 ? t.rows : '?'}</span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </Card>

    <!-- Schema -->
    <Card title="Schema" subtitle={activeTable ?? 'No table selected'} tone="mint" class="lg:col-span-2">
      {#if schemaLoading}
        <div class="flex items-center gap-2 text-xs font-mono text-muted py-4">
          <Loader2 class="w-3.5 h-3.5 animate-spin" />
          <span>Loading…</span>
        </div>
      {:else if !schema}
        <p class="text-xs font-mono text-muted py-4">Pick a table to inspect its columns.</p>
      {:else}
        <div class="flex flex-wrap items-center gap-2 mb-3">
          <Badge variant="default">{schema.columns.length} columns</Badge>
          <Badge variant="info">{schema.indexes.length} indexes</Badge>
          <Button variant="secondary" size="sm" onclick={selectTableQuery}>
            <Play class="w-3.5 h-3.5 mr-1.5" />
            <span>Query This Table</span>
          </Button>
        </div>

        <div class="overflow-auto max-h-64 border border-border rounded">
          <table class="w-full text-[11px] font-mono">
            <thead class="bg-card-sub sticky top-0">
              <tr>
                <th class="text-left px-2 py-1 font-bold">Column</th>
                <th class="text-left px-2 py-1 font-bold">Type</th>
                <th class="text-left px-2 py-1 font-bold">Flags</th>
                <th class="text-left px-2 py-1 font-bold">Default</th>
              </tr>
            </thead>
            <tbody>
              {#each schema.columns as c (c.cid)}
                <tr class="border-t border-border">
                  <td class="px-2 py-1 flex items-center gap-1.5">
                    <Columns3 class="w-3 h-3 text-muted" />
                    <span>{c.name}</span>
                  </td>
                  <td class="px-2 py-1 text-muted">{c.type || 'ANY'}</td>
                  <td class="px-2 py-1">
                    {#if c.pk}<Badge variant="warning">PK</Badge>{/if}
                    {#if c.notNull}<span class="ml-1 text-[10px] text-muted">NOT NULL</span>{/if}
                  </td>
                  <td class="px-2 py-1 text-muted">{c.default || '—'}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>

        {#if schema.sql}
          <pre class="mt-3 p-2 bg-card-sub border border-border rounded text-[10px] font-mono overflow-auto max-h-40 whitespace-pre-wrap">{schema.sql}</pre>
        {/if}
      {/if}
    </Card>
  </div>

  <!-- SQL editor -->
  <Card title="SQL Console" subtitle="Statements run as the Termux user" tone="butter">
    <textarea
      bind:value={sqlText}
      spellcheck="false"
      rows="6"
      class="w-full px-2.5 py-2 bg-card-sub border border-border rounded text-foreground text-xs font-mono focus:outline-none focus:border-accent"
    ></textarea>

    <div class="flex flex-wrap items-center gap-2 mt-2">
      <label class="flex items-center gap-1.5 text-[10px] font-mono text-muted">
        <span>Limit</span>
        <input
          type="number"
          min="1"
          max="1000"
          bind:value={rowLimit}
          class="w-20 px-2 py-1 bg-card-sub border border-border rounded text-foreground text-xs font-mono focus:outline-none focus:border-accent"
        />
      </label>
      <Button variant="primary" size="sm" onclick={() => executeSql(false)} disabled={running || !dbPath}>
        {#if running}
          <Loader2 class="w-3.5 h-3.5 mr-1.5 animate-spin" />
        {:else}
          <Play class="w-3.5 h-3.5 mr-1.5" />
        {/if}
        <span>Run</span>
      </Button>
      <Button variant="ghost" size="sm" onclick={() => (sqlText = '')} disabled={running}>
        Clear
      </Button>
      {#if result}
        <span class="ml-auto text-[10px] font-mono text-muted flex items-center gap-1.5">
          <CheckCircle2 class="w-3.5 h-3.5 text-success" />
          {result.rowCount} row(s) · {result.durationMs} ms
          {#if result.changes > 0}· {result.changes} changed{/if}
        </span>
      {/if}
    </div>

    {#if lastError}
      <p class="mt-3 text-[11px] font-mono text-danger whitespace-pre-wrap">{lastError}</p>
    {/if}

    {#if result && result.columns.length > 0}
      <div class="mt-3 overflow-auto max-h-96 border border-border rounded">
        <table class="w-full text-[11px] font-mono">
          <thead class="bg-card-sub sticky top-0">
            <tr>
              {#each result.columns as col, i (i)}
                <th class="text-left px-2 py-1 font-bold whitespace-nowrap">{col}</th>
              {/each}
            </tr>
          </thead>
          <tbody>
            {#each result.rows as row, ri (ri)}
              <tr class="border-t border-border">
                {#each row as cell, ci (ci)}
                  <td class="px-2 py-1 whitespace-nowrap {cell === null ? 'text-muted italic' : ''}">
                    {cellText(cell)}
                  </td>
                {/each}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      {#if result.truncated}
        <p class="mt-2 text-[10px] font-mono text-warning">
          Result truncated at {result.rowCount} rows. Raise the limit or add a WHERE clause.
        </p>
      {/if}
    {:else if result}
      <p class="mt-3 text-[11px] font-mono text-muted">Statement executed. {result.changes} row(s) changed.</p>
    {/if}
  </Card>
</div>

<!-- File picker modal -->
{#if pickupOpen}
  <div class="fixed inset-0 z-50 flex items-center justify-center p-3 bg-black/60">
    <Card title="Choose Database" subtitle={browserPath} tone="ice" class="w-full max-w-2xl max-h-[85vh] overflow-hidden">
      <div class="flex items-center gap-1 flex-wrap text-[10px] font-mono mb-2">
        {#each listing?.crumbs ?? [] as crumb (crumb.path)}
          <button
            type="button"
            onclick={() => goCrumb(crumb)}
            class="px-1.5 py-0.5 rounded border border-border bg-card-sub hover:border-accent"
          >
            {crumb.name}
          </button>
          <ChevronRight class="w-3 h-3 text-muted" />
        {/each}
      </div>

      <div class="overflow-auto max-h-[55vh] border border-border rounded">
        {#if listingLoading}
          <div class="flex items-center gap-2 text-xs font-mono text-muted p-4">
            <Loader2 class="w-3.5 h-3.5 animate-spin" />
            <span>Listing…</span>
          </div>
        {:else if lastError}
          <p class="p-4 text-[11px] font-mono text-danger">{lastError}</p>
        {:else if !listing || listing.entries.length === 0}
          <p class="p-4 text-xs font-mono text-muted">Empty directory.</p>
        {:else}
          <ul>
            {#if listing.parent}
              <li>
                <button
                  type="button"
                  onclick={() => browse(listing!.parent)}
                  class="w-full flex items-center gap-2 px-2 py-1.5 text-left hover:bg-card-sub border-b border-border"
                >
                  <Folder class="w-3.5 h-3.5 text-muted" />
                  <span class="font-mono text-xs">..</span>
                </button>
              </li>
            {/if}
            {#each listing.entries as entry (entry.path)}
              <li>
                <button
                  type="button"
                  onclick={() => pickEntry(entry)}
                  class="w-full flex items-center gap-2 px-2 py-1.5 text-left hover:bg-card-sub border-b border-border"
                >
                  {#if entry.isDir}
                    <Folder class="w-3.5 h-3.5 text-accent shrink-0" />
                  {:else}
                    <Database class="w-3.5 h-3.5 text-success shrink-0" />
                  {/if}
                  <span class="flex-1 truncate font-mono text-xs">{entry.name}</span>
                  {#if !entry.isDir}
                    <span class="text-[10px] font-mono text-muted">{formatSize(entry.size)}</span>
                    <span class="text-[10px] font-mono text-muted">{formatTime(entry.modTime)}</span>
                  {/if}
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </div>

      <div class="flex items-center gap-2 mt-3">
        <Button variant="ghost" size="sm" onclick={() => (pickupOpen = false, (lastError = ''))}>Cancel</Button>
      </div>
    </Card>
  </div>
{/if}

<!-- Write confirmation modal -->
{#if pendingWrite}
  <div class="fixed inset-0 z-50 flex items-center justify-center p-3 bg-black/60">
    <Card title="Confirm Write" tone="peach" class="w-full max-w-lg">
      <p class="text-xs font-mono text-danger flex items-center gap-1.5 mb-2">
        <ShieldAlert class="w-4 h-4" />
        <span>{pendingWrite.message}</span>
      </p>
      <pre class="p-2 bg-card-sub border border-border rounded text-[10px] font-mono overflow-auto max-h-40 whitespace-pre-wrap">{pendingWrite.sql}</pre>
      <div class="flex items-center gap-2 mt-3">
        <Button variant="danger" size="sm" onclick={confirmWrite} disabled={running}>
          {#if running}
            <Loader2 class="w-3.5 h-3.5 mr-1.5 animate-spin" />
          {:else}
            <Play class="w-3.5 h-3.5 mr-1.5" />
          {/if}
          <span>Execute</span>
        </Button>
        <Button variant="ghost" size="sm" onclick={() => (pendingWrite = null)}>Cancel</Button>
      </div>
    </Card>
  </div>
{/if}
