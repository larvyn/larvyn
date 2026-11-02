import { create } from 'zustand'
import { Events } from '@wailsio/runtime'
import {
  AgentService,
  ProjectService,
  SystemService,
  type Project,
  type ProjectInfo,
  type SessionInfo,
  type Settings,
} from '../../bindings/larvyn'
import { StackService } from '../../bindings/larvyn/internal/stack'
import { TerminalService, type ProjectLayout, type SessionSpec } from '../../bindings/larvyn/internal/terminal'
import { errMsg, load, save } from './util'

export type View =
  | 'overview'
  | 'terminals'
  | 'agents'
  | 'artisan'
  | 'routes'
  | 'models'
  | 'database'
  | 'tinker'
  | 'tests'
  | 'queues'
  | 'logs'
  | 'git'
  | 'env'
  | 'files'
  | 'deps'
  | 'settings'

export interface Toast {
  id: number
  kind: 'success' | 'error' | 'info'
  title: string
  message?: string
}

export interface TermTab {
  id: string
  panes: string[]
  dir: 'row' | 'col'
}

interface State {
  ready: boolean
  projects: Project[]
  activeId: string | null
  infos: Record<string, ProjectInfo>
  view: View
  sessions: SessionInfo[]
  settings: Settings | null
  platform: string
  paletteOpen: boolean
  paletteQuery: string
  newProjectOpen: boolean
  sidebar: boolean
  toasts: Toast[]
  tabs: Record<string, TermTab[]>
  activeTab: Record<string, string>
  drawer: { open: boolean; active: string | null; height: number }
  focus: { table?: string; agentPrompt?: string; tinker?: string; logEntry?: string; artisan?: string; file?: string } // cross-view hand-offs
  logErrors: Record<string, number>
}

const initial: State = {
  ready: false,
  projects: [],
  activeId: null,
  infos: {},
  view: load<View>('view', 'overview'),
  sessions: [],
  settings: null,
  platform: 'windows',
  paletteOpen: false,
  paletteQuery: '',
  newProjectOpen: false,
  sidebar: true,
  toasts: [],
  tabs: {},
  activeTab: {},
  drawer: { open: false, active: null, height: load('drawerHeight', 280) },
  focus: {},
  logErrors: {},
}

export const useStore = create<State>(() => initial)
const set = useStore.setState
const get = useStore.getState

let toastSeq = 0
const toastTimers = new Map<number, ReturnType<typeof setTimeout>>()
/** Shows a toast. One already on screen with the same kind, title and message
 * is not repeated: its timer restarts instead, so nothing ever shows twice. */
export function toast(kind: Toast['kind'], title: string, message?: string) {
  const ttl = kind === 'error' ? 7000 : 3800
  const same = get().toasts.find((t) => t.kind === kind && t.title === title && (t.message ?? '') === (message ?? ''))
  const id = same?.id ?? ++toastSeq
  if (!same) set((s) => ({ toasts: [...s.toasts.slice(-4), { id, kind, title, message }] }))
  clearTimeout(toastTimers.get(id))
  toastTimers.set(
    id,
    setTimeout(() => {
      toastTimers.delete(id)
      set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }))
    }, ttl),
  )
}
export const fail = (title: string) => (e: unknown) => toast('error', title, errMsg(e))

export function useActive() {
  const id = useStore((s) => s.activeId)
  const project = useStore((s) => s.projects.find((p) => p.id === s.activeId) ?? null)
  const info = useStore((s) => (id ? s.infos[id] : undefined))
  return { id, project, info }
}

let booting: Promise<void> | null = null
/** Loads the app state and subscribes to backend events, once: React may run
 * the mount effect twice, which would register every listener twice (and run
 * LARVYN_RUN / LARVYN_AGENT twice). */
export function boot() {
  booting ??= doBoot()
  return booting
}

async function doBoot() {
  SystemService.Log('info', 'boot start')
  // Bring back the terminals of the previous run first, so the session list
  // below already holds them.
  const layout = await TerminalService.Restore().catch((e) => {
    SystemService.Log('error', 'restore failed: ' + errMsg(e))
    return null
  })
  const [projectList, settings, platform, active, sessionList, launch] = await Promise.all([
    ProjectService.List(),
    SystemService.Settings(),
    SystemService.Platform(),
    ProjectService.Active(),
    TerminalService.List(),
    SystemService.Launch(),
  ])
  const projects = projectList ?? []
  const sessions = sessionList ?? []
  // No saved project means the last one was closed: start on the welcome screen.
  const activeId = active ? (projects.find((p) => p.id === active)?.id ?? projects[0]?.id ?? null) : null
  SystemService.Log('info', `boot loaded ${projects.length} projects, active ${activeId}, launch ${JSON.stringify(launch)}`)
  const ids = new Set(sessions.map((x) => x.id))
  const tabs: Record<string, TermTab[]> = {}
  const activeTab: Record<string, string> = {}
  for (const [pid, pl] of Object.entries(layout ?? {})) {
    const ts = (pl?.tabs ?? [])
      .map((t) => ({ id: t.id, dir: (t.dir === 'col' ? 'col' : 'row') as TermTab['dir'], panes: (t.panes ?? []).filter((p) => ids.has(p)) }))
      .filter((t) => t.panes.length > 0)
    if (!ts.length) continue
    tabs[pid] = ts
    activeTab[pid] = ts.some((t) => t.id === pl?.active) ? pl!.active : ts[0].id
  }
  set({ projects, settings, platform, activeId, sessions, tabs, activeTab, ready: true, ...(launch.view ? { view: launch.view as View } : {}) })
  syncLayout()
  if (launch.palette) openPalette(launch.palette === '1' ? '' : launch.palette)
  if (activeId && launch.file) handoff('files', { file: launch.file })
  if (activeId && launch.run) newTerminal(activeId, { command: launch.run, title: launch.run })
  if (activeId && launch.agent) AgentService.Launch({ projectId: activeId, agent: launch.agent } as any).catch(fail('Could not start agent'))
  if (activeId) refreshInfo(activeId)

  Events.On('projects:changed', (ev: any) => {
    const list = (ev.data ?? []) as Project[]
    set((s) => ({
      projects: list,
      activeId: s.activeId === null || list.some((p) => p.id === s.activeId) ? s.activeId : list[0]?.id ?? null,
    }))
  })
  Events.On('sessions:changed', (ev: any) => onSessions((ev.data ?? []) as SessionInfo[]))
  Events.On('term:exit', (ev: any) => onExit(ev.data.id, ev.data.code, !!ev.data.stopped))
}

/** Sends the terminal tabs to the backend whenever they change, so they come
 * back after a restart. */
function syncLayout() {
  let timer: ReturnType<typeof setTimeout> | undefined
  // onSessions rebuilds the tabs on every session event, so compare what
  // would be sent instead of the objects.
  let sent = ''
  useStore.subscribe((s, prev) => {
    if (s.tabs === prev.tabs && s.activeTab === prev.activeTab) return
    clearTimeout(timer)
    timer = setTimeout(() => {
      const { tabs, activeTab } = get()
      const layout: Record<string, ProjectLayout> = {}
      for (const [pid, ts] of Object.entries(tabs)) {
        if (ts.length) layout[pid] = { tabs: ts.map((t) => ({ id: t.id, panes: t.panes, dir: t.dir })), active: activeTab[pid] ?? '' }
      }
      const json = JSON.stringify(layout)
      if (json === sent) return
      sent = json
      TerminalService.SetLayout(layout).catch(() => {})
    }, 250)
  })
}

/** Starts a stopped terminal again in place; agents resume their conversation. */
export async function relaunch(id: string) {
  try {
    await TerminalService.Relaunch(id)
  } catch (e) {
    toast('error', 'Could not relaunch', errMsg(e))
  }
}

function onSessions(list: SessionInfo[]) {
  const ids = new Set(list.map((s) => s.id))
  set((s) => {
    const tabs: Record<string, TermTab[]> = {}
    for (const [pid, ts] of Object.entries(s.tabs)) {
      tabs[pid] = ts.map((t) => ({ ...t, panes: t.panes.filter((p) => ids.has(p)) })).filter((t) => t.panes.length > 0)
    }
    const drawer = s.drawer.active && !ids.has(s.drawer.active) ? { ...s.drawer, active: null } : s.drawer
    return { sessions: list, tabs, drawer }
  })
}

function onExit(id: string, code: number, stopped = false) {
  const sess = get().sessions.find((s) => s.id === id)
  if (!sess) return
  if (sess.meta?.target && sess.key === 'create') {
    if (code === 0) {
      ProjectService.Add(sess.meta.target)
        .then(async (p) => {
          toast('success', 'Project created', p.name)
          selectProject(p.id)
          // Old Laravel installers write APP_URL=http://localhost:8000:8000,
          // which breaks every artisan command.
          if (await ProjectService.RepairEnv(p.id).catch(() => false))
            toast('info', 'Fixed APP_URL in .env', 'Your Laravel installer wrote the port twice. Update it in Settings → Toolchain.')
          // When asked for in the New app dialog: the installer does not always
          // leave the SQLite file behind (or migrate it), and every request
          // then fails with "Database file at path [...] does not exist".
          const created = sess.meta?.sqlite === '1' && (await ProjectService.EnsureSQLite(p.id).catch((e) => {
            fail('Could not create the SQLite database')(e)
            return false
          }))
          if (created) artisanRun(p.id, ['migrate', '--graceful', '--force'])
        })
        .catch(fail('Could not add project'))
    } else toast('error', 'Project creation failed', `exit code ${code}`)
    return
  }
  if (sess.kind === 'run' && sess.projectId) {
    if (code === 0) toast('success', 'Finished', sess.title)
    else toast('error', `Failed (exit ${code})`, sess.title)
    if (sess.projectId === get().activeId) refreshInfo(sess.projectId)
  }
  if (sess.kind === 'service' && !stopped && code !== 0 && code !== 1 && code !== -1) {
    toast('error', `${sess.title} stopped`, `exit code ${code}`)
  }
}

export async function refreshInfo(id: string) {
  try {
    const info = await ProjectService.Info(id)
    set((s) => ({ infos: { ...s.infos, [id]: info } }))
  } catch (e) {
    console.error(e)
  }
}

export function selectProject(id: string) {
  set({ activeId: id })
  ProjectService.Touch(id)
  refreshInfo(id)
}

/** Updates the global laravel/installer in a console tab. */
export async function updateLaravelInstaller() {
  try {
    const sess = await SystemService.UpdateLaravelInstaller()
    set((s) => ({ sessions: [...s.sessions, sess], drawer: { ...s.drawer, open: true, active: sess.id } }))
    toast('info', 'Updating the Laravel installer', 'composer global update laravel/installer')
  } catch (e) {
    toast('error', 'Could not update the Laravel installer', errMsg(e))
  }
}

/** Stops a project's terminals and services and goes back to the welcome
 * screen. The project stays in the rail. */
export async function closeProject(id: string) {
  try {
    await ProjectService.Close(id)
    set((s) => (s.activeId === id ? { activeId: null } : {}))
  } catch (e) {
    toast('error', 'Could not close the project', errMsg(e))
  }
}

export function setView(view: View) {
  set({ view })
  save('view', view)
}

export function handoff(view: View, focus: State['focus']) {
  set({ view, focus })
}

export function openPalette(query = '') {
  set({ paletteOpen: true, paletteQuery: query })
}

export async function addProject() {
  try {
    const p = await ProjectService.Pick()
    if (p) {
      selectProject(p.id)
      toast('success', 'Project added', p.path)
    }
  } catch (e) {
    toast('error', 'Could not add project', errMsg(e))
  }
}

// ---- terminals -------------------------------------------------------------

function newTabId() {
  return Math.random().toString(36).slice(2, 9)
}

export function openInTab(projectId: string, sessionId: string) {
  set((s) => {
    const tabs = s.tabs[projectId] ?? []
    const existing = tabs.find((t) => t.panes.includes(sessionId))
    if (existing) return { activeTab: { ...s.activeTab, [projectId]: existing.id }, view: 'terminals' }
    const tab = { id: newTabId(), panes: [sessionId], dir: 'row' as const }
    return { tabs: { ...s.tabs, [projectId]: [...tabs, tab] }, activeTab: { ...s.activeTab, [projectId]: tab.id }, view: 'terminals' }
  })
}

export async function newTerminal(projectId: string, opts: Partial<SessionSpec> = {}, split?: 'row' | 'col') {
  try {
    const sess = await TerminalService.Start({ projectId, kind: 'shell', ...opts } as SessionSpec)
    set((s) => {
      const tabs = s.tabs[projectId] ?? []
      const activeId = s.activeTab[projectId]
      if (split && activeId) {
        return {
          tabs: {
            ...s.tabs,
            [projectId]: tabs.map((t) => (t.id === activeId ? { ...t, dir: split, panes: [...t.panes, sess.id] } : t)),
          },
          sessions: s.sessions.some((x) => x.id === sess.id) ? s.sessions : [...s.sessions, sess],
          view: 'terminals',
        }
      }
      const tab = { id: newTabId(), panes: [sess.id], dir: 'row' as const }
      return {
        tabs: { ...s.tabs, [projectId]: [...tabs, tab] },
        activeTab: { ...s.activeTab, [projectId]: tab.id },
        sessions: s.sessions.some((x) => x.id === sess.id) ? s.sessions : [...s.sessions, sess],
        view: 'terminals',
      }
    })
    return sess
  } catch (e) {
    toast('error', 'Could not start terminal', errMsg(e))
  }
}

/** Runs a one-off command in the console drawer. */
const starting = new Set<string>()
export async function runInConsole(projectId: string, argv: string[], title?: string, cwd?: string, command?: string) {
  const name = title ?? argv.join(' ')
  // A second click on the same button must not start the command twice (and
  // later report the same failure twice): show the run already in progress.
  const key = projectId + '\0' + name
  const running = get().sessions.find((s) => s.projectId === projectId && s.kind === 'run' && s.title === name && s.running)
  if (running || starting.has(key)) {
    if (running) showConsole(true, running.id)
    return running
  }
  starting.add(key)
  try {
    const sess = await TerminalService.Start({ projectId, kind: 'run', argv, command: command ?? '', title: name, cwd: cwd ?? '' } as SessionSpec)
    set((s) => ({
      sessions: s.sessions.some((x) => x.id === sess.id) ? s.sessions : [...s.sessions, sess],
      drawer: { ...s.drawer, open: true, active: sess.id },
    }))
    return sess
  } catch (e) {
    toast('error', 'Could not run command', errMsg(e))
  } finally {
    starting.delete(key)
  }
}

/** Runs Composer, installing Larvyn's own copy first when there is none. */
export async function composerRun(projectId: string, args: string[]) {
  if (!(await SystemService.HasComposer())) {
    toast('info', 'Installing Composer first', 'Run the command again once it finishes.')
    const sess = await SystemService.InstallComposer().catch(fail('Could not install Composer'))
    if (sess) set((s) => ({ sessions: s.sessions.some((x) => x.id === sess.id) ? s.sessions : [...s.sessions, sess], drawer: { ...s.drawer, open: true, active: sess.id } }))
    return
  }
  return runInConsole(projectId, ['composer', ...args], 'composer ' + args.join(' '))
}

export function artisanRun(projectId: string, args: string[]) {
  const php = get().settings?.phpBinary || 'php'
  return runInConsole(projectId, [php, 'artisan', ...args], 'artisan ' + args.join(' '))
}

export function showConsole(open?: boolean, active?: string) {
  set((s) => ({ drawer: { ...s.drawer, open: open ?? !s.drawer.open, active: active ?? s.drawer.active } }))
}

export function setDrawerHeight(h: number) {
  set((s) => ({ drawer: { ...s.drawer, height: h } }))
  save('drawerHeight', h)
}

export async function toggleService(projectId: string, key: string, running: boolean) {
  try {
    if (running) await StackService.Stop(projectId, key)
    else await StackService.Start(projectId, key)
  } catch (e) {
    toast('error', `Could not ${running ? 'stop' : 'start'} ${key}`, errMsg(e))
  }
}

export function agentStatus(s: SessionInfo, now = Date.now()): 'working' | 'idle' | 'attention' | 'exited' {
  if (!s.running) return 'exited'
  if (s.bell) return 'attention'
  return now - s.lastActivity < 2500 ? 'working' : 'idle'
}
