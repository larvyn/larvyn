import { useEffect, useMemo, useState } from 'react'
import { Window } from '@wailsio/runtime'
import {
  Bot,
  Box,
  Braces,
  ChevronDown,
  ChevronRight,
  Copy,
  Database,
  FileCode2,
  FlaskConical,
  FolderOpen,
  FolderTree,
  GitBranch,
  Globe,
  LayoutDashboard,
  Maximize2,
  Minus,
  Package,
  PanelBottom,
  Play,
  Plus,
  ScrollText,
  Search,
  Settings,
  Square,
  SquareTerminal,
  Terminal,
  TestTube2,
  Timer,
  Trash2,
  Waypoints,
  X,
  Zap,
  Puzzle,
} from 'lucide-react'
import { SystemService } from '../../bindings/larvyn/internal/system'
import { StackService, type StackItem } from '../../bindings/larvyn/internal/stack'
import { TerminalService } from '../../bindings/larvyn/internal/terminal'
import {
  addProject,
  agentStatus,
  fail,
  openPalette,
  selectProject,
  setDrawerHeight,
  setView,
  showConsole,
  useActive,
  useStore,
  type View,
} from '../lib/store'
import { PLUGINS_CHANGED, toggleService } from './pluginRun'
import { usePluginViews, viewKey } from './PluginView'
import { cx, initials, mod } from '../lib/util'
import { Logo, TermPane, Toggle, useNow } from './ui'
import { activity } from '../lib/terms'
import { FeedbackButton, TitleFeedback } from './Feedback'
import { askRemoveProject } from './removeProject'

export function TitleBar() {
  const platform = useStore((s) => s.platform)
  const { project, info } = useActive()
  const view = useStore((s) => s.view)
  return (
    <div className={cx('titlebar', platform === 'darwin' && 'mac')} onDoubleClick={() => Window.ToggleMaximise()}>
      {platform !== 'darwin' && (
        <div className="brand">
          <div className="brand-mark">
            <Logo />
          </div>
          Larvyn
        </div>
      )}
      {project && (
        <div className="crumbs">
          <ChevronRight style={{ width: 13, height: 13, color: 'var(--text-4)' }} />
          <b>{project.name}</b>
          {info?.branch && (
            <span className="badge mono" style={{ height: 19, fontSize: 10.5, gap: 4 }}>
              <GitBranch style={{ width: 10, height: 10 }} />
              {info.branch}
            </span>
          )}
          <ChevronRight style={{ width: 13, height: 13, color: 'var(--text-4)' }} />
          <span>{NAV_ITEMS.find((n) => n.view === view)?.label ?? (view.startsWith('plugin:') ? 'Plugin' : 'Settings')}</span>
        </div>
      )}
      <button className="searchbar" onClick={() => openPalette()}>
        <Search />
        <span className="grow">Search, run artisan, start services…</span>
        <kbd>{mod}</kbd>
        <kbd>K</kbd>
      </button>
      <TitleFeedback />
      {platform !== 'darwin' && (
        <div className="winctl">
          <button onClick={() => Window.Minimise()} title="Minimise">
            <Minus />
          </button>
          <button onClick={() => Window.ToggleMaximise()} title="Maximise">
            <Maximize2 style={{ width: 12, height: 12 }} />
          </button>
          <button className="close" onClick={() => Window.Close()} title="Close">
            <X />
          </button>
        </div>
      )}
    </div>
  )
}

export function Rail() {
  const projects = useStore((s) => s.projects)
  const activeId = useStore((s) => s.activeId)
  const sessions = useStore((s) => s.sessions)
  return (
    <div className="rail">
      {projects.map((p) => {
        const running = sessions.filter((s) => s.projectId === p.id && s.running && s.kind === 'service').length
        const bells = sessions.filter((s) => s.projectId === p.id && s.running && s.bell).length
        return (
          <button
            key={p.id}
            className={cx('rail-item', p.id === activeId && 'active')}
            style={{ background: p.color }}
            title={`${p.name}\n${p.path}`}
            onClick={() => selectProject(p.id)}
          >
            {initials(p.name)}
            {running > 0 && <span className="dot" />}
            {bells > 0 && <span className="bell">{bells}</span>}
          </button>
        )
      })}
      {projects.length > 0 && <div className="rail-sep" />}
      <button className="rail-btn" title="Open a Laravel project" onClick={addProject}>
        <FolderOpen />
      </button>
      <button className="rail-btn" title="Create a new Laravel app" onClick={() => useStore.setState({ newProjectOpen: true })}>
        <Plus />
      </button>
      <div className="spacer" />
      <button className="rail-btn solid" title="Settings" onClick={() => setView('settings')}>
        <Settings />
      </button>
    </div>
  )
}

const NAV: { group: string; items: { view: View; label: string; icon: any; hint?: string }[] }[] = [
  {
    group: 'Workspace',
    items: [
      { view: 'overview', label: 'Overview', icon: LayoutDashboard, hint: '1' },
      { view: 'agents', label: 'Agents', icon: Bot, hint: '2' },
      { view: 'terminals', label: 'Terminals', icon: SquareTerminal, hint: '3' },
    ],
  },
  {
    group: 'Laravel',
    items: [
      { view: 'artisan', label: 'Artisan', icon: Zap, hint: '4' },
      { view: 'routes', label: 'Routes', icon: Waypoints, hint: '5' },
      { view: 'models', label: 'Models', icon: Box, hint: '6' },
      { view: 'database', label: 'Database', icon: Database, hint: '7' },
      { view: 'tinker', label: 'Tinker', icon: FlaskConical, hint: '8' },
      { view: 'tests', label: 'Tests', icon: TestTube2 },
      { view: 'queues', label: 'Queues & Schedule', icon: Timer },
      { view: 'logs', label: 'Logs', icon: ScrollText, hint: '9' },
    ],
  },
  {
    group: 'Project',
    items: [
      { view: 'files', label: 'Files', icon: FolderTree },
      { view: 'git', label: 'Source control', icon: GitBranch },
      { view: 'env', label: 'Environment', icon: Braces },
      { view: 'deps', label: 'Dependencies', icon: Package },
    ],
  },
]
export const NAV_ITEMS = NAV.flatMap((g) => g.items)

export function Sidebar() {
  const { id, project, info } = useActive()
  const view = useStore((s) => s.view)
  const open = useStore((s) => s.sidebar)
  const sessions = useStore((s) => s.sessions)
  const logErrors = useStore((s) => (id ? s.logErrors[id] ?? 0 : 0))
  const [stack, setStack] = useState<StackItem[]>([])
  const pluginViews = usePluginViews()
  const mine = sessions.filter((s) => s.projectId === id)
  const sessKey = mine.map((s) => s.id + s.running).join()

  useEffect(() => {
    if (!id) return
    const load = () => StackService.Items(id).then((x) => setStack(x ?? [])).catch(() => setStack([]))
    load()
    // Plugins add and remove stack items without any session changing.
    window.addEventListener(PLUGINS_CHANGED, load)
    return () => window.removeEventListener(PLUGINS_CHANGED, load)
  }, [id, sessKey, info?.hasVendor, info?.siteUrl])

  if (!project) return <div className="sidebar collapsed" />
  const agents = mine.filter((s) => s.kind === 'agent' && s.running).length
  const terms = mine.filter((s) => s.kind === 'shell' && s.running).length
  const counts: Partial<Record<View, { n: number; hot?: boolean }>> = {
    agents: agents ? { n: agents, hot: mine.some((s) => s.kind === 'agent' && s.bell) } : undefined,
    terminals: terms ? { n: terms } : undefined,
    git: info?.dirty ? { n: info.dirty } : undefined,
    logs: logErrors ? { n: logErrors, hot: true } : undefined,
    models: info?.counts?.models ? { n: info.counts.models } : undefined,
  }
  const running = stack.filter((s) => s.session?.running).length
  return (
    <div className={cx('sidebar', !open && 'collapsed')}>
      <div className="side-head">
        <h2>
          <span style={{ width: 9, height: 9, borderRadius: 3, background: project.color, flexShrink: 0 }} />
          <span className="ellipsis grow">{project.name}</span>
          <button className="icon-btn sm danger side-remove" title="Remove from Larvyn (your files stay)" onClick={() => askRemoveProject(project)}>
            <Trash2 />
          </button>
        </h2>
        <div className="meta">
          {info?.isLaravel ? (
            <span className="badge accent">Laravel {info.laravelVersion?.split('.').slice(0, 2).join('.')}</span>
          ) : info ? (
            <span className="badge">Not Laravel</span>
          ) : null}
          {info?.appEnv && <span className={cx('badge', info.appEnv === 'production' ? 'red' : 'green')}>{info.appEnv}</span>}
        </div>
      </div>
      <div className="side-scroll">
        {NAV.filter((g) => g.group !== 'Laravel' || !info || info.isLaravel).map((g) => (
          <div className="nav-group" key={g.group}>
            <div className="nav-label">{g.group}</div>
            {g.items.map((it) => {
              const c = counts[it.view]
              return (
                <button key={it.view} className={cx('nav-item', view === it.view && 'active')} onClick={() => setView(it.view)}>
                  <it.icon />
                  {it.label}
                  {c && <span className={cx('count', c.hot && 'hot')}>{c.n}</span>}
                </button>
              )
            })}
          </div>
        ))}
        {(pluginViews?.length ?? 0) > 0 && (
          <div className="nav-group">
            <div className="nav-label">Plugins</div>
            {pluginViews!.map((v) => (
              <button key={viewKey(v)} className={cx('nav-item', view === viewKey(v) && 'active')} title={`From the ${v.pluginName} plugin`} onClick={() => setView(viewKey(v))}>
                <Puzzle />
                {v.title}
              </button>
            ))}
          </div>
        )}
      </div>
      {stack.some((s) => s.available || s.session) && (
        <div className="side-stack">
          <div className="head">
            <span className="nav-label" style={{ padding: 0 }}>
              Stack {running > 0 && <span style={{ color: 'var(--green)' }}>· {running} up</span>}
            </span>
            {running > 0 ? (
              <button className="icon-btn sm" title="Stop all" onClick={() => StackService.StopAll(id!)}>
                <Square />
              </button>
            ) : (
              <button className="icon-btn sm" title="Start the dev stack (what php artisan dev runs)" onClick={() => StackService.StartAll(id!, []).catch(fail('Some services did not start'))}>
                <Play />
              </button>
            )}
          </div>
          {stack.map((s) => {
            const on = !!s.session?.running
            // Flock serves the app already: shown as up, with its URL, no toggle.
            if (s.servedBy && !on)
              return (
                <div key={s.key} className="svc on" title={s.description}>
                  <span className="status-dot on" />
                  <span className="label">{s.label}</span>
                  <button className="icon-btn sm" title={s.url} onClick={() => SystemService.OpenURL(s.url)}>
                    <Globe />
                  </button>
                  <span className="badge" style={{ fontSize: 10.5, padding: '0 6px' }}>
                    {s.servedBy}
                  </span>
                </div>
              )
            return (
              <div key={s.key} className={cx('svc', on && 'on')} title={s.reason || s.description}>
                <span className={cx('status-dot', on && 'on', s.session && !on && !s.session.stopped && s.session.exitCode !== 0 && 'err')} />
                <span className="label" onClick={() => s.session && showConsole(true, s.session.id)}>
                  {s.label}
                  {s.plugin && <Puzzle style={{ width: 11, height: 11, marginLeft: 5, opacity: 0.6, verticalAlign: -1 }} />}
                </span>
                {on && s.url && (
                  <button className="icon-btn sm" title={s.url} onClick={() => SystemService.OpenURL(s.url)}>
                    <Globe />
                  </button>
                )}
                <Toggle on={on} disabled={!s.available && !on} onChange={() => toggleService(id!, s.key, on)} />
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

export function StatusBar() {
  const { id, info } = useActive()
  const sessions = useStore((s) => s.sessions)
  const drawer = useStore((s) => s.drawer)
  const logErrors = useStore((s) => (id ? s.logErrors[id] ?? 0 : 0))
  const now = useNow(1500)
  const mine = sessions.filter((s) => s.projectId === id)
  const services = mine.filter((s) => s.kind === 'service' && s.running)
  const agents = mine.filter((s) => s.kind === 'agent' && s.running)
  const working = agents.filter((a) => agentStatus({ ...a, lastActivity: activity.get(a.id) ?? a.lastActivity } as any, now) === 'working').length
  const runs = mine.filter((s) => s.kind === 'run')
  return (
    <div className="statusbar">
      {info?.branch && (
        <button className="sb-item" onClick={() => setView('git')}>
          <GitBranch />
          {info.branch}
          {info.dirty > 0 && <span style={{ color: 'var(--yellow)' }}>●{info.dirty}</span>}
        </button>
      )}
      <button className="sb-item" onClick={() => setView('overview')}>
        <span className={cx('status-dot', services.length > 0 && 'on')} style={{ width: 6, height: 6, boxShadow: 'none' }} />
        {services.length ? `${services.length} service${services.length > 1 ? 's' : ''} running` : 'Stack stopped'}
      </button>
      {agents.length > 0 && (
        <button className="sb-item" onClick={() => setView('agents')}>
          <Bot />
          {agents.length} agent{agents.length > 1 ? 's' : ''}
          {working > 0 && <span style={{ color: 'var(--accent)' }}>· {working} working</span>}
        </button>
      )}
      {logErrors > 0 && (
        <button className="sb-item" style={{ color: 'var(--red)' }} onClick={() => setView('logs')}>
          <ScrollText />
          {logErrors} new error{logErrors > 1 ? 's' : ''}
        </button>
      )}
      <div className="spacer" />
      {info?.isLaravel && (
        <span className="sb-item">
          <FileCode2 />
          Laravel {info.laravelVersion} · PHP {info.phpConstraint}
        </span>
      )}
      {info?.dbConnection && (
        <span className="sb-item">
          <Database />
          {info.dbConnection}
        </span>
      )}
      <FeedbackButton />
      <button className={cx('sb-item')} onClick={() => showConsole()} style={drawer.open ? { color: 'var(--text)' } : undefined}>
        <PanelBottom />
        Console{runs.length > 0 && ` (${runs.length})`}
        <kbd style={{ height: 15, fontSize: 9.5 }}>{mod}J</kbd>
      </button>
    </div>
  )
}

export function Drawer() {
  const { id } = useActive()
  const drawer = useStore((s) => s.drawer)
  const sessions = useStore((s) => s.sessions)
  const runs = useMemo(
    () => sessions.filter((s) => (s.projectId === id || (!s.projectId && s.kind === 'run')) && (s.kind === 'run' || s.kind === 'service')),
    [sessions, id],
  )
  const active = runs.find((r) => r.id === drawer.active) ?? runs.filter((r) => r.kind === 'run').at(-1) ?? runs[0]
  if (!drawer.open) return null
  const startDrag = (e: React.MouseEvent) => {
    const startY = e.clientY
    const startH = drawer.height
    const move = (ev: MouseEvent) => setDrawerHeight(Math.max(140, Math.min(window.innerHeight - 200, startH + startY - ev.clientY)))
    const up = () => {
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
    }
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
  }
  return (
    <div className="drawer" style={{ height: drawer.height }}>
      <div className="resize" onMouseDown={startDrag} />
      <div className="drawer-tabs">
        <Terminal style={{ width: 14, height: 14, color: 'var(--text-3)', marginRight: 6, flexShrink: 0 }} />
        {runs.length === 0 && <span className="faint" style={{ fontSize: 12 }}>Commands you run from Artisan, the palette or the doctor show up here.</span>}
        {runs.map((r) => (
          <button key={r.id} className={cx('dtab', active?.id === r.id && 'active')} onClick={() => showConsole(true, r.id)}>
            <span className={cx('status-dot', r.running ? (r.kind === 'service' ? 'on' : 'busy') : r.exitCode === 0 || r.stopped ? '' : 'err')} style={{ width: 6, height: 6, boxShadow: 'none' }} />
            <span className="ellipsis">{r.title}</span>
            {r.kind === 'run' && (
              <span
                className="icon-btn sm x"
                onClick={(e) => {
                  e.stopPropagation()
                  TerminalService.Close(r.id)
                }}
              >
                <X />
              </span>
            )}
          </button>
        ))}
        <div className="spacer" />
        {runs.some((r) => r.kind === 'run' && !r.running) && (
          <button
            className="icon-btn sm"
            title="Clear finished"
            onClick={() => runs.filter((r) => r.kind === 'run' && !r.running).forEach((r) => TerminalService.Close(r.id))}
          >
            <Trash2 />
          </button>
        )}
        {active && (
          <button className="icon-btn sm" title="Copy output" onClick={() => TerminalService.Output(active.id).then((t) => navigator.clipboard.writeText(t))}>
            <Copy />
          </button>
        )}
        <button className="icon-btn sm" title="Hide" onClick={() => showConsole(false)}>
          <ChevronDown />
        </button>
      </div>
      {active ? <TermPane key={active.id} id={active.id} /> : <div className="grow" />}
    </div>
  )
}

