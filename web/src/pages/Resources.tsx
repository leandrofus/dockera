import { t as tr, tc } from '../i18n'
import { useMemo, useState } from 'react'
import { Image as ImageIcon, Network as NetIcon, HardDrive, Plus, Search, Download, Hammer, Trash2, ChevronRight, PlugZap, Box, Map as MapIcon, LayoutList, Ruler, Upload, Tag as TagIcon, Play } from 'lucide-react'
import { api, enc } from '../lib/api'
import type { Container, Image, Network, Volume } from '../lib/types'
import { useResource, invalidate, peek } from '../lib/store'
import { usePref } from '../lib/prefs'
import { ago, bytes, cname, copy, dateTime, shortId, hue } from '../lib/format'
import { go, href } from '../lib/router'
import { Avatar, CodeEditor, DataTable, Empty, MenuButton, Pill, Props, Section, Spinner, Tabs, Tag } from '../ui/common'
import { useMenu } from '../ui/ContextMenu'
import { Modal, confirm, openDialog, prompt, runJob, toast } from '../ui/overlays'
import { imageActions, networkActions, pullImage, removeImages, volumeActions } from '../actions'
import { ContainerFormDialog } from '../components/ContainerForm'
import { JsonView } from './ContainerRecord'

const tagsOf = (im: Image) => (im.RepoTags || []).filter((t) => t !== '<none>:<none>')

async function askPull(ep: string) {
  const regs = await api.get<any[]>('/registries').catch(() => [])
  const image = await prompt({
    title: tr('Pull image'),
    label: tr('Image'),
    placeholder: 'nginx:alpine · ghcr.io/org/app:1.2',
    confirm: tr('Download'),
    help: regs.length ? tr('Credentials will be used for: {0}', regs.map((r) => r.server).join(', ')) : tr('For private registries add credentials in Settings → Registries.'),
  })
  if (image) pullImage(ep, image)
}

async function prune(ep: string, kind: 'images' | 'networks' | 'volumes', all = false) {
  const labels = { images: all ? tr('all unused images') : tr('untagged (dangling) images'), networks: tr('unused networks'), volumes: tr('unused volumes') }
  if (!(await confirm({ title: tr('Clear'), body: tr('This will delete {0}.', labels[kind]), danger: true, confirm: tr('Clear') }))) return
  try {
    const r = await api.post(`/e/${enc(ep)}/${kind}/prune${all ? '?all=1' : ''}`)
    toast.ok(tr('{0} reclaimed', bytes(r?.SpaceReclaimed || 0)))
  } catch (e) {
    toast.err(e)
  }
  invalidate(`/${kind}`)
}

function BuildDialog({ ep, onClose }: { ep: string; onClose: () => void }) {
  const [tag, setTag] = useState('')
  const [df, setDf] = useState('FROM alpine:3\nRUN apk add --no-cache curl\nCMD ["sh"]\n')
  const [pull, setPull] = useState(true)
  return (
    <Modal
      wide
      title={tr('Build image')}
      icon={<Hammer size={18} />}
      onClose={onClose}
      footer={
        <>
          <label className="check"><input type="checkbox" checked={pull} onChange={(e) => setPull(e.target.checked)} /> {tr('Pull base image')}</label>
          <span className="grow" />
          <button className="btn" onClick={onClose}>{tr('Cancel')}</button>
          <button className="btn btn-primary" disabled={!tag} onClick={() => {
            onClose()
            runJob(`Build ${tag}`, 'POST', `/e/${enc(ep)}/images/build`, { tag, dockerfile: df, pull }).catch((e) => toast.err(e)).finally(() => invalidate('/images'))
          }}>
            <Hammer size={14} /> {tr('Build')}
          </button>
        </>
      }
    >
      <label className="field"><span>{tr('Tag')}</span><input className="mono" value={tag} placeholder={tr('my-image:1.0')} onChange={(e) => setTag(e.target.value)} autoFocus /></label>
      <div className="label">Dockerfile</div>
      <CodeEditor value={df} onChange={setDf} minRows={14} />
    </Modal>
  )
}

export function Images({ ep }: { ep: string }) {
  const { data, error } = useResource<Image[]>(`/e/${enc(ep)}/images`, { poll: 20000 })
  const [q, setQ] = useState('')
  const [view, setView] = usePref('images.view', 'all')
  const [sel, setSel] = useState<Set<string>>(new Set())
  const menu = useMenu()
  const all = data || []
  const filt = (im: Image) =>
    view === 'used' ? (im.UsedBy?.length ?? 0) > 0 : view === 'unused' ? !(im.UsedBy?.length ?? 0) : view === 'dangling' ? tagsOf(im).length === 0 : true
  const rows = all.filter((im) => filt(im) && (!q || `${tagsOf(im).join(' ')} ${im.Id}`.toLowerCase().includes(q.toLowerCase())))
  const total = all.reduce((a, im) => a + im.Size, 0)
  const pageMenu = () => [
    { label: tr('Pull image…'), icon: <Download size={14} />, onClick: () => askPull(ep) },
    { label: tr('Build from a Dockerfile…'), icon: <Hammer size={14} />, onClick: () => openDialog((c) => <BuildDialog ep={ep} onClose={c} />) },
    'sep' as const,
    { label: tr('Delete untagged images'), icon: <Trash2 size={14} />, onClick: () => prune(ep, 'images') },
    { label: tr('Delete all unused images'), icon: <Trash2 size={14} />, danger: true, onClick: () => prune(ep, 'images', true) },
  ]
  return (
    <div className="page" onContextMenu={(e) => (e.target as HTMLElement).closest('.page-body') && menu(pageMenu)(e)}>
      <div className="page-head">
        <div className="page-title">
          <ImageIcon size={20} />
          <h1>{tr('Images')}</h1>
          <span className="count">{all.length}</span>
          <span className="muted small">{bytes(total)}</span>
        </div>
        <div className="page-actions">
          <div className="search"><Search size={14} /><input placeholder={tr('Search image…')} value={q} onChange={(e) => setQ(e.target.value)} /></div>
          <MenuButton items={pageMenu} label={<><Trash2 size={14} /> {tr('Clear')}</>} className="btn" />
          <button className="btn" onClick={() => openDialog((c) => <BuildDialog ep={ep} onClose={c} />)}><Hammer size={15} /> Build</button>
          <button className="btn btn-primary" onClick={() => askPull(ep)}><Download size={15} /> Pull</button>
        </div>
      </div>
      <Tabs
        active={view}
        onChange={setView}
        tabs={[
          { id: 'all', label: tc('f', 'All'), count: all.length },
          { id: 'used', label: tr('In use'), count: all.filter((i) => i.UsedBy?.length).length },
          { id: 'unused', label: tr('Unused'), count: all.filter((i) => !i.UsedBy?.length).length },
          { id: 'dangling', label: tr('Untagged'), count: all.filter((i) => !tagsOf(i).length).length },
        ]}
      />
      {sel.size > 0 && (
        <div className="bulkbar">
          <strong>{sel.size} {tc('f', 'selected')}</strong>
          <button className="btn btn-sm btn-danger-ghost" onClick={() => removeImages(ep, all.filter((i) => sel.has(i.Id))).then(() => setSel(new Set()))}><Trash2 size={14} /> {tr('Delete')}</button>
          <span className="grow" />
          <button className="btn btn-sm btn-ghost" onClick={() => setSel(new Set())}>{tr('Clear selection')}</button>
        </div>
      )}
      <div className="page-body">
        {error && <div className="alert alert-bad">{error.message}</div>}
        {!data && !error && <div className="center"><Spinner /></div>}
        {data && (
          <DataTable
            rows={rows}
            rowKey={(i) => i.Id}
            selected={sel}
            setSelected={setSel}
            onOpen={(i) => go(ep, 'images', i.Id)}
            rowMenu={(i) => imageActions(ep, i)}
            bulkMenu={(list) => [{ label: tr('Delete {0} images…', list.length), icon: <Trash2 size={14} />, danger: true, onClick: () => removeImages(ep, list) }]}
            defaultSort={{ key: 'created', dir: 1 }}
            empty={<Empty icon={<ImageIcon size={28} />} title={tr('No images')} />}
            columns={[
              {
                key: 'tag',
                label: tr('Image'),
                sort: (i) => tagsOf(i)[0] || '~',
                render: (i) => (
                  <span className="cell-name">
                    <Avatar name={(tagsOf(i)[0] || 'none').split('/').pop()!} size={24} square />
                    <span className="cell-name-text">
                      <strong className="mono small">{tagsOf(i)[0] || <span className="muted">&lt;{tr('untagged')}&gt;</span>}</strong>
                      {tagsOf(i).length > 1 && <span className="muted small">+{tagsOf(i).length - 1} {tr('tags')}</span>}
                    </span>
                  </span>
                ),
              },
              { key: 'id', label: 'ID', width: 120, render: (i) => <span className="mono small muted">{shortId(i.Id)}</span> },
              { key: 'used', label: tc('f', 'Used by'), width: 200, sort: (i) => -(i.UsedBy?.length || 0), render: (i) => (i.UsedBy?.length ? <span className="tags">{i.UsedBy.slice(0, 2).map((n) => <Tag key={n}>{n}</Tag>)}{i.UsedBy.length > 2 && <span className="muted small">+{i.UsedBy.length - 2}</span>}</span> : <span className="muted small">{tr('unused')}</span>) },
              { key: 'size', label: tr('Size'), width: 100, sort: (i) => -i.Size, render: (i) => bytes(i.Size), className: 'num' },
              { key: 'created', label: tc('f', 'Created'), width: 130, sort: (i) => -i.Created, render: (i) => <span className="muted small" title={dateTime(i.Created)}>{ago(i.Created)}</span> },
              { key: 'm', label: '', width: 36, render: (i) => <MenuButton items={() => imageActions(ep, i)} /> },
            ]}
          />
        )}
      </div>
    </div>
  )
}

export function ImageRecord({ ep, id }: { ep: string; id: string }) {
  const { data, error } = useResource<{ inspect: any; history: any[] }>(`/e/${enc(ep)}/images/${id}`)
  const { data: images } = useResource<Image[]>(`/e/${enc(ep)}/images`)
  const { data: containers } = useResource<Container[]>(`/e/${enc(ep)}/containers`)
  const [tab, setTab] = useState('overview')
  const menu = useMenu()
  if (error) return <div className="page"><Empty icon={<ImageIcon size={28} />} title={tr('Image not found')}>{error.message}</Empty></div>
  if (!data) return <div className="page center"><Spinner /></div>
  const im = data.inspect
  const summary: Image = images?.find((x) => x.Id === im.Id) ?? { Id: im.Id, RepoTags: im.RepoTags, RepoDigests: im.RepoDigests, Created: Date.parse(im.Created) / 1000, Size: im.Size, Containers: 0, Labels: im.Config?.Labels, UsedBy: [] }
  const tags = tagsOf(summary)
  const using = (containers || []).filter((c) => c.ImageID === im.Id)
  const cfg = im.Config || {}
  return (
    <div className="page record">
      <div className="breadcrumbs"><a href={href(ep, 'images')}>{tr('Images')}</a><ChevronRight size={13} /><span>{tags[0] || shortId(im.Id)}</span></div>
      <header className="record-head" onContextMenu={menu(() => imageActions(ep, summary))}>
        <Avatar name={(tags[0] || 'img').split('/').pop()!} size={52} square />
        <div className="record-title">
          <h1 className="mono">{tags[0] || shortId(im.Id)}</h1>
          <div className="row gap wrap">
            {tags.slice(1).map((t) => <Tag key={t} mono>{t}</Tag>)}
            <span className="muted small">{bytes(im.Size)} · {im.Os}/{im.Architecture} · {ago(im.Created)}</span>
          </div>
        </div>
        <div className="record-actions">
          <button className="btn btn-primary" onClick={() => openDialog((close) => <ContainerFormDialog ep={ep} initial={{ image: tags[0] || im.Id, name: '' }} mode="create" onClose={close} />)}>
            <Play size={14} /> {tr('Create container')}
          </button>
          {tags[0] && <button className="btn" onClick={() => pullImage(ep, tags[0])}><Download size={14} /> Pull</button>}
          {tags[0] && <button className="btn" onClick={() => runJob(`Push ${tags[0]}`, 'POST', `/e/${enc(ep)}/images/push`, { image: tags[0] }).catch((e) => toast.err(e))}><Upload size={14} /> Push</button>}
          <MenuButton items={() => imageActions(ep, summary)} className="btn" />
        </div>
      </header>
      <div className="record-body">
        <aside className="record-side">
          <Section title={tr('Properties')}>
            <Props
              items={[
                ['ID', <span className="mono small">{shortId(im.Id)}</span>, () => [{ label: tr('Copy ID'), onClick: () => copy(im.Id) }]],
                [tc('f', 'Created'), dateTime(im.Created)],
                [tr('Size'), bytes(im.Size)],
                [tr('Platform'), `${im.Os}/${im.Architecture}${im.Variant ? '/' + im.Variant : ''}`],
                cfg.Entrypoint && ['Entrypoint', <span className="mono small">{cfg.Entrypoint.join(' ')}</span>],
                cfg.Cmd && ['Cmd', <span className="mono small">{cfg.Cmd.join(' ')}</span>],
                cfg.WorkingDir && ['Workdir', <span className="mono small">{cfg.WorkingDir}</span>],
                cfg.User && [tr('User'), cfg.User],
                cfg.ExposedPorts && [tr('Ports'), <span className="tags">{Object.keys(cfg.ExposedPorts).map((p) => <Tag key={p} mono>{p}</Tag>)}</span>],
                cfg.Volumes && [tr('Volumes'), <span className="mono small">{Object.keys(cfg.Volumes).join(', ')}</span>],
              ]}
            />
          </Section>
          <Section title={tr('Tags')}>
            <ul className="rel-list">
              {tags.map((t) => (
                <li key={t} onContextMenu={menu(() => [{ label: tr('Copy'), onClick: () => copy(t) }, { label: 'Pull', onClick: () => pullImage(ep, t) }, { label: tr('Remove tag'), danger: true, onClick: () => api.del(`/e/${enc(ep)}/images/${t}`).then(() => invalidate('/images')).catch(toast.err) }])}>
                  <TagIcon size={14} /> <span className="mono small grow">{t}</span>
                </li>
              ))}
              <li onClick={async () => {
                const t = await prompt({ title: tr('Add tag'), label: 'repo:tag', confirm: tr('Tag') })
                if (t) api.post(`/e/${enc(ep)}/images/tag`, { source: im.Id, target: t }).then(() => (invalidate('/images'), toast.ok(tr('Tagged')))).catch(toast.err)
              }}><Plus size={14} /> <span className="muted">{tr('Add tag…')}</span></li>
            </ul>
          </Section>
          <Section title={tr('Containers · {0}', using.length)}>
            <ul className="rel-list">
              {using.map((c) => (
                <li key={c.Id} onClick={() => go(ep, 'containers', c.Id)}><Box size={14} /><span className="grow">{cname(c)}</span><Pill state={c.State} /></li>
              ))}
              {!using.length && <li className="muted small">{tr('No container uses it')}</li>}
            </ul>
          </Section>
        </aside>
        <main className="record-main">
          <Tabs active={tab} onChange={setTab} tabs={[{ id: 'overview', label: 'Capas', count: data.history?.length }, { id: 'env', label: tr('Environment and labels') }, { id: 'inspect', label: 'Inspect' }]} />
          <div className="tab-body">
            {tab === 'overview' && (
              <div className="table-wrap">
                <table className="table compact">
                  <thead><tr><th>{tr('Instruction')}</th><th style={{ width: 100 }}>{tr('Size')}</th><th style={{ width: 130 }}>{tc('f', 'Created')}</th></tr></thead>
                  <tbody>
                    {(data.history || []).map((h, i) => (
                      <tr key={i} onContextMenu={menu(() => [{ label: tr('Copy instruction'), onClick: () => copy(h.CreatedBy) }])}>
                        <td className="mono small layer-cmd" title={h.CreatedBy}>{h.CreatedBy.replace(/^\/bin\/sh -c #\(nop\)\s*/, '').replace(/^\/bin\/sh -c /, 'RUN ')}</td>
                        <td className="num small">{h.Size ? bytes(h.Size) : '—'}</td>
                        <td className="muted small">{ago(h.Created)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            {tab === 'env' && (
              <div className="overview-grid">
                <div className="panel"><h4>{tr('Variables')}</h4><pre className="mono small">{(cfg.Env || []).join('\n')}</pre></div>
                <div className="panel"><h4>Labels</h4><pre className="mono small">{Object.entries(cfg.Labels || {}).map(([k, v]) => `${k}=${v}`).join('\n') || '—'}</pre></div>
              </div>
            )}
            {tab === 'inspect' && <JsonView value={im} />}
          </div>
        </main>
      </div>
    </div>
  )
}

// ---------- networks ----------

function NewNetworkDialog({ ep, onClose }: { ep: string; onClose: () => void }) {
  const [f, setF] = useState({ name: '', driver: 'bridge', subnet: '', gateway: '', ip_range: '', internal: false, attachable: true, ipv6: false, parent: '' })
  const set = (p: Partial<typeof f>) => setF({ ...f, ...p })
  const submit = async () => {
    try {
      await api.post(`/e/${enc(ep)}/networks`, { ...f, options: f.driver === 'macvlan' || f.driver === 'ipvlan' ? { parent: f.parent } : undefined })
      toast.ok(tr('Network {0} created', f.name))
      invalidate('/networks')
      onClose()
    } catch (e) {
      toast.err(e)
    }
  }
  return (
    <Modal title={tr('New network')} icon={<NetIcon size={18} />} onClose={onClose} footer={<><button className="btn" onClick={onClose}>{tr('Cancel')}</button><button className="btn btn-primary" disabled={!f.name} onClick={submit}>{tr('Create network')}</button></>}>
      <div className="grid-2">
        <label className="field"><span>{tr('Name')}</span><input value={f.name} onChange={(e) => set({ name: e.target.value })} autoFocus /></label>
        <label className="field"><span>Driver</span>
          <select value={f.driver} onChange={(e) => set({ driver: e.target.value })}>
            <option>bridge</option><option>overlay</option><option>macvlan</option><option>ipvlan</option>
          </select>
        </label>
        <label className="field"><span>{tr('Subnet (CIDR)')}</span><input className="mono" placeholder="172.30.0.0/16 (auto)" value={f.subnet} onChange={(e) => set({ subnet: e.target.value })} /></label>
        <label className="field"><span>Gateway</span><input className="mono" placeholder="172.30.0.1 (auto)" value={f.gateway} onChange={(e) => set({ gateway: e.target.value })} /></label>
        <label className="field"><span>{tr('IP range')}</span><input className="mono" placeholder={tr('optional')} value={f.ip_range} onChange={(e) => set({ ip_range: e.target.value })} /></label>
        {(f.driver === 'macvlan' || f.driver === 'ipvlan') && <label className="field"><span>{tr('Parent interface')}</span><input className="mono" placeholder="eth0" value={f.parent} onChange={(e) => set({ parent: e.target.value })} /></label>}
      </div>
      <div className="checks">
        <label className="check"><input type="checkbox" checked={f.internal} onChange={(e) => set({ internal: e.target.checked })} /> {tr('Internal (no internet access)')}</label>
        <label className="check"><input type="checkbox" checked={f.attachable} onChange={(e) => set({ attachable: e.target.checked })} /> Attachable</label>
        <label className="check"><input type="checkbox" checked={f.ipv6} onChange={(e) => set({ ipv6: e.target.checked })} /> IPv6</label>
      </div>
    </Modal>
  )
}

const subnet = (n: Network) => n.IPAM?.Config?.map((c) => c.Subnet).filter(Boolean).join(', ') || '—'

export function Networks({ ep }: { ep: string }) {
  const { data, error } = useResource<Network[]>(`/e/${enc(ep)}/networks`, { poll: 15000 })
  useResource<Container[]>(`/e/${enc(ep)}/containers`)
  const [q, setQ] = useState('')
  const [layout, setLayout] = usePref<'table' | 'map'>('networks.layout', 'table')
  const [hideEmpty, setHideEmpty] = usePref('networks.hideEmpty', false)
  const [hover, setHover] = useState<string | null>(null)
  const menu = useMenu()
  const all = data || []
  const rows = all.filter((n) => (!q || `${n.Name} ${subnet(n)} ${n.Attached.map((a) => a.name).join(' ')}`.toLowerCase().includes(q.toLowerCase())) && (!hideEmpty || n.Attached.length > 0))
  const newNet = () => openDialog((c) => <NewNetworkDialog ep={ep} onClose={c} />)
  return (
    <div className="page" onContextMenu={(e) => (e.target as HTMLElement).closest('.page-body') && menu(() => [
      { label: tr('New network…'), icon: <Plus size={14} />, onClick: newNet },
      { label: tr('Hide empty networks'), checked: hideEmpty, onClick: () => setHideEmpty(!hideEmpty) },
      { label: tr('Table view'), checked: layout === 'table', onClick: () => setLayout('table') },
      { label: tr('Connection map'), checked: layout === 'map', onClick: () => setLayout('map') },
      'sep',
      { label: tr('Delete unused networks…'), icon: <Trash2 size={14} />, danger: true, onClick: () => prune(ep, 'networks') },
    ])(e)}>
      <div className="page-head">
        <div className="page-title"><NetIcon size={20} /><h1>{tr('Networks')}</h1><span className="count">{all.length}</span></div>
        <div className="page-actions">
          <div className="search"><Search size={14} /><input placeholder={tr('Network, subnet or container…')} value={q} onChange={(e) => setQ(e.target.value)} /></div>
          <div className="seg">
            <button className={layout === 'table' ? 'on' : ''} onClick={() => setLayout('table')} title={tr('Table')}><LayoutList size={15} /></button>
            <button className={layout === 'map' ? 'on' : ''} onClick={() => setLayout('map')} title={tr('Map')}><MapIcon size={15} /></button>
          </div>
          <button className={`btn btn-sm${hideEmpty ? ' on' : ''}`} onClick={() => setHideEmpty(!hideEmpty)}>{tr('Hide empty')}</button>
          <button className="btn btn-primary" onClick={newNet}><Plus size={15} /> {tr('New network')}</button>
        </div>
      </div>
      <div className="page-body">
        {error && <div className="alert alert-bad">{error.message}</div>}
        {!data && !error && <div className="center"><Spinner /></div>}
        {data && layout === 'table' && (
          <DataTable
            rows={rows}
            rowKey={(n) => n.Id}
            onOpen={(n) => go(ep, 'networks', n.Id)}
            rowMenu={(n) => networkActions(ep, n)}
            defaultSort={{ key: 'name', dir: 1 }}
            columns={[
              { key: 'name', label: tr('Network'), sort: (n) => n.Name, render: (n) => <span className="cell-name"><span className="net-swatch" style={{ ['--h' as any]: hue(n.Name) }} /><strong>{n.Name}</strong>{n.Internal && <Tag>{tr('internal')}</Tag>}</span> },
              { key: 'driver', label: 'Driver', width: 100, sort: (n) => n.Driver, render: (n) => <Tag>{n.Driver}</Tag> },
              { key: 'subnet', label: tr('Subnet'), width: 170, render: (n) => <span className="mono small">{subnet(n)}</span> },
              { key: 'gw', label: 'Gateway', width: 130, render: (n) => <span className="mono small muted">{n.IPAM?.Config?.[0]?.Gateway || '—'}</span> },
              {
                key: 'att',
                label: tr('Containers'),
                sort: (n) => -n.Attached.length,
                render: (n) => (
                  <span className="avatars">
                    {n.Attached.slice(0, 6).map((a) => <span key={a.id} title={`${a.name} ${a.ipv4 || ''}`} onClick={(e) => (e.stopPropagation(), go(ep, 'containers', a.id))}><Avatar name={a.name} size={22} /></span>)}
                    {n.Attached.length > 6 && <span className="muted small">+{n.Attached.length - 6}</span>}
                    {!n.Attached.length && <span className="muted small">—</span>}
                  </span>
                ),
              },
              { key: 'created', label: tc('f', 'Created'), width: 120, sort: (n) => -Date.parse(n.Created), render: (n) => <span className="muted small">{ago(n.Created)}</span> },
              { key: 'm', label: '', width: 36, render: (n) => <MenuButton items={() => networkActions(ep, n)} /> },
            ]}
          />
        )}
        {data && layout === 'map' && (
          <div className="netmap">
            {rows.filter((n) => n.Name !== 'none').map((n) => (
              <div key={n.Id} className="netmap-net" style={{ ['--h' as any]: hue(n.Name) }} onContextMenu={menu(() => networkActions(ep, n))}>
                <div className="netmap-head" onClick={() => go(ep, 'networks', n.Id)}>
                  <NetIcon size={14} /> <strong>{n.Name}</strong>
                  <span className="muted small">{n.Driver} · {subnet(n)}</span>
                </div>
                <div className="netmap-body">
                  {n.Attached.map((a) => (
                    <div
                      key={a.id}
                      className={`netmap-node${hover === a.id ? ' hot' : ''}${a.state !== 'running' ? ' off' : ''}`}
                      onMouseEnter={() => setHover(a.id)}
                      onMouseLeave={() => setHover(null)}
                      onClick={() => go(ep, 'containers', a.id)}
                      onContextMenu={menu(() => {
                        const c = (useResourcePeek(ep) || []).find((x) => x.Id === a.id)
                        return [
                          { header: a.name },
                          { label: tr('Open container'), icon: <Box size={14} />, onClick: () => go(ep, 'containers', a.id) },
                          { label: tr('Copy IP'), hint: a.ipv4, onClick: () => copy(a.ipv4?.split('/')[0] || '') },
                          c && c.State === 'running' && { label: tr('Terminal'), onClick: () => go(ep, 'containers', a.id, 'terminal') },
                          'sep',
                          { label: tr('Disconnect from {0}', n.Name), danger: true, onClick: () => api.post(`/e/${enc(ep)}/networks/${enc(n.Id)}/disconnect`, { container: a.id, force: true }).then(() => invalidate('/networks', '/containers')).catch(toast.err) },
                        ]
                      })}
                    >
                      <Avatar name={a.name} size={20} />
                      <span className="ellipsis">{a.name}</span>
                      <span className="mono small muted">{a.ipv4?.split('/')[0]}</span>
                    </div>
                  ))}
                  {!n.Attached.length && <div className="muted small">{tr('no containers')}</div>}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

const useResourcePeek = (ep: string) => peek<Container[]>(`/e/${enc(ep)}/containers`)

export function NetworkRecord({ ep, id }: { ep: string; id: string }) {
  const { data } = useResource<Network[]>(`/e/${enc(ep)}/networks`, { poll: 10000 })
  const { data: inspect } = useResource<any>(`/e/${enc(ep)}/networks/${enc(id)}`)
  const { data: containers } = useResource<Container[]>(`/e/${enc(ep)}/containers`)
  const [tab, setTab] = useState('containers')
  const [pick, setPick] = useState('')
  const menu = useMenu()
  const n = data?.find((x) => x.Id === id || x.Name === id)
  if (!n) return <div className="page center">{data ? <Empty title={tr('Network not found')} /> : <Spinner />}</div>
  const candidates = (containers || []).filter((c) => !n.Attached.some((a) => a.id === c.Id))
  const connect = async () => {
    if (!pick) return
    try {
      await api.post(`/e/${enc(ep)}/networks/${enc(n.Id)}/connect`, { container: pick })
      toast.ok(tr('Connected'))
      setPick('')
    } catch (e) {
      toast.err(e)
    }
    invalidate('/networks', '/containers')
  }
  return (
    <div className="page record">
      <div className="breadcrumbs"><a href={href(ep, 'networks')}>{tr('Networks')}</a><ChevronRight size={13} /><span>{n.Name}</span></div>
      <header className="record-head" onContextMenu={menu(() => networkActions(ep, n))}>
        <span className="net-swatch big" style={{ ['--h' as any]: hue(n.Name) }}><NetIcon size={24} /></span>
        <div className="record-title">
          <h1>{n.Name}</h1>
          <div className="row gap wrap"><Tag>{n.Driver}</Tag><Tag>{n.Scope}</Tag>{n.Internal && <Tag tone="warn">{tr('internal')}</Tag>}<span className="mono small muted">{subnet(n)}</span></div>
        </div>
        <div className="record-actions"><MenuButton items={() => networkActions(ep, n)} className="btn" /></div>
      </header>
      <div className="record-body">
        <aside className="record-side">
          <Section title={tr('Properties')}>
            <Props items={[
              ['ID', <span className="mono small">{shortId(n.Id)}</span>, () => [{ label: tr('Copy ID'), onClick: () => copy(n.Id) }]],
              ['Driver', n.Driver],
              [tr('Subnet'), <span className="mono small">{subnet(n)}</span>],
              ['Gateway', <span className="mono small">{n.IPAM?.Config?.[0]?.Gateway || '—'}</span>],
              ['IPv6', n.EnableIPv6 ? tr('Yes') : tr('No')],
              ['Attachable', n.Attachable ? tr('Yes') : tr('No')],
              [tc('f', 'Created'), dateTime(n.Created)],
              n.Labels?.['com.docker.compose.project'] && ['Stack', <a href={href(ep, 'stacks', n.Labels['com.docker.compose.project'])}>{n.Labels['com.docker.compose.project']}</a>],
            ]} />
          </Section>
          {n.Options && Object.keys(n.Options).length > 0 && (
            <Section title={tr('Options')}><Props items={Object.entries(n.Options).map(([k, v]) => [<span className="mono small">{k}</span>, <span className="mono small">{v}</span>])} /></Section>
          )}
        </aside>
        <main className="record-main">
          <Tabs active={tab} onChange={setTab} tabs={[{ id: 'containers', label: tr('Connected containers'), count: n.Attached.length }, { id: 'inspect', label: 'Inspect' }]} />
          <div className="tab-body">
            {tab === 'containers' && (
              <>
                {!['host', 'none'].includes(n.Name) && (
                  <div className="row gap connect-row">
                    <PlugZap size={15} />
                    <select value={pick} onChange={(e) => setPick(e.target.value)}>
                      <option value="">{tr('Connect a container…')}</option>
                      {candidates.map((c) => <option key={c.Id} value={c.Id}>{cname(c)}</option>)}
                    </select>
                    <button className="btn btn-sm" disabled={!pick} onClick={connect}>{tr('Connect')}</button>
                  </div>
                )}
                <DataTable
                  rows={n.Attached}
                  rowKey={(a) => a.id}
                  onOpen={(a) => go(ep, 'containers', a.id)}
                  rowMenu={(a) => [
                    { header: a.name },
                    { label: tr('Open container'), icon: <Box size={14} />, onClick: () => go(ep, 'containers', a.id) },
                    { label: tr('Copy IP'), hint: a.ipv4, onClick: () => copy(a.ipv4?.split('/')[0] || '') },
                    a.mac && { label: tr('Copy MAC'), onClick: () => copy(a.mac!) },
                    'sep',
                    { label: tr('Disconnect'), danger: true, onClick: () => api.post(`/e/${enc(ep)}/networks/${enc(n.Id)}/disconnect`, { container: a.id, force: true }).then(() => invalidate('/networks', '/containers')).catch(toast.err) },
                  ]}
                  empty={<Empty title={tr('No connected containers')} />}
                  columns={[
                    { key: 'n', label: tr('Container'), sort: (a) => a.name, render: (a) => <span className="cell-name"><Avatar name={a.name} size={24} /><strong>{a.name}</strong></span> },
                    { key: 's', label: tr('Status'), width: 150, render: (a) => <Pill state={a.state} /> },
                    { key: 'ip', label: 'IPv4', width: 160, render: (a) => <span className="mono small">{a.ipv4 || '—'}</span> },
                    { key: 'mac', label: 'MAC', width: 170, render: (a) => <span className="mono small muted">{a.mac || '—'}</span> },
                  ]}
                />
              </>
            )}
            {tab === 'inspect' && <JsonView value={inspect} />}
          </div>
        </main>
      </div>
    </div>
  )
}

// ---------- volumes ----------

export function Volumes({ ep }: { ep: string }) {
  const [sizes, setSizes] = usePref('volumes.sizes', false)
  const { data, error, loading } = useResource<Volume[]>(`/e/${enc(ep)}/volumes${sizes ? '?size=1' : ''}`, { poll: 30000 })
  const [q, setQ] = useState('')
  const [view, setView] = useState('all')
  const menu = useMenu()
  const all = data || []
  const rows = useMemo(() => all.filter((v) => (!q || `${v.Name} ${v.UsedBy.map((u) => u.name).join(' ')}`.toLowerCase().includes(q.toLowerCase())) && (view === 'all' || (view === 'used' ? v.UsedBy.length : !v.UsedBy.length))), [all, q, view])
  const newVol = async () => {
    const name = await prompt({ title: tr('New volume'), label: tr('Name'), confirm: tr('Create') })
    if (!name) return
    api.post(`/e/${enc(ep)}/volumes`, { name }).then(() => (toast.ok(tr('Volume created')), invalidate('/volumes'))).catch(toast.err)
  }
  return (
    <div className="page" onContextMenu={(e) => (e.target as HTMLElement).closest('.page-body') && menu(() => [
      { label: tr('New volume…'), icon: <Plus size={14} />, onClick: newVol },
      { label: tr('Compute sizes'), checked: sizes, onClick: () => setSizes(!sizes) },
      'sep',
      { label: tr('Delete unused volumes…'), icon: <Trash2 size={14} />, danger: true, onClick: () => prune(ep, 'volumes') },
    ])(e)}>
      <div className="page-head">
        <div className="page-title"><HardDrive size={20} /><h1>{tr('Volumes')}</h1><span className="count">{all.length}</span>{loading && <Spinner />}</div>
        <div className="page-actions">
          <div className="search"><Search size={14} /><input placeholder={tr('Volume or container…')} value={q} onChange={(e) => setQ(e.target.value)} /></div>
          <button className={`btn btn-sm${sizes ? ' on' : ''}`} onClick={() => setSizes(!sizes)} title={tr('Uses docker system df (may take a while)')}><Ruler size={14} /> {tr('Sizes')}</button>
          <button className="btn" onClick={() => prune(ep, 'volumes')}><Trash2 size={14} /> {tr('Clear')}</button>
          <button className="btn btn-primary" onClick={newVol}><Plus size={15} /> {tr('New')}</button>
        </div>
      </div>
      <Tabs active={view} onChange={setView} tabs={[
        { id: 'all', label: tr('All'), count: all.length },
        { id: 'used', label: tr('In use'), count: all.filter((v) => v.UsedBy.length).length },
        { id: 'unused', label: tr('Orphaned'), count: all.filter((v) => !v.UsedBy.length).length },
      ]} />
      <div className="page-body">
        {error && <div className="alert alert-bad">{error.message}</div>}
        {!data && !error && <div className="center"><Spinner /></div>}
        {data && (
          <DataTable
            rows={rows}
            rowKey={(v) => v.Name}
            onOpen={(v) => go(ep, 'volumes', v.Name)}
            rowMenu={(v) => volumeActions(ep, v)}
            defaultSort={{ key: 'name', dir: 1 }}
            empty={<Empty icon={<HardDrive size={28} />} title={tr('No volumes')} />}
            columns={[
              { key: 'name', label: tr('Volume'), sort: (v) => v.Name, render: (v) => <span className="cell-name"><HardDrive size={15} className="muted" /><strong className={v.Name.length > 40 ? 'mono small' : ''}>{v.Name.length > 40 ? shortId(v.Name) + '…' : v.Name}</strong>{v.Labels?.['com.docker.compose.project'] && <Tag tone="stack">{v.Labels['com.docker.compose.project']}</Tag>}</span> },
              { key: 'used', label: tr('Used by'), sort: (v) => -v.UsedBy.length, render: (v) => v.UsedBy.length ? <span className="tags">{v.UsedBy.map((u) => <Tag key={u.id + u.dest} onClick={() => go(ep, 'containers', u.id)}>{u.name}</Tag>)}</span> : <span className="muted small">{tr('orphaned')}</span> },
              { key: 'driver', label: 'Driver', width: 90, render: (v) => <span className="muted small">{v.Driver}</span> },
              sizes ? { key: 'size', label: tr('Size'), width: 100, className: 'num', sort: (v) => -(v.Size ?? 0), render: (v) => (v.Size !== undefined && v.Size >= 0 ? bytes(v.Size) : '—') } : { key: 'size', label: '', width: 0, render: () => null },
              { key: 'created', label: tr('Created'), width: 120, sort: (v) => -Date.parse(v.CreatedAt), render: (v) => <span className="muted small">{ago(v.CreatedAt)}</span> },
              { key: 'm', label: '', width: 36, render: (v) => <MenuButton items={() => volumeActions(ep, v)} /> },
            ]}
          />
        )}
      </div>
    </div>
  )
}

export function VolumeRecord({ ep, name }: { ep: string; name: string }) {
  const { data } = useResource<Volume[]>(`/e/${enc(ep)}/volumes`)
  const { data: inspect } = useResource<any>(`/e/${enc(ep)}/volumes/${enc(name)}`)
  const menu = useMenu()
  const v = data?.find((x) => x.Name === name)
  if (!v) return <div className="page center">{data ? <Empty title={tr('Volume not found')} /> : <Spinner />}</div>
  const running = v.UsedBy.find((u) => u.state === 'running')
  return (
    <div className="page record">
      <div className="breadcrumbs"><a href={href(ep, 'volumes')}>{tr('Volumes')}</a><ChevronRight size={13} /><span>{v.Name}</span></div>
      <header className="record-head" onContextMenu={menu(() => volumeActions(ep, v))}>
        <span className="net-swatch big" style={{ ['--h' as any]: 200 }}><HardDrive size={24} /></span>
        <div className="record-title"><h1 className={v.Name.length > 30 ? 'mono' : ''}>{v.Name}</h1><div className="row gap"><Tag>{v.Driver}</Tag><span className="muted small">{ago(v.CreatedAt)}</span></div></div>
        <div className="record-actions">
          {running && <a className="btn" href={href(ep, 'containers', running.id, 'files') + `?path=${enc(running.dest || '/')}`}>{tr('Browse content')}</a>}
          <MenuButton items={() => volumeActions(ep, v)} className="btn" />
        </div>
      </header>
      <div className="record-body">
        <aside className="record-side">
          <Section title={tr('Properties')}>
            <Props items={[
              ['Driver', v.Driver],
              [tr('Mount point'), <span className="mono small">{v.Mountpoint}</span>, () => [{ label: tr('Copy path'), onClick: () => copy(v.Mountpoint) }]],
              [tr('Created'), dateTime(v.CreatedAt)],
              ['Scope', v.Scope],
              v.Labels?.['com.docker.compose.project'] && ['Stack', <a href={href(ep, 'stacks', v.Labels['com.docker.compose.project'])}>{v.Labels['com.docker.compose.project']}</a>],
            ]} />
          </Section>
        </aside>
        <main className="record-main">
          <h4 className="subhead">{tr('Used by')}</h4>
          <DataTable
            rows={v.UsedBy}
            rowKey={(u) => u.id + u.dest}
            onOpen={(u) => go(ep, 'containers', u.id)}
            rowMenu={(u) => [{ label: tr('Open container'), onClick: () => go(ep, 'containers', u.id) }, u.state === 'running' && { label: tr('Browse this mount'), onClick: () => (location.hash = href(ep, 'containers', u.id, 'files') + `?path=${enc(u.dest || '/')}`) }]}
            empty={<Empty title={tr('No container uses this volume')} />}
            columns={[
              { key: 'n', label: tr('Container'), render: (u) => <span className="cell-name"><Avatar name={u.name} size={24} /><strong>{u.name}</strong></span> },
              { key: 's', label: tr('Status'), width: 150, render: (u) => <Pill state={u.state} /> },
              { key: 'd', label: tr('Mounted at'), render: (u) => <span className="mono small">{u.dest}</span> },
            ]}
          />
          <h4 className="subhead">Inspect</h4>
          <JsonView value={inspect} />
        </main>
      </div>
    </div>
  )
}
