import { useState } from 'react'
import { AlertOctagon, CalendarClock, ChevronDown, ChevronRight, Inbox, Play, RefreshCw, RotateCcw, Timer, Trash2, Workflow } from 'lucide-react'
import { LaravelService } from '../../bindings/larvyn'
import { artisanRun, setView, useActive, useStore } from '../lib/store'
import { cronText, num } from '../lib/util'
import { Empty, Loading, confirmDialog, useAsync } from '../components/ui'

async function bridge(id: string, action: string) {
  const r: any = await LaravelService.Bridge(id, action, {})
  if (!r?.ok) throw new Error(r?.error ? `${r.error.class}: ${r.error.message}` : 'Bridge failed')
  return r
}

export function Queues() {
  const { id, info } = useActive()
  const sessions = useStore((s) => s.sessions)
  const queue = useAsync(() => bridge(id!, 'queue.failed'), [id])
  const sched = useAsync(() => bridge(id!, 'schedule'), [id])
  const [open, setOpen] = useState<string | null>(null)
  const worker = sessions.find((s) => s.projectId === id && s.kind === 'service' && s.key === 'queue' && s.running)
  const scheduler = sessions.find((s) => s.projectId === id && s.kind === 'service' && s.key === 'schedule' && s.running)

  if (!info?.isLaravel) return <Empty icon={<Workflow />} title="Not a Laravel project" />
  if (!info.hasVendor) return <Empty icon={<Workflow />} title="Install dependencies first" />
  const failed: any[] = queue.data?.failed ?? []
  const events: any[] = sched.data?.events ?? []
  const after = (fn: () => void) => setTimeout(fn, 2500)

  return (
    <div className="view pad">
      <div className="page-head">
        <div className="grow">
          <h1>Queues & Schedule</h1>
          <p>
            Default connection <span className="mono">{queue.data?.default ?? info.queueConnection}</span>
            {queue.data?.pending != null && ` · ${num(queue.data.pending)} jobs waiting`}
          </p>
        </div>
        <button className="icon-btn" onClick={() => (queue.reload(), sched.reload())}>
          <RefreshCw />
        </button>
      </div>

      <div className="stats" style={{ marginBottom: 18 }}>
        <div className="stat link" onClick={() => setView('overview')}>
          <div className="v" style={{ color: worker ? 'var(--green)' : 'var(--text-3)', fontSize: 16, paddingTop: 5 }}>{worker ? 'Running' : 'Stopped'}</div>
          <div className="k">
            <Workflow /> Queue worker
          </div>
        </div>
        <div className="stat">
          <div className="v">{queue.data?.pending != null ? num(queue.data.pending) : '—'}</div>
          <div className="k">
            <Inbox /> Pending jobs
          </div>
        </div>
        <div className="stat">
          <div className="v" style={{ color: failed.length ? 'var(--red)' : undefined }}>{queue.loading ? '…' : failed.length}</div>
          <div className="k">
            <AlertOctagon /> Failed jobs
          </div>
        </div>
        <div className="stat link" onClick={() => setView('overview')}>
          <div className="v" style={{ color: scheduler ? 'var(--green)' : 'var(--text-3)', fontSize: 16, paddingTop: 5 }}>{scheduler ? 'Running' : 'Stopped'}</div>
          <div className="k">
            <Timer /> Scheduler
          </div>
        </div>
      </div>

      <div className="card" style={{ marginBottom: 18 }}>
        <div className="card-head">
          <h3>
            <AlertOctagon /> Failed jobs
          </h3>
          <span className="pill-n">{failed.length}</span>
          <span className="spacer" />
          <button className="btn sm" disabled={!failed.length} onClick={() => (artisanRun(id!, ['queue:retry', 'all']), after(queue.reload))}>
            <RotateCcw /> Retry all
          </button>
          <button
            className="btn sm danger"
            disabled={!failed.length}
            onClick={async () => {
              if (await confirmDialog({ title: 'Delete every failed job?', confirm: 'Flush', danger: true })) {
                artisanRun(id!, ['queue:flush'])
                after(queue.reload)
              }
            }}
          >
            <Trash2 /> Flush
          </button>
        </div>
        {queue.loading ? (
          <Loading />
        ) : queue.error ? (
          <div className="error-box" style={{ margin: 14 }}>{queue.error}</div>
        ) : failed.length === 0 ? (
          <div className="check-row faint">No failed jobs. Nice.</div>
        ) : (
          failed.map((j) => (
            <div key={j.id} style={{ borderBottom: '1px solid var(--line)' }}>
              <div className="check-row" style={{ cursor: 'pointer', borderBottom: 0 }} onClick={() => setOpen(open === j.id ? null : j.id)}>
                {open === j.id ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                <div className="t">
                  <b className="mono" style={{ fontSize: 12 }}>
                    {j.name ?? 'Job'}
                  </b>
                  <span className="ellipsis" style={{ display: 'block' }}>
                    {j.exception?.split('\n')[0]}
                  </span>
                </div>
                <span className="badge">{j.queue}</span>
                <span className="faint" style={{ fontSize: 11 }}>
                  {j.failedAt}
                </span>
                <button className="icon-btn sm" title="Retry" onClick={(e) => (e.stopPropagation(), artisanRun(id!, ['queue:retry', String(j.id)]), after(queue.reload))}>
                  <RotateCcw />
                </button>
                <button className="icon-btn sm danger" title="Forget" onClick={(e) => (e.stopPropagation(), artisanRun(id!, ['queue:forget', String(j.id)]), after(queue.reload))}>
                  <Trash2 />
                </button>
              </div>
              {open === j.id && <pre className="code" style={{ margin: '0 14px 14px', padding: 12, background: 'rgba(0,0,0,.3)', borderRadius: 8, maxHeight: 280, overflow: 'auto', whiteSpace: 'pre-wrap', fontSize: 11 }}>{j.exception}</pre>}
            </div>
          ))
        )}
      </div>

      <div className="card">
        <div className="card-head">
          <h3>
            <CalendarClock /> Scheduled tasks
          </h3>
          <span className="pill-n">{events.length}</span>
          <span className="spacer" />
          <button className="btn sm" onClick={() => artisanRun(id!, ['schedule:run'])}>
            <Play /> Run due tasks now
          </button>
        </div>
        {sched.loading ? (
          <Loading />
        ) : sched.error ? (
          <div className="error-box" style={{ margin: 14 }}>{sched.error}</div>
        ) : events.length === 0 ? (
          <div className="check-row faint">Nothing scheduled. Add tasks in routes/console.php with Schedule::command(…).</div>
        ) : (
          <table className="grid">
            <thead>
              <tr>
                <th>Task</th>
                <th>When</th>
                <th>Next run</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {events.map((e, i) => (
                <tr key={i}>
                  <td className="mono" style={{ fontSize: 12 }}>
                    {e.description && e.description !== e.command ? (
                      <>
                        {e.description} <span className="faint">{e.command}</span>
                      </>
                    ) : (
                      e.command
                    )}
                  </td>
                  <td>
                    <span className="badge">{cronText(e.expression)}</span> <span className="mono faint" style={{ fontSize: 11 }}>{e.expression}</span>
                  </td>
                  <td className="faint" style={{ fontSize: 12 }}>
                    {e.next}
                  </td>
                  <td>
                    <div className="row" style={{ gap: 4 }}>
                      {e.withoutOverlapping && <span className="badge">no overlap</span>}
                      {e.onOneServer && <span className="badge">one server</span>}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
