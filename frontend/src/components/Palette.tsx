import { useEffect, useMemo, useState } from 'react'
import { Command } from 'cmdk'
import { openDiscussions, reportBug, requestFeature } from './Feedback'
import { checkForUpdate } from '../lib/update'
import {
  ArrowRight,
  Bot,
  Bug,
  Lightbulb,
  MessageCircle,
  CornerDownLeft,
  FlaskConical,
  FolderOpen,
  Globe,
  Package,
  PanelBottom,
  Play,
  Plus,
  Search,
  Square,
  SquareTerminal,
  Code2,
  Zap,
  RefreshCw,
  X,
} from 'lucide-react'
import { SystemService } from '../../bindings/larvyn/internal/system'
import { StackService, type StackItem } from '../../bindings/larvyn/internal/stack'
import {
  addProject,
  artisanRun,
  fail,
  handoff,
  newTerminal,
  runInConsole,
  selectProject,
  setView,
  showConsole,
  useActive,
  useStore,
} from '../lib/store'
import { NAV_ITEMS } from './Shell'
import { askCloseProject } from './closeProject'
import { initials, splitArgs } from '../lib/util'

const QUICK: { args: string[]; label: string; danger?: boolean }[] = [
  { args: ['optimize:clear'], label: 'Clear every cache (optimize:clear)' },
  { args: ['optimize'], label: 'Cache config, routes, events and views' },
  { args: ['migrate'], label: 'Run pending migrations' },
  { args: ['migrate:rollback'], label: 'Roll back the last batch', danger: true },
  { args: ['migrate:fresh', '--seed'], label: 'Drop all tables, migrate and seed', danger: true },
  { args: ['db:seed'], label: 'Seed the database' },
  { args: ['queue:restart'], label: 'Restart queue workers' },
  { args: ['queue:retry', 'all'], label: 'Retry all failed jobs' },
  { args: ['storage:link'], label: 'Link public/storage' },
  { args: ['key:generate'], label: 'Generate APP_KEY' },
  { args: ['route:clear'], label: 'Clear the route cache' },
  { args: ['config:clear'], label: 'Clear the config cache' },
  { args: ['view:clear'], label: 'Clear compiled views' },
  { args: ['about'], label: 'About this application' },
  { args: ['schedule:list'], label: 'List scheduled tasks' },
  { args: ['down'], label: 'Put the app in maintenance mode', danger: true },
  { args: ['up'], label: 'Bring the app out of maintenance mode' },
]

export function Palette() {
  const open = useStore((s) => s.paletteOpen)
  const initialQuery = useStore((s) => s.paletteQuery)
  const projects = useStore((s) => s.projects)
  const settings = useStore((s) => s.settings)
  const { id, project, info } = useActive()
  const [q, setQ] = useState('')
  const [stack, setStack] = useState<StackItem[]>([])

  useEffect(() => {
    if (open) {
      setQ(initialQuery)
      if (id) StackService.Items(id).then((x) => setStack(x ?? [])).catch(() => {})
    }
  }, [open])

  const close = () => useStore.setState({ paletteOpen: false })
  const act = (fn: () => void) => () => {
    close()
    fn()
  }

  const trimmed = q.trim()
  const artisanLine = trimmed.replace(/^(php\s+)?artisan\s+/, '').replace(/^>\s*/, '')
  const shellLine = trimmed.startsWith('$') ? trimmed.slice(1).trim() : ''
  // What the typed text most likely is: an explicit prefix wins, otherwise
  // guess from its shape (make:model …, User::count(), or plain words).
  const explicitArtisan = /^(>|(php\s+)?artisan\s)/.test(trimmed)
  const looksLikeCode = /::|->|\(|\$\w|;/.test(trimmed)
  const looksLikeCommand = /^[a-z][a-z-]*(:[a-z:-]+)?(\s+(-{1,2}[\w-]+(=\S+)?|\S+)){0,3}$/.test(trimmed) && !/\s(a|an|the|with|and|to|for|de|un|una|con|y|para|el|la)\s/i.test(' ' + trimmed + ' ')
  // Words that mean Larvyn's own help items: never guess artisan/agent for them,
  // so Enter opens the bug report instead of running `php artisan bug`.
  const isHelp = /^(feedback|(report\s+(a\s+)?)?bugs?|(request\s+(a\s+)?)?features?|ideas?|issues?|help|updates?|check\s+for\s+updates?)$/i.test(trimmed)
  const intent = isHelp ? { shell: false, artisan: false, tinker: false, agent: false } : {
    shell: !!shellLine,
    artisan: !shellLine && (explicitArtisan || (looksLikeCommand && !looksLikeCode)),
    tinker: !shellLine && !explicitArtisan && looksLikeCode,
    agent: !shellLine && !explicitArtisan && !looksLikeCode && !/^[a-z-]+:/.test(trimmed) && trimmed.length > 3 && (!looksLikeCommand || trimmed.includes(' ')),
  }

  const scripts = useMemo(() => {
    const out: { label: string; argv: string[] }[] = []
    for (const [k] of Object.entries(info?.composerScripts ?? {})) out.push({ label: `composer ${k}`, argv: ['composer', 'run', k] })
    for (const [k] of Object.entries(info?.npmScripts ?? {})) out.push({ label: `npm run ${k}`, argv: ['npm', 'run', k] })
    return out
  }, [info])

  if (!open) return null
  return (
    <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <Command className="palette" loop onKeyDown={(e) => e.key === 'Escape' && close()}>
        <div className="pal-search">
          <Search />
          <Command.Input value={q} onValueChange={setQ} autoFocus placeholder="Type a command, artisan line, or > / $ to run…" />
        </div>
        <Command.List>
          {!(id && trimmed) && <Command.Empty>Nothing found. Try “&gt; migrate” to run artisan.</Command.Empty>}

          {id && trimmed && !isHelp && (
            <Command.Group heading="Run" forceMount>
              {intent.agent && (
                <Command.Item forceMount value={'ask-agent ' + q} onSelect={act(() => handoff('agents', { agentPrompt: trimmed }))}>
                  <Bot />
                  <span>
                    Ask an agent: <b>{trimmed}</b>
                  </span>
                </Command.Item>
              )}
              {intent.artisan && (
                <Command.Item forceMount value={'run-artisan ' + q} onSelect={act(() => artisanRun(id, splitArgs(artisanLine)))}>
                  <Zap />
                  <span>
                    php artisan <b>{artisanLine}</b>
                  </span>
                  <span className="hint">
                    <CornerDownLeft size={12} />
                  </span>
                </Command.Item>
              )}
              {intent.shell && (
                <Command.Item forceMount value={'run-shell ' + q} onSelect={act(() => runInConsole(id, [], shellLine, undefined, shellLine))}>
                  <SquareTerminal />
                  <span>
                    $ <b>{shellLine}</b>
                  </span>
                </Command.Item>
              )}
              {intent.tinker && (
                <Command.Item forceMount value={'tinker ' + q} onSelect={act(() => handoff('tinker', { tinker: trimmed }))}>
                  <FlaskConical />
                  <span>
                    Evaluate in Tinker: <b className="mono">{trimmed}</b>
                  </span>
                </Command.Item>
              )}
            </Command.Group>
          )}

          {project && (
            <Command.Group heading="Go to">
              {NAV_ITEMS.map((n) => (
                <Command.Item key={n.view} value={'go ' + n.label} onSelect={act(() => setView(n.view))}>
                  <n.icon />
                  {n.label}
                  {n.hint && (
                    <span className="hint">
                      <kbd>Alt</kbd>
                      <kbd>{n.hint}</kbd>
                    </span>
                  )}
                </Command.Item>
              ))}
            </Command.Group>
          )}

          {project && (
            <Command.Group heading="Actions">
              <Command.Item value="new terminal" onSelect={act(() => newTerminal(id!))}>
                <SquareTerminal /> New terminal
                <span className="hint">
                  <kbd>Ctrl</kbd>
                  <kbd>Shift</kbd>
                  <kbd>T</kbd>
                </span>
              </Command.Item>
              <Command.Item value="new agent task" onSelect={act(() => setView('agents'))}>
                <Bot /> New agent task
              </Command.Item>
              <Command.Item value="toggle console" onSelect={act(() => showConsole())}>
                <PanelBottom /> Toggle console
              </Command.Item>
              <Command.Item value="open in editor" onSelect={act(() => SystemService.OpenInEditor(project.path, 1).catch(fail('Could not open editor')))}>
                <Code2 /> Open in {settings?.editor ?? 'editor'}
              </Command.Item>
              <Command.Item value="reveal folder explorer finder" onSelect={act(() => SystemService.OpenFolder(project.path))}>
                <FolderOpen /> Reveal in file manager
              </Command.Item>
              {info?.appUrl && (
                <Command.Item value="open site browser" onSelect={act(() => SystemService.OpenURL(info.herdUrl || info.appUrl))}>
                  <Globe /> Open {info.herdUrl || info.appUrl}
                </Command.Item>
              )}
              <Command.Item value="start stack all services" onSelect={act(() => StackService.StartAll(id!, []).catch(fail('Some services did not start')))}>
                <Play /> Start dev stack (server, Vite, queue)
              </Command.Item>
              <Command.Item value="stop stack all services" onSelect={act(() => StackService.StopAll(id!))}>
                <Square /> Stop all services
              </Command.Item>
              {stack.map((s) => (
                <Command.Item
                  key={s.key}
                  value={'service ' + s.label + ' ' + s.key}
                  disabled={!s.available && !s.session?.running}
                  onSelect={act(() => (s.session?.running ? StackService.Restart(id!, s.key) : StackService.Start(id!, s.key)).catch(fail(s.label)))}
                >
                  {s.session?.running ? <RefreshCw /> : <Play />}
                  {s.session?.running ? 'Restart' : 'Start'} {s.label}
                  <span className="hint">{s.description}</span>
                </Command.Item>
              ))}
            </Command.Group>
          )}

          <Command.Group heading="Help & feedback">
            <Command.Item value="report a bug issue problem crash feedback" onSelect={act(reportBug)}>
              <Bug /> Report a bug
              <span className="hint">opens GitHub with your version filled in</span>
            </Command.Item>
            <Command.Item value="request a feature idea suggestion feedback" onSelect={act(requestFeature)}>
              <Lightbulb /> Request a feature
            </Command.Item>
            <Command.Item value="ask question discussion idea feedback community" onSelect={act(openDiscussions)}>
              <MessageCircle /> Ask a question or share an idea
            </Command.Item>
            <Command.Item value="check for updates update new version help" onSelect={act(() => checkForUpdate(true))}>
              <RefreshCw /> Check for updates
            </Command.Item>
          </Command.Group>

          {project && info?.isLaravel && (
            <Command.Group heading="Artisan">
              {QUICK.map((c) => (
                <Command.Item key={c.args.join(' ')} value={'artisan ' + c.args.join(' ') + ' ' + c.label} onSelect={act(() => artisanRun(id!, c.args))}>
                  <Zap />
                  <span className="mono" style={{ color: 'var(--text)' }}>
                    {c.args.join(' ')}
                  </span>
                  <span className="hint" style={c.danger ? { color: 'var(--orange)' } : undefined}>
                    {c.label}
                  </span>
                </Command.Item>
              ))}
            </Command.Group>
          )}

          {project && scripts.length > 0 && (
            <Command.Group heading="Scripts">
              {scripts.map((s) => (
                <Command.Item key={s.label} value={'script ' + s.label} onSelect={act(() => runInConsole(id!, s.argv, s.label))}>
                  <Package /> {s.label}
                </Command.Item>
              ))}
            </Command.Group>
          )}

          <Command.Group heading="Projects">
            {projects.map((p) => (
              <Command.Item key={p.id} value={'project ' + p.name + ' ' + p.path} onSelect={act(() => selectProject(p.id))}>
                <span style={{ width: 18, height: 18, borderRadius: 5, background: p.color, color: '#fff', fontSize: 8.5, fontWeight: 800, display: 'grid', placeItems: 'center' }}>
                  {initials(p.name)}
                </span>
                {p.name}
                <span className="hint mono" style={{ fontSize: 10.5 }}>
                  {p.path}
                </span>
              </Command.Item>
            ))}
            {project && (
              <Command.Item value={'close project ' + project.name} onSelect={act(() => askCloseProject(project))}>
                <X /> Close {project.name}
              </Command.Item>
            )}
            <Command.Item value="open add project folder" onSelect={act(addProject)}>
              <FolderOpen /> Open a project folder…
            </Command.Item>
            <Command.Item value="create new laravel project" onSelect={act(() => useStore.setState({ newProjectOpen: true }))}>
              <Plus /> Create a new Laravel app…
            </Command.Item>
          </Command.Group>
        </Command.List>
        <div className="pal-foot">
          <span>
            <kbd>↑</kbd>
            <kbd>↓</kbd> navigate
          </span>
          <span>
            <kbd>↵</kbd> run
          </span>
          <span>
            <kbd>&gt;</kbd> artisan
          </span>
          <span>
            <kbd>$</kbd> shell
          </span>
          <span className="spacer" />
          <span>
            <ArrowRight size={12} /> Larvyn
          </span>
        </div>
      </Command>
    </div>
  )
}
