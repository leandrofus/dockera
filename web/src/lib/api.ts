import { getLang } from '../i18n'
// Thin client for ducktainer's HTTP API.

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

let onUnauthorized: () => void = () => {}
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

export interface RequestOpts {
  /** Title in the tasks tray; false keeps the request out of it. */
  task?: string | false
}

// Every mutating request is shown as a task (see ui/overlays).
type Tracker = (method: string, path: string, p: Promise<unknown>, title?: string) => void
let tracker: Tracker | null = null
export function setTaskTracker(fn: Tracker) {
  tracker = fn
}
const untracked = ['/login', '/logout', '/settings/access/test']

function request<T>(method: string, path: string, body?: unknown, opts: RequestOpts = {}): Promise<T> {
  const p = send<T>(method, path, body)
  if (method !== 'GET' && opts.task !== false && tracker && !untracked.includes(path)) tracker(method, path, p, opts.task || undefined)
  return p
}

async function send<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch('/api' + path, {
    method,
    headers: { 'X-Ducktainer': '1', 'X-Lang': getLang(), ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}) },
    body: body !== undefined ? JSON.stringify(body) : undefined,
    credentials: 'same-origin',
  })
  if (res.status === 401 && path !== '/login') onUnauthorized()
  const text = await res.text()
  let data: any = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    data = text
  }
  if (!res.ok) throw new ApiError(res.status, (data && data.error) || res.statusText)
  return data as T
}

export const api = {
  get: <T = any>(p: string) => request<T>('GET', p),
  post: <T = any>(p: string, body: unknown = {}, opts?: RequestOpts) => request<T>('POST', p, body, opts),
  put: <T = any>(p: string, body: unknown = {}, opts?: RequestOpts) => request<T>('PUT', p, body, opts),
  del: <T = any>(p: string, opts?: RequestOpts) => request<T>('DELETE', p, undefined, opts),
}

export const enc = encodeURIComponent

/** Reads an NDJSON stream, calling onEvent per object. Resolves when it ends. */
export async function stream(
  method: string,
  path: string,
  body: unknown,
  onEvent: (ev: any) => void,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch('/api' + path, {
    method,
    headers: { 'X-Ducktainer': '1', 'X-Lang': getLang(), ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}) },
    body: body !== undefined ? JSON.stringify(body) : undefined,
    signal,
  })
  if (res.status === 401) onUnauthorized()
  if (!res.ok || !res.body) {
    let msg = res.statusText
    try {
      msg = (await res.json()).error || msg
    } catch {}
    throw new ApiError(res.status, msg)
  }
  const reader = res.body.getReader()
  const dec = new TextDecoder()
  let buf = ''
  let streamErr: string | null = null
  for (;;) {
    const { value, done } = await reader.read()
    if (done) break
    buf += dec.decode(value, { stream: true })
    let i: number
    while ((i = buf.indexOf('\n')) >= 0) {
      const line = buf.slice(0, i).trim()
      buf = buf.slice(i + 1)
      if (!line) continue
      try {
        const ev = JSON.parse(line)
        if (ev.error && Object.keys(ev).length === 1) streamErr = ev.error
        onEvent(ev)
      } catch {}
    }
  }
  if (streamErr) throw new ApiError(500, streamErr)
}

export function wsURL(path: string) {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${location.host}/api${path}${path.includes('?') ? '&' : '?'}lang=${getLang()}`
}
