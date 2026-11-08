import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Bot,
  Check,
  Columns2,
  Copy,
  RefreshCw,
  Rows3,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Download,
  FileDiff,
  GitBranch,
  GitMerge,
  LayoutGrid,
  Maximize2,
  Rocket,
  RotateCcw,
  Send,
  ShieldCheck,
  Sparkles,
  Square,
  TestTube2,
  Trash2,
  Wand2,
  Bug,
  Gauge,
  Layers,
  ArrowUpCircle,
  Code2,
  Globe,
} from 'lucide-react'
import { SystemService } from '../../bindings/larvyn/internal/system'
import { type SessionInfo } from '../../bindings/larvyn/internal/terminal'
import { AgentService, type LaunchRequest, type AgentKind } from '../../bindings/larvyn/internal/agents'
import { TerminalService } from '../../bindings/larvyn/internal/terminal'
import { GitService } from '../../bindings/larvyn/internal/git'
import { agentStatus, composerRun, fail, relaunch, runInConsole, toast, useActive, useStore } from '../lib/store'
import { activity } from '../lib/terms'
import { copy, cx, duration, errMsg, initials, load, mod, save } from '../lib/util'
import { useRunShortcut } from '../lib/shortcuts'
import { Empty, SidePanel, TermPane, confirmDialog, useNow } from '../components/ui'
import { FileDiff as FileDiffView, firstLine, parseDiff } from '../components/DiffView'
import { Brand, brandUrl } from '../components/Brand'

const TEMPLATES = [
  {
    icon: Layers,
    title: 'CRUD resource',
    text: 'Build a complete CRUD for a {Model} resource: migration, model with casts and relationships, factory, seeder, Form Requests for validation, a policy, a resource controller, routes and Pest feature tests. Follow the conventions already used in this codebase.',
  },
  {
    icon: TestTube2,
    title: 'Write tests',
    text: 'Find the untested controllers, actions and jobs in app/ and write Pest feature and unit tests for them. Use factories, cover the happy path and the validation failures, and run php artisan test until everything passes.',
  },
  {
    icon: Bug,
    title: 'Fix failing tests',
    text: 'Run php artisan test, investigate every failure, and fix the root cause in the application code (not the tests, unless a test is clearly wrong). Re-run until green and summarise what was broken.',
  },
  {
    icon: Gauge,
    title: 'Find N+1 queries',
    text: 'Audit controllers, Livewire components and API resources for N+1 query problems and missing indexes. Add eager loading and migrations for indexes where needed, and explain each change.',
  },
  {
    icon: ShieldCheck,
    title: 'Security review',
    text: 'Review this Laravel app for security problems: mass assignment, missing authorization (policies/gates), unvalidated input, raw SQL, XSS in Blade ({!! !!}), CSRF exemptions, exposed secrets and insecure config. Fix what you can and list the rest.',
  },
  {
    icon: ArrowUpCircle,
    title: 'Upgrade Laravel',
    text: 'Upgrade this application to the latest Laravel major version following the official upgrade guide: update composer.json constraints, apply the breaking changes, migrate to the slim application structure if appropriate, and make sure the test suite passes.',
  },
  {
    icon: Wand2,
    title: 'Refactor to actions',
    text: 'Move business logic out of fat controllers into single-purpose Action classes in app/Actions, keep controllers thin, add type hints and return types, and keep behaviour identical (tests must still pass).',
  },
  {
    icon: Code2,
    title: 'API endpoints',
    text: 'Add a versioned JSON API (routes/api.php, /api/v1) for the main models with API Resources, Form Request validation, Sanctum authentication, pagination and filtering, plus Pest tests for every endpoint.',
  },
]

type Status = ReturnType<typeof agentStatus>
const STATUS_LABEL: Record<Status, string> = { working: 'Working', idle: 'Waiting for you', attention: 'Needs attention', exited: 'Finished' }

export function Agents() {
  const { id, project, info } = useActive()
  const sessions = useStore((s) => s.sessions)
  const settings = useStore((s) => s.settings)
  const focusPrompt = useStore((s) => s.focus.agentPrompt)
  const [kinds, setKinds] = useState<AgentKind[]>([])
  const [picked, setPicked] = useState<string[]>(() => load('agents:picked', [settings?.defaultAgent || 'claude']))
  const [prompt, setPrompt] = useState('')
  const [isolate, setIsolate] = useState<boolean>(() => load('agents:isolate', true))
  const [deps, setDeps] = useState<boolean>(() => load('agents:deps', false))
  const [busy, setBusy] = useState(false)
  const [focused, setFocused] = useState<string | null>(null)
  const [diffFor, setDiffFor] = useState<string | null>(null)
  const taRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    AgentService.Kinds().then((k) => setKinds(k ?? []))
  }, [])
  useEffect(() => {
    if (focusPrompt) {
      setPrompt(focusPrompt)
      useStore.setState({ focus: {} })
      taRef.current?.focus()
    }
  }, [focusPrompt])
  useEffect(() => save('agents:picked', picked), [picked])
  useEffect(() => save('agents:isolate', isolate), [isolate])
  useEffect(() => save('agents:deps', deps), [deps])

  const agents = useMemo(() => sessions.filter((s) => s.projectId === id && s.kind === 'agent'), [sessions, id])
  const installed = kinds.filter((k) => k.installed)
  const multi = picked.length > 1

  // launch is declared below the early return; the shortcut is off without a project.
  useRunShortcut(() => !busy && installed.length > 0 && launch(), { enabled: !!project && !focused, within: taRef })

  if (!project) return null

  const launch = async () => {
    if (!picked.length) return toast('info', 'Pick at least one agent')
    setBusy(true)
    try {
      const req = { projectId: id!, prompt: prompt.trim(), worktree: isolate || multi, installDeps: deps } as LaunchRequest
      if (multi) {
        await AgentService.Fanout(req, picked)
        toast('success', `${picked.length} agents started`, 'Each one works on its own branch. Compare and merge the best result.')
      } else {
        await AgentService.Launch({ ...req, agent: picked[0] })
      }
      setPrompt('')
    } catch (e) {
      toast('error', 'Could not start agent', errMsg(e))
    } finally {
      setBusy(false)
    }
  }

  const shown = focused ? agents.filter((a) => a.id === focused) : agents

  const reviewing = agents.find((a) => a.id === diffFor && a.meta?.worktree)

  return (
    <div className="view flush with-side">
      <div className="view-main">
        <div className="toolbar">
          <h1>
            <Bot /> Agents
          </h1>
          <span className="faint" style={{ fontSize: 12 }}>
            Run coding agents side by side, each in its own git worktree. Compare, then merge the winner.
          </span>
          <span className="spacer" />
          {focused && (
            <button className="btn sm ghost" onClick={() => setFocused(null)}>
              <ChevronLeft /> All agents
            </button>
          )}
          {agents.some((a) => !a.running) && (
            <button className="btn sm ghost" onClick={() => agents.filter((a) => !a.running).forEach((a) => TerminalService.Close(a.id))}>
              <Trash2 /> Clear finished
            </button>
          )}
        </div>

        {!focused && (
          <div style={{ padding: '16px 16px 4px' }}>
            <div className="composer">
              <textarea
                ref={taRef}
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                placeholder={`What should the agent${multi ? 's' : ''} build in ${project.name}?  e.g. “Add a Post model with comments, CRUD, policies and Pest tests”`}
              />
              <div className="cbar">
                <div className="agents">
                {kinds.map((k) => {
                  const on = picked.includes(k.id)
                  return (
                    <button
                      key={k.id}
                      className={cx('agent-pick', on && 'on', !k.installed && 'off')}
                      title={k.installed ? k.path : `Not installed: ${k.install}`}
                      onClick={() => {
                        if (!k.installed) {
                          confirmDialog({ title: `Install ${k.name}?`, message: <span className="mono">{k.install}</span>, confirm: 'Install' }).then(
                            (ok) => {
                              if (ok && !k.install.startsWith('see')) runInConsole(id!, [], `install ${k.name}`, undefined, k.install)
                            },
                          )
                          return
                        }
                        setPicked((p) => (on ? p.filter((x) => x !== k.id) : [...p, k.id]))
                      }}
                    >
                      <span className="sq" style={{ background: brandUrl(k.id) && !(k.installed && on) ? 'var(--bg-3)' : k.color }}>
                        {k.installed && on ? <Check size={10} strokeWidth={3.5} /> : <Brand id={k.id} size={12} fallback={initials(k.name).slice(0, 1)} />}
                      </span>
                      {k.name}
                      {!k.installed && <Download size={11} />}
                    </button>
                  )
                })}
                </div>
                <label className="check" title="Each agent gets a branch larvyn/<task> in its own folder, with .env and SQLite copied">
                  <input type="checkbox" checked={isolate || multi} disabled={multi} onChange={(e) => setIsolate(e.target.checked)} />
                  <GitBranch size={13} /> Worktree
                </label>
                {(isolate || multi) && (
                  <label className="check" title="Run composer install and npm install in the new worktree">
                    <input type="checkbox" checked={deps} onChange={(e) => setDeps(e.target.checked)} /> Install deps
                  </label>
                )}
                <button className="btn primary" disabled={busy || !picked.length || installed.length === 0} onClick={launch}>
                  {busy ? <div className="spinner" style={{ borderTopColor: '#fff' }} /> : <Rocket />}
                  {multi ? `Launch ${picked.length} agents` : 'Launch'}
                  <kbd style={{ background: 'rgba(0,0,0,.2)', color: '#fff', borderColor: 'rgba(255,255,255,.25)' }}>{mod} ↵</kbd>
                </button>
              </div>
            </div>
            {agents.length === 0 && (
              <>
                <div className="section-title">
                  <Sparkles size={13} /> Laravel playbooks
                </div>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 1fr))', gap: 10 }}>
                  {TEMPLATES.map((t) => (
                    <button
                      key={t.title}
                      className="template"
                      onClick={() => {
                        setPrompt(t.text)
                        taRef.current?.focus()
                      }}
                    >
                      <b>
                        <t.icon /> {t.title}
                      </b>
                      <span>{t.text.slice(0, 96)}…</span>
                    </button>
                  ))}
                </div>
                {installed.length === 0 && kinds.length > 0 && (
                  <div className="error-box" style={{ marginTop: 16, background: 'var(--yellow-soft)', borderColor: 'rgba(245,197,66,.25)', color: 'var(--yellow)' }}>
                    No agent CLI found on PATH. Click one of the agents above to install it (for example Claude Code: npm i -g @anthropic-ai/claude-code).
                  </div>
                )}
                {info?.isLaravel && !info.packages?.some((p) => p.name === 'laravel/boost') && (
                  <div className="card" style={{ marginTop: 16 }}>
                    <div className="check-row">
                      <div className="ic wait">
                        <Sparkles />
                      </div>
                      <div className="t">
                        <b>Make your agents Laravel experts</b>
                        <span>Laravel Boost gives them version-specific docs, your schema, routes, logs and Tinker through MCP.</span>
                      </div>
                      <button className="btn sm" onClick={() => composerRun(id!, ['require', 'laravel/boost', '--dev'])}>
                        Install Boost
                      </button>
                    </div>
                  </div>
                )}
              </>
            )}
          </div>
        )}

        {agents.length > 0 && (
          <div className={cx('agent-grid', focused && 'focus')} style={{ flex: 1, overflow: 'auto', minHeight: 0 }}>
            {shown.map((a) => (
              <AgentCard
                key={a.id}
                s={a}
                focused={!!focused}
                onFocus={() => setFocused(focused ? null : a.id)}
                reviewing={diffFor === a.id}
                onDiff={() => setDiffFor(diffFor === a.id ? null : a.id)}
              />
            ))}
          </div>
        )}
      </div>
      {reviewing && <ReviewPanel key={reviewing.id} s={reviewing} onClose={() => setDiffFor(null)} />}
    </div>
  )
}

function AgentCard(props: { s: SessionInfo; focused: boolean; reviewing: boolean; onFocus: () => void; onDiff: () => void }) {
  const { s } = props
  // The clock ticks here, not in the view: only this card re-renders each
  // second, and only while its agent runs.
  const now = useNow(1000, s.running)
  const status: Status = agentStatus({ ...s, lastActivity: activity.get(s.id) ?? s.lastActivity } as SessionInfo, now)
  const { id } = useActive()
  const [msg, setMsg] = useState('')
  const color = s.meta?.color || '#888'
  const send = () => {
    if (!msg.trim()) return
    AgentService.Send(s.id, msg).catch(fail('Could not send'))
    setMsg('')
  }
  const merge = () => mergeAttempt(id!, s)
  const openSite = async () => {
    const url = await AgentService.WorktreeURL(s.meta!.worktree!).catch(() => '')
    if (url) SystemService.OpenURL(url)
    else toast('info', 'Flock is still setting this worktree up', 'It serves new worktrees within a few seconds. Try again shortly.')
  }
  const discard = () => discardAttempt(id!, s)
  return (
    <div className={cx('agent-card', status === 'working' && 'working', status === 'attention' && 'attention', props.reviewing && 'reviewing')} onMouseDown={() => s.bell && TerminalService.ClearBell(s.id)}>
      <div className="ahead">
        <div className="avatar" style={{ background: brandUrl(s.key) ? 'var(--bg-3)' : color }}>
          <Brand id={s.key} size={15} fallback={initials(s.meta?.agentName || s.key).slice(0, 1)} />
        </div>
        <div className="grow" style={{ minWidth: 0 }}>
          <div className="ttl ellipsis" title={s.meta?.prompt}>
            {s.title}
          </div>
          <div className="sub ellipsis">
            {s.meta?.agentName}
            {s.meta?.branch && (
              <>
                {' · '}
                <GitBranch size={10} style={{ verticalAlign: -1 }} /> {s.meta.branch}
              </>
            )}
            {' · '}
            {duration((s.running ? now : s.endedAt) - s.startedAt)}
          </div>
        </div>
        {props.reviewing && (
          <span className="badge review-tag" title="Its changes are open in the panel on the right">
            <FileDiff /> Reviewing
          </span>
        )}
        <span className={cx('badge', status === 'working' ? 'accent' : status === 'attention' ? 'yellow' : status === 'idle' ? '' : s.exitCode === 0 ? 'green' : 'red')}>
          <span className={cx('status-dot', status === 'working' ? 'busy' : status === 'attention' ? 'idle' : status === 'idle' ? 'on' : '')} style={{ width: 6, height: 6, boxShadow: 'none' }} />
          {status === 'exited' && s.restored ? 'Restored' : STATUS_LABEL[status]}
        </span>
        {s.meta?.worktree && (
          <>
            <button className={cx('icon-btn sm', props.reviewing && 'active')} title="Review changes" onClick={props.onDiff}>
              <FileDiff />
            </button>
            <button className="icon-btn sm" title="Open worktree in editor" onClick={() => SystemService.OpenInEditor(s.meta!.worktree!, 1).catch(fail('Editor'))}>
              <Code2 />
            </button>
            {s.meta.site === 'flock' && (
              <button className="icon-btn sm" title="Open this attempt in the browser (served by Flock)" onClick={openSite}>
                <Globe />
              </button>
            )}
            <button className="icon-btn sm" title="Merge into current branch" onClick={merge}>
              <GitMerge />
            </button>
          </>
        )}
        <button className="icon-btn sm" title={props.focused ? 'Back to grid' : 'Focus'} onClick={props.onFocus}>
          {props.focused ? <LayoutGrid /> : <Maximize2 />}
        </button>
        {!s.running && (
          <button className="btn sm" title="Start the agent again, resuming its conversation when it can" onClick={() => relaunch(s.id)}>
            <RotateCcw /> Relaunch
          </button>
        )}
        {s.running ? (
          <button className="icon-btn sm danger" title="Stop" onClick={() => TerminalService.Kill(s.id)}>
            <Square />
          </button>
        ) : (
          <button className="icon-btn sm danger" title={s.meta?.worktree ? 'Discard attempt' : 'Close'} onClick={discard}>
            <Trash2 />
          </button>
        )}
      </div>
      <TermPane id={s.id} focus={props.focused} />
      {s.running && (
        <div className="afoot">
          <input
            className="input"
            placeholder={s.setup ? 'Setting up the worktree…' : 'Follow up…'}
            disabled={s.setup}
            value={msg}
            onChange={(e) => setMsg(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && send()}
          />
          <button className="icon-btn" disabled={s.setup || !msg.trim()} onClick={send}>
            <Send />
          </button>
        </div>
      )}
    </div>
  )
}

async function mergeAttempt(projectId: string, s: SessionInfo) {
  if (!s.meta?.worktree) return false
  if (!(await confirmDialog({ title: `Merge ${s.meta.branch}?`, message: 'Pending changes in the worktree are committed, then the branch is merged into your current branch with --no-ff.', confirm: 'Merge' })))
    return false
  try {
    await GitService.MergeWorktree(projectId, s.meta.worktree, `feat: ${s.title}`)
    toast('success', 'Merged', s.meta.branch)
    return true
  } catch (e) {
    toast('error', 'Merge failed', errMsg(e))
    return false
  }
}

async function discardAttempt(projectId: string, s: SessionInfo) {
  if (s.meta?.worktree) {
    if (!(await confirmDialog({ title: 'Discard this attempt?', message: `The agent is stopped, the worktree folder and branch ${s.meta.branch} are deleted.`, confirm: 'Discard', danger: true })))
      return false
    await GitService.RemoveWorktree(projectId, s.meta.worktree, true).catch(fail('Could not remove worktree'))
  }
  TerminalService.Close(s.id)
  return true
}

/** The agent's branch against the main worktree's branch, docked beside the agents. */
function ReviewPanel({ s, onClose }: { s: SessionInfo; onClose: () => void }) {
  const { id } = useActive()
  const [text, setText] = useState<string | null>(null)
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)
  const [mode, setMode] = useState<'split' | 'unified'>(() => load('agents:diffMode', 'split'))
  const [listOpen, setListOpen] = useState(false)
  // Side by side needs room; a narrow panel shows the unified view instead.
  const box = useRef<HTMLDivElement>(null)
  const [narrow, setNarrow] = useState(false)
  useEffect(() => {
    const el = box.current?.closest('.side-panel')
    if (!el) return
    const ro = new ResizeObserver(([e]) => setNarrow(e.contentRect.width < 640))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  const shownMode = narrow ? 'unified' : mode
  const wt = s.meta!.worktree!
  useEffect(() => save('agents:diffMode', mode), [mode])

  const refresh = () => {
    setLoading(true)
    return GitService.Worktrees(wt)
      .then((list) => GitService.BranchDiff(wt, list?.find((w) => w.main)?.branch || 'HEAD'))
      .then((t) => {
        setText(t)
        setErr('')
      })
      .catch((e) => setErr(errMsg(e)))
      .finally(() => setLoading(false))
  }
  useEffect(() => {
    refresh()
    // While the agent works the diff keeps moving; follow it.
    if (!s.running) return
    const t = setInterval(refresh, 5000)
    return () => clearInterval(t)
  }, [s.id, s.running])

  const files = useMemo(() => (text ? parseDiff(text) : []), [text])
  const adds = files.reduce((n, f) => n + f.adds, 0)
  const dels = files.reduce((n, f) => n + f.dels, 0)
  const open = (path: string, line: number) => SystemService.OpenInEditor(`${wt.replace(/\/$/, '')}/${path}`, line).catch(fail('Editor'))
  const jump = (i: number) => document.getElementById(`dv-${s.id}-${i}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' })

  return (
    <SidePanel
      id="agent-review"
      className="review-panel"
      icon={
        <div className="avatar" style={{ background: brandUrl(s.key) ? 'var(--bg-3)' : s.meta?.color || '#888' }}>
          <Brand id={s.key} size={16} fallback={initials(s.meta?.agentName || s.key).slice(0, 1)} />
        </div>
      }
      title={
        <>
          {s.meta?.agentName} <span className="faint" style={{ fontWeight: 500 }}>· changes</span>
        </>
      }
      subtitle={
        <>
          <span title={s.meta?.prompt}>{s.title}</span>
          <span className="mono">
            {'  ·  '}
            <GitBranch size={10} style={{ verticalAlign: -1 }} /> {s.meta?.branch}
          </span>
        </>
      }
      actions={
        <button className="icon-btn" title="Refresh" disabled={loading} onClick={refresh}>
          <RefreshCw className={cx(loading && 'spin')} />
        </button>
      }
      onClose={onClose}
      footer={
        <>
          <span className="faint" style={{ fontSize: 12, marginRight: 'auto' }}>
            {files.length} {files.length === 1 ? 'file' : 'files'} · <span className="dv-count add">+{adds}</span> <span className="dv-count del">−{dels}</span>
          </span>
          <button className="btn sm danger" onClick={() => discardAttempt(id!, s).then((ok) => ok && onClose())}>
            <Trash2 /> Discard
          </button>
          <button className="btn sm success" disabled={!files.length} onClick={() => mergeAttempt(id!, s).then((ok) => void (ok && refresh()))}>
            <GitMerge /> Merge into current branch
          </button>
        </>
      }
    >
      <div ref={box} />
      {err ? (
        <div className="error-box" style={{ margin: 14 }}>{err}</div>
      ) : text == null ? (
        <div className="empty"><div className="spinner lg" /></div>
      ) : !files.length ? (
        <Empty icon={<FileDiff />} title="No changes yet" />
      ) : (
        <>
          <div className="dv-toolbar">
            <div className="tabs">
              <button
                className={cx('tab', shownMode === 'split' && 'active')}
                disabled={narrow}
                title={narrow ? 'Widen the panel to compare side by side' : undefined}
                onClick={() => setMode('split')}
              >
                <Columns2 /> Split
              </button>
              <button className={cx('tab', shownMode === 'unified' && 'active')} onClick={() => setMode('unified')}>
                <Rows3 /> Unified
              </button>
            </div>
            <span className="spacer" />
            <button className="btn sm ghost" onClick={() => copy(text).then(() => toast('success', 'Diff copied'))}>
              <Copy /> Copy diff
            </button>
          </div>
          <div className="dv-files">
            <button className="dv-files-head" onClick={() => setListOpen(!listOpen)}>
              {listOpen ? <ChevronDown size={14} /> : <ChevronRight size={14} />} Changed files ({files.length})
            </button>
            {listOpen &&
              files.map((f, i) => (
                <button key={f.path} className="dv-files-row" onClick={() => jump(i)} title={f.path}>
                  <span className="mono ellipsis grow">{f.path}</span>
                  <span className="dv-count add">+{f.adds}</span>
                  <span className="dv-count del">−{f.dels}</span>
                </button>
              ))}
          </div>
          {files.map((f, i) => (
            <FileDiffView key={f.path} id={`dv-${s.id}-${i}`} file={f} mode={shownMode} onOpen={() => open(f.path, firstLine(f))} />
          ))}
        </>
      )}
    </SidePanel>
  )
}
