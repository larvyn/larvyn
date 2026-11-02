import { lazy, Suspense, useEffect } from 'react'
import { SystemService } from '../bindings/larvyn/internal/system'
import { boot, newTerminal, openPalette, selectProject, setView, showConsole, useStore } from './lib/store'
import { Drawer, NAV_ITEMS, Rail, Sidebar, StatusBar, TitleBar } from './components/Shell'
import { Palette } from './components/Palette'
import { ConfirmHost, PromptHost, Toasts } from './components/ui'
import { ErrorBoundary } from './components/ErrorBoundary'
import { UpdateNotice } from './components/UpdateNotice'
import { startUpdateChecks } from './lib/update'
import { Overview } from './views/Overview'
import { Agents } from './views/Agents'
import { Terminals } from './views/Terminals'
import { Artisan } from './views/Artisan'
import { Routes } from './views/Routes'
import { Models } from './views/Models'
import { Database } from './views/Database'
import { Tinker } from './views/Tinker'
import { Tests } from './views/Tests'
import { Queues } from './views/Queues'
import { Logs, useLogWatcher } from './views/Logs'
import { Git } from './views/Git'
import { Env } from './views/Env'
import { Deps } from './views/Deps'
import { Settings } from './views/Settings'
import { NewProject, Welcome } from './views/Welcome'
import { Loading } from './components/ui'

// The editor pulls in CodeMirror: load it only when Files is opened.
const LazyFiles = lazy(() => import('./views/Files').then((m) => ({ default: m.Files })))
const Files = () => (
  <Suspense fallback={<Loading />}>
    <LazyFiles />
  </Suspense>
)

const VIEWS = {
  overview: Overview,
  agents: Agents,
  terminals: Terminals,
  artisan: Artisan,
  routes: Routes,
  models: Models,
  database: Database,
  tinker: Tinker,
  tests: Tests,
  queues: Queues,
  logs: Logs,
  git: Git,
  env: Env,
  files: Files,
  deps: Deps,
  settings: Settings,
}

export default function App() {
  const ready = useStore((s) => s.ready)
  const view = useStore((s) => s.view)
  const activeId = useStore((s) => s.activeId)
  useLogWatcher()
  useEffect(() => (ready ? startUpdateChecks() : undefined), [ready])

  useEffect(() => {
    boot().catch((e) => SystemService.Log('error', 'boot failed: ' + (e?.stack ?? e?.message ?? e)))
    const onKey = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey
      const k = e.key.toLowerCase()
      const s = useStore.getState()
      if (mod && !e.shiftKey && k === 'k') {
        e.preventDefault()
        if (s.paletteOpen) useStore.setState({ paletteOpen: false })
        else openPalette()
      } else if (mod && e.shiftKey && k === 'p') {
        e.preventDefault()
        openPalette()
      } else if (mod && k === 'j') {
        e.preventDefault()
        showConsole()
      } else if (mod && k === 'b') {
        e.preventDefault()
        useStore.setState({ sidebar: !s.sidebar })
      } else if (mod && k === ',') {
        e.preventDefault()
        setView('settings')
      } else if (mod && e.shiftKey && k === 't' && s.activeId) {
        e.preventDefault()
        newTerminal(s.activeId)
      } else if (mod && e.shiftKey && (e.key === ']' || e.key === '[' || e.key === '}' || e.key === '{')) {
        e.preventDefault()
        const i = s.projects.findIndex((p) => p.id === s.activeId)
        const dir = e.key === ']' || e.key === '}' ? 1 : -1
        const next = s.projects[(i + dir + s.projects.length) % s.projects.length]
        if (next) selectProject(next.id)
      } else if (e.altKey && /^[1-9]$/.test(e.key)) {
        const item = NAV_ITEMS.find((n) => n.hint === e.key)
        if (item) {
          e.preventDefault()
          setView(item.view)
        }
      }
    }
    window.addEventListener('keydown', onKey, true)
    // The webview's own context menu (Reload, Inspect) is not useful here.
    const onMenu = (e: MouseEvent) => {
      const t = e.target as HTMLElement
      if (!t.closest('input, textarea, .selectable, .xterm')) e.preventDefault()
    }
    window.addEventListener('contextmenu', onMenu)
    return () => {
      window.removeEventListener('keydown', onKey, true)
      window.removeEventListener('contextmenu', onMenu)
    }
  }, [])

  const View = VIEWS[view] ?? Overview
  return (
    <div className="app">
      <TitleBar />
      <div className="body">
        <Rail />
        {ready && activeId ? <Sidebar /> : <div />}
        <main className="main">
          <ErrorBoundary resetKey={activeId + view}>
            {!ready ? <div className="view" /> : activeId || view === 'settings' ? <View key={view === 'terminals' ? 'terms' : activeId + view} /> : <Welcome />}
          </ErrorBoundary>
          {activeId && <Drawer />}
        </main>
      </div>
      <StatusBar />
      <Palette />
      <NewProject />
      <ConfirmHost />
      <PromptHost />
      <Toasts />
      <UpdateNotice />
    </div>
  )
}
