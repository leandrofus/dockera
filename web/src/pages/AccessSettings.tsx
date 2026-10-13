import { useEffect, useMemo, useState } from 'react'
import { Shield, ShieldCheck, Users, Globe, KeyRound, ClipboardCopy, Check, Plus, X, AlertTriangle, Terminal, RefreshCw, UserPlus, Lock, LockOpen, Eye, AppWindow } from 'lucide-react'
import { api } from '../lib/api'
import { useResource } from '../lib/store'
import { usePref } from '../lib/prefs'
import { copy } from '../lib/format'
import { Avatar, Empty, Spinner, Tabs, Tag } from '../ui/common'
import { useMenu } from '../ui/ContextMenu'
import { Drawer, confirm, toast } from '../ui/overlays'
import { t } from '../i18n'
import { StartMenu, useShortcuts } from '../components/Shortcuts'

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
interface Draft {
  allow: string[]
  auth: AuthT
}

type Sub = 'people' | 'network' | 'signin' | 'startmenu'

const roleLabel = (r: string) => (r === 'admin' ? t('Administrator') : t('Read-only'))

// ---------- small pieces ----------

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

function Switch({ checked, onChange, disabled }: { checked: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <button role="switch" aria-checked={checked} className={`switch${checked ? ' on' : ''}`} disabled={disabled} onClick={() => onChange(!checked)}>
      <span />
    </button>
  )
}

/** Chips with an inline input: Enter, comma or space adds, × removes. */
function ChipInput({ values, onChange, placeholder, tone }: { values: string[]; onChange: (v: string[]) => void; placeholder: string; tone?: (v: string) => string | undefined }) {
  const [text, setText] = useState('')
  const add = (raw: string) => {
    const items = raw.split(/[\s,]+/).map((x) => x.trim()).filter((x) => x && !values.includes(x))
    if (items.length) onChange([...values, ...items])
    setText('')
  }
  return (
    <div className="chip-input" onClick={(e) => (e.currentTarget.querySelector('input') as HTMLInputElement)?.focus()}>
      {values.map((v) => (
        <span key={v} className={`chip${tone?.(v) ? ' chip-' + tone(v) : ''}`}>
          <span className="mono">{v}</span>
          <button title={t('Remove')} onClick={() => onChange(values.filter((x) => x !== v))}>
            <X size={11} />
          </button>
        </span>
      ))}
      <input
        value={text}
        placeholder={values.length ? '' : placeholder}
        onChange={(e) => (/[\s,]$/.test(e.target.value) ? add(e.target.value) : setText(e.target.value))}
        onKeyDown={(e) => {
          if (e.key === 'Enter') (e.preventDefault(), add(text))
          else if (e.key === 'Backspace' && !text && values.length) onChange(values.slice(0, -1))
        }}
        onBlur={() => text && add(text)}
      />
    </div>
  )
}

function Setting({ title, desc, children, tone }: { title: string; desc: React.ReactNode; children: React.ReactNode; tone?: 'warn' }) {
  return (
    <div className={`setting${tone ? ' setting-' + tone : ''}`}>
      <div className="setting-text">
        <strong>{title}</strong>
        <span className="help">{desc}</span>
      </div>
      <div className="setting-control">{children}</div>
    </div>
  )
}

// ---------- status strip ----------

function StatusStrip({ info, draft, go }: { info: AccessInfo; draft: Draft; go: (s: Sub) => void }) {
  const items: { tone: 'ok' | 'warn' | 'bad'; icon: React.ReactNode; text: React.ReactNode; sub: Sub }[] = [
    info.tls
      ? { tone: 'ok', icon: <Lock size={13} />, text: <>TLS · <code>{info.listen}</code></>, sub: 'network' }
      : { tone: 'warn', icon: <LockOpen size={13} />, text: <>{t('no TLS')} · <code>{info.listen}</code></>, sub: 'network' },
    info.loopback_only
      ? { tone: 'warn', icon: <Globe size={13} />, text: t('Only reachable from this machine'), sub: 'network' }
      : info.client_rule
        ? { tone: 'ok', icon: <Globe size={13} />, text: <>{t('You')} <code>{info.client_ip}</code> · {info.client_rule}</>, sub: 'network' }
        : { tone: 'bad', icon: <Globe size={13} />, text: <>{t('You')} <code>{info.client_ip}</code> · {t('not in the list')}</>, sub: 'network' },
    {
      tone: draft.auth.pam || draft.auth.token_login ? 'ok' : 'bad',
      icon: <KeyRound size={13} />,
      text: [draft.auth.pam && t('system users'), draft.auth.token_login && t('token')].filter(Boolean).join(' + ') || t('no sign-in method'),
      sub: 'signin',
    },
  ]
  if (draft.auth.allow_root) items.push({ tone: 'warn', icon: <AlertTriangle size={13} />, text: t('root can sign in'), sub: 'signin' })
  if (info.pam.enabled && info.roles.some((r) => r.groups.some((g) => !g.exists))) items.push({ tone: 'warn', icon: <AlertTriangle size={13} />, text: t('missing groups'), sub: 'people' })
  return (
    <div className="acc-status">
      {items.map((it, i) => (
        <button key={i} className={`acc-pill acc-pill-${it.tone}`} onClick={() => go(it.sub)}>
          {it.icon}
          <span>{it.text}</span>
        </button>
      ))}
    </div>
  )
}

// ---------- people ----------

function GiveAccess({ info, onClose }: { info: AccessInfo; onClose: () => void }) {
  const [username, setUsername] = useState('')
  const [role, setRole] = useState<'admin' | 'viewer'>('admin')
  const user = username.trim() || t('<user>')
  // suggest the dedicated ducktainer group; any existing one otherwise
  const roleGroups = info.roles.find((r) => r.role === role)?.groups ?? []
  const pick = roleGroups.find((g) => g.exists && g.name !== 'docker') ?? roleGroups.find((g) => g.exists) ?? roleGroups.find((g) => g.name !== 'docker') ?? roleGroups[0]
  const group = pick?.name ?? (role === 'admin' ? 'ducktainer-admins' : 'ducktainer-viewers')
  return (
    <Drawer onClose={onClose} width={520}>
      <div className="acc-drawer">
        <div className="row gap">
          <UserPlus size={18} />
          <h3 className="grow">{t('Give someone access')}</h3>
          <button className="icon-btn" onClick={onClose}><X size={16} /></button>
        </div>
        <p className="help">{t('Access is granted on the server itself: ducktainer trusts system accounts and their groups.')}</p>
        <div className="row gap wrap">
          <input autoFocus className="grow" placeholder={t('username')} value={username} onChange={(e) => setUsername(e.target.value.replace(/\s/g, ''))} />
          <div className="seg seg-text">
            <button className={role === 'admin' ? 'on' : ''} onClick={() => setRole('admin')}>{t('Administrator')}</button>
            <button className={role === 'viewer' ? 'on' : ''} onClick={() => setRole('viewer')}>{t('Read-only')}</button>
          </div>
        </div>
        <ol className="steps">
          <li className="step">
            <span className="step-n">1</span>
            <div className="step-body">
              <strong>{t('Make sure the system account exists')}</strong>
              <p className="help">{t('Skip this if the person already has an account on this server (or in your LDAP/SSSD directory).')}</p>
              <Cmd>{`sudo useradd -m ${user} && sudo passwd ${user}`}</Cmd>
            </div>
          </li>
          <li className="step">
            <span className="step-n">2</span>
            <div className="step-body">
              <strong>{t('Add the account to the role group')}</strong>
              {!pick?.exists && <Cmd>{`sudo groupadd ${group}`}</Cmd>}
              <Cmd>{`sudo usermod -aG ${group} ${user}`}</Cmd>
              {group === 'docker' && <p className="help tone-warn">{t('Members of docker can also use the docker CLI directly on this server.')}</p>}
              <p className="help">{t('Group membership is checked at sign-in and every 5 minutes, so removing someone revokes access quickly.')}</p>
            </div>
          </li>
          <li className="step">
            <span className="step-n">3</span>
            <div className="step-body">
              <strong>{t('Check it (optional)')}</strong>
              <Cmd>{`sudo ducktainer auth test ${user}`}</Cmd>
            </div>
          </li>
          <li className="step">
            <span className="step-n">4</span>
            <div className="step-body">
              <strong>{t('Share the address')}</strong>
              <p className="help">{t('They sign in with their own system password at:')} <code>{location.origin}</code></p>
            </div>
          </li>
        </ol>
      </div>
    </Drawer>
  )
}

function People({ info, draft, setAuth, go }: { info: AccessInfo; draft: Draft; setAuth: (a: Partial<AuthT>) => void; go: (s: Sub) => void }) {
  const [giving, setGiving] = useState(false)
  const menu = useMenu()
  // one row per person, admin wins over read-only
  const people = useMemo(() => {
    const m = new Map<string, { role: 'admin' | 'viewer'; via: string[] }>()
    for (const r of info.roles) {
      const add = (u: string, via: string) => {
        const cur = m.get(u)
        if (!cur) m.set(u, { role: r.role, via: [via] })
        else if (cur.role === r.role) cur.via.push(via)
        else if (r.role === 'admin') m.set(u, { role: 'admin', via: [via] })
      }
      r.groups.forEach((g) => g.members.forEach((u) => add(u, g.name)))
      r.users.forEach((u) => add(u, ''))
    }
    return [...m.entries()].sort((a, b) => (a[1].role === b[1].role ? a[0].localeCompare(b[0]) : a[1].role === 'admin' ? -1 : 1))
  }, [info])
  const groupInfo = (name: string) => info.roles.flatMap((r) => r.groups).find((g) => g.name === name)
  const groupTone = (name: string) => {
    const g = groupInfo(name)
    return g && !g.exists ? 'warn' : undefined
  }
  const roles = [
    { role: 'admin' as const, groups: 'admin_groups' as const, users: 'admin_users' as const, icon: <ShieldCheck size={16} className="tone-ok" />, desc: t('Full control: containers, stacks, images, terminals, settings and users.') },
    { role: 'viewer' as const, groups: 'viewer_groups' as const, users: 'viewer_users' as const, icon: <Eye size={16} className="muted" />, desc: t('Can see everything except settings; cannot change anything, open terminals or download files.') },
  ]

  return (
    <div className="acc-section">
      {!info.pam.enabled && (
        <div className="alert alert-warn">
          <AlertTriangle size={14} />
          <span>
            {info.pam.built ? t('Sign-in with system users is disabled: only the token works.') : t('This build has no PAM support (system users). Install the rpm/deb packages to sign in with system users.')}{' '}
            {info.pam.built && <button className="link" onClick={() => go('signin')}>{t('Enable it')}</button>}
          </span>
        </div>
      )}

      <div className="acc-block">
        <div className="acc-block-head">
          <div>
            <h4><Users size={15} /> {t('People with access')} <span className="count">{people.length}</span></h4>
            <p className="help">{t('System accounts (PAM: local users, LDAP/SSSD/FreeIPA) whose groups map to a role.')} {info.pam.enabled && <code>{info.pam.mode}</code>}</p>
          </div>
          <button className="btn btn-primary btn-sm" onClick={() => setGiving(true)}><UserPlus size={14} /> {t('Give access')}</button>
        </div>
        {people.length === 0 ? (
          <Empty icon={<Users size={26} />} title={t('Nobody yet')}>{t('Add someone to one of the groups below, or sign in with the token.')}</Empty>
        ) : (
          <div className="table-wrap">
            <table className="table compact">
              <thead>
                <tr><th>{t('User')}</th><th>{t('Role')}</th><th>{t('Through')}</th></tr>
              </thead>
              <tbody>
                {people.map(([u, p]) => (
                  <tr
                    key={u}
                    onContextMenu={menu(() => [
                      { header: u },
                      { label: t('Copy name'), onClick: () => copy(u) },
                      ...p.via.filter(Boolean).map((g) => ({ label: t('Copy command to remove from {0}', g), onClick: () => copy(`sudo gpasswd -d ${u} ${g}`) })),
                    ])}
                  >
                    <td><span className="row gap"><Avatar name={u} size={22} /> <strong>{u}</strong></span></td>
                    <td><Tag tone={p.role === 'admin' ? 'ok' : undefined}>{roleLabel(p.role)}</Tag></td>
                    <td><span className="tags">{p.via.map((g, i) => (g ? <Tag key={i} mono>{g}</Tag> : <span key={i} className="muted small">{t('listed by name')}</span>))}</span></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {info.allow_root && <p className="help tone-warn">{t('root can sign in (allow_root).')}</p>}
      </div>

      <div className="acc-block">
        <h4><Shield size={15} /> {t('Who gets each role')}</h4>
        <div className="acc-roles">
          {roles.map((r) => (
            <div key={r.role} className="acc-role">
              <div className="row gap">{r.icon}<strong>{roleLabel(r.role)}</strong></div>
              <p className="help">{r.desc}</p>
              <label className="field">
                <span>{t('Groups')}</span>
                <ChipInput values={draft.auth[r.groups] || []} onChange={(v) => setAuth({ [r.groups]: v })} placeholder={t('add a group…')} tone={groupTone} />
              </label>
              {(draft.auth[r.groups] || []).some((g) => groupInfo(g) && !groupInfo(g)!.exists) && (
                <p className="help tone-warn">{t('Highlighted groups do not exist on this server yet.')}</p>
              )}
              <label className="field">
                <span>{t('Individual users')}</span>
                <ChipInput values={draft.auth[r.users] || []} onChange={(v) => setAuth({ [r.users]: v })} placeholder={t('add a user…')} />
              </label>
            </div>
          ))}
        </div>
      </div>
      {giving && <GiveAccess info={info} onClose={() => setGiving(false)} />}
    </div>
  )
}

// ---------- network ----------

function kindOf(v: string): { label: string; ok: boolean } {
  if (v === '*' || v === 'any') return { label: t('anyone'), ok: true }
  if (/^[\d.]+\/\d+$/.test(v) || /^[0-9a-f:]+\/\d+$/i.test(v)) return { label: 'CIDR', ok: true }
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(v) || (/^[0-9a-f:]+$/i.test(v) && v.includes(':'))) return { label: 'IP', ok: true }
  if (/^[a-z0-9]([a-z0-9-.]*[a-z0-9])?$/i.test(v)) return { label: 'host', ok: true }
  return { label: t('invalid'), ok: false }
}

function Network({ info, draft, setAllow }: { info: AccessInfo; draft: Draft; setAllow: (v: string[]) => void }) {
  const [entry, setEntry] = useState('')
  const [probe, setProbe] = useState('')
  const [probeRes, setProbeRes] = useState<{ ip: string; rule: string }[] | null>(null)
  const rows = draft.allow
  const myNet = info.client_ip.includes('.') ? info.client_ip.split('.').slice(0, 3).join('.') + '.0/24' : ''
  const add = (v: string) => {
    v = v.trim()
    if (v && !rows.includes(v)) setAllow([...rows, v])
    setEntry('')
  }
  const remove = async (r: string) => {
    if (r === info.client_rule && !(await confirm({ title: t('Remove your own access?'), body: t('This entry is what allows your current connection. Removing it can lock you out.'), danger: true, confirm: t('Remove') }))) return
    setAllow(rows.filter((x) => x !== r))
  }
  const k = kindOf(entry.trim())
  return (
    <div className="acc-section">
      {info.loopback_only && (
        <div className="alert alert-warn">
          <AlertTriangle size={14} />
          <span>
            {t('ducktainer only listens on this machine (loopback), so nobody else can reach it yet. Add allowed hosts below and restart the service so it listens on the network:')} <code>sudo systemctl restart ducktainer</code>
          </span>
        </div>
      )}
      <div className="acc-facts">
        <div className="acc-fact">
          <span className="muted small">{t('Listening on')}</span>
          <code>{info.listen || '—'}</code>
          <Tag tone={info.tls ? 'ok' : 'warn'}>{info.tls ? 'TLS' : t('no TLS')}</Tag>
        </div>
        <div className="acc-fact">
          <span className="muted small">{t('Your address')}</span>
          <code>{info.client_ip}</code>
          {info.client_rule ? <Tag tone="ok">{t('allowed by {0}', info.client_rule)}</Tag> : <Tag tone="bad">{t('not in the list')}</Tag>}
        </div>
        {info.expose && (
          <div className="acc-fact">
            <span className="muted small">{t('Docker API exposed on')}</span>
            <code>{info.expose}</code>
          </div>
        )}
      </div>

      <div className="acc-block">
        <h4><Globe size={15} /> {t('Allowed addresses')}</h4>
        <p className="help">{t('Connections from anywhere else are dropped before TLS. Host names are re-resolved every minute, so dynamic DNS works.')}</p>
        <div className="table-wrap">
          <table className="table compact acc-allow">
            <tbody>
              <tr className="fixed">
                <td><code>127.0.0.1, ::1</code></td>
                <td><Tag>loopback</Tag></td>
                <td className="muted small">{t('always allowed')}</td>
                <td />
              </tr>
              {info.allow_flags.map((f) => (
                <tr key={f} className="fixed">
                  <td><code>{f}</code></td>
                  <td><Tag>{kindOf(f).label}</Tag></td>
                  <td className="muted small">{t('from --allow (command line)')}</td>
                  <td />
                </tr>
              ))}
              {rows.map((r) => (
                <tr key={r} className={info.allow.includes(r) ? '' : 'added'}>
                  <td><code>{r}</code></td>
                  <td><Tag tone={kindOf(r).ok ? undefined : 'bad'}>{kindOf(r).label}</Tag></td>
                  <td className="small">
                    {r === info.client_rule && <Tag tone="ok">{t('you')}</Tag>}
                    {!info.allow.includes(r) && <span className="tone-accent">{t('new')}</span>}
                  </td>
                  <td className="right">
                    <button className="icon-btn" title={t('Remove')} onClick={() => remove(r)}><X size={14} /></button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <form className="acc-add" onSubmit={(e) => (e.preventDefault(), k.ok && add(entry))}>
          <input className="mono grow" placeholder="192.168.1.0/24 · 10.0.0.25 · vpn.example.com" value={entry} onChange={(e) => setEntry(e.target.value)} />
          {entry.trim() && <Tag tone={k.ok ? undefined : 'bad'}>{k.label}</Tag>}
          <button className="btn btn-sm" disabled={!entry.trim() || !k.ok}><Plus size={14} /> {t('Add')}</button>
        </form>
        <div className="row gap wrap">
          {info.client_rule !== 'loopback' && !rows.includes(info.client_ip) && <button className="btn btn-sm btn-ghost" onClick={() => add(info.client_ip)}><Plus size={13} /> {t('My IP ({0})', info.client_ip)}</button>}
          {myNet && info.client_rule !== 'loopback' && !rows.includes(myNet) && <button className="btn btn-sm btn-ghost" onClick={() => add(myNet)}><Plus size={13} /> {t('My network ({0})', myNet)}</button>}
        </div>
      </div>

      <div className="acc-block">
        <h4>{t('Would this address be allowed?')}</h4>
        <form
          className="acc-add"
          onSubmit={async (e) => {
            e.preventDefault()
            try {
              setProbeRes(await api.post('/settings/access/test', { address: probe }))
            } catch (err) {
              toast.err(err)
            }
          }}
        >
          <input className="mono grow" placeholder="10.0.0.25 / office.example.com" value={probe} onChange={(e) => (setProbe(e.target.value), setProbeRes(null))} />
          <button className="btn btn-sm" disabled={!probe.trim()}>{t('Check')}</button>
        </form>
        {probeRes?.map((p) => (
          <div key={p.ip} className="small">
            <code>{p.ip}</code> → {p.rule ? <span className="tone-ok">{t('allowed by {0}', p.rule)}</span> : <span className="tone-bad">{t('blocked')}</span>}
          </div>
        ))}
        <p className="help">{t('Checks against the saved list.')}</p>
      </div>
      <p className="help">
        <Terminal size={12} /> {t('Also from the terminal:')} <code>sudo ducktainer allow add 192.168.1.0/24</code> · <code>sudo ducktainer allow ls</code>
      </p>
    </div>
  )
}

// ---------- sign-in ----------

function SignIn({ info, draft, setAuth }: { info: AccessInfo; draft: Draft; setAuth: (a: Partial<AuthT>) => void }) {
  const [token, setToken] = useState('')
  const a = draft.auth
  return (
    <div className="acc-section">
      <div className="acc-block acc-settings">
        <h4><KeyRound size={15} /> {t('Sign-in methods')}</h4>
        <Setting
          title={t('System users')}
          desc={info.pam.built ? <>{t('Accounts of this server or its directory (LDAP/SSSD/FreeIPA), with the role of their groups.')} <code>{info.pam.mode}</code></> : t('This build has no PAM support (system users). Install the rpm/deb packages to sign in with system users.')}
        >
          <Switch checked={a.pam} disabled={!info.pam.built && !a.pam} onChange={(v) => setAuth({ pam: v })} />
        </Setting>
        <Setting title={t('Emergency token')} desc={<>{t('A single secret that signs in as administrator and authorizes the API (Authorization: Bearer). Only its hash is stored: create a new one with')} <code>sudo ducktainer token --rotate</code></>}>
          <Switch checked={a.token_login} onChange={(v) => setAuth({ token_login: v })} />
        </Setting>
        {!a.pam && !a.token_login && <div className="alert alert-bad"><AlertTriangle size={14} /> {t('Keep at least one sign-in method enabled.')}</div>}
        <Setting title={t('Allow root')} desc={t('Lets the root account sign in. Better to use a personal account in an admin group.')} tone={a.allow_root ? 'warn' : undefined}>
          <Switch checked={a.allow_root} onChange={(v) => setAuth({ allow_root: v })} />
        </Setting>
        <Setting title={t('Session length')} desc={t('After this, people sign in again. Removing someone from a group takes effect within 5 minutes anyway.')}>
          <span className="row gap">
            <input type="number" min={1} max={720} style={{ width: 76 }} value={a.session_hours} onChange={(e) => setAuth({ session_hours: Number(e.target.value) || 12 })} />
            <span className="muted small">{t('hours')}</span>
          </span>
        </Setting>
      </div>

      <div className="acc-block">
        <h4><RefreshCw size={15} /> {t('Rotate the token')}</h4>
        <p className="help">{t('Other browsers and ducktainer instances using this host via expose will need the new token.')}</p>
        {token && (
          <div className="token-box mono">
            <span className="ellipsis">{token}</span>
            <button className="icon-btn" onClick={() => copy(token)}><ClipboardCopy size={14} /></button>
          </div>
        )}
        <div>
          <button
            className="btn btn-danger-ghost btn-sm"
            onClick={async () => {
              if (!(await confirm({ title: t('Rotate token'), body: t('Other browsers and ducktainer instances using this host via expose will need the new token.'), danger: true, confirm: t('Rotate') }))) return
              try {
                setToken((await api.post('/settings/rotate-token')).token)
              } catch (e) {
                toast.err(e)
              }
            }}
          >
            <RefreshCw size={14} /> {t('Rotate token')}
          </button>
        </div>
      </div>
    </div>
  )
}

// ---------- page ----------

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b)

export function AccessSettings() {
  const info = useResource<AccessInfo>('/settings/access')
  const settings = useResource<SettingsT>('/settings')
  const [sub, setSub] = usePref<Sub>('access.tab', 'people')
  const menu = useShortcuts()
  const [draft, setDraft] = useState<Draft | null>(null)
  const [busy, setBusy] = useState(false)
  const saved: Draft | null = settings.data && info.data ? { allow: info.data.allow, auth: settings.data.auth } : null
  const savedKey = JSON.stringify(saved)
  // start over from the server whenever its state changes (load, save)
  useEffect(() => {
    if (saved) setDraft(saved)
  }, [savedKey])
  if (!info.data || !settings.data || !draft || !saved) return <Spinner />

  const dirtyAuth = !same(draft.auth, saved.auth)
  const dirtyAllow = !same(draft.allow, saved.allow)
  const dirty = { people: ['admin_groups', 'viewer_groups', 'admin_users', 'viewer_users'].some((k) => !same((draft.auth as any)[k], (saved.auth as any)[k])), network: dirtyAllow, signin: ['pam', 'token_login', 'allow_root', 'session_hours'].some((k) => !same((draft.auth as any)[k], (saved.auth as any)[k])) }
  const setAuth = (p: Partial<AuthT>) => setDraft({ ...draft, auth: { ...draft.auth, ...p } })
  const setAllow = (allow: string[]) => setDraft({ ...draft, allow })
  const save = async () => {
    setBusy(true)
    try {
      await api.put('/settings', { allow: draft.allow, template_sources: settings.data!.template_sources, auth: draft.auth })
      toast.ok(t('Access updated (applied immediately)'))
      info.reload()
      settings.reload()
    } catch (e) {
      toast.err(e)
    } finally {
      setBusy(false)
    }
  }
  const label = (id: Sub, text: string, icon: React.ReactNode) => (
    <>
      {icon} {text}
      {dirty[id as keyof typeof dirty] && <span className="dot dot-accent" title={t('Unsaved changes')} />}
    </>
  )
  const changes = Object.values(dirty).filter(Boolean).length

  return (
    <div className="acc">
      <StatusStrip info={info.data} draft={draft} go={setSub} />
      <Tabs
        active={sub}
        onChange={(id) => setSub(id as Sub)}
        tabs={[
          { id: 'people', label: label('people', t('People'), <Users size={13} />) },
          { id: 'network', label: label('network', t('Network'), <Globe size={13} />) },
          { id: 'signin', label: label('signin', t('Sign-in'), <KeyRound size={13} />) },
          ...(menu.data?.info.supported ? [{ id: 'startmenu', label: label('startmenu', t('Start menu'), <AppWindow size={13} />) }] : []),
        ]}
      />
      {sub === 'people' && <People info={info.data} draft={draft} setAuth={setAuth} go={setSub} />}
      {sub === 'network' && <Network info={info.data} draft={draft} setAllow={setAllow} />}
      {sub === 'signin' && <SignIn info={info.data} draft={draft} setAuth={setAuth} />}
      {sub === 'startmenu' && <StartMenu />}
      {(dirtyAuth || dirtyAllow) && (
        <div className="savebar dirty">
          <span>{changes === 1 ? t('Unsaved changes in 1 section') : t('Unsaved changes in {0} sections', changes)}</span>
          <span className="grow" />
          <button className="btn" disabled={busy} onClick={() => setDraft(saved)}>{t('Discard')}</button>
          <button className="btn btn-primary" disabled={busy || (!draft.auth.pam && !draft.auth.token_login)} onClick={save}>{busy ? <Spinner /> : t('Save')}</button>
        </div>
      )}
    </div>
  )
}
