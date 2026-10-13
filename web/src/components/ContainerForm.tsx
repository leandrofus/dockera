import { t } from '../i18n'
import { useState } from 'react'
import type { ReactNode } from 'react'
import { Plus, X, ClipboardPaste, List } from 'lucide-react'
import type { Network, Spec, Volume } from '../lib/types'
import { useResource, invalidate } from '../lib/store'
import { enc } from '../lib/api'
import { Modal, runJob, toast } from '../ui/overlays'
import { useMenu } from '../ui/ContextMenu'
import { FormTabs } from '../ui/common'
import { go } from '../lib/router'

export const emptySpec = (p: Partial<Spec> = {}): Spec => ({
  name: '',
  image: '',
  cmd: null,
  entrypoint: null,
  env: [],
  ports: [],
  mounts: [],
  restart: 'unless-stopped',
  labels: {},
  networks: [],
  network_mode: '',
  hostname: '',
  user: '',
  working_dir: '',
  privileged: false,
  tty: false,
  open_stdin: false,
  memory: 0,
  nano_cpus: 0,
  cap_add: [],
  extra_hosts: [],
  ...p,
})

// Shell-ish split that respects quotes, for cmd/entrypoint inputs.
export function splitArgs(s: string): string[] | null {
  const out: string[] = []
  let cur = ''
  let q: string | null = null
  let has = false
  for (const ch of s) {
    if (q) {
      if (ch === q) q = null
      else cur += ch
    } else if (ch === '"' || ch === "'") {
      q = ch
      has = true
    } else if (/\s/.test(ch)) {
      if (cur || has) out.push(cur)
      cur = ''
      has = false
    } else cur += ch
  }
  if (cur || has) out.push(cur)
  return out.length ? out : null
}
const joinArgs = (a: string[] | null) => (a || []).map((x) => (/[\s"']/.test(x) || x === '' ? JSON.stringify(x) : x)).join(' ')

function Pane({ children, hint }: { children: ReactNode; hint?: ReactNode }) {
  return (
    <div className="form-pane">
      {hint && <p className="help">{hint}</p>}
      {children}
    </div>
  )
}

/** Editable key/value rows with a "paste as text" mode. */
export function KVEditor({ rows, onChange, keyLabel = t('Key'), valueLabel = t('Value'), sep = '=' }: { rows: [string, string][]; onChange: (r: [string, string][]) => void; keyLabel?: string; valueLabel?: string; sep?: string }) {
  const [text, setText] = useState<string | null>(null)
  const menu = useMenu()
  if (text !== null)
    return (
      <div>
        <textarea className="mono" rows={Math.max(6, rows.length + 2)} value={text} onChange={(e) => setText(e.target.value)} placeholder={t('KEY{0}value', sep)} />
        <div className="row-actions">
          <button
            type="button"
            className="btn btn-sm"
            onClick={() => {
              onChange(
                text
                  .split('\n')
                  .map((l) => l.trim())
                  .filter((l) => l && !l.startsWith('#'))
                  .map((l) => {
                    const i = l.indexOf(sep)
                    return (i < 0 ? [l, ''] : [l.slice(0, i), l.slice(i + 1)]) as [string, string]
                  }),
              )
              setText(null)
            }}
          >
            {t('Apply')}
          </button>
          <button type="button" className="btn btn-sm btn-ghost" onClick={() => setText(null)}>
            {t('Cancel')}
          </button>
        </div>
      </div>
    )
  return (
    <div className="kv">
      {rows.map(([k, v], i) => (
        <div
          className="kv-row"
          key={i}
          onContextMenu={menu(() => [
            { label: t('Duplicate row'), onClick: () => onChange([...rows.slice(0, i + 1), [k, v], ...rows.slice(i + 1)]) },
            { label: t('Copy'), hint: `${k}${sep}…`, onClick: () => navigator.clipboard?.writeText(`${k}${sep}${v}`) },
            'sep',
            { label: t('Delete row'), danger: true, onClick: () => onChange(rows.filter((_, j) => j !== i)) },
          ])}
        >
          <input className="mono" placeholder={keyLabel} value={k} onChange={(e) => onChange(rows.map((r, j) => (j === i ? [e.target.value, r[1]] : r)))} />
          <input className="mono" placeholder={valueLabel} value={v} onChange={(e) => onChange(rows.map((r, j) => (j === i ? [r[0], e.target.value] : r)))} />
          <button type="button" className="icon-btn" onClick={() => onChange(rows.filter((_, j) => j !== i))}>
            <X size={14} />
          </button>
        </div>
      ))}
      <div className="row-actions">
        <button type="button" className="btn btn-sm btn-ghost" onClick={() => onChange([...rows, ['', '']])}>
          <Plus size={14} /> {t('Add')}
        </button>
        <button type="button" className="btn btn-sm btn-ghost" onClick={() => setText(rows.map(([k, v]) => `${k}${sep}${v}`).join('\n'))}>
          <ClipboardPaste size={14} /> {t('Edit as text')}
        </button>
      </div>
    </div>
  )
}

const envRows = (env: string[] | null): [string, string][] =>
  (env || []).map((e) => {
    const i = e.indexOf('=')
    return i < 0 ? [e, ''] : [e.slice(0, i), e.slice(i + 1)]
  })

export function ContainerForm({ ep, value, onChange, hidePaths }: { ep: string; value: Spec; onChange: (s: Spec) => void; hidePaths?: boolean }) {
  const { data: nets } = useResource<Network[]>(`/e/${enc(ep)}/networks`)
  const { data: vols } = useResource<Volume[]>(`/e/${enc(ep)}/volumes`)
  const set = (p: Partial<Spec>) => onChange({ ...value, ...p })
  const ports = value.ports || []
  const mounts = value.mounts || []
  const networks = value.networks || []
  const restartBase = value.restart.split(':')[0] || 'no'
  return (
    <FormTabs
      tabs={[
        { id: 'general', label: t('General'), content: (
      <Pane>
        <div className="grid-2">
          <label className="field">
            <span>{t('Name')}</span>
            <input value={value.name} placeholder="my-app" onChange={(e) => set({ name: e.target.value })} />
          </label>
          <label className="field">
            <span>{t('Image')}</span>
            <input className="mono" value={value.image} placeholder="nginx:alpine" onChange={(e) => set({ image: e.target.value })} />
          </label>
          <label className="field">
            <span>{t('Restart policy')}</span>
            <select value={restartBase} onChange={(e) => set({ restart: e.target.value })}>
              <option value="no">{t('Don\'t restart')}</option>
              <option value="always">{t('Always')}</option>
              <option value="unless-stopped">{t('Unless stopped manually')}</option>
              <option value="on-failure">{t('On failure')}</option>
            </select>
          </label>
          <label className="field">
            <span>Hostname</span>
            <input value={value.hostname} placeholder={t('(automatic)')} onChange={(e) => set({ hostname: e.target.value })} />
          </label>
          <label className="field span-2">
            <span>{t('Command')}</span>
            <input className="mono" value={joinArgs(value.cmd)} placeholder={t('(from the image)')} onChange={(e) => set({ cmd: splitArgs(e.target.value) })} />
          </label>
          {!hidePaths && (
            <>
              <label className="field">
                <span>Entrypoint</span>
                <input className="mono" value={joinArgs(value.entrypoint)} placeholder={t('(from the image)')} onChange={(e) => set({ entrypoint: splitArgs(e.target.value) })} />
              </label>
              <label className="field">
                <span>{t('Working directory')}</span>
                <input className="mono" value={value.working_dir} onChange={(e) => set({ working_dir: e.target.value })} />
              </label>
              <label className="field">
                <span>{t('User')}</span>
                <input className="mono" value={value.user} placeholder={t('(from the image)')} onChange={(e) => set({ user: e.target.value })} />
              </label>
            </>
          )}
        </div>
      </Pane>
        ) },
        { id: 'ports', label: t('Ports'), count: ports.length, content: (
      <Pane hint={t('Host port → container port. Leave the IP empty for all interfaces, or 127.0.0.1 to expose locally only.')}>
        {ports.map((p, i) => (
          <div className="port-row" key={i}>
            <input className="mono ip" placeholder={t('IP (all)')} value={p.host_ip} onChange={(e) => set({ ports: ports.map((x, j) => (j === i ? { ...x, host_ip: e.target.value } : x)) })} />
            <input className="mono" placeholder="host" value={p.host_port} onChange={(e) => set({ ports: ports.map((x, j) => (j === i ? { ...x, host_port: e.target.value } : x)) })} />
            <span className="muted">→</span>
            <input className="mono" placeholder={t('80 or 53/udp')} value={p.container} onChange={(e) => set({ ports: ports.map((x, j) => (j === i ? { ...x, container: e.target.value } : x)) })} />
            <button type="button" className="icon-btn" onClick={() => set({ ports: ports.filter((_, j) => j !== i) })}>
              <X size={14} />
            </button>
          </div>
        ))}
        <button type="button" className="btn btn-sm btn-ghost" onClick={() => set({ ports: [...ports, { host_ip: '', host_port: '', container: '' }] })}>
          <Plus size={14} /> {t('Publish port')}
        </button>
      </Pane>
        ) },
        { id: 'env', label: t('Environment'), count: (value.env || []).length, content: (
      <Pane>
        <KVEditor rows={envRows(value.env)} onChange={(rows) => set({ env: rows.filter(([k]) => k).map(([k, v]) => `${k}=${v}`) })} keyLabel={t('NAME')} />
      </Pane>
        ) },
        { id: 'mounts', label: t('Storage'), count: mounts.length, content: (
      <Pane>
        <datalist id="vol-names">
          {(vols || []).map((v) => (
            <option key={v.Name} value={v.Name} />
          ))}
        </datalist>
        {mounts.map((m, i) => (
          <div className="mount-row" key={i}>
            <select value={m.type} onChange={(e) => set({ mounts: mounts.map((x, j) => (j === i ? { ...x, type: e.target.value } : x)) })}>
              <option value="volume">{t('volume')}</option>
              <option value="bind">{t('bind (path)')}</option>
              <option value="tmpfs">tmpfs</option>
            </select>
            <input
              className="mono"
              list={m.type === 'volume' ? 'vol-names' : undefined}
              placeholder={m.type === 'bind' ? t('/path/on/host') : m.type === 'tmpfs' ? '—' : t('volume-name')}
              disabled={m.type === 'tmpfs'}
              value={m.source}
              onChange={(e) => set({ mounts: mounts.map((x, j) => (j === i ? { ...x, source: e.target.value } : x)) })}
            />
            <span className="muted">→</span>
            <input className="mono" placeholder={t('/path/in/container')} value={m.target} onChange={(e) => set({ mounts: mounts.map((x, j) => (j === i ? { ...x, target: e.target.value } : x)) })} />
            <label className="check" title={t('Read-only')}>
              <input type="checkbox" checked={m.readonly} onChange={(e) => set({ mounts: mounts.map((x, j) => (j === i ? { ...x, readonly: e.target.checked } : x)) })} /> ro
            </label>
            <button type="button" className="icon-btn" onClick={() => set({ mounts: mounts.filter((_, j) => j !== i) })}>
              <X size={14} />
            </button>
          </div>
        ))}
        <button type="button" className="btn btn-sm btn-ghost" onClick={() => set({ mounts: [...mounts, { type: 'volume', source: '', target: '', readonly: false }] })}>
          <Plus size={14} /> {t('Add mount')}
        </button>
      </Pane>
        ) },
        { id: 'net', label: t('Network'), count: value.network_mode ? 1 : networks.length, content: (
      <Pane>
        <label className="field">
          <span>{t('Mode')}</span>
          <select value={value.network_mode || 'networks'} onChange={(e) => set({ network_mode: e.target.value === 'networks' ? '' : e.target.value, networks: e.target.value === 'networks' ? networks : [] })}>
            <option value="networks">{t('Networks (bridge / user-defined)')}</option>
            <option value="host">{t('host (host network)')}</option>
            <option value="none">{t('none (no network)')}</option>
          </select>
        </label>
        {!value.network_mode && (
          <>
            {networks.map((n, i) => (
              <div className="net-row" key={i}>
                <select value={n.name} onChange={(e) => set({ networks: networks.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)) })}>
                  {!nets?.some((x) => x.Name === n.name) && <option value={n.name}>{n.name || '—'}</option>}
                  {(nets || [])
                    .filter((x) => !['host', 'none'].includes(x.Name))
                    .map((x) => (
                      <option key={x.Id} value={x.Name}>
                        {x.Name}
                      </option>
                    ))}
                </select>
                <input className="mono" placeholder={t('aliases (comma)')} value={(n.aliases || []).join(', ')} onChange={(e) => set({ networks: networks.map((x, j) => (j === i ? { ...x, aliases: e.target.value.split(',').map((s) => s.trim()).filter(Boolean) } : x)) })} />
                <input className="mono" placeholder={t('Fixed IP (optional)')} value={n.ipv4} onChange={(e) => set({ networks: networks.map((x, j) => (j === i ? { ...x, ipv4: e.target.value } : x)) })} />
                <button type="button" className="icon-btn" onClick={() => set({ networks: networks.filter((_, j) => j !== i) })}>
                  <X size={14} />
                </button>
              </div>
            ))}
            <button type="button" className="btn btn-sm btn-ghost" onClick={() => set({ networks: [...networks, { name: 'bridge', aliases: [], ipv4: '' }] })}>
              <Plus size={14} /> {t('Connect to network')}
            </button>
          </>
        )}
      </Pane>
        ) },
        { id: 'res', label: t('Resources'), content: (
      <Pane>
        <div className="grid-3">
          <label className="field">
            <span>{t('Max memory (MB)')}</span>
            <input type="number" min={0} value={value.memory ? Math.round(value.memory / 1048576) : ''} placeholder={t('unlimited')} onChange={(e) => set({ memory: Number(e.target.value || 0) * 1048576 })} />
          </label>
          <label className="field">
            <span>CPUs</span>
            <input type="number" min={0} step={0.25} value={value.nano_cpus ? value.nano_cpus / 1e9 : ''} placeholder={t('unlimited')} onChange={(e) => set({ nano_cpus: Math.round(Number(e.target.value || 0) * 1e9) })} />
          </label>
          <label className="field">
            <span>{t('Extra capabilities')}</span>
            <input className="mono" placeholder="NET_ADMIN, SYS_TIME" value={(value.cap_add || []).join(', ')} onChange={(e) => set({ cap_add: e.target.value.split(',').map((s) => s.trim()).filter(Boolean) })} />
          </label>
        </div>
        <div className="checks">
          <label className="check">
            <input type="checkbox" checked={value.privileged} onChange={(e) => set({ privileged: e.target.checked })} /> {t('Privileged')}
          </label>
          <label className="check">
            <input type="checkbox" checked={value.tty} onChange={(e) => set({ tty: e.target.checked })} /> TTY
          </label>
          <label className="check">
            <input type="checkbox" checked={value.open_stdin} onChange={(e) => set({ open_stdin: e.target.checked })} /> {t('stdin open (-i)')}
          </label>
        </div>
      </Pane>
        ) },
        { id: 'labels', label: t('Labels'), count: Object.keys(value.labels || {}).length, content: (
      <Pane>
        <KVEditor rows={Object.entries(value.labels || {})} onChange={(rows) => set({ labels: Object.fromEntries(rows) })} />
      </Pane>
        ) },
        { id: 'hosts', label: t('Hosts'), count: (value.extra_hosts || []).length, content: (
      <Pane>
        <KVEditor rows={(value.extra_hosts || []).map((h) => { const i = h.indexOf(':'); return [h.slice(0, i), h.slice(i + 1)] as [string, string] })} onChange={(rows) => set({ extra_hosts: rows.filter(([k]) => k).map(([k, v]) => `${k}:${v}`) })} keyLabel="host" valueLabel="IP" sep=":" />
      </Pane>
        ) },
      ]}
    />
  )
}

export function ContainerFormDialog({ ep, initial, mode, sourceId, onClose }: { ep: string; initial: Partial<Spec>; mode: 'create' | 'duplicate'; sourceId?: string; onClose: () => void }) {
  const [spec, setSpec] = useState<Spec>(() => emptySpec(initial))
  const [pull, setPull] = useState(mode === 'create')
  const [start, setStart] = useState(true)
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    setBusy(true)
    try {
      const path = mode === 'duplicate' ? `/e/${enc(ep)}/containers/${enc(sourceId!)}/duplicate` : `/e/${enc(ep)}/containers`
      const res = await runJob(`${mode === 'duplicate' ? t('Duplicate') : t('Create')} ${spec.name || spec.image}`, 'POST', path, { spec, pull, start })
      onClose()
      invalidate('/containers')
      if (res?.id) go(ep, 'containers', res.id)
    } catch (e) {
      toast.err(e)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Modal
      wide="xl"
      title={mode === 'duplicate' ? t('Duplicate container') : t('New container')}
      onClose={onClose}
      icon={<List size={18} />}
      footer={
        <>
          <label className="check">
            <input type="checkbox" checked={pull} onChange={(e) => setPull(e.target.checked)} /> {t('Always pull the image')}
          </label>
          <label className="check">
            <input type="checkbox" checked={start} onChange={(e) => setStart(e.target.checked)} /> {t('Start after creating')}
          </label>
          <span className="grow" />
          <button className="btn" onClick={onClose}>
            {t('Cancel')}
          </button>
          <button className="btn btn-primary" disabled={busy || !spec.image} onClick={submit}>
            {mode === 'duplicate' ? t('Create copy') : t('Create container')}
          </button>
        </>
      }
    >
      <ContainerForm ep={ep} value={spec} onChange={setSpec} />
    </Modal>
  )
}
