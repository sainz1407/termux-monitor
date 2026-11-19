/**
 * Terminal macro snippets: presets, user snippets (localStorage), and the
 * destructive-command guard. Ported from olp's `lib/snippets.ts`.
 */

/** localStorage key (namespaced; wipes cleanly on version bump). */
export const SNIPPET_KEY = 'tmon.terminal.snippets.v1'

/** Max user snippets (keeps the row usable). */
export const MAX_CUSTOM = 20

/** Max command length (storage sanity; shells take far longer). */
export const MAX_LEN = 500

/** Built-in presets. Safe one-tap except restarts, which the guard catches. */
export const DEFAULT_SNIPPETS: readonly string[] = [
  'su -c id',
  'top -b -n 1 | head -n 20',
  'df -h',
  'free -m',
  'ps -A | head -n 20',
  'dumpsys battery | head -n 12',
]

/** Minimal storage surface (real `localStorage` satisfies it). */
export interface SnippetStorage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
  removeItem(key: string): void
}

function defaultStorage(): SnippetStorage | null {
  try {
    if (typeof localStorage !== 'undefined') return localStorage
  } catch {
    // blocked (private mode) or absent: fall through
  }
  return null
}

/** In-memory no-op (never throws, never persists). */
function nullStorage(): SnippetStorage {
  const mem = new Map<string, string>()
  return {
    getItem: (key) => mem.get(key) ?? null,
    setItem: (key, value) => {
      mem.set(key, value)
    },
    removeItem: (key) => {
      mem.delete(key)
    },
  }
}

/** Clamp a raw list (non-empty strings, length cap, deduped, max count). */
export function cleanSnippets(raw: unknown): string[] {
  if (!Array.isArray(raw)) return []
  const out: string[] = []
  for (const entry of raw) {
    if (typeof entry !== 'string') continue
    const cmd = entry.trim()
    if (cmd === '' || cmd.length > MAX_LEN || out.includes(cmd)) continue
    out.push(cmd)
    if (out.length >= MAX_CUSTOM) break
  }
  return out
}

/** Load user snippets (corrupt/absent/blocked -> `[]`, never throws). */
export function loadSnippets(storage?: SnippetStorage): string[] {
  const store = storage ?? defaultStorage() ?? nullStorage()
  try {
    return cleanSnippets(JSON.parse(store.getItem(SNIPPET_KEY) ?? 'null'))
  } catch {
    return []
  }
}

/** Save user snippets (validated + capped the same way). */
export function saveSnippets(custom: string[], storage?: SnippetStorage): void {
  const store = storage ?? defaultStorage() ?? nullStorage()
  try {
    store.setItem(SNIPPET_KEY, JSON.stringify(cleanSnippets(custom)))
  } catch {
    // quota/blocked: snippets stay session-local (never throws)
  }
}

/**
 * Destructive-command patterns (accident-prevention UX guard, NOT a
 * security boundary -- the raw exec form stays unguarded by design).
 */
const DESTRUCTIVE: RegExp[] = [
  /\bkill(all)?\b/i,
  /\brestart\b/i,
  /\brm\s+-[^\s]*r/i,
  /\b(reboot|halt|poweroff|shutdown)\b/i,
  /\bmkfs\b/i,
  /(^|[\s;&|])dd\b/i,
  /\bpm2\s+(restart|kill|delete|killall)\b/i,
]

/** True when a tap should ask first (best-effort pattern match). */
export function isDestructive(cmd: string): boolean {
  return DESTRUCTIVE.some((re) => re.test(cmd))
}
