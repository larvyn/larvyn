import { useEffect, useMemo, useState } from 'react'
import { Bot, Check, ChevronDown, ChevronRight, CircleSlash, Code2, FlaskConical, Play, RotateCcw, TestTube2, X, Zap } from 'lucide-react'
import { SystemService } from '../../bindings/larvyn/internal/system'
import { LaravelService, type TestCase, type TestReport } from '../../bindings/larvyn/internal/laravel'
import { fail, handoff, showConsole, useActive, useStore } from '../lib/store'
import { ago, cx } from '../lib/util'
import { Empty, SearchInput } from '../components/ui'

type Filter = 'all' | 'failed' | 'passed' | 'skipped'

export function Tests() {
  const { id, project, info } = useActive()
  const sessions = useStore((s) => s.sessions)
  const [report, setReport] = useState<TestReport | null>(null)
  const [filter, setFilter] = useState<Filter>('all')
  const [q, setQ] = useState('')
  const [open, setOpen] = useState<string | null>(null)
  const [parallel, setParallel] = useState(false)
  const running = sessions.find((s) => s.projectId === id && s.kind === 'run' && s.key === 'tests' && s.running)

  const load = () => id && LaravelService.TestResults(id).then(setReport).catch(fail('Could not read test results'))
  // One effect, so opening the view reads the report once; it also re-reads
  // when the project changes or a run starts or finishes.
  useEffect(() => {
    load()
  }, [id, !!running])

  const run = (f = '') =>
    LaravelService.RunTests(id!, f, parallel)
      .then((s) => showConsole(true, s.id))
      .catch(fail('Could not run tests'))

  const cases = report?.cases ?? []
  const groups = useMemo(() => {
    const ql = q.toLowerCase()
    const map = new Map<string, TestCase[]>()
    for (const c of cases) {
      if (filter === 'failed' && c.status !== 'failed' && c.status !== 'error') continue
      if (filter === 'passed' && c.status !== 'passed') continue
      if (filter === 'skipped' && c.status !== 'skipped') continue
      if (ql && !`${c.class} ${c.name}`.toLowerCase().includes(ql)) continue
      map.set(c.class, [...(map.get(c.class) ?? []), c])
    }
    return [...map.entries()].sort((a, b) => {
      const fa = a[1].some((c) => c.status === 'failed' || c.status === 'error') ? 0 : 1
      const fb = b[1].some((c) => c.status === 'failed' || c.status === 'error') ? 0 : 1
      return fa - fb || a[0].localeCompare(b[0])
    })
  }, [cases, filter, q])

  if (!info?.isLaravel) return <Empty icon={<TestTube2 />} title="Not a Laravel project" />

  const openFile = (c: TestCase) => {
    const file = c.file.split('::')[0]
    const abs = /^[A-Za-z]:|^\//.test(file) ? file : `${project!.path}/${file}`
    SystemService.OpenInEditor(abs, c.line || 1).catch(fail('Could not open file'))
  }
  const askAgent = (c: TestCase) =>
    handoff('agents', {
      agentPrompt: `This test fails in my Laravel app. Find out why and fix the application code (change the test only if it is clearly wrong). Then run \`php artisan test --filter="${c.name}"\` until it passes.\n\nTest: ${c.class} › ${c.name}\n\n${c.details.slice(0, 3000)}`,
    })
  const pct = report?.tests ? Math.round((report.passed / report.tests) * 100) : 0

  return (
    <div className="view flush">
      <div className="toolbar">
        <h1>
          <TestTube2 /> Tests
        </h1>
        {info.testRunner && <span className="badge blue">{info.testRunner}</span>}
        <SearchInput value={q} onChange={setQ} placeholder="Filter tests…" width={240} />
        <div className="tabs">
          {(['all', 'failed', 'passed', 'skipped'] as Filter[]).map((f) => (
            <button key={f} className={cx('tab', filter === f && 'active')} onClick={() => setFilter(f)} style={{ textTransform: 'capitalize' }}>
              {f}
              {report && f !== 'all' && <span className="pill-n">{f === 'failed' ? report.failed : f === 'passed' ? report.passed : report.skipped}</span>}
            </button>
          ))}
        </div>
        <span className="spacer" />
        <label className="check">
          <input type="checkbox" checked={parallel} onChange={(e) => setParallel(e.target.checked)} /> Parallel
        </label>
        {report && report.failed > 0 && (
          <button className="btn sm" disabled={!!running} onClick={() => run(cases.filter((c) => c.status === 'failed' || c.status === 'error').map((c) => escapeFilter(c.name)).join('|'))}>
            <RotateCcw /> Re-run failed
          </button>
        )}
        <button className="btn sm primary" disabled={!!running} onClick={() => run()}>
          {running ? <div className="spinner" style={{ borderTopColor: '#fff' }} /> : <Play />} {running ? 'Running…' : 'Run all'}
        </button>
      </div>

      {report?.exists && (
        <div style={{ padding: '14px 16px 4px' }}>
          <div className="row" style={{ gap: 14, marginBottom: 10 }}>
            <span style={{ fontSize: 22, fontWeight: 700, letterSpacing: '-0.03em', color: report.failed ? 'var(--red)' : 'var(--green)' }}>{report.failed ? `${report.failed} failing` : 'All green'}</span>
            <span className="muted">
              {report.passed}/{report.tests} passed · {report.skipped} skipped · {report.time.toFixed(2)}s
            </span>
            <span className="spacer" />
            <span className="faint" style={{ fontSize: 12 }}>
              Last run {ago(report.modified)}
            </span>
          </div>
          <div style={{ height: 6, borderRadius: 4, background: 'var(--bg-4)', overflow: 'hidden', display: 'flex' }}>
            <div style={{ width: `${pct}%`, background: 'var(--green)' }} />
            <div style={{ width: `${report.tests ? (report.failed / report.tests) * 100 : 0}%`, background: 'var(--red)' }} />
          </div>
        </div>
      )}

      <div style={{ flex: 1, overflow: 'auto', padding: '8px 0 24px' }}>
        {!report?.exists ? (
          <Empty
            icon={<FlaskConical />}
            title={report?.incomplete && !running ? 'The last run did not finish' : 'Run your test suite'}
            action={
              <button className="btn primary" disabled={!!running} onClick={() => run()}>
                <Play /> Run all tests
              </button>
            }
          >
            {report?.incomplete && !running
              ? 'No test report was written, so the run probably stopped early or failed to boot. Check its output in the console, then run the tests again.'
              : 'Larvyn runs php artisan test and shows every test with its result, the failure and the line it failed on. Re-run one test, or hand a failure to an agent.'}
          </Empty>
        ) : (
          groups.map(([cls, list]) => {
            const failed = list.filter((c) => c.status === 'failed' || c.status === 'error').length
            return (
              <div key={cls} style={{ marginBottom: 6 }}>
                <div className="list-group" style={{ padding: '12px 18px 6px', textTransform: 'none', letterSpacing: 0, fontSize: 12 }}>
                  <span className="mono" style={{ color: 'var(--text-2)' }}>
                    {cls.replace(/^Tests\\/, '')}
                  </span>
                  <span style={{ color: failed ? 'var(--red)' : 'var(--text-3)' }}>{failed ? `${failed} failing` : `${list.length}`}</span>
                </div>
                {list.map((c) => {
                  const key = cls + c.name
                  const bad = c.status === 'failed' || c.status === 'error'
                  return (
                    <div key={key} className="log">
                      <div className="log-row" onClick={() => bad && setOpen(open === key ? null : key)} style={{ cursor: bad ? 'pointer' : 'default', alignItems: 'center', padding: '7px 18px' }}>
                        {c.status === 'passed' ? (
                          <Check size={14} style={{ color: 'var(--green)' }} />
                        ) : c.status === 'skipped' ? (
                          <CircleSlash size={14} className="faint" />
                        ) : (
                          <X size={14} style={{ color: 'var(--red)' }} />
                        )}
                        <div className="msg" style={{ color: bad ? 'var(--text)' : 'var(--text-2)' }}>
                          {c.name}
                          {bad && <div className="exc" style={{ color: '#ffb3ae' }}>{c.message}</div>}
                        </div>
                        <span className="faint mono" style={{ fontSize: 11 }}>
                          {(c.time * 1000).toFixed(0)} ms
                        </span>
                        {bad && (open === key ? <ChevronDown size={14} /> : <ChevronRight size={14} className="faint" />)}
                        <button
                          className="icon-btn sm"
                          title="Run only this test"
                          disabled={!!running}
                          onClick={(e) => {
                            e.stopPropagation()
                            run(escapeFilter(c.name))
                          }}
                        >
                          <Zap />
                        </button>
                      </div>
                      {open === key && (
                        <div className="log-detail" style={{ paddingLeft: 42 }}>
                          <div className="row" style={{ marginBottom: 10 }}>
                            <button className="btn sm" onClick={() => openFile(c)}>
                              <Code2 /> Open {c.file.split('::')[0].split(/[\\/]/).pop()}:{c.line}
                            </button>
                            <button className="btn sm primary" onClick={() => askAgent(c)}>
                              <Bot /> Ask an agent to fix it
                            </button>
                          </div>
                          <pre className="code" style={{ margin: 0, padding: 12, background: 'rgba(0,0,0,.3)', borderRadius: 8, whiteSpace: 'pre-wrap', fontSize: 11.5, maxHeight: 320, overflow: 'auto' }}>
                            {c.details}
                          </pre>
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}

// --filter is a regex; test names are taken literally.
function escapeFilter(name: string) {
  return name.replace(/[.*+?^${}()|[\]\\/]/g, '\\$&')
}
