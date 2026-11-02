import type { Project } from '../../bindings/larvyn/internal/store'
import { closeProject, useStore } from '../lib/store'
import { confirmDialog } from './ui'

/** Closes a project, asking first when it still has something running. */
export async function askCloseProject(p: Project) {
  const running = useStore.getState().sessions.filter((s) => s.projectId === p.id && s.running).length
  if (
    running > 0 &&
    !(await confirmDialog({
      title: `Close ${p.name}?`,
      message: `This stops ${running} running ${running === 1 ? 'terminal or service' : 'terminals and services'}. The project stays in Larvyn.`,
      confirm: 'Close project',
      danger: true,
    }))
  )
    return
  await closeProject(p.id)
}
