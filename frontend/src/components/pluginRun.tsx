import { PluginService, type PluginCommand } from '../../bindings/larvyn/internal/plugins'
import { StackService } from '../../bindings/larvyn/internal/stack'
import { type SessionInfo } from '../../bindings/larvyn/internal/terminal'
import { toast, useStore } from '../lib/store'
import { errMsg } from '../lib/util'
import { confirmDialog } from './ui'

/** Asks before the first run of a plugin's command line. Larvyn's Go side
 * refuses to run it until this was confirmed, and asks again if the plugin
 * changes the line. */
function askRun(pluginName: string, title: string, line: string) {
  return confirmDialog({
    title: `Run ${title}?`,
    message: (
      <div style={{ display: 'grid', gap: 8 }}>
        <div>The {pluginName} plugin wants to run this command for the first time:</div>
        <pre className="mono" style={{ margin: 0, padding: 8, background: 'var(--bg-3)', borderRadius: 6, whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
          {line}
        </pre>
        <div style={{ fontSize: 12 }}>Larvyn won't ask again unless the plugin changes it.</div>
      </div>
    ),
    confirm: 'Run',
  })
}

export function showSession(sess: SessionInfo) {
  useStore.setState((s) => ({
    sessions: s.sessions.some((x) => x.id === sess.id) ? s.sessions : [...s.sessions, sess],
    drawer: { ...s.drawer, open: true, active: sess.id },
  }))
}

/** Runs a plugin command from ⌘K in a project terminal. */
export async function runPluginCommand(projectId: string, c: PluginCommand) {
  if (c.needsConfirm && !(await askRun(c.pluginName, c.title, c.run))) return
  try {
    showSession(await PluginService.RunCommand(projectId, c.pluginId, c.id, c.run))
  } catch (e) {
    toast('error', `Could not run ${c.title}`, errMsg(e))
  }
}

const CONFIRM = 'plugin-confirm:'

/** Runs start; a command line that was never confirmed comes back with it
 * (a plugin's stack item, or a command or Artisan line a view asked for):
 * ask, record the answer, and run start again. Returns null if the user
 * said no. */
export async function withConfirm<T>(start: () => Promise<T>): Promise<T | null> {
  try {
    return await start()
  } catch (e) {
    const msg = errMsg(e)
    if (!msg.startsWith(CONFIRM)) throw e
    const req = JSON.parse(msg.slice(CONFIRM.length)) as { pluginId: string; pluginName: string; kind?: string; key: string; title: string; line: string }
    if (!(await askRun(req.pluginName, req.title, req.line))) return null
    await PluginService.ConfirmRun(req.pluginId, req.kind ?? 'stack', req.key, req.line)
    return await start()
  }
}

/** Starts a stack item, asking first for a plugin's unconfirmed one. */
export const startStackItem = (projectId: string, key: string) => withConfirm(() => StackService.Start(projectId, key))

/** Restarts a stack item. A plugin item whose command changed is confirmed
 * before the running one stops (the backend checks that first). */
export const restartStackItem = (projectId: string, key: string) => withConfirm(() => StackService.Restart(projectId, key))

/** Tells views that show plugin contributions (the sidebar stack) to reload
 * after a plugin was installed, turned on or off, or uninstalled. */
export const PLUGINS_CHANGED = 'larvyn:plugins-changed'
export const pluginsChanged = () => window.dispatchEvent(new Event(PLUGINS_CHANGED))

/** Starts or stops a stack item, asking first for a plugin's item that was
 * never run. It lives here, not in lib/store, to keep store free of UI imports. */
export async function toggleService(projectId: string, key: string, running: boolean) {
  try {
    if (running) await StackService.Stop(projectId, key)
    else await startStackItem(projectId, key)
  } catch (e) {
    toast('error', `Could not ${running ? 'stop' : 'start'} ${key}`, errMsg(e))
  }
}
