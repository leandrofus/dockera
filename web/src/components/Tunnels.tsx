import { t } from '../i18n'
import { useState } from 'react'
import { ArrowLeftRight, Clock, Copy, ExternalLink, Infinity as Forever, Pencil, Timer, Users, User, X } from 'lucide-react'
import { api, enc } from '../lib/api'
import { invalidate, useResource } from '../lib/store'
import { copy, dateTime } from '../lib/format'
import { isViewer } from '../lib/session'
import { go } from '../lib/router'
import { Empty, Spinner, Tag } from '../ui/common'
import { useMenu } from '../ui/ContextMenu'
import type { MenuEntry } from '../ui/ContextMenu'
import { Modal, confirm, openDialog, toast } from '../ui/overlays'

export interface Tunnel {
  id: string
  endpoint: string
  container: string
  container_id?: string
  port: number
  local_port: number
  mode: 'idle' | 'until' | 'persistent'
  expires?: string
  shared: boolean
  client: string
  user: string
  created: string
  last_used?: string
  active: number
  mine: boolean
}

/** What a tunnel is opened to. */
export interface TunnelTarget {
  container: string
  container_id?: string
  port: number
  scheme?: string
}

// A tunnel listens on this ducktainer server, so it is reached at the page's host.
export const tunnelAddr = (tn: Tunnel) => ({ host: location.hostname, port: tn.local_port })
export const tunnelURL = (tn: Tunnel, scheme = 'http') => `${scheme}://${location.hostname}:${tn.local_port}`

export function durationText(tn: Tunnel) {
  if (tn.mode === 'persistent') return t('until closed')
  if (tn.mode === 'until') return t('until {0}', dateTime(tn.expires!))
  return t('closes after 30 min without use')
}

export function DurationTag({ tn }: { tn: Tunnel }) {
  if (tn.mode === 'persistent') return <Tag tone="accent" title={durationText(tn)}><Forever size={11} /> {t('persistent')}</Tag>
  if (tn.mode === 'until') return <Tag title={durationText(tn)}><Clock size={11} /> {t('until {0}', dateTime(tn.expires!))}</Tag>
  return <Tag title={durationText(tn)}><Timer size={11} /> {t('temporary')}</Tag>
}

const HOURS = [1, 4, 8, 24, 72, 168]
const hoursLabel = (h: number) => (h < 24 ? t('{0} h', h) : h === 24 ? t('1 day') : h === 168 ? t('1 week') : t('{0} days', h / 24))

function TunnelDialog({ ep, target, existing, onClose }: { ep: string; target?: TunnelTarget; existing?: Tunnel; onClose: () => void }) {
  const [mode, setMode] = useState<Tunnel['mode']>(existing?.mode || 'idle')
  const [hours, setHours] = useState(8)
  const [shared, setShared] = useState(existing?.shared || false)
  const [busy, setBusy] = useState(false)
  const name = existing ? `${existing.container}:${existing.port}` : `${target!.container}:${target!.port}`
  const submit = async () => {
    setBusy(true)
    try {
      const body = { mode, hours: mode === 'until' ? hours : 0, shared }
      const tn = existing
        ? await api.put<Tunnel>(`/tunnels/${existing.id}`, body)
        : await api.post<Tunnel>(`/e/${enc(ep)}/tunnels`, { container: target!.container, container_id: target!.container_id, port: target!.port, ...body })
      invalidate('/tunnels')
      onClose()
      toast.ok(existing ? t('Tunnel updated') : t('Tunnel open at {0}', `${location.hostname}:${tn.local_port}`))
    } catch (e) {
      toast.err(e)
    } finally {
      setBusy(false)
    }
  }
  const opt = (m: Tunnel['mode'], icon: React.ReactNode, title: string, text: string) => (
    <button type="button" className={`expose-kind${mode === m ? ' on' : ''}`} onClick={() => setMode(m)}>
      <strong>{icon} {title}</strong>
      <span className="muted small">{text}</span>
    </button>
  )
  return (
    <Modal
      title={existing ? t('Tunnel to {0}', name) : t('Open a tunnel to {0}', name)}
      icon={<ArrowLeftRight size={18} />}
      onClose={onClose}
      footer={
        <>
          <button className="btn" onClick={onClose}>{t('Cancel')}</button>
          <button className="btn btn-primary" disabled={busy} onClick={submit}>{busy ? <Spinner /> : <ArrowLeftRight size={14} />} {existing ? t('Save') : t('Open tunnel')}</button>
        </>
      }
    >
      <div className="expose-form">
        <p className="muted small">{t('Forwards a port of this ducktainer server to the container, also on hosts behind an agent or NAT. Only reachable by the addresses you allow.')}</p>
        <div className="field">
          <span>{t('How long')}</span>
          <div className="tunnel-modes">
            {opt('idle', <Timer size={13} />, t('Temporary'), t('Closes after 30 minutes without connections.'))}
            {opt('until', <Clock size={13} />, t('For some hours'), t('Closes at a fixed time, even if it is in use.'))}
            {opt('persistent', <Forever size={13} />, t('Persistent'), t('Stays open, also after restarts, until you close it.'))}
          </div>
          {mode === 'until' && (
            <select value={hours} onChange={(e) => setHours(+e.target.value)} style={{ marginTop: 8, alignSelf: 'flex-start' }}>
              {HOURS.map((h) => <option key={h} value={h}>{hoursLabel(h)}</option>)}
            </select>
          )}
        </div>
        <div className="field">
          <span>{t('Who can connect')}</span>
          <label className="check"><input type="radio" checked={!shared} onChange={() => setShared(false)} /> <User size={13} /> {t('Only from the address that opened it ({0})', existing?.client || t('yours'))}</label>
          <label className="check"><input type="radio" checked={shared} onChange={() => setShared(true)} /> <Users size={13} /> {t('Any address allowed to use ducktainer (Settings → Access → Network)')}</label>
          {mode === 'persistent' && !shared && <p className="help">{t('If your address changes, a persistent tunnel limited to it stops letting you in.')}</p>}
        </div>
      </div>
    </Modal>
  )
}

export function openTunnelDialog(ep: string, target: TunnelTarget) {
  openDialog((c) => <TunnelDialog ep={ep} target={target} onClose={c} />)
}

export function editTunnel(tn: Tunnel) {
  openDialog((c) => <TunnelDialog ep={tn.endpoint} existing={tn} onClose={c} />)
}

export async function closeTunnelAsk(tn: Tunnel) {
  if (tn.mode !== 'idle' || tn.active > 0) {
    const ok = await confirm({
      title: t('Close the tunnel to {0}?', `${tn.container}:${tn.port}`),
      body: tn.active > 0 ? t('{0} connections in use will be cut.', tn.active) : t('The port {0} stops working.', tn.local_port),
      confirm: t('Close tunnel'),
      danger: true,
    })
    if (!ok) return
  }
  try {
    await api.del(`/tunnels/${tn.id}`)
    invalidate('/tunnels')
  } catch (e) {
    toast.err(e)
  }
}

export function tunnelEntryMenu(tn: Tunnel): MenuEntry[] {
  const a = tunnelAddr(tn)
  return [
    { header: `${tn.container}:${tn.port} → :${tn.local_port}` },
    { label: t('Copy host:port'), icon: <Copy size={14} />, onClick: () => copy(`${a.host}:${a.port}`) },
    { label: t('Open in a new tab'), icon: <ExternalLink size={14} />, onClick: () => window.open(tunnelURL(tn), '_blank', 'noopener') },
    !isViewer() && 'sep',
    !isViewer() && { label: t('Duration and access…'), icon: <Pencil size={14} />, onClick: () => editTunnel(tn) },
    !isViewer() && { label: t('Close tunnel'), icon: <X size={14} />, danger: true, onClick: () => closeTunnelAsk(tn) },
    'sep',
    { label: t('Go to the container'), onClick: () => go(tn.endpoint, 'containers', tn.container) },
  ]
}

/** Private tunnels (port forwards through this server). */
export function TunnelList({ ep, all }: { ep?: string; all?: boolean }) {
  const { data, error } = useResource<Tunnel[]>(all ? '/tunnels?all=1' : '/tunnels', { poll: 10000 })
  const menu = useMenu()
  const list = (data || []).filter((x) => !ep || x.endpoint === ep)
  if (error) return <div className="alert alert-bad">{error.message}</div>
  if (!data) return <div className="center"><Spinner /></div>
  if (!list.length)
    return (
      <Empty icon={<ArrowLeftRight size={28} />} title={t('No open tunnels')}>
        {t('Right-click a port and choose “Open a tunnel…” to reach it through ducktainer: for a while, for some hours or until you close it.')}
      </Empty>
    )
  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr>
            <th>{t('Service')}</th>
            <th>{t('Address')}</th>
            <th>{t('Duration')}</th>
            <th>{t('Who can connect')}</th>
            <th>{t('Use')}</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {list.map((tn) => {
            const a = `${location.hostname}:${tn.local_port}`
            return (
              <tr key={tn.id} onContextMenu={menu(() => tunnelEntryMenu(tn))}>
                <td>
                  <strong>{tn.container}</strong>
                  <span className="muted mono small">:{tn.port}</span>
                  {!ep && <div className="muted small">{tn.endpoint}</div>}
                </td>
                <td className="jump-conn">
                  <span className="jump-conn-row">
                    <code>{a}</code>
                    <button className="icon-btn" title={t('Copy host:port')} onClick={() => copy(a)}><Copy size={13} /></button>
                    <a className="icon-btn" href={tunnelURL(tn)} target="_blank" rel="noopener noreferrer" title={t('Open in a new tab')}><ExternalLink size={13} /></a>
                  </span>
                </td>
                <td><DurationTag tn={tn} /></td>
                <td className="small">
                  {tn.shared ? <><Users size={12} /> {t('allowed addresses')}</> : <><User size={12} /> <code>{tn.client}</code>{tn.mine && <span className="muted"> ({t('you')})</span>}</>}
                  <div className="muted">{t('by {0}', tn.user)}</div>
                </td>
                <td className="small">
                  {tn.active > 0 ? <Tag tone="ok">{t('{0} connected', tn.active)}</Tag> : tn.last_used ? <span className="muted">{t('last used {0}', dateTime(tn.last_used))}</span> : <span className="muted">{t('not used yet')}</span>}
                </td>
                <td className="row-actions">
                  {!isViewer() && (
                    <>
                      <button className="icon-btn" title={t('Duration and access…')} onClick={() => editTunnel(tn)}><Pencil size={13} /></button>
                      <button className="icon-btn" title={t('Close tunnel')} onClick={() => closeTunnelAsk(tn)}><X size={13} /></button>
                    </>
                  )}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
