import { t } from '../i18n'
import { useState } from 'react'
import { Globe, Cloud, Copy, ExternalLink, Link2, Lock, ShieldCheck, Trash2, Pencil, AlertTriangle, Users, Network, Check, Unplug } from 'lucide-react'
import { api, enc } from '../lib/api'
import { invalidate, useResource } from '../lib/store'
import { copy, dateTime } from '../lib/format'
import { isViewer } from '../lib/session'
import { go } from '../lib/router'
import { Empty, Spinner, Tag } from '../ui/common'
import { useMenu } from '../ui/ContextMenu'
import type { MenuEntry } from '../ui/ContextMenu'
import { Modal, confirm, openDialog, toast } from '../ui/overlays'

export interface ExposeAccess {
  ips?: string[] | null
  login?: boolean
  users?: string[] | null
  groups?: string[] | null
}

export interface Exposure {
  id: string
  kind: 'quick' | 'domain'
  endpoint: string
  container: string
  port: number
  scheme?: string
  hostname?: string
  access: ExposeAccess
  expires?: string
  user: string
  created: string
  url: string
  state: string
  link?: string
  has_link: boolean
}

interface Zone {
  id: string
  name: string
  status: string
}

export interface CloudflareInfo {
  connected?: boolean
  account_name?: string
  tunnel_name?: string
  connector: string
  binary: string
  version: string
  pam: boolean
  zones?: Zone[]
  zones_error?: string
}

const isPublic = (x: Pick<Exposure, 'access' | 'has_link'>) => !x.access.login && !x.has_link && !x.access.ips?.length

/** Short description of who gets in. */
export function accessSummary(x: Pick<Exposure, 'access' | 'has_link'>) {
  if (isPublic(x)) return t('Anyone on the internet')
  const parts: string[] = []
  if (x.access.login) {
    const who = [...(x.access.users || []), ...(x.access.groups || []).map((g) => '@' + g)]
    parts.push(who.length ? t('sign-in: {0}', who.join(', ')) : t('sign-in: ducktainer users'))
  }
  if (x.has_link) parts.push(t('secret link'))
  if (x.access.ips?.length) parts.push(t('from {0}', x.access.ips.join(', ')))
  return parts.join(' · ')
}

function stateTag(x: Exposure) {
  if (x.state === 'connected') return <Tag tone="ok">{t('online')}</Tag>
  if (x.state === 'starting') return <Tag tone="warn">{t('starting')}</Tag>
  if (x.state === 'stopped') return <Tag>{t('stopped')}</Tag>
  return <Tag tone="bad" title={x.state}>{t('error')}</Tag>
}

const splitList = (v: string) => v.split(/[\s,]+/).map((s) => s.trim()).filter(Boolean)

interface AccessDraft {
  mode: 'public' | 'restricted'
  login: boolean
  users: string
  groups: string
  link: boolean
  ipsOn: boolean
  ips: string
}

const draftOf = (x?: Pick<Exposure, 'access' | 'has_link'>): AccessDraft => ({
  mode: !x || isPublic(x) ? (x ? 'public' : 'restricted') : 'restricted',
  login: x ? !!x.access.login : true,
  users: (x?.access.users || []).join(', '),
  groups: (x?.access.groups || []).join(', '),
  link: !!x?.has_link,
  ipsOn: !!x?.access.ips?.length,
  ips: (x?.access.ips || []).join('\n'),
})

const bodyOf = (d: AccessDraft, newLink = false) =>
  d.mode === 'public'
    ? {}
    : {
        login: d.login,
        users: d.login ? splitList(d.users) : [],
        groups: d.login ? splitList(d.groups) : [],
        link: d.link,
        new_link: newLink,
        ips: d.ipsOn ? splitList(d.ips) : [],
      }

const draftValid = (d: AccessDraft) => d.mode === 'public' || d.login || d.link || (d.ipsOn && splitList(d.ips).length > 0)

function AccessEditor({ d, set, pam }: { d: AccessDraft; set: (d: AccessDraft) => void; pam: boolean }) {
  return (
    <div className="expose-access">
      <div className="seg seg-text">
        <button type="button" className={d.mode === 'restricted' ? 'on' : ''} onClick={() => set({ ...d, mode: 'restricted' })}>
          <Lock size={13} /> {t('Restricted')}
        </button>
        <button type="button" className={d.mode === 'public' ? 'on' : ''} onClick={() => set({ ...d, mode: 'public' })}>
          <Globe size={13} /> {t('Public')}
        </button>
      </div>
      {d.mode === 'public' ? (
        <div className="alert alert-warn">
          <AlertTriangle size={15} />
          <span>{t('Anyone who finds the address can use the service. Only do this for services meant to be public, with their own login.')}</span>
        </div>
      ) : (
        <div className="expose-rules">
          <label className="check">
            <input type="checkbox" checked={d.login} disabled={!pam} onChange={(e) => set({ ...d, login: e.target.checked })} />
            <Users size={14} /> {t('Sign in with a system account')}
          </label>
          {!pam && <p className="help">{t('Needs system users, which are disabled in Settings → Access.')}</p>}
          {d.login && (
            <div className="grid-2 expose-indent">
              <label className="field">
                <span>{t('Users')}</span>
                <input className="mono" placeholder="ana, juan" value={d.users} onChange={(e) => set({ ...d, users: e.target.value })} />
              </label>
              <label className="field">
                <span>{t('Groups')}</span>
                <input className="mono" placeholder="developers" value={d.groups} onChange={(e) => set({ ...d, groups: e.target.value })} />
              </label>
              <p className="help span-2">{t('Leave both empty to let in everyone who can sign in to ducktainer.')}</p>
            </div>
          )}
          <label className="check">
            <input type="checkbox" checked={d.link} onChange={(e) => set({ ...d, link: e.target.checked })} />
            <Link2 size={14} /> {t('Secret link')}
          </label>
          <p className="help expose-indent">{t('Whoever opens the link gets in, without an account. Good to share with people outside.')}</p>
          <label className="check">
            <input type="checkbox" checked={d.ipsOn} onChange={(e) => set({ ...d, ipsOn: e.target.checked })} />
            <Network size={14} /> {t('Only from these addresses')}
          </label>
          {d.ipsOn && (
            <div className="expose-indent">
              <textarea className="mono" rows={3} placeholder={'203.0.113.7\n198.51.100.0/24\noffice.example.com'} value={d.ips} onChange={(e) => set({ ...d, ips: e.target.value })} />
              <p className="help">{t('IPs, CIDR ranges or hostnames (resolved periodically, so dynamic DNS works). Combined with the options above, both must pass.')}</p>
            </div>
          )}
          {!draftValid(d) && <p className="help tone-warn">{t('Pick at least one restriction, or make it public.')}</p>}
        </div>
      )}
    </div>
  )
}

const EXPIRY = [
  { v: 0, label: () => t('Never') },
  { v: 60, label: () => t('1 hour') },
  { v: 8 * 60, label: () => t('8 hours') },
  { v: 24 * 60, label: () => t('1 day') },
  { v: 7 * 24 * 60, label: () => t('1 week') },
]

function ExposeDialog({ ep, container, port, scheme, onClose }: { ep: string; container: string; port: number; scheme?: string; onClose: () => void }) {
  const { data: cf } = useResource<CloudflareInfo>('/cloudflare?zones=1')
  const [kind, setKind] = useState<'quick' | 'domain'>('quick')
  const [sub, setSub] = useState(container.replace(/[^a-z0-9-]/gi, '-').toLowerCase())
  const [zone, setZone] = useState('')
  const [expires, setExpires] = useState(0)
  const [https, setHttps] = useState(scheme === 'https')
  const [d, setD] = useState<AccessDraft>(draftOf())
  const [busy, setBusy] = useState(false)
  const zones = cf?.zones || []
  const zoneId = zone || zones[0]?.id || ''
  const zoneName = zones.find((z) => z.id === zoneId)?.name || ''
  const ok = draftValid(d) && (kind === 'quick' || (!!cf?.connected && !!zoneId))

  const submit = async () => {
    setBusy(true)
    try {
      const x = await api.post<Exposure>(`/e/${enc(ep)}/exposures`, {
        container,
        port,
        scheme: https ? 'https' : 'http',
        kind,
        subdomain: sub,
        zone_id: zoneId,
        expires_minutes: expires,
        access: bodyOf(d),
      })
      invalidate('/exposures')
      onClose()
      openDialog((c) => <ExposedDialog x={x} onClose={c} />)
    } catch (e) {
      toast.err(e)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      wide
      title={t('Expose {0} to the internet', `${container}:${port}`)}
      icon={<Cloud size={18} />}
      onClose={onClose}
      footer={
        <>
          <button className="btn" onClick={onClose}>{t('Cancel')}</button>
          <button className="btn btn-primary" disabled={busy || !ok} onClick={submit}>
            {busy ? <Spinner /> : <Globe size={14} />} {t('Expose')}
          </button>
        </>
      }
    >
      <div className="expose-form">
        <div className="expose-kinds">
          <button type="button" className={`expose-kind${kind === 'quick' ? ' on' : ''}`} onClick={() => setKind('quick')}>
            <strong>{t('Quick tunnel')}</strong>
            <span className="muted small">{t('No account. A random trycloudflare.com address that changes when ducktainer restarts.')}</span>
          </button>
          <button type="button" className={`expose-kind${kind === 'domain' ? ' on' : ''}`} onClick={() => setKind('domain')}>
            <strong>{t('Your domain')}</strong>
            <span className="muted small">{cf?.connected ? t('A fixed name on a domain of {0}.', cf.account_name || 'Cloudflare') : t('Connect a Cloudflare account to use your own domains.')}</span>
          </button>
        </div>

        {kind === 'domain' && !cf?.connected && (
          <div className="alert">
            <Cloud size={15} />
            <span>
              {t('No Cloudflare account connected.')}{' '}
              <button className="link" onClick={() => (onClose(), go(ep, 'settings', 'cloudflare'))}>{t('Connect one in Settings → Cloudflare')}</button>
            </span>
          </div>
        )}
        {kind === 'domain' && cf?.connected && (
          <div className="field">
            <span>{t('Address')}</span>
            <div className="row gap">
              <input className="mono grow" value={sub} placeholder={t('(the domain itself)')} onChange={(e) => setSub(e.target.value.toLowerCase())} />
              <span className="muted">.</span>
              <select value={zoneId} onChange={(e) => setZone(e.target.value)}>
                {zones.map((z) => <option key={z.id} value={z.id}>{z.name}</option>)}
              </select>
            </div>
            {cf.zones_error && <p className="help tone-bad">{cf.zones_error}</p>}
            {zoneName && <p className="help mono">https://{sub ? sub + '.' : ''}{zoneName}</p>}
          </div>
        )}

        <div className="grid-2">
          <label className="field">
            <span>{t('Close automatically')}</span>
            <select value={expires} onChange={(e) => setExpires(+e.target.value)}>
              {EXPIRY.map((o) => <option key={o.v} value={o.v}>{o.label()}</option>)}
            </select>
          </label>
          <label className="check" style={{ alignSelf: 'end', paddingBottom: 6 }} title={t('The container itself serves HTTPS (usually with a self-signed certificate)')}>
            <input type="checkbox" checked={https} onChange={(e) => setHttps(e.target.checked)} /> {t('The service speaks HTTPS')}
          </label>
        </div>

        <div className="field">
          <span>{t('Who can open it')}</span>
          <AccessEditor d={d} set={setD} pam={!!cf?.pam} />
        </div>
      </div>
    </Modal>
  )
}

function ExposedDialog({ x, onClose }: { x: Exposure; onClose: () => void }) {
  const { data } = useResource<Exposure[]>('/exposures', { poll: x.url ? 0 : 2000 })
  const cur = data?.find((e) => e.id === x.id) || x
  return (
    <Modal title={t('{0} is on the internet', `${cur.container}:${cur.port}`)} icon={<Globe size={18} />} onClose={onClose} footer={<button className="btn btn-primary" onClick={onClose}>{t('Done')}</button>}>
      <div className="kv">
        {cur.url ? (
          <CopyRow label={t('Address')} value={cur.url} open />
        ) : (
          <p className="row gap muted"><Spinner /> {t('Waiting for Cloudflare to assign the address…')}</p>
        )}
        {cur.link && <CopyRow label={t('Secret link')} value={cur.link} />}
        <p className="help">{t('Access')}: {accessSummary(cur)}</p>
        {cur.kind === 'domain' && <p className="help">{t('A new DNS name can take a minute to work everywhere.')}</p>}
      </div>
    </Modal>
  )
}

function CopyRow({ label, value, open }: { label: string; value: string; open?: boolean }) {
  const [done, setDone] = useState(false)
  return (
    <div className="field">
      <span>{label}</span>
      <div className="cmd">
        <code>{value}</code>
        {open && <a className="icon-btn" href={value} target="_blank" rel="noopener noreferrer" title={t('Open in a new tab')}><ExternalLink size={14} /></a>}
        <button className="icon-btn" title={t('Copy')} onClick={() => (copy(value), setDone(true), setTimeout(() => setDone(false), 1200))}>
          {done ? <Check size={14} /> : <Copy size={14} />}
        </button>
      </div>
    </div>
  )
}

function AccessDialog({ x, onClose }: { x: Exposure; onClose: () => void }) {
  const { data: cf } = useResource<CloudflareInfo>('/cloudflare')
  const [d, setD] = useState<AccessDraft>(draftOf(x))
  const [busy, setBusy] = useState(false)
  const save = async (newLink = false) => {
    setBusy(true)
    try {
      await api.put(`/exposures/${x.id}/access`, bodyOf(d, newLink))
      invalidate('/exposures')
      toast.ok(newLink ? t('New link created: the old one no longer works') : t('Access updated'))
      if (!newLink) onClose()
    } catch (e) {
      toast.err(e)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Modal
      title={t('Who can open {0}', x.hostname || `${x.container}:${x.port}`)}
      icon={<ShieldCheck size={18} />}
      onClose={onClose}
      footer={
        <>
          {x.has_link && d.mode === 'restricted' && d.link && (
            <button className="btn" disabled={busy} style={{ marginRight: 'auto' }} onClick={() => save(true)} title={t('The current link stops working')}>
              {t('New secret link')}
            </button>
          )}
          <button className="btn" onClick={onClose}>{t('Cancel')}</button>
          <button className="btn btn-primary" disabled={busy || !draftValid(d)} onClick={() => save()}>{t('Save')}</button>
        </>
      }
    >
      <AccessEditor d={d} set={setD} pam={!!cf?.pam} />
    </Modal>
  )
}

export function openExpose(ep: string, container: string, port: number, scheme?: string) {
  openDialog((c) => <ExposeDialog ep={ep} container={container} port={port} scheme={scheme} onClose={c} />)
}

export async function stopExposure(x: Exposure) {
  const ok = await confirm({
    title: t('Stop exposing {0}?', x.hostname || `${x.container}:${x.port}`),
    body: x.kind === 'domain' ? t('The address stops working and its DNS record is deleted.') : t('The address stops working.'),
    confirm: t('Stop exposing'),
    danger: true,
  })
  if (!ok) return
  try {
    await api.del(`/exposures/${x.id}`)
    invalidate('/exposures')
  } catch (e) {
    toast.err(e)
  }
}

export function exposureMenu(x: Exposure): MenuEntry[] {
  return [
    { header: x.hostname || x.url || `${x.container}:${x.port}` },
    x.url && { label: t('Open in a new tab'), icon: <ExternalLink size={14} />, onClick: () => window.open(x.url, '_blank', 'noopener') },
    x.url && { label: t('Copy address'), icon: <Copy size={14} />, onClick: () => copy(x.url) },
    x.link && { label: t('Copy secret link'), icon: <Link2 size={14} />, onClick: () => copy(x.link!) },
    !isViewer() && 'sep',
    !isViewer() && { label: t('Who can open it…'), icon: <Pencil size={14} />, onClick: () => openDialog((c) => <AccessDialog x={x} onClose={c} />) },
    !isViewer() && { label: t('Stop exposing'), icon: <Trash2 size={14} />, danger: true, onClick: () => stopExposure(x) },
    'sep',
    { label: t('Go to the container'), onClick: () => go(x.endpoint, 'containers', x.container) },
  ]
}

/** The exposures of a host (or all of them). */
export function ExposureList({ ep }: { ep?: string }) {
  const { data, error } = useResource<Exposure[]>('/exposures', { poll: 10000 })
  const menu = useMenu()
  const list = (data || []).filter((x) => !ep || x.endpoint === ep)
  if (error) return <div className="alert alert-bad">{error.message}</div>
  if (!data) return <div className="center"><Spinner /></div>
  if (!list.length)
    return (
      <Empty icon={<Cloud size={28} />} title={t('Nothing exposed to the internet')}>
        {t('Right-click a web port and choose “Expose to the internet…” to publish it through Cloudflare, publicly or only for some people.')}
      </Empty>
    )
  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr>
            <th>{t('Service')}</th>
            <th>{t('Address')}</th>
            <th>{t('Who can open it')}</th>
            <th>{t('State')}</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {list.map((x) => (
            <tr key={x.id} onContextMenu={menu(() => exposureMenu(x))}>
              <td>
                <strong>{x.container}</strong>
                <span className="muted mono small">:{x.port}</span>
                {!ep && <div className="muted small">{x.endpoint}</div>}
              </td>
              <td className="jump-conn">
                {x.url ? (
                  <span className="jump-conn-row">
                    <a className="mono small ellipsis" href={x.url} target="_blank" rel="noopener noreferrer">{x.url.replace(/^https:\/\//, '')}</a>
                    <button className="icon-btn" title={t('Copy address')} onClick={() => copy(x.url)}><Copy size={13} /></button>
                    {x.link && <button className="icon-btn" title={t('Copy secret link')} onClick={() => copy(x.link!)}><Link2 size={13} /></button>}
                  </span>
                ) : (
                  <span className="muted small">{t('waiting for an address…')}</span>
                )}
                <div className="tags">
                  <Tag>{x.kind === 'quick' ? t('quick tunnel') : t('domain')}</Tag>
                  {x.expires && <Tag title={dateTime(x.expires)}>{t('until {0}', dateTime(x.expires))}</Tag>}
                </div>
              </td>
              <td>
                <span className={isPublic(x) ? 'tone-warn' : ''}>
                  {isPublic(x) ? <Globe size={13} /> : <Lock size={13} />} {accessSummary(x)}
                </span>
              </td>
              <td>{stateTag(x)}</td>
              <td className="row-actions">
                {!isViewer() && (
                  <>
                    <button className="icon-btn" title={t('Who can open it…')} onClick={() => openDialog((c) => <AccessDialog x={x} onClose={c} />)}><Pencil size={13} /></button>
                    <button className="icon-btn" title={t('Stop exposing')} onClick={() => stopExposure(x)}><Trash2 size={13} /></button>
                  </>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

/** Settings → Cloudflare */
export function CloudflareSettings() {
  const { data, reload } = useResource<CloudflareInfo>('/cloudflare', { poll: 10000 })
  const [token, setToken] = useState('')
  const [accounts, setAccounts] = useState<{ id: string; name: string }[] | null>(null)
  const [busy, setBusy] = useState(false)
  const connect = async (accountId = '') => {
    setBusy(true)
    try {
      const res = await api.put<{ choose?: { id: string; name: string }[]; tunnel_name?: string }>('/cloudflare', { api_token: token, account_id: accountId })
      if (res.choose) return setAccounts(res.choose)
      setToken('')
      setAccounts(null)
      toast.ok(t('Connected: tunnel {0}', res.tunnel_name || ''))
      reload()
      invalidate('/cloudflare')
    } catch (e) {
      toast.err(e)
    } finally {
      setBusy(false)
    }
  }
  const disconnect = async () => {
    const ok = await confirm({
      title: t('Disconnect Cloudflare?'),
      body: t('Everything exposed on your domains stops working: their DNS records and the tunnel are deleted. Quick tunnels keep working.'),
      confirm: t('Disconnect'),
      danger: true,
    })
    if (!ok) return
    try {
      await api.del('/cloudflare')
      invalidate('/cloudflare', '/exposures')
      reload()
    } catch (e) {
      toast.err(e)
    }
  }
  if (!data) return <Spinner />
  return (
    <div className="settings-stack">
      <div className="panel">
        <h4><Cloud size={14} /> {t('Cloudflare account')}</h4>
        {data.connected ? (
          <div className="kv">
            <p>
              {t('Connected to {0}.', data.account_name || '')} {t('Tunnel')} <code>{data.tunnel_name}</code>{' '}
              {data.connector === 'connected' ? <Tag tone="ok">{t('online')}</Tag> : data.connector === '' ? <Tag>{t('idle')}</Tag> : data.connector === 'starting' ? <Tag tone="warn">{t('starting')}</Tag> : <Tag tone="bad" title={data.connector}>{t('error')}</Tag>}
            </p>
            {data.connector && !['connected', 'starting'].includes(data.connector) && <div className="alert alert-bad">{data.connector}</div>}
            <p className="help">{t('The tunnel only runs while something is exposed on your domains. Each exposure gets its own DNS name pointing at it.')}</p>
            <div className="row-actions">
              <button className="btn btn-danger" onClick={disconnect}><Unplug size={14} /> {t('Disconnect')}</button>
            </div>
          </div>
        ) : (
          <div className="kv">
            <p className="muted small">{t('Optional: quick tunnels work without an account. With one, services get fixed names on your own domains.')}</p>
            <ol className="help expose-steps">
              <li>
                {t('Create an API token at')}{' '}
                <a href="https://dash.cloudflare.com/profile/api-tokens" target="_blank" rel="noopener noreferrer">dash.cloudflare.com/profile/api-tokens</a>{' '}
                {t('with these permissions:')}
                <ul>
                  <li><code>Account › Cloudflare Tunnel › Edit</code></li>
                  <li><code>Zone › DNS › Edit</code></li>
                  <li><code>Zone › Zone › Read</code></li>
                </ul>
              </li>
              <li>{t('Paste it here. ducktainer creates a tunnel for this server and keeps the token encrypted.')}</li>
            </ol>
            <form className="row gap" onSubmit={(e) => (e.preventDefault(), connect())}>
              <input className="mono grow" type="password" autoComplete="off" placeholder={t('API token')} value={token} onChange={(e) => setToken(e.target.value)} />
              <button className="btn btn-primary" disabled={busy || !token.trim()}>{busy ? <Spinner /> : <Cloud size={14} />} {t('Connect')}</button>
            </form>
            {accounts && (
              <div className="field">
                <span>{t('The token can see several accounts. Which one?')}</span>
                <div className="row gap" style={{ flexWrap: 'wrap' }}>
                  {accounts.map((a) => <button key={a.id} className="btn" disabled={busy} onClick={() => connect(a.id)}>{a.name}</button>)}
                </div>
              </div>
            )}
          </div>
        )}
      </div>
      <div className="panel">
        <h4><Globe size={14} /> {t('Exposed services')}</h4>
        <ExposureList />
      </div>
      <p className="help">
        {t('Runs')} <code>cloudflared</code>{' '}
        {data.binary ? <>{t('from')} <code>{data.binary}</code></> : t('version {0}, downloaded (checksum verified) the first time it is needed', data.version)}.{' '}
        {t('Traffic goes from Cloudflare to this server and from here to the container, also on hosts behind an agent.')}
      </p>
    </div>
  )
}

