import { type ReactNode, useCallback, useEffect, useMemo, useState } from 'react'
import { Events } from '@wailsio/runtime'
import {
  AlertTriangle,
  Check,
  Code2,
  Compass,
  Copy,
  FileDiff,
  FileText,
  GitBranch,
  GitMerge,
  Hammer,
  Pencil,
  RefreshCw,
  Rocket,
  Save,
  Scale,
  Send,
  Trash2,
  Wrench,
} from 'lucide-react'
import { AgentService, type AgentKind, type DriftReport, type SpecFile } from '../../bindings/larvyn/internal/agents'
import { type SessionInfo } from '../../bindings/larvyn/internal/terminal'
import { SystemService } from '../../bindings/larvyn/internal/system'
import { agentStatus, fail, toast } from '../lib/store'
import { activity } from '../lib/terms'
import { copy, cx, duration, errMsg, initials } from '../lib/util'
import { Empty, SidePanel, confirmDialog, useNow } from './ui'
import { MarkdownView } from './MarkdownView'
import { Brand, brandUrl } from './Brand'

/*
 * The Elephant-Goldfish workflow (internal/agents/egm.go) in the Agents view:
 * the specs of a project, the panel that walks one through draft → cold
 * review → revision → builders → drift check → merge, and the drift report
 * of a builder.
 */

export type SpecTab = 'spec' | 'review' | 'build'
export type Role = 'elephant' | 'goldfish' | 'builder' | 'drift'

export const ROLE_LABEL: Record<Role, string> = { elephant: 'Elephant', goldfish: 'Goldfish', builder: 'Builder', drift: 'Drift audit' }

/** The project's specs, refreshed whenever a spec file changes on disk. */
export function useSpecs(projectId: string | undefined) {
  const [specs, setSpecs] = useState<SpecFile[]>([])
  const refresh = useCallback(() => {
    if (!projectId) return
    AgentService.Specs(projectId)
      .then((list) => setSpecs(list ?? []))
      .catch(() => {})
  }, [projectId])
  useEffect(() => {
    setSpecs([])
    refresh()
  }, [refresh])
  useSpecsChanged(projectId, refresh)
  return { specs, refresh }
}

/** Calls fn when a spec file of the project (or a drift report in one of its worktrees) changes. */
export function useSpecsChanged(projectId: string | undefined, fn: () => void) {
  useEffect(() => {
    if (!projectId) return
    return Events.On('specs:changed', (ev: any) => ev.data === projectId && fn())
  }, [projectId, fn])
}

/** Whether an agent is working or asking something right now. */
function atWork(a: SessionInfo) {
  const st = agentStatus({ ...a, lastActivity: activity.get(a.id) ?? a.lastActivity } as SessionInfo, Date.now())
  return st === 'working' || st === 'attention'
}

/** Re-renders every 2 s while an agent of the project runs, so "at work" follows it. */
function useTick(agents: SessionInfo[]) {
  useNow(2000, agents.some((a) => a.running && a.meta?.spec))
}

/** Whether a stateless session (Goldfish, auditor) wrote its report since it
 * started: it is done, though some agents keep their TUI open afterwards. */
function reported(a: SessionInfo, reportAt: number | undefined) {
  return !!reportAt && reportAt >= a.startedAt
}

/** The sessions of a spec still at work, by role, and all its builders. */
function specSessions(agents: SessionInfo[], sf: Pick<SpecFile, 'slug' | 'reviewAt'>) {
  const of = (role: Role) => agents.filter((a) => a.meta?.spec === sf.slug && a.meta?.role === role)
  const elephant = of('elephant').find((a) => a.running)
  return {
    // Open, maybe waiting for the user; `writing` only while it works on the spec.
    elephant,
    writing: !!elephant && atWork(elephant),
    goldfish: of('goldfish').find((a) => a.running && !reported(a, sf.reviewAt)),
    builders: of('builder'),
    drifts: of('drift').filter((a) => a.running),
  }
}

export function verdictClass(verdict?: string) {
  switch (verdict) {
    case 'READY':
    case 'ALIGNED':
      return 'green'
    case 'NEEDS REVISION':
    case 'MINOR DRIFT':
      return 'yellow'
    case 'BLOCKED':
    case 'MAJOR DRIFT':
      return 'red'
  }
  return ''
}

/** Where a spec stands, as a badge. */
export function specState(sf: SpecFile, agents: SessionInfo[]): { label: string; cls: string; busy?: boolean } {
  const s = specSessions(agents, sf)
  if (s.goldfish) return { label: 'Reviewing', cls: 'yellow', busy: true }
  if (s.writing) return { label: 'Elephant', cls: 'blue', busy: true }
  if (s.builders.some((b) => b.running)) return { label: 'Building', cls: 'accent', busy: true }
  if (sf.approved) return { label: 'Approved', cls: 'green' }
  if (!sf.hasReview) return { label: 'Draft', cls: '' }
  if (sf.stale) return { label: 'Review again', cls: 'yellow' }
  if (!sf.verdict) return { label: 'No verdict', cls: 'yellow' }
  return { label: sf.verdict === 'NEEDS REVISION' ? 'Needs revision' : 'Blocked', cls: verdictClass(sf.verdict) }
}

function StateBadge({ sf, agents }: { sf: SpecFile; agents: SessionInfo[] }) {
  const st = specState(sf, agents)
  return (
    <span className={cx('badge', st.cls)}>
      {st.busy && <span className="status-dot busy" style={{ width: 6, height: 6, boxShadow: 'none' }} />}
      {st.label}
    </span>
  )
}

/** The specs under the composer, one pill each. */
export function SpecStrip(props: { specs: SpecFile[]; agents: SessionInfo[]; active: string | null; onOpen: (slug: string) => void; hint: boolean }) {
  useTick(props.agents)
  if (!props.specs.length && !props.hint) return null
  return (
    <div className="spec-strip">
      <span className="spec-strip-label">
        <FileText size={13} /> Specs
      </span>
      {props.specs.map((sf) => (
        <button
          key={sf.slug}
          type="button"
          className={cx('spec-pill', props.active === sf.slug && 'active')}
          onClick={() => props.onOpen(sf.slug)}
          title={`${sf.path}${sf.summary ? ' · ' + sf.summary : ''}`}
        >
          <span className="ellipsis" style={{ maxWidth: 180 }}>
            {sf.title}
          </span>
          <StateBadge sf={sf} agents={props.agents} />
        </button>
      ))}
      {!props.specs.length && (
        <span className="faint" style={{ fontSize: 11.5 }}>
          None yet. Describe a feature above and the Elephant writes its spec in <span className="mono">.larvyn/specs/</span> first.
        </span>
      )}
    </div>
  )
}

/** Picks one or more installed agents. */
function AgentPicker(props: { kinds: AgentKind[]; value: string[]; onChange: (v: string[]) => void; single?: boolean }) {
  return (
    <div className="agents">
      {props.kinds.map((k) => {
        const on = props.value.includes(k.id)
        return (
          <button
            key={k.id}
            type="button"
            className={cx('agent-pick', on && 'on')}
            onClick={() => props.onChange(props.single ? [k.id] : on ? props.value.filter((x) => x !== k.id) : [...props.value, k.id])}
          >
            <span className="sq" style={{ background: brandUrl(k.id) && !on ? 'var(--bg-3)' : k.color }}>
              {on ? <Check size={10} strokeWidth={3.5} /> : <Brand id={k.id} size={12} fallback={initials(k.name).slice(0, 1)} />}
            </span>
            {k.name}
          </button>
        )
      })}
    </div>
  )
}

/** Another agent than `not` when one is installed: a second model catches what the first one missed. */
function otherAgent(kinds: AgentKind[], not: string | undefined, fallback: string) {
  return kinds.find((k) => k.id !== not)?.id || not || fallback
}

function Steps(props: { tab: SpecTab; setTab: (t: SpecTab) => void; sf: SpecFile | null; agents: SessionInfo[] }) {
  const s = props.sf ? specSessions(props.agents, props.sf) : null
  const steps: { id: SpecTab; icon: typeof Compass; title: string; sub: string; done: boolean; busy: boolean }[] = [
    { id: 'spec', icon: Compass, title: '1 · Spec', sub: s?.writing ? 'Elephant at work' : s?.elephant ? 'Elephant waiting for you' : 'Elephant, no code', done: !!props.sf?.hasReview, busy: !!s?.writing },
    {
      id: 'review',
      icon: Scale,
      title: `2 · Review${props.sf?.rounds ? ` (${props.sf.rounds})` : ''}`,
      sub: s?.goldfish ? 'Goldfish reading cold' : props.sf?.approved ? 'Approved' : props.sf?.stale ? 'Review again' : props.sf?.verdict || 'Cold, no memory',
      done: !!props.sf?.approved,
      busy: !!s?.goldfish,
    },
    {
      id: 'build',
      icon: Hammer,
      title: `3 · Build${s?.builders.length ? ` (${s.builders.length})` : ''}`,
      sub: s?.builders.length ? 'Drift check, then merge' : 'One worktree per agent',
      done: false,
      busy: !!s?.builders.some((b) => b.running) || !!s?.drifts.length,
    },
  ]
  return (
    <div className="egm-steps">
      {steps.map((st) => (
        <button key={st.id} type="button" className={cx('egm-step', props.tab === st.id && 'active', st.done && 'done')} onClick={() => props.setTab(st.id)}>
          <b>
            {st.done ? <Check /> : <st.icon />} {st.title}
            {st.busy && <span className="status-dot busy" style={{ width: 6, height: 6, boxShadow: 'none' }} />}
          </b>
          <span>{st.sub}</span>
        </button>
      ))}
    </div>
  )
}

/** One spec through the whole workflow, docked beside the agents. */
export function SpecPanel(props: {
  projectId: string
  slug: string
  tab: SpecTab
  setTab: (t: SpecTab) => void
  kinds: AgentKind[]
  picked: string[]
  agents: SessionInfo[]
  onFocusAgent: (id: string) => void
  onReview: (id: string, drift?: boolean) => void
  onMerge: (s: SessionInfo) => void
  onDiscard: (s: SessionInfo) => void
  onDeleted: () => void
  onClose: () => void
}) {
  const { projectId, slug, tab, setTab, kinds, agents } = props
  const [sf, setSf] = useState<SpecFile | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  // Editing: the text and the version of the file it started from.
  const [draft, setDraft] = useState<string | null>(null)
  const [editBase, setEditBase] = useState(0)
  useTick(agents)
  const s = specSessions(agents, sf ?? { slug, reviewAt: 0 })
  const author = s.elephant?.key || props.picked[0]
  const [reviewer, setReviewer] = useState(() => otherAgent(kinds, author, 'claude'))
  const [focus, setFocus] = useState('')
  const [builders, setBuilders] = useState<string[]>(() => (props.picked.length ? props.picked : kinds.slice(0, 1).map((k) => k.id)))
  const [notes, setNotes] = useState('')
  const [deps, setDeps] = useState(true)

  const reload = useCallback(() => {
    AgentService.ReadSpec(projectId, slug)
      .then((f) => {
        setSf(f)
        setErr('')
      })
      .catch((e) => setErr(errMsg(e)))
  }, [projectId, slug])
  useEffect(reload, [reload])
  useSpecsChanged(projectId, reload)
  // The installed agents arrive after the panel opens.
  useEffect(() => {
    if (kinds.length && !kinds.some((k) => k.id === reviewer)) setReviewer(otherAgent(kinds, author, kinds[0].id))
  }, [kinds])

  const run = async (what: string, fn: () => Promise<unknown>) => {
    setBusy(true)
    try {
      await fn()
    } catch (e) {
      toast('error', what, errMsg(e))
    } finally {
      setBusy(false)
    }
  }

  const save = (base: number) =>
    run('Could not save the spec', async () => {
      try {
        const f = await AgentService.SaveSpec(projectId, slug, draft ?? '', base)
        setSf(f)
        setDraft(null)
        toast('success', 'Spec saved')
      } catch (e) {
        if (!errMsg(e).includes('changed on disk')) throw e
        const ok = await confirmDialog({
          title: 'The spec changed on disk',
          message: 'Someone (most likely the Elephant) changed it while you were editing. Overwrite their changes with yours?',
          confirm: 'Overwrite',
          danger: true,
        })
        if (ok) await save(0)
      }
    })

  const continueElephant = () =>
    run('Could not start the Elephant', async () => {
      const e = await AgentService.LaunchElephant({ projectId, agent: author || kinds[0]?.id || 'claude', slug, prompt: '' })
      props.onFocusAgent(e.id)
    })

  const review = () =>
    run('Could not start the review', async () => {
      await AgentService.LaunchGoldfish({ projectId, agent: reviewer, slug, focus: focus.trim() })
      setTab('review')
      toast('success', 'Cold review started', 'A fresh agent with no memory of the spec is reading it against the code.')
    })

  const revise = () =>
    run('Could not hand the review to the Elephant', async () => {
      const e = await AgentService.ReviseSpec(projectId, slug, author || kinds[0]?.id || 'claude')
      setTab('spec')
      toast('success', s.elephant ? 'Review sent to the Elephant' : 'Elephant started on the review', 'Review the spec again once it is done.')
      if (e?.id) props.onFocusAgent(e.id)
    })

  const build = () =>
    run('Could not start the builders', async () => {
      if (!builders.length) throw new Error('Pick at least one agent')
      let force = false
      if (!sf?.approved) {
        force = await confirmDialog({
          title: 'Build without an approved review?',
          message: !sf?.hasReview
            ? 'Nobody has reviewed this spec yet. A cold review catches the guesses builders would otherwise make.'
            : sf.stale
              ? 'The spec changed after its last review, so that review no longer vouches for it.'
              : `The last review says ${sf.verdict || 'nothing conclusive'}.`,
          confirm: 'Build anyway',
          danger: true,
        })
        if (!force) return
      }
      const started = await AgentService.LaunchFromSpec({ projectId, slug, notes: notes.trim(), worktree: true, installDeps: deps, base: '', force }, builders)
      setTab('build')
      toast('success', `${started?.length ?? 0} ${started?.length === 1 ? 'builder' : 'builders'} started`, 'Each one has its own worktree with a copy of the spec.')
    })

  const remove = async () => {
    const ok = await confirmDialog({
      title: `Delete ${sf?.title || slug}?`,
      message: 'The spec, its reviews and drift reports are deleted. Worktrees and branches stay.',
      confirm: 'Delete',
      danger: true,
    })
    if (!ok) return
    run('Could not delete the spec', async () => {
      await AgentService.DeleteSpec(projectId, slug)
      props.onDeleted()
    })
  }

  const changedWhileEditing = draft != null && !!sf && editBase > 0 && sf.updatedAt !== editBase
  const running = !!(s.elephant || s.goldfish)

  return (
    <SidePanel
      id="spec"
      className="spec-panel"
      icon={<Compass />}
      title={
        <span className="ellipsis" title={sf?.title}>
          {sf?.title || slug}
        </span>
      }
      subtitle={
        <>
          {sf && <StateBadge sf={sf} agents={agents} />}
          <span className="mono"> {sf?.path || `.larvyn/specs/${slug}.md`}</span>
        </>
      }
      actions={
        <>
          <button className="icon-btn" title="Open in editor" disabled={!sf} onClick={() => sf && SystemService.OpenInEditor(sf.absPath, 1).catch(fail('Editor'))}>
            <Code2 />
          </button>
          <button className="icon-btn" title="Reload" onClick={reload}>
            <RefreshCw className={cx(running && 'spin')} />
          </button>
          <button className="icon-btn danger" title="Delete spec" disabled={running} onClick={remove}>
            <Trash2 />
          </button>
        </>
      }
      onClose={props.onClose}
      footer={footer()}
    >
      <Steps tab={tab} setTab={setTab} sf={sf} agents={agents} />
      {err ? (
        <div className="error-box" style={{ margin: 14 }}>
          {err}
        </div>
      ) : !sf ? (
        <div className="empty">
          <div className="spinner lg" />
        </div>
      ) : tab === 'spec' ? (
        <>
          <div className="spec-bar">
            <span className="faint ellipsis grow" style={{ fontSize: 11.5 }}>
              {s.writing ? (
                <>
                  <span className="status-dot busy" style={{ width: 6, height: 6, boxShadow: 'none', marginRight: 6 }} />
                  The Elephant is writing this spec.
                </>
              ) : s.elephant ? (
                'The Elephant is waiting: answer its questions or tell it what to change in its terminal.'
              ) : sf.hasReview ? (
                `Reviewed ${sf.rounds} ${sf.rounds === 1 ? 'time' : 'times'}${sf.stale ? ', changed since' : ''}`
              ) : (
                'Not reviewed yet'
              )}
            </span>
            {draft != null ? (
              <>
                <button className="btn sm ghost" onClick={() => setDraft(null)}>
                  Cancel
                </button>
                <button className="btn sm primary" disabled={busy} onClick={() => save(editBase)}>
                  <Save /> Save
                </button>
              </>
            ) : (
              <>
                {s.elephant ? (
                  <button className="btn sm ghost" onClick={() => props.onFocusAgent(s.elephant!.id)}>
                    <Compass /> Elephant
                  </button>
                ) : (
                  <button className="btn sm ghost" disabled={busy || !kinds.length} onClick={continueElephant} title="Start an Elephant on this spec">
                    <Compass /> Work on it with the Elephant
                  </button>
                )}
                <button className="btn sm ghost" onClick={() => copy(sf.content).then(() => toast('success', 'Spec copied'))}>
                  <Copy /> Copy
                </button>
                <button
                  className="btn sm"
                  onClick={() => {
                    setDraft(sf.content)
                    setEditBase(sf.updatedAt)
                  }}
                >
                  <Pencil /> Edit
                </button>
              </>
            )}
          </div>
          {changedWhileEditing && (
            <div className="spec-note yellow">
              <AlertTriangle size={13} /> The file changed on disk while you edit. Saving will ask before it overwrites.
            </div>
          )}
          {draft != null ? (
            <textarea className="spec-editor" value={draft} onChange={(e) => setDraft(e.target.value)} spellCheck={false} autoFocus />
          ) : (
            <MarkdownView content={sf.content} />
          )}
        </>
      ) : tab === 'review' ? (
        <>
          <div className="review-box">
            <div className="row">
              <b>
                <Scale size={13} /> Cold review
              </b>
              <span className="faint">A new agent with no memory reads the spec against the code and writes a verdict.</span>
            </div>
            <AgentPicker kinds={kinds} value={[reviewer]} onChange={(v) => setReviewer(v[0])} single />
            {author && reviewer === author && kinds.length > 1 && (
              <span className="faint" style={{ fontSize: 11.5 }}>
                Tip: a different agent than the one that wrote the spec reviews it with fresher eyes.
              </span>
            )}
            <div className="row">
              <input
                className="input grow"
                style={{ height: 30, fontSize: 12 }}
                placeholder="Look especially at… (optional, e.g. tenant isolation, N+1 queries)"
                value={focus}
                onChange={(e) => setFocus(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && !busy && !s.goldfish && review()}
              />
              <button className="btn sm primary" disabled={busy || !!s.goldfish || s.writing || !reviewer} onClick={review} title={s.writing ? 'Wait until the Elephant is done' : undefined}>
                <Scale /> {s.goldfish ? 'Reviewing…' : sf.hasReview ? 'Review again' : 'Review'}
              </button>
            </div>
          </div>
          {sf.hasReview && !s.goldfish && <Verdict sf={sf} busy={busy} elephant={!!s.elephant} writing={s.writing} onRevise={revise} onReview={review} onBuild={() => setTab('build')} />}
          {s.goldfish ? (
            <Empty icon={<Scale />} title="The Goldfish is reviewing the spec">
              <button className="btn sm ghost" onClick={() => props.onFocusAgent(s.goldfish!.id)}>
                Watch it
              </button>
            </Empty>
          ) : sf.hasReview ? (
            <MarkdownView content={sf.review} />
          ) : (
            <Empty icon={<Scale />} title="No review yet">
              It catches the assumptions, schema gaps and missing policies a builder would otherwise guess at.
            </Empty>
          )}
        </>
      ) : (
        <>
          <div className="review-box">
            <div className="row">
              <b>
                <Hammer size={13} /> Builders
              </b>
              <span className="faint">Each agent implements the spec on its own branch. Check each one for drift, then merge the best.</span>
            </div>
            <AgentPicker kinds={kinds} value={builders} onChange={setBuilders} />
            <div className="row">
              <input className="input grow" style={{ height: 30, fontSize: 12 }} placeholder="Notes for the builders (optional)" value={notes} onChange={(e) => setNotes(e.target.value)} />
              <label className="check" title="Run composer install and npm install in each new worktree">
                <input type="checkbox" checked={deps} onChange={(e) => setDeps(e.target.checked)} /> Install deps
              </label>
              <button className={cx('btn sm', sf.approved ? 'primary' : '')} disabled={busy || !builders.length} onClick={build}>
                <Rocket /> Build with {builders.length} {builders.length === 1 ? 'agent' : 'agents'}
              </button>
            </div>
            {!sf.approved && (
              <span className="spec-note yellow inline">
                <AlertTriangle size={13} /> {sf.hasReview ? (sf.stale ? 'Changed since its review.' : `Review says ${sf.verdict || 'no verdict'}.`) : 'Not reviewed yet.'}
              </span>
            )}
          </div>
          {s.builders.length === 0 ? (
            <Empty icon={<Hammer />} title="No builders yet" />
          ) : (
            <div className="attempts">
              {s.builders.map((b) => (
                <Attempt
                  key={b.id}
                  projectId={projectId}
                  b={b}
                  kinds={kinds}
                  agents={agents}
                  onFocus={() => props.onFocusAgent(b.id)}
                  onReview={(drift) => props.onReview(b.id, drift)}
                  onMerge={() => props.onMerge(b)}
                  onDiscard={() => props.onDiscard(b)}
                />
              ))}
            </div>
          )}
        </>
      )}
    </SidePanel>
  )

  function footer() {
    if (!sf) return null
    if (tab === 'spec')
      return (
        <>
          <span className="faint" style={{ fontSize: 12, marginRight: 'auto' }}>
            Next: a cold review by an agent that didn't write it
          </span>
          <button className="btn sm primary" disabled={s.writing} onClick={() => setTab('review')}>
            <Scale /> Review
          </button>
        </>
      )
    if (tab === 'review')
      return (
        <>
          <span className="faint" style={{ fontSize: 12, marginRight: 'auto' }}>
            {sf.approved ? 'Approved: ready to build' : 'Build once a review says READY'}
          </span>
          <button className={cx('btn sm', sf.approved && 'primary')} onClick={() => setTab('build')}>
            <Hammer /> Build
          </button>
        </>
      )
    return (
      <span className="faint" style={{ fontSize: 12, marginRight: 'auto' }}>
        {s.builders.length} {s.builders.length === 1 ? 'attempt' : 'attempts'} · merging one keeps the spec out of the commit
      </span>
    )
  }
}

/** The latest review's verdict and what to do about it. */
function Verdict(props: { sf: SpecFile; busy: boolean; elephant: boolean; writing: boolean; onRevise: () => void; onReview: () => void; onBuild: () => void }) {
  const { sf } = props
  let text: string, action: ReactNode
  if (sf.approved) {
    text = 'The spec is ready: a builder can implement it without guessing.'
    action = (
      <button className="btn sm primary" onClick={props.onBuild}>
        <Hammer /> Build it
      </button>
    )
  } else if (sf.stale) {
    text = 'The spec changed after this review. Review it again so the new version is vouched for.'
    action = (
      <button className="btn sm primary" disabled={props.busy || props.writing} onClick={props.onReview}>
        <Scale /> Review again
      </button>
    )
  } else {
    text = !sf.verdict
      ? 'The review names no verdict on its first line.'
      : sf.verdict === 'BLOCKED'
        ? 'Blocked: it needs a decision from you, or can’t work as written. Answer in the Elephant’s terminal.'
        : 'Gaps to close first. The Elephant folds the amendments into the spec.'
    action = (
      <>
        {sf.rounds >= 2 && (
          <button className="btn sm ghost" onClick={props.onBuild} title="You decide the spec is good enough: building asks you to confirm">
            <Hammer /> Build anyway
          </button>
        )}
        <button className="btn sm primary" disabled={props.busy} onClick={props.onRevise}>
          <Send /> {props.elephant ? 'Send to the Elephant' : 'Revise with the Elephant'}
        </button>
      </>
    )
  }
  return (
    <div className="verdict-row">
      <span className={cx('badge', sf.stale ? 'yellow' : verdictClass(sf.verdict))}>{sf.verdict || 'No verdict'}</span>
      <span className="faint grow">{text}</span>
      {action}
    </div>
  )
}

/** A builder's drift report, refreshed when it changes. */
export function useDrift(projectId: string, b: SessionInfo) {
  const [rep, setRep] = useState<DriftReport | null>(null)
  const slug = b.meta?.spec || ''
  const refresh = useCallback(() => {
    if (!slug) return
    AgentService.ReadDrift(projectId, b.cwd, slug)
      .then((r) => setRep(r?.content ? r : null))
      .catch(() => setRep(null))
  }, [projectId, b.cwd, slug])
  useEffect(refresh, [refresh])
  useSpecsChanged(projectId, refresh)
  return rep
}

/** Runs a drift check on a builder and sends it the fixes. */
function useDriftActions(projectId: string, b: SessionInfo, kinds: AgentKind[], agents: SessionInfo[], rep: DriftReport | null) {
  const auditing = agents.some((a) => a.running && a.meta?.role === 'drift' && a.cwd === b.cwd && !reported(a, rep?.updatedAt))
  const check = () =>
    AgentService.LaunchDriftCheck({ projectId, agent: otherAgent(kinds, b.key, b.key), slug: b.meta?.spec || '', worktree: b.cwd })
      .then(() => toast('success', 'Drift check started', 'A fresh agent compares this attempt with the spec.'))
      .catch(fail('Could not start the drift check'))
  const fix = () =>
    AgentService.FixDrift(b.id)
      .then(() => toast('success', 'Drift report sent', `${b.meta?.agentName || 'The builder'} is working through its fixes.`))
      .catch(fail('Could not send the fixes'))
  return { auditing, check, fix }
}

function Attempt(props: {
  projectId: string
  b: SessionInfo
  kinds: AgentKind[]
  agents: SessionInfo[]
  onFocus: () => void
  onReview: (drift: boolean) => void
  onMerge: () => void
  onDiscard: () => void
}) {
  const { b } = props
  const now = useNow(1000, b.running)
  const st = agentStatus({ ...b, lastActivity: activity.get(b.id) ?? b.lastActivity } as SessionInfo, now)
  const rep = useDrift(props.projectId, b)
  const { auditing, check, fix } = useDriftActions(props.projectId, b, props.kinds, props.agents, rep)
  const label = st === 'working' ? 'Working' : st === 'attention' ? 'Needs you' : st === 'done' ? 'Done' : st === 'idle' ? 'Waiting' : b.exitCode === 0 ? 'Finished' : 'Exited'
  return (
    <div className="attempt">
      <div className="avatar" style={{ background: brandUrl(b.key) ? 'var(--bg-3)' : b.meta?.color || '#888' }}>
        <Brand id={b.key} size={14} fallback={initials(b.meta?.agentName || b.key).slice(0, 1)} />
      </div>
      <button className="grow attempt-name" onClick={props.onFocus} title="Show its terminal">
        <b>{b.meta?.agentName || b.key}</b>
        <span className="mono faint">
          <GitBranch size={10} style={{ verticalAlign: -1 }} /> {b.meta?.branch || 'project folder'} · {duration((b.running ? now : b.endedAt) - b.startedAt)}
        </span>
      </button>
      <span className={cx('badge', st === 'working' ? 'accent' : st === 'attention' ? 'yellow' : st === 'done' ? 'green' : st === 'exited' && b.exitCode !== 0 ? 'red' : '')}>{label}</span>
      {auditing ? (
        <span className="badge yellow">
          <span className="status-dot busy" style={{ width: 6, height: 6, boxShadow: 'none' }} /> Auditing
        </span>
      ) : rep ? (
        <button className={cx('badge', rep.stale ? '' : verdictClass(rep.verdict))} title={rep.stale ? 'The spec changed after this audit' : 'Open the drift report'} onClick={() => props.onReview(true)}>
          {rep.verdict || 'No verdict'}
          {rep.stale && ' (old)'}
        </button>
      ) : null}
      <div className="attempt-actions">
        <button className="icon-btn sm" title="Review its changes" onClick={() => props.onReview(false)}>
          <FileDiff />
        </button>
        <button className="icon-btn sm" title={st === 'working' ? 'Wait until it is done' : 'Check its changes against the spec'} disabled={auditing || st === 'working'} onClick={check}>
          <Scale />
        </button>
        {rep && rep.verdict !== 'ALIGNED' && (
          <button className="icon-btn sm" title={b.running ? 'Send it the drift report to fix' : 'Relaunch it first'} disabled={!b.running || auditing} onClick={fix}>
            <Wrench />
          </button>
        )}
        {b.meta?.worktree && (
          <>
            <button className="icon-btn sm" title="Merge into current branch" onClick={props.onMerge}>
              <GitMerge />
            </button>
            <button className="icon-btn sm danger" title="Discard attempt" onClick={props.onDiscard}>
              <Trash2 />
            </button>
          </>
        )}
      </div>
    </div>
  )
}

/** The drift tab of a builder's review panel. */
export function DriftView(props: { projectId: string; b: SessionInfo; kinds: AgentKind[]; agents: SessionInfo[]; onOpenSpec: () => void }) {
  const { b } = props
  const rep = useDrift(props.projectId, b)
  const { auditing, check, fix } = useDriftActions(props.projectId, b, props.kinds, props.agents, rep)
  const auditor = useMemo(() => props.kinds.find((k) => k.id === otherAgent(props.kinds, b.key, b.key))?.name, [props.kinds, b.key])
  return (
    <>
      <div className="review-box">
        <div className="row">
          <b>
            <Scale size={13} /> Spec drift
          </b>
          <span className="faint">
            A fresh agent{auditor ? ` (${auditor})` : ''} checks every requirement of <span className="mono">{b.meta?.spec}.md</span> against this attempt.
          </span>
        </div>
        <div className="row">
          {rep && <span className={cx('badge', rep.stale ? '' : verdictClass(rep.verdict))}>{rep.verdict || 'No verdict'}</span>}
          {rep?.stale && <span className="faint">The spec changed after this audit.</span>}
          <span className="spacer" />
          <button className="btn sm ghost" onClick={props.onOpenSpec}>
            <FileText /> Spec
          </button>
          {rep && rep.verdict !== 'ALIGNED' && (
            <button className="btn sm" disabled={!b.running || auditing} title={b.running ? undefined : 'Relaunch the builder first'} onClick={fix}>
              <Wrench /> Send fixes to builder
            </button>
          )}
          <button className="btn sm primary" disabled={auditing} onClick={check}>
            <Scale /> {auditing ? 'Auditing…' : rep ? 'Check again' : 'Check drift'}
          </button>
        </div>
      </div>
      {rep ? (
        <MarkdownView content={rep.content} />
      ) : (
        <Empty icon={<Scale />} title={auditing ? 'The auditor is comparing the changes with the spec' : 'Not checked yet'} />
      )}
    </>
  )
}
