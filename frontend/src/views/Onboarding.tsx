import { useEffect, useState, type ReactNode } from 'react'
import { ArrowLeft, ArrowRight, Bot, Check, Code2, Command, Copy, Database, ExternalLink, FolderOpen, Hand, History, Play, Plus, RefreshCw, Rocket, Server, ShieldCheck, SquareTerminal, X, Zap } from 'lucide-react'
import { SystemService, type Tool } from '../../bindings/larvyn/internal/system'
import { AgentService, type AgentKind } from '../../bindings/larvyn/internal/agents'
import { type Settings } from '../../bindings/larvyn/internal/store'
import { addProject, toast, useStore } from '../lib/store'
import { EDITORS, copy, cx, errMsg, isMac, mod } from '../lib/util'
import { Brand } from '../components/Brand'
import { Logo } from '../components/ui'

const STEPS = [
  { id: 'welcome', title: 'Welcome', text: 'What Larvyn does' },
  { id: 'toolchain', title: 'Toolchain', text: 'PHP, Composer, Node, Git' },
  { id: 'workflow', title: 'Your workflow', text: 'Editor and AI agents' },
  { id: 'project', title: 'First project', text: 'Open or create an app' },
] as const

// Tools a Laravel app can't run without, and ones that only make life easier.
// Composer is not required: Larvyn installs its own on first use.
const REQUIRED = ['php', 'node', 'git']
const RECOMMENDED = ['composer', 'laravel']

const HIGHLIGHTS = [
  { icon: Server, title: 'Your dev stack in one click', text: 'artisan serve, Vite, queues, scheduler and Reverb, with their output side by side.' },
  { icon: Bot, title: 'AI agents in parallel', text: 'Give one task to several agents, each in its own git worktree, and merge the best result.' },
  { icon: Database, title: 'Inside your app', text: 'Database, models, routes, logs, tests and Tinker through the app’s own config.' },
]

/** The first-run setup: checks the toolchain, picks the editor and the
 * agent, and ends on the first project. Finishing or skipping saves
 * settings.onboarded so it shows once; Settings can bring it back. */
export function Onboarding() {
  const saved = useStore((s) => s.settings)
  const [step, setStep] = useState(0)
  const [v, setV] = useState<Settings | null>(saved)
  const [tools, setTools] = useState<Tool[] | null>(null)
  const [agents, setAgents] = useState<AgentKind[] | null>(null)
  const [checking, setChecking] = useState(false)

  const check = async () => {
    setChecking(true)
    try {
      const [t, a] = await Promise.all([SystemService.Tools(), AgentService.Kinds()])
      setTools(t ?? [])
      setAgents(a ?? [])
    } catch (e) {
      toast('error', 'Could not check the toolchain', errMsg(e))
    } finally {
      setChecking(false)
    }
  }
  // Start checking on the welcome step so the results are there by the next one.
  useEffect(() => {
    check()
  }, [])
  // Default to an agent that is installed when the saved one is not.
  useEffect(() => {
    if (!agents || !v) return
    const current = agents.find((a) => a.id === v.defaultAgent)
    const installed = agents.find((a) => a.installed)
    if (!current?.installed && installed) setV({ ...v, defaultAgent: installed.id })
  }, [agents])

  if (!v) return null
  const set = (patch: Partial<Settings>) => setV({ ...v, ...patch })

  /** Saves the choices, closes the setup and runs `then` (open a project…). */
  const finish = async (then?: () => void) => {
    const next = { ...v, onboarded: true }
    try {
      await SystemService.SaveSettings(next)
    } catch (e) {
      toast('error', 'Could not save your settings', errMsg(e))
    }
    useStore.setState({ settings: next, onboarding: false })
    then?.()
  }
  const skip = () => finish()

  const last = step === STEPS.length - 1
  const next = () => setStep((s) => Math.min(s + 1, STEPS.length - 1))
  const back = () => setStep((s) => Math.max(s - 1, 0))

  return (
    <div className="onboard">
      <div className="glow" />
      <div className="onboard-card">
        <aside className="onboard-side">
          <div className="onboard-brand">
            <span className="onboard-logo">
              <Logo />
            </span>
            <b>Larvyn</b>
          </div>
          <ol className="onboard-steps">
            {STEPS.map((s, i) => (
              <li key={s.id}>
                <button className={cx('onboard-step', i === step && 'active', i < step && 'done')} onClick={() => setStep(i)}>
                  <span className="n">{i < step ? <Check /> : i + 1}</span>
                  <span className="grow">
                    <b>{s.title}</b>
                    <span>{s.text}</span>
                  </span>
                </button>
              </li>
            ))}
          </ol>
          <p className="onboard-note">You can change all of this later in Settings.</p>
        </aside>

        <section className="onboard-main">
          <div className="onboard-body" key={step}>
            {step === 0 && <WelcomeStep />}
            {step === 1 && <ToolchainStep tools={tools} checking={checking} onCheck={check} />}
            {step === 2 && <WorkflowStep v={v} set={set} agents={agents} />}
            {step === 3 && <ProjectStep onOpen={() => finish(addProject)} onCreate={() => finish(() => useStore.setState({ newProjectOpen: true }))} />}
          </div>
          <footer className="onboard-foot">
            {step > 0 ? (
              <button className="btn ghost" onClick={back}>
                <ArrowLeft /> Back
              </button>
            ) : (
              <button className="btn ghost" onClick={skip}>
                Skip setup
              </button>
            )}
            <span className="onboard-dots">
              {STEPS.map((s, i) => (
                <i key={s.id} className={cx(i === step && 'on')} />
              ))}
            </span>
            {last ? (
              <button className="btn" onClick={() => finish()}>
                I’ll do it later
              </button>
            ) : (
              <button className="btn primary" onClick={next}>
                {step === 0 ? 'Get started' : 'Continue'} <ArrowRight />
              </button>
            )}
          </footer>
        </section>
      </div>
    </div>
  )
}

function Head({ eyebrow, title, children }: { eyebrow: string; title: ReactNode; children?: ReactNode }) {
  return (
    <header className="onboard-head">
      <span className="eyebrow">{eyebrow}</span>
      <h1>{title}</h1>
      {children && <p>{children}</p>}
    </header>
  )
}

function WelcomeStep() {
  return (
    <>
      <Head
        eyebrow="Welcome"
        title={
          <>
            The Laravel <span className="grad">workbench</span>
          </>
        }
      >
        Your apps, their services, terminals, data and AI agents in one window. Two minutes of setup and you’re ready to ship.
      </Head>
      <div className="onboard-highlights">
        {HIGHLIGHTS.map((h) => (
          <div key={h.title} className="onboard-highlight">
            <span className="ic">
              <h.icon />
            </span>
            <div>
              <b>{h.title}</b>
              <span>{h.text}</span>
            </div>
          </div>
        ))}
      </div>
    </>
  )
}

function ToolchainStep({ tools, checking, onCheck }: { tools: Tool[] | null; checking: boolean; onCheck: () => void }) {
  const platform = useStore((s) => s.platform)
  const byId = new Map((tools ?? []).map((t) => [t.id, t]))
  // One local server is enough: the one installed, else the one this OS has
  // (Herd has no Linux version; Flock runs everywhere).
  const server = byId.get('flock')?.path ? 'flock' : byId.get('herd')?.path ? 'herd' : platform === 'linux' ? 'flock' : 'herd'
  const missing = REQUIRED.filter((id) => !byId.get(id)?.path)
  const row = (id: string, required: boolean) => {
    const t = byId.get(id)
    if (!t) return null
    const ok = !!t.path && !t.outdated
    const note = t.outdated
      ? t.outdated
      : t.path
        ? t.path
        : id === 'composer'
          ? 'Not on PATH: Larvyn installs its own Composer the first time it needs it.'
          : t.hint || 'Not found'
    const url = !t.path && t.hint?.includes('.') && !t.hint.includes(' ') ? 'https://' + t.hint : ''
    const cmd = !t.path && t.hint?.includes(' ') ? t.hint : t.outdated && id === 'laravel' ? 'composer global update laravel/installer' : ''
    return (
      <div key={id} className="check-row">
        <div className={cx('ic', ok ? 'ok' : required ? 'bad' : 'wait')}>{ok ? <Check /> : <X />}</div>
        <Brand id={id} size={18} fallback={<Zap style={{ width: 18, height: 18, color: 'var(--text-3)' }} />} />
        <div className="t">
          <b>
            {t.name} {t.version && <span className="badge mono">{t.version}</span>} {!required && <span className="faint">optional</span>}
          </b>
          <span className={cx(!t.path && 'mono', t.outdated && 'warn-text')}>{note}</span>
        </div>
        {url && (
          <button className="btn sm ghost" onClick={() => SystemService.OpenURL(url)}>
            <ExternalLink /> Get it
          </button>
        )}
        {cmd && <CopyCommand text={cmd} />}
      </div>
    )
  }
  return (
    <>
      <Head eyebrow="Step 1 of 3" title="Check your toolchain">
        Larvyn runs your apps with the tools already on this machine. Here’s what it found.
      </Head>
      {!tools ? (
        <div className="card">
          {[1, 2, 3, 4, 5].map((i) => (
            <div key={i} className="skeleton" style={{ height: 22, margin: 14 }} />
          ))}
        </div>
      ) : (
        <>
          <div className={cx('onboard-status', missing.length ? 'warn' : 'ok')}>
            {missing.length ? <X /> : <ShieldCheck />}
            <div className="grow">
              <b>{missing.length ? `${missing.length} required ${missing.length === 1 ? 'tool is' : 'tools are'} missing` : 'Everything you need is installed'}</b>
              <span>{missing.length ? 'You can continue and install them later, but some views won’t work until you do.' : 'Laravel apps can boot, build and run from Larvyn.'}</span>
            </div>
            <button className="btn sm ghost" disabled={checking} onClick={onCheck}>
              {checking ? <div className="spinner" /> : <RefreshCw />} Check again
            </button>
          </div>
          <div className="card onboard-list">
            <div className="onboard-group">Required</div>
            {REQUIRED.map((id) => row(id, true))}
            <div className="onboard-group">Recommended</div>
            {[...RECOMMENDED, server].map((id) => row(id, false))}
          </div>
        </>
      )}
    </>
  )
}

function WorkflowStep({ v, set, agents }: { v: Settings; set: (p: Partial<Settings>) => void; agents: AgentKind[] | null }) {
  const sorted = [...(agents ?? [])].sort((a, b) => Number(b.installed) - Number(a.installed))
  const yolo = v.agentPermissions !== 'manual'
  const restore = v.restore === 'all' || v.restore === 'off' ? v.restore : 'terminals'
  return (
    <>
      <Head eyebrow="Step 2 of 3" title="Make it yours">
        Where files open, which agent takes your prompts, how much it may do on its own and what comes back when you reopen.
      </Head>

      <div className="onboard-section">
        <h3>
          <Code2 /> Editor
        </h3>
        <div className="onboard-choices cols-4">
          {EDITORS.map((e) => (
            <button key={e.id} className={cx('onboard-choice', v.editor === e.id && 'active')} onClick={() => set({ editor: e.id })}>
              <b>{e.label}</b>
            </button>
          ))}
        </div>
        {v.editor === 'custom' && (
          <input className="input mono" placeholder="myeditor --goto {file}:{line}" value={v.editorCommand} onChange={(e) => set({ editorCommand: e.target.value })} />
        )}
      </div>

      <div className="onboard-section">
        <h3>
          <Bot /> Default agent
        </h3>
        {!agents ? (
          <div className="skeleton" style={{ height: 64 }} />
        ) : (
          <div className="onboard-choices cols-3">
            {sorted.map((a) => (
              <button key={a.id} className={cx('onboard-choice', v.defaultAgent === a.id && 'active', !a.installed && 'muted')} onClick={() => set({ defaultAgent: a.id })} title={a.installed ? a.path : a.install}>
                <b>
                  <Brand id={a.id} size={15} fallback={<Bot />} /> {a.name}
                </b>
                <span>{a.installed ? 'Installed' : 'Not installed'}</span>
              </button>
            ))}
          </div>
        )}
        {(() => {
          const a = agents?.find((x) => x.id === v.defaultAgent)
          return a && !a.installed && a.install ? (
            <div className="onboard-hint">
              {a.install.startsWith('see ') ? (
                <span>
                  To install {a.name}, {a.install}.
                </span>
              ) : (
                <>
                  <span>Install {a.name} with</span> <code className="mono">{a.install}</code> <CopyCommand text={a.install} />
                </>
              )}
            </div>
          ) : null
        })()}
      </div>

      <div className="onboard-section">
        <h3>
          <ShieldCheck /> Agent permissions
        </h3>
        <div className="onboard-choices cols-2">
          <button className={cx('onboard-choice tall', !yolo && 'active')} onClick={() => set({ agentPermissions: 'manual' })}>
            <b>
              <Hand /> Ask before acting
            </b>
            <span>The agent asks you to approve commands and edits. Safer while you get to know it.</span>
          </button>
          <button className={cx('onboard-choice tall', yolo && 'active')} onClick={() => set({ agentPermissions: 'yolo' })}>
            <b>
              <Rocket /> Work on its own
            </b>
            <span>Skips approval prompts. Agents work in their own git worktree, so you review the diff before merging.</span>
          </button>
        </div>
      </div>

      <div className="onboard-section">
        <h3>
          <History /> When Larvyn opens
        </h3>
        <div className="onboard-choices cols-3">
          <button className={cx('onboard-choice tall', restore === 'terminals' && 'active')} onClick={() => set({ restore: 'terminals' })}>
            <b>
              <SquareTerminal /> Tabs, nothing running
            </b>
            <span>Tabs come back with their output and shells start. You relaunch services and agents.</span>
          </button>
          <button className={cx('onboard-choice tall', restore === 'all' && 'active')} onClick={() => set({ restore: 'all' })}>
            <b>
              <Play /> Pick up where I left
            </b>
            <span>Services that ran start again and agents resume their conversation.</span>
          </button>
          <button className={cx('onboard-choice tall', restore === 'off' && 'active')} onClick={() => set({ restore: 'off' })}>
            <b>
              <X /> Start clean
            </b>
            <span>Nothing comes back from the previous run.</span>
          </button>
        </div>
      </div>
    </>
  )
}

function ProjectStep({ onOpen, onCreate }: { onOpen: () => void; onCreate: () => void }) {
  const key = (k: string) => (k === 'Ctrl' ? mod : k === 'Alt' && isMac ? '⌥' : k === 'Shift' && isMac ? '⇧' : k)
  const tips = [
    { icon: Command, text: 'Command palette: views, services, artisan and shell lines', keys: 'Ctrl K' },
    { icon: SquareTerminal, text: 'New terminal in the project', keys: 'Ctrl Shift T' },
    { icon: Zap, text: 'Jump between views', keys: 'Alt 1 … 9' },
  ]
  return (
    <>
      <Head eyebrow="Step 3 of 3" title="Bring in your first app">
        Open a Laravel project you already have, or start a new one with the starter kit of your choice.
      </Head>
      <div className="onboard-choices cols-2">
        <button className="onboard-cta" onClick={onOpen}>
          <span className="ic">
            <FolderOpen />
          </span>
          <b>Open a Laravel project</b>
          <span>Pick the folder with its artisan file. Larvyn runs a Doctor check on it.</span>
        </button>
        <button className="onboard-cta" onClick={onCreate}>
          <span className="ic">
            <Plus />
          </span>
          <b>Create a new app</b>
          <span>Blank, React, Vue or Livewire, with Pest or PHPUnit, on SQLite, MySQL or Postgres.</span>
        </button>
      </div>
      <div className="onboard-section">
        <h3>Good to know</h3>
        <div className="card onboard-list">
          {tips.map((t) => (
            <div key={t.keys} className="check-row">
              <t.icon style={{ width: 15, height: 15, color: 'var(--text-3)' }} />
              <div className="t">
                <span style={{ color: 'var(--text-2)', fontSize: 12.5 }}>{t.text}</span>
              </div>
              <span>
                {t.keys.split(' ').map((k, i) => (
                  <kbd key={i} style={{ marginLeft: 4 }}>
                    {key(k)}
                  </kbd>
                ))}
              </span>
            </div>
          ))}
        </div>
      </div>
    </>
  )
}

function CopyCommand({ text }: { text: string }) {
  const [done, setDone] = useState(false)
  return (
    <button
      className="btn sm ghost"
      title={text}
      onClick={() =>
        copy(text)
          .then(() => {
            setDone(true)
            setTimeout(() => setDone(false), 1500)
          })
          .catch(() => toast('error', 'Could not copy'))
      }
    >
      {done ? <Check /> : <Copy />} {done ? 'Copied' : 'Copy command'}
    </button>
  )
}
