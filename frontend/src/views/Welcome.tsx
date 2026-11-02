import { useEffect, useState } from 'react'
import { AlertTriangle, Bot, Database, Download, FlaskConical, FolderOpen, Plus, RefreshCw, ScrollText, Server, SquareTerminal, Waypoints, Zap } from 'lucide-react'
import { ProjectService, SystemService, type Tool } from '../../bindings/larvyn'
import { addProject, showConsole, toast, updateLaravelInstaller, useStore } from '../lib/store'
import { cx, errMsg } from '../lib/util'
import { Logo, Modal } from '../components/ui'

const FEATURES = [
  { icon: Server, title: 'One-click dev stack', text: 'artisan serve, Vite, queues, scheduler, Reverb and Pail with live output.' },
  { icon: Bot, title: 'Parallel agents', text: 'Claude Code, Codex and more side by side, each in its own git worktree.' },
  { icon: SquareTerminal, title: 'Real terminals', text: 'Tabs and splits that keep running while you switch projects.' },
  { icon: Database, title: 'Database browser', text: 'Tables, structure, SQL and migrations through your app’s own connection.' },
  { icon: FlaskConical, title: 'Tinker playground', text: 'Run code in the booted app with dumps, timing and query log.' },
  { icon: ScrollText, title: 'Live logs', text: 'Parsed exceptions, stack frames that open in your editor, one-click agent fixes.' },
  { icon: Waypoints, title: 'Routes & models', text: 'Every route, controller and relationship, searchable.' },
  { icon: Zap, title: 'Artisan studio', text: 'Forms for every command, generators for every make:*.' },
]

export function Welcome() {
  return (
    <div className="welcome">
      <div className="glow" />
      <div className="welcome-inner">
        <div className="logo-xl">
          <Logo />
        </div>
        <h1>
          The Laravel <span>workbench</span>
          <br />
          for people who ship.
        </h1>
        <p className="lead">
          Larvyn puts your Laravel apps, their services, terminals, data and AI agents in one fast window. Open a project to begin, or scaffold a fresh one.
        </p>
        <div className="row">
          <button className="btn primary lg" onClick={addProject}>
            <FolderOpen /> Open a Laravel project
          </button>
          <button className="btn lg" onClick={() => useStore.setState({ newProjectOpen: true })}>
            <Plus /> New Laravel app
          </button>
        </div>
        <div className="feature-grid" style={{ gridTemplateColumns: 'repeat(4, 1fr)' }}>
          {FEATURES.map((f) => (
            <div key={f.title} className="feature">
              <f.icon />
              <b>{f.title}</b>
              <span>{f.text}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

const STARTERS = [
  { id: 'none', label: 'Blank', text: 'Blade + Vite' },
  { id: 'react', label: 'React', text: 'Inertia + React + TS' },
  { id: 'vue', label: 'Vue', text: 'Inertia + Vue + TS' },
  { id: 'livewire', label: 'Livewire', text: 'Livewire + Flux' },
]

export function NewProject() {
  const open = useStore((s) => s.newProjectOpen)
  const [parent, setParent] = useState('')
  const [name, setName] = useState('')
  const [starter, setStarter] = useState('none')
  const [testing, setTesting] = useState('pest')
  const [db, setDb] = useState('sqlite')
  const [createSqlite, setCreateSqlite] = useState(true)
  const [busy, setBusy] = useState(false)
  const [installer, setInstaller] = useState<Tool | null>(null)
  const checkInstaller = () => SystemService.LaravelInstaller().then(setInstaller).catch(() => setInstaller(null))
  useEffect(() => {
    if (open) checkInstaller()
  }, [open])
  if (!open) return null
  const close = () => useStore.setState({ newProjectOpen: false })
  const create = async () => {
    setBusy(true)
    try {
      const r: any = await SystemService.CreateProject({ parent, name, starter, testing, db, createSqlite: db === 'sqlite' && createSqlite })
      toast('info', 'Creating your app…', r.path)
      showConsole(true, r.session.id)
      close()
    } catch (e) {
      toast('error', 'Could not create project', errMsg(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Modal
      title="New Laravel application"
      subtitle="Uses the Laravel installer when available, Composer otherwise."
      onClose={close}
      footer={
        <>
          <button className="btn ghost" onClick={close}>
            Cancel
          </button>
          <button className="btn primary" disabled={!parent || !name.trim() || busy} onClick={create}>
            {busy ? <div className="spinner" /> : <Plus />} Create
          </button>
        </>
      }
    >
      {installer?.outdated && (
        <div className="warn-box" style={{ marginBottom: 14 }}>
          <AlertTriangle />
          <div className="grow">
            <b>Your Laravel installer is out of date ({installer.version})</b>
            <div>It writes a broken APP_URL in new Laravel 13 apps, so Larvyn creates this one with Composer instead. Update it to use the installer again.</div>
            <div className="row" style={{ marginTop: 8 }}>
              <button className="btn sm primary" onClick={updateLaravelInstaller}>
                <Download /> Update installer
              </button>
              <button className="btn sm ghost" onClick={checkInstaller}>
                <RefreshCw /> Check again
              </button>
            </div>
          </div>
        </div>
      )}
      <div className="field">
        <label>Name</label>
        <input className="input" autoFocus placeholder="acme-crm" value={name} onChange={(e) => setName(e.target.value)} />
      </div>
      <div className="field">
        <label>Location</label>
        <div className="row">
          <input className="input mono grow" value={parent} placeholder="Choose a parent folder" readOnly />
          <button className="btn" onClick={() => ProjectService.PickFolder('Where should the project live?').then((p) => p && setParent(p))}>
            <FolderOpen /> Browse
          </button>
        </div>
        {parent && name && <span className="hint mono">{parent.replace(/[\\/]$/, '')}/{name.toLowerCase().replace(/[^a-z0-9]+/g, '-')}</span>}
      </div>
      <div className="field">
        <label>Starter kit</label>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 8 }}>
          {STARTERS.map((s) => (
            <button key={s.id} className={cx('template', starter === s.id && 'active')} style={starter === s.id ? { borderColor: 'var(--accent-line)', background: 'var(--accent-soft)' } : undefined} onClick={() => setStarter(s.id)}>
              <b>{s.label}</b>
              <span>{s.text}</span>
            </button>
          ))}
        </div>
      </div>
      <div className="row" style={{ gap: 16 }}>
        <div className="field grow">
          <label>Testing</label>
          <div className="tabs" style={{ alignSelf: 'flex-start' }}>
            {['pest', 'phpunit'].map((t) => (
              <button key={t} className={cx('tab', testing === t && 'active')} onClick={() => setTesting(t)}>
                {t === 'pest' ? 'Pest' : 'PHPUnit'}
              </button>
            ))}
          </div>
        </div>
        <div className="field grow">
          <label>Database</label>
          <div className="tabs" style={{ alignSelf: 'flex-start' }}>
            {['sqlite', 'mysql', 'pgsql'].map((t) => (
              <button key={t} className={cx('tab', db === t && 'active')} onClick={() => setDb(t)}>
                {t}
              </button>
            ))}
          </div>
        </div>
      </div>
      {db === 'sqlite' && (
        <label className="check">
          <input type="checkbox" checked={createSqlite} onChange={(e) => setCreateSqlite(e.target.checked)} /> Create the SQLite database file (database/database.sqlite) and run migrations
        </label>
      )}
    </Modal>
  )
}
