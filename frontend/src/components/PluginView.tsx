import { useEffect, useRef, useState } from 'react'
import { Puzzle } from 'lucide-react'
import { PluginService, type PluginView as View } from '../../bindings/larvyn/internal/plugins'
import { setView, toast, useStore } from '../lib/store'
import { errMsg } from '../lib/util'
import { Empty } from './ui'
import { PLUGINS_CHANGED, showSession, withConfirm } from './pluginRun'

/** The views of the plugins that are on in the open project, kept fresh
 * when plugins or the project change. */
export function usePluginViews() {
  const projectId = useStore((s) => s.activeId)
  // null until the first load: a view must not say it is unavailable while
  // the list is still on its way.
  const [views, setViews] = useState<View[] | null>(null)
  useEffect(() => {
    // Only the latest request may set the list: an older one finishing last
    // would bring back views from before a plugin changed.
    let seq = 0
    const load = () => {
      const mine = ++seq
      PluginService.Views(projectId ?? '')
        .then((v) => mine === seq && setViews(v ?? []))
        .catch(() => mine === seq && setViews([]))
    }
    load()
    window.addEventListener(PLUGINS_CHANGED, load)
    return () => {
      seq++
      window.removeEventListener(PLUGINS_CHANGED, load)
    }
  }, [projectId])
  return views
}

export const viewKey = (v: View) => `plugin:${v.pluginId}:${v.id}` as const

/** A plugin's view: its page in a sandboxed iframe (scripts only, so it has
 * no access to Larvyn's page, storage or Go bindings), and the other end of
 * its window.larvyn. A call is trusted to come from this plugin only because
 * it comes from this iframe; Larvyn's Go side then checks the plugin's
 * permissions. */
export function PluginView({ view }: { view: string }) {
  const views = usePluginViews()
  const projectId = useStore((s) => s.activeId)
  const frame = useRef<HTMLIFrameElement>(null)
  const v = views?.find((x) => viewKey(x) === view)

  useEffect(() => {
    if (!v) return
    const onMessage = async (e: MessageEvent) => {
      const win = frame.current?.contentWindow
      if (!win || e.source !== win) return
      const m = e.data
      if (!m || m.larvyn !== 1 || typeof m.id !== 'number' || typeof m.method !== 'string') return
      const params = m.params && typeof m.params === 'object' && !Array.isArray(m.params) ? m.params : {}
      try {
        // Running a command line for the first time asks the user first.
        const r = await withConfirm(() => PluginService.Call(v.pluginId, projectId ?? '', m.method, params))
        if (!r) throw new Error('the user did not allow this command')
        if (r.notify) toast('info', v.pluginName, r.notify)
        if (r.session) showSession(r.session)
        win.postMessage({ larvyn: 1, id: m.id, result: r.result ?? null }, '*')
      } catch (err) {
        win.postMessage({ larvyn: 1, id: m.id, error: errMsg(err) }, '*')
      }
    }
    window.addEventListener('message', onMessage)
    return () => window.removeEventListener('message', onMessage)
  }, [v?.pluginId, v?.id, projectId])

  if (!views) return <div className="view" />
  if (!v)
    return (
      <div className="view pad">
        <Empty
          icon={<Puzzle />}
          title="This plugin view is not available"
          action={
            <button className="btn" onClick={() => setView('settings')}>
              Open Settings → Plugins
            </button>
          }
        >
          The plugin may be off or uninstalled.
        </Empty>
      </div>
    )

  return (
    <div className="view" style={{ padding: 0, display: 'flex' }}>
      <iframe
        key={projectId ?? ''}
        ref={frame}
        title={`${v.pluginName}: ${v.title}`}
        src={v.url}
        sandbox="allow-scripts"
        referrerPolicy="no-referrer"
        style={{ flex: 1, width: '100%', height: '100%', border: 0, background: 'transparent' }}
      />
    </div>
  )
}
