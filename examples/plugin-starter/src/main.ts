import { call, inLarvyn } from './larvyn'

const $ = (id: string) => document.getElementById(id)!

async function main() {
  if (!inLarvyn) $('mode').textContent = 'dev'

  // Needs "project.read" in larvyn.json.
  const project = await call('project.info')
  $('title').textContent = `Hello, ${project.name}!`
  $('name').textContent = project.name
  $('path').textContent = project.path

  // Needs "storage": counts how often this view was opened. The value is
  // kept by Larvyn, outside the plugin folder, so updates keep it.
  const opened = (Number(await call('storage.get', { key: 'opened' })) || 0) + 1
  await call('storage.set', { key: 'opened', value: opened })
  $('count').textContent = opened === 1 ? 'for the first time' : `${opened} times`

  // Needs "ui.notify".
  $('wave').addEventListener('click', () => {
    call('ui.notify', { message: `Hello from ${project.name}` }).catch(showError)
  })
}

function showError(err: unknown) {
  $('title').textContent = err instanceof Error ? err.message : String(err)
}

main().catch(showError)
