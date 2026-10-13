import { t as tr, tc } from '../i18n'
import { useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Box, HardDrive, Network as NetIcon, Plus, X, AlertTriangle, Copy, Pencil, Trash2, FileCode2, Settings2, ClipboardCopy, Hammer, Check, Undo2 } from 'lucide-react'
import type { Document } from 'yaml'
import * as C from '../lib/composeDoc'
import { copy } from '../lib/format'
import { Avatar, CodeEditor, FormTabs, Tag } from '../ui/common'
import { useMenu } from '../ui/ContextMenu'
import type { MenuEntry } from '../ui/ContextMenu'
import { confirm, prompt, toast } from '../ui/overlays'
import { KVEditor, splitArgs } from './ContainerForm'

type Edit = (fn: (doc: Document) => void) => void

export interface Known {
  services: string[]
  volumes: string[]
  networks: string[]
}

/** Local draft for list editors: incomplete rows stay visible while only valid ones reach the YAML. */
function useRows<T>(fromDoc: T[], valid: (r: T) => boolean, write: (rows: T[]) => void): [T[], (r: T[]) => void] {
  const sig = JSON.stringify(fromDoc)
  const [rows, setRows] = useState(fromDoc)
  useEffect(() => {
    if (JSON.stringify(rows.filter(valid)) !== sig) setRows(fromDoc)
  }, [sig])
  return [rows, (r) => (setRows(r), write(r.filter(valid)))]
}

function Resolved({ value, env }: { value: unknown; env: Record<string, string> }) {
  if (typeof value !== 'string' || !value.includes('$')) return null
  const r = C.resolveVars(value, env)
  return (
    <small className={`resolved${r.missing.length ? ' missing' : ''}`} title={tr('Value with the .env variables')}>
      → {r.value || tr('(empty)')}
      {r.missing.length > 0 && tr(' · undefined: {0}', r.missing.join(', '))}
    </small>
  )
}

function Field({ label, children, hint, span }: { label: ReactNode; children: ReactNode; hint?: ReactNode; span?: boolean }) {
  return (
    <label className={`field${span ? ' span-2' : ''}`}>
      <span>{label}</span>
      {children}
      {hint}
    </label>
  )
}

/** Text input bound to one YAML path. */
function Text({ doc, edit, path, env, ...rest }: { doc: Document; edit: Edit; path: C.Path; env: Record<string, string>; placeholder?: string; mono?: boolean }) {
  const v = C.get(doc, path)
  const val = v === undefined || v === null ? '' : typeof v === 'object' ? JSON.stringify(v) : String(v)
  const [draft, setDraft] = useState(val)
  useEffect(() => setDraft(val), [val])
  return (
    <>
      <input
        className={rest.mono ? 'mono' : ''}
        value={draft}
        placeholder={rest.placeholder}
        disabled={typeof v === 'object' && v !== null}
        onChange={(e) => {
          setDraft(e.target.value)
          edit((d) => C.setValue(d, path, e.target.value))
        }}
      />
      <Resolved value={draft} env={env} />
    </>
  )
}

// ---------- service editor ----------

function ServiceEditor({ doc, edit, name, env, known, tab, setTab }: { doc: Document; edit: Edit; name: string; env: Record<string, string>; known: Known; tab: string; setTab: (t: string) => void }) {
  const base: C.Path = ['services', name]
  const svc = C.get(doc, base) || {}
  const menu = useMenu()
  const at = (...p: C.Path) => [...base, ...p]

  const [ports, setPorts] = useRows(C.readPorts(svc.ports), (r) => !!r.container.trim(), (rows) => edit((d) => C.writePorts(d, at('ports'), rows)))
  const envKV = C.readKV(svc.environment)
  const [envRows, setEnvRows] = useRows(envKV.rows, ([k]) => !!k.trim(), (rows) => edit((d) => C.writeKV(d, at('environment'), { form: envKV.form, rows })))
  const lblKV = C.readKV(svc.labels)
  const [lblRows, setLblRows] = useRows(lblKV.rows, ([k]) => !!k.trim(), (rows) => edit((d) => C.writeKV(d, at('labels'), { form: lblKV.form, rows })))
  const [mounts, setMounts] = useRows(C.readMounts(svc.volumes), (r) => !!r.target.trim(), (rows) => edit((d) => C.writeMounts(d, at('volumes'), rows)))
  const nets = C.readNets(svc.networks)
  const [netRows, setNetRows] = useRows(nets.rows, (r) => !!r.name.trim(), (rows) => edit((d) => C.writeNets(d, at('networks'), nets.form, rows)))
  const deps = C.readDeps(svc.depends_on)
  const envFiles: string[] = Array.isArray(svc.env_file) ? svc.env_file.map((x: any) => (typeof x === 'string' ? x : x.path)) : svc.env_file ? [String(svc.env_file)] : []
  const hosts = C.readKV(Array.isArray(svc.extra_hosts) ? svc.extra_hosts.map((h: string) => h.replace(/[:=]/, '=')) : svc.extra_hosts)
  const [hostRows, setHostRows] = useRows(hosts.rows, ([k]) => !!k.trim(), (rows) => edit((d) => (Array.isArray(svc.extra_hosts) || !svc.extra_hosts ? C.setList(d, at('extra_hosts'), rows.map(([h, ip]) => `${h}:${ip}`)) : C.setMap(d, at('extra_hosts'), rows))))

  const hc = svc.healthcheck || {}
  const hcTest = Array.isArray(hc.test) ? hc.test : hc.test ? ['CMD-SHELL', String(hc.test)] : []
  const hcMode = hcTest[0] ?? 'CMD-SHELL'
  const hcCmd = hcMode === 'CMD' ? C.cmdText(hcTest.slice(1)) : hcTest.slice(1).join(' ')
  const usesDeploy = svc.mem_limit === undefined && svc.cpus === undefined
  const memPath = usesDeploy ? at('deploy', 'resources', 'limits', 'memory') : at('mem_limit')
  const cpuPath = usesDeploy ? at('deploy', 'resources', 'limits', 'cpus') : at('cpus')
  const others = known.services.filter((s) => s !== name)

  const warnings = serviceWarnings(svc, name, known)

  const setCmd = (key: 'command' | 'entrypoint', text: string) =>
    edit((d) => C.setValue(d, at(key), Array.isArray(svc[key]) ? splitArgs(text) ?? [] : text))

  const tabs = [
    {
      id: 'general',
      label: tr('General'),
      warn: warnings.some((w) => w.tab === 'general'),
      content: (
        <div className="grid-2">
          <Field label={tr('Image')}>
            <Text doc={doc} edit={edit} path={at('image')} env={env} mono placeholder={svc.build ? tr('(built)') : 'nginx:alpine'} />
          </Field>
          <Field label={tr('Restart policy')}>
            <select value={svc.restart ?? ''} onChange={(e) => edit((d) => C.setValue(d, at('restart'), e.target.value))}>
              <option value="">(no definido)</option>
              <option value="no">no</option>
              <option value="always">always</option>
              <option value="unless-stopped">unless-stopped</option>
              <option value="on-failure">on-failure</option>
              {svc.restart && !['no', 'always', 'unless-stopped', 'on-failure'].includes(svc.restart) && <option>{svc.restart}</option>}
            </select>
          </Field>
          <Field label={tr('container_name')}>
            <Text doc={doc} edit={edit} path={at('container_name')} env={env} placeholder={tr('(automatic: project-{0}-1)', name)} />
          </Field>
          <Field label={tr('Hostname')}>
            <Text doc={doc} edit={edit} path={at('hostname')} env={env} />
          </Field>
          <Field label={tr('Command')} span hint={Array.isArray(svc.command) ? <small className="help">{tr('list format')}</small> : undefined}>
            <CmdInput value={C.cmdText(svc.command)} onChange={(t) => setCmd('command', t)} placeholder={tr('(from the image)')} />
          </Field>
          <Field label={tr('Entrypoint')}>
            <CmdInput value={C.cmdText(svc.entrypoint)} onChange={(t) => setCmd('entrypoint', t)} placeholder={tr('(from the image)')} />
          </Field>
          <Field label={tr('Working directory')}>
            <Text doc={doc} edit={edit} path={at('working_dir')} env={env} mono />
          </Field>
          <Field label={tr('User')}>
            <Text doc={doc} edit={edit} path={at('user')} env={env} mono />
          </Field>
          <Field label={tr('Platform')}>
            <Text doc={doc} edit={edit} path={at('platform')} env={env} mono placeholder="linux/amd64" />
          </Field>
          <div className="field span-2">
            <span>Build</span>
            {svc.build !== undefined ? (
              <div className="grid-3">
                <Text doc={doc} edit={edit} path={typeof svc.build === 'string' ? at('build') : at('build', 'context')} env={env} mono placeholder={tr('context (.)')} />
                <input
                  className="mono"
                  placeholder="Dockerfile"
                  value={typeof svc.build === 'object' ? svc.build.dockerfile ?? '' : ''}
                  onChange={(e) =>
                    edit((d) => {
                      if (typeof svc.build === 'string') C.setValue(d, at('build'), { context: svc.build })
                      C.setValue(d, at('build', 'dockerfile'), e.target.value)
                    })
                  }
                />
                <button type="button" className="btn btn-sm btn-ghost" onClick={() => edit((d) => C.setValue(d, at('build'), undefined))}>
                  <X size={14} /> {tr('Remove build')}
                </button>
              </div>
            ) : (
              <button type="button" className="btn btn-sm btn-ghost" onClick={() => edit((d) => C.setValue(d, at('build'), { context: '.' }))}>
                <Hammer size={14} /> {tr('Build from a Dockerfile')}
              </button>
            )}
          </div>
          <div className="field span-2">
            <span>{tr('Depends on')}</span>
            <div className="dep-chips">
              {others.length === 0 && <span className="muted small">{tr('No other services')}</span>}
              {others.map((o) => {
                const row = deps.rows.find((r) => r.name === o)
                return (
                  <span key={o} className={`dep-chip${row ? ' on' : ''}`}>
                    <label className="check">
                      <input
                        type="checkbox"
                        checked={!!row}
                        onChange={(e) => edit((d) => C.writeDeps(d, at('depends_on'), deps.form, e.target.checked ? [...deps.rows, { name: o, condition: 'service_started' }] : deps.rows.filter((r) => r.name !== o)))}
                      />
                      {o}
                    </label>
                    {row && (
                      <select value={row.condition} onChange={(e) => edit((d) => C.writeDeps(d, at('depends_on'), 'map', deps.rows.map((r) => (r.name === o ? { ...r, condition: e.target.value } : r))))}>
                        <option value="service_started">{tc('dep', 'started')}</option>
                        <option value="service_healthy">{tr('healthy')}</option>
                        <option value="service_completed_successfully">{tr('completed OK')}</option>
                      </select>
                    )}
                  </span>
                )
              })}
              {deps.rows.filter((r) => !others.includes(r.name)).map((r) => (
                <Tag key={r.name} tone="bad">{r.name} ({tr('does not exist')})</Tag>
              ))}
            </div>
          </div>
        </div>
      ),
    },
    {
      id: 'ports',
      label: tr('Ports'),
      count: ports.filter((p) => p.container).length,
      warn: warnings.some((w) => w.tab === 'ports'),
      content: (
        <div className="rows">
          <p className="help">{tr('host → container. Without a host port, Docker picks a free one. Variables are supported:')} <code>{'${HTTP_PORT:-8080}'}</code>.</p>
          {ports.map((p, i) => (
            <div
              className="port-row"
              key={i}
              onContextMenu={menu(() => [
                { label: tr('Copy'), onClick: () => copy(String(C.formatPort(p))) },
                { label: tr('Duplicate row'), onClick: () => setPorts([...ports.slice(0, i + 1), { ...p, raw: undefined }, ...ports.slice(i + 1)]) },
                'sep',
                { label: tr('Delete'), danger: true, onClick: () => setPorts(ports.filter((_, j) => j !== i)) },
              ])}
            >
              <input className="mono ip" placeholder={tr('IP (all)')} value={p.ip} onChange={(e) => setPorts(ports.map((x, j) => (j === i ? { ...x, ip: e.target.value } : x)))} />
              <div className="col">
                <input className="mono" placeholder="host" value={p.host} onChange={(e) => setPorts(ports.map((x, j) => (j === i ? { ...x, host: e.target.value } : x)))} />
                <Resolved value={p.host} env={env} />
              </div>
              <span className="muted">→</span>
              <input className="mono" placeholder={tr('container')} value={p.container} onChange={(e) => setPorts(ports.map((x, j) => (j === i ? { ...x, container: e.target.value } : x)))} />
              <select value={p.proto} onChange={(e) => setPorts(ports.map((x, j) => (j === i ? { ...x, proto: e.target.value } : x)))}>
                <option>tcp</option>
                <option>udp</option>
                <option>sctp</option>
              </select>
              {p.long && <Tag>largo</Tag>}
              <button type="button" className="icon-btn" onClick={() => setPorts(ports.filter((_, j) => j !== i))}>
                <X size={14} />
              </button>
            </div>
          ))}
          <button type="button" className="btn btn-sm btn-ghost" onClick={() => setPorts([...ports, { ip: '', host: '', container: '', proto: 'tcp' }])}>
            <Plus size={14} /> {tr('Publish port')}
          </button>
          {warnings.filter((w) => w.tab === 'ports').map((w, i) => <div key={i} className="alert alert-warn"><AlertTriangle size={14} /> {w.text}</div>)}
        </div>
      ),
    },
    {
      id: 'env',
      label: tr('Environment'),
      count: envRows.filter(([k]) => k).length,
      content: (
        <div className="rows">
          <p className="help">{tr('Format in the YAML:')} <strong>{envKV.form === 'list' ? tr('list (- KEY=value)') : tr('map (KEY: value)')}</strong>{tr('; kept when saving.')}</p>
          <KVEditor rows={envRows} onChange={setEnvRows} keyLabel={tr('NAME')} />
          <div className="field">
            <span>{tr('env_file files')}</span>
            <StringList
              items={envFiles}
              placeholder=".env.app"
              onChange={(items) => edit((d) => (Array.isArray(svc.env_file) || items.length !== 1 ? C.setList(d, at('env_file'), items) : C.setValue(d, at('env_file'), items[0])))}
            />
          </div>
        </div>
      ),
    },
    {
      id: 'storage',
      label: tr('Storage'),
      count: mounts.filter((m) => m.target).length,
      warn: warnings.some((w) => w.tab === 'storage'),
      content: (
        <div className="rows">
          <datalist id={`vols-${name}`}>
            {known.volumes.map((v) => <option key={v} value={v} />)}
          </datalist>
          {mounts.map((m, i) => {
            const undeclared = m.type === 'volume' && m.source && !m.source.includes('$') && !known.volumes.includes(m.source)
            return (
              <div
                key={i}
                className="mount-row"
                onContextMenu={menu(() => [
                  { label: tr('Copy'), onClick: () => copy(String(C.formatMount(m))) },
                  undeclared && { label: tr('Declare volume “{0}”', m.source), onClick: () => edit((d) => C.declare(d, 'volumes', m.source)) },
                  'sep',
                  { label: tr('Delete'), danger: true, onClick: () => setMounts(mounts.filter((_, j) => j !== i)) },
                ])}
              >
                <select value={m.type} onChange={(e) => setMounts(mounts.map((x, j) => (j === i ? { ...x, type: e.target.value as C.MountRow['type'], long: x.long || e.target.value === 'tmpfs' } : x)))}>
                  <option value="volume">{tr('volume')}</option>
                  <option value="bind">{tr('bind (path)')}</option>
                  <option value="tmpfs">tmpfs</option>
                </select>
                <div className="col">
                  <input className="mono" list={m.type === 'volume' ? `vols-${name}` : undefined} disabled={m.type === 'tmpfs'} placeholder={m.type === 'bind' ? './data' : m.type === 'tmpfs' ? '—' : tr('name (empty = anonymous)')} value={m.source} onChange={(e) => setMounts(mounts.map((x, j) => (j === i ? { ...x, source: e.target.value } : x)))} />
                  <Resolved value={m.source} env={env} />
                  {undeclared && (
                    <small className="resolved missing">
                      {tr('not declared')} ·{' '}
                      <button type="button" className="link small" onClick={() => edit((d) => C.declare(d, 'volumes', m.source))}>
                        {tr('declare')}
                      </button>
                    </small>
                  )}
                </div>
                <span className="muted">→</span>
                <input className="mono" placeholder={tr('/path/in/container')} value={m.target} onChange={(e) => setMounts(mounts.map((x, j) => (j === i ? { ...x, target: e.target.value } : x)))} />
                <label className="check" title={tr('Read-only')}>
                  <input type="checkbox" checked={m.ro} onChange={(e) => setMounts(mounts.map((x, j) => (j === i ? { ...x, ro: e.target.checked } : x)))} /> ro
                </label>
                <button type="button" className="icon-btn" onClick={() => setMounts(mounts.filter((_, j) => j !== i))}>
                  <X size={14} />
                </button>
              </div>
            )
          })}
          <button type="button" className="btn btn-sm btn-ghost" onClick={() => setMounts([...mounts, { type: 'volume', source: '', target: '', ro: false, mode: '' }])}>
            <Plus size={14} /> {tr('Add mount')}
          </button>
        </div>
      ),
    },
    {
      id: 'net',
      label: tr('Network'),
      count: netRows.length,
      warn: warnings.some((w) => w.tab === 'net'),
      content: (
        <div className="rows">
          <Field label={tr('network_mode')}>
            <select value={svc.network_mode ?? ''} onChange={(e) => edit((d) => C.setValue(d, at('network_mode'), e.target.value))}>
              <option value="">{tr('(project networks)')}</option>
              <option value="host">host</option>
              <option value="none">none</option>
              {others.map((o) => <option key={o} value={`service:${o}`}>service:{o}</option>)}
              {svc.network_mode && !['host', 'none'].includes(svc.network_mode) && !svc.network_mode.startsWith('service:') && <option>{svc.network_mode}</option>}
            </select>
          </Field>
          {!svc.network_mode && (
            <>
              <datalist id={`nets-${name}`}>
                {['default', ...known.networks].map((n) => <option key={n} value={n} />)}
              </datalist>
              {netRows.length === 0 && <p className="help">{tr('No explicit networks: the service uses the project\'s')} <code>default</code> {tr('network.')}</p>}
              {netRows.map((n, i) => (
                <div className="net-row" key={i}>
                  <input className="mono" list={`nets-${name}`} placeholder={tr('network')} value={n.name} onChange={(e) => setNetRows(netRows.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))} />
                  <input className="mono" placeholder={tr('aliases (comma)')} value={n.aliases.join(', ')} onChange={(e) => setNetRows(netRows.map((x, j) => (j === i ? { ...x, aliases: e.target.value.split(',').map((s) => s.trim()).filter(Boolean) } : x)))} />
                  <input className="mono" placeholder={tr('Fixed IPv4')} value={n.ipv4} onChange={(e) => setNetRows(netRows.map((x, j) => (j === i ? { ...x, ipv4: e.target.value } : x)))} />
                  {n.name && n.name !== 'default' && !known.networks.includes(n.name) && <Tag tone="bad">{tc('f', 'not declared')}</Tag>}
                  <button type="button" className="icon-btn" onClick={() => setNetRows(netRows.filter((_, j) => j !== i))}>
                    <X size={14} />
                  </button>
                </div>
              ))}
              <button type="button" className="btn btn-sm btn-ghost" onClick={() => setNetRows([...netRows, { name: '', aliases: [], ipv4: '' }])}>
                <Plus size={14} /> {tr('Connect to network')}
              </button>
            </>
          )}
          <div className="field">
            <span>{tr('Extra hosts')}</span>
            <KVEditor rows={hostRows} onChange={setHostRows} keyLabel="host" valueLabel="IP" sep=":" />
          </div>
        </div>
      ),
    },
    {
      id: 'health',
      label: tr('Health'),
      warn: false,
      content: (
        <div className="rows">
          {hc.disable ? (
            <div className="alert">
              {tr('Healthcheck disabled.')}{' '}
              <button type="button" className="link" onClick={() => edit((d) => C.setValue(d, at('healthcheck', 'disable'), undefined))}>
                {tr('Enable')}
              </button>
            </div>
          ) : (
            <div className="grid-2">
              <Field label={tr('Test')} span>
                <div className="row gap">
                  <select
                    value={hcMode}
                    onChange={(e) => edit((d) => C.setValue(d, at('healthcheck', 'test'), hcCmd ? (e.target.value === 'CMD' ? ['CMD', ...(splitArgs(hcCmd) ?? [])] : ['CMD-SHELL', hcCmd]) : undefined))}
                  >
                    <option value="CMD-SHELL">CMD-SHELL</option>
                    <option value="CMD">CMD</option>
                  </select>
                  <CmdInput
                    value={hcCmd}
                    placeholder="curl -fs http://localhost/ || exit 1"
                    onChange={(t) => edit((d) => C.setValue(d, at('healthcheck', 'test'), t ? (hcMode === 'CMD' ? ['CMD', ...(splitArgs(t) ?? [])] : typeof hc.test === 'string' ? t : ['CMD-SHELL', t]) : undefined))}
                  />
                </div>
              </Field>
              {(['interval', 'timeout', 'start_period', 'retries'] as const).map((k) => (
                <Field key={k} label={{ interval: tr('Interval'), timeout: tr('Timeout'), start_period: tr('Start period'), retries: tr('Retries') }[k]}>
                  <Text doc={doc} edit={edit} path={at('healthcheck', k)} env={env} mono placeholder={k === 'retries' ? '3' : '30s'} />
                </Field>
              ))}
              <div className="span-2">
                <button type="button" className="btn btn-sm btn-ghost" onClick={() => edit((d) => C.setValue(d, at('healthcheck'), { disable: true }))}>
                  {tr('Disable the image healthcheck')}
                </button>
              </div>
            </div>
          )}
        </div>
      ),
    },
    {
      id: 'resources',
      label: tr('Resources'),
      content: (
        <div className="grid-3">
          <Field label={usesDeploy ? tr('Memory (deploy.resources)') : 'mem_limit'}>
            <Text doc={doc} edit={edit} path={memPath} env={env} mono placeholder="512m" />
          </Field>
          <Field label={usesDeploy ? 'CPUs (deploy.resources)' : 'cpus'}>
            <Text doc={doc} edit={edit} path={cpuPath} env={env} mono placeholder="1.5" />
          </Field>
          <Field label={tr('shm_size')}>
            <Text doc={doc} edit={edit} path={at('shm_size')} env={env} mono placeholder="64m" />
          </Field>
          <Field label={tr('Extra capabilities (cap_add)')} span>
            <input className="mono" value={(svc.cap_add || []).join(', ')} placeholder="NET_ADMIN, SYS_TIME" onChange={(e) => edit((d) => C.setList(d, at('cap_add'), e.target.value.split(',').map((s) => s.trim()).filter(Boolean)))} />
          </Field>
          <div className="checks span-2">
            {(['privileged', 'init', 'tty', 'stdin_open', 'read_only'] as const).map((k) => (
              <label key={k} className="check">
                <input type="checkbox" checked={!!svc[k]} onChange={(e) => edit((d) => C.setValue(d, at(k), e.target.checked ? true : undefined))} /> {k}
              </label>
            ))}
          </div>
        </div>
      ),
    },
    {
      id: 'labels',
      label: 'Labels',
      count: lblRows.filter(([k]) => k).length,
      content: <KVEditor rows={lblRows} onChange={setLblRows} />,
    },
    {
      id: 'yaml',
      label: <><FileCode2 size={13} /> YAML</>,
      content: <IsolatedYAML key={name} doc={doc} edit={edit} path={base} />,
    },
  ]

  return (
    <div className="svc-editor">
      {warnings.length > 0 && (
        <div className="alert alert-warn">
          <AlertTriangle size={14} />
          <span>{warnings.map((w) => w.text).join(' · ')}</span>
        </div>
      )}
      <FormTabs tabs={tabs} active={tab} onChange={setTab} />
    </div>
  )
}

function CmdInput({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  const [draft, setDraft] = useState(value)
  useEffect(() => setDraft(value), [value])
  return <input className="mono grow" value={draft} placeholder={placeholder} onChange={(e) => (setDraft(e.target.value), onChange(e.target.value))} />
}

function StringList({ items, onChange, placeholder }: { items: string[]; onChange: (v: string[]) => void; placeholder?: string }) {
  const [rows, setRows] = useRows(items, (s) => !!s.trim(), onChange)
  return (
    <div className="rows">
      {rows.map((v, i) => (
        <div key={i} className="row gap">
          <input className="mono grow" value={v} placeholder={placeholder} onChange={(e) => setRows(rows.map((x, j) => (j === i ? e.target.value : x)))} />
          <button type="button" className="icon-btn" onClick={() => setRows(rows.filter((_, j) => j !== i))}>
            <X size={14} />
          </button>
        </div>
      ))}
      <button type="button" className="btn btn-sm btn-ghost" onClick={() => setRows([...rows, ''])}>
        <Plus size={14} /> {tr('Add')}
      </button>
    </div>
  )
}

/** Edit one node's YAML on its own and splice it back into the file. */
function IsolatedYAML({ doc, edit, path }: { doc: Document; edit: Edit; path: C.Path }) {
  const text = C.nodeText(doc, path)
  const [draft, setDraft] = useState(text)
  const [err, setErr] = useState('')
  useEffect(() => setDraft(text), [text])
  return (
    <div className="rows">
      <p className="help">{tr('Only this service. Whatever the form does not cover (logging, ulimits, secrets…) is edited here; applying inserts it into the file.')}</p>
      <CodeEditor value={draft} onChange={(v) => (setDraft(v), setErr(''))} minRows={16} />
      {err && <div className="alert alert-bad">{err}</div>}
      <div className="row-actions">
        <button
          type="button"
          className="btn btn-sm btn-primary"
          disabled={draft === text}
          onClick={() =>
            edit((d) => {
              const e = C.replaceNode(d, path, draft)
              if (e) setErr(e)
            })
          }
        >
          <Check size={14} /> {tr('Apply to the file')}
        </button>
        <button type="button" className="btn btn-sm" disabled={draft === text} onClick={() => setDraft(text)}>
          <Undo2 size={14} /> {tr('Discard')}
        </button>
      </div>
    </div>
  )
}

interface Warning {
  tab: string
  text: string
}

function serviceWarnings(svc: any, name: string, known: Known): Warning[] {
  const w: Warning[] = []
  if (!svc.image && !svc.build && !svc.extends) w.push({ tab: 'general', text: tr('no image or build') })
  for (const d of C.readDeps(svc.depends_on).rows) if (!known.services.includes(d.name)) w.push({ tab: 'general', text: tr('depends on “{0}”, which does not exist', d.name) })
  for (const m of C.readMounts(svc.volumes))
    if (m.type === 'volume' && m.source && !m.source.includes('$') && !known.volumes.includes(m.source)) w.push({ tab: 'storage', text: tr('volume “{0}” not declared', m.source) })
  for (const n of C.readNets(svc.networks).rows) if (n.name !== 'default' && !known.networks.includes(n.name)) w.push({ tab: 'net', text: tr('network “{0}” not declared', n.name) })
  void name
  return w
}

function hostPorts(svc: any): string[] {
  return C.readPorts(svc?.ports).filter((p) => p.host && !p.host.includes('$')).map((p) => `${p.ip || '*'}:${p.host}/${p.proto}`)
}

// ---------- top-level volumes & networks ----------

function TopLevel({ doc, edit, kind, env }: { doc: Document; edit: Edit; kind: 'volumes' | 'networks'; env: Record<string, string> }) {
  const raw = C.get(doc, [kind]) || {}
  const fromDoc = Object.entries(raw).map(([name, c]: [string, any]) => ({ name, driver: c?.driver ?? '', external: !!c?.external, real: c?.name ?? '' }))
  const toEntries = (rows: typeof fromDoc) =>
    rows.map((r) => {
      const prev = raw[r.name] && typeof raw[r.name] === 'object' ? { ...raw[r.name] } : {}
      delete prev.driver
      delete prev.external
      delete prev.name
      const c: Record<string, unknown> = { ...prev }
      if (r.real) c.name = r.real
      if (r.external) c.external = true
      if (r.driver) c.driver = r.driver
      return [r.name, Object.keys(c).length ? c : null] as [string, unknown]
    })
  const [rows, setRows] = useRows(fromDoc, (r) => !!r.name.trim(), (rs) => edit((d) => C.setMap(d, [kind], toEntries(rs))))
  const users = (n: string) =>
    C.services(doc).filter((s) => {
      const svc = C.get(doc, ['services', s])
      return kind === 'volumes' ? C.readMounts(svc?.volumes).some((m) => m.source === n) : C.readNets(svc?.networks).rows.some((r) => r.name === n)
    })
  return (
    <div className="rows">
      <p className="help">
        {kind === 'volumes' ? tr('Named volumes of the project. Mark “external” if it already exists and compose must not create it.') : tr('Project networks. “external” = it already exists outside compose (e.g. a shared proxy\'s).')}
      </p>
      <div className="table-wrap">
        <table className="table compact">
          <thead>
            <tr>
              <th>{tr('Key')}</th>
              <th>{tr('Actual name')}</th>
              <th>Driver</th>
              <th>{tr('External')}</th>
              <th>{tr('Used by')}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {rows.map((r, i) => (
              <tr key={i}>
                <td><input className="mono" value={r.name} onChange={(e) => setRows(rows.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))} /></td>
                <td><input className="mono" placeholder={tr('(project_key)')} value={r.real} onChange={(e) => setRows(rows.map((x, j) => (j === i ? { ...x, real: e.target.value } : x)))} /><Resolved value={r.real} env={env} /></td>
                <td><input className="mono" placeholder={kind === 'volumes' ? 'local' : 'bridge'} value={r.driver} onChange={(e) => setRows(rows.map((x, j) => (j === i ? { ...x, driver: e.target.value } : x)))} /></td>
                <td><input type="checkbox" checked={r.external} onChange={(e) => setRows(rows.map((x, j) => (j === i ? { ...x, external: e.target.checked } : x)))} /></td>
                <td><span className="tags">{users(r.name).map((u) => <Tag key={u}>{u}</Tag>)}</span></td>
                <td><button type="button" className="icon-btn" onClick={() => setRows(rows.filter((_, j) => j !== i))}><X size={14} /></button></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <button type="button" className="btn btn-sm btn-ghost" onClick={() => setRows([...rows, { name: '', driver: '', external: false, real: '' }])}>
        <Plus size={14} /> {kind === 'volumes' ? tr('Declare volume') : tr('Declare network')}
      </button>
    </div>
  )
}

// ---------- main ----------

type Sel = { kind: 'service'; name: string } | { kind: 'volumes' } | { kind: 'networks' } | { kind: 'project' }

export function ComposeVisual({ text, onChange, envText, known: extraKnown, readOnly }: { text: string; onChange: (t: string) => void; envText: string; known?: Known; readOnly?: boolean }) {
  const parsed = useMemo(() => C.parse(text), [text])
  const latest = useRef(text)
  latest.current = text
  const env = useMemo(() => C.envMap(envText), [envText])
  const [sel, setSel] = useState<Sel>({ kind: 'project' })
  const [tab, setTab] = useState('general')
  const menu = useMenu()
  const doc = parsed.doc
  const services = doc ? C.services(doc) : []
  useEffect(() => {
    if (sel.kind === 'project' && services.length) setSel({ kind: 'service', name: services[0] })
    if (sel.kind === 'service' && !services.includes(sel.name)) setSel(services.length ? { kind: 'service', name: services[0] } : { kind: 'project' })
  }, [services.join(',')])

  if (!doc) return <div className="alert alert-bad">{tr('The YAML has errors and cannot be shown in visual mode:')} {parsed.error}{tr('. Fix it in the YAML view.')}</div>

  const edit: Edit = (fn) => {
    if (readOnly) return
    const fresh = C.parse(latest.current).doc
    if (!fresh) return
    fn(fresh)
    const out = fresh.toString()
    if (out === latest.current) return
    latest.current = out
    onChange(out)
  }
  const known: Known = {
    services: [...new Set([...services, ...(extraKnown?.services ?? [])])],
    volumes: [...new Set([...C.topNames(doc, 'volumes'), ...(extraKnown?.volumes ?? [])])],
    networks: [...new Set([...C.topNames(doc, 'networks'), ...(extraKnown?.networks ?? [])])],
  }
  // host ports published by more than one service
  const portOwners = new Map<string, string[]>()
  services.forEach((s) => hostPorts(C.get(doc, ['services', s])).forEach((p) => portOwners.set(p, [...(portOwners.get(p) ?? []), s])))
  const clashes = [...portOwners.entries()].filter(([, o]) => o.length > 1)

  const newName = async (title: string, value = '') => {
    const n = await prompt({ title, label: tr('Service name'), value })
    if (!n) return null
    const clean = n.trim().replace(/\s+/g, '-')
    if (services.includes(clean)) {
      toast.err(tr('A service “{0}” already exists', clean))
      return null
    }
    return clean
  }
  const addService = async () => {
    const n = await newName(tr('New service'))
    if (!n) return
    edit((d) => C.addService(d, n, { image: '', restart: 'unless-stopped' }))
    setSel({ kind: 'service', name: n })
    setTab('general')
  }
  const svcMenu = (s: string): MenuEntry[] => [
    { header: s },
    { label: tr('Edit'), icon: <Pencil size={14} />, onClick: () => setSel({ kind: 'service', name: s }) },
    { label: tr('View service YAML'), icon: <FileCode2 size={14} />, onClick: () => (setSel({ kind: 'service', name: s }), setTab('yaml')) },
    'sep',
    {
      label: tr('Duplicate…'),
      icon: <Copy size={14} />,
      onClick: async () => {
        const n = await newName(tr('Duplicate service'), `${s}-copy`)
        if (n) (edit((d) => C.duplicateService(d, s, n)), setSel({ kind: 'service', name: n }))
      },
    },
    {
      label: tr('Rename…'),
      icon: <Pencil size={14} />,
      onClick: async () => {
        const n = await newName(tr('Rename service'), s)
        if (n && n !== s) (edit((d) => C.renameService(d, s, n)), setSel({ kind: 'service', name: n }))
      },
    },
    { label: tr('Copy name'), icon: <ClipboardCopy size={14} />, onClick: () => copy(s) },
    'sep',
    {
      label: tr('Delete service…'),
      icon: <Trash2 size={14} />,
      danger: true,
      onClick: async () => {
        if (await confirm({ title: tr('Delete service {0}', s), body: tr('It is removed from the YAML (and from any depends_on naming it). Nothing changes in Docker until you save and deploy.'), danger: true, confirm: tr('Delete') }))
          edit((d) => C.removeService(d, s))
      },
    },
  ]

  return (
    <div className={`compose-visual${readOnly ? ' readonly' : ''}`}>
      <aside className="cv-side" onContextMenu={menu(() => [{ label: tr('New service…'), icon: <Plus size={14} />, onClick: addService }])}>
        <button className={`cv-item${sel.kind === 'project' ? ' active' : ''}`} onClick={() => setSel({ kind: 'project' })}>
          <Settings2 size={15} /> <span className="grow">{tr('Project')}</span>
        </button>
        <div className="nav-title">
          <Box size={12} /> {tr('Services')} <span className="grow" />
          <button className="icon-btn" title={tr('New service')} onClick={addService}>
            <Plus size={13} />
          </button>
        </div>
        {services.map((s) => {
          const svc = C.get(doc, ['services', s]) || {}
          const warn = serviceWarnings(svc, s, known).length > 0 || clashes.some(([, o]) => o.includes(s))
          const ports = C.readPorts(svc.ports).filter((p) => p.host)
          return (
            <button key={s} className={`cv-item${sel.kind === 'service' && sel.name === s ? ' active' : ''}`} onClick={() => setSel({ kind: 'service', name: s })} onContextMenu={menu(() => svcMenu(s))}>
              <Avatar name={s} size={24} />
              <span className="cv-item-text">
                <strong>{s}</strong>
                <span className="muted small ellipsis">{svc.image || (svc.build ? 'build' : '—')}</span>
              </span>
              {ports.length > 0 && <span className="muted small mono">{C.resolveVars(ports[0].host, env).value}</span>}
              {warn && <AlertTriangle size={13} className="tone-warn" />}
            </button>
          )
        })}
        {services.length === 0 && <p className="muted small">{tr('No services in this file.')}</p>}
        <div className="nav-title"><HardDrive size={12} /> {tr('Resources')}</div>
        <button className={`cv-item${sel.kind === 'volumes' ? ' active' : ''}`} onClick={() => setSel({ kind: 'volumes' })}>
          <HardDrive size={15} /> <span className="grow">{tr('Volumes')}</span> <span className="muted small">{C.topNames(doc, 'volumes').length}</span>
        </button>
        <button className={`cv-item${sel.kind === 'networks' ? ' active' : ''}`} onClick={() => setSel({ kind: 'networks' })}>
          <NetIcon size={15} /> <span className="grow">{tr('Networks')}</span> <span className="muted small">{C.topNames(doc, 'networks').length}</span>
        </button>
      </aside>
      <section className="cv-main">
        {readOnly && <div className="alert">{tr('Read-only.')}</div>}
        {sel.kind === 'service' && services.includes(sel.name) && (
          <>
            <div className="cv-head">
              <Avatar name={sel.name} size={32} square />
              <h3 className="grow">{sel.name}</h3>
              <button className="icon-btn" onClick={(e) => menu(() => svcMenu(sel.name))(e)} title={tr('Actions')}>
                <Settings2 size={15} />
              </button>
            </div>
            {clashes.filter(([, o]) => o.includes(sel.name)).map(([p, o]) => (
              <div key={p} className="alert alert-warn"><AlertTriangle size={14} /> {tr('Port {0} is also published by {1}', p.split('/')[0].replace('*:', ''), o.filter((x) => x !== sel.name).join(', '))}</div>
            ))}
            <ServiceEditor key={sel.name} doc={doc} edit={edit} name={sel.name} env={env} known={known} tab={tab} setTab={setTab} />
          </>
        )}
        {sel.kind === 'volumes' && <TopLevel doc={doc} edit={edit} kind="volumes" env={env} />}
        {sel.kind === 'networks' && <TopLevel doc={doc} edit={edit} kind="networks" env={env} />}
        {sel.kind === 'project' && (
          <div className="rows">
            <div className="grid-2">
              <Field label={tr('Project name (name:)')}>
                <Text doc={doc} edit={edit} path={['name']} env={env} placeholder={tr('(the directory\'s or -p)')} />
              </Field>
            </div>
            <div className="kpis small-kpis">
              <div className="kpi"><span className="kpi-label">{tr('Services')}</span><span className="kpi-value">{services.length}</span></div>
              <div className="kpi"><span className="kpi-label">{tr('Volumes')}</span><span className="kpi-value">{C.topNames(doc, 'volumes').length}</span></div>
              <div className="kpi"><span className="kpi-label">{tr('Networks')}</span><span className="kpi-value">{C.topNames(doc, 'networks').length}</span></div>
              <div className="kpi"><span className="kpi-label">{tr('.env variables')}</span><span className="kpi-value">{Object.keys(env).length}</span></div>
            </div>
            {clashes.length > 0 && clashes.map(([p, o]) => <div key={p} className="alert alert-warn"><AlertTriangle size={14} /> {tr('Port {0} published by {1}', p.split('/')[0].replace('*:', ''), o.join(', '))}</div>)}
            <button className="btn btn-sm" onClick={addService}><Plus size={14} /> {tr('New service')}</button>
          </div>
        )}
      </section>
    </div>
  )
}
