import { useEffect, useState } from 'react'
import { Shield, Users, Globe, KeyRound, ClipboardCopy, Check, Plus, X, AlertTriangle, Terminal, RefreshCw, UserPlus, Info } from 'lucide-react'
import { api } from '../lib/api'
import { useResource } from '../lib/store'
import { copy } from '../lib/format'
import { Spinner, Tag } from '../ui/common'
import { useMenu } from '../ui/ContextMenu'
import { confirm, toast } from '../ui/overlays'
import { t } from '../i18n'

interface Group {
  name: string
  exists: boolean
  gid?: string
  members: string[]
}
interface Role {
  role: 'admin' | 'viewer'
  groups: Group[]
  users: string[]
}
interface AccessInfo {
  client_ip: string
  client_rule: string
  listen: string
  expose: string
  tls: boolean
  loopback_only: boolean
  allow: string[]
  allow_flags: string[]
  service_user: string
  pam: { enabled: boolean; built: boolean; mode: string }
  token_login: boolean
  allow_root: boolean
  roles: Role[]
}
interface AuthT {
  pam: boolean
  pam_service: string
  token_login: boolean
  admin_groups: string[] | null
  viewer_groups: string[] | null
  admin_users: string[] | null
  viewer_users: string[] | null
  allow_root: boolean
  session_hours: number
}
interface SettingsT {
  allow: string[]
  template_sources: unknown
  auth: AuthT
}

const list = (v: string) => v.split(/[\s,]+/).map((x) => x.trim()).filter(Boolean)

function Cmd({ children }: { children: string }) {
  const [done, setDone] = useState(false)
  return (
    <div className="cmd">
      <code>{children}</code>
      <button
        className="icon-btn"
        title={t('Copy')}
        onClick={() => {
          copy(children)
          setDone(true)
          setTimeout(() => setDone(false), 1200)
        }}
      >
        {done ? <Check size={14} /> : <ClipboardCopy size={14} />}
      </button>
    </div>
  )
}

function Step({ n, title, children }: { n: number; title: string; children: React.ReactNode }) {
  return (
    <li className="step">
      <span className="step-n">{n}</span>
      <div className="step-body">
        <strong>{title}</strong>
        {children}
      </div>
    </li>
  )
}

/** Who can sign in: roles, groups and members, plus a guided "add a user". */
function WhoCanSignIn({ info, settings, reload }: { info: AccessInfo; settings: SettingsT; reload: () => void }) {
  const [a, setA] = useState<AuthT>(settings.auth)
  const [busy, setBusy] = useState(false)
  const [username, setUsername] = useState('')
  const [role, setRole] = useState<'admin' | 'viewer'>('admin')
  const menu = useMenu()
  useEffect(() => setA(settings.auth), [settings.auth])
  const dirty = JSON.stringify(a) !== JSON.stringify(settings.auth)
  const save = async () => {
    setBusy(true)
    try {
      await api.put('/settings', { allow: settings.allow, template_sources: settings.template_sources, auth: a })
      toast.ok(t('Access updated'))
      reload()
    } catch (e) {
      toast.err(e)
    } finally {
      setBusy(false)
    }
  }
  const user = username.trim() || t('<user>')
  // suggest the dedicated dockler group; any existing one otherwise
  const roleGroups = info.roles.find((r) => r.role === role)?.groups ?? []
  const pick = roleGroups.find((g) => g.exists && g.name !== 'docker') ?? roleGroups.find((g) => g.exists) ?? roleGroups.find((g) => g.name !== 'docker') ?? roleGroups[0]
  const group = pick?.name ?? (role === 'admin' ? 'dockler-admins' : 'dockler-viewers')
  const groupMissing = !pick?.exists
  const roleLabel = (r: string) => (r === 'admin' ? t('Administrator') : t('Read-only'))
  const roleDesc = (r: string) =>
    r === 'admin'
      ? t('Full control: containers, stacks, images, terminals, settings and users.')
      : t('Can see everything except settings; cannot change anything, open terminals or download files.')

  return (
    <div className="panel access-panel">
      <h4><Users size={15} /> {t('Who can sign in')}</h4>
      {!info.pam.enabled ? (
        <div className="alert alert-warn">
          <AlertTriangle size={14} />
          <span>{info.pam.built ? t('Sign-in with system users is disabled: only the token works.') : t('This build has no PAM support (system users). Install the rpm/deb packages to sign in with system users.')}</span>
        </div>
      ) : (
        <p className="help">
          {t('People sign in with their operating-system account (PAM: local users, LDAP/SSSD/FreeIPA). Their role comes from the groups they belong to. Method:')} <code>{info.pam.mode}</code>
        </p>
      )}

      <div className="roles">
        {info.roles.map((r) => (
          <div key={r.role} className="role-card">
            <div className="row gap">
              <Shield size={15} className={r.role === 'admin' ? 'tone-ok' : 'muted'} />
              <strong>{roleLabel(r.role)}</strong>
            </div>
            <p className="help">{roleDesc(r.role)}</p>
            <div className="role-groups">
              {r.groups.map((g) => (
                <div key={g.name} className="role-group">
                  <div className="row gap">
                    <Tag tone={g.exists ? 'accent' : 'warn'} mono>{g.name}</Tag>
                    {!g.exists && <span className="tone-warn small">{t('group does not exist')}</span>}
                    {g.exists && <span className="muted small">{g.members.length === 1 ? t('1 member') : t('{0} members', g.members.length)}</span>}
                  </div>
                  <div className="members">
                    {g.members.map((m) => (
                      <span
                        key={m}
                        className="member"
                        onContextMenu={menu(() => [
                          { header: m },
                          { label: t('Copy name'), onClick: () => copy(m) },
                          { label: t('Copy command to remove from {0}', g.name), onClick: () => copy(`sudo gpasswd -d ${m} ${g.name}`) },
                        ])}
                      >
                        {m}
                      </span>
                    ))}
                    {g.exists && g.members.length === 0 && <span className="muted small">{t('nobody yet')}</span>}
                  </div>
                </div>
              ))}
              {r.users.length > 0 && (
                <div className="role-group">
                  <span className="muted small">{t('Individual users:')}</span>
                  <div className="members">{r.users.map((u) => <span key={u} className="member">{u}</span>)}</div>
                </div>
              )}
            </div>
          </div>
        ))}
      </div>
      {info.allow_root && <p className="help tone-warn">{t('root can sign in (allow_root).')}</p>}

      <h4 className="subhead"><UserPlus size={15} /> {t('Give someone access')}</h4>
      <div className="row gap wrap guide-inputs">
        <input placeholder={t('username')} value={username} onChange={(e) => setUsername(e.target.value.replace(/\s/g, ''))} />
        <div className="seg seg-text">
          <button className={role === 'admin' ? 'on' : ''} onClick={() => setRole('admin')}>{t('Administrator')}</button>
          <button className={role === 'viewer' ? 'on' : ''} onClick={() => setRole('viewer')}>{t('Read-only')}</button>
        </div>
      </div>
      <ol className="steps">
        <Step n={1} title={t('Make sure the system account exists')}>
          <p className="help">{t('Skip this if the person already has an account on this server (or in your LDAP/SSSD directory).')}</p>
          <Cmd>{`sudo useradd -m ${user} && sudo passwd ${user}`}</Cmd>
        </Step>
        <Step n={2} title={t('Add the account to the role group')}>
          {groupMissing && <Cmd>{`sudo groupadd ${group}`}</Cmd>}
          <Cmd>{`sudo usermod -aG ${group} ${user}`}</Cmd>
          {group === 'docker' && <p className="help tone-warn">{t('Members of docker can also use the docker CLI directly on this server.')}</p>}
          <p className="help">{t('Group membership is checked at sign-in and every 5 minutes, so removing someone revokes access quickly.')}</p>
        </Step>
        <Step n={3} title={t('Check it (optional)')}>
          <Cmd>{`sudo dockler auth test ${user}`}</Cmd>
        </Step>
        <Step n={4} title={t('Share the address')}>
          <p className="help">{t('They sign in with their own system password at:')} <code>{location.origin}</code></p>
        </Step>
      </ol>

      <h4 className="subhead"><Shield size={15} /> {t('Role mapping')}</h4>
      <div className="grid-2">
        {(['admin_groups', 'viewer_groups', 'admin_users', 'viewer_users'] as const).map((k) => (
          <label key={k} className="field">
            <span>{{ admin_groups: t('Administrator groups'), viewer_groups: t('Read-only groups'), admin_users: t('Administrator users'), viewer_users: t('Read-only users') }[k]}</span>
            <input className="mono" value={(a[k] || []).join(', ')} placeholder={k.endsWith('groups') ? 'group-a, group-b' : 'ana, pedro'} onChange={(e) => setA({ ...a, [k]: list(e.target.value) })} />
          </label>
        ))}
      </div>
      <div className="checks">
        <label className="check"><input type="checkbox" checked={a.pam} disabled={!info.pam.built && !a.pam} onChange={(e) => setA({ ...a, pam: e.target.checked })} /> {t('Allow system users')}</label>
        <label className="check"><input type="checkbox" checked={a.allow_root} onChange={(e) => setA({ ...a, allow_root: e.target.checked })} /> {t('Allow root')}</label>
        <label className="check">{t('Session length')} <input type="number" min={1} max={720} style={{ width: 70 }} value={a.session_hours} onChange={(e) => setA({ ...a, session_hours: Number(e.target.value) || 12 })} /> {t('hours')}</label>
      </div>
      <div className="row-actions">
        <button className="btn btn-primary" disabled={!dirty || busy} onClick={save}>{t('Save')}</button>
        <button className="btn" disabled={!dirty} onClick={() => setA(settings.auth)}>{t('Discard')}</button>
      </div>
    </div>
  )
}

/** From where: listen address, the caller's own IP and the allowlist. */
function FromWhere({ info, settings, reload }: { info: AccessInfo; settings: SettingsT; reload: () => void }) {
  const [rows, setRows] = useState<string[]>(info.allow)
  const [probe, setProbe] = useState('')
  const [probeRes, setProbeRes] = useState<{ ip: string; rule: string }[] | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => setRows(info.allow), [info.allow.join(',')])
  const dirty = rows.join(',') !== info.allow.join(',')
  const save = async (next = rows) => {
    setBusy(true)
    try {
      await api.put('/settings', { allow: next.map((x) => x.trim()).filter(Boolean), template_sources: settings.template_sources })
      toast.ok(t('Allowlist updated (applied immediately)'))
      reload()
    } catch (e) {
      toast.err(e)
    } finally {
      setBusy(false)
    }
  }
  const myNet = info.client_ip.includes('.') ? info.client_ip.split('.').slice(0, 3).join('.') + '.0/24' : ''
  const add = (v: string) => !rows.includes(v) && setRows([...rows, v])
  return (
    <div className="panel access-panel">
      <h4><Globe size={15} /> {t('From where')}</h4>
      <div className="facts">
        <div className="fact">
          <span className="muted small">{t('Listening on')}</span>
          <code>{info.listen || '—'}</code>
          <Tag tone={info.tls ? 'ok' : 'warn'}>{info.tls ? 'TLS' : t('no TLS')}</Tag>
        </div>
        <div className="fact">
          <span className="muted small">{t('Your address')}</span>
          <code>{info.client_ip}</code>
          {info.client_rule ? <Tag tone="ok">{t('allowed by {0}', info.client_rule)}</Tag> : <Tag tone="bad">{t('not in the list')}</Tag>}
        </div>
        {info.expose && (
          <div className="fact">
            <span className="muted small">{t('Docker API exposed on')}</span>
            <code>{info.expose}</code>
          </div>
        )}
      </div>
      {info.loopback_only && (
        <div className="alert alert-warn">
          <AlertTriangle size={14} />
          <span>
            {t('dockler only listens on this machine (loopback), so nobody else can reach it yet. Add allowed hosts below and restart the service so it listens on the network:')} <code>sudo systemctl restart dockler</code>
          </span>
        </div>
      )}
      <p className="help">
        {t('Connections from anywhere else are dropped before TLS. The local machine (loopback) is always allowed. Entries can be IP addresses, CIDR ranges or host names (re-resolved every minute, so dynamic DNS works).')}
      </p>
      <div className="allow-list">
        <div className="allow-row fixed"><code>127.0.0.1, ::1</code><span className="muted small">{t('loopback (always)')}</span></div>
        {info.allow_flags.map((f) => (
          <div key={f} className="allow-row fixed"><code>{f}</code><span className="muted small">{t('from --allow (command line)')}</span></div>
        ))}
        {rows.map((r, i) => (
          <div key={i} className="allow-row">
            <input className="mono" value={r} placeholder="192.168.1.0/24" onChange={(e) => setRows(rows.map((x, j) => (j === i ? e.target.value : x)))} />
            {r === info.client_rule && <Tag tone="ok">{t('you')}</Tag>}
            <button
              className="icon-btn"
              onClick={async () => {
                if (r === info.client_rule && !(await confirm({ title: t('Remove your own access?'), body: t('This entry is what allows your current connection. Removing it can lock you out.'), danger: true, confirm: t('Remove') }))) return
                setRows(rows.filter((_, j) => j !== i))
              }}
            >
              <X size={14} />
            </button>
          </div>
        ))}
      </div>
      <div className="row gap wrap">
        <button className="btn btn-sm btn-ghost" onClick={() => setRows([...rows, ''])}><Plus size={14} /> {t('Add entry')}</button>
        {info.client_rule !== 'loopback' && !rows.includes(info.client_ip) && <button className="btn btn-sm btn-ghost" onClick={() => add(info.client_ip)}><Plus size={14} /> {t('My IP ({0})', info.client_ip)}</button>}
        {myNet && info.client_rule !== 'loopback' && !rows.includes(myNet) && <button className="btn btn-sm btn-ghost" onClick={() => add(myNet)}><Plus size={14} /> {t('My network ({0})', myNet)}</button>}
        <span className="grow" />
        <button className="btn btn-primary btn-sm" disabled={!dirty || busy} onClick={() => save()}>{t('Save allowlist')}</button>
      </div>
      <div className="probe">
        <span className="muted small">{t('Would this address be allowed?')}</span>
        <form
          className="row gap"
          onSubmit={async (e) => {
            e.preventDefault()
            try {
              setProbeRes(await api.post('/settings/access/test', { address: probe }))
            } catch (err) {
              toast.err(err)
            }
          }}
        >
          <input className="mono" placeholder="10.0.0.25 / office.example.com" value={probe} onChange={(e) => (setProbe(e.target.value), setProbeRes(null))} />
          <button className="btn btn-sm" disabled={!probe.trim()}>{t('Check')}</button>
        </form>
        {probeRes?.map((p) => (
          <div key={p.ip} className="small">
            <code>{p.ip}</code> → {p.rule ? <span className="tone-ok">{t('allowed by {0}', p.rule)}</span> : <span className="tone-bad">{t('blocked')}</span>}
          </div>
        ))}
      </div>
      <p className="help">
        <Terminal size={12} /> {t('Also from the terminal:')} <code>sudo dockler allow add 192.168.1.0/24</code> · <code>sudo dockler allow ls</code>
      </p>
    </div>
  )
}

function TokenCard({ info }: { info: AccessInfo }) {
  const [token, setToken] = useState('')
  return (
    <div className="panel access-panel">
      <h4><KeyRound size={15} /> {t('Emergency token')}</h4>
      <p className="help">
        {t('A single secret that signs in as administrator and authorizes the API (Authorization: Bearer). Keep it for automation and emergencies. Show it with')} <code>sudo dockler token</code>.
      </p>
      <p className="small">{info.token_login ? <Tag tone="ok">{t('enabled')}</Tag> : <Tag tone="warn">{t('disabled')}</Tag>}</p>
      {token && (
        <div className="token-box mono">
          <span className="ellipsis">{token}</span>
          <button className="icon-btn" onClick={() => copy(token)}><ClipboardCopy size={14} /></button>
        </div>
      )}
      <button
        className="btn btn-danger-ghost"
        onClick={async () => {
          if (!(await confirm({ title: t('Rotate token'), body: t('Other browsers and dockler instances using this host via expose will need the new token.'), danger: true, confirm: t('Rotate') }))) return
          try {
            setToken((await api.post('/settings/rotate-token')).token)
          } catch (e) {
            toast.err(e)
          }
        }}
      >
        <RefreshCw size={14} /> {t('Rotate token')}
      </button>
      <p className="help"><Info size={12} /> {t('To allow only system users, disable it with')} <code>sudo dockler auth set --token-login off</code></p>
    </div>
  )
}

export function AccessSettings() {
  const info = useResource<AccessInfo>('/settings/access')
  const settings = useResource<SettingsT>('/settings')
  if (!info.data || !settings.data) return <Spinner />
  const reload = () => {
    info.reload()
    settings.reload()
  }
  return (
    <div className="access-grid">
      <WhoCanSignIn info={info.data} settings={settings.data} reload={reload} />
      <div className="access-col">
        <FromWhere info={info.data} settings={settings.data} reload={reload} />
        <TokenCard info={info.data} />
      </div>
    </div>
  )
}
