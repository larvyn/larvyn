import { useEffect, useMemo, useRef, useState } from 'react'
import { Bot, ChevronDown, ChevronRight, Copy, Pause, Play, ScrollText, Trash2 } from 'lucide-react'
import { SystemService } from '../../bindings/larvyn/internal/system'
import { LaravelService, type LogEntry, type LogFile } from '../../bindings/larvyn/internal/laravel'
import { fail, handoff, toast, useActive, useStore } from '../lib/store'
import { bytes, copy, cx } from '../lib/util'
import { Empty, SearchInput, confirmDialog } from '../components/ui'

const LEVELS = ['emergency', 'alert', 'critical', 'error', 'warning', 'notice', 'info', 'debug']
const ERRORISH = new Set(['emergency', 'alert', 'critical', 'error'])

/** Polls the active project's log in the background so the sidebar can show new errors. */
export function useLogWatcher() {
  const { id } = useActive()
  const view = useStore((s) => s.view)
  const offset = useRef<Record<string, number>>({})
  useEffect(() => {
    if (!id) return
    let alive = true
    const tick = async () => {
      try {
        const prev = offset.current[id]
        const chunk = await LaravelService.ReadLog(id, 'laravel.log', prev ?? -1)
        if (!alive) return
        if (prev != null && !chunk.reset) {
          const errors = (chunk.entries ?? []).filter((e) => ERRORISH.has(e.level)).length
          if (errors && useStore.getState().view !== 'logs') {
            useStore.setState((s) => ({ logErrors: { ...s.logErrors, [id]: (s.logErrors[id] ?? 0) + errors } }))
            const last = chunk.entries!.filter((e) => ERRORISH.has(e.level)).at(-1)!
            toast('error', `${last.exception?.split('\\').pop() || 'Error'} logged`, last.message.slice(0, 160))
          }
        }
        offset.current[id] = chunk.offset
      } catch {}
    }
    tick()
    const t = setInterval(tick, 3000)
    return () => {
      alive = false
      clearInterval(t)
    }
  }, [id])
  useEffect(() => {
    if (view === 'logs' && id) useStore.setState((s) => ({ logErrors: { ...s.logErrors, [id]: 0 } }))
  }, [view, id])
}

export function Logs() {
  const { id, project } = useActive()
  const [files, setFiles] = useState<LogFile[]>([])
  const [file, setFile] = useState('laravel.log')
  const [entries, setEntries] = useState<LogEntry[]>([])
  const [levels, setLevels] = useState<string[]>([])
  const [q, setQ] = useState('')
  const [live, setLive] = useState(true)
  const [open, setOpen] = useState<number | null>(null)
  const offset = useRef(-1)

  useEffect(() => {
    if (!id) return
    LaravelService.LogFiles(id).then((f) => {
      setFiles(f ?? [])
      if (f?.length && !f.some((x) => x.name === file)) setFile(f[0].name)
    })
  }, [id])

  useEffect(() => {
    if (!id) return
    offset.current = -1
    setEntries([])
    let alive = true
    const poll = async () => {
      try {
        const c = await LaravelService.ReadLog(id, file, offset.current)
        if (!alive) return
        offset.current = c.offset
        if (c.reset) setEntries(c.entries ?? [])
        else if (c.entries?.length) setEntries((e) => [...e, ...c.entries!].slice(-3000))
      } catch {}
    }
    poll()
    const t = live ? setInterval(poll, 1500) : undefined
    return () => {
      alive = false
      clearInterval(t)
    }
  }, [id, file, live])

  const counts = useMemo(() => {
    const c: Record<string, number> = {}
    for (const e of entries) c[e.level] = (c[e.level] ?? 0) + 1
    return c
  }, [entries])
  const shown = useMemo(() => {
    const ql = q.toLowerCase()
    return entries
      .filter((e) => (!levels.length || levels.includes(e.level)) && (!ql || (e.message + e.exception + e.context).toLowerCase().includes(ql)))
      .slice()
      .reverse()
  }, [entries, levels, q])

  if (!project) return null
  const openLoc = (path: string, line: number) => SystemService.OpenInEditor(path, line).catch(fail('Could not open file'))
  const askAgent = (e: LogEntry) => {
    const frames = e.stack
      .split('\n')
      .filter((l) => !l.includes('/vendor/') && !l.includes('\\vendor\\'))
      .slice(0, 8)
      .join('\n')
    handoff('agents', {
      agentPrompt: `This error was logged in my Laravel app. Find the root cause and fix it, then add a regression test.\n\n${e.exception ? e.exception + ': ' : ''}${e.message}\n${e.file ? `at ${e.file}:${e.line}\n` : ''}\nRelevant frames:\n${frames}`,
    })
  }

  return (
    <div className="view flush">
      <div className="toolbar">
        <h1>
          <ScrollText /> Logs
        </h1>
        {files.length > 1 ? (
          <select className="select" value={file} onChange={(e) => setFile(e.target.value)} style={{ width: 200 }}>
            {files.map((f) => (
              <option key={f.name} value={f.name}>
                {f.name} · {bytes(f.size)}
              </option>
            ))}
          </select>
        ) : (
          <span className="badge mono">{file}</span>
        )}
        <SearchInput value={q} onChange={setQ} placeholder="Search messages…" width={240} />
        <span className="spacer" />
        <button className={cx('btn sm', live ? 'success' : 'ghost')} onClick={() => setLive(!live)}>
          {live ? <Pause /> : <Play />} {live ? 'Live' : 'Paused'}
        </button>
        <button
          className="icon-btn"
          title="Clear log file"
          onClick={async () => {
            if (await confirmDialog({ title: `Empty ${file}?`, confirm: 'Clear', danger: true })) {
              await LaravelService.ClearLog(id!, file).catch(fail('Could not clear'))
              offset.current = -1
              setEntries([])
            }
          }}
        >
          <Trash2 />
        </button>
      </div>
      <div className="row wrap" style={{ padding: '8px 14px', borderBottom: '1px solid var(--line)', gap: 6 }}>
        {LEVELS.filter((l) => counts[l]).map((l) => (
          <button key={l} className={cx('chip', levels.includes(l) && 'active')} onClick={() => setLevels((x) => (x.includes(l) ? x.filter((y) => y !== l) : [...x, l]))}>
            <span className={cx('lvl', l)} style={{ minWidth: 0, padding: '0 5px', fontSize: 9 }}>
              {l}
            </span>
            <span className="n">{counts[l]}</span>
          </button>
        ))}
        {!entries.length && <span className="faint" style={{ fontSize: 12 }}>Waiting for log entries… (storage/logs/{file})</span>}
      </div>
      <div style={{ flex: 1, overflow: 'auto' }}>
        {shown.length === 0 && entries.length > 0 && <Empty icon={<ScrollText />} title="Nothing matches" />}
        {entries.length === 0 && <Empty icon={<ScrollText />} title="The log is quiet">New entries appear here live. Errors also show up as notifications in any view.</Empty>}
        {shown.map((e) => {
          const isOpen = open === e.offset
          return (
            <div key={e.offset} className="log">
              <div className="log-row" onClick={() => setOpen(isOpen ? null : e.offset)}>
                {isOpen ? <ChevronDown size={14} style={{ marginTop: 3, flexShrink: 0 }} /> : <ChevronRight size={14} style={{ marginTop: 3, flexShrink: 0 }} className="faint" />}
                <span className={cx('lvl', e.level)}>{e.level}</span>
                <span className="time">{e.time.replace('T', ' ').slice(5, 19)}</span>
                <div className="msg">
                  <div style={{ overflow: 'hidden', textOverflow: 'ellipsis', display: '-webkit-box', WebkitLineClamp: isOpen ? 'none' : 2, WebkitBoxOrient: 'vertical' } as any}>{e.message}</div>
                  {e.exception && (
                    <div className="exc">
                      {e.exception} · {e.file?.split(/[\\/]/).slice(-2).join('/')}:{e.line}
                    </div>
                  )}
                </div>
              </div>
              {isOpen && (
                <div className="log-detail">
                  <div className="row" style={{ marginBottom: 10 }}>
                    {e.file && (
                      <button className="btn sm" onClick={() => openLoc(e.file, e.line)}>
                        Open {e.file.split(/[\\/]/).pop()}:{e.line}
                      </button>
                    )}
                    {ERRORISH.has(e.level) && (
                      <button className="btn sm primary" onClick={() => askAgent(e)}>
                        <Bot /> Ask an agent to fix it
                      </button>
                    )}
                    <button className="btn sm ghost" onClick={() => copy(`${e.message}\n${e.stack}`).then(() => toast('success', 'Copied'))}>
                      <Copy /> Copy
                    </button>
                  </div>
                  {e.stack && (
                    <div className="frames">
                      {e.stack.split('\n').map((l, i) => {
                        const m = l.match(/^#\d+ (.+?)\((\d+)\): (.*)$/)
                        const isApp = m && !/[\\/]vendor[\\/]/.test(m[1])
                        return (
                          <div key={i} className={cx('frame', isApp && 'app')}>
                            {m ? (
                              <>
                                <span className="loc" onClick={() => openLoc(m[1], Number(m[2]))}>
                                  {m[1].replace(project.path, '').replace(/^[\\/]/, '')}:{m[2]}
                                </span>
                                <span className="ellipsis" style={{ color: 'var(--text-3)' }}>
                                  {m[3]}
                                </span>
                              </>
                            ) : (
                              <span>{l}</span>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  )}
                  {!e.stack && e.context && <pre className="code" style={{ whiteSpace: 'pre-wrap', margin: 0 }}>{e.context}</pre>}
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
