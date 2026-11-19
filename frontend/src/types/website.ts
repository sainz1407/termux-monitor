// Types for the Website tab. They mirror the JSON emitted by
// internal/handlers/website_handler.go.

/** Supported site engines. `static` is served by python3's built-in http.server. */
export type WebsiteEngine = 'node' | 'php' | 'static'

/** A managed site as stored in the registry. */
export interface WebsiteSite {
  id: string
  name: string
  engine: WebsiteEngine
  path: string
  port: number
  /** Epoch milliseconds of the creation time. */
  createdAt: number
}

/** A site enriched with live runtime state. Runtime fields are not persisted. */
export interface WebsiteSiteView extends WebsiteSite {
  running: boolean
  /** 0 when the site is not running. */
  pid: number
  /** http://<host>:<port>, empty while stopped. */
  url: string
  /** Milliseconds since the site started, 0 when stopped. */
  uptimeMs: number
  /** False when the engine binary is missing from $PREFIX. */
  engineInstalled: boolean
  /** First line of the engine's --version output, empty when unknown. */
  engineVersion: string
}

/** Result of GET /api/website/list. */
export interface WebsiteListResponse {
  /** False when the Termux environment is not usable at all. */
  available: boolean
  reason: string
  sites: WebsiteSiteView[]
  engines: WebsiteEngine[]
}

/** One engine as reported by GET /api/website/probe. */
export interface WebsiteEngineInfo {
  engine: WebsiteEngine
  bin: string
  installed: boolean
}

/** Result of GET /api/website/probe. */
export interface WebsiteProbeResponse {
  engines: WebsiteEngineInfo[]
}

/** Result of GET /api/website/logs. */
export interface WebsiteLogsResponse {
  logs: string
  running: boolean
}

/** Result of POST /api/website/add. */
export interface WebsiteAddResponse {
  site: WebsiteSite
}
