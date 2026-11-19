<script lang="ts">
  interface Props {
    onKey: (data: string) => void
    ctrlActive?: boolean
    altActive?: boolean
    onToggleCtrl?: () => void
    onToggleAlt?: () => void
  }

  let {
    onKey,
    ctrlActive = false,
    altActive = false,
    onToggleCtrl,
    onToggleAlt,
  }: Props = $props()

  // Prevent blur of xterm canvas on touch/click
  function handlePrevent(e: MouseEvent | TouchEvent) {
    e.preventDefault()
  }
</script>

<div
  class="bg-card-sub border-t-2 border-border px-2 py-1.5 flex items-center gap-1.5 overflow-x-auto custom-scrollbar select-none touch-manipulation"
  role="toolbar"
  aria-label="Terminal Virtual Keys"
>
  <!-- Modifiers -->
  <div class="flex items-center gap-1">
    <button
      type="button"
      onmousedown={handlePrevent}
      ontouchstart={handlePrevent}
      onclick={() => onKey('\x1b')}
      class="px-2.5 py-1 text-xs font-mono font-bold rounded border border-border bg-card hover:bg-card-sub text-foreground active:scale-95 transition-all shadow-sm"
      title="Escape key (\x1b)"
    >
      ESC
    </button>

    <button
      type="button"
      onmousedown={handlePrevent}
      ontouchstart={handlePrevent}
      onclick={() => onKey('\t')}
      class="px-2.5 py-1 text-xs font-mono font-bold rounded border border-border bg-card hover:bg-card-sub text-foreground active:scale-95 transition-all shadow-sm"
      title="Tab key (\t)"
    >
      TAB
    </button>

    <button
      type="button"
      onmousedown={handlePrevent}
      ontouchstart={handlePrevent}
      onclick={() => onToggleCtrl?.()}
      class="px-2.5 py-1 text-xs font-mono font-bold rounded border transition-all active:scale-95 shadow-sm {ctrlActive ? 'bg-accent text-accent-text border-accent shadow-neobrutal' : 'border-border bg-card hover:bg-card-sub text-foreground'}"
      title="Sticky Control modifier (Ctrl+key)"
      aria-pressed={ctrlActive}
    >
      CTRL
    </button>

    <button
      type="button"
      onmousedown={handlePrevent}
      ontouchstart={handlePrevent}
      onclick={() => onToggleAlt?.()}
      class="px-2.5 py-1 text-xs font-mono font-bold rounded border transition-all active:scale-95 shadow-sm {altActive ? 'bg-accent text-accent-text border-accent shadow-neobrutal' : 'border-border bg-card hover:bg-card-sub text-foreground'}"
      title="Sticky Alt modifier (Alt+key)"
      aria-pressed={altActive}
    >
      ALT
    </button>
  </div>

  <div class="h-4 w-px bg-border shrink-0 mx-0.5"></div>

  <!-- Direct Control Shortcuts -->
  <div class="flex items-center gap-1">
    <button
      type="button"
      onmousedown={handlePrevent}
      ontouchstart={handlePrevent}
      onclick={() => onKey('\x03')}
      class="px-2 py-1 text-xs font-mono font-bold rounded border border-red-900/60 bg-red-950/30 hover:bg-red-950/60 text-red-300 active:scale-95 transition-all shadow-sm"
      title="Send SIGINT / Interrupt (\x03)"
    >
      ^C
    </button>

    <button
      type="button"
      onmousedown={handlePrevent}
      ontouchstart={handlePrevent}
      onclick={() => onKey('\x04')}
      class="px-2 py-1 text-xs font-mono font-bold rounded border border-border bg-card hover:bg-card-sub text-foreground active:scale-95 transition-all shadow-sm"
      title="Send EOF / Exit shell (\x04)"
    >
      ^D
    </button>

    <button
      type="button"
      onmousedown={handlePrevent}
      ontouchstart={handlePrevent}
      onclick={() => onKey('\x1a')}
      class="px-2 py-1 text-xs font-mono font-bold rounded border border-border bg-card hover:bg-card-sub text-foreground active:scale-95 transition-all shadow-sm"
      title="Send SIGTSTP / Suspend background (\x1a)"
    >
      ^Z
    </button>

    <button
      type="button"
      onmousedown={handlePrevent}
      ontouchstart={handlePrevent}
      onclick={() => onKey('\x0c')}
      class="px-2 py-1 text-xs font-mono font-bold rounded border border-border bg-card hover:bg-card-sub text-foreground active:scale-95 transition-all shadow-sm"
      title="Clear screen (\x0c)"
    >
      ^L
    </button>
  </div>

  <div class="h-4 w-px bg-border shrink-0 mx-0.5"></div>

  <!-- Arrow Navigation Keys -->
  <div class="flex items-center gap-1">
    <button
      type="button"
      onmousedown={handlePrevent}
      ontouchstart={handlePrevent}
      onclick={() => onKey('\x1b[A')}
      class="px-2 py-1 text-xs font-mono font-bold rounded border border-border bg-card hover:bg-card-sub text-foreground active:scale-95 transition-all shadow-sm"
      title="Up arrow"
    >
      ▲
    </button>

    <button
      type="button"
      onmousedown={handlePrevent}
      ontouchstart={handlePrevent}
      onclick={() => onKey('\x1b[B')}
      class="px-2 py-1 text-xs font-mono font-bold rounded border border-border bg-card hover:bg-card-sub text-foreground active:scale-95 transition-all shadow-sm"
      title="Down arrow"
    >
      ▼
    </button>

    <button
      type="button"
      onmousedown={handlePrevent}
      ontouchstart={handlePrevent}
      onclick={() => onKey('\x1b[D')}
      class="px-2 py-1 text-xs font-mono font-bold rounded border border-border bg-card hover:bg-card-sub text-foreground active:scale-95 transition-all shadow-sm"
      title="Left arrow"
    >
      ◄
    </button>

    <button
      type="button"
      onmousedown={handlePrevent}
      ontouchstart={handlePrevent}
      onclick={() => onKey('\x1b[C')}
      class="px-2 py-1 text-xs font-mono font-bold rounded border border-border bg-card hover:bg-card-sub text-foreground active:scale-95 transition-all shadow-sm"
      title="Right arrow"
    >
      ►
    </button>
  </div>

  <div class="h-4 w-px bg-border shrink-0 mx-0.5"></div>

  <!-- Line Navigation Keys -->
  <div class="flex items-center gap-1">
    <button
      type="button"
      onmousedown={handlePrevent}
      ontouchstart={handlePrevent}
      onclick={() => onKey('\x1b[H')}
      class="px-2 py-1 text-xs font-mono font-bold rounded border border-border bg-card hover:bg-card-sub text-foreground active:scale-95 transition-all shadow-sm"
      title="Home key"
    >
      HOME
    </button>

    <button
      type="button"
      onmousedown={handlePrevent}
      ontouchstart={handlePrevent}
      onclick={() => onKey('\x1b[F')}
      class="px-2 py-1 text-xs font-mono font-bold rounded border border-border bg-card hover:bg-card-sub text-foreground active:scale-95 transition-all shadow-sm"
      title="End key"
    >
      END
    </button>

    <button
      type="button"
      onmousedown={handlePrevent}
      ontouchstart={handlePrevent}
      onclick={() => onKey('\x1b[5~')}
      class="px-2 py-1 text-xs font-mono font-bold rounded border border-border bg-card hover:bg-card-sub text-foreground active:scale-95 transition-all shadow-sm"
      title="Page Up"
    >
      PGUP
    </button>

    <button
      type="button"
      onmousedown={handlePrevent}
      ontouchstart={handlePrevent}
      onclick={() => onKey('\x1b[6~')}
      class="px-2 py-1 text-xs font-mono font-bold rounded border border-border bg-card hover:bg-card-sub text-foreground active:scale-95 transition-all shadow-sm"
      title="Page Down"
    >
      PGDN
    </button>
  </div>

  <div class="h-4 w-px bg-border shrink-0 mx-0.5"></div>

  <!-- Shell Symbols -->
  <div class="flex items-center gap-1">
    {#each ['|', '~', '/', '-', '_', '$', '&'] as sym}
      <button
        type="button"
        onmousedown={handlePrevent}
        ontouchstart={handlePrevent}
        onclick={() => onKey(sym)}
        class="px-2 py-1 text-xs font-mono font-bold rounded border border-border bg-card hover:bg-card-sub text-muted hover:text-foreground active:scale-95 transition-all shadow-sm"
        title="Symbol '{sym}'"
      >
        {sym}
      </button>
    {/each}
  </div>
</div>
