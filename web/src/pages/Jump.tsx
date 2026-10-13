import { t } from '../i18n'
import { useMemo, useState } from 'react'
import { Zap, Globe, Plug, Search, X, Pencil, Copy, ExternalLink, Server, Lock, EyeOff, ArrowLeftRight, Cloud, AppWindow } from 'lucide-react'
import { api, enc } from '../lib/api'
import { invalidate, useResource } from '../lib/store'
import { usePref } from '../lib/prefs'
import { copy } from '../lib/format'
import { go } from '../lib/router'
import { Empty, Spinner, Tabs, Tag } from '../ui/common'
import { useMenu } from '../ui/ContextMenu'
import type { MenuEntry } from '../ui/ContextMenu'
import { prompt, toast } from '../ui/overlays'
import { isViewer } from '../lib/session'
import { ExposureList, accessSummary, exposureMenu, openExpose } from '../components/Expose'
import type { Exposure } from '../components/Expose'
import { toggleShortcut, useShortcuts } from '../components/Shortcuts'
import { DurationTag, TunnelList, closeTunnelAsk, durationText, editTunnel, openTunnelDialog, tunnelAddr, tunnelURL } from '../components/Tunnels'
import type { Tunnel } from '../components/Tunnels'

interface JumpEntry {
  container_id: string
  container: string
  stack?: string
  service?: string
  image: string
  state: string
  port: number
  public_port?: number
  host_ip?: string
  proto: string
  kind: 'http' | 'tcp'
  scheme?: string
  url?: string
  app?: string
  conn?: string
  missing?: string[]
  masked?: boolean
}

interface JumpData {
  host: string
  via: 'local' | 'direct' | 'agent' | ''
  tunnel: boolean
  entries: JumpEntry[]
}

// Overrides are keyed by stack/service (or container name), not by id, so
// they survive a recreate.
const linkKey = (ep: string, e: JumpEntry) => `${ep}/${e.stack && e.service ? e.stack + '/' + e.service : e.container}/${e.port}`

const isLoopback = (ip?: string) => !!ip && (ip.startsWith('127.') || ip === '::1')

function addr(e: JumpEntry, host: string) {
  // published: reachable at the host; internal: only from the container's networks
  return e.public_port ? { host: isLoopback(e.host_ip) ? '127.0.0.1' : host, port: e.public_port } : { host: e.service || e.container, port: e.port }
}

function defaultURL(e: JumpEntry, host: string) {
  if (e.url) return e.url
  const a = addr(e, host)
  return `${e.scheme || 'http'}://${a.host}:${a.port}`
}

const tunURL = (e: JumpEntry, tn: Tunnel) => tunnelURL(tn, e.scheme || 'http')

function connString(e: JumpEntry, host: string, tn?: Tunnel) {
  if (!e.conn) return ''
  const a = tn ? tunnelAddr(tn) : addr(e, host)
  return e.conn.replaceAll('{host}', a.host).replaceAll('{port}', String(a.port))
}

function Owner({ e }: { e: JumpEntry }) {
  return (
    <span className="jump-owner">
      <strong className="ellipsis">{e.service || e.container}</strong>
      {e.stack && <span className="muted small ellipsis">{e.stack}</span>}
    </span>
  )
}

export function Jump({ ep }: { ep: string }) {
  const { data, loading, error } = useResource<JumpData>(`/e/${enc(ep)}/jump`, { poll: 30000 })
  const [tab, setTab] = usePref<'http' | 'tcp' | 'tunnels'>('jump.tab', 'http')
  const [tunTab, setTunTab] = usePref<'private' | 'public'>('jump.tunnels', 'private')
  const [allHosts, setAllHosts] = usePref('jump.tunnels.all', false)
  const [internal, setInternal] = usePref('jump.internal', false)
  const [hosts, setHosts] = usePref<Record<string, string>>('jump.hosts', {})
  const [links, setLinks] = usePref<Record<string, string>>('jump.links', {})
  const [q, setQ] = useState('')
  const [opening, setOpening] = useState<string | null>(null)
  const { data: tunnels } = useResource<Tunnel[]>('/tunnels', { poll: 30000 })
  const { data: exposures } = useResource<Exposure[]>('/exposures', { poll: 30000 })
  const { data: menuData } = useShortcuts()
  const menu = useMenu()

  // behind an agent the node's address is unknown unless it connected from a
  // routable one; the browser's own host would be a lie there
  const unknownHost = !hosts[ep] && !data?.host && data?.via === 'agent'
  const host = hosts[ep] || data?.host || (unknownHost ? t('<host>') : location.hostname)
  const all = data?.entries || []
  const visible = useMemo(() => {
    const ql = q.toLowerCase().trim()
    return all.filter((e) => {
      if (!internal && !e.public_port) return false
      if (!ql) return true
      return `${e.container} ${e.service ?? ''} ${e.stack ?? ''} ${e.image} ${e.app ?? ''} ${e.port} ${e.public_port ?? ''}`.toLowerCase().includes(ql)
    })
  }, [all, q, internal])
  const http = visible.filter((e) => e.kind === 'http')
  const tcp = visible.filter((e) => e.kind === 'tcp')

  const urlOf = (e: JumpEntry) => links[linkKey(ep, e)] || defaultURL(e, host)
  const ekey = (e: JumpEntry) => e.container_id + ':' + e.port + '/' + e.proto
  const tunOf = (e: JumpEntry) => tunnels?.find((x) => x.endpoint === ep && x.container === e.container && x.port === e.port)
  const exposedOf = (e: JumpEntry) => (exposures || []).filter((x) => x.endpoint === ep && x.container === e.container && x.port === e.port)
  const canTunnel = !!data?.tunnel && !isViewer()
  // Nodes behind an agent (often NAT) and internal ports are reached through
  // a tunnel unless someone set a link by hand.
  const preferTunnel = (e: JumpEntry) => canTunnel && e.proto === 'tcp' && !links[linkKey(ep, e)] && !e.url && (data?.via === 'agent' || !e.public_port)
  const openTunnel = async (e: JumpEntry) => {
    setOpening(ekey(e))
    try {
      const tn = await api.post<Tunnel>(`/e/${enc(ep)}/tunnels`, { container_id: e.container_id, container: e.container, port: e.port })
      invalidate('/tunnels')
      return tn
    } finally {
      setOpening(null)
    }
  }
  const openViaTunnel = (e: JumpEntry) => {
    const existing = tunOf(e)
    if (existing) return void window.open(tunURL(e, existing), '_blank', 'noopener')
    // open the tab now, inside the click, or the popup blocker eats it
    const w = window.open('about:blank', '_blank')
    openTunnel(e)
      .then((tn) => {
        if (!w) return void window.open(tunURL(e, tn), '_blank', 'noopener')
        w.opener = null
        w.location.href = tunURL(e, tn)
      })
      .catch((err) => (w?.close(), toast.err(err)))
  }
  const closeTunnel = closeTunnelAsk
  const editLink = async (e: JumpEntry) => {
    const k = linkKey(ep, e)
    const v = await prompt({
      title: t('Custom link for {0}', `${e.service || e.container}:${e.port}`),
      label: 'URL',
      value: urlOf(e),
      placeholder: defaultURL(e, host),
      help: t('Leave it empty to go back to the detected address.'),
    })
    if (v === null) return
    const next = { ...links }
    if (v.trim() && v.trim() !== defaultURL(e, host)) next[k] = v.trim()
    else delete next[k]
    setLinks(next)
  }
  const resetLink = (e: JumpEntry) => {
    const next = { ...links }
    delete next[linkKey(ep, e)]
    setLinks(next)
  }
  const editHost = async () => {
    const v = await prompt({
      title: t('Host for links'),
      label: t('Hostname or IP'),
      value: host,
      placeholder: data?.host || location.hostname,
      help: t('Address used to reach published ports on {0}. Leave it empty to detect it.', ep),
    })
    if (v === null) return
    const next = { ...hosts }
    if (v.trim()) next[ep] = v.trim()
    else delete next[ep]
    setHosts(next)
  }

  const common = (e: JumpEntry): MenuEntry[] => [
    'sep',
    { label: t('Go to the container'), onClick: () => go(ep, 'containers', e.container_id) },
    e.stack && { label: t('Go to the stack'), onClick: () => go(ep, 'stacks', e.stack!) },
  ]
  const tunnelMenu = (e: JumpEntry): MenuEntry[] => {
    const tn = tunOf(e)
    return [
      canTunnel && 'sep',
      canTunnel && !tn && { label: t('Open a tunnel…'), icon: <ArrowLeftRight size={14} />, onClick: () => openTunnelDialog(ep, { container: e.container, container_id: e.container_id, port: e.port, scheme: e.scheme }) },
      tn && canTunnel && { label: t('Tunnel duration and access…'), icon: <Pencil size={14} />, onClick: () => editTunnel(tn) },
      tn && canTunnel && { label: t('Close tunnel :{0}', tn.local_port), danger: true, onClick: () => closeTunnel(tn) },
    ]
  }
  const exposeMenu = (e: JumpEntry): MenuEntry[] => {
    const xs = exposedOf(e)
    return [
      canTunnel && 'sep',
      canTunnel && { label: t('Expose to the internet…'), icon: <Cloud size={14} />, onClick: () => openExpose(ep, e.container, e.port, e.scheme) },
      ...xs.map((x) => ({ label: x.hostname || x.url || t('Exposed'), icon: <Globe size={14} />, submenu: exposureMenu(x) }) as MenuEntry),
    ]
  }
  // start menu entries: published web ports of this machine's containers
  const shortcutMenu = (e: JumpEntry): MenuEntry[] => {
    const c = menuData?.info.supported && e.public_port ? menuData.candidates.find((x) => x.endpoint === ep && x.container === e.container && x.port === e.port) : undefined
    if (!c || !menuData) return []
    if (menuData.all) return ['sep', { label: t('In the start menu (all containers)'), icon: <AppWindow size={14} />, disabled: true }]
    return ['sep', { label: c.on ? t('Remove from the start menu') : t('Add to the start menu'), icon: <AppWindow size={14} />, onClick: () => toggleShortcut(menuData, c, !c.on) }]
  }
  const httpMenu = (e: JumpEntry): MenuEntry[] => [
    { header: `${e.service || e.container}:${e.port}` },
    { label: t('Open in a new tab'), icon: <ExternalLink size={14} />, onClick: () => (preferTunnel(e) ? openViaTunnel(e) : window.open(urlOf(e), '_blank', 'noopener')) },
    canTunnel && !preferTunnel(e) && { label: t('Open via tunnel'), icon: <ArrowLeftRight size={14} />, onClick: () => openViaTunnel(e) },
    { label: t('Copy URL'), icon: <Copy size={14} />, onClick: () => copy(urlOf(e)) },
    { label: t('Edit link…'), icon: <Pencil size={14} />, onClick: () => editLink(e) },
    links[linkKey(ep, e)] && { label: t('Reset link'), onClick: () => resetLink(e) },
    ...tunnelMenu(e),
    ...exposeMenu(e),
    ...shortcutMenu(e),
    ...common(e),
  ]
  const tcpMenu = (e: JumpEntry): MenuEntry[] => {
    const tn = tunOf(e)
    const a = tn ? tunnelAddr(tn) : addr(e, host)
    return [
      { header: `${e.service || e.container}:${e.port}/${e.proto}` },
      { label: t('Copy host:port'), onClick: () => copy(`${a.host}:${a.port}`) },
      e.conn && { label: t('Copy connection string'), icon: <Copy size={14} />, onClick: () => copy(connString(e, host, tn)) },
      ...(e.proto === 'tcp' ? tunnelMenu(e) : []),
      ...common(e),
    ]
  }

  return (
    <div className="page jump">
      <div className="page-head">
        <div className="page-title">
          <Zap size={20} />
          <h1>{t('Jump')}</h1>
          {loading && <Spinner />}
        </div>
        <div className="page-actions">
          <div className="search">
            <Search size={14} />
            <input placeholder={t('Search by service, image, port…')} value={q} onChange={(e) => setQ(e.target.value)} />
            {q && (
              <button className="icon-btn" onClick={() => setQ('')}>
                <X size={13} />
              </button>
            )}
          </div>
          <button className="btn btn-sm" onClick={editHost} title={t('Host for links')}>
            <Server size={14} /> <span className={`mono${unknownHost ? ' tone-warn' : ''}`}>{unknownHost ? t('unknown host') : host}</span>
            {hosts[ep] && <Pencil size={12} className="muted" />}
          </button>
          <button className={`btn btn-sm${internal ? ' on' : ''}`} onClick={() => setInternal(!internal)} title={t('Ports not published on the host: reachable only from the containers’ networks')}>
            <EyeOff size={14} /> {t('Internal ports')}
          </button>
        </div>
      </div>
      <Tabs
        active={tab}
        onChange={(id) => setTab(id as 'http' | 'tcp' | 'tunnels')}
        tabs={[
          { id: 'http', label: <><Globe size={13} /> HTTP</>, count: http.length },
          { id: 'tcp', label: <><Plug size={13} /> TCP/UDP</>, count: tcp.length },
          { id: 'tunnels', label: <><ArrowLeftRight size={13} /> {t('Tunnels')}</>, count: (tunnels || []).filter((x) => x.endpoint === ep).length + (exposures || []).filter((x) => x.endpoint === ep).length },
        ]}
      />
      <div className="page-body">
        {error && <div className="alert alert-bad">{error.message}</div>}
        {!data && !error && <div className="center"><Spinner /></div>}
        {data && tab === 'http' && (
          http.length === 0 ? (
            <Empty icon={<Globe size={28} />} title={t('No HTTP ports')}>{q ? t('Nothing matches the search.') : t('No running container publishes a web port.')}</Empty>
          ) : (
            <div className="jump-grid">
              {http.map((e) => {
                const custom = !!links[linkKey(ep, e)]
                const tn = tunOf(e)
                const viaTunnel = preferTunnel(e)
                const url = viaTunnel && tn ? tunURL(e, tn) : urlOf(e)
                const reachable = !!e.public_port || custom || !!e.url || viaTunnel
                return (
                  <div key={e.container_id + e.port + e.proto} className={`jump-card${reachable ? '' : ' disabled'}`} onContextMenu={menu(() => httpMenu(e))}>
                    <a
                      className="jump-link"
                      href={reachable ? url : undefined}
                      target="_blank"
                      rel="noopener noreferrer"
                      title={viaTunnel && !tn ? t('Opens a tunnel through ducktainer') : reachable ? url : t('Internal port: not reachable from the browser')}
                      onClick={(ev) => {
                        if (viaTunnel && !tn) {
                          ev.preventDefault()
                          openViaTunnel(e)
                        }
                      }}
                    >
                      <span className="jump-head">
                        <Owner e={e} />
                        {opening === ekey(e) ? <Spinner /> : reachable && (viaTunnel ? <ArrowLeftRight size={14} className="muted" /> : <ExternalLink size={14} className="muted" />)}
                      </span>
                      <span className="mono small ellipsis jump-url">{viaTunnel && !tn ? t('via tunnel') : url}</span>
                      <span className="tags">
                        <Tag mono>{e.public_port ? `${e.public_port} → ${e.port}` : `${e.port}`}</Tag>
                        {e.app && <Tag>{e.app}</Tag>}
                        {custom && <Tag tone="accent">{t('custom')}</Tag>}
                        {tn && <Tag tone="ok" title={durationText(tn)} onContextMenu={menu(() => tunnelMenu(e))}>{t('tunnel :{0}', tn.local_port)}</Tag>}
                        {tn && tn.mode !== 'idle' && <DurationTag tn={tn} />}
                        {!custom && e.url && <Tag tone="accent">{t('label')}</Tag>}
                        {exposedOf(e).map((x) => (
                          <Tag key={x.id} tone={x.access.login || x.has_link || x.access.ips?.length ? 'ok' : 'warn'} title={`${x.url || t('waiting for an address…')} · ${accessSummary(x)}`} onContextMenu={menu(() => exposureMenu(x))}>
                            <Cloud size={11} /> {x.hostname || t('internet')}
                          </Tag>
                        ))}
                        {!e.public_port && <Tag>{t('internal')}</Tag>}
                        {isLoopback(e.host_ip) && <Tag tone="warn" title={t('Published only on the loopback of the host')}>localhost</Tag>}
                      </span>
                    </a>
                    <button className="icon-btn jump-edit" title={t('Edit link…')} onClick={() => editLink(e)}>
                      <Pencil size={13} />
                    </button>
                  </div>
                )
              })}
            </div>
          )
        )}
        {tab === 'tunnels' && (
          <div className="tunnels-tab">
            <div className="row gap">
              <div className="seg seg-text">
                <button className={tunTab === 'private' ? 'on' : ''} onClick={() => setTunTab('private')} title={t('Port forwards through this ducktainer server')}>
                  <Lock size={13} /> {t('Private tunnels')} <span className="muted">{(tunnels || []).filter((x) => allHosts || x.endpoint === ep).length}</span>
                </button>
                <button className={tunTab === 'public' ? 'on' : ''} onClick={() => setTunTab('public')} title={t('Published on the internet through Cloudflare')}>
                  <Cloud size={13} /> {t('Public tunnels')} <span className="muted">{(exposures || []).filter((x) => allHosts || x.endpoint === ep).length}</span>
                </button>
              </div>
              <span className="grow" />
              <label className="check small"><input type="checkbox" checked={allHosts} onChange={(e) => setAllHosts(e.target.checked)} /> {t('All hosts')}</label>
            </div>
            {tunTab === 'private' ? <TunnelList ep={allHosts ? undefined : ep} all={allHosts && !isViewer()} /> : <ExposureList ep={allHosts ? undefined : ep} />}
          </div>
        )}
        {data && tab === 'tcp' && (
          tcp.length === 0 ? (
            <Empty icon={<Plug size={28} />} title={t('No TCP/UDP ports')}>{q ? t('Nothing matches the search.') : t('No running container publishes a non-web port.')}</Empty>
          ) : (
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr>
                    <th>{t('Service')}</th>
                    <th>{t('Port')}</th>
                    <th>{t('Detected as')}</th>
                    <th>{t('Connection')}</th>
                  </tr>
                </thead>
                <tbody>
                  {tcp.map((e) => {
                    const tn = tunOf(e)
                    const a = tn ? tunnelAddr(tn) : addr(e, host)
                    const conn = connString(e, host, tn)
                    return (
                      <tr key={e.container_id + e.port + e.proto} onContextMenu={menu(() => tcpMenu(e))}>
                        <td><Owner e={e} /></td>
                        <td>
                          <span className="tags">
                            <Tag mono>{e.public_port ? `${e.public_port} → ${e.port}/${e.proto}` : `${e.port}/${e.proto}`}</Tag>
                            {!e.public_port && <Tag>{t('internal')}</Tag>}
                            {isLoopback(e.host_ip) && <Tag tone="warn">localhost</Tag>}
                            {tn && (
                              <Tag tone="ok" title={durationText(tn)}>
                                {t('tunnel :{0}', tn.local_port)}
                              </Tag>
                            )}
                            {tn ? (
                              <button className="icon-btn" title={t('Close tunnel :{0}', tn.local_port)} onClick={() => closeTunnel(tn)}><X size={13} /></button>
                            ) : (
                              canTunnel && e.proto === 'tcp' && (
                                <button className="btn btn-sm btn-ghost" disabled={opening === ekey(e)} title={t('Forward this port through ducktainer')} onClick={() => openTunnelDialog(ep, { container: e.container, container_id: e.container_id, port: e.port })}>
                                  {opening === ekey(e) ? <Spinner /> : <ArrowLeftRight size={13} />} {t('Tunnel')}
                                </button>
                              )
                            )}
                          </span>
                        </td>
                        <td>{e.app ? e.app : <span className="muted">{t('unknown')}</span>}</td>
                        <td className="jump-conn">
                          {conn ? (
                            <span className="jump-conn-row">
                              <code className="ellipsis" title={conn}>{conn}</code>
                              <button className="icon-btn" title={t('Copy connection string')} onClick={() => (copy(conn), e.masked && toast.info(t('The password is hidden for read-only users')))}>
                                <Copy size={13} />
                              </button>
                              {e.masked && <span title={t('The password is hidden for read-only users')}><Lock size={13} className="muted" /></span>}
                              {e.missing?.length ? <Tag tone="warn" title={t('Not found in the compose environment')}>{t('missing: {0}', e.missing.join(', '))}</Tag> : null}
                            </span>
                          ) : (
                            <span className="jump-conn-row">
                              <span className="muted">{t('Unknown connection string')}</span>
                              <button className="link mono small" onClick={() => copy(`${a.host}:${a.port}`)}>{`${a.host}:${a.port}`}</button>
                            </span>
                          )}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )
        )}
        {data && isViewer() && tab === 'tcp' && tcp.some((e) => e.masked) && (
          <p className="muted small">{t('Passwords are only shown to administrators.')}</p>
        )}
      </div>
    </div>
  )
}
