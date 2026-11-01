import { create } from 'zustand'
import { SystemService, type UpdateInfo } from '../../bindings/larvyn'
import { toast, useStore } from './store'
import { errMsg, load, save } from './util'

type UpdateState = {
  info: UpdateInfo | null
  checking: boolean
  installing: boolean
  /** Release the user chose "Later" for; its notice stays hidden. */
  dismissed: string
}

export const useUpdate = create<UpdateState>(() => ({
  info: null,
  checking: false,
  installing: false,
  dismissed: load('update.dismissed', ''),
}))

/** Looks for a newer release. manual (from Settings) reports every outcome. */
export async function checkForUpdate(manual = false) {
  if (useUpdate.getState().checking) return
  useUpdate.setState({ checking: true })
  try {
    const info = await SystemService.CheckForUpdate()
    useUpdate.setState({ info, ...(manual ? { dismissed: '' } : {}) })
    if (manual && !info.available) toast('success', 'Larvyn is up to date', info.reason || `You have ${info.current}, the newest release.`)
  } catch (e) {
    if (manual) toast('error', 'Could not check for updates', errMsg(e))
  } finally {
    useUpdate.setState({ checking: false })
  }
}

export function dismissUpdate() {
  const tag = useUpdate.getState().info?.latest ?? ''
  save('update.dismissed', tag)
  useUpdate.setState({ dismissed: tag })
}

export async function installUpdate() {
  const info = useUpdate.getState().info
  if (!info?.latest) return
  useUpdate.setState({ installing: true })
  try {
    // On success Larvyn restarts into the new version.
    await SystemService.InstallUpdate(info.latest)
  } catch (e) {
    useUpdate.setState({ installing: false })
    toast('error', 'Update failed', errMsg(e))
  }
}

/** Checks a few seconds after start and every 6 hours, unless turned off. */
export function startUpdateChecks() {
  const tick = () => {
    if (!useStore.getState().settings?.noUpdateCheck) checkForUpdate()
  }
  const first = setTimeout(tick, 5000)
  const every = setInterval(tick, 6 * 60 * 60 * 1000)
  return () => {
    clearTimeout(first)
    clearInterval(every)
  }
}
