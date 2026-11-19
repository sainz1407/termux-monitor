<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import {
    Globe,
    RefreshCw,
    Play,
    Square,
    ExternalLink,
    Copy,
    Upload,
    Terminal,
  } from '@lucide/svelte'
  import { api } from '../../../api/client'
  import { toastStore } from '../../../stores/toast.svelte'
  import Card from '../../ui/Card.svelte'
  import Button from '../../ui/Button.svelte'
  import Badge from '../../ui/Badge.svelte'
  import Input from '../../ui/Input.svelte'

  type Engine = 'cloudflare' | 'tailscale' | 'zerotier' | 'ngrok' | 'pinggy'

  interface TunnelConfig {
    engine: Engine
    enabled?: boolean
    cloudflare_token?: string
    cloudflare_quick?: boolean
    tailscale_auth_key?: string
    zerotier_network_id?: string
    ngrok_auth_token?: string
    pinggy_token?: string
  }

  interface TunnelStatus {
    active?: boolean
    running?: boolean
    engine?: string
    public_url?: string
    ip_address?: string
    logs?: string[]
    binary_found?: boolean
    binary_path?: string
  }

  const ENGINES: { id: Engine; label: string; desc: string }[] = [
    { id: 'cloudflare', label: 'Cloudflare', desc: 'Quick tunnel (HTTPS)' },
    { id: 'tailscale', label: 'Tailscale', desc: 'Mesh VPN' },
    { id: 'zerotier', label: 'ZeroTier', desc: 'P2P Virtual LAN' },
    { id: 'ngrok', label: 'Ngrok', desc: 'Public HTTPS URL' },
    { id: 'pinggy', label: 'Pinggy', desc: 'SSH reverse tunnel' },
  ]

  let config = $state<TunnelConfig>({
    engine: 'cloudflare',
    enabled: false,
    cloudflare_quick: true,
  })
  let token = $state('')
  let status = $state<TunnelStatus>({ active: false, running: false })
  let isLoading = $state(false)
  let isToggling = $state(false)
  let pollTimer: ReturnType<typeof setInterval> | null = null

  onMount(async () => {
    await fetchTunnel(true)
    pollTimer = setInterval(() => fetchTunnel(false), 3000)
  })

  onDestroy(() => {
    if (pollTimer) clearInterval(pollTimer)
  })

  async function fetchTunnel(withConfig = false) {
    try {
      const res = await api.get<{
        config?: TunnelConfig
        status?: TunnelStatus
      }>('/api/tunnel/status')
      // The saved configuration is only applied once, so polling never
      // overwrites the engine the user is currently editing.
      if (withConfig && res?.config) {
        config = { ...config, ...res.config }
        // Restore the saved credential of the selected engine.
        if (!token) {
          token =
            config.engine === 'cloudflare'
              ? (config.cloudflare_token ?? '')
              : config.engine === 'tailscale'
                ? (config.tailscale_auth_key ?? '')
                : config.engine === 'zerotier'
                  ? (config.zerotier_network_id ?? '')
                  : config.engine === 'ngrok'
                    ? (config.ngrok_auth_token ?? '')
                    : (config.pinggy_token ?? '')
        }
      }
      if (res?.status) {
        status = { ...status, ...res.status }
      }
    } catch {
      // Ignored: the status endpoint is polled, transient failures are fine.
    } finally {
      isLoading = false
    }
  }

  async function refreshNow() {
    isLoading = true
    await fetchTunnel()
  }

  function buildPayload(): Record<string, unknown> {
    const payload: Record<string, unknown> = {
      engine: config.engine,
      enabled: true,
    }

    switch (config.engine) {
      case 'cloudflare':
        payload.cloudflare_token = token
        // An empty token means a quick tunnel, a filled one a named tunnel.
        payload.cloudflare_quick = token.trim() === ''
        break
      case 'tailscale':
        payload.tailscale_auth_key = token
        break
      case 'zerotier':
        payload.zerotier_network_id = token
        break
      case 'ngrok':
        payload.ngrok_auth_token = token
        break
      case 'pinggy':
        payload.pinggy_token = token
        break
    }

    return payload
  }

  async function startTunnel() {
    try {
      isToggling = true
      await api.post('/api/tunnel/start', buildPayload())
      await fetchTunnel()
      toastStore.success(`Tunnel (${config.engine}) started successfully.`)
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to start tunnel')
      await fetchTunnel()
    } finally {
      isToggling = false
    }
  }

  async function stopTunnel() {
    try {
      isToggling = true
      await api.post('/api/tunnel/stop', { enabled: false })
      await fetchTunnel()
      toastStore.success('Tunnel stopped.')
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Failed to stop tunnel')
    } finally {
      isToggling = false
    }
  }

  async function copyUrl() {
    if (!status.public_url) return
    try {
      await navigator.clipboard.writeText(status.public_url)
      toastStore.success('Public URL copied to clipboard.')
    } catch {
      toastStore.error('Failed to copy the URL.')
    }
  }

  // Binary Upload State
  let uploadEngine = $state<'cloudflare' | 'zerotier'>('cloudflare')
  let selectedBinaryFile = $state<File | null>(null)
  let isUploadingBinary = $state(false)
  let binaryFileInputEl: HTMLInputElement | null = $state(null)

  function handleFileSelect(e: Event) {
    const input = e.target as HTMLInputElement
    if (input.files && input.files[0]) {
      selectedBinaryFile = input.files[0]
    }
  }

  async function uploadBinary() {
    if (!selectedBinaryFile) {
      toastStore.warning('Please select a binary file first.')
      return
    }
    try {
      isUploadingBinary = true
      const formData = new FormData()
      formData.append('engine', uploadEngine)
      formData.append('binary', selectedBinaryFile)

      const res = await api.post<{ success: boolean; path?: string; error?: string }>(
        '/api/tunnel/upload',
        formData
      )

      if (res && res.success) {
        toastStore.success(`Binary for ${uploadEngine} installed successfully to ${res.path || 'system'}`)
        selectedBinaryFile = null
        if (binaryFileInputEl) binaryFileInputEl.value = ''
        await fetchTunnel()
      } else {
        toastStore.error(res?.error || 'Failed to upload binary')
      }
    } catch (err: unknown) {
      toastStore.error(err instanceof Error ? err.message : 'Binary upload failed')
    } finally {
      isUploadingBinary = false
    }
  }
</script>

<div class="space-y-3 sm:space-y-6">
  <!-- Top Bar Controls -->
  <div class="flex items-center justify-between">
    <div class="flex items-center gap-2">
      <Globe class="w-5 h-5 text-accent" />
      <h2 class="text-base sm:text-lg font-mono font-bold uppercase tracking-wider text-foreground">
        Remote Access Tunnels
      </h2>
      <Badge variant={status.active ? 'success' : 'default'}>
        {status.active ? `${(status.engine || config.engine).toUpperCase()} Active` : 'Inactive'}
      </Badge>
    </div>

    <div class="flex items-center gap-2">
      <Button
        variant="secondary"
        size="sm"
        disabled={isLoading}
        onclick={refreshNow}
      >
        <RefreshCw class="w-3.5 h-3.5 mr-1.5 {isLoading ? 'animate-spin' : ''}" />
        <span>Refresh</span>
      </Button>

      {#if status.active}
        <Button
          variant="danger"
          size="sm"
          disabled={isToggling}
          onclick={stopTunnel}
        >
          <Square class="w-3.5 h-3.5 mr-1" />
          <span>Stop Tunnel</span>
        </Button>
      {:else}
        <Button
          variant="primary"
          size="sm"
          disabled={isToggling}
          onclick={startTunnel}
        >
          <Play class="w-3.5 h-3.5 mr-1" />
          <span>Start Tunnel</span>
        </Button>
      {/if}
    </div>
  </div>

  <div class="grid grid-cols-1 lg:grid-cols-3 gap-3 sm:gap-6">
    <!-- Tunnel Configuration -->
    <Card
      title="Tunnel Configuration"
      subtitle="Expose WebUI remotely without port forwarding"
      tone="ice"
      class="lg:col-span-2"
    >
      <div class="space-y-5 font-mono text-xs">
        <!-- Engine Selector -->
        <div>
          <span class="text-[10px] text-muted uppercase font-bold block mb-2">
            Tunnel Provider Engine
          </span>
          <div class="grid grid-cols-2 sm:grid-cols-3 gap-3">
            {#each ENGINES as eng}
              <button
                type="button"
                class="neo-button p-3 rounded border border-border bg-card-sub hover:border-accent text-center cursor-pointer transition-all {config.engine === eng.id ? 'border-accent bg-accent/15 text-accent font-bold shadow-neobrutal-sm' : 'text-foreground'}"
                onclick={() => (config.engine = eng.id)}
                disabled={status.active}
              >
                <div class="font-black text-sm uppercase">{eng.label}</div>
                <div class="text-[10px] text-muted mt-0.5">{eng.desc}</div>
              </button>
            {/each}
          </div>
        </div>

        <!-- Token / Credential Input -->
        {#if config.engine === 'cloudflare'}
          <Input
            type="password"
            label="Cloudflare Tunnel Token (leave empty for a free quick tunnel)"
            placeholder="eyJhIjoi..."
            bind:value={token}
            disabled={status.active}
          />
        {:else if config.engine === 'tailscale'}
          <Input
            type="password"
            label="Tailscale Auth Key (optional)"
            placeholder="tskey-auth-..."
            bind:value={token}
            disabled={status.active}
          />
        {:else if config.engine === 'zerotier'}
          <Input
            label="ZeroTier Network ID (16 Hex Digits)"
            placeholder="8056c2e21c000001"
            bind:value={token}
            disabled={status.active}
          />
        {:else if config.engine === 'ngrok'}
          <Input
            type="password"
            label="Ngrok Auth Token (optional)"
            placeholder="2abc..."
            bind:value={token}
            disabled={status.active}
          />
        {:else if config.engine === 'pinggy'}
          <Input
            label="Pinggy Token (optional)"
            placeholder="free"
            bind:value={token}
            disabled={status.active}
          />
        {/if}

        <!-- Engine Binary Status -->
        <div class="p-3 bg-card-sub border border-border rounded flex flex-col sm:flex-row sm:items-center justify-between gap-2 text-[11px]">
          <span class="text-muted">Engine binary:</span>
          {#if status.binary_found}
            <span class="text-accent font-bold break-all">{status.binary_path}</span>
          {:else}
            <span class="text-red-400 font-bold">
              Not found — install it in Termux (pkg install cloudflared) or upload the binary below.
            </span>
          {/if}
        </div>
      </div>
    </Card>

    <!-- Public URL & Connectivity -->
    <Card title="Public Remote Endpoint" subtitle="Worldwide access URL" tone="ice">
      <div class="space-y-3 font-mono text-xs">
        {#if status.active && status.public_url}
          <div class="p-3 bg-card-sub border border-border rounded space-y-1">
            <span class="text-[10px] text-muted uppercase font-bold">Public Domain:</span>
            <a
              href={status.public_url}
              target="_blank"
              rel="noreferrer"
              class="font-bold text-accent break-all hover:underline flex items-center gap-1"
            >
              <span>{status.public_url}</span>
              <ExternalLink class="w-3.5 h-3.5 shrink-0" />
            </a>
            <button
              type="button"
              class="mt-1 flex items-center gap-1 text-[10px] text-muted hover:text-foreground cursor-pointer"
              onclick={copyUrl}
            >
              <Copy class="w-3 h-3" />
              <span>Copy URL</span>
            </button>
          </div>
        {:else if status.active}
          <div class="p-3 bg-card-sub border border-border rounded text-amber-400 font-bold">
            Tunnel is starting, waiting for the public URL...
          </div>
        {:else}
          <p class="text-muted text-center py-4">
            Tunnel is stopped. Configure your provider credentials and click Start.
          </p>
        {/if}

        {#if status.ip_address}
          <div class="p-3 bg-card-sub border border-border rounded">
            <span class="text-[10px] text-muted uppercase font-bold">Tunnel IP:</span>
            <span class="font-bold text-foreground">{status.ip_address}</span>
          </div>
        {/if}
      </div>
    </Card>
  </div>

  <!-- Engine Logs -->
  <Card title="Tunnel Engine Log" subtitle="Live output of the tunnel daemon" tone="ice">
    <div class="p-3 bg-card-sub border border-border rounded max-h-56 overflow-y-auto">
      {#if status.logs && status.logs.length > 0}
        <pre class="text-[10px] font-mono text-muted whitespace-pre-wrap break-all">{status.logs.join('\n')}</pre>
      {:else}
        <p class="text-[10px] text-muted font-mono flex items-center gap-1">
          <Terminal class="w-3 h-3" />
          <span>No tunnel log entries yet.</span>
        </p>
      {/if}
    </div>
  </Card>

  <!-- Engine Binary Management Card -->
  <Card
    title="Engine Binary Management"
    subtitle="Upload custom ARM64 or x86 executable binary for cloudflared / zerotier-one"
    tone="ice"
  >
    <div class="space-y-4 font-mono text-xs">
      <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
        <!-- Engine selection -->
        <div class="space-y-2">
          <span class="text-[10px] text-muted uppercase font-bold block">Target Tunnel Engine</span>
          <div class="grid grid-cols-2 gap-2">
            <button
              type="button"
              class="neo-button p-2.5 rounded border transition-all text-center cursor-pointer {uploadEngine === 'cloudflare' ? 'border-accent bg-accent/15 text-accent font-bold shadow-neobrutal-sm' : 'border-border bg-card-sub text-muted hover:text-foreground'}"
              onclick={() => (uploadEngine = 'cloudflare')}
            >
              <div class="font-bold text-xs">Cloudflare</div>
              <div class="text-[10px] text-muted">cloudflared</div>
            </button>

            <button
              type="button"
              class="neo-button p-2.5 rounded border transition-all text-center cursor-pointer {uploadEngine === 'zerotier' ? 'border-accent bg-accent/15 text-accent font-bold shadow-neobrutal-sm' : 'border-border bg-card-sub text-muted hover:text-foreground'}"
              onclick={() => (uploadEngine = 'zerotier')}
            >
              <div class="font-bold text-xs">ZeroTier</div>
              <div class="text-[10px] text-muted">zerotier-one</div>
            </button>
          </div>
        </div>

        <!-- File selection & Upload -->
        <div class="space-y-2">
          <span class="text-[10px] text-muted uppercase font-bold block">Select Executable Binary</span>
          <div class="flex flex-col sm:flex-row items-stretch sm:items-center gap-2">
            <label class="flex-1 neo-button bg-card-sub border border-dashed border-border hover:border-accent p-2.5 rounded cursor-pointer text-center truncate select-none">
              <span class="text-xs text-foreground truncate">
                {selectedBinaryFile ? selectedBinaryFile.name : 'Choose executable binary file...'}
              </span>
              <input
                type="file"
                bind:this={binaryFileInputEl}
                onchange={handleFileSelect}
                class="hidden"
              />
            </label>

            <Button
              variant="primary"
              size="md"
              disabled={isUploadingBinary || !selectedBinaryFile}
              onclick={uploadBinary}
            >
              <Upload class="w-3.5 h-3.5 mr-1 {isUploadingBinary ? 'animate-bounce' : ''}" />
              <span>{isUploadingBinary ? 'Uploading...' : 'Upload Binary'}</span>
            </Button>
          </div>
        </div>
      </div>

      <div class="p-3 bg-card-sub border border-border rounded flex flex-col sm:flex-row sm:items-center justify-between gap-2 text-muted text-[11px]">
        <span>Binaries are saved to the module bin directory with executable (<code class="text-accent font-bold">0755</code>) permissions.</span>
        <Badge variant="default">ARM64 / ARMv7</Badge>
      </div>
    </div>
  </Card>
</div>
