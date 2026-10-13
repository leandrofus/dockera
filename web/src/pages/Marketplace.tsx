import { t as tr, tc } from '../i18n'
import { useEffect, useMemo, useState } from 'react'
import { Store, Search, Rocket, Box, Layers, ExternalLink, RefreshCw, Settings2, ClipboardCopy, Info } from 'lucide-react'
import { api, enc } from '../lib/api'
import type { Spec, Template, TemplateEnv } from '../lib/types'
import { useResource, invalidate } from '../lib/store'
import { usePref } from '../lib/prefs'
import { copy } from '../lib/format'
import { go } from '../lib/router'
import { Avatar, CodeEditor, Empty, Spinner, Tag } from '../ui/common'
import { useMenu } from '../ui/ContextMenu'
import { Modal, openDialog, runJob, toast } from '../ui/overlays'
import { ContainerFormDialog, emptySpec, splitArgs } from '../components/ContainerForm'

// Template notes are HTML from remote sources: keep a tiny safe subset.
function safeHTML(html: string) {
  const doc = new DOMParser().parseFromString(html, 'text/html')
  const ok = new Set(['B', 'STRONG', 'I', 'EM', 'CODE', 'BR', 'A', 'P', 'UL', 'LI', 'PRE'])
  const walk = (n: Element) => {
    for (const el of [...n.children]) {
      if (!ok.has(el.tagName)) {
        el.replaceWith(...el.childNodes)
        continue
      }
      for (const a of [...el.attributes]) {
        if (!(el.tagName === 'A' && a.name === 'href' && /^https?:/i.test(a.value))) el.removeAttribute(a.name)
      }
      if (el.tagName === 'A') {
        el.setAttribute('target', '_blank')
        el.setAttribute('rel', 'noopener noreferrer')
      }
      walk(el)
    }
  }
  walk(doc.body)
  return doc.body.innerHTML
}

const slug = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '').slice(0, 40)
const kindLabel = { 1: tr('Container'), 2: 'Swarm', 3: 'Stack' } as const

function defaultEnv(t: Template) {
  return Object.fromEntries((t.env || []).map((e) => [e.name, e.select?.find((o) => o.default)?.value ?? e.select?.[0]?.value ?? e.default ?? '']))
}

export function templateToSpec(t: Template, name: string, env: Record<string, string>): Spec {
  return emptySpec({
    name,
    image: t.image || '',
    env: Object.entries(env).filter(([, v]) => v !== '').map(([k, v]) => `${k}=${v}`),
    ports: (t.ports || []).map((p) => {
      const [hostPart, cont] = p.includes(':') ? [p.slice(0, p.lastIndexOf(':')), p.slice(p.lastIndexOf(':') + 1)] : ['', p]
      return { host_ip: '', host_port: hostPart, container: cont.replace('/tcp', '') }
    }),
    mounts: (t.volumes || []).map((v) => ({
      type: v.bind ? 'bind' : 'volume',
      source: v.bind || `${name}-${v.container.split('/').filter(Boolean).pop() || 'data'}`,
      target: v.container,
      readonly: !!v.readonly,
    })),
    cmd: t.command ? splitArgs(t.command) : null,
    restart: t.restart_policy || 'unless-stopped',
    labels: Object.fromEntries((t.labels || []).map((l) => [l.name, l.value])),
    privileged: !!t.privileged,
    hostname: t.hostname || '',
    tty: !!t.interactive,
    open_stdin: !!t.interactive,
    network_mode: t.network === 'host' ? 'host' : '',
    networks: t.network && t.network !== 'host' ? [{ name: t.network, aliases: [], ipv4: '' }] : [],
  })
}

function EnvFields({ env, values, onChange }: { env: TemplateEnv[]; values: Record<string, string>; onChange: (v: Record<string, string>) => void }) {
  const visible = env.filter((e) => !e.preset)
  if (!visible.length) return null
  return (
    <div className="grid-2">
      {visible.map((e) => (
        <label key={e.name} className="field">
          <span>
            {tr(e.label || e.name)} <code className="muted small">{e.name}</code>
          </span>
          {e.select ? (
            <select value={values[e.name]} onChange={(ev) => onChange({ ...values, [e.name]: ev.target.value })}>
              {e.select.map((o) => <option key={o.value} value={o.value}>{tr(o.text)}</option>)}
            </select>
          ) : (
            <input value={values[e.name] ?? ''} placeholder={e.default || ''} type={/PASS|SECRET|TOKEN/i.test(e.name) ? 'password' : 'text'} onChange={(ev) => onChange({ ...values, [e.name]: ev.target.value })} />
          )}
          {e.description && <small className="help">{e.description}</small>}
        </label>
      ))}
    </div>
  )
}

function DeployDialog({ ep, t, onClose }: { ep: string; t: Template; onClose: () => void }) {
  const [name, setName] = useState(slug(t.name || t.title))
  const [env, setEnv] = useState<Record<string, string>>(() => defaultEnv(t))
  const [compose, setCompose] = useState<string | null>(t.type === 1 ? '' : null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [showCompose, setShowCompose] = useState(false)

  useEffect(() => {
    if (t.type === 1) return
    api.get(`/templates/compose?id=${enc(t.id)}`).then((r) => setCompose(r.compose)).catch((e) => setErr(e.message))
  }, [t])

  const missing = (t.env || []).filter((e) => !e.preset && !e.default && !e.select && !env[e.name]).map((e) => e.label || e.name)

  const deploy = async () => {
    setBusy(true)
    try {
      if (t.type === 1) {
        const res = await runJob(tr('Deploy {0}', t.title), 'POST', `/e/${enc(ep)}/containers`, { spec: templateToSpec(t, name, env), pull: true, start: true })
        onClose()
        invalidate('/containers')
        if (res?.id) go(ep, 'containers', res.id)
      } else {
        const envText = Object.entries(env).map(([k, v]) => `${k}=${v}`).join('\n')
        await runJob(tr('Deploy {0}', t.title), 'POST', `/e/${enc(ep)}/stacks`, { name, compose, env: envText, deploy: true, pull: true })
        onClose()
        invalidate('/stacks', '/containers')
        go(ep, 'stacks', name)
      }
    } catch (e) {
      toast.err(e)
    } finally {
      setBusy(false)
    }
  }

  const advanced = () => {
    onClose()
    openDialog((close) => <ContainerFormDialog ep={ep} initial={templateToSpec(t, name, env)} mode="create" onClose={close} />)
  }

  return (
    <Modal
      wide={t.type === 1 ? true : 'xl'}
      title={<span className="row gap"><Avatar name={t.title} src={t.logo} size={26} square /> {t.title}</span>}
      onClose={onClose}
      footer={
        <>
          {t.type === 1 && <button className="btn btn-ghost" onClick={advanced}><Settings2 size={14} /> {tr('Advanced options')}</button>}
          {t.type !== 1 && compose !== null && <button className="btn btn-ghost" onClick={() => setShowCompose(!showCompose)}>{showCompose ? tr('Hide compose') : tr('View / edit compose')}</button>}
          <span className="grow" />
          {missing.length > 0 && <span className="muted small">{tr('Missing: {0}', missing.join(', '))}</span>}
          <button className="btn" onClick={onClose}>{tr('Cancel')}</button>
          <button className="btn btn-primary" disabled={busy || !name || (t.type !== 1 && !compose)} onClick={deploy}>
            <Rocket size={14} /> {tr('Deploy')}
          </button>
        </>
      }
    >
      <p className="muted">{tr(t.description)}</p>
      {t.note && <div className="alert" dangerouslySetInnerHTML={{ __html: safeHTML(tr(t.note)) }} />}
      {t.type === 2 && <div className="alert alert-warn">{tr('Template meant for Docker Swarm: it will be deployed with docker compose and may need adjustments.')}</div>}
      <div className="row gap wrap">
        <Tag tone="accent">{tr(kindLabel[t.type])}</Tag>
        {t.image && <Tag mono>{t.image}</Tag>}
        {(t.ports || []).map((p) => <Tag key={p} mono>{p}</Tag>)}
      </div>
      <label className="field">
        <span>{t.type === 1 ? tr('Container name') : tr('Stack name')}</span>
        <input value={name} onChange={(e) => setName(t.type === 1 ? e.target.value : slug(e.target.value))} />
      </label>
      <EnvFields env={t.env || []} values={env} onChange={setEnv} />
      {t.type === 1 && (t.volumes || []).length > 0 && (
        <p className="help">
          {tr('Volumes')}: {(t.volumes || []).map((v) => <code key={v.container}>{v.bind || `${name}-${v.container.split('/').filter(Boolean).pop()}`} → {v.container}</code>)}
        </p>
      )}
      {t.type !== 1 && compose === null && !err && <Spinner />}
      {err && <div className="alert alert-bad">{err}</div>}
      {t.type !== 1 && compose !== null && showCompose && <CodeEditor value={compose} onChange={setCompose} minRows={18} />}
    </Modal>
  )
}

export function Marketplace({ ep }: { ep: string }) {
  const [refresh, setRefresh] = useState(0)
  const { data, error, reload } = useResource<{ templates: Template[]; sources: { name: string; url: string; count: number; error?: string }[] }>(`/templates${refresh ? '?refresh=1' : ''}`)
  const [q, setQ] = useState('')
  const [cat, setCat] = usePref('market.cat', '')
  const [kind, setKind] = usePref('market.kind', 0)
  const [src, setSrc] = usePref('market.src', '')
  const menu = useMenu()
  const all = data?.templates || []
  const categories = useMemo(() => {
    const m = new Map<string, number>()
    all.forEach((t) => (t.categories || []).forEach((c) => m.set(c.trim(), (m.get(c.trim()) || 0) + 1)))
    return [...m.entries()].sort((a, b) => b[1] - a[1])
  }, [all])
  const rows = useMemo(() => {
    const ql = q.toLowerCase()
    const seen = new Set<string>()
    return all.filter((t) => {
      if (cat && !(t.categories || []).some((c) => c.trim() === cat)) return false
      if (kind && t.type !== kind) return false
      if (src && t.source !== src) return false
      if (ql && !`${t.title} ${t.description} ${t.image ?? ''} ${(t.categories || []).join(' ')}`.toLowerCase().includes(ql)) return false
      const key = `${t.title.toLowerCase()}|${t.type}`
      if (seen.has(key)) return false // same app listed by several sources
      seen.add(key)
      return true
    })
  }, [all, q, cat, kind, src])
  const deploy = (t: Template) => openDialog((close) => <DeployDialog ep={ep} t={t} onClose={close} />)

  return (
    <div className="page market">
      <div className="page-head">
        <div className="page-title"><Store size={20} /><h1>Marketplace</h1><span className="count">{rows.length}</span></div>
        <div className="page-actions">
          <div className="search wide"><Search size={14} /><input placeholder={tr('Search apps: postgres, wiki, monitor…')} value={q} onChange={(e) => setQ(e.target.value)} autoFocus /></div>
          <button className="btn" onClick={() => (setRefresh((n) => n + 1), reload())} title={tr('Download the sources again')}><RefreshCw size={14} /></button>
          <button className="btn" onClick={() => go(ep, 'settings', 'marketplace')}><Settings2 size={14} /> {tr('Sources')}</button>
        </div>
      </div>
      <div className="market-body">
        <aside className="filters">
          <div className="filter-group">
            <div className="filter-title">{tr('Type')}</div>
            {[[0, tr('All')], [1, tr('Containers')], [3, tr('Compose stacks')]].map(([k, l]) => (
              <button key={k} className={`filter${kind === k ? ' on' : ''}`} onClick={() => setKind(k as number)}>{l}</button>
            ))}
          </div>
          <div className="filter-group">
            <div className="filter-title">{tr('Source')}</div>
            <button className={`filter${!src ? ' on' : ''}`} onClick={() => setSrc('')}>{tc('f', 'All')}</button>
            {(data?.sources || []).map((s) => (
              <button key={s.url} className={`filter${src === s.url ? ' on' : ''}`} onClick={() => setSrc(s.url)} title={s.error || s.url}>
                <span className="ellipsis">{s.name}</span>
                <span className={s.error ? 'tone-bad small' : 'muted small'}>{s.error ? 'error' : s.count}</span>
              </button>
            ))}
          </div>
          <div className="filter-group">
            <div className="filter-title">{tr('Categories')}</div>
            <button className={`filter${!cat ? ' on' : ''}`} onClick={() => setCat('')}>{tc('f', 'All')}</button>
            {categories.slice(0, 40).map(([c, n]) => (
              <button key={c} className={`filter${cat === c ? ' on' : ''}`} onClick={() => setCat(c)}>
                <span className="ellipsis">{tr(c)}</span>
                <span className="muted small">{n}</span>
              </button>
            ))}
          </div>
        </aside>
        <div className="market-grid-wrap">
          {error && <div className="alert alert-bad">{error.message}</div>}
          {!data && !error && <div className="center"><Spinner /></div>}
          {data && rows.length === 0 && <Empty icon={<Store size={28} />} title={tr('No results')} />}
          <div className="market-grid">
            {rows.slice(0, 400).map((t) => (
              <div
                key={t.id}
                className="app-card"
                onClick={() => deploy(t)}
                onContextMenu={menu(() => [
                  { header: t.title },
                  { label: tr('Deploy…'), icon: <Rocket size={14} />, onClick: () => deploy(t) },
                  t.type === 1 && { label: tr('Create with advanced options…'), icon: <Settings2 size={14} />, onClick: () => openDialog((close) => <ContainerFormDialog ep={ep} initial={templateToSpec(t, slug(t.name || t.title), defaultEnv(t))} mode="create" onClose={close} />) },
                  t.image && { label: tr('Copy image'), icon: <ClipboardCopy size={14} />, hint: t.image, onClick: () => copy(t.image!) },
                  t.repository && { label: tr('View repository'), icon: <ExternalLink size={14} />, onClick: () => window.open(t.repository!.url, '_blank') },
                  t.image && { label: tr('Search Docker Hub'), icon: <ExternalLink size={14} />, onClick: () => window.open(`https://hub.docker.com/search?q=${enc(t.image!.split(':')[0])}`, '_blank') },
                  'sep',
                  ...(t.categories || []).slice(0, 4).map((c) => ({ label: tr('Show category {0}', c), onClick: () => setCat(c.trim()) })),
                ])}
              >
                <div className="row gap">
                  <Avatar name={t.title} src={t.logo} size={40} square />
                  <div className="grow">
                    <strong>{t.title}</strong>
                    <div className="row gap">
                      {t.type === 1 ? <Box size={12} className="muted" /> : <Layers size={12} className="muted" />}
                      <span className="muted small">{tr(kindLabel[t.type])}</span>
                      {t.source === 'builtin' && <Tag tone="accent">dockler</Tag>}
                    </div>
                  </div>
                </div>
                <p className="app-desc">{tr(t.description)}</p>
                <div className="tags">
                  {(t.categories || []).slice(0, 3).map((c) => <Tag key={c}>{tr(c)}</Tag>)}
                  {t.note && <Info size={13} className="muted" />}
                </div>
              </div>
            ))}
          </div>
          {rows.length > 400 && <p className="muted center">{tr('Showing 400 of')} {rows.length}{tr('; refine the search.')}</p>}
        </div>
      </div>
    </div>
  )
}
