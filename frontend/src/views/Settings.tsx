import { useEffect, useState } from 'react'
import { ArrowUpCircle, Bug, Check, Cog, ExternalLink, FileText, FolderOpen, Info, Lightbulb, RefreshCw, Save, Sparkles, Wrench, X } from 'lucide-react'
import { checkForUpdate, installUpdate, useUpdate } from '../lib/update'
import { Brand } from '../components/Brand'
import { PluginsCard } from '../components/PluginsCard'
import { SystemService, type AppInfo, type Tool } from '../../bindings/larvyn/internal/system'
import { type Settings as S } from '../../bindings/larvyn/internal/store'
import { ProjectService } from '../../bindings/larvyn/internal/project'
import { AgentService, type AgentKind } from '../../bindings/larvyn/internal/agents'
import { toast, updateLaravelInstaller, useStore } from '../lib/store'
import { setFontSize } from '../lib/terms'
import { EDITORS, errMsg, isMac, mod } from '../lib/util'


export function Settings() {
  const settings = useStore((s) => s.settings)
  const [v, setV] = useState<S | null>(settings)
  const [tools, setTools] = useState<Tool[] | null>(null)
  const [agents, setAgents] = useState<AgentKind[]>([])
  const [home, setHome] = useState('')
  const [info, setInfo] = useState<AppInfo | null>(null)
  const update = useUpdate((s) => s.info)
  const checking = useUpdate((s) => s.checking)
  const installing = useUpdate((s) => s.installing)
  useEffect(() => {
    SystemService.Tools().then(setTools)
    AgentService.Kinds().then((k) => setAgents(k ?? []))
    SystemService.Home().then(setHome)
    SystemService.Info().then(setInfo)
  }, [])
  useEffect(() => setV(settings), [settings])
  if (!v) return null
  const set = (patch: Partial<S>) => setV({ ...v, ...patch } as S)
  const save = async () => {
    try {
      await SystemService.SaveSettings(v)
      useStore.setState({ settings: v })
      setFontSize(v.terminalFontSize)
      toast('success', 'Settings saved')
    } catch (e) {
      toast('error', 'Could not save', errMsg(e))
    }
  }
  const dirty = JSON.stringify(v) !== JSON.stringify(settings)

  return (
    <div className="view pad">
      <div style={{ maxWidth: 860 }}>
        <div className="page-head">
          <div className="grow">
            <h1>Settings</h1>
            <p>Stored in {home}</p>
          </div>
          <button className="btn primary" disabled={!dirty} onClick={save}>
            <Save /> Save
          </button>
        </div>

        <div className="card" style={{ marginBottom: 18 }}>
          <div className="card-head">
            <h3>
              <Cog /> Workbench
            </h3>
          </div>
          <div className="card-body" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
            <div className="field">
              <label>Editor</label>
              <select className="select" value={v.editor} onChange={(e) => set({ editor: e.target.value })}>
                {EDITORS.map((e) => (
                  <option key={e.id} value={e.id}>
                    {e.label}
                  </option>
                ))}
              </select>
              {v.editor === 'custom' && (
                <input className="input mono" placeholder="myeditor --goto {file}:{line}" value={v.editorCommand} onChange={(e) => set({ editorCommand: e.target.value })} />
              )}
              <span className="hint">Used for “Open” on routes, logs, models and migrations.</span>
            </div>
            <div className="field">
              <label>Default agent</label>
              <select className="select" value={v.defaultAgent} onChange={(e) => set({ defaultAgent: e.target.value })}>
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name} {a.installed ? '' : '(not installed)'}
                  </option>
                ))}
              </select>
            </div>
            <div className="field">
              <label>Agent permissions</label>
              <select className="select" value={v.agentPermissions === 'manual' ? 'manual' : 'yolo'} onChange={(e) => set({ agentPermissions: e.target.value })}>
                <option value="yolo">Yolo — skip approval prompts</option>
                <option value="manual">Manual — the agent asks before acting</option>
              </select>
              <span className="hint">Yolo starts agents with their skip-approvals flag (claude --dangerously-skip-permissions, codex --dangerously-bypass-approvals-and-sandbox…).</span>
            </div>
            <div className="field">
              <label>PHP binary</label>
              <input className="input mono" placeholder="php (from PATH)" value={v.phpBinary} onChange={(e) => set({ phpBinary: e.target.value })} />
              <span className="hint">Point to a specific version, e.g. a Herd PHP.</span>
            </div>
            <div className="field">
              <label>Shell</label>
              <input className="input mono" placeholder="Default (pwsh / $SHELL)" value={v.shell} onChange={(e) => set({ shell: e.target.value })} />
            </div>
            <div className="field">
              <label>Worktree folder</label>
              <div className="row">
                <input className="input mono grow" placeholder={`${home}/worktrees`} value={v.worktreeRoot} onChange={(e) => set({ worktreeRoot: e.target.value })} />
                <button
                  className="icon-btn"
                  onClick={() =>
                    ProjectService.PickFolder('Worktree folder').then((p) => {
                      if (p) set({ worktreeRoot: p })
                    })
                  }
                >
                  <FolderOpen />
                </button>
              </div>
            </div>
            <div className="field">
              <label>Terminal font size · {v.terminalFontSize}px</label>
              <input type="range" min={10} max={20} value={v.terminalFontSize} onChange={(e) => set({ terminalFontSize: Number(e.target.value) })} style={{ accentColor: 'var(--accent)' }} />
            </div>
            <div className="field">
              <label>Restore on start</label>
              <select className="select" value={v.noRestore ? 'off' : 'on'} onChange={(e) => set({ noRestore: e.target.value === 'off' })}>
                <option value="on">Restore terminals, agents and services</option>
                <option value="off">Start with nothing open</option>
              </select>
              <span className="hint">Shells come back with their output, agents resume their conversation and services that ran start again.</span>
            </div>
            {/* Flock is optional: its setting shows only once it is installed. */}
            {tools?.some((t) => t.id === 'flock' && t.path) && (
              <div className="field">
                <label>Flock</label>
                <select className="select" value={v.noFlockAutoServe ? 'off' : 'on'} onChange={(e) => set({ noFlockAutoServe: e.target.value === 'off' })}>
                  <option value="on">Serve projects I add at their .test domain</option>
                  <option value="off">Only when I ask</option>
                </select>
                <span className="hint">When Flock runs, Laravel apps you add to Larvyn are served at &lt;folder&gt;.test, and every agent's worktree at its own URL.</span>
              </div>
            )}
          </div>
        </div>

        <PluginsCard />

        <div className="card">
          <div className="card-head">
            <h3>
              <Wrench /> Toolchain
            </h3>
          </div>
          {!tools && [1, 2, 3, 4].map((i) => <div key={i} className="skeleton" style={{ height: 20, margin: 14 }} />)}
          {tools?.map((t) => (
            <div key={t.id} className="check-row">
              <div className={`ic ${t.path ? 'ok' : 'wait'}`}>{t.path ? <Check /> : <X />}</div>
              <div className="t">
                <b style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                  <Brand id={t.id} size={14} />
                  {t.name} {t.version && <span className="badge mono" style={{ marginLeft: 6 }}>{t.version}</span>}
                </b>
                <span className="mono">{t.path || t.hint || 'not found'}</span>
                {t.outdated && <span style={{ color: 'var(--yellow)', fontSize: 11.5 }}>{t.outdated}</span>}
              </div>
              {t.outdated && t.id === 'laravel' && (
                <button className="btn sm primary" onClick={updateLaravelInstaller}>
                  Update
                </button>
              )}
              {!t.path && t.id === 'composer' && (
                <button
                  className="btn sm primary"
                  onClick={() =>
                    SystemService.InstallComposer()
                      .then((sess) => {
                        useStore.setState((s) => ({ sessions: [...s.sessions, sess], drawer: { ...s.drawer, open: true, active: sess.id } }))
                        toast('info', 'Installing Composer', 'Reopen Settings to see it detected.')
                      })
                      .catch((e) => toast('error', 'Could not install Composer', errMsg(e)))
                  }
                >
                  Install
                </button>
              )}
              {!t.path && t.id !== 'composer' && t.hint?.includes('.') && !t.hint.includes(' ') && (
                <button className="btn sm ghost" onClick={() => SystemService.OpenURL('https://' + t.hint)}>
                  <ExternalLink /> Get it
                </button>
              )}
            </div>
          ))}
        </div>

        <div className="card" style={{ marginTop: 18 }}>
          <div className="card-head">
            <h3>
              <Info /> About &amp; feedback
            </h3>
          </div>
          <div className="card-body">
            <div className="kv" style={{ marginBottom: 14 }}>
              <div>Version</div>
              <div className="mono">
                {info?.version ?? '…'}
                {update?.available && <span className="badge blue" style={{ marginLeft: 8 }}>{update.latest} available</span>}
              </div>
              <div>System</div>
              <div className="mono">{info?.os ?? '…'}</div>
              <div>Log file</div>
              <div className="mono">{info?.logPath ?? '…'}</div>
            </div>
            <div className="row" style={{ gap: 8, flexWrap: 'wrap', marginBottom: 12 }}>
              {update?.available && update.canInstall ? (
                <button className="btn sm primary" disabled={installing} onClick={installUpdate}>
                  {installing ? <span className="spinner" /> : <ArrowUpCircle />} {installing ? 'Updating…' : `Update to ${update.latest}`}
                </button>
              ) : (
                <button className="btn sm" disabled={checking} onClick={() => checkForUpdate(true)}>
                  {checking ? <span className="spinner" /> : <RefreshCw />} Check for updates
                </button>
              )}
              {update?.available && (
                <button className="btn sm ghost" onClick={() => SystemService.OpenURL(update.notesUrl)}>
                  <ExternalLink /> What's new
                </button>
              )}
              <label className="row" style={{ gap: 6, fontSize: 12.5, color: 'var(--text-2)', marginLeft: 'auto' }}>
                <input
                  type="checkbox"
                  checked={!settings?.noUpdateCheck}
                  onChange={async (e) => {
                    const next = { ...settings!, noUpdateCheck: !e.target.checked } as S
                    try {
                      await SystemService.SaveSettings(next)
                      useStore.setState({ settings: next })
                      setV((cur) => (cur ? ({ ...cur, noUpdateCheck: next.noUpdateCheck } as S) : cur))
                    } catch (err) {
                      toast('error', 'Could not save', errMsg(err))
                    }
                  }}
                  style={{ accentColor: 'var(--accent)' }}
                />
                Check for updates automatically
              </label>
            </div>
            <div className="row" style={{ gap: 8, flexWrap: 'wrap' }}>
              <button className="btn sm" onClick={() => useStore.setState({ onboarding: true })}>
                <Sparkles /> Run the setup again
              </button>
              <button className="btn sm primary" onClick={() => SystemService.ReportBug('').catch((e) => toast('error', 'Could not open the browser', errMsg(e)))}>
                <Bug /> Report a bug
              </button>
              <button className="btn sm" onClick={() => SystemService.RequestFeature().catch((e) => toast('error', 'Could not open the browser', errMsg(e)))}>
                <Lightbulb /> Request a feature
              </button>
              <button className="btn sm ghost" onClick={() => SystemService.OpenLogFolder().catch((e) => toast('error', 'Could not open the folder', errMsg(e)))}>
                <FileText /> Open log folder
              </button>
            </div>
            <span style={{ display: 'block', marginTop: 10, fontSize: 11.5, color: 'var(--text-3)' }}>
              Reports are public issues on github.com/larvyn/releases. Your version and system are filled in; check the log for paths or project names before pasting it.
            </span>
            <span style={{ display: 'block', marginTop: 6, fontSize: 11.5, color: 'var(--text-3)' }}>
              Tool logos from thesvg.org and pi.dev, trademarks of their owners. PHP logo CC BY-SA 4.0, Git logo by Jason Long CC BY 3.0.
            </span>
          </div>
        </div>

        <div className="section-title">Keyboard</div>
        <div className="card">
          <div className="kv">
            {[
              ['Command palette', 'Ctrl K'],
              ['Switch view', 'Alt 1 … 9'],
              ['New terminal', 'Ctrl Shift T'],
              ['Toggle console', 'Ctrl J'],
              ['Toggle sidebar', 'Ctrl B'],
              ['Next / previous project', 'Ctrl Shift ] / ['],
              ['Run (Artisan, Tinker, SQL, agents)', 'Ctrl Enter'],
              ['Settings', 'Ctrl ,'],
            ].flatMap(([a, b]) => [
              <div key={a}>{a}</div>,
              <div key={a + 'k'}>
                {b.split(' ').map((k) => (k === 'Ctrl' ? mod : k === 'Alt' && isMac ? '⌥' : k === 'Shift' && isMac ? '⇧' : k)).map((k, i) => (
                  <kbd key={i} style={{ marginRight: 4 }}>
                    {k}
                  </kbd>
                ))}
              </div>,
            ])}
          </div>
        </div>
      </div>
    </div>
  )
}
