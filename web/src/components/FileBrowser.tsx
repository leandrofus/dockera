import { t } from '../i18n'
import { useEffect, useState } from 'react'
import { Folder, File, Link2, ArrowUp, Download, Eye, RefreshCw, ClipboardCopy, Home } from 'lucide-react'
import { api, enc } from '../lib/api'
import { bytes, copy, dateTime } from '../lib/format'
import { useMenu } from '../ui/ContextMenu'
import { Modal, openDialog, toast } from '../ui/overlays'
import { Spinner } from '../ui/common'

interface Entry {
  name: string
  dir: boolean
  link?: string
  size: number
  mode: string
  mtime?: string
}

const join = (a: string, b: string) => (a.endsWith('/') ? a + b : a + '/' + b)
const parent = (p: string) => p.replace(/\/[^/]+\/?$/, '') || '/'

export function FileBrowser({ ep, id, initial = '/' }: { ep: string; id: string; initial?: string }) {
  const [path, setPath] = useState(initial)
  const [input, setInput] = useState(initial)
  const [entries, setEntries] = useState<Entry[] | null>(null)
  const [error, setError] = useState('')
  const [truncated, setTruncated] = useState(false)
  const [tick, setTick] = useState(0)
  const menu = useMenu()
  const base = `/e/${enc(ep)}/containers/${enc(id)}`

  useEffect(() => {
    setEntries(null)
    setError('')
    setInput(path)
    api
      .get(`${base}/files?path=${enc(path)}`)
      .then((r) => {
        const list = (r.entries as Entry[]).sort((a, b) => Number(b.dir) - Number(a.dir) || a.name.localeCompare(b.name))
        setEntries(list)
        setTruncated(r.truncated)
      })
      .catch((e) => setError(e.message))
  }, [path, tick, base])

  const dl = (p: string, tar = false) => window.open(`/api${base}/download?path=${enc(p)}${tar ? '&tar=1' : ''}`, '_blank')
  const view = async (p: string, size: number) => {
    if (size > 2 << 20) return toast.info(t('File too large to preview; download it.'))
    try {
      const res = await fetch(`/api${base}/download?path=${enc(p)}&inline=1`)
      const text = await res.text()
      openDialog((close) => (
        <Modal title={p} wide="xl" onClose={close} footer={<button className="btn" onClick={() => dl(p)}>{t('Download')}</button>}>
          <pre className="file-view">{text}</pre>
        </Modal>
      ))
    } catch (e) {
      toast.err(e)
    }
  }

  const open = (e: Entry) => {
    const p = join(path, e.name)
    if (e.dir || (e.link && !e.link.includes('.'))) setPath(p)
    else view(p, e.size)
  }

  const crumbs = path.split('/').filter(Boolean)

  return (
    <div className="files">
      <div className="files-bar">
        <button className="icon-btn" title={t('Up')} onClick={() => setPath(parent(path))} disabled={path === '/'}>
          <ArrowUp size={15} />
        </button>
        <button className="icon-btn" title={t('Root')} onClick={() => setPath('/')}>
          <Home size={15} />
        </button>
        <div className="crumbs">
          <button className="link" onClick={() => setPath('/')}>/</button>
          {crumbs.map((c, i) => (
            <span key={i}>
              <button className="link" onClick={() => setPath('/' + crumbs.slice(0, i + 1).join('/'))}>{c}</button>/
            </span>
          ))}
        </div>
        <form className="grow" onSubmit={(e) => (e.preventDefault(), setPath(input || '/'))}>
          <input className="mono" value={input} onChange={(e) => setInput(e.target.value)} />
        </form>
        <button className="icon-btn" title={t('Reload')} onClick={() => setTick((n) => n + 1)}>
          <RefreshCw size={15} />
        </button>
        <button className="btn btn-sm" onClick={() => dl(path, true)}>
          <Download size={14} /> .tar
        </button>
      </div>
      {error && <div className="alert alert-bad">{error}</div>}
      {!entries && !error && (
        <div className="center">
          <Spinner />
        </div>
      )}
      {truncated && <div className="alert">{t('Partial listing: the directory is very large.')}</div>}
      {entries && (
        <div className="file-list" onContextMenu={menu(() => [
          { label: t('Reload'), icon: <RefreshCw size={14} />, onClick: () => setTick((n) => n + 1) },
          { label: t('Download folder (.tar)'), icon: <Download size={14} />, onClick: () => dl(path, true) },
          { label: t('Copy path'), icon: <ClipboardCopy size={14} />, onClick: () => copy(path) },
          path !== '/' && { label: t('Go up one level'), icon: <ArrowUp size={14} />, onClick: () => setPath(parent(path)) },
        ])}>
          {entries.length === 0 && <div className="muted center">{t('Empty directory')}</div>}
          {entries.map((e) => {
            const p = join(path, e.name)
            return (
              <div
                key={e.name}
                className="file-row"
                onDoubleClick={() => open(e)}
                onClick={() => e.dir && setPath(p)}
                onContextMenu={menu(() => [
                  { header: e.name },
                  e.dir ? { label: t('Open'), icon: <Folder size={14} />, onClick: () => setPath(p) } : { label: t('View content'), icon: <Eye size={14} />, onClick: () => view(p, e.size) },
                  !e.dir && { label: t('Download'), icon: <Download size={14} />, onClick: () => dl(p) },
                  { label: t('Download as .tar'), icon: <Download size={14} />, onClick: () => dl(p, true) },
                  'sep',
                  { label: t('Copy path'), icon: <ClipboardCopy size={14} />, onClick: () => copy(p) },
                  { label: t('Copy name'), onClick: () => copy(e.name) },
                  e.link && { label: t('Go to link target'), icon: <Link2 size={14} />, hint: e.link, onClick: () => setPath(e.link!.startsWith('/') ? e.link! : join(path, e.link!)) },
                ])}
              >
                <span className="file-icon">{e.link ? <Link2 size={15} /> : e.dir ? <Folder size={15} /> : <File size={15} />}</span>
                <span className="file-name">
                  {e.name}
                  {e.link && <span className="muted"> → {e.link}</span>}
                </span>
                <span className="mono muted small">{e.mode}</span>
                <span className="muted small num">{e.dir ? '' : bytes(e.size)}</span>
                <span className="muted small">{dateTime(e.mtime)}</span>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
