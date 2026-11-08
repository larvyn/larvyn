import { useEffect, useRef, useState } from 'react'
import { AlertTriangle, ArrowUpCircle, Download, ExternalLink, FileArchive, FolderOpen, FolderPlus, Puzzle, RefreshCw, ShieldAlert, ShieldCheck, Store, Trash2 } from 'lucide-react'
import { PluginService, type Listing, type Missing, type Permission, type Plugin } from '../../bindings/larvyn/internal/plugins'
import { ProjectService } from '../../bindings/larvyn/internal/project'
import { SystemService } from '../../bindings/larvyn/internal/system'
import { fail, toast, useStore } from '../lib/store'
import { cx, errMsg } from '../lib/util'
import { SearchInput, Toggle, confirmDialog, openConfirm } from './ui'
import { pluginsChanged } from './pluginRun'

/** A plugin's icon.svg, or the puzzle piece when it has none or it doesn't
 * render. Dimmed while the plugin is off. */
function PluginIcon({ icon, off, big }: { icon?: string; off?: boolean; big?: boolean }) {
  const [broken, setBroken] = useState('')
  return icon && broken !== icon ? (
    <img className={cx('plugin-icon', off && 'off', big && 'big')} src={icon} alt="" onError={() => setBroken(icon)} />
  ) : (
    <div className={cx('plugin-icon', 'none', off && 'off', big && 'big')}>
      <Puzzle />
    </div>
  )
}

/** "1 command", "2 commands". */
const plural = (n: number, one: string) => `${n} ${one}${n === 1 ? '' : 's'}`

/** What a plugin adds, e.g. "4 commands · 1 stack item". */
function summary(p: Plugin) {
  const c = p.manifest.contributes
  return [c.commands?.length && plural(c.commands.length, 'command'), c.stack?.length && plural(c.stack.length, 'stack item'), c.views?.length && plural(c.views.length, 'view')]
    .filter(Boolean)
    .join(' · ')
}

/** The list of what a plugin may do, sensitive permissions in red. */
function PermissionList({ p, all, market }: { p: Plugin; all: Permission[]; market: boolean }) {
  const asked = p.manifest.permissions ?? []
  const label = (name: string) => all.find((x) => x.name === name)
  // Reading secrets and reaching the network together can send them away.
  const leak = asked.includes('project.env.read') && asked.includes('network')
  return (
    <div style={{ display: 'grid', gap: 8 }}>
      <div className="row" style={{ gap: 6 }}>
        {p.official ? (
          <>
            <span className="badge green">
              <ShieldCheck /> By Larvyn
            </span>
            <span style={{ fontSize: 12 }}>Made and published by the Larvyn team.</span>
          </>
        ) : (
          <>
            <span className="badge yellow">
              <ShieldAlert /> {market ? 'Marketplace' : 'Unverified'}
            </span>
            <span style={{ fontSize: 12 }}>
              {market ? 'Checked by the marketplace, not code-reviewed' : 'Installed locally, not reviewed by Larvyn'}. Only install plugins you trust.
            </span>
          </>
        )}
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

/** The dialog that asks the user to approve a plugin's permissions. */
const approval = (p: Plugin, all: Permission[], action: string, market = p.source === 'marketplace') => ({
  title: `${action} ${p.manifest.name} ${p.manifest.version}?`,
  message: <PermissionList p={p} all={all} market={market} />,
  icon: <PluginIcon icon={p.icon} big />,
  confirm: `Allow and ${action.toLowerCase()}`,
  danger: true,
})

/** Asks the user to approve a plugin's permissions. */
const approve = (p: Plugin, all: Permission[], action: string, market?: boolean) => confirmDialog(approval(p, all, action, market))

/** Settings > Plugins. Installed: every installed plugin, wherever it came
 * from, with its toggles, updates, export and uninstall. Marketplace: the
 * plugins listed at github.com/larvyn/plugins. See #56. */
export function PluginsCard() {
  const project = useStore((s) => s.projects.find((p) => p.id === s.activeId))
  const [plugins, setPlugins] = useState<Plugin[] | null>(null)
  const [perms, setPerms] = useState<Permission[]>([])
  const [folder, setFolder] = useState('')
  const [tab, setTab] = useState<'installed' | 'market'>('installed')
  const [market, setMarket] = useState<Listing[] | null>(null)
  const [marketErr, setMarketErr] = useState('')
  const [q, setQ] = useState('')
  // The plugins being installed after the user approved them.
  const [busy, setBusy] = useState<string[]>([])

  /** Reads the installed plugins. */
  const load = () =>
    PluginService.List()
      .then((l) => {
        setPlugins(l ?? [])
        pluginsChanged()
      })
      .catch(fail('Could not list the plugins'))
  /** Reads the marketplace, which also applies its revocations. */
  const loadMarket = () =>
    PluginService.Marketplace()
      .then((l) => {
        setMarket(l ?? [])
        setMarketErr('')
      })
      .catch((e) => setMarketErr(errMsg(e)))
  // The installed list at once, then again after the marketplace answers:
  // it may turn off a revoked plugin.
  const reload = () => load().then(loadMarket).then(load)
  useEffect(() => {
    reload()
    PluginService.Permissions().then((l) => setPerms(l ?? []))
    PluginService.Folder().then(setFolder)
  }, [])
  // Opened to find a plugin the project needs (the Overview's Project card).
  const pluginSearch = useStore((s) => s.pluginSearch)
  const cardRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!pluginSearch) return
    cardRef.current?.scrollIntoView({ block: 'start' })
    setTab('market')
    setQ(pluginSearch)
    useStore.setState({ pluginSearch: '' })
  }, [pluginSearch])

  /** Checks a plugin, asks for its permissions, then installs it. The same
   * steps for a folder and a .zip (which Larvyn unpacks safely first). The
   * dialog opens at once and fills in when the plugin has been read: a
   * marketplace plugin is downloaded first. */
  const installFrom = async (
    src: string,
    inspect: (s: string) => Promise<Plugin>,
    installIt: (s: string, granted: string[]) => Promise<Plugin>,
    fromMarket = false,
    listing?: Listing,
  ) => {
    const action = (id?: string) => (plugins?.some((x) => x.manifest.id === id) ? 'Update' : 'Install')
    const dlg = openConfirm({
      title: listing ? `${action(listing.id)} ${listing.name} ${listing.version}?` : 'Install plugin?',
      icon: <PluginIcon icon={listing?.icon} big />,
      confirm: `Allow and ${action(listing?.id).toLowerCase()}`,
      danger: true,
      loading: true,
    })
    const inspected = inspect(src)
    inspected.then(
      (p) => dlg.update({ ...approval(p, perms, action(p.manifest.id), fromMarket), loading: false }),
      (e) => {
        dlg.cancel()
        toast('error', 'Not a plugin Larvyn can install', errMsg(e))
      },
    )
    // Confirm is enabled only once the plugin was read.
    if (!(await dlg.result)) return
    const p = await inspected
    const verb = action(p.manifest.id) === 'Update' ? 'updated' : 'installed'
    const id = p.manifest.id
    setBusy((b) => [...b, id])
    try {
      await installIt(src, p.manifest.permissions ?? [])
      toast('success', `${p.manifest.name} ${verb}`)
    } catch (e) {
      toast('error', 'Could not install the plugin', errMsg(e))
    }
    setBusy((b) => b.filter((x) => x !== id))
    reload()
  }

  /** Downloads a marketplace plugin, checks it against the index, then asks
   * for its permissions like any other install. */
  const installListing = (l: Listing) =>
    installFrom(
      l.id,
      (id) => PluginService.InspectListing(id, l.version),
      (id, granted) => PluginService.InstallListing(id, l.version, granted),
      true,
      l,
    )

  /** Installs a plugin from a folder the user picks. */
  const install = async () => {
    const dir = await ProjectService.PickFolder('Choose a plugin folder').catch(() => '')
    if (dir) await installFrom(dir, PluginService.Inspect, PluginService.Install)
  }

  /** Installs a plugin from a .zip the user picks. */
  const installZip = async () => {
    const zip = await PluginService.PickZip().catch(() => '')
    if (zip) await installFrom(zip, PluginService.InspectZip, PluginService.InstallZip)
  }

  /** Turns a plugin off, or on after the user approves its permissions. */
  const toggle = async (p: Plugin, on: boolean) => {
    try {
      if (!on) await PluginService.Disable(p.manifest.id)
      else if (await approve(p, perms, 'Enable')) await PluginService.Enable(p.manifest.id, p.manifest.permissions ?? [])
    } catch (e) {
      toast('error', 'Could not change the plugin', errMsg(e))
    }
    load()
  }

  /** Turns a plugin on or off in the open project only. */
  const toggleHere = async (p: Plugin, on: boolean) => {
    if (!project) return
    await PluginService.SetOnInProject(p.manifest.id, project.id, on).catch(fail('Could not change the plugin'))
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

  const updates = (market ?? []).filter((l) => l.update)
  /** The marketplace update for an installed plugin, if there is one. */
  const updateFor = (p: Plugin) => updates.find((l) => l.id === p.manifest.id)

  /** Uninstalls a plugin after the user confirms. */
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
    reload()
  }

  return (
    <div className="card" style={{ marginBottom: 18 }} ref={cardRef}>
      <div className="card-head">
        <h3>
          <Puzzle /> Plugins
        </h3>
        <span className="badge">preview</span>
        <div className="tabs">
          <button className={cx('tab', tab === 'installed' && 'active')} onClick={() => setTab('installed')}>
            Installed
            {updates.length > 0 && <span className="badge blue">{updates.length}</span>}
          </button>
          <button className={cx('tab', tab === 'market' && 'active')} onClick={() => setTab('market')}>
            <Store /> Marketplace
          </button>
        </div>
        <span className="spacer" />
        <button className="btn sm ghost" title="Reload" onClick={reload}>
          <RefreshCw />
        </button>
        {tab === 'installed' ? (
          <>
            <button className="btn sm ghost" onClick={() => SystemService.OpenFolder(folder).catch(fail('Could not open the folder'))}>
              <FolderOpen /> Open folder
            </button>
            <button className="btn sm" onClick={installZip} title="Install a plugin someone shared as a .zip">
              <FileArchive /> Install .zip
            </button>
            <button className="btn sm" onClick={install}>
              <FolderPlus /> Install from folder
            </button>
          </>
        ) : (
          <SearchInput value={q} onChange={setQ} placeholder="Search plugins…" width={200} />
        )}
      </div>
      {tab === 'market' ? marketList() : installedList()}
    </div>
  )

  /** The Marketplace tab. */
  function marketList() {
    if (marketErr)
      return (
        <div className="check-row">
          <div className="t">
            <b>Could not load the marketplace</b>
            <span>{marketErr}</span>
          </div>
        </div>
      )
    if (!market) return <div className="skeleton" style={{ height: 20, margin: 14 }} />
    const needle = q.trim().toLowerCase()
    const shown = market.filter((l) => !needle || [l.name, l.id, l.description, l.author].some((x) => x?.toLowerCase().includes(needle)))
    if (shown.length === 0)
      return (
        <div className="check-row">
          <div className="t">
            <b>No plugins found</b>
          </div>
        </div>
      )
    return shown.map((l) => (
      <div key={l.id} className="check-row">
        <PluginIcon icon={l.icon} />
        <div className="t">
          <b style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            {l.name}
            <span className="badge mono">{l.version}</span>
            {l.author && <span style={{ fontWeight: 400, fontSize: 11, color: 'var(--text-3)' }}>{l.author}</span>}
          </b>
          {l.error ? <span style={{ color: 'var(--yellow)' }}>{l.error}</span> : <span>{l.description}</span>}
        </div>
        <button className="icon-btn" title="Source code" onClick={() => SystemService.OpenURL(l.repo)}>
          <ExternalLink />
        </button>
        {busy.includes(l.id) ? (
          <button className="btn sm ghost" disabled>
            <span className="spinner" /> {l.installed ? 'Updating…' : 'Installing…'}
          </button>
        ) : l.update ? (
          <button className="btn sm primary" disabled={!!l.error} title={l.error} onClick={() => installListing(l)}>
            <ArrowUpCircle /> Update
          </button>
        ) : l.installed ? (
          <button className="btn sm ghost" disabled>
            Installed
          </button>
        ) : (
          <button className="btn sm primary" disabled={!!l.error} onClick={() => installListing(l)}>
            <Download /> Install
          </button>
        )}
      </div>
    ))
  }

  /** The Installed tab. */
  function installedList() {
    return (
      <>
      {!plugins && <div className="skeleton" style={{ height: 20, margin: 14 }} />}
      {plugins?.length === 0 && (
        <div className="check-row">
          <div className="t">
            <b>No plugins yet</b>
            <span>
              Find one in the{' '}
              <a href="#" onClick={(e) => (e.preventDefault(), setTab('market'))}>
                Marketplace
              </a>
              .
            </span>
          </div>
        </div>
      )}
      {plugins?.map((p) => (
        <div key={p.dir} className="check-row">
          <PluginIcon icon={p.icon} off={!p.enabled} />
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
            {p.revoked ? (
              <span style={{ color: 'var(--red)' }}>Revoked: {p.revoked}</span>
            ) : p.error ? (
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
          {busy.includes(p.manifest.id) ? (
            <button className="btn sm ghost" disabled>
              <span className="spinner" /> Updating…
            </button>
          ) : updateFor(p) && (
            <button className="btn sm primary" disabled={!!updateFor(p)!.error} title={updateFor(p)!.error} onClick={() => installListing(updateFor(p)!)}>
              <ArrowUpCircle /> Update to {updateFor(p)!.version}
            </button>
          )}
          {p.enabled && project && (
            <label className="row" style={{ gap: 6, fontSize: 12, color: 'var(--text-3)' }}>
              {project.name}
              <Toggle
                on={!p.offIn?.includes(project.id)}
                title={p.offIn?.includes(project.id) ? `Turn on in ${project.name}` : `Turn off in ${project.name} only`}
                onChange={(on) => toggleHere(p, on)}
              />
            </label>
          )}
          <Toggle on={p.enabled} disabled={!!p.error || !!p.revoked} title={p.revoked ? 'Revoked' : p.error ? 'Fix the error to turn it on' : p.enabled ? 'Turn off everywhere' : 'Turn on'} onChange={(on) => toggle(p, on)} />
          <button className="icon-btn" title="Export as .zip, to share it" onClick={() => exportZip(p)}>
            <FileArchive />
          </button>
          <button className="icon-btn" title="Uninstall" onClick={() => uninstall(p)}>
            <Trash2 />
          </button>
        </div>
      ))}
      <div style={{ padding: '8px 14px', fontSize: 11.5, color: 'var(--text-3)' }}>
        Plugins load from <span className="mono">{folder}</span>. Their commands are in ⌘K under Plugins, their stack items in the sidebar's Stack, and their views in the
        sidebar under Plugins.
      </div>
      </>
    )
  }
}
