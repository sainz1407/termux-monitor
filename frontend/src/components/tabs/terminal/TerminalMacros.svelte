<script lang="ts">
  import { Play, Plus, Trash2, X } from '@lucide/svelte'
  import Button from '../../ui/Button.svelte'
  import {
    DEFAULT_SNIPPETS,
    MAX_CUSTOM,
    MAX_LEN,
    cleanSnippets,
    isDestructive,
    loadSnippets,
    saveSnippets,
  } from './snippets'

  interface Props {
    /** Sends a command line to the shell (newline appended by the parent). */
    onRun: (cmd: string) => void
  }

  let { onRun }: Props = $props()

  let custom = $state<string[]>([])
  let adding = $state(false)
  let draft = $state('')
  let pending = $state<string | null>(null)

  $effect(() => {
    custom = loadSnippets()
  })

  function requestRun(cmd: string) {
    if (isDestructive(cmd)) {
      pending = cmd
      return
    }
    onRun(cmd)
  }

  function confirmRun() {
    if (pending) onRun(pending)
    pending = null
  }

  function cancelRun() {
    pending = null
  }

  function startAdd() {
    adding = true
    draft = ''
  }

  function cancelAdd() {
    adding = false
    draft = ''
  }

  function commitAdd() {
    const cmd = draft.trim()
    if (cmd === '') {
      cancelAdd()
      return
    }
    if (cmd.length > MAX_LEN) return
    const next = cleanSnippets([...custom, cmd])
    custom = next
    saveSnippets(next)
    cancelAdd()
  }

  function removeAt(index: number) {
    const next = custom.filter((_, i) => i !== index)
    custom = next
    saveSnippets(next)
  }

  function handleDraftKey(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault()
      commitAdd()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      cancelAdd()
    }
  }

  const canAdd = $derived(custom.length < MAX_CUSTOM)
  const tooLong = $derived(draft.trim().length > MAX_LEN)
</script>

<div class="bg-card-sub border-b border-border px-2 py-1.5 space-y-1.5 select-none">
  <div class="flex items-center gap-1.5 overflow-x-auto custom-scrollbar">
    <span class="text-[10px] font-mono font-bold uppercase tracking-wider text-muted shrink-0 mr-0.5">Macros</span>

    {#each DEFAULT_SNIPPETS as cmd}
      <button
        type="button"
        class="px-2 py-1 text-[11px] font-mono rounded border border-border bg-card hover:bg-card-sub text-foreground whitespace-nowrap active:scale-95 transition-all shadow-sm"
        title={cmd}
        onclick={() => requestRun(cmd)}
      >
        {cmd}
      </button>
    {/each}

    {#each custom as cmd, i}
      <span class="inline-flex items-center shrink-0">
        <button
          type="button"
          class="px-2 py-1 text-[11px] font-mono rounded-l border border-border bg-card hover:bg-card-sub text-foreground whitespace-nowrap active:scale-95 transition-all shadow-sm"
          title={cmd}
          onclick={() => requestRun(cmd)}
        >
          {cmd}
        </button>
        <button
          type="button"
          class="px-1 py-[5px] text-[11px] rounded-r border border-l-0 border-border bg-card hover:bg-red-950/60 hover:text-red-300 text-muted transition-colors"
          title="Remove macro"
          aria-label="Remove macro {cmd}"
          onclick={() => removeAt(i)}
        >
          <Trash2 class="w-3 h-3" />
        </button>
      </span>
    {/each}

    {#if adding}
      <div class="flex items-center gap-1 shrink-0">
        <input
          bind:value={draft}
          onkeydown={handleDraftKey}
          maxlength={MAX_LEN + 1}
          placeholder="command..."
          class="w-44 bg-card border {tooLong ? 'border-red-600' : 'border-border'} rounded px-2 py-1 text-[11px] font-mono text-foreground placeholder:text-muted focus:outline-none focus:border-accent"
        />
        <button
          type="button"
          class="p-1 rounded border border-border bg-card hover:bg-card-sub text-emerald-400 disabled:opacity-40 disabled:cursor-not-allowed"
          title="Save macro (Enter)"
          disabled={tooLong || draft.trim() === ''}
          onclick={commitAdd}
        >
          <Plus class="w-3 h-3" />
        </button>
        <button
          type="button"
          class="p-1 rounded border border-border bg-card hover:bg-card-sub text-muted hover:text-foreground"
          title="Cancel (Esc)"
          onclick={cancelAdd}
        >
          <X class="w-3 h-3" />
        </button>
      </div>
    {:else if canAdd}
      <button
        type="button"
        class="px-2 py-1 text-[11px] font-mono rounded border border-dashed border-border bg-transparent hover:bg-card text-muted hover:text-foreground whitespace-nowrap active:scale-95 transition-all shrink-0 inline-flex items-center gap-1"
        title="Add a custom macro (max {MAX_CUSTOM})"
        onclick={startAdd}
      >
        <Plus class="w-3 h-3" />
        Add
      </button>
    {/if}
  </div>

  {#if pending !== null}
    <div class="flex flex-wrap items-center gap-2 rounded border border-amber-800 bg-amber-950/40 px-2 py-1.5">
      <Play class="w-3.5 h-3.5 text-amber-400 shrink-0" />
      <span class="text-[11px] font-mono text-amber-200">
        This macro looks destructive: <code class="font-bold text-amber-300">{pending}</code>
      </span>
      <div class="flex items-center gap-1.5 ml-auto">
        <Button variant="ghost" size="sm" onclick={cancelRun}>Cancel</Button>
        <Button variant="danger" size="sm" onclick={confirmRun}>Run anyway</Button>
      </div>
    </div>
  {/if}
</div>
