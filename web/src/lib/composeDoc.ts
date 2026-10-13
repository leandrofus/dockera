import { t } from '../i18n'
// Visual editing of compose files on top of the YAML document itself: the
// YAML is the source of truth, every helper here edits the parsed Document
// in place so comments, formatting and unknown keys survive. Readers accept
// both the short and long syntaxes compose allows; writers keep whichever
// syntax the file already used.
import { Document, isMap, isNode, isPair, isScalar, isSeq, parseDocument, Scalar, YAMLMap, YAMLSeq } from 'yaml'

export type Path = (string | number)[]

export function parse(text: string): { doc: Document.Parsed | null; error: string | null } {
  const doc = parseDocument(text)
  if (doc.errors.length) return { doc: null, error: doc.errors[0].message }
  return { doc, error: null }
}

export const toJS = (v: unknown): any => (isNode(v) ? (v as any).toJSON() : v)
export const get = (doc: Document, path: Path): any => toJS(doc.getIn(path, true))
const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b)

// Values YAML would read as bool/null/number when unquoted.
const AMBIGUOUS = /^(true|false|yes|no|on|off|y|n|null|~|[-+]?(\d[\d_]*)?\.?\d+([eE][-+]?\d+)?|0x[0-9a-fA-F]+|0o[0-7]+)$/i

function node(doc: Document, v: unknown, quote = false): unknown {
  if (v === null) return emptyScalar()
  if (typeof v === 'string') {
    const s = new Scalar(v)
    if (quote || AMBIGUOUS.test(v) || /^\S*\d:\d/.test(v)) s.type = Scalar.QUOTE_DOUBLE
    return s
  }
  return doc.createNode(v)
}

const empty = (v: unknown) =>
  v === undefined || v === null || v === '' || (Array.isArray(v) && v.length === 0) || (typeof v === 'object' && !Array.isArray(v) && Object.keys(v as object).length === 0)

/** Sets a value (or deletes it when empty), editing scalars in place. */
export function setValue(doc: Document, path: Path, v: unknown) {
  if (empty(v)) {
    if (doc.hasIn(path)) doc.deleteIn(path)
    pruneEmptyParents(doc, path.slice(0, -1))
    return
  }
  const cur = doc.getIn(path, true)
  if (isScalar(cur) && typeof v !== 'object') {
    if (String(cur.value) !== String(v)) {
      cur.value = v
      if (typeof v === 'string' && AMBIGUOUS.test(v)) cur.type = Scalar.QUOTE_DOUBLE
    }
    return
  }
  if (cur !== undefined && same(toJS(cur), v)) return
  doc.setIn(path, node(doc, v))
}

// deploy: {resources: {limits: {}}} left empty after clearing a field
function pruneEmptyParents(doc: Document, path: Path) {
  while (path.length > 2) {
    const n = doc.getIn(path, true)
    if (!(isMap(n) && n.items.length === 0)) return
    doc.deleteIn(path)
    path = path.slice(0, -1)
  }
}

/** Replaces a sequence keeping unchanged items (and their comments). */
export function setList(doc: Document, path: Path, items: unknown[], quote = false) {
  const cur = doc.getIn(path, true)
  if (!items.length) {
    if (cur !== undefined) doc.deleteIn(path)
    return
  }
  if (!isSeq(cur)) {
    const seq = new YAMLSeq()
    items.forEach((it) => seq.items.push(node(doc, it, quote) as any))
    doc.setIn(path, seq)
    return
  }
  items.forEach((it, i) => {
    const old = cur.items[i]
    if (old !== undefined && same(toJS(old), it)) return
    if (isScalar(old) && typeof it !== 'object') {
      old.value = it
      return
    }
    const n = node(doc, it, quote) as any
    if (old === undefined) cur.items.push(n)
    else cur.items[i] = n
  })
  cur.items.length = items.length
}

/** Replaces a mapping's entries keeping existing pairs (and comments). */
export function setMap(doc: Document, path: Path, entries: [string, unknown][]) {
  const cur = doc.getIn(path, true)
  if (!entries.length) {
    if (cur !== undefined) doc.deleteIn(path)
    return
  }
  if (!isMap(cur)) {
    const m = new YAMLMap()
    entries.forEach(([k, v]) => m.items.push(doc.createPair(k, node(doc, v)) as any))
    doc.setIn(path, m)
    return
  }
  const keys = entries.map(([k]) => k)
  cur.items = cur.items.filter((p) => keys.includes(String(toJS(p.key))))
  for (const [k, v] of entries) {
    const pair = cur.items.find((p) => String(toJS(p.key)) === k)
    if (!pair) {
      cur.items.push(doc.createPair(k, node(doc, v)) as any)
    } else if (isScalar(pair.value) && typeof v !== 'object' && v !== null) {
      if (String(pair.value.value ?? '') !== String(v)) pair.value.value = v
    } else if (!same(toJS(pair.value), v)) {
      pair.value = node(doc, v) as any
    }
  }
  // follow the editor's order (renames, additions) without touching untouched pairs
  cur.items.sort((a, b) => keys.indexOf(String(toJS(a.key))) - keys.indexOf(String(toJS(b.key))))
}

/** Renames a mapping key in place, keeping its position and value. */
export function renameKey(doc: Document, path: Path, from: string, to: string) {
  const m = doc.getIn(path, true)
  if (!isMap(m)) return
  const pair = m.items.find((p) => isPair(p) && String(toJS(p.key)) === from)
  if (pair) pair.key = new Scalar(to) as any
}

// ---------- splitting helpers that respect ${VAR:-default} ----------

export function splitTop(s: string, sep: string): string[] {
  const out: string[] = []
  let depth = 0
  let cur = ''
  let bracket = 0
  for (let i = 0; i < s.length; i++) {
    const c = s[i]
    if (c === '{' && s[i - 1] === '$') depth++
    else if (c === '}' && depth > 0) depth--
    else if (c === '[') bracket++
    else if (c === ']') bracket--
    if (c === sep && depth === 0 && bracket === 0) {
      out.push(cur)
      cur = ''
    } else cur += c
  }
  out.push(cur)
  return out
}

// ---------- key/value (environment, labels): list or map ----------

export interface KV {
  form: 'list' | 'map'
  rows: [string, string][]
}

export function readKV(v: unknown): KV {
  if (Array.isArray(v))
    return {
      form: 'list',
      rows: v.map((x) => {
        const s = String(x)
        const i = s.indexOf('=')
        return (i < 0 ? [s, ''] : [s.slice(0, i), s.slice(i + 1)]) as [string, string]
      }),
    }
  if (v && typeof v === 'object') return { form: 'map', rows: Object.entries(v).map(([k, x]) => [k, x === null || x === undefined ? '' : String(x)]) }
  return { form: 'map', rows: [] }
}

export function writeKV(doc: Document, path: Path, kv: KV) {
  const rows = kv.rows.filter(([k]) => k.trim())
  if (kv.form === 'list') setList(doc, path, rows.map(([k, v]) => `${k}=${v}`))
  else setMap(doc, path, rows)
}

// ---------- ports ----------

export interface PortRow {
  ip: string
  host: string
  container: string
  proto: string
  long?: boolean // was long syntax
  extra?: Record<string, unknown> // unknown long-syntax fields (mode, name…)
  raw?: unknown // original item, written back untouched if the row didn't change
  rawKey?: string
}

const portKey = (p: PortRow) => JSON.stringify([p.ip, p.host, p.container, p.proto])

export function readPorts(v: unknown): PortRow[] {
  if (!Array.isArray(v)) return []
  return v.map((p) => {
    const r = parsePort(p)
    return { ...r, raw: p, rawKey: portKey(r) }
  })
}

function parsePort(p: unknown): PortRow {
  {
    if (p && typeof p === 'object') {
      const { target, published, host_ip, protocol, ...extra } = p as any
      return { ip: host_ip ?? '', host: published !== undefined ? String(published) : '', container: String(target ?? ''), proto: protocol ?? 'tcp', long: true, extra }
    }
    let s = String(p)
    let proto = 'tcp'
    const slash = s.lastIndexOf('/')
    if (slash > s.lastIndexOf('}')) {
      proto = s.slice(slash + 1)
      s = s.slice(0, slash)
    }
    const parts = splitTop(s, ':')
    if (parts.length === 1) return { ip: '', host: '', container: parts[0], proto }
    if (parts.length === 2) return { ip: '', host: parts[0], container: parts[1], proto }
    return { ip: parts.slice(0, -2).join(':').replace(/^\[|\]$/g, ''), host: parts[parts.length - 2], container: parts[parts.length - 1], proto }
  }
}

export function formatPort(p: PortRow): unknown {
  if (p.raw !== undefined && p.rawKey === portKey(p)) return p.raw
  if (p.long) {
    const o: Record<string, unknown> = { target: /^\d+$/.test(p.container) ? Number(p.container) : p.container }
    if (p.host) o.published = p.host
    if (p.ip) o.host_ip = p.ip
    if (p.proto && p.proto !== 'tcp') o.protocol = p.proto
    return { ...o, ...p.extra }
  }
  const ip = p.ip ? (p.ip.includes(':') ? `[${p.ip}]` : p.ip) + ':' : ''
  const proto = p.proto && p.proto !== 'tcp' ? '/' + p.proto : ''
  if (!p.host) return ip ? `${ip}:${p.container}${proto}` : `${p.container}${proto}`
  return `${ip}${p.host}:${p.container}${proto}`
}

export function writePorts(doc: Document, path: Path, rows: PortRow[]) {
  setList(doc, path, rows.filter((r) => r.container.trim()).map(formatPort), true)
}

// ---------- volumes (service mounts) ----------

export interface MountRow {
  type: 'volume' | 'bind' | 'tmpfs'
  source: string
  target: string
  ro: boolean
  mode: string // other short-syntax options (z, Z, rslave…)
  long?: boolean
  extra?: Record<string, unknown>
  raw?: unknown
  rawKey?: string
}

const mountKey = (m: MountRow) => JSON.stringify([m.type, m.source, m.target, m.ro, m.mode])

const looksPath = (s: string) => /^[./~]/.test(s) || s.includes('/')

export function readMounts(v: unknown): MountRow[] {
  if (!Array.isArray(v)) return []
  return v.map((m) => {
    const r = parseMount(m)
    return { ...r, raw: m, rawKey: mountKey(r) }
  })
}

function parseMount(m: unknown): MountRow {
  {
    if (m && typeof m === 'object') {
      const { type, source, target, read_only, ...extra } = m as any
      return { type: type ?? 'volume', source: source ?? '', target: target ?? '', ro: !!read_only, mode: '', long: true, extra }
    }
    const parts = splitTop(String(m), ':')
    if (parts.length === 1) return { type: 'volume', source: '', target: parts[0], ro: false, mode: '' }
    const [source, target, opts = ''] = parts
    const flags = opts.split(',').filter(Boolean)
    return {
      type: looksPath(source) ? 'bind' : 'volume',
      source,
      target,
      ro: flags.includes('ro'),
      mode: flags.filter((f) => f !== 'ro' && f !== 'rw').join(','),
    }
  }
}

export function formatMount(m: MountRow): unknown {
  if (m.raw !== undefined && m.rawKey === mountKey(m)) return m.raw
  if (m.long || m.type === 'tmpfs') {
    const o: Record<string, unknown> = { type: m.type, target: m.target }
    if (m.source && m.type !== 'tmpfs') o.source = m.source
    if (m.ro) o.read_only = true
    return { ...o, ...m.extra }
  }
  if (!m.source) return m.target
  const opts = [m.ro ? 'ro' : '', m.mode].filter(Boolean).join(',')
  return `${m.source}:${m.target}${opts ? ':' + opts : ''}`
}

export function writeMounts(doc: Document, path: Path, rows: MountRow[]) {
  setList(doc, path, rows.filter((r) => r.target.trim()).map(formatMount))
}

// ---------- service networks ----------

export interface NetRow {
  name: string
  aliases: string[]
  ipv4: string
}

export function readNets(v: unknown): { form: 'list' | 'map'; rows: NetRow[] } {
  if (Array.isArray(v)) return { form: 'list', rows: v.map((n) => ({ name: String(n), aliases: [], ipv4: '' })) }
  if (v && typeof v === 'object')
    return {
      form: 'map',
      rows: Object.entries(v).map(([name, c]: [string, any]) => ({ name, aliases: c?.aliases ?? [], ipv4: c?.ipv4_address ?? '' })),
    }
  return { form: 'list', rows: [] }
}

export function writeNets(doc: Document, path: Path, form: 'list' | 'map', rows: NetRow[]) {
  rows = rows.filter((r) => r.name.trim())
  const plain = rows.every((r) => !r.aliases.length && !r.ipv4)
  if (form === 'list' && plain) return setList(doc, path, rows.map((r) => r.name))
  setMap(
    doc,
    path,
    rows.map((r) => {
      const c: Record<string, unknown> = {}
      if (r.aliases.length) c.aliases = r.aliases
      if (r.ipv4) c.ipv4_address = r.ipv4
      return [r.name, Object.keys(c).length ? c : null] as [string, unknown]
    }),
  )
}

// ---------- depends_on ----------

export interface DepRow {
  name: string
  condition: string
}

export function readDeps(v: unknown): { form: 'list' | 'map'; rows: DepRow[] } {
  if (Array.isArray(v)) return { form: 'list', rows: v.map((n) => ({ name: String(n), condition: 'service_started' })) }
  if (v && typeof v === 'object') return { form: 'map', rows: Object.entries(v).map(([name, c]: [string, any]) => ({ name, condition: c?.condition ?? 'service_started' })) }
  return { form: 'list', rows: [] }
}

export function writeDeps(doc: Document, path: Path, form: 'list' | 'map', rows: DepRow[]) {
  if (form === 'list' && rows.every((r) => r.condition === 'service_started')) return setList(doc, path, rows.map((r) => r.name))
  setMap(doc, path, rows.map((r) => [r.name, { condition: r.condition }]))
}

// ---------- commands ----------

export function cmdText(v: unknown): string {
  if (Array.isArray(v)) return v.map((x) => (/[\s"']/.test(String(x)) ? JSON.stringify(String(x)) : String(x))).join(' ')
  return v === undefined || v === null ? '' : String(v)
}

// ---------- services ----------

export function services(doc: Document): string[] {
  const s = doc.getIn(['services'], true)
  return isMap(s) ? s.items.map((p) => String(toJS(p.key))) : []
}

export function topNames(doc: Document, key: 'volumes' | 'networks'): string[] {
  const s = doc.getIn([key], true)
  return isMap(s) ? s.items.map((p) => String(toJS(p.key))) : []
}

export function addService(doc: Document, name: string, def: Record<string, unknown>) {
  if (!doc.has('services')) doc.set('services', new YAMLMap())
  doc.setIn(['services', name], doc.createNode(def))
}

export function duplicateService(doc: Document, from: string, to: string) {
  const n = doc.getIn(['services', from], true)
  const copy = isNode(n) ? (n as any).clone() : doc.createNode({})
  if (isMap(copy)) copy.delete('container_name')
  doc.setIn(['services', to], copy)
}

/** Renames a service and fixes depends_on / links references. */
export function renameService(doc: Document, from: string, to: string) {
  renameKey(doc, ['services'], from, to)
  for (const s of services(doc)) {
    const deps = doc.getIn(['services', s, 'depends_on'], true)
    if (isSeq(deps)) deps.items.forEach((it) => isScalar(it) && it.value === from && (it.value = to))
    else if (isMap(deps)) renameKey(doc, ['services', s, 'depends_on'], from, to)
  }
}

export function removeService(doc: Document, name: string) {
  doc.deleteIn(['services', name])
  for (const s of services(doc)) {
    const deps = readDeps(get(doc, ['services', s, 'depends_on']))
    if (deps.rows.some((r) => r.name === name)) writeDeps(doc, ['services', s, 'depends_on'], deps.form, deps.rows.filter((r) => r.name !== name))
  }
}

/** YAML text of a single node, comments included (isolated editing). */
export function nodeText(doc: Document, path: Path): string {
  const n = doc.getIn(path, true)
  const d = new Document()
  d.contents = isNode(n) ? (n as any).clone() : d.createNode(n ?? {})
  return d.toString()
}

export function replaceNode(doc: Document, path: Path, text: string): string | null {
  const sub = parseDocument(text)
  if (sub.errors.length) return sub.errors[0].message
  if (!isMap(sub.contents)) return t('the service must be a YAML map')
  doc.setIn(path, sub.contents)
  return null
}

// ---------- variables ----------

export function envMap(text: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const line of text.split('\n')) {
    const m = /^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$/.exec(line)
    if (m) out[m[1]] = m[2].replace(/^(['"])(.*)\1$/, '$2')
  }
  return out
}

/** Resolves ${VAR}, ${VAR:-def}, ${VAR-def}, ${VAR:?err} and $VAR. */
export function resolveVars(s: string, env: Record<string, string>): { value: string; missing: string[] } {
  const missing: string[] = []
  const value = s
    .replace(/\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:?[-?+])([^}]*))?\}/g, (_, name, op, arg) => {
      const v = env[name]
      const unset = v === undefined
      const emptyish = unset || v === ''
      if (op === ':-') return emptyish ? arg : v
      if (op === '-') return unset ? arg : v
      if (op === ':+') return emptyish ? '' : arg
      if (op === '+') return unset ? '' : arg
      if (unset) missing.push(name)
      return v ?? ''
    })
    .replace(/\$([A-Za-z_][A-Za-z0-9_]*)/g, (_, name) => {
      if (env[name] === undefined) missing.push(name)
      return env[name] ?? ''
    })
    .replace(/\$\$/g, '$')
  return { value, missing }
}

/** Declares a top-level volume/network (`name:` with no options). */
export function declare(doc: Document, kind: 'volumes' | 'networks', name: string) {
  if (doc.hasIn([kind, name])) return
  if (!isMap(doc.get(kind, true))) doc.set(kind, new YAMLMap())
  ;(doc.get(kind, true) as YAMLMap).items.push(doc.createPair(name, emptyScalar()) as any)
}

// renders as `key:` instead of `key: null`, like hand-written compose files
export function emptyScalar() {
  const s = new Scalar(null)
  ;(s as any).source = ''
  return s
}
