// Types for the Store tab. They mirror the JSON emitted by
// internal/handlers/store_handler.go.

/** Result of GET /api/store/status. */
export interface StoreStatusResponse {
  available: boolean
  reason: string
  uid: number
  home: string
  prefix: string
  /** Curated mirror id detected in the active sources.list; "" when unrecognised. */
  mirrorId?: string
  /** Absolute path of the active sources.list. */
  mirrorList?: string
}

/** One installable package from the Termux-only catalog. */
export interface StoreItem {
  id: string
  name: string
  desc: string
  /** UI grouping: runtime | database | web | tools | network. */
  group: string
  /** Static config path for display only; empty when the item has none. */
  conf: string
  installed: boolean
  /** Version string, only populated when installed. */
  version: string
  /** True while an install run for this item is still executing. */
  installing: boolean
}

/** Result of GET /api/store/list. */
export interface StoreListResponse {
  available: boolean
  reason?: string
  items: StoreItem[]
}

/** Result of GET /api/store/install/log and GET /api/store/op/log. */
export interface StoreLogResponse {
  logs: string
  running: boolean
  /** null until the run finishes. */
  exitCode: number | null
}

/** One selectable Termux package repository. */
export interface StoreMirror {
  id: string
  name: string
  description: string
  host: string
  urls: string[]
}

/** Result of GET /api/store/mirror. */
export interface StoreMirrorResponse {
  available: boolean
  reason: string
  /** Absolute path of the active sources.list. */
  path: string
  /** False when the file is missing (a broken Termux install). */
  exists: boolean
  content: string
  /** Curated mirror id detected in the file; "" when unrecognised. */
  activeId: string
  /** Non-empty when the file could not be read. */
  readError: string
  /** Path of the latest pre-write backup, or "". */
  backup: string
  mirrors: StoreMirror[]
}

/** Result of POST /api/store/mirror. */
export interface StoreMirrorWriteResponse {
  ok: boolean
  backup: string
  content: string
  activeId: string
}
