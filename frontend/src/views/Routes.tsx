import { useMemo, useState } from 'react'
import { ArrowUpRight, Code2, Copy, RefreshCw, Waypoints, X } from 'lucide-react'
import { SystemService } from '../../bindings/larvyn'
import { LaravelService } from '../../bindings/larvyn/internal/laravel'
import { fail, toast, useActive, useStore } from '../lib/store'
import { copy, cx } from '../lib/util'
import { Empty, Loading, SearchInput, useAsync } from '../components/ui'

interface RouteRow {
  domain: string | null
  method: string
  uri: string
  name: string | null
  action: string
  middleware: string[]
}

const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE']

export function Routes() {
  const { id, info } = useActive()
  const sessions = useStore((s) => s.sessions)
  const { data, error, loading, reload } = useAsync(() => LaravelService.Routes(id!) as Promise<any>, [id])
  const [q, setQ] = useState('')
  const [methods, setMethods] = useState<string[]>([])
  const [appOnly, setAppOnly] = useState(false)
  const [sel, setSel] = useState<RouteRow | null>(null)

  const routes: RouteRow[] = (data as RouteRow[]) ?? []
  const filtered = useMemo(() => {
    const ql = q.toLowerCase()
    return routes.filter((r) => {
      if (ql && !`${r.uri} ${r.name ?? ''} ${r.action} ${r.method}`.toLowerCase().includes(ql)) return false
      if (methods.length && !methods.some((m) => r.method.includes(m))) return false
      if (appOnly && !r.action.startsWith('App\\') && r.action !== 'Closure') return false
      return true
    })
  }, [routes, q, methods, appOnly])

  const serve = sessions.find((s) => s.projectId === id && s.key === 'serve' && s.running)?.meta?.url
  const base = (info?.herdUrl || serve || info?.appUrl || '').replace(/\/$/, '')

  const openAction = async (action: string) => {
    const [cls, method] = action.split('@')
    try {
      const r: any = await LaravelService.ResolveClass(id!, cls, method ?? '__invoke')
      await SystemService.OpenInEditor(r.path, r.line)
    } catch (e) {
      fail('Could not open')(e)
    }
  }

  if (!info?.isLaravel) return <Empty icon={<Waypoints />} title="Not a Laravel project" />

  return (
    <div className="view flush">
      <div className="toolbar">
        <h1>
          <Waypoints /> Routes <span className="pill-n">{filtered.length}</span>
        </h1>
        <SearchInput value={q} onChange={setQ} placeholder="Filter by URI, name, controller…" width={300} autoFocus />
        {METHODS.map((m) => (
          <button key={m} className={cx('chip', methods.includes(m) && 'active')} onClick={() => setMethods((x) => (x.includes(m) ? x.filter((y) => y !== m) : [...x, m]))}>
            {m}
          </button>
        ))}
        <label className="check" style={{ marginLeft: 6 }}>
          <input type="checkbox" checked={appOnly} onChange={(e) => setAppOnly(e.target.checked)} /> App routes only
        </label>
        <span className="spacer" />
        <button className="icon-btn" title="Reload" onClick={reload}>
          <RefreshCw />
        </button>
      </div>
      <div className="split">
        <div className="pane-main">
          {loading ? (
            <Loading label="Reading routes…" />
          ) : error ? (
            <div className="error-box" style={{ margin: 16 }}>{error}</div>
          ) : (
            <div className="table-wrap" style={{ flex: 1 }}>
              <table className="grid">
                <thead>
                  <tr>
                    <th style={{ width: 110 }}>Method</th>
                    <th>URI</th>
                    <th>Name</th>
                    <th>Action</th>
                    <th>Middleware</th>
                  </tr>
                </thead>
                <tbody>
                  {filtered.map((r, i) => (
                    <tr key={i} className={cx(sel === r && 'selected')} onClick={() => setSel(r)} style={{ cursor: 'pointer' }}>
                      <td>
                        <div className="row" style={{ gap: 4 }}>
                          {r.method
                            .split('|')
                            .filter((m) => m !== 'HEAD' || r.method === 'HEAD')
                            .map((m) => (
                              <span key={m} className={cx('method', m)}>
                                {m}
                              </span>
                            ))}
                        </div>
                      </td>
                      <td className="mono" style={{ fontSize: 12 }}>
                        <Uri uri={r.uri} />
                      </td>
                      <td className="mono faint" style={{ fontSize: 11.5 }}>
                        {r.name ?? ''}
                      </td>
                      <td className="mono" style={{ fontSize: 11.5, color: r.action.startsWith('App\\') ? 'var(--text-2)' : 'var(--text-3)' }}>
                        {r.action.replace(/^App\\Http\\Controllers\\/, '')}
                      </td>
                      <td>
                        <div className="row" style={{ gap: 4 }}>
                          {r.middleware?.slice(0, 3).map((m) => (
                            <span key={m} className="badge" style={{ height: 18, fontSize: 10.5 }}>
                              {shortMw(m)}
                            </span>
                          ))}
                          {r.middleware?.length > 3 && <span className="faint" style={{ fontSize: 11 }}>+{r.middleware.length - 3}</span>}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
        {sel && (
          <div className="pane-list" style={{ width: 360, borderRight: 0, borderLeft: '1px solid var(--line)' }}>
            <div className="toolbar" style={{ minHeight: 44 }}>
              <span className={cx('method', sel.method.split('|')[0])}>{sel.method.split('|')[0]}</span>
              <span className="mono ellipsis grow" style={{ fontSize: 12 }}>
                /{sel.uri.replace(/^\//, '')}
              </span>
              <button className="icon-btn sm" onClick={() => setSel(null)}>
                <X />
              </button>
            </div>
            <div className="list-scroll" style={{ padding: 14 }}>
              <div className="col" style={{ gap: 14 }}>
                <div className="row wrap">
                  {sel.action.startsWith('App\\') && (
                    <button className="btn sm" onClick={() => openAction(sel.action)}>
                      <Code2 /> Open controller
                    </button>
                  )}
                  {sel.method.includes('GET') && !sel.uri.includes('{') && base && (
                    <button className="btn sm" onClick={() => SystemService.OpenURL(`${base}/${sel.uri.replace(/^\//, '')}`)}>
                      <ArrowUpRight /> Open in browser
                    </button>
                  )}
                  {sel.name && (
                    <button className="btn sm ghost" onClick={() => copy(`route('${sel.name}')`).then(() => toast('success', 'Copied', `route('${sel.name}')`))}>
                      <Copy /> route()
                    </button>
                  )}
                </div>
                <Detail k="Name" v={sel.name ?? '—'} />
                <Detail k="Action" v={sel.action} />
                {sel.domain && <Detail k="Domain" v={sel.domain} />}
                <div>
                  <div className="nav-label" style={{ padding: '0 0 6px' }}>
                    Middleware
                  </div>
                  <div className="col" style={{ gap: 4 }}>
                    {sel.middleware?.map((m) => (
                      <span key={m} className="mono selectable" style={{ fontSize: 11.5, color: 'var(--text-2)', wordBreak: 'break-all' }}>
                        {m}
                      </span>
                    ))}
                    {!sel.middleware?.length && <span className="faint">None</span>}
                  </div>
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

function Detail({ k, v }: { k: string; v: string }) {
  return (
    <div>
      <div className="nav-label" style={{ padding: '0 0 4px' }}>
        {k}
      </div>
      <div className="mono selectable" style={{ fontSize: 12, wordBreak: 'break-all' }}>
        {v}
      </div>
    </div>
  )
}

function Uri({ uri }: { uri: string }) {
  const parts = uri.split(/(\{[^}]+\})/)
  return (
    <>
      <span className="faint">/</span>
      {parts.map((p, i) =>
        p.startsWith('{') ? (
          <span key={i} className="uri-param">
            {p}
          </span>
        ) : (
          <span key={i}>{i === 0 ? p.replace(/^\//, '') : p}</span>
        ),
      )}
    </>
  )
}

function shortMw(m: string) {
  return m.replace(/^Illuminate\\.*\\/, '').replace(/^App\\Http\\Middleware\\/, '')
}
