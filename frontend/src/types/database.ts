// Types for the Database tab. They mirror the JSON emitted by
// internal/handlers/database_handler.go and internal/database/service.go.

/** Result of GET /api/database/status. */
export interface DatabaseStatus {
  /** False when the Termux environment cannot run commands at all. */
  available: boolean
  /** Explains an unavailable environment. */
  reason: string
  /** False when `sqlite3` is missing from $PREFIX. */
  sqliteInstalled: boolean
  /** Termux HOME, shown in the breadcrumb. */
  home: string
  /** Directories the picker may browse. */
  roots: string[]
  /** First line of `sqlite3 --version`, empty when unknown. */
  version: string
}

/** One row of the file picker. */
export interface DatabaseEntry {
  name: string
  path: string
  isDir: boolean
  size: number
  /** Epoch milliseconds. */
  modTime: number
  /** True when the file starts with the SQLite magic header. */
  database: boolean
}

/** One breadcrumb segment. */
export interface DatabaseCrumb {
  name: string
  path: string
}

/** Result of GET /api/database/ls. */
export interface DatabaseListing {
  path: string
  /** Empty at a root. */
  parent: string
  crumbs: DatabaseCrumb[]
  entries: DatabaseEntry[]
}

/** A table or view; `rows` is -1 when the count is unknown. */
export interface DatabaseTable {
  name: string
  type: 'table' | 'view'
  rows: number
}

/** Result of GET /api/database/tables. */
export interface DatabaseTablesResponse {
  path: string
  tables: DatabaseTable[]
}

/** One column of a table. */
export interface DatabaseColumn {
  cid: number
  name: string
  type: string
  notNull: boolean
  default?: string
  pk: boolean
}

/** One index of a table. */
export interface DatabaseIndex {
  name: string
  unique: boolean
  columns: string[]
}

/** Result of GET /api/database/schema. */
export interface DatabaseSchema {
  table: string
  sql: string
  columns: DatabaseColumn[]
  indexes: DatabaseIndex[]
}

/** Result of POST /api/database/query. */
export interface DatabaseQueryResult {
  columns: string[]
  rows: (string | null)[][]
  rowCount: number
  truncated: boolean
  changes: number
  durationMs: number
}

/** Body of POST /api/database/query. */
export interface DatabaseQueryRequest {
  path: string
  sql: string
  limit?: number
  /** Required for statements that are not read-only. */
  confirm?: boolean
}
