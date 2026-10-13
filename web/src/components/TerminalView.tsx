import { t as tr } from '../i18n'
import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { RotateCw, Maximize2, Minimize2 } from 'lucide-react'
import { wsURL, enc } from '../lib/api'
import { useMenu } from '../ui/ContextMenu'

const theme = {
  background: '#0f1117',
  foreground: '#e6e6ef',
  cursor: '#a5a6ff',
  selectionBackground: '#3a3d6b',
  black: '#1c1e26',
  brightBlack: '#5c6370',
}

const SHELLS = [
  { label: tr('Automatic (bash → ash → sh)'), cmd: '' },
  { label: '/bin/bash', cmd: 'bash' },
  { label: '/bin/sh', cmd: 'sh' },
  { label: '/bin/ash', cmd: 'ash' },
  { label: 'zsh', cmd: 'zsh' },
]

export function TerminalView({ ep, id, disabled }: { ep: string; id: string; disabled?: boolean }) {
  const host = useRef<HTMLDivElement>(null)
  const term = useRef<Terminal | null>(null)
  const [cmd, setCmd] = useState('')
  const [custom, setCustom] = useState('')
  const [user, setUser] = useState('')
  const [session, setSession] = useState(0)
  const [state, setState] = useState<'connecting' | 'open' | 'closed'>('connecting')
  const [full, setFull] = useState(false)
  const menu = useMenu()

  useEffect(() => {
    if (disabled || !host.current) return
    const t = new Terminal({
      fontFamily: '"JetBrains Mono", "Fira Code", ui-monospace, Menlo, monospace',
      fontSize: 13,
      cursorBlink: true,
      theme,
      allowProposedApi: true,
      scrollback: 5000,
    })
    const fit = new FitAddon()
    t.loadAddon(fit)
    t.open(host.current)
    fit.fit()
    term.current = t
    const q = new URLSearchParams({ cols: String(t.cols), rows: String(t.rows) })
    if (cmd) q.set('cmd', cmd)
    if (user) q.set('user', user)
    const ws = new WebSocket(wsURL(`/e/${enc(ep)}/containers/${enc(id)}/exec?${q}`))
    ws.binaryType = 'arraybuffer'
    setState('connecting')
    const encd = new TextEncoder()
    ws.onopen = () => {
      setState('open')
      t.focus()
    }
    ws.onmessage = (ev) => {
      if (typeof ev.data === 'string') {
        try {
          const m = JSON.parse(ev.data)
          if (m.type === 'exit') t.write(`\r\n\x1b[2m[${tr('process exited, code {0}', m.code)}]\x1b[0m\r\n`)
        } catch {}
        return
      }
      t.write(new Uint8Array(ev.data))
    }
    ws.onclose = () => setState('closed')
    ws.onerror = () => t.write('\r\n\x1b[31m' + tr('Could not open the session.') + '\x1b[0m\r\n')
    const d1 = t.onData((d) => ws.readyState === 1 && ws.send(encd.encode(d)))
    const d2 = t.onBinary((d) => ws.readyState === 1 && ws.send(Uint8Array.from(d, (c) => c.charCodeAt(0))))
    const d3 = t.onResize(({ cols, rows }) => ws.readyState === 1 && ws.send(JSON.stringify({ type: 'resize', cols, rows })))
    const ro = new ResizeObserver(() => {
      try {
        fit.fit()
      } catch {}
    })
    ro.observe(host.current)
    return () => {
      d1.dispose()
      d2.dispose()
      d3.dispose()
      ro.disconnect()
      ws.close()
      t.dispose()
      term.current = null
    }
  }, [ep, id, cmd, user, session, disabled])

  if (disabled) return <div className="terminal-off muted">{tr('The container is not running; start it to open a terminal.')}</div>

  const ctx = menu(() => [
    { label: tr('Copy selection'), disabled: !term.current?.hasSelection(), hint: 'Ctrl+Shift+C', onClick: () => navigator.clipboard.writeText(term.current?.getSelection() || '') },
    {
      label: tr('Paste'),
      hint: 'Ctrl+Shift+V',
      onClick: async () => {
        const txt = await navigator.clipboard.readText()
        term.current?.paste(txt)
      },
    },
    { label: tr('Select all'), onClick: () => term.current?.selectAll() },
    { label: tr('Clear screen'), onClick: () => term.current?.clear() },
    'sep',
    { label: 'Shell', submenu: SHELLS.map((s) => ({ label: s.label, checked: cmd === s.cmd, onClick: () => setCmd(s.cmd) })) },
    { label: tr('As root'), checked: user === 'root', onClick: () => setUser(user === 'root' ? '' : 'root') },
    { label: tr('New session'), onClick: () => setSession((n) => n + 1) },
    { label: full ? tr('Exit full screen') : tr('Full screen'), onClick: () => setFull(!full) },
  ])

  return (
    <div className={`terminal${full ? ' full' : ''}`}>
      <div className="terminal-bar">
        <span className={`dot dot-${state}`} />
        <select value={SHELLS.some((s) => s.cmd === cmd) ? cmd : '__custom'} onChange={(e) => e.target.value !== '__custom' && setCmd(e.target.value)}>
          {SHELLS.map((s) => (
            <option key={s.cmd} value={s.cmd}>
              {s.label}
            </option>
          ))}
          {!SHELLS.some((s) => s.cmd === cmd) && <option value="__custom">{cmd}</option>}
        </select>
        <form
          className="terminal-cmd"
          onSubmit={(e) => {
            e.preventDefault()
            if (custom.trim()) setCmd(custom.trim())
          }}
        >
          <input className="mono" placeholder={tr('or run a command: tail -f /var/log/app.log')} value={custom} onChange={(e) => setCustom(e.target.value)} />
        </form>
        <input className="mono user" placeholder={tr('user')} value={user} onChange={(e) => setUser(e.target.value)} title={tr('User (empty = the container\'s)')} />
        <button className="icon-btn" title={tr('Reconnect')} onClick={() => setSession((n) => n + 1)}>
          <RotateCw size={15} />
        </button>
        <button className="icon-btn" title={tr('Full screen')} onClick={() => setFull(!full)}>
          {full ? <Minimize2 size={15} /> : <Maximize2 size={15} />}
        </button>
      </div>
      <div className="terminal-host" ref={host} onContextMenu={ctx} />
      {state === 'closed' && (
        <div className="terminal-closed">
          {tr('Session closed')} ·{' '}
          <button className="link" onClick={() => setSession((n) => n + 1)}>
            {tr('reconnect')}
          </button>
        </div>
      )}
    </div>
  )
}
