import { useEffect, useState } from 'react'
import { AlertTriangle, Download, FileArchive, FolderOpen, FolderPlus, Puzzle, RefreshCw, ShieldAlert, Trash2 } from 'lucide-react'
import { PluginService, type Missing, type Permission, type Plugin } from '../../bindings/larvyn/internal/plugins'
import { ProjectService } from '../../bindings/larvyn/internal/project'
import { SystemService } from '../../bindings/larvyn/internal/system'
import { fail, toast, useStore } from '../lib/store'
import { errMsg } from '../lib/util'
import { Toggle, confirmDialog } from './ui'
import { pluginsChanged } from './pluginRun'

const plural = (n: number, one: string) => `${n} ${one}${n === 1 ? '' : 's'}`

/** What a plugin adds, e.g. "4 commands · 1 stack item". */
function summary(p: Plugin) {
  const c = p.manifest.contributes
  return [c.commands?.length && plural(c.commands.length, 'command'), c.stack?.length && plural(c.stack.length, 'stack item'), c.views?.length && plural(c.views.length, 'view')]
    .filter(Boolean)
    .join(' · ')
}

/** The list of what a plugin may do, sensitive permissions in red. */
function PermissionList({ p, all }: { p: Plugin; all: Permission[] }) {
  const asked = p.manifest.permissions ?? []
  const label = (name: string) => all.find((x) => x.name === name)
  // Reading secrets and reaching the network together can send them away.
  const leak = asked.includes('project.env.read') && asked.includes('network')
  return (
    <div style={{ display: 'grid', gap: 8 }}>
      <div className="row" style={{ gap: 6 }}>
        <span className="badge yellow">
          <ShieldAlert /> Unverified
        </span>
        <span style={{ fontSize: 12 }}>Installed from a folder, not reviewed by Larvyn. Only install plugins you trust.</span>
      </div>
      {asked.length === 0 ? (
        <div>It asks for no permissions.</div>
      ) : (
        <>
          <div>It will be able to:</div>
          <ul style={{ margin: 0, paddingLeft: 18 }}>
            {asked.map((name) => {
              const perm = label(name)
              return (
                <li key={name} style={{ color: perm?.sensitive ? 'var(--red)' : undefined }}>
                  {perm?.label ?? name}
                  {name === 'network' && p.manifest.network?.length ? <span className="mono"> ({p.manifest.network.join(', ')})</span> : null}
                  {name === 'project.env.keys' && p.manifest.env?.length ? <span className="mono">: {p.manifest.env.join(', ')}</span> : null}
                </li>
              )
            })}
          </ul>
        </>
      )}
      {(p.missing?.length ?? 0) > 0 && (
        <div style={{ display: 'grid', gap: 4, color: 'var(--yellow)' }}>
          <div>It needs programs that are not installed:</div>
          {p.missing!.map((m) => (
            <div key={m.program} style={{ paddingLeft: 18 }}>
              <span className="mono">{m.program}</span>
              {m.install ? (
                <>
                  {' '}
                  — <span className="mono">{m.install}</span>
                </>
              ) : (
                m.url && <> — see {m.url}</>
              )}
            </div>
          ))}
          <div style={{ fontSize: 12, color: 'var(--text-3)' }}>You can install them from the plugin's row afterwards.</div>
        </div>
      )}
      {leak && (
        <div className="row" style={{ gap: 6, color: 'var(--red)' }}>
          <AlertTriangle style={{ width: 14, height: 14, flexShrink: 0 }} />
          It can read your .env secrets and connect to the internet, so it could send those secrets elsewhere.
        </div>
      )}
    </div>
  )
}

/** Asks the user to approve a plugin's permissions. */
const approve = (p: Plugin, all: Permission[], action: string) =>
  confirmDialog({
    title: `${action} ${p.manifest.name} ${p.manifest.version}?`,
    message: <PermissionList p={p} all={all} />,
    confirm: `Allow and ${action.toLowerCase()}`,
    danger: true,
  })

/** Settings > Plugins: the installed plugins, installing one from a folder,
 * turning plugins on and off, and uninstalling them. See #56. */
export function PluginsCard() {
  const [plugins, setPlugins] = useState<Plugin[] | null>(null)
  const [perms, setPerms] = useState<Permission[]>([])
  const [folder, setFolder] = useState('')

  const load = () =>
    PluginService.List()
      .then((l) => {
        setPlugins(l ?? [])
        pluginsChanged()
      })
      .catch(fail('Could not list the plugins'))
  useEffect(() => {
    load()
    PluginService.Permissions().then((l) => setPerms(l ?? []))
    PluginService.Folder().then(setFolder)
  }, [])

  const install = async () => {
    const dir = await ProjectService.PickFolder('Choose a plugin folder').catch(() => '')
    if (!dir) return
    let p: Plugin
    try {
      p = await PluginService.Inspect(dir)
    } catch (e) {
      toast('error', 'Not a plugin Larvyn can install', errMsg(e))
      return
    }
    const existing = plugins?.find((x) => x.manifest.id === p.manifest.id)
    if (!(await approve(p, perms, existing ? 'Update' : 'Install'))) return
    try {
      await PluginService.Install(dir, p.manifest.permissions ?? [])
      toast('success', `${p.manifest.name} installed`)
    } catch (e) {
      toast('error', 'Could not install the plugin', errMsg(e))
    }
    load()
  }

  const toggle = async (p: Plugin, on: boolean) => {
    try {
      if (!on) await PluginService.Disable(p.manifest.id)
      else if (await approve(p, perms, 'Enable')) await PluginService.Enable(p.manifest.id, p.manifest.permissions ?? [])
    } catch (e) {
      toast('error', 'Could not change the plugin', errMsg(e))
    }
    load()
  }

  /** Runs a missing program's install command in a terminal, after showing
   * the exact command: it comes from the plugin, so the user decides. */
  const installProgram = async (p: Plugin, m: Missing) => {
    if (!m.install) {
      if (m.url) SystemService.OpenURL(m.url)
      return
    }
    const ok = await confirmDialog({
      title: `Install ${m.program}?`,
      message: (
        <div style={{ display: 'grid', gap: 8 }}>
          <div>{p.manifest.name} suggests this command. Larvyn runs it in a terminal you can watch:</div>
          <pre className="mono" style={{ margin: 0, padding: 8, background: 'var(--bg-3)', borderRadius: 6, whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
            {m.install}
          </pre>
          <div style={{ fontSize: 12 }}>Only run it if you trust this plugin.{m.url && <> Instructions: {m.url}</>}</div>
        </div>
      ),
      confirm: 'Run in terminal',
    })
    if (!ok) return
    try {
      const sess = await PluginService.InstallRequirement(p.manifest.id, m.program, m.install)
      useStore.setState((s) => ({ sessions: [...s.sessions, sess], drawer: { ...s.drawer, open: true, active: sess.id } }))
    } catch (e) {
      toast('error', `Could not install ${m.program}`, errMsg(e))
    }
  }

  /** Saves the plugin as a .zip, ready to share or attach to a release. */
  const exportZip = async (p: Plugin) => {
    try {
      const path = await PluginService.Export(p.folder)
      if (path) toast('success', `${p.manifest.name || p.manifest.id} exported`, path)
    } catch (e) {
      toast('error', 'Could not export the plugin', errMsg(e))
    }
  }

  const uninstall = async (p: Plugin) => {
    const ok = await confirmDialog({
      title: `Uninstall ${p.manifest.name || p.manifest.id}?`,
      message: p.linked
        ? 'This plugin is a link to a folder elsewhere. Larvyn removes the link; the folder it points to is not touched.'
        : 'Larvyn deletes the plugin from its plugins folder.',
      confirm: 'Uninstall',
      danger: true,
    })
    if (!ok) return
    await PluginService.Uninstall(p.folder).catch(fail('Could not uninstall the plugin'))
    load()
  }

  return (
    <div className="card" style={{ marginBottom: 18 }}>
      <div className="card-head">
        <h3>
          <Puzzle /> Plugins
        </h3>
        <span className="badge">preview</span>
        <span className="spacer" />
        <button className="btn sm ghost" title="Reload the plugins folder" onClick={load}>
          <RefreshCw />
        </button>
        <button className="btn sm ghost" onClick={() => SystemService.OpenFolder(folder).catch(fail('Could not open the folder'))}>
          <FolderOpen /> Open folder
        </button>
        <button className="btn sm primary" onClick={install}>
          <FolderPlus /> Install from folder
        </button>
      </div>
      {!plugins && <div className="skeleton" style={{ height: 20, margin: 14 }} />}
      {plugins?.length === 0 && (
        <div className="check-row">
          <div className="t">
            <b>No plugins yet</b>
            <span>Install one from a folder, e.g. examples/plugins in the Larvyn repository. Plugins can add commands, stack items and sidebar views.</span>
          </div>
        </div>
      )}
      {plugins?.map((p) => (
        <div key={p.dir} className="check-row">
          <div className={`ic ${p.enabled ? 'ok' : p.error || p.needsApproval ? 'bad' : 'wait'}`}>
            <Puzzle />
          </div>
          <div className="t">
            <b style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              {p.manifest.name || p.manifest.id}
              {p.manifest.version && <span className="badge mono">{p.manifest.version}</span>}
              <span
                className="mono"
                style={{
                  fontWeight: 400,
                  fontSize: 11,
                  color: 'var(--text-3)',
                }}
              >
                {p.manifest.id}
              </span>
              {p.linked && <span className="badge blue">linked</span>}
            </b>
            {p.error ? (
              <span style={{ color: 'var(--yellow)' }}>{p.error}</span>
            ) : p.needsApproval ? (
              <span style={{ color: 'var(--yellow)' }}>Off: it now asks for more permissions. Turn it on to review them.</span>
            ) : (
              <span>{[p.manifest.description, summary(p)].filter(Boolean).join(' — ')}</span>
            )}
            {!p.error &&
              p.missing?.map((m) => (
                <span key={m.program} className="row" style={{ gap: 6, color: 'var(--yellow)', marginTop: 4 }}>
                  Needs <span className="mono">{m.program}</span>, not found on PATH.
                  <button className="btn sm" onClick={() => installProgram(p, m)}>
                    <Download /> {m.install ? 'Install' : 'How to install'}
                  </button>
                </span>
              ))}
          </div>
          <Toggle on={p.enabled} disabled={!!p.error} title={p.error ? 'Fix the error to turn it on' : p.enabled ? 'Turn off' : 'Turn on'} onChange={(on) => toggle(p, on)} />
          <button className="icon-btn" title="Export as .zip, to share it" onClick={() => exportZip(p)}>
            <FileArchive />
          </button>
          <button className="icon-btn" title="Uninstall" onClick={() => uninstall(p)}>
            <Trash2 />
          </button>
        </div>
      ))}
      <div style={{ padding: '8px 14px', fontSize: 11.5, color: 'var(--text-3)' }}>
        Plugins load from <span className="mono">{folder}</span>. Commands, stack items and views from plugins arrive in the next Larvyn updates; see the plugin plan on GitHub
        (#56).
      </div>
    </div>
  )
}
