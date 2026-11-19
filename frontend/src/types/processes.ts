export interface ProcessInfo {
  pid: number
  ppid: number
  name: string
  cmd: string
  cpu: number
  mem_pct: number
  mem_rss: number
  mem_text: string
  state: string
  user: string
  exe_path: string
  runtime: string
  elapsed: string
  elapsed_sec: number
  threads: number
}

export interface ProcessesResponse {
  processes: ProcessInfo[]
  count: number
  sort: string
}

export interface ProcessKillResponse {
  success: boolean
  error?: string
}
