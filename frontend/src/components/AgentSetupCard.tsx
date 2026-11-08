import { useState } from 'react'
import { Eye, Plug, Trash2 } from 'lucide-react'
import { AgentService } from '../../bindings/larvyn/internal/agents'
import { type Change } from '../../bindings/larvyn/internal/agentsetup'
import { toast } from '../lib/store'
import { errMsg } from '../lib/util'

/**
 * Tells the coding agents about Larvyn: a skill that teaches the larvyn
 * command and a session-start hook with where the agent runs, written to
 * the config of the agents that are installed (larvyn agents setup does the
 * same). Preview lists the changes first; Remove takes them out again.
 */
export function AgentSetupCard() {
  const [changes, setChanges] = useState<{ remove: boolean; dryRun: boolean; list: Change[] } | null>(null)
  const [busy, setBusy] = useState(false)
  const run = async (remove: boolean, dryRun: boolean) => {
    setBusy(true)
    try {
      const list = (await AgentService.SetupAgents(remove, dryRun)) ?? []
      setChanges({ remove, dryRun, list })
      if (!dryRun) toast('success', remove ? 'Larvyn removed from the agents' : 'Agents set up', list.some((c) => c.note) ? 'Codex asks you to review the new hook once (/hooks)' : undefined)
    } catch (e) {
      toast('error', remove ? 'Could not remove it' : 'Could not set up the agents', errMsg(e))
    } finally {
      setBusy(false)
    }
  }
  const changed = changes?.list.filter((c) => c.action !== 'unchanged') ?? []
  return (
    <div className="card" style={{ marginBottom: 18 }}>
      <div className="card-head">
        <h3>
          <Plug /> Agents know Larvyn
        </h3>
        <div className="row" style={{ marginLeft: 'auto', gap: 8 }}>
          <button className="btn sm" disabled={busy} onClick={() => run(false, true)}>
            <Eye /> Preview
          </button>
          <button className="btn sm primary" disabled={busy} onClick={() => run(false, false)}>
            <Plug /> Set up agents
          </button>
          <button className="btn sm" disabled={busy} onClick={() => run(true, false)} title="Take the skill and the hook out again">
            <Trash2 /> Remove
          </button>
        </div>
      </div>
      <div className="card-body" style={{ display: 'grid', gap: 10 }}>
        <span className="hint">
          Installs a <span className="mono">larvyn</span> skill and a session-start hook in Claude Code and Codex, so an agent started in Larvyn knows its project, worktree and dev stack, and uses the{' '}
          <span className="mono">larvyn</span> command to start services, run tests and hand work to other agents. Running it again is safe.
        </span>
        {changes && (
          <div style={{ display: 'grid', gap: 4 }}>
            {changes.dryRun && <strong>Would change:</strong>}
            {changed.length === 0 && <span className="hint">{changes.remove ? 'Nothing of Larvyn’s is installed.' : 'Already set up: nothing to change.'}</span>}
            {changed.map((c) => (
              <div key={c.path} className="mono" style={{ fontSize: 12 }}>
                {c.action} · {c.what} · {c.path}
                {c.note && <div className="hint">{c.note}</div>}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
