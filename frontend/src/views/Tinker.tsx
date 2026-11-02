import { useEffect, useRef, useState } from 'react'
import { Clock, Cpu, Database, FlaskConical, History, Play, Sparkles, Trash2 } from 'lucide-react'
import { LaravelService } from '../../bindings/larvyn'
import { useActive, useStore } from '../lib/store'
import { bytes, cx, errMsg, load, mod, save } from '../lib/util'
import { useRunShortcut } from '../lib/shortcuts'
import { Empty } from '../components/ui'

const SNIPPETS = [
  { label: 'Count users', code: '\\App\\Models\\User::count()' },
  { label: 'Latest user', code: '\\App\\Models\\User::latest()->first()' },
  { label: 'App config', code: "config('app')" },
  { label: 'Database tables', code: "collect(Schema::getTables())->pluck('name')" },
  { label: 'Routes by name', code: "collect(Route::getRoutes()->getRoutesByName())->keys()" },
  { label: 'Cache test', code: "Cache::put('larvyn', now(), 60);\nCache::get('larvyn')" },
  { label: 'Pending jobs', code: "DB::table('jobs')->count()" },
  { label: 'Send test mail', code: "Mail::raw('Hello from Larvyn', fn ($m) => $m->to('test@example.com')->subject('Test'));\n'sent'" },
]

interface Result {
  ok: boolean
  output?: string
  dump?: string
  type?: string
  json?: any
  queries?: { sql: string; bindings: any[]; time: number }[]
  ms?: number
  memory?: number
  error?: { class: string; message: string; file: string; line: number }
}

export function Tinker() {
  const { id, info } = useActive()
  const focus = useStore((s) => s.focus.tinker)
  const [code, setCode] = useState<string>(() => load('tinker:' + id, "\\App\\Models\\User::query()->latest()->take(5)->get(['id', 'name', 'email'])"))
  const [res, setRes] = useState<Result | null>(null)
  const [busy, setBusy] = useState(false)
  const [hist, setHist] = useState<string[]>(() => load('tinkerhist:' + id, []))
  const [view, setView] = useState<'dump' | 'json'>('dump')
  const taRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    setCode(load('tinker:' + id, code))
    setHist(load('tinkerhist:' + id, []))
    setRes(null)
  }, [id])
  useEffect(() => save('tinker:' + id, code), [code, id])
  useEffect(() => {
    if (focus) {
      setCode(focus)
      useStore.setState({ focus: {} })
      setTimeout(() => run(focus), 50)
    }
  }, [focus])

  const run = async (src = code) => {
    const sel = taRef.current && taRef.current.selectionStart !== taRef.current.selectionEnd ? src.slice(taRef.current.selectionStart, taRef.current.selectionEnd) : src
    if (!sel.trim()) return
    setBusy(true)
    try {
      const r = (await LaravelService.Bridge(id!, 'tinker', { code: sel })) as Result
      setRes(r)
      if (r.json == null) setView('dump')
      const h = [sel, ...hist.filter((x) => x !== sel)].slice(0, 40)
      setHist(h)
      save('tinkerhist:' + id, h)
    } catch (e) {
      setRes({ ok: false, error: { class: 'Larvyn', message: errMsg(e), file: '', line: 0 } })
    } finally {
      setBusy(false)
    }
  }

  useRunShortcut(() => !busy && run(), { enabled: !!info?.isLaravel, within: taRef })
  // Picking a snippet or a recent entry puts the cursor back in the editor, so
  // typing and the shortcut carry on from there.
  const pick = (src: string) => {
    setCode(src)
    requestAnimationFrame(() => taRef.current?.focus())
  }

  if (!info?.isLaravel) return <Empty icon={<FlaskConical />} title="Not a Laravel project" />
  const lines = code.split('\n').length

  return (
    <div className="view flush">
      <div className="toolbar">
        <h1>
          <FlaskConical /> Tinker
        </h1>
        <span className="faint" style={{ fontSize: 12 }}>
          Evaluate PHP inside your booted app. The last expression is returned.
        </span>
        <span className="spacer" />
        <button className="btn sm primary" disabled={busy} onClick={() => run()}>
          {busy ? <div className="spinner" /> : <Play />} Run <kbd style={{ background: 'rgba(0,0,0,.2)', color: '#fff', borderColor: 'rgba(255,255,255,.25)' }}>{mod} ↵</kbd>
        </button>
      </div>
      <div style={{ flex: 1, display: 'flex', minHeight: 0 }}>
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0, borderRight: '1px solid var(--line-2)' }}>
          <div className="editor">
            <div className="gutter">
              {Array.from({ length: lines }, (_, i) => (
                <div key={i}>{i + 1}</div>
              ))}
            </div>
            <textarea
              ref={taRef}
              spellCheck={false}
              value={code}
              onChange={(e) => setCode(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Tab') {
                  e.preventDefault()
                  const t = e.currentTarget
                  const s = t.selectionStart
                  setCode(code.slice(0, s) + '    ' + code.slice(t.selectionEnd))
                  requestAnimationFrame(() => (t.selectionStart = t.selectionEnd = s + 4))
                }
              }}
              onScroll={(e) => {
                const g = e.currentTarget.previousElementSibling as HTMLElement
                if (g) g.scrollTop = e.currentTarget.scrollTop
              }}
            />
          </div>
          <div style={{ borderTop: '1px solid var(--line)', padding: '10px 12px', maxHeight: 200, overflow: 'auto' }}>
            <div className="nav-label" style={{ padding: '0 0 8px', display: 'flex', alignItems: 'center', gap: 6 }}>
              <Sparkles size={11} /> Snippets
            </div>
            <div className="row wrap" style={{ gap: 6 }}>
              {SNIPPETS.map((s) => (
                <button key={s.label} className="chip" onClick={() => pick(s.code)}>
                  {s.label}
                </button>
              ))}
            </div>
            {hist.length > 0 && (
              <>
                <div className="nav-label" style={{ padding: '12px 0 6px', display: 'flex', alignItems: 'center', gap: 6 }}>
                  <History size={11} /> Recent
                  <button className="icon-btn sm" style={{ marginLeft: 'auto' }} onClick={() => (setHist([]), save('tinkerhist:' + id, []))}>
                    <Trash2 />
                  </button>
                </div>
                {hist.slice(0, 8).map((h, i) => (
                  <button key={i} className="list-item mono" style={{ fontSize: 11, minHeight: 26, padding: '4px 8px' }} onClick={() => pick(h)}>
                    <span className="ellipsis">{h.replace(/\n/g, ' ⏎ ')}</span>
                  </button>
                ))}
              </>
            )}
          </div>
        </div>
        <div style={{ width: '48%', display: 'flex', flexDirection: 'column', minWidth: 0 }}>
          {!res ? (
            <Empty icon={<FlaskConical />} title="Run some code">
              Models, facades and helpers are all available. Select part of the code to run only that.
            </Empty>
          ) : (
            <>
              <div className="row" style={{ padding: '8px 14px', borderBottom: '1px solid var(--line)', minHeight: 42 }}>
                {res.ok ? <span className="badge green">{res.type}</span> : <span className="badge red">{res.error?.class.split('\\').pop()}</span>}
                {res.ms != null && (
                  <span className="faint row" style={{ gap: 4, fontSize: 11.5 }}>
                    <Clock size={12} /> {res.ms} ms
                  </span>
                )}
                {res.memory != null && (
                  <span className="faint row" style={{ gap: 4, fontSize: 11.5 }}>
                    <Cpu size={12} /> {bytes(res.memory)}
                  </span>
                )}
                {!!res.queries?.length && (
                  <span className="faint row" style={{ gap: 4, fontSize: 11.5 }}>
                    <Database size={12} /> {res.queries.length} quer{res.queries.length > 1 ? 'ies' : 'y'}
                  </span>
                )}
                <span className="spacer" />
                {res.ok && res.json != null && (
                  <div className="tabs">
                    <button className={cx('tab', view === 'dump' && 'active')} onClick={() => setView('dump')}>
                      Dump
                    </button>
                    <button className={cx('tab', view === 'json' && 'active')} onClick={() => setView('json')}>
                      JSON
                    </button>
                  </div>
                )}
              </div>
              <div style={{ flex: 1, overflow: 'auto', padding: 14 }}>
                {res.output && (
                  <>
                    <div className="nav-label" style={{ padding: '0 0 6px' }}>
                      Output
                    </div>
                    <pre className="code" style={{ margin: '0 0 14px', whiteSpace: 'pre-wrap' }}>
                      {res.output}
                    </pre>
                  </>
                )}
                {!res.ok && res.error && (
                  <div className="error-box">
                    <b>{res.error.class}</b>
                    {'\n'}
                    {res.error.message}
                    {res.error.file && (
                      <div className="mono" style={{ marginTop: 8, fontSize: 11, opacity: 0.8 }}>
                        {res.error.file}:{res.error.line}
                      </div>
                    )}
                  </div>
                )}
                {res.ok && (
                  <pre className="code" style={{ margin: 0, whiteSpace: 'pre-wrap', color: 'var(--text)' }}>
                    {view === 'json' ? JSON.stringify(res.json, null, 2) : res.dump}
                  </pre>
                )}
                {!!res.queries?.length && (
                  <>
                    <div className="nav-label" style={{ padding: '18px 0 6px' }}>
                      Queries
                    </div>
                    <div className="frames">
                      {res.queries.map((q, i) => (
                        <div key={i} className="frame" style={{ display: 'block' }}>
                          <span style={{ color: 'var(--text-2)' }}>{q.sql}</span>
                          {q.bindings?.length > 0 && <span className="faint"> [{q.bindings.map((b) => JSON.stringify(b)).join(', ')}]</span>}
                          <span style={{ float: 'right', color: 'var(--cyan)' }}>{q.time} ms</span>
                        </div>
                      ))}
                    </div>
                  </>
                )}
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
