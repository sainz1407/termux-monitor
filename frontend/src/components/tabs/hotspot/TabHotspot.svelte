<script lang="ts">
  import { onMount } from 'svelte'
  import {
    Wifi,
    RefreshCw,
    Users,
    Layers,
  } from '@lucide/svelte'
  import { api } from '../../../api/client'
  import type { HotspotStatus, ConnectedClient } from '../../../types/hotspot'
  import Button from '../../ui/Button.svelte'
  import Badge from '../../ui/Badge.svelte'
  import SubtabTetheringControl from './subtabs/SubtabTetheringControl.svelte'
  import SubtabClients from './subtabs/SubtabClients.svelte'

  type ActiveSubtab = 'tethering' | 'clients'

  let activeTab = $state<ActiveSubtab>('tethering')
  let status = $state<HotspotStatus>({ enabled: false, ssid: 'AndroidAP' })
  let clients = $state<ConnectedClient[]>([])
  let isLoading = $state(false)

  onMount(async () => {
    await fetchAll()
  })

  async function fetchAll() {
    try {
      isLoading = true
      const [s, c] = await Promise.all([
        api.get<HotspotStatus>('/api/hotspot/status'),
        api.get<ConnectedClient[]>('/api/hotspot/clients'),
      ])
      status = s
      clients = Array.isArray(c) ? c : []
    } catch {
      // Handled by api client
    } finally {
      isLoading = false
    }
  }
</script>

<div class="space-y-4 sm:space-y-6">
  <!-- Top Bar Controls -->
  <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
    <div class="flex items-center gap-2.5">
      <div class="p-2 rounded bg-card border-2 border-border text-accent">
        <Wifi class="w-5 h-5" />
      </div>
      <div>
        <h2 class="text-base sm:text-lg font-mono font-bold uppercase tracking-wider text-foreground">
          Tethering Suite
        </h2>
        <span class="text-[11px] font-mono text-muted">
          Android SoftAP, USB/Ethernet links & connected client monitoring
        </span>
      </div>
    </div>

    <div class="flex items-center gap-2">
      <Badge variant={status.enabled || status.usb_enabled || status.ethernet_enabled ? 'success' : 'default'}>
        {status.enabled || status.usb_enabled || status.ethernet_enabled ? 'ONLINE' : 'IDLE'}
      </Badge>
      <Button
        variant="secondary"
        size="sm"
        disabled={isLoading}
        onclick={fetchAll}
      >
        <RefreshCw class="w-3.5 h-3.5 mr-1.5 {isLoading ? 'animate-spin' : ''}" />
        <span>Refresh</span>
      </Button>
    </div>
  </div>

  <!-- Segmented Sub-tab Navigation (Neo-Brutalist) -->
  <div class="flex flex-wrap items-center gap-1.5 p-1 bg-card-sub border-2 border-border rounded-lg font-mono text-xs">
    <button
      type="button"
      class="flex items-center gap-1.5 px-3 py-2 rounded font-bold uppercase tracking-wider transition-all cursor-pointer {activeTab === 'tethering' ? 'bg-accent text-accent-text shadow-sm' : 'text-muted hover:text-foreground hover:bg-card'}"
      onclick={() => (activeTab = 'tethering')}
    >
      <Layers class="w-3.5 h-3.5" />
      <span>Tethering Channels</span>
    </button>

    <button
      type="button"
      class="flex items-center gap-1.5 px-3 py-2 rounded font-bold uppercase tracking-wider transition-all cursor-pointer {activeTab === 'clients' ? 'bg-accent text-accent-text shadow-sm' : 'text-muted hover:text-foreground hover:bg-card'}"
      onclick={() => (activeTab = 'clients')}
    >
      <Users class="w-3.5 h-3.5" />
      <span>Clients ({clients.length})</span>
    </button>
  </div>

  <!-- Active Sub-tab View -->
  <div>
    {#if activeTab === 'tethering'}
      <SubtabTetheringControl
        bind:status
        onRefresh={fetchAll}
      />
    {:else if activeTab === 'clients'}
      <SubtabClients
        {clients}
        {isLoading}
        onRefresh={fetchAll}
      />
    {/if}
  </div>
</div>
