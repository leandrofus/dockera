import { t as tr } from '../i18n'
import { useEffect, useState } from 'react'
import { Box, Layers, Image as ImageIcon, HardDrive, Network as NetIcon, AlertTriangle, Star, Activity as ActIcon, Server, Trash2, Search, Pause, Play, Cpu, MemoryStick } from 'lucide-react'
import { api, enc, stream } from '../lib/api'
import type { Container, DockerEvent, Endpoint, Image, Stack } from '../lib/types'
import { useResource, invalidate } from '../lib/store'
import { usePref } from '../lib/prefs'
import { ago, bytes, cname, copy, dateTime, exitCode, humanStatus } from '../lib/format'
import { go, href } from '../lib/router'
import { Empty, Pill, Spinner, Tag } from '../ui/common'
import { useMenu } from '../ui/ContextMenu'
import { Modal, openDialog, toast } from '../ui/overlays'
import { containerActions } from '../actions'
import type { Fav } from '../actions'
import { hasProblem, ContainerName } from './Containers'
import { eventLabel } from './ContainerRecord'

function Kpi({ icon, label, value, sub, onClick, tone }: { icon: React.ReactNode; label: string; value: React.ReactNode; sub?: React.ReactNode; onClick?: () => void; tone?: string }) {
  return (
    <button className={`kpi${tone ? ' kpi-' + tone : ''}`} onClick={onClick}>
      <span className="kpi-icon">{icon}</span>
      <span className="kpi-label">{label}</span>
      <span className="kpi-value">{value}</span>
      {sub && <span className="kpi-sub">{sub}</span>}
    </button>
  )
}

function PruneDialog({ ep, onClose }: { ep: string; onClose: () => void }) {
  const [o, setO] = useState({ containers: true, images: true, all_images: false, networks: true, volumes: false, build_cache: true })
  const opts: [keyof typeof o, string][] = [
    ['containers', tr('Stopped containers')],
    ['images', tr('Untagged images')],
    ['all_images', tr('…and all unused images')],
    ['networks', tr('Unused networks')],
    ['volumes', tr('Unused volumes (deletes data!)')],
    ['build_cache', tr('Build cache')],
  ]
  return (
    <Modal title={tr('Clean up system')} icon={<Trash2 size={18} />} onClose={onClose} footer={<>
      <button className="btn" onClick={onClose}>{tr('Cancel')}</button>
      <button className="btn btn-danger" onClick={() => {
        // runs in the background: progress and result stay in the tasks tray
        onClose()
        api
          .post(`/e/${enc(ep)}/prune`, o)
          .then((r) => {
            const freed = Object.values(r as Record<string, any>).reduce((a, x) => a + (x?.SpaceReclaimed || 0), 0)
            toast.ok(tr('Cleanup done: {0} reclaimed', bytes(freed)))
          })
          .catch((e) => toast.err(e))
          .finally(() => invalidate())
      }}>{tr('Clear')}</button>
    </>}>
      <div className="checks col">
        {opts.map(([k, l]) => (
          <label key={k} className="check"><input type="checkbox" checked={o[k]} disabled={k === 'all_images' && !o.images} onChange={(e) => setO({ ...o, [k]: e.target.checked })} /> {l}</label>
        ))}
      </div>
    </Modal>
  )
}

export function Home({ ep }: { ep: string }) {
  const { data: info } = useResource<{ info: any; version: any }>(`/e/${enc(ep)}/info`, { poll: 30000 })
  const { data: cts } = useResource<Container[]>(`/e/${enc(ep)}/containers`, { poll: 10000 })
  const { data: stacks } = useResource<Stack[]>(`/e/${enc(ep)}/stacks`)
  const { data: images } = useResource<Image[]>(`/e/${enc(ep)}/images`)
  const { data: vols } = useResource<any[]>(`/e/${enc(ep)}/volumes`)
  const { data: nets } = useResource<any[]>(`/e/${enc(ep)}/networks`)
  const { data: df } = useResource<any>(`/e/${enc(ep)}/df`)
  const { data: endpoints } = useResource<Endpoint[]>(`/endpoints`)
  const [favs] = usePref<Fav[]>('favorites', [])
  const menu = useMenu()

  const problems = (cts || []).filter(hasProblem)
  const running = (cts || []).filter((c) => c.State === 'running').length
  const i = info?.info
  const myFavs = favs.filter((f) => f.ep === ep)

  const disk = df && [
    [tr('Images'), df.LayersSize || 0, (df.Images || []).filter((x: any) => x.Containers === 0).reduce((a: number, x: any) => a + x.Size, 0)],
    [tr('Containers'), (df.Containers || []).reduce((a: number, x: any) => a + (x.SizeRw || 0), 0), 0],
    [tr('Volumes'), (df.Volumes || []).reduce((a: number, x: any) => a + Math.max(0, x.UsageData?.Size || 0), 0), 0],
    [tr('Build cache'), (df.BuildCache || []).reduce((a: number, x: any) => a + x.Size, 0), (df.BuildCache || []).filter((x: any) => !x.InUse).reduce((a: number, x: any) => a + x.Size, 0)],
  ] as [string, number, number][]
  const diskMax = disk ? Math.max(1, ...disk.map((d: [string, number, number]) => d[1])) : 1

  return (
    <div className="page home">
      <div className="home-hero">
        <div>
          <h1>{ep}</h1>
          {i ? (
            <p className="muted">
              {i.OperatingSystem} · Docker {i.ServerVersion} · <Cpu size={13} /> {i.NCPU} CPU · <MemoryStick size={13} /> {bytes(i.MemTotal)} · {i.Architecture}
            </p>
          ) : <Spinner />}
        </div>
        <button className="btn" onClick={() => openDialog((c) => <PruneDialog ep={ep} onClose={c} />)}><Trash2 size={14} /> {tr('Clean up system')}</button>
      </div>

      <div className="kpis">
        <Kpi icon={<Box size={16} />} label={tr('Containers')} value={cts ? <>{running}<small>/{cts.length}</small></> : '—'} sub={tr('running')} onClick={() => go(ep, 'containers')} />
        <Kpi icon={<AlertTriangle size={16} />} label={tr('Need attention')} value={problems.length} tone={problems.length ? 'bad' : 'ok'} sub={problems.length ? tr('failing') : tr('all good')} onClick={() => go(ep, 'containers')} />
        <Kpi icon={<Layers size={16} />} label="Stacks" value={stacks?.length ?? '—'} sub={stacks ? tr('{0} active', stacks.filter((s) => s.status === 'running').length) : ''} onClick={() => go(ep, 'stacks')} />
        <Kpi icon={<ImageIcon size={16} />} label={tr('Images')} value={images?.length ?? '—'} sub={images ? bytes(images.reduce((a, x) => a + x.Size, 0)) : ''} onClick={() => go(ep, 'images')} />
        <Kpi icon={<HardDrive size={16} />} label={tr('Volumes')} value={vols?.length ?? '—'} sub={vols ? tr('{0} orphaned', vols.filter((v) => !v.UsedBy.length).length) : ''} onClick={() => go(ep, 'volumes')} />
        <Kpi icon={<NetIcon size={16} />} label={tr('Networks')} value={nets?.length ?? '—'} onClick={() => go(ep, 'networks')} />
      </div>

      <div className="home-grid">
        <div className="panel">
          <h4><AlertTriangle size={14} /> {tr('Needs attention')}</h4>
          {!cts && <Spinner />}
          {cts && problems.length === 0 && <p className="muted small">{tr('No failing containers. 🎉')}</p>}
          <ul className="task-list">
            {problems.slice(0, 12).map((c) => (
              <li key={c.Id} onClick={() => go(ep, 'containers', c.Id)} onContextMenu={menu(() => containerActions(ep, c))}>
                <ContainerName c={c} />
                <span className="grow" />
                <span className="muted small">{c.State === 'exited' ? tr('exited with {0}', exitCode(c.Status)) : humanStatus(c.Status)}</span>
                <Pill state={c.State} />
              </li>
            ))}
          </ul>
        </div>

        <div className="panel">
          <h4><Star size={14} /> {tr('Favorites')}</h4>
          {myFavs.length === 0 && <p className="muted small">{tr('Right-click any container, stack or network → “Add to favorites”.')}</p>}
          <ul className="task-list">
            {myFavs.map((f) => {
              const c = f.kind === 'container' ? cts?.find((x) => cname(x) === f.id) : undefined
              const s = f.kind === 'stack' ? stacks?.find((x) => x.name === f.id) : undefined
              return (
                <li key={f.kind + f.id} onClick={() => go(ep, f.kind === 'container' ? 'containers' : f.kind + 's', c?.Id ?? f.id)} onContextMenu={c ? menu(() => containerActions(ep, c)) : undefined}>
                  <Tag>{f.kind}</Tag>
                  <strong>{f.label}</strong>
                  <span className="grow" />
                  {c && <Pill state={c.State} />}
                  {s && <Pill state={s.status} />}
                </li>
              )
            })}
          </ul>
        </div>

        <div className="panel span-2">
          <h4><ActIcon size={14} /> {tr('Activity')}</h4>
          <EventFeed ep={ep} compact />
        </div>

        <div className="panel">
          <h4><HardDrive size={14} /> {tr('Disk usage')}</h4>
          {!disk && <Spinner />}
          {disk && (
            <div className="bars">
              {disk.map(([l, v, r]: [string, number, number]) => (
                <div key={l} className="bar-row">
                  <span>{l}</span>
                  <div className="bar"><div style={{ width: `${(v / diskMax) * 100}%` }} /><div className="bar-reclaim" style={{ width: `${(r / diskMax) * 100}%` }} /></div>
                  <span className="num small">{bytes(v)}</span>
                </div>
              ))}
              <p className="muted small">{tr('The striped part can be reclaimed by cleaning up.')}</p>
            </div>
          )}
        </div>

        <div className="panel">
          <h4><Server size={14} /> {tr('Hosts')}</h4>
          <ul className="task-list">
            {(endpoints || []).map((e) => (
              <li key={e.name} onClick={() => go(e.name, 'jump')}>
                <span className={`dot dot-${e.online ? 'open' : 'closed'}`} />
                <strong>{e.name}</strong>
                <span className="muted small ellipsis">{e.url}</span>
                <span className="grow" />
                <span className="muted small">{e.online ? `${e.running}/${e.total}` : 'offline'}</span>
              </li>
            ))}
          </ul>
          <a className="link small" href={href(ep, 'settings', 'hosts')}>{tr('Manage hosts →')}</a>
        </div>
      </div>
    </div>
  )
}

const TYPE_ICON: Record<string, React.ReactNode> = {
  container: <Box size={14} />, image: <ImageIcon size={14} />, network: <NetIcon size={14} />, volume: <HardDrive size={14} />,
}

export function EventFeed({ ep, compact }: { ep: string; compact?: boolean }) {
  const [events, setEvents] = useState<DockerEvent[]>([])
  const [paused, setPaused] = useState(false)
  const [q, setQ] = useState('')
  const [types, setTypes] = usePref<string[]>('activity.types', ['container', 'image', 'network', 'volume'])
  const menu = useMenu()
  useEffect(() => {
    const ac = new AbortController()
    const since = Math.floor(Date.now() / 1000) - (compact ? 3600 * 6 : 86400)
    setEvents([])
    let buf: DockerEvent[] = []
    let t: any = 0
    stream('GET', `/e/${enc(ep)}/events?since=${since}`, undefined, (e) => {
      if (!e.Action || e.Action.startsWith('exec_') || e.Action.startsWith('top') ) return
      buf.push(e)
      if (!t) t = setTimeout(() => {
        const add = buf
        buf = []
        t = 0
        setEvents((p) => [...add.reverse(), ...p].slice(0, 500))
      }, 200)
    }, ac.signal).catch(() => {})
    return () => ac.abort()
  }, [ep, compact])
  const [frozen, setFrozen] = useState<DockerEvent[] | null>(null)
  const list = (paused ? frozen ?? events : events).filter((e) => types.includes(e.Type) && (!q || JSON.stringify(e.Actor.Attributes).toLowerCase().includes(q.toLowerCase()) || e.Action.includes(q)))
  const shown = compact ? list.slice(0, 12) : list
  const target = (e: DockerEvent) => {
    const a = e.Actor.Attributes
    if (e.Type === 'container') return { label: a.name, go: () => go(ep, 'containers', e.Actor.ID) }
    if (e.Type === 'network') return { label: a.name, go: () => go(ep, 'networks', e.Actor.ID) }
    if (e.Type === 'volume') return { label: e.Actor.ID.length > 30 ? e.Actor.ID.slice(0, 12) : e.Actor.ID, go: () => go(ep, 'volumes', e.Actor.ID) }
    if (e.Type === 'image') return { label: a.name || e.Actor.ID.slice(7, 19), go: () => go(ep, 'images', e.Actor.ID) }
    return { label: e.Actor.ID.slice(0, 12), go: () => {} }
  }
  return (
    <div className="feed">
      {!compact && (
        <div className="logs-bar">
          <div className="search"><Search size={14} /><input placeholder={tr('Filter activity…')} value={q} onChange={(e) => setQ(e.target.value)} /></div>
          {['container', 'image', 'network', 'volume'].map((t) => (
            <label key={t} className="check"><input type="checkbox" checked={types.includes(t)} onChange={(e) => setTypes(e.target.checked ? [...types, t] : types.filter((x) => x !== t))} /> {t}</label>
          ))}
          <span className="grow" />
          <button className={`btn btn-sm ${paused ? '' : 'btn-live'}`} onClick={() => { setFrozen(paused ? null : events); setPaused(!paused) }}>
            {paused ? <Play size={13} /> : <Pause size={13} />} {paused ? tr('Paused') : tr('Live')}
          </button>
        </div>
      )}
      {shown.length === 0 && <Empty title={tr('No recent activity')} />}
      <ol className="timeline feed-list">
        {shown.map((e, i) => {
          const t = target(e)
          return (
            <li key={e.timeNano + ':' + i} className={`tl-${e.Action.split(':')[0]}`} onClick={t.go} onContextMenu={menu(() => [
              { label: tr('Open {0}', t.label), onClick: t.go },
              { label: tr('Copy ID'), onClick: () => copy(e.Actor.ID) },
              { label: tr('Copy event (JSON)'), onClick: () => copy(JSON.stringify(e, null, 2)) },
              { label: tr('Filter by “{0}”', t.label), onClick: () => setQ(t.label) },
            ])}>
              <span className="tl-dot" />
              <span className="tl-icon">{TYPE_ICON[e.Type] ?? <ActIcon size={14} />}</span>
              <div className="grow">
                <strong>{t.label}</strong> <span className="muted">· {eventLabel(e)}</span>
                {e.Actor.Attributes['com.docker.compose.project'] && <Tag tone="stack">{e.Actor.Attributes['com.docker.compose.project']}</Tag>}
                {e.Action === 'die' && e.Actor.Attributes.exitCode !== '0' && <Tag tone="bad">{tr('code')} {e.Actor.Attributes.exitCode}</Tag>}
              </div>
              <span className="muted small" title={dateTime(e.time)}>{ago(e.time)}</span>
            </li>
          )
        })}
      </ol>
      {compact && list.length > 12 && <a className="link small" href={href(ep, 'activity')}>{tr('See all activity →')}</a>}
    </div>
  )
}

export function Activity({ ep }: { ep: string }) {
  return (
    <div className="page">
      <div className="page-head">
        <div className="page-title"><ActIcon size={20} /><h1>{tr('Activity')}</h1><span className="muted small">{tr('last 24 h + live')}</span></div>
      </div>
      <div className="page-body"><EventFeed ep={ep} /></div>
    </div>
  )
}

