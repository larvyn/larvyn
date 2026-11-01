import type { Project } from '../../bindings/larvyn'
import { ProjectService } from '../../bindings/larvyn'
import { fail } from '../lib/store'
import { confirmDialog } from './ui'

/** Removes a project from Larvyn after asking; its files are not touched. */
export async function askRemoveProject(p: Project) {
  const ok = await confirmDialog({
    title: `Remove ${p.name}?`,
    message: 'Larvyn forgets the project and stops its terminals. Your files are not touched.',
    confirm: 'Remove',
    danger: true,
  })
  if (ok) await ProjectService.Remove(p.id).catch(fail('Could not remove the project'))
}
