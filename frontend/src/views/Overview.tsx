import { useEffect, useState } from 'react'
import {
  AlertTriangle,
  ArrowUpRight,
  Bot,
  Box,
  Check,
  Code2,
  Cog,
  Database,
  FileCode2,
  FolderOpen,
  GitCommitHorizontal,
  Globe,
  Layers,
  Mail,
  Package,
  Play,
  Puzzle,
  Radio,
  RefreshCw,
  Route,
  Server,
  Sparkles,
  Square,
  SquareTerminal,
  TestTube2,
  Trash2,
  Workflow,
  Wrench,
} from 'lucide-react'
import { LaravelService, ProjectService, StackService, SystemService, type StackItem } from '../../bindings/larvyn'
import { GitService, type Commit } from '../../bindings/larvyn/internal/git'
import {
  artisanRun,
  composerRun,
  fail,
  newTerminal,
  refreshInfo,
  runInConsole,
  setView,
  showConsole,
  toast,
  toggleService,
  useActive,
  useStore,
  type View,
} from '../lib/store'
import { ago, cx } from '../lib/util'
import { Toggle, confirmDialog, promptDialog } from '../components/ui'
import { Brand } from '../components/Brand'

const KIND_COLOR: Record<string, string> = {
  queue: 'orange', debug: 'yellow', monitoring: 'cyan', realtime: 'purple', server: 'red', auth: 'blue', starter: 'accent',
  billing: 'green', search: 'cyan', flags: 'purple', routing: 'blue', docker: 'blue', quality: 'green', ai: 'purple',
  admin: 'orange', frontend: 'purple', testing: 'green', storage: 'cyan', data: 'blue', cache: 'red',
}

interface Check {
  key: string
  label: string
  detail: string
  ok: boolean | null
  fix?: { label: string; run: () => void }
}

export function Overview() {
  const { id, project, info } = useActive()
  const sessions = useStore((s) => s.sessions)
  const [stack, setStack] = useState<StackItem[]>([])
  const [commits, setCommits] = useState<Commit[]>([])
  const [pending, setPending] = useState<number | null>(null)
  const [storageLinked, setStorageLinked] = useState<boolean | null>(null)
  const sessKey = sessions.filter((s) => s.projectId === id).map((s) => s.id + s.running).join()

  useEffect(() => {
    if (!id) return
    StackService.Items(id).then((s) => setStack(s ?? [])).catch(() => {})
  }, [id, sessKey, info?.hasVendor, info?.hasNodeModules])

  useEffect(() => {
    if (!id) return
    setPending(null)
    GitService.Status(id).then((s) => setCommits(s.commits?.slice(0, 6) ?? [])).catch(() => setCommits([]))
    LaravelService.PathExists(id, 'public/storage').then(setStorageLinked)
    if (info?.hasVendor && info?.hasEnv) {
      LaravelService.Bridge(id, 'migrations', {})
        .then((r: any) => setPending(r?.ok ? r.migrations.filter((m: any) => !m.ran).length : null))
        .catch(() => setPending(null))
    }
  }, [id, info?.hasVendor, info?.hasEnv, info?.sqliteMissing])

  if (!project || !info) return <div className="view pad" />

  const php = useStore.getState().settings?.phpBinary || 'php'
  const checks: Check[] = info.isLaravel
    ? [
        {
          key: 'vendor', label: 'Composer dependencies', detail: info.hasVendor ? 'vendor/ is installed' : 'vendor/ is missing', ok: info.hasVendor,
          fix: { label: 'composer install', run: () => composerRun(id!, ['install']) },
        },
        {
          key: 'env', label: 'Environment file', detail: info.hasEnv ? '.env exists' : 'No .env yet', ok: info.hasEnv,
          fix: info.hasEnvExample ? { label: 'Copy .env.example', run: () => runInConsole(id!, [php, '-r', "copy('.env.example', '.env');"], 'cp .env.example .env') } : undefined,
        },
        {
          key: 'key', label: 'Application key', detail: info.hasAppKey ? 'APP_KEY is set' : 'APP_KEY is empty: sessions and encryption fail', ok: info.hasEnv ? info.hasAppKey : null,
          fix: { label: 'key:generate', run: () => artisanRun(id!, ['key:generate']) },
        },
        ...(info.sqliteFile
          ? [
              {
                key: 'sqlite', label: 'SQLite database',
                detail: info.sqliteMissing ? `${info.sqliteFile} does not exist: every query fails` : 'Database file exists',
                ok: !info.sqliteMissing,
                fix: {
                  label: 'Create database',
                  run: () =>
                    ProjectService.EnsureSQLite(id!)
                      .then(() => refreshInfo(id!))
                      .then(() => artisanRun(id!, ['migrate', '--graceful', '--force']))
                      .catch(fail('Could not create the database')),
                },
              },
            ]
          : []),
        {
          key: 'node', label: 'Frontend dependencies', detail: Object.keys(info.npmScripts ?? {}).length === 0 ? 'No package.json' : info.hasNodeModules ? 'node_modules/ is installed' : 'node_modules/ is missing',
          ok: Object.keys(info.npmScripts ?? {}).length === 0 ? true : info.hasNodeModules,
          fix: { label: 'npm install', run: () => runInConsole(id!, ['npm', 'install'], 'npm install') },
        },
        {
          key: 'migrations', label: 'Database migrations',
          detail: pending == null ? 'Checking…' : pending === 0 ? 'Everything is migrated' : `${pending} pending migration${pending > 1 ? 's' : ''}`,
          ok: pending == null ? null : pending === 0,
          fix: { label: 'migrate', run: () => artisanRun(id!, ['migrate']) },
        },
        {
          key: 'storage', label: 'Public storage link', detail: storageLinked ? 'public/storage is linked' : 'public/storage is not linked', ok: storageLinked,
          fix: { label: 'storage:link', run: () => artisanRun(id!, ['storage:link']) },
        },
        {
          key: 'debug', label: 'Debug mode', detail: info.appEnv === 'production' && info.appDebug ? 'APP_DEBUG=true in production leaks secrets' : `APP_DEBUG=${info.appDebug}`,
          ok: !(info.appEnv === 'production' && info.appDebug),
          fix: { label: 'Turn off', run: () => LaravelService.SetEnv(id!, 'APP_DEBUG', 'false').then(() => refreshInfo(id!)) },
        },
      ]
    : []
  const problems = checks.filter((c) => c.ok === false).length
  const running = stack.filter((s) => s.session?.running)
  // While php artisan serve runs, open it: requests then show up in its output. Otherwise Herd's site.
  const siteUrl = running.find((s) => s.key === 'serve')?.url || info.herdUrl || info.appUrl

  const go = (v: View) => () => setView(v)
  const counts = info.counts ?? {}
  const packages = info.packages ?? []
  const stats: { k: string; v: number | undefined; icon: any; view?: View }[] = [
    { k: 'Models', v: counts.models, icon: Box, view: 'models' },
    { k: 'Controllers', v: counts.controllers, icon: Route, view: 'routes' },
    { k: 'Migrations', v: counts.migrations, icon: Database, view: 'database' },
    { k: 'Jobs', v: counts.jobs, icon: Workflow, view: 'queues' },
    { k: 'Events', v: counts.events, icon: Radio },
    { k: 'Requests', v: counts.requests, icon: FileCode2 },
    { k: 'Mail & notifications', v: (counts.mail ?? 0) + (counts.notifications ?? 0), icon: Mail },
    { k: 'Tests', v: counts.tests, icon: TestTube2, view: 'tests' },
  ]

  return (
    <div className="view pad">
      <div className="hero" style={{ ['--hero' as any]: `color-mix(in srgb, ${project.color} 22%, transparent)` }}>
        <div className="row" style={{ alignItems: 'flex-start' }}>
          <div className="grow">
            <div className="row" style={{ gap: 10, marginBottom: 8 }}>
              {info.isLaravel && <span className="badge accent">Laravel {info.laravelVersion}</span>}
              {info.phpConstraint && <span className="badge">PHP {info.phpConstraint}</span>}
              {info.appEnv && <span className={cx('badge', info.appEnv === 'production' ? 'red' : 'green')}>{info.appEnv}</span>}
              {info.appDebug && <span className="badge yellow">debug</span>}
              {info.testRunner && <span className="badge blue">{info.testRunner}</span>}
              {info.frontend?.map((f) => (
                <span key={f} className="badge purple">
                  <Brand id={f} size={11} />
                  {f}
                </span>
              ))}
            </div>
            <h1>{info.appName && info.appName !== 'Laravel' ? info.appName : project.name}</h1>
            <div className="path">{project.path}</div>
          </div>
          <div className="row">
            <button className="icon-btn" title="Project settings" onClick={() => editProject(project)}>
              <Cog />
            </button>
            <button
              className="icon-btn danger"
              title="Remove from Larvyn"
              onClick={async () => {
                if (await confirmDialog({ title: `Remove ${project.name}?`, message: 'Larvyn forgets the project and stops its terminals. Your files are not touched.', confirm: 'Remove', danger: true }))
                  ProjectService.Remove(project.id)
              }}
            >
              <Trash2 />
            </button>
          </div>
        </div>
        <div className="row wrap" style={{ marginTop: 18 }}>
          {running.length === 0 ? (
            <button className="btn primary" disabled={!info.isLaravel} onClick={() => StackService.StartAll(id!, []).catch(fail('Some services did not start'))}>
              <Play /> Start dev stack
            </button>
          ) : (
            <button className="btn" onClick={() => StackService.StopAll(id!)}>
              <Square /> Stop {running.length} service{running.length > 1 ? 's' : ''}
            </button>
          )}
          {siteUrl && (
            <button className="btn" onClick={() => SystemService.OpenURL(siteUrl)}>
              <Globe /> Open site <ArrowUpRight style={{ width: 12, height: 12, opacity: 0.6 }} />
            </button>
          )}
          <button className="btn" onClick={() => SystemService.OpenInEditor(project.path, 1).catch(fail('Could not open editor'))}>
            <Code2 /> Editor
          </button>
          <button className="btn" onClick={() => newTerminal(id!)}>
            <SquareTerminal /> Terminal
          </button>
          <button className="btn" onClick={go('agents')}>
            <Bot /> New agent task
          </button>
          <button className="btn ghost" onClick={() => SystemService.OpenFolder(project.path)}>
            <FolderOpen /> Reveal
          </button>
        </div>
      </div>

      {!info.exists && <div className="error-box" style={{ marginBottom: 16 }}>This folder no longer exists. Remove the project or restore the folder.</div>}

      <div className="grid-3">
        <div className="col" style={{ gap: 16 }}>
          <div className="card">
            <div className="card-head">
              <h3>
                <Server /> Dev stack
              </h3>
              <span className="spacer" />
              <button className="btn sm ghost" onClick={() => showConsole(true)}>
                Console
              </button>
            </div>
            <div>
              {stack.map((s) => {
                const on = !!s.session?.running
                return (
                  <div key={s.key} className="check-row">
                    <span className={cx('status-dot', on && 'on', s.session && !on && !s.session.stopped && s.session.exitCode !== 0 && 'err')} />
                    <div className="t">
                      <b>{s.label}</b>
                      <span className="mono">{s.reason ? s.reason : s.description}</span>
                    </div>
                    {on && s.url && (
                      <button className="btn sm ghost" onClick={() => SystemService.OpenURL(s.url)}>
                        {s.url.replace('http://', '')} <ArrowUpRight />
                      </button>
                    )}
                    {s.session && (
                      <button className="btn sm ghost" onClick={() => showConsole(true, s.session!.id)}>
                        Output
                      </button>
                    )}
                    {on && (
                      <button className="icon-btn sm" title="Restart" onClick={() => StackService.Restart(id!, s.key).catch(fail('Restart failed'))}>
                        <RefreshCw />
                      </button>
                    )}
                    <Toggle on={on} disabled={!s.available && !on} onChange={() => toggleService(id!, s.key, on)} />
                  </div>
                )
              })}
              {stack.length === 0 && <div className="check-row faint">Nothing to run here.</div>}
            </div>
          </div>

          {info.isLaravel && (
            <div className="card">
              <div className="card-head">
                <h3>
                  <Wrench /> Doctor
                </h3>
                {problems > 0 ? <span className="badge yellow">{problems} to fix</span> : <span className="badge green">healthy</span>}
                <span className="spacer" />
                <button className="icon-btn sm" title="Re-check" onClick={() => refreshInfo(id!)}>
                  <RefreshCw />
                </button>
              </div>
              {checks.map((c) => (
                <div key={c.key} className="check-row">
                  <div className={cx('ic', c.ok === true ? 'ok' : c.ok === false ? 'bad' : 'wait')}>
                    {c.ok === true ? <Check /> : c.ok === false ? <AlertTriangle /> : <div className="spinner" style={{ width: 10, height: 10 }} />}
                  </div>
                  <div className="t">
                    <b>{c.label}</b>
                    <span>{c.detail}</span>
                  </div>
                  {c.ok === false && c.fix && (
                    <button className="btn sm" onClick={c.fix.run}>
                      <Sparkles /> {c.fix.label}
                    </button>
                  )}
                </div>
              ))}
            </div>
          )}

          <div>
            <div className="section-title" style={{ marginTop: 4 }}>
              <Layers style={{ width: 13, height: 13 }} /> Codebase
            </div>
            <div className="stats">
              {stats.map((s) => (
                <div key={s.k} className={cx('stat', s.view && 'link')} onClick={s.view ? go(s.view) : undefined}>
                  <div className="v">{s.v ?? 0}</div>
                  <div className="k">
                    <s.icon /> {s.k}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>

        <div className="col" style={{ gap: 16 }}>
          <div className="card">
            <div className="card-head">
              <h3>
                <Cog /> Configuration
              </h3>
              <span className="spacer" />
              <button className="btn sm ghost" onClick={go('env')}>
                Edit .env
              </button>
            </div>
            <div className="kv">
              <div>URL</div>
              <div className="mono">{info.appUrl || '—'}</div>
              <div>Database</div>
              <div>
                {info.dbConnection || '—'} {info.dbDatabase && <span className="faint mono">· {info.dbDatabase.split(/[\\/]/).pop()}</span>}
              </div>
              <div>Queue</div>
              <div>{info.queueConnection || '—'}</div>
              <div>Cache</div>
              <div>{info.cacheStore || '—'}</div>
              <div>Session</div>
              <div>{info.sessionDriver || '—'}</div>
              <div>Mail</div>
              <div>{info.mailMailer || '—'}</div>
            </div>
          </div>

          {packages.length > 0 && (
            <div className="card">
              <div className="card-head">
                <h3>
                  <Puzzle /> Ecosystem
                </h3>
                <span className="pill-n">{packages.length}</span>
              </div>
              <div className="card-body row wrap" style={{ gap: 6 }}>
                {packages.map((p) => (
                  <span key={p.name} className={cx('badge', KIND_COLOR[p.kind])} title={`${p.name} ${p.version}${p.dev ? ' (dev)' : ''}`}>
                    <Brand id={p.label} size={11} />
                    {p.label}
                    <span style={{ opacity: 0.6, fontWeight: 500 }}>{p.version.replace(/^[\^~]/, '').split('.').slice(0, 2).join('.')}</span>
                  </span>
                ))}
              </div>
              {!packages.some((p) => p.name === 'laravel/boost') && (
                <div className="check-row" style={{ borderTop: '1px solid var(--line)' }}>
                  <div className="ic wait">
                    <Sparkles />
                  </div>
                  <div className="t">
                    <b>Laravel Boost</b>
                    <span>Gives your agents Laravel docs, schema and Tinker over MCP</span>
                  </div>
                  <button className="btn sm" onClick={() => composerRun(id!, ['require', 'laravel/boost', '--dev'])}>
                    <Package /> Install
                  </button>
                </div>
              )}
            </div>
          )}

          {commits.length > 0 && (
            <div className="card">
              <div className="card-head">
                <h3>
                  <GitCommitHorizontal /> Recent commits
                </h3>
                <span className="spacer" />
                <button className="btn sm ghost" onClick={go('git')}>
                  Source control
                </button>
              </div>
              {commits.map((c) => (
                <div key={c.hash} className="check-row" style={{ padding: '8px 14px' }}>
                  <span className="mono faint" style={{ fontSize: 11 }}>
                    {c.hash}
                  </span>
                  <div className="t ellipsis" style={{ fontSize: 12.5 }}>
                    {c.subject}
                  </div>
                  <span className="faint" style={{ fontSize: 11, whiteSpace: 'nowrap' }}>
                    {ago(c.date)}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

async function editProject(p: { id: string; name: string; color: string; port: number; pinned: boolean; path: string }) {
  const name = await promptDialog({ title: 'Rename project', label: 'Name', value: p.name })
  if (name == null) return
  const port = await promptDialog({ title: 'HTTP server port', label: 'php artisan serve --port', value: String(p.port || 8000) })
  if (port == null) return
  await ProjectService.Update({ ...p, name: name || p.name, port: Number(port) || p.port } as any)
  toast('success', 'Project updated')
}
