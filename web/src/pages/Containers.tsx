import { t, tc } from '../i18n'
import { useMemo, useState } from 'react'
import { Box, Plus, Search, LayoutList, Columns3, Layers, X, ExternalLink, HeartPulse, Star, Bookmark } from 'lucide-react'
import { api, enc } from '../lib/api'
import type { Container } from '../lib/types'
import { useResource, invalidate } from '../lib/store'
import { usePref } from '../lib/prefs'
import { ago, cname, exitCode, healthOf, humanStatus, imageLabel, serviceOf, stackOf, uniquePorts, copy } from '../lib/format'
import { go } from '../lib/router'
import { Avatar, DataTable, Empty, MenuButton, Pill, Tabs, Tag, Spinner } from '../ui/common'
import type { Column } from '../ui/common'
import { useMenu } from '../ui/ContextMenu'
import type { MenuEntry } from '../ui/ContextMenu'
import { Drawer, openDialog, prompt, toast } from '../ui/overlays'
import { bulkContainerActions, containerActions, endpointHost, isFav } from '../actions'
import { ContainerFormDialog } from '../components/ContainerForm'
import { ContainerSummary } from './ContainerRecord'

interface View {
  id: string
  label: string
  q?: string
  state?: string[]
  stack?: string
  problems?: boolean
  standalone?: boolean
  favorites?: boolean
}

const BUILTIN_VIEWS: View[] = [
  { id: 'all', label: t('All') },
  { id: 'running', label: t('Running'), state: ['running'] },
  { id: 'stopped', label: tc('pl', 'Stopped'), state: ['exited', 'created', 'dead'] },
  { id: 'problems', label: t('With problems'), problems: true },
  { id: 'standalone', label: t('No stack'), standalone: true },
  { id: 'favorites', label: t('Favorites'), favorites: true },
]

export function hasProblem(c: Container) {
  const code = exitCode(c.Status)
  // 137/143 = stopped by signal, 255 = daemon shutdown or host reboot
  return c.State === 'restarting' || c.State === 'dead' || healthOf(c.Status) === 'unhealthy' || (c.State === 'exited' && code !== null && ![0, 130, 137, 143, 255].includes(code))
}

function matches(c: Container, v: View, ep: string) {
  if (v.state && !v.state.includes(c.State)) return false
  if (v.problems && !hasProblem(c)) return false
  if (v.standalone && stackOf(c)) return false
  if (v.stack && stackOf(c) !== v.stack) return false
  if (v.favorites && !isFav(ep, 'container', cname(c))) return false
  if (v.q) {
    const q = v.q.toLowerCase()
    const hay = `${cname(c)} ${c.Image} ${stackOf(c)} ${c.Id.slice(0, 12)} ${uniquePorts(c).map((p) => p.PublicPort).join(' ')}`.toLowerCase()
    if (!hay.includes(q)) return false
  }
  return true
}

export function PortTags({ ep, c, max = 3 }: { ep: string; c: Container; max?: number }) {
  const menu = useMenu()
  const host = endpointHost(ep)
  const ports = uniquePorts(c)
  return (
    <span className="tags">
      {ports.slice(0, max).map((p) => {
        const url = `${p.PrivatePort === 443 ? 'https' : 'http'}://${host}:${p.PublicPort}`
        return (
          <Tag
            key={`${p.PublicPort}${p.Type}`}
            mono
            onClick={() => window.open(url, '_blank')}
            title={`${p.IP || '0.0.0.0'}:${p.PublicPort} → ${p.PrivatePort}/${p.Type}`}
            onContextMenu={menu(() => [
              { header: `${p.PublicPort} → ${p.PrivatePort}/${p.Type}` },
              { label: t('Open in the browser'), icon: <ExternalLink size={14} />, onClick: () => window.open(url, '_blank') },
              { label: t('Open with https'), onClick: () => window.open(url.replace(/^http:/, 'https:'), '_blank') },
              { label: t('Copy URL'), onClick: () => copy(url) },
              { label: t('Copy host:port'), onClick: () => copy(`${host}:${p.PublicPort}`) },
            ])}
          >
            {p.PublicPort}
            {p.PublicPort !== p.PrivatePort && <span className="muted">→{p.PrivatePort}</span>}
          </Tag>
        )
      })}
      {ports.length > max && <span className="muted small">+{ports.length - max}</span>}
    </span>
  )
}

export function StackTag({ ep, name }: { ep: string; name: string }) {
  const menu = useMenu()
  if (!name) return null
  return (
    <Tag
      tone="stack"
      onClick={() => go(ep, 'stacks', name)}
      onContextMenu={menu(() => [
        { label: t('Open stack {0}', name), icon: <Layers size={14} />, onClick: () => go(ep, 'stacks', name) },
        { label: t('Stack logs'), onClick: () => go(ep, 'stacks', name, 'logs') },
        { label: t('Copy name'), onClick: () => copy(name) },
      ])}
    >
      <Layers size={11} /> {name}
    </Tag>
  )
}

export function ContainerName({ c, sub }: { c: Container; sub?: boolean }) {
  const h = healthOf(c.Status)
  return (
    <span className="cell-name">
      <Avatar name={serviceOf(c) || cname(c)} size={26} />
      <span className="cell-name-text">
        <strong>{cname(c)}</strong>
        {sub && <span className="muted small ellipsis">{imageLabel(c.Image)}</span>}
      </span>
      {h && <HeartPulse size={13} className={`tone-${h === 'healthy' ? 'ok' : h === 'unhealthy' ? 'bad' : 'warn'}`} aria-label={h} />}
    </span>
  )
}

export function Containers({ ep }: { ep: string }) {
  const { data, error, loading } = useResource<Container[]>(`/e/${enc(ep)}/containers`, { poll: 10000 })
  const [viewId, setViewId] = usePref('containers.view', 'all')
  const [custom, setCustom] = usePref<View[]>('containers.views', [])
  const [layout, setLayout] = usePref<'table' | 'kanban'>('containers.layout', 'table')
  const [grouped, setGrouped] = usePref('containers.grouped', true)
  const [q, setQ] = useState('')
  const [sel, setSel] = useState<Set<string>>(new Set())
  const [preview, setPreview] = useState<string | null>(null)
  const menu = useMenu()

  const views = [...BUILTIN_VIEWS, ...custom]
  const view = views.find((v) => v.id === viewId) ?? views[0]
  const all = data || []
  const rows = useMemo(() => all.filter((c) => matches(c, view, ep) && matches(c, { id: '', label: '', q }, ep)), [all, view, q, ep])
  const counts = useMemo(() => Object.fromEntries(views.map((v) => [v.id, all.filter((c) => matches(c, v, ep)).length])), [all, views, ep])
  const selected = rows.filter((c) => sel.has(c.Id))

  const saveView = async () => {
    const label = await prompt({ title: t('Save view'), label: t('View name'), value: q || view.label + ' (' + t('copy') + ')', confirm: t('Save') })
    if (!label) return
    const v: View = { ...view, id: 'v' + Date.now(), label, q: q || view.q }
    setCustom([...custom, v])
    setViewId(v.id)
    setQ('')
  }

  const columns: Column<Container>[] = [
    { key: 'name', label: t('Name'), render: (c) => <ContainerName c={c} />, sort: (c) => cname(c) },
    {
      key: 'state',
      label: t('Status'),
      width: 150,
      render: (c) => <Pill state={c.State} title={c.Status} label={c.State === 'exited' && exitCode(c.Status) ? t('Exited ({0})', exitCode(c.Status)) : undefined} />,
      sort: (c) => c.State,
    },
    ...(grouped ? [] : [{ key: 'stack', label: 'Stack', width: 170, render: (c: Container) => (stackOf(c) ? <StackTag ep={ep} name={stackOf(c)} /> : <span className="muted">—</span>), sort: (c: Container) => stackOf(c) || '~' }]),
    { key: 'image', label: t('Image'), render: (c) => <span className="mono small cell-clip" title={c.Image}>{imageLabel(c.Image)}</span>, sort: (c) => c.Image },
    { key: 'ports', label: t('Ports'), width: 180, render: (c) => <PortTags ep={ep} c={c} /> },
    { key: 'created', label: t('Detailed status'), width: 190, render: (c) => <span className="muted small" title={t('Created {0}', ago(c.Created))}>{humanStatus(c.Status)}</span>, sort: (c) => -c.Created },
    { key: 'menu', label: '', width: 36, render: (c) => <MenuButton items={() => containerActions(ep, c, { preview: () => setPreview(c.Id) })} /> },
  ]

  const newContainer = () => openDialog((close) => <ContainerFormDialog ep={ep} initial={{}} mode="create" onClose={close} />)

  const pageMenu = (): MenuEntry[] => [
    { label: t('New container…'), icon: <Plus size={14} />, onClick: newContainer },
    { label: t('Reload'), onClick: () => invalidate('/containers') },
    'sep',
    { label: t('Table'), checked: layout === 'table', onClick: () => setLayout('table') },
    { label: t('Kanban by state'), checked: layout === 'kanban', onClick: () => setLayout('kanban') },
    { label: t('Group by stack'), checked: grouped, onClick: () => setGrouped(!grouped) },
    'sep',
    { label: t('Save current view…'), icon: <Bookmark size={14} />, onClick: saveView },
    sel.size > 0 && { label: t('Clear selection'), onClick: () => setSel(new Set()) },
    { label: t('Select all visible'), onClick: () => setSel(new Set(rows.map((c) => c.Id))) },
  ]

  return (
    <div className="page" onContextMenu={(e) => (e.target as HTMLElement).closest('.page-body, .page-head') && menu(pageMenu)(e)}>
      <div className="page-head">
        <div className="page-title">
          <Box size={20} />
          <h1>{t('Containers')}</h1>
          <span className="count">{all.length}</span>
          {loading && <Spinner />}
        </div>
        <div className="page-actions">
          <div className="search">
            <Search size={14} />
            <input placeholder={t('Search by name, image, port…')} value={q} onChange={(e) => setQ(e.target.value)} />
            {q && (
              <button className="icon-btn" onClick={() => setQ('')}>
                <X size={13} />
              </button>
            )}
          </div>
          <div className="seg">
            <button className={layout === 'table' ? 'on' : ''} onClick={() => setLayout('table')} title={t('Table')}>
              <LayoutList size={15} />
            </button>
            <button className={layout === 'kanban' ? 'on' : ''} onClick={() => setLayout('kanban')} title="Kanban">
              <Columns3 size={15} />
            </button>
          </div>
          {layout === 'table' && (
            <button className={`btn btn-sm${grouped ? ' on' : ''}`} onClick={() => setGrouped(!grouped)}>
              <Layers size={14} /> {t('Group')}
            </button>
          )}
          <button className="btn btn-primary needs-admin" onClick={newContainer}>
            <Plus size={15} /> {t('New')}
          </button>
        </div>
      </div>
      <Tabs
        active={view.id}
        onChange={setViewId}
        tabs={[
          ...views.map((v) => ({
            id: v.id,
            label: v.favorites ? <><Star size={12} /> {v.label}</> : v.label,
            count: counts[v.id],
            menu: () => [
              { header: v.label },
              { label: t('Open view'), onClick: () => setViewId(v.id) },
              { label: t('Duplicate as new view'), onClick: () => setCustom([...custom, { ...v, id: 'v' + Date.now(), label: v.label + ' (' + t('copy') + ')' }]) },
              custom.some((x) => x.id === v.id) && {
                label: t('Rename…'),
                onClick: async () => {
                  const n = await prompt({ title: t('Rename view'), value: v.label })
                  if (n) setCustom(custom.map((x) => (x.id === v.id ? { ...x, label: n } : x)))
                },
              },
              custom.some((x) => x.id === v.id) && { label: t('Delete view'), danger: true, onClick: () => (setCustom(custom.filter((x) => x.id !== v.id)), setViewId('all')) },
            ],
          })),
        ]}
      />
      {q && (
        <div className="filter-hint">
          {t('Filtering by “{0}”', q)} ·{' '}
          <button className="link" onClick={saveView}>
            {t('save as view')}
          </button>
        </div>
      )}
      {selected.length > 0 && (
        <div className="bulkbar">
          <strong>{selected.length} {t('selected')}</strong>
          {bulkContainerActions(ep, selected)
            .filter((x): x is Exclude<MenuEntry, 'sep' | false | null | undefined> => !!x && x !== 'sep' && 'label' in x)
            .map((a: any) => (
              <button key={a.label} className={`btn btn-sm${a.danger ? ' btn-danger-ghost' : ''}`} onClick={a.onClick}>
                {a.icon} {a.label}
              </button>
            ))}
          <span className="grow" />
          <button className="icon-btn" onClick={() => setSel(new Set())}>
            <X size={15} />
          </button>
        </div>
      )}
      <div className="page-body">
        {error && <div className="alert alert-bad">{error.message}</div>}
        {!data && !error && <div className="center"><Spinner /></div>}
        {data && layout === 'table' && (
          <DataTable
            rows={rows}
            columns={columns}
            rowKey={(c) => c.Id}
            selected={sel}
            setSelected={setSel}
            onPreview={(c) => setPreview(c.Id)}
            onOpen={(c) => go(ep, 'containers', c.Id)}
            rowMenu={(c) => containerActions(ep, c, { preview: () => setPreview(c.Id) })}
            bulkMenu={(list) => bulkContainerActions(ep, list)}
            groupBy={grouped ? (c) => stackOf(c) : undefined}
            groupHeader={(g, rs) => (
              <span className="group-title">
                {g ? <Layers size={14} /> : <Box size={14} />}
                <strong>{g || t('Standalone containers')}</strong>
                <span className="muted small">{rs.filter((c) => c.State === 'running').length}/{rs.length} {t('running')}</span>
              </span>
            )}
            groupMenu={(g, rs) => [
              g && { label: t('Open stack {0}', g), icon: <Layers size={14} />, onClick: () => go(ep, 'stacks', g) },
              g && { label: t('Combined logs'), onClick: () => go(ep, 'stacks', g, 'logs') },
              { label: t('Select the group'), onClick: () => setSel(new Set([...sel, ...rs.map((c) => c.Id)])) },
              'sep',
              ...bulkContainerActions(ep, rs),
            ]}
            rowClass={(c) => (hasProblem(c) ? 'row-problem' : '')}
            defaultSort={{ key: 'name', dir: 1 }}
            empty={<Empty icon={<Box size={28} />} title={t('No containers in this view')} action={<button className="btn btn-primary" onClick={newContainer}><Plus size={15} /> {t('New container')}</button>} />}
          />
        )}
        {data && layout === 'kanban' && <Kanban ep={ep} rows={rows} onPreview={setPreview} />}
      </div>
      {preview && (
        <Drawer onClose={() => setPreview(null)} width={620}>
          <ContainerSummary ep={ep} id={preview} onClose={() => setPreview(null)} />
        </Drawer>
      )}
    </div>
  )
}

const LANES: { id: string; label: string; states: string[]; drop?: string }[] = [
  { id: 'running', label: t('Running'), states: ['running'], drop: 'start' },
  { id: 'paused', label: tc('pl', 'Paused'), states: ['paused'], drop: 'pause' },
  { id: 'restarting', label: t('Restarting'), states: ['restarting'], drop: 'restart' },
  { id: 'stopped', label: tc('pl', 'Stopped'), states: ['exited', 'dead', 'created', 'removing'], drop: 'stop' },
]

function Kanban({ ep, rows, onPreview }: { ep: string; rows: Container[]; onPreview: (id: string) => void }) {
  const menu = useMenu()
  const [over, setOver] = useState<string | null>(null)
  const drop = async (lane: (typeof LANES)[number], id: string) => {
    setOver(null)
    const c = rows.find((x) => x.Id === id)
    if (!c || lane.states.includes(c.State) || !lane.drop) return
    let action = lane.drop
    if (lane.drop === 'start' && c.State === 'paused') action = 'unpause'
    if (lane.drop === 'pause' && c.State !== 'running') return toast.info(t('Only running containers can be paused'))
    try {
      await api.post(`/e/${enc(ep)}/containers/${enc(id)}/action/${action}`)
      toast.ok(`${cname(c)}: ${lane.label.toLowerCase()}`)
    } catch (e) {
      toast.err(e)
    }
    invalidate('/containers', '/stacks')
  }
  return (
    <div className="kanban">
      {LANES.map((lane) => {
        const cards = rows.filter((c) => lane.states.includes(c.State))
        return (
          <div
            key={lane.id}
            className={`lane${over === lane.id ? ' over' : ''}`}
            onDragOver={(e) => (e.preventDefault(), setOver(lane.id))}
            onDragLeave={() => setOver(null)}
            onDrop={(e) => drop(lane, e.dataTransfer.getData('text/container'))}
          >
            <div className="lane-head">
              <Pill state={lane.states[0]} label={lane.label} />
              <span className="count">{cards.length}</span>
            </div>
            <div className="lane-body">
              {cards.map((c) => (
                <div
                  key={c.Id}
                  className={`card${hasProblem(c) ? ' card-problem' : ''}`}
                  draggable
                  onDragStart={(e) => e.dataTransfer.setData('text/container', c.Id)}
                  onClick={() => onPreview(c.Id)}
                  onDoubleClick={() => go(ep, 'containers', c.Id)}
                  onContextMenu={menu(() => containerActions(ep, c, { preview: () => onPreview(c.Id) }))}
                >
                  <ContainerName c={c} sub />
                  <div className="card-meta">
                    <StackTag ep={ep} name={stackOf(c)} />
                    <PortTags ep={ep} c={c} max={2} />
                  </div>
                  <div className="muted small">{humanStatus(c.Status)}</div>
                </div>
              ))}
              {cards.length === 0 && <div className="lane-empty">{t('Drop here to move to')} {lane.label.toLowerCase()}</div>}
            </div>
          </div>
        )
      })}
    </div>
  )
}
