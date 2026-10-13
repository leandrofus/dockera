import { t } from '../i18n'
import { useState } from 'react'
import { AppWindow, ExternalLink } from 'lucide-react'
import { api } from '../lib/api'
import { invalidate, useResource } from '../lib/store'
import { isViewer } from '../lib/session'
import { Empty, Spinner, Tag } from '../ui/common'
import { toast } from '../ui/overlays'

export interface ShortcutRef {
  endpoint: string
  container: string
  port: number
}

interface Candidate extends ShortcutRef {
  name: string
  stack?: string
  url: string
  on: boolean
  running: boolean
}

export interface ShortcutsInfo {
  info: { supported: boolean; reason?: string; dir?: string; scope?: 'user' | 'system'; menu?: string; os: string }
  all: boolean
  candidates: Candidate[]
}

const same = (a: ShortcutRef, b: ShortcutRef) => a.endpoint === b.endpoint && a.container === b.container && a.port === b.port

/** The start menu entries of this machine (admins; null while unknown). */
export function useShortcuts() {
  return useResource<ShortcutsInfo>(isViewer() ? null : '/shortcuts')
}

async function save(all: boolean, items: ShortcutRef[]) {
  try {
    await api.put('/shortcuts', { all, items: items.map(({ endpoint, container, port }) => ({ endpoint, container, port })) })
    invalidate('/shortcuts')
  } catch (e) {
    toast.err(e)
  }
}

const chosen = (d: ShortcutsInfo) => d.candidates.filter((c) => c.on && !d.all)

/** Adds or removes one container port (from the Jump menu). */
export function toggleShortcut(d: ShortcutsInfo, ref: ShortcutRef, on: boolean) {
  const items = chosen(d).filter((c) => !same(c, ref))
  return save(d.all, on ? [...items, ref] : items).then(() => toast.ok(on ? t('Added to the start menu') : t('Removed from the start menu')))
}

export function StartMenu() {
  const { data, error } = useShortcuts()
  const [busy, setBusy] = useState(false)
  if (error) return <div className="alert alert-bad">{error.message}</div>
  if (!data) return <Spinner />
  const { info } = data
  if (!info.supported) return <Empty icon={<AppWindow size={28} />} title={t('Not available on this machine')}>{info.reason}</Empty>
  const run = async (all: boolean, items: ShortcutRef[]) => {
    setBusy(true)
    await save(all, items)
    setBusy(false)
  }
  const items = chosen(data)
  return (
    <div className="settings-stack">
      <div className="panel">
        <h4><AppWindow size={14} /> {t('Start menu')}</h4>
        <p className="muted small">
          {t('Adds the web apps of the containers running on this machine to {0}. They open in the default browser.', info.os === 'windows' ? t('Start › Programs › Containers') : t('the “Containers” menu'))}{' '}
          {info.scope === 'system' ? t('Every user of this machine sees them.') : t('Only in your own menu.')}
        </p>
        <label className="check" style={{ margin: '6px 0 12px' }}>
          <input type="checkbox" checked={data.all} disabled={busy} onChange={(e) => run(e.target.checked, e.target.checked ? [] : data.candidates.filter((c) => c.on))} />
          {t('Add every web container automatically (and remove them when they are gone)')}
        </label>
        {data.candidates.length === 0 ? (
          <p className="muted small">{t('No running container of this machine publishes a web port.')}</p>
        ) : (
          <div className="table-wrap">
            <table className="table compact">
              <thead>
                <tr>
                  <th style={{ width: 32 }} />
                  <th>{t('Name in the menu')}</th>
                  <th>{t('Opens')}</th>
                  <th>{t('Container')}</th>
                </tr>
              </thead>
              <tbody>
                {data.candidates.map((c) => (
                  <tr key={`${c.endpoint}/${c.container}/${c.port}`}>
                    <td>
                      <input
                        type="checkbox"
                        checked={c.on}
                        disabled={busy || data.all}
                        onChange={(e) => run(false, e.target.checked ? [...items, c] : items.filter((x) => !same(x, c)))}
                      />
                    </td>
                    <td><strong>{c.name}</strong></td>
                    <td>
                      {c.url ? (
                        <a className="mono small" href={c.url} target="_blank" rel="noopener noreferrer">{c.url} <ExternalLink size={11} /></a>
                      ) : (
                        <Tag>{t('stopped')}</Tag>
                      )}
                    </td>
                    <td className="muted small">
                      {c.container}:{c.port}
                      {c.stack && <> · {c.stack}</>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
      <p className="help">
        {t('Only HTTP(S) ports published on this machine. Entries are kept up to date when containers are recreated; stopped ones keep theirs.')}{' '}
        <code>{info.dir}</code>
      </p>
    </div>
  )
}
