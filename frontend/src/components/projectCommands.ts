import { useEffect, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { ProjectService, type ProjectCommand, type ProjectFileInfo } from '../../bindings/larvyn/internal/project'
import { setView, toast, useStore } from '../lib/store'
import { errMsg } from '../lib/util'
import { promptDialog } from './ui'
import { showSession } from './pluginRun'

/** The project's commands and its .larvyn/project.json, read again when the
 * file changes on disk (git pull, branch switch) or Larvyn edits it. */
export function useProjectFile(id: string | null | undefined, enabled = true) {
  const [file, setFile] = useState<ProjectFileInfo | null>(null)
  const [commands, setCommands] = useState<ProjectCommand[]>([])
  const [rev, setRev] = useState(0)
  // Nothing of another project may show, or run, while its own load.
  useEffect(() => {
    setFile(null)
    setCommands([])
  }, [id])
  useEffect(() => {
    if (!id || !enabled) return
    return Events.On('projectfile:changed', (ev: any) => ev.data === id && setRev((r) => r + 1))
  }, [id, enabled])
  useEffect(() => {
    if (!id || !enabled) return
    let current = true
    ProjectService.File(id).then((f) => current && setFile(f)).catch(() => {})
    ProjectService.Commands(id).then((c) => current && setCommands(c ?? [])).catch(() => {})
    return () => {
      current = false
    }
  }, [id, enabled, rev])
  return { file, commands, reload: () => setRev((r) => r + 1) }
}

/** Runs a project command in the console. */
export async function runProjectCommand(id: string, c: ProjectCommand) {
  try {
    showSession(await ProjectService.RunCommand(id, c.id))
  } catch (e) {
    toast('error', `Could not run ${c.title}`, errMsg(e))
  }
}

/** Asks for a command's title and command line, then saves it: a new one
 * is the user's own until they share it. */
export async function editProjectCommand(id: string, c?: ProjectCommand) {
  const title = await promptDialog({ title: c ? 'Edit command' : 'New command', label: 'Title', value: c?.title, placeholder: 'Fresh database' })
  if (!title?.trim()) return
  const run = await promptDialog({ title: c ? 'Edit command' : 'New command', label: 'Command line', value: c?.run, placeholder: 'php artisan migrate:fresh --seed', confirm: 'Save' })
  if (!run?.trim()) return
  try {
    await ProjectService.SaveCommand(id, { id: c?.id ?? '', title, run, shared: c?.shared ?? false })
    toast('success', c ? 'Command saved' : 'Command added', c?.shared ? 'In .larvyn/project.json: commit it to share it' : 'Only you see it until you share it')
  } catch (e) {
    toast('error', 'Could not save the command', errMsg(e))
  }
}

/** Moves a command between the user's own and the team's file. */
export async function moveProjectCommand(id: string, c: ProjectCommand) {
  try {
    await ProjectService.MoveCommand(id, c.id, !c.shared)
    toast('success', c.shared ? `${c.title} is now only yours` : `${c.title} moved to .larvyn/project.json`, c.shared ? undefined : 'Commit it to share it with the team')
  } catch (e) {
    toast('error', 'Could not move the command', errMsg(e))
  }
}

export async function deleteProjectCommand(id: string, c: ProjectCommand) {
  try {
    await ProjectService.DeleteCommand(id, c.id, c.shared)
  } catch (e) {
    toast('error', 'Could not delete the command', errMsg(e))
  }
}

/** Opens Settings → Plugins → Marketplace on a plugin the project needs. */
export function findPlugin(pluginId: string) {
  useStore.setState({ pluginSearch: pluginId })
  setView('settings')
}
