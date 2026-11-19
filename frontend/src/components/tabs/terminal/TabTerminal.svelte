<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { Terminal as XTerminal } from '@xterm/xterm'
  import { FitAddon } from '@xterm/addon-fit'
  import '@xterm/xterm/css/xterm.css'
  import {
    Terminal,
    RefreshCw,
    Maximize2,
    Trash2,
    Keyboard,
    ClipboardPaste,
    Minus,
    Plus,
  } from '@lucide/svelte'
  import { WebSocketClient } from '../../../ws/socket'
  import Button from '../../ui/Button.svelte'
  import Badge from '../../ui/Badge.svelte'
  import Modal from '../../ui/Modal.svelte'
  import TerminalVirtualKeys from './TerminalVirtualKeys.svelte'
  import TerminalMacros from './TerminalMacros.svelte'

  type LinkStatus = 'connecting' | 'connected' | 'disconnected'
  type ShellMode = 'pty' | 'pipe'

  let terminalContainer: HTMLDivElement | null = $state(null)
  let term: XTerminal | null = null
  let fitAddon: FitAddon | null = null
  let wsClient: WebSocketClient | null = null
  let resizeObserver: ResizeObserver | null = null

  let status = $state<LinkStatus>('connecting')
  let mode = $state<ShellMode | null>(null)
  let shell = $state<string | null>(null)
  let showVirtualKeys = $state(false)
  let fontSize = $state(14)
  let execCmd = $state('')
  let hint = $state('')
  let stickyCtrl = $state(false)
  let stickyAlt = $state(false)
  let confirmNew = $state(false)

  const FONT_MIN = 10
  const FONT_MAX = 24

  // The Termux shell runs as the Termux user, every other candidate runs as
  // root.
  const sessionUser = $derived(shell && shell.startsWith('termux') ? 'termux' : 'root')

  // The session banner carries the shell and the transport mode, e.g.
  // "=== Terminal ready (termux bash, pty) - persistent session ... ===".
  const MODE_PATTERN = /\(([^)]*?),\s*(pty|pipe)\)/

  // Mobile detection for virtual keys ergonomics
  function isMobileClient(): boolean {
    if (typeof window === 'undefined' || typeof navigator === 'undefined') return false
    const uaMatches = /Android|webOS|iPhone|iPad|iPod|BlackBerry|IEMobile|Opera Mini|Mobile/i.test(navigator.userAgent)
    const touchMatches = navigator.maxTouchPoints > 0 && window.innerWidth < 1024
    return uaMatches || touchMatches
  }

  onMount(() => {
    showVirtualKeys = isMobileClient()

    initTerminal()
    connectWebSocket()

    const handleResize = () => {
      handleFitAndResize()
    }
    window.addEventListener('resize', handleResize)

    return () => {
      window.removeEventListener('resize', handleResize)
      cleanup()
    }
  })

  onDestroy(() => {
    cleanup()
  })

  /** Sends keystrokes to the shell using the JSON wire protocol. */
  function sendInput(data: string) {
    if (!wsClient || wsClient.readyState !== WebSocket.OPEN) return
    wsClient.send(JSON.stringify({ t: 'in', d: data }))
  }

  /** Reports the current canvas geometry to the pty. */
  function sendResize() {
    if (!term || !wsClient || wsClient.readyState !== WebSocket.OPEN) return
    if (term.cols <= 0 || term.rows <= 0) return
    wsClient.send(JSON.stringify({ t: 'rs', c: term.cols, r: term.rows }))
  }

  /**
   * Applies the sticky Ctrl/Alt modifiers, then clears them so the next
   * keystroke is unmodified.
   */
  function applySticky(data: string): string {
    let out = data
    if (stickyCtrl && /^[a-zA-Z]$/.test(out)) {
      out = String.fromCharCode(out.toUpperCase().charCodeAt(0) - 64)
    } else if (stickyAlt && out.length === 1) {
      out = '\x1b' + out
    }
    if (stickyCtrl || stickyAlt) {
      stickyCtrl = false
      stickyAlt = false
    }
    return out
  }

  function handleFitAndResize() {
    if (!fitAddon || !term) return
    try {
      fitAddon.fit()
      sendResize()
    } catch {
      // Suppress fit errors during transition or zero-dimension states
    }
  }

  function toggleVirtualKeys() {
    showVirtualKeys = !showVirtualKeys
    setTimeout(() => {
      handleFitAndResize()
      term?.focus()
    }, 60)
  }

  function handleVirtualKey(data: string) {
    sendInput(applySticky(data))
    term?.focus()
  }

  function toggleCtrl() {
    stickyCtrl = !stickyCtrl
    stickyAlt = false
  }

  function toggleAlt() {
    stickyAlt = !stickyAlt
    stickyCtrl = false
  }

  function cleanup() {
    if (resizeObserver) {
      resizeObserver.disconnect()
      resizeObserver = null
    }
    if (wsClient) {
      wsClient.disconnect()
      wsClient = null
    }
    if (term) {
      term.dispose()
      term = null
    }
    if (fitAddon) {
      fitAddon.dispose()
      fitAddon = null
    }
    status = 'disconnected'
  }

  function initTerminal() {
    if (!terminalContainer) return

    const xterm = new XTerminal({
      cursorBlink: true,
      fontSize,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
      theme: {
        background: '#090d16',
        foreground: '#f8fafc',
        cursor: '#38bdf8',
        selectionBackground: 'rgba(56, 189, 248, 0.3)',
        black: '#090d16',
        red: '#ef4444',
        green: '#10b981',
        yellow: '#f59e0b',
        blue: '#3b82f6',
        magenta: '#d946ef',
        cyan: '#06b6d4',
        white: '#f8fafc',
      },
    })
    term = xterm

    fitAddon = new FitAddon()
    xterm.loadAddon(fitAddon)
    xterm.open(terminalContainer)

    // Ctrl/Cmd+C copies the current selection instead of interrupting.
    xterm.attachCustomKeyEventHandler((e) => {
      if ((e.ctrlKey || e.metaKey) && e.key === 'c' && xterm.getSelection() !== '') {
        navigator.clipboard?.writeText(xterm.getSelection()).catch(() => {})
        return false
      }
      return true
    })

    // Attach ResizeObserver to keep cols/rows synchronized on any dimension shift
    if (typeof ResizeObserver !== 'undefined') {
      resizeObserver = new ResizeObserver(() => {
        handleFitAndResize()
      })
      resizeObserver.observe(terminalContainer)
    }

    setTimeout(() => {
      handleFitAndResize()
    }, 100)

    xterm.onData((data) => {
      sendInput(applySticky(data))
    })
  }

  /** Reads the shell and transport mode out of a session banner. */
  function detectMode(text: string) {
    const match = MODE_PATTERN.exec(text)
    if (match) {
      mode = match[2] as ShellMode
      shell = match[1]
    }
  }

  /** Writes server output to the canvas, accepting both raw and framed output. */
  function handleOutput(text: string) {
    detectMode(text)
    try {
      const parsed = JSON.parse(text) as { t?: unknown; d?: unknown }
      if (parsed && typeof parsed === 'object' && parsed.t === 'out' && typeof parsed.d === 'string') {
        term?.write(parsed.d)
        return
      }
    } catch {
      // not JSON: fall through to the raw write
    }
    term?.write(text)
  }

  function connectWebSocket() {
    if (wsClient && wsClient.readyState === WebSocket.OPEN) {
      return
    }

    status = 'connecting'

    wsClient = new WebSocketClient({
      path: '/api/terminal/ws',
      autoReconnect: true,
      binaryType: 'arraybuffer',
      onOpen: () => {
        status = 'connected'
        handleFitAndResize()
      },
      onMessage: (data: string | Blob | ArrayBuffer) => {
        if (typeof data === 'string') {
          handleOutput(data)
        } else if (data instanceof ArrayBuffer) {
          handleOutput(new TextDecoder().decode(data))
        } else if (data instanceof Blob) {
          data.text().then((text) => handleOutput(text))
        }
      },
      onError: () => {
        term?.write('\r\n\x1b[31m[-] WebSocket Error\x1b[0m\r\n')
      },
      onClose: () => {
        status = 'disconnected'
      },
    })

    wsClient.connect()
  }

  function reconnect() {
    cleanup()
    initTerminal()
    connectWebSocket()
  }

  function clearTerminal() {
    term?.clear()
  }

  function changeFont(delta: number) {
    const next = Math.min(FONT_MAX, Math.max(FONT_MIN, fontSize + delta))
    fontSize = next
    const xterm = term
    if (!xterm) return
    try {
      xterm.options.fontSize = next
      fitAddon?.fit()
      sendResize()
    } catch {
      // ignore fit errors on a disposed terminal
    }
  }

  /** Sends a raw command line to the shell. */
  function execCommand() {
    const cmd = execCmd.trim()
    if (cmd === '') return
    execCmd = ''
    sendInput(cmd + '\n')
  }

  async function paste() {
    try {
      const text = await navigator.clipboard.readText()
      if (text !== '') sendInput(text)
    } catch {
      hint = 'Clipboard is not accessible to the browser. Use Ctrl+Shift+V / right-click.'
    }
  }

  /** Requests a brand new shell; the server migrates every attached client. */
  function requestNewSession() {
    confirmNew = true
  }

  function confirmNewSession() {
    confirmNew = false
    if (!wsClient || wsClient.readyState !== WebSocket.OPEN) return
    wsClient.send(JSON.stringify({ t: 'new' }))
  }

  const statusLabel = $derived(
    status === 'connected' ? 'Connected' : status === 'connecting' ? 'Connecting...' : 'Disconnected'
  )
  const statusVariant = $derived(
    status === 'connected' ? 'success' : status === 'connecting' ? 'warning' : 'default'
  )
</script>

<div class="space-y-4">
  <!-- Header & Toolbar -->
  <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
    <div class="flex items-center gap-2 flex-wrap">
      <Terminal class="w-5 h-5 text-accent" />
      <h2 class="text-base sm:text-lg font-mono font-bold uppercase tracking-wider text-foreground">
        Interactive Terminal
      </h2>
      <Badge variant={statusVariant}>
        {statusLabel}
      </Badge>
      {#if mode === 'pty'}
        <Badge variant="success">pty</Badge>
      {:else if mode === 'pipe'}
        <span title="No pty available: no window size, job control unavailable">
          <Badge variant="warning">pipe (no job control)</Badge>
        </span>
      {/if}
    </div>

    <div class="flex items-center gap-2 flex-wrap">
      <!-- Font Size -->
      <Button variant="ghost" size="sm" onclick={() => changeFont(-1)} title="Decrease font size">
        <Minus class="w-3.5 h-3.5" />
      </Button>
      <span class="text-[10px] font-mono text-muted w-8 text-center">{fontSize}px</span>
      <Button variant="ghost" size="sm" onclick={() => changeFont(1)} title="Increase font size">
        <Plus class="w-3.5 h-3.5" />
      </Button>

      <!-- Virtual Keys Toggle -->
      <Button
        variant={showVirtualKeys ? 'secondary' : 'outline'}
        size="sm"
        onclick={toggleVirtualKeys}
        title="Toggle Virtual Keypad Bar"
      >
        <Keyboard class="w-3.5 h-3.5 mr-1" />
        <span>{showVirtualKeys ? 'Keys: On' : 'Keys: Off'}</span>
      </Button>

      <!-- Clear Terminal -->
      <Button variant="outline" size="sm" onclick={clearTerminal} title="Clear Terminal">
        <Trash2 class="w-3.5 h-3.5 mr-1" />
        <span>Clear</span>
      </Button>

      <!-- Paste -->
      <Button variant="outline" size="sm" onclick={() => void paste()} title="Paste from clipboard">
        <ClipboardPaste class="w-3.5 h-3.5 mr-1" />
        <span>Paste</span>
      </Button>

      <!-- Reconnect Session -->
      <Button variant="secondary" size="sm" onclick={reconnect} title="Reconnect Terminal">
        <RefreshCw class="w-3.5 h-3.5 mr-1 {status === 'connecting' ? 'animate-spin' : ''}" />
        <span>Reconnect</span>
      </Button>

      <!-- New Session -->
      <Button variant="danger" size="sm" onclick={requestNewSession} title="Start a new shell">
        <span>New session</span>
      </Button>
    </div>
  </div>

  <!-- Raw Command Form -->
  <form
    class="flex gap-2"
    onsubmit={(e) => {
      e.preventDefault()
      execCommand()
    }}
  >
    <input
      bind:value={execCmd}
      type="text"
      placeholder="Send raw command to the shell..."
      aria-label="Send raw command"
      class="flex-1 bg-card border border-border rounded px-3 py-2 text-sm font-mono text-foreground placeholder:text-muted focus:outline-none focus:border-accent"
    />
    <Button variant="outline" size="sm" type="submit">Exec</Button>
  </form>

  {#if hint !== ''}
    <p role="alert" class="text-xs font-mono text-amber-400">{hint}</p>
  {/if}

  <!-- Terminal Window (Neo-Brutalist Full-Height Frame) -->
  <div class="neo-card bg-card border-2 border-border shadow-neobrutal rounded-lg overflow-hidden h-[calc(100vh-300px)] min-h-[460px] flex flex-col">
    <!-- Terminal Header Bar -->
    <div class="bg-card-sub border-b border-border px-3 py-2 flex items-center justify-between font-mono text-xs text-muted shrink-0">
      <div class="flex items-center gap-2">
        <span class="w-3 h-3 rounded-full bg-red-500/80 inline-block"></span>
        <span class="w-3 h-3 rounded-full bg-amber-500/80 inline-block"></span>
        <span class="w-3 h-3 rounded-full bg-emerald-500/80 inline-block"></span>
        <span class="ml-2 font-bold text-foreground">{sessionUser}@android ({mode ?? 'pty'} / {shell ?? 'sh'})</span>
      </div>
      <div class="flex items-center gap-2 text-[10px]">
        <span>UTF-8</span>
        <span>|</span>
        <button
          type="button"
          class="hover:text-foreground cursor-pointer flex items-center gap-1"
          onclick={() => handleFitAndResize()}
          title="Force Fit Terminal Canvas"
        >
          <Maximize2 class="w-3 h-3" />
          <span>Fit</span>
        </button>
      </div>
    </div>

    <!-- Macro Row -->
    <TerminalMacros onRun={(cmd) => sendInput(cmd + '\n')} />

    <!-- Terminal Canvas Container -->
    <div
      bind:this={terminalContainer}
      class="flex-1 w-full p-2 bg-[#090d16] font-mono overflow-hidden"
      role="application"
      aria-label="Terminal output"
      oncontextmenu={(e) => {
        e.preventDefault()
        void paste()
      }}
    ></div>

    <!-- Virtual Touch Keypad (Mobile / Toggleable) -->
    {#if showVirtualKeys}
      <TerminalVirtualKeys
        ctrlActive={stickyCtrl}
        altActive={stickyAlt}
        onKey={handleVirtualKey}
        onToggleCtrl={toggleCtrl}
        onToggleAlt={toggleAlt}
      />
    {/if}
  </div>
</div>

<!-- New Session Confirmation -->
<Modal bind:open={confirmNew} title="New terminal session" size="sm">
  <p class="text-sm font-mono text-muted">
    Start a new terminal session? The current session will be killed, including every process
    running inside it.
  </p>
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (confirmNew = false)}>Cancel</Button>
    <Button variant="danger" size="sm" onclick={confirmNewSession}>New session</Button>
  {/snippet}
</Modal>
