import { getLang, t } from '../i18n'
import type { Container } from './types'

export function bytes(n?: number, digits = 1) {
  if (n === undefined || n === null || isNaN(n)) return '—'
  if (n < 1024) return `${Math.round(n)} B`
  const u = ['KB', 'MB', 'GB', 'TB']
  let i = -1
  do {
    n /= 1024
    i++
  } while (n >= 1024 && i < u.length - 1)
  return `${n.toFixed(n >= 100 ? 0 : digits)} ${u[i]}`
}

const rtf = new Intl.RelativeTimeFormat(getLang(), { numeric: 'auto' })

export function ago(input: number | string | undefined) {
  if (!input) return '—'
  const ms = typeof input === 'number' ? (input < 1e12 ? input * 1000 : input) : Date.parse(input)
  const s = (ms - Date.now()) / 1000
  const abs = Math.abs(s)
  if (abs < 45) return t('now')
  if (abs < 3600) return rtf.format(Math.round(s / 60), 'minute')
  if (abs < 86400) return rtf.format(Math.round(s / 3600), 'hour')
  if (abs < 86400 * 30) return rtf.format(Math.round(s / 86400), 'day')
  if (abs < 86400 * 365) return rtf.format(Math.round(s / 86400 / 30), 'month')
  return rtf.format(Math.round(s / 86400 / 365), 'year')
}

export function dateTime(input: number | string | undefined) {
  if (!input) return '—'
  const ms = typeof input === 'number' ? (input < 1e12 ? input * 1000 : input) : Date.parse(input)
  return new Date(ms).toLocaleString(getLang(), { dateStyle: 'medium', timeStyle: 'short' })
}

/** Docker's English Status ("Up 2 hours (healthy)"), localized. */
export function humanStatus(s: string) {
  if (getLang() === 'en') return s
  const units: [RegExp, string][] = [
    [/Less than a second/, t('less than 1 s')], [/About a minute/, t('1 minute')], [/About an hour/, t('1 hour')], [/An? (\w+)/, '1 $1'],
    [/seconds?/, 's'], [/minutes?/, 'min'], [/hours?/, 'h'], [/days?/, t('days')], [/weeks?/, t('weeks')], [/months?/, t('months')], [/years?/, t('years')],
  ]
  let out = s
  for (const [re, rep] of units) out = out.replace(re, rep)
  return out
    .replace(/^Up (.*?)( \((.*)\))?$/, (_, d, __, h) => `${t('up {0}', d)}${h ? ` · ${h.replace('health: starting', t('starting')).replace('unhealthy', t('unhealthy')).replace('healthy', t('healthy'))}` : ''}`)
    .replace(/^Exited \((\d+)\) (.*) ago$/, (_, c, d) => t('exited ({0}) {1} ago', c, d))
    .replace(/^Created$/, t('created'))
    .replace(/^Restarting \((\d+)\) (.*) ago$/, (_, c, d) => t('restarting ({0}) {1} ago', c, d))
    .replace(/ \(Paused\)$/, ' · ' + t('paused'))
}

/** Shortens image references that are bare IDs. */
export const imageLabel = (ref: string) => (/^(sha256:)?[0-9a-f]{64}$/.test(ref) ? shortId(ref) : ref)

export const cname = (c: { Names?: string[]; Id: string }) => (c.Names?.[0] || c.Id.slice(0, 12)).replace(/^\//, '')
export const shortId = (id: string) => id.replace(/^sha256:/, '').slice(0, 12)
export const stackOf = (c: Container) => c.Labels?.['com.docker.compose.project'] || ''
export const serviceOf = (c: Container) => c.Labels?.['com.docker.compose.service'] || ''

export function uniquePorts(c: Container) {
  const seen = new Set<string>()
  return (c.Ports || [])
    .filter((p) => p.PublicPort)
    .filter((p) => {
      const k = `${p.PublicPort}:${p.PrivatePort}/${p.Type}`
      if (seen.has(k)) return false
      seen.add(k)
      return true
    })
    .sort((a, b) => a.PrivatePort - b.PrivatePort)
}

export function stateTone(state: string): 'ok' | 'warn' | 'bad' | 'idle' {
  switch (state) {
    case 'running':
      return 'ok'
    case 'paused':
    case 'restarting':
    case 'partial':
    case 'created':
      return 'warn'
    case 'dead':
      return 'bad'
    default:
      return 'idle'
  }
}

export const stateLabel: Record<string, string> = {
  running: t('Running'),
  exited: t('Stopped'),
  paused: t('Paused'),
  restarting: t('Restarting'),
  created: t('Created'),
  dead: t('Dead'),
  removing: t('Removing'),
  partial: t('Partial'),
  stopped: t('Stopped'),
  empty: t('No containers'),
}

/** Exit code from a Status string like "Exited (137) 2 hours ago". */
export function exitCode(status: string) {
  const m = /Exited \((\d+)\)/.exec(status)
  return m ? Number(m[1]) : null
}

export function healthOf(status: string): 'healthy' | 'unhealthy' | 'starting' | null {
  if (status.includes('(healthy)')) return 'healthy'
  if (status.includes('(unhealthy)')) return 'unhealthy'
  if (status.includes('health: starting')) return 'starting'
  return null
}

export function hue(s: string) {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0
  return h % 360
}

export function copy(text: string) {
  navigator.clipboard?.writeText(text).catch(() => {
    const ta = document.createElement('textarea')
    ta.value = text
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    ta.remove()
  })
}

export function imageRepo(ref: string) {
  const at = ref.indexOf('@')
  if (at >= 0) return ref.slice(0, at)
  const i = ref.lastIndexOf(':')
  return i > ref.lastIndexOf('/') ? ref.slice(0, i) : ref
}

export function imageTag(ref: string) {
  const i = ref.lastIndexOf(':')
  return i > ref.lastIndexOf('/') ? ref.slice(i + 1) : 'latest'
}
