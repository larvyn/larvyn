import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ArrowDown,
  ArrowUp,
  ChevronLeft,
  ChevronRight,
  Clock,
  Code2,
  Columns3,
  Database as DbIcon,
  History,
  KeyRound,
  Link2,
  ListChecks,
  Play,
  RefreshCw,
  Rows3,
  Table2,
  Trash2,
  Undo2,
  Sprout,
  Check,
  CircleDashed,
} from 'lucide-react'
import { SystemService } from '../../bindings/larvyn/internal/system'
import { LaravelService } from '../../bindings/larvyn/internal/laravel'
import { artisanRun, fail, toast, useActive, useStore } from '../lib/store'
import { bytes, cx, errMsg, load, mod, num, save } from '../lib/util'
import { useRunShortcut } from '../lib/shortcuts'
import { Empty, Loading, Modal, SearchInput, confirmDialog, useAsync } from '../components/ui'

type Tab = 'data' | 'structure' | 'sql' | 'migrations'

async function bridge(id: string, action: string, payload: Record<string, any> = {}) {
  const r: any = await LaravelService.Bridge(id, action, payload)
  if (!r?.ok) throw new Error(r?.error ? `${r.error.class}: ${r.error.message}` : 'Bridge failed')
  return r
}

export function Database() {
  const { id, info } = useActive()
  const focusTable = useStore((s) => s.focus.table)
  const overview = useAsync(() => bridge(id!, 'db.overview'), [id])
  const [table, setTable] = useState<string | null>(null)
  const [tab, setTab] = useState<Tab>('data')
  const [q, setQ] = useState('')

  useEffect(() => {
    if (focusTable) {
      setTable(focusTable)
      setTab('data')
      useStore.setState({ focus: {} })
    }
  }, [focusTable])
  useEffect(() => setTable(null), [id])

  const tables: any[] = overview.data?.tables ?? []
  const shown = tables.filter((t) => !q || t.name.toLowerCase().includes(q.toLowerCase()))

  if (!info?.isLaravel) return <Empty icon={<DbIcon />} title="Not a Laravel project" />
  if (!info.hasVendor) return <Empty icon={<DbIcon />} title="Install dependencies first">The database browser boots your app, so it needs vendor/. Run composer install.</Empty>

  return (
    <div className="view flush">
      <div className="split">
        <div className="pane-list" style={{ width: 260 }}>
          <div style={{ padding: '12px 12px 10px', borderBottom: '1px solid var(--line)' }}>
            <div className="row" style={{ marginBottom: 10 }}>
              <DbIcon size={15} style={{ color: 'var(--accent)' }} />
              <div className="grow" style={{ minWidth: 0 }}>
                <div style={{ fontWeight: 620, fontSize: 13 }} className="ellipsis">
                  {overview.data?.database?.split(/[\\/]/).pop() ?? info.dbDatabase ?? 'Database'}
                </div>
                <div className="faint" style={{ fontSize: 11 }}>
                  {overview.data ? `${overview.data.driver} ${overview.data.version ?? ''} · ${tables.length} tables` : info.dbConnection}
                </div>
              </div>
              <button className="icon-btn sm" title="Reload" onClick={overview.reload}>
                <RefreshCw />
              </button>
            </div>
            <SearchInput value={q} onChange={setQ} placeholder="Filter tables…" width="100%" />
          </div>
          <div className="list-scroll">
            <button className={cx('list-item', tab === 'sql' && 'active')} onClick={() => setTab('sql')}>
              <Code2 /> SQL console
            </button>
            <button className={cx('list-item', tab === 'migrations' && 'active')} onClick={() => setTab('migrations')}>
              <ListChecks /> Migrations
            </button>
            <div className="list-group">
              Tables <span>{shown.length}</span>
            </div>
            {overview.loading && [1, 2, 3, 4, 5].map((i) => <div key={i} className="skeleton" style={{ height: 22, margin: '6px 10px' }} />)}
            {overview.error && <div className="error-box" style={{ margin: 6, fontSize: 11.5 }}>{overview.error}</div>}
            {shown.map((t) => (
              <button
                key={t.name}
                className={cx('list-item', table === t.name && (tab === 'data' || tab === 'structure') && 'active')}
                onClick={() => {
                  setTable(t.name)
                  if (tab !== 'structure') setTab('data')
                }}
              >
                <Table2 />
                <span className="grow ellipsis mono" style={{ fontSize: 12 }}>
                  {t.name}
                </span>
                <span className="faint" style={{ fontSize: 11, fontVariantNumeric: 'tabular-nums' }}>
                  {t.rows != null ? num(t.rows) : ''}
                </span>
              </button>
            ))}
          </div>
        </div>
        <div className="pane-main">
          {tab === 'sql' ? (
            <SqlConsole projectId={id!} />
          ) : tab === 'migrations' ? (
            <Migrations projectId={id!} onChanged={overview.reload} />
          ) : table ? (
            <>
              <div className="toolbar">
                <h1>
                  <Table2 /> <span className="mono">{table}</span>
                </h1>
                <div className="tabs">
                  <button className={cx('tab', tab === 'data' && 'active')} onClick={() => setTab('data')}>
                    <Rows3 /> Data
                  </button>
                  <button className={cx('tab', tab === 'structure' && 'active')} onClick={() => setTab('structure')}>
                    <Columns3 /> Structure
                  </button>
                </div>
                <span className="faint" style={{ fontSize: 11.5 }}>
                  {bytes(tables.find((t) => t.name === table)?.size)}
                </span>
              </div>
              {tab === 'data' ? <TableData key={table} projectId={id!} table={table} /> : <Structure key={table} projectId={id!} table={table} />}
            </>
          ) : (
            <Empty icon={<Table2 />} title="Pick a table">
              Browse rows, inspect columns, indexes and foreign keys, or write SQL. Everything goes through your app's own database connection.
            </Empty>
          )}
        </div>
      </div>
    </div>
  )
}

function Cell({ v }: { v: any }) {
  if (v === null || v === undefined) return <span className="null">NULL</span>
  if (typeof v === 'boolean') return <span style={{ color: 'var(--purple)' }}>{String(v)}</span>
  if (typeof v === 'number') return <span style={{ color: 'var(--cyan)' }}>{v}</span>
  const s = typeof v === 'object' ? JSON.stringify(v) : String(v)
  return <>{s.length > 200 ? s.slice(0, 200) + '…' : s}</>
}

function TableData({ projectId, table }: { projectId: string; table: string }) {
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState('')
  const [debounced, setDebounced] = useState('')
  const [order, setOrder] = useState<{ col: string; dir: 'asc' | 'desc' } | null>(null)
  const [cell, setCell] = useState<{ row: any; col: string } | null>(null)
  useEffect(() => {
    const t = setTimeout(() => setDebounced(search), 300)
    return () => clearTimeout(t)
  }, [search])
  useEffect(() => setPage(1), [debounced])
  const per = 100
  const res = useAsync(
    () => bridge(projectId, 'db.rows', { table, page, perPage: per, search: debounced, orderBy: order?.col, direction: order?.dir }),
    [projectId, table, page, debounced, order?.col, order?.dir],
  )
  const cols: string[] = res.data?.columns ?? []
  const rows: any[] = res.data?.rows ?? []
  const total = res.data?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / per))
  const key = cols.includes('id') ? 'id' : null

  const del = async (row: any) => {
    if (!key) return
    if (!(await confirmDialog({ title: `Delete row ${row[key]}?`, message: `DELETE FROM ${table} WHERE ${key} = ${row[key]}`, confirm: 'Delete', danger: true }))) return
    bridge(projectId, 'db.delete', { table, key, id: row[key] })
      .then(() => {
        toast('success', 'Row deleted')
        res.reload()
      })
      .catch(fail('Delete failed'))
  }

  return (
    <>
      <div className="row" style={{ padding: '8px 14px', borderBottom: '1px solid var(--line)' }}>
        <SearchInput value={search} onChange={setSearch} placeholder="Search every column…" width={280} />
        {res.loading && <div className="spinner" />}
        <span className="spacer" />
        <span className="faint" style={{ fontSize: 12 }}>
          {num(total)} rows {res.data?.ms != null && `· ${res.data.ms} ms`}
        </span>
        <button className="icon-btn sm" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
          <ChevronLeft />
        </button>
        <span style={{ fontSize: 12, fontVariantNumeric: 'tabular-nums' }}>
          {page} / {pages}
        </span>
        <button className="icon-btn sm" disabled={page >= pages} onClick={() => setPage((p) => p + 1)}>
          <ChevronRight />
        </button>
        <button className="icon-btn sm" onClick={res.reload}>
          <RefreshCw />
        </button>
      </div>
      {res.error ? (
        <div className="error-box" style={{ margin: 14 }}>{res.error}</div>
      ) : (
        <div className="table-wrap" style={{ flex: 1 }}>
          <table className="grid data">
            <thead>
              <tr>
                {cols.map((c) => (
                  <th
                    key={c}
                    className="sortable"
                    onClick={() => setOrder((o) => (o?.col === c ? (o.dir === 'asc' ? { col: c, dir: 'desc' } : null) : { col: c, dir: 'asc' }))}
                  >
                    {c} {order?.col === c && (order.dir === 'asc' ? <ArrowUp size={11} /> : <ArrowDown size={11} />)}
                  </th>
                ))}
                {key && <th style={{ width: 40 }} />}
              </tr>
            </thead>
            <tbody>
              {rows.map((r, i) => (
                <tr key={i}>
                  {cols.map((c) => (
                    <td key={c} onDoubleClick={() => setCell({ row: r, col: c })} title="Double-click to inspect">
                      <Cell v={r[c]} />
                    </td>
                  ))}
                  {key && (
                    <td>
                      <button className="icon-btn sm danger" onClick={() => del(r)}>
                        <Trash2 />
                      </button>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
          {!res.loading && rows.length === 0 && <Empty icon={<Rows3 />} title="No rows" />}
        </div>
      )}
      {cell && <CellEditor projectId={projectId} table={table} keyCol={key} row={cell.row} col={cell.col} onClose={() => setCell(null)} onSaved={res.reload} />}
    </>
  )
}

function CellEditor(props: { projectId: string; table: string; keyCol: string | null; row: any; col: string; onClose: () => void; onSaved: () => void }) {
  const orig = props.row[props.col]
  const pretty = (() => {
    if (typeof orig === 'string' && /^[[{]/.test(orig.trim())) {
      try {
        return JSON.stringify(JSON.parse(orig), null, 2)
      } catch {}
    }
    return orig == null ? '' : String(orig)
  })()
  const [val, setVal] = useState(pretty)
  const [isNull, setNull] = useState(orig == null)
  const saveIt = () => {
    if (!props.keyCol) return
    let v: any = isNull ? null : val
    if (!isNull && typeof orig === 'string' && /^[[{]/.test(orig.trim())) {
      try {
        v = JSON.stringify(JSON.parse(val))
      } catch {}
    }
    bridge(props.projectId, 'db.update', { table: props.table, key: props.keyCol, id: props.row[props.keyCol], values: { [props.col]: v } })
      .then(() => {
        toast('success', 'Saved')
        props.onSaved()
        props.onClose()
      })
      .catch(fail('Update failed'))
  }
  return (
    <Modal
      title={<span className="mono">{props.col}</span>}
      subtitle={props.keyCol ? `${props.table} · ${props.keyCol} = ${props.row[props.keyCol]}` : props.table}
      onClose={props.onClose}
      footer={
        props.keyCol ? (
          <>
            <label className="check" style={{ marginRight: 'auto' }}>
              <input type="checkbox" checked={isNull} onChange={(e) => setNull(e.target.checked)} /> NULL
            </label>
            <button className="btn ghost" onClick={props.onClose}>
              Cancel
            </button>
            <button className="btn primary" onClick={saveIt}>
              Save
            </button>
          </>
        ) : undefined
      }
    >
      <textarea className="textarea mono" rows={12} value={val} disabled={isNull} onChange={(e) => setVal(e.target.value)} />
    </Modal>
  )
}

function Structure({ projectId, table }: { projectId: string; table: string }) {
  const res = useAsync(() => bridge(projectId, 'db.table', { table }), [projectId, table])
  if (res.loading) return <Loading />
  if (res.error) return <div className="error-box" style={{ margin: 14 }}>{res.error}</div>
  const cols: any[] = res.data.columns ?? []
  const idx: any[] = res.data.indexes ?? []
  const fks: any[] = res.data.foreignKeys ?? []
  return (
    <div className="view pad" style={{ paddingTop: 14 }}>
      <div className="card" style={{ overflow: 'hidden' }}>
        <table className="grid">
          <thead>
            <tr>
              <th>Column</th>
              <th>Type</th>
              <th>Nullable</th>
              <th>Default</th>
              <th>Extra</th>
            </tr>
          </thead>
          <tbody>
            {cols.map((c) => {
              const pk = idx.some((i) => i.primary && i.columns?.includes(c.name))
              const fk = fks.find((f) => f.columns?.includes(c.name))
              return (
                <tr key={c.name}>
                  <td className="mono" style={{ fontWeight: 600 }}>
                    <span className="row" style={{ gap: 6 }}>
                      {pk && <KeyRound size={12} style={{ color: 'var(--yellow)' }} />}
                      {fk && <Link2 size={12} style={{ color: 'var(--blue)' }} />}
                      {c.name}
                    </span>
                  </td>
                  <td className="mono" style={{ color: 'var(--cyan)', fontSize: 12 }}>
                    {c.type}
                  </td>
                  <td>{c.nullable ? <span className="badge">null</span> : <span className="faint">not null</span>}</td>
                  <td className="mono faint" style={{ fontSize: 12 }}>
                    {c.default ?? ''}
                  </td>
                  <td>
                    <div className="row" style={{ gap: 4 }}>
                      {c.auto_increment && <span className="badge yellow">auto increment</span>}
                      {fk && (
                        <span className="badge blue mono">
                          → {fk.foreign_table}.{fk.foreign_columns?.join(',')}
                        </span>
                      )}
                      {c.comment && <span className="faint">{c.comment}</span>}
                    </div>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      {idx.length > 0 && (
        <>
          <div className="section-title">Indexes</div>
          <div className="card" style={{ overflow: 'hidden' }}>
            <table className="grid">
              <tbody>
                {idx.map((i) => (
                  <tr key={i.name}>
                    <td className="mono">{i.name}</td>
                    <td className="mono" style={{ color: 'var(--text-2)' }}>
                      ({i.columns?.join(', ')})
                    </td>
                    <td>
                      {i.primary ? <span className="badge yellow">primary</span> : i.unique ? <span className="badge purple">unique</span> : <span className="badge">index</span>}
                    </td>
                    <td className="faint">{i.type}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  )
}

function SqlConsole({ projectId }: { projectId: string }) {
  const [sql, setSql] = useState<string>(() => load('sql:' + projectId, 'select * from users limit 50'))
  const [hist, setHist] = useState<string[]>(() => load('sqlhist:' + projectId, []))
  const [res, setRes] = useState<any>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const taRef = useRef<HTMLTextAreaElement>(null)
  useEffect(() => save('sql:' + projectId, sql), [sql])
  const run = async () => {
    const text = sql.trim()
    if (!text) return
    const isWrite = !/^\s*(select|with|show|describe|desc|explain|pragma|values)\b/i.test(text)
    if (isWrite && !(await confirmDialog({ title: 'Run a write statement?', message: <span className="mono">{text.slice(0, 300)}</span>, confirm: 'Run', danger: true }))) return
    setBusy(true)
    setErr('')
    try {
      setRes(await bridge(projectId, 'db.query', { sql: text }))
      const h = [text, ...hist.filter((x) => x !== text)].slice(0, 30)
      setHist(h)
      save('sqlhist:' + projectId, h)
    } catch (e) {
      setErr(errMsg(e))
      setRes(null)
    } finally {
      setBusy(false)
    }
  }
  useRunShortcut(() => !busy && run(), { within: taRef })
  return (
    <>
      <div className="toolbar">
        <h1>
          <Code2 /> SQL console
        </h1>
        <span className="faint" style={{ fontSize: 12 }}>
          Runs on your app's default connection
        </span>
        <span className="spacer" />
        <button className="btn sm primary" disabled={busy} onClick={run}>
          {busy ? <div className="spinner" /> : <Play />} Run <kbd style={{ background: 'rgba(0,0,0,.2)', color: '#fff', borderColor: 'rgba(255,255,255,.25)' }}>{mod} ↵</kbd>
        </button>
      </div>
      <div style={{ display: 'flex', minHeight: 0, flex: 1 }}>
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
          <div className="editor" style={{ flex: '0 0 180px', borderBottom: '1px solid var(--line-2)' }}>
            <textarea
              spellCheck={false}
              value={sql}
              onChange={(e) => setSql(e.target.value)}
              ref={taRef}
              style={{ paddingLeft: 16 }}
            />
          </div>
          <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
            {err && <div className="error-box" style={{ margin: 14 }}>{err}</div>}
            {res?.kind === 'affected' && (
              <div className="empty">
                <div className="ico" style={{ color: 'var(--green)' }}>
                  <Check />
                </div>
                <h3>{res.affected} row(s) affected</h3>
                <p>{res.ms} ms</p>
              </div>
            )}
            {res?.kind === 'rows' && (
              <>
                <div className="row" style={{ padding: '6px 14px', borderBottom: '1px solid var(--line)', fontSize: 12 }}>
                  <span className="muted">
                    {num(res.total)} rows{res.truncated && ' (showing 1000)'}
                  </span>
                  <span className="faint">· {res.ms} ms</span>
                </div>
                <div className="table-wrap" style={{ flex: 1 }}>
                  <table className="grid data">
                    <thead>
                      <tr>
                        {res.columns.map((c: string) => (
                          <th key={c}>{c}</th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {res.rows.map((r: any, i: number) => (
                        <tr key={i}>
                          {res.columns.map((c: string) => (
                            <td key={c}>
                              <Cell v={r[c]} />
                            </td>
                          ))}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </>
            )}
          </div>
        </div>
        <div style={{ width: 240, borderLeft: '1px solid var(--line)', display: 'flex', flexDirection: 'column' }}>
          <div className="list-group" style={{ padding: '12px 12px 6px' }}>
            <span className="row" style={{ gap: 6 }}>
              <History size={12} /> History
            </span>
          </div>
          <div className="list-scroll">
            {hist.map((h, i) => (
              <button key={i} className="list-item mono" style={{ fontSize: 11 }} onClick={() => setSql(h)} title={h}>
                <span className="ellipsis">{h}</span>
              </button>
            ))}
            {!hist.length && <div className="faint" style={{ padding: 10, fontSize: 12 }}>Queries you run appear here.</div>}
          </div>
        </div>
      </div>
    </>
  )
}

function Migrations({ projectId, onChanged }: { projectId: string; onChanged: () => void }) {
  const res = useAsync(() => bridge(projectId, 'migrations'), [projectId])
  const sessions = useStore((s) => s.sessions)
  const runningMigrate = sessions.some((s) => s.projectId === projectId && s.kind === 'run' && s.running && s.title.includes('migrate'))
  useEffect(() => {
    if (!runningMigrate) {
      res.reload()
      onChanged()
    }
  }, [runningMigrate])
  const list: any[] = res.data?.migrations ?? []
  const pending = list.filter((m) => !m.ran).length
  const batches = useMemo(() => [...new Set(list.filter((m) => m.batch).map((m) => m.batch))].sort((a, b) => b - a), [list])
  const danger = async (args: string[], title: string) => {
    if (await confirmDialog({ title, message: <span className="mono">php artisan {args.join(' ')}</span>, confirm: 'Run', danger: true })) artisanRun(projectId, args)
  }
  return (
    <>
      <div className="toolbar">
        <h1>
          <ListChecks /> Migrations
        </h1>
        {res.data && (pending ? <span className="badge yellow">{pending} pending</span> : <span className="badge green">up to date</span>)}
        <span className="spacer" />
        <button className="btn sm primary" disabled={!pending} onClick={() => artisanRun(projectId, ['migrate'])}>
          <Play /> Migrate
        </button>
        <button className="btn sm" disabled={!batches.length} onClick={() => danger(['migrate:rollback'], 'Roll back the last batch?')}>
          <Undo2 /> Rollback
        </button>
        <button className="btn sm danger" onClick={() => danger(['migrate:fresh', '--seed'], 'Drop every table and re-run all migrations with seeders?')}>
          <Sprout /> Fresh + seed
        </button>
        <button className="icon-btn sm" onClick={res.reload}>
          <RefreshCw />
        </button>
      </div>
      {res.loading ? (
        <Loading />
      ) : res.error ? (
        <div className="error-box" style={{ margin: 14 }}>{res.error}</div>
      ) : (
        <div className="table-wrap" style={{ flex: 1 }}>
          <table className="grid">
            <thead>
              <tr>
                <th style={{ width: 40 }} />
                <th>Migration</th>
                <th style={{ width: 90 }}>Batch</th>
                <th style={{ width: 50 }} />
              </tr>
            </thead>
            <tbody>
              {list.map((m) => (
                <tr key={m.name}>
                  <td>{m.ran ? <Check size={14} style={{ color: 'var(--green)' }} /> : <CircleDashed size={14} style={{ color: 'var(--yellow)' }} />}</td>
                  <td className="mono" style={{ fontSize: 12 }}>
                    <span className="faint">{m.name.slice(0, 18)}</span>
                    {m.name.slice(18)}
                  </td>
                  <td>{m.batch ? <span className="badge">#{m.batch}</span> : <span className="badge yellow">pending</span>}</td>
                  <td>
                    <button className="icon-btn sm" title="Open file" onClick={() => SystemService.OpenInEditor(m.path, 1).catch(fail('Editor'))}>
                      <Code2 />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <div className="faint row" style={{ padding: '6px 14px', fontSize: 11, borderTop: '1px solid var(--line)' }}>
        <Clock size={11} /> Status refreshes when a migrate command finishes.
      </div>
    </>
  )
}
