import { useEffect, useState } from 'react'
import { ArrowDown, ArrowUp, Code2, FolderGit2, GitBranch, GitCommitHorizontal, GitMerge, Plus, RefreshCw, Trash2, Undo2 } from 'lucide-react'
import { GitService, SystemService, type WorktreeRequest, type GitStatus } from '../../bindings/larvyn'
import { fail, newTerminal, refreshInfo, toast, useActive } from '../lib/store'
import { ago, cx, errMsg } from '../lib/util'
import { Diff, Empty, Loading, confirmDialog, promptDialog, useAsync } from '../components/ui'

const STATUS_COLOR: Record<string, string> = { M: 'var(--orange)', A: 'var(--green)', D: 'var(--red)', R: 'var(--blue)', '?': 'var(--green)', U: 'var(--red)' }

export function Git() {
  const { id, project } = useActive()
  const st = useAsync<GitStatus>(() => GitService.Status(id!), [id])
  const [file, setFile] = useState<string | null>(null)
  const [diff, setDiff] = useState<string>('')
  const [msg, setMsg] = useState('')
  const [tab, setTab] = useState<'changes' | 'history' | 'worktrees'>('changes')

  useEffect(() => {
    if (!id || !file) return setDiff('')
    GitService.Diff(id, file, false).then(setDiff).catch((e) => setDiff(errMsg(e)))
  }, [id, file, st.data])
  useEffect(() => {
    const t = setInterval(() => st.reload(), 6000)
    return () => clearInterval(t)
  }, [id])

  if (!project) return null
  if (st.loading && !st.data) return <Loading />
  const s = st.data
  if (!s?.isRepo)
    return (
      <Empty icon={<GitBranch />} title="Not a git repository" action={<button className="btn primary" onClick={() => newTerminal(id!, { command: 'git init' })}>git init</button>}>
        Initialise a repository to track changes and run agents in worktrees.
      </Empty>
    )
  const files = s.files ?? []

  const commit = async () => {
    try {
      await GitService.Commit(id!, msg, [])
      setMsg('')
      setFile(null)
      toast('success', 'Committed', msg.split('\n')[0])
      st.reload()
      refreshInfo(id!)
    } catch (e) {
      toast('error', 'Commit failed', errMsg(e))
    }
  }

  return (
    <div className="view flush">
      <div className="toolbar">
        <h1>
          <GitBranch /> {s.branch}
        </h1>
        {s.upstream && <span className="badge mono">{s.upstream}</span>}
        {s.ahead > 0 && (
          <span className="badge green">
            <ArrowUp /> {s.ahead}
          </span>
        )}
        {s.behind > 0 && (
          <span className="badge yellow">
            <ArrowDown /> {s.behind}
          </span>
        )}
        <select
          className="select"
          value={s.branch}
          style={{ width: 200, marginLeft: 8 }}
          onChange={(e) => GitService.Checkout(id!, e.target.value, false).then(st.reload).then(() => refreshInfo(id!)).catch(fail('Checkout failed'))}
        >
          {(s.branches ?? []).map((b) => (
            <option key={b}>{b}</option>
          ))}
        </select>
        <button
          className="btn sm ghost"
          onClick={async () => {
            const name = await promptDialog({ title: 'New branch', label: 'Branch name', placeholder: 'feature/…', confirm: 'Create' })
            if (name) GitService.Checkout(id!, name, true).then(st.reload).then(() => refreshInfo(id!)).catch(fail('Could not create branch'))
          }}
        >
          <Plus /> Branch
        </button>
        <span className="spacer" />
        <div className="tabs">
          <button className={cx('tab', tab === 'changes' && 'active')} onClick={() => setTab('changes')}>
            Changes {files.length > 0 && <span className="pill-n">{files.length}</span>}
          </button>
          <button className={cx('tab', tab === 'history' && 'active')} onClick={() => setTab('history')}>
            History
          </button>
          <button className={cx('tab', tab === 'worktrees' && 'active')} onClick={() => setTab('worktrees')}>
            Worktrees {(s.worktrees?.length ?? 0) > 1 && <span className="pill-n">{(s.worktrees?.length ?? 1) - 1}</span>}
          </button>
        </div>
        <button className="icon-btn" onClick={st.reload}>
          <RefreshCw />
        </button>
      </div>

      {tab === 'changes' && (
        <div className="split">
          <div className="pane-list" style={{ width: 320 }}>
            <div style={{ padding: 10, borderBottom: '1px solid var(--line)' }} className="col">
              <textarea
                className="textarea"
                rows={3}
                placeholder="Commit message (Ctrl+Enter)"
                value={msg}
                onChange={(e) => setMsg(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && (e.ctrlKey || e.metaKey) && msg.trim() && commit()}
              />
              <button className="btn primary" disabled={!msg.trim() || !files.length} onClick={commit}>
                <GitCommitHorizontal /> Commit {files.length} file{files.length === 1 ? '' : 's'}
              </button>
            </div>
            <div className="list-scroll">
              {files.length === 0 && <div className="faint" style={{ padding: 14, fontSize: 12 }}>Working tree clean.</div>}
              {files.map((f) => {
                const code = (f.index !== ' ' && f.index !== '?' ? f.index : f.worktree).trim() || '?'
                return (
                  <div key={f.path} className={cx('list-item', file === f.path && 'active')} onClick={() => setFile(f.path)} style={{ cursor: 'pointer' }}>
                    <span className="mono" style={{ color: STATUS_COLOR[code] ?? 'var(--text-3)', fontWeight: 700, width: 12 }}>
                      {code === '?' ? 'U' : code}
                    </span>
                    <div className="grow" style={{ minWidth: 0 }}>
                      <div className="ellipsis">{f.path.split('/').pop()}</div>
                      <div className="faint ellipsis mono" style={{ fontSize: 10.5 }}>
                        {f.path.split('/').slice(0, -1).join('/')}
                      </div>
                    </div>
                    {(f.added > 0 || f.removed > 0) && (
                      <span className="mono" style={{ fontSize: 10.5 }}>
                        <span style={{ color: 'var(--green)' }}>+{f.added}</span> <span style={{ color: 'var(--red)' }}>−{f.removed}</span>
                      </span>
                    )}
                    <button
                      className="icon-btn sm danger"
                      title="Discard changes"
                      onClick={async (e) => {
                        e.stopPropagation()
                        if (await confirmDialog({ title: `Discard changes to ${f.path}?`, message: 'This cannot be undone.', confirm: 'Discard', danger: true }))
                          GitService.Discard(id!, f.path).then(st.reload).catch(fail('Discard failed'))
                      }}
                    >
                      <Undo2 />
                    </button>
                  </div>
                )
              })}
            </div>
          </div>
          <div className="pane-main" style={{ overflow: 'auto' }}>
            {file ? (
              <>
                <div className="toolbar" style={{ minHeight: 40 }}>
                  <span className="mono" style={{ fontSize: 12 }}>
                    {file}
                  </span>
                  <span className="spacer" />
                  <button className="btn sm ghost" onClick={() => SystemService.OpenInEditor(project.path + '/' + file, 1).catch(fail('Editor'))}>
                    <Code2 /> Open
                  </button>
                </div>
                <div style={{ paddingBottom: 30 }}>
                  <Diff text={diff} />
                </div>
              </>
            ) : (
              <Empty icon={<GitCommitHorizontal />} title="Select a file to see its diff" />
            )}
          </div>
        </div>
      )}

      {tab === 'history' && (
        <div style={{ overflow: 'auto', flex: 1 }}>
          {(s.commits ?? []).map((c) => (
            <div key={c.hash} className="check-row">
              <GitCommitHorizontal size={14} className="faint" />
              <span className="mono faint" style={{ fontSize: 11.5 }}>
                {c.hash}
              </span>
              <div className="t">
                <b style={{ fontWeight: 500 }}>{c.subject}</b>
              </div>
              <span className="faint" style={{ fontSize: 12 }}>
                {c.author}
              </span>
              <span className="faint" style={{ fontSize: 11.5, width: 80, textAlign: 'right' }}>
                {ago(c.date)}
              </span>
            </div>
          ))}
        </div>
      )}

      {tab === 'worktrees' && <Worktrees projectId={id!} status={s} reload={st.reload} />}
    </div>
  )
}

function Worktrees({ projectId, status, reload }: { projectId: string; status: GitStatus; reload: () => void }) {
  const list = status.worktrees ?? []
  return (
    <div className="view pad">
      <div className="page-head">
        <div className="grow">
          <h1>Worktrees</h1>
          <p>Parallel checkouts of this repo. Agents launched with “Worktree” get one each.</p>
        </div>
        <button
          className="btn primary"
          onClick={async () => {
            const name = await promptDialog({ title: 'New worktree', label: 'Task name', placeholder: 'checkout-redesign', confirm: 'Create' })
            if (!name) return
            try {
              const wt = await GitService.CreateWorktree({ projectId, name, copyEnv: true } as WorktreeRequest)
              toast('success', 'Worktree created', wt.path)
              reload()
            } catch (e) {
              toast('error', 'Could not create worktree', errMsg(e))
            }
          }}
        >
          <Plus /> New worktree
        </button>
      </div>
      <div className="card">
        {list.map((w) => (
          <div key={w.path} className="check-row">
            <FolderGit2 size={16} style={{ color: w.main ? 'var(--accent)' : 'var(--text-3)' }} />
            <div className="t">
              <b className="row" style={{ gap: 8 }}>
                <span className="mono">{w.branch || (w.detached ? `detached ${w.head}` : w.head)}</span>
                {w.main && <span className="badge accent">main checkout</span>}
                {w.managed && <span className="badge purple">larvyn</span>}
                {w.changes > 0 && <span className="badge yellow">{w.changes} changed</span>}
                {w.ahead > 0 && <span className="badge green">{w.ahead} commits ahead</span>}
              </b>
              <span className="mono">{w.path}</span>
            </div>
            <button className="icon-btn sm" title="Terminal here" onClick={() => newTerminal(projectId, { cwd: w.path, title: w.branch })}>
              <Plus />
            </button>
            <button className="icon-btn sm" title="Open in editor" onClick={() => SystemService.OpenInEditor(w.path, 1).catch(fail('Editor'))}>
              <Code2 />
            </button>
            {!w.main && (
              <>
                <button
                  className="icon-btn sm"
                  title="Merge into current branch"
                  onClick={async () => {
                    if (!(await confirmDialog({ title: `Merge ${w.branch}?`, message: 'Pending changes are committed first.', confirm: 'Merge' }))) return
                    GitService.MergeWorktree(projectId, w.path, '')
                      .then(() => (toast('success', 'Merged', w.branch), reload()))
                      .catch(fail('Merge failed'))
                  }}
                >
                  <GitMerge />
                </button>
                <button
                  className="icon-btn sm danger"
                  title="Remove worktree"
                  onClick={async () => {
                    if (!(await confirmDialog({ title: 'Remove this worktree?', message: `${w.path}${w.managed ? ` and branch ${w.branch}` : ''} will be deleted.`, confirm: 'Remove', danger: true }))) return
                    GitService.RemoveWorktree(projectId, w.path, w.managed).then(reload).catch(fail('Remove failed'))
                  }}
                >
                  <Trash2 />
                </button>
              </>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
