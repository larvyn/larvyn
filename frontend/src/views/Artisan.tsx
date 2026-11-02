import { useEffect, useMemo, useState } from 'react'
import { Copy, Play, RefreshCw, Star, Terminal, Zap, Box, FileCode2, Database, Workflow, Mail, Shield, TestTube2, Layers, Radio, Bell, Sprout, Factory, Route, Puzzle } from 'lucide-react'
import { LaravelService } from '../../bindings/larvyn'
import { artisanRun, toast, useActive, useStore } from '../lib/store'
import { copy, cx, load, mod, save } from '../lib/util'
import { Empty, Loading, SearchInput, useAsync } from '../components/ui'

interface Arg {
  name: string
  is_required: boolean
  is_array: boolean
  description: string
  default: any
}
interface Opt {
  name: string
  shortcut: string
  accept_value: boolean
  is_value_required: boolean
  is_multiple: boolean
  description: string
  default: any
}
interface Cmd {
  name: string
  description: string
  help: string
  hidden: boolean
  usage: string[]
  definition: { arguments: Record<string, Arg> | []; options: Record<string, Opt> | [] }
}

const GLOBAL_OPTS = new Set(['help', 'quiet', 'verbose', 'version', 'ansi', 'no-ansi', 'no-interaction', 'env', 'silent'])

const GENERATORS: { cmd: string; label: string; icon: any }[] = [
  { cmd: 'make:model', label: 'Model', icon: Box },
  { cmd: 'make:controller', label: 'Controller', icon: Route },
  { cmd: 'make:migration', label: 'Migration', icon: Database },
  { cmd: 'make:request', label: 'Form request', icon: FileCode2 },
  { cmd: 'make:resource', label: 'API resource', icon: Layers },
  { cmd: 'make:policy', label: 'Policy', icon: Shield },
  { cmd: 'make:job', label: 'Job', icon: Workflow },
  { cmd: 'make:event', label: 'Event', icon: Radio },
  { cmd: 'make:listener', label: 'Listener', icon: Radio },
  { cmd: 'make:mail', label: 'Mailable', icon: Mail },
  { cmd: 'make:notification', label: 'Notification', icon: Bell },
  { cmd: 'make:test', label: 'Test', icon: TestTube2 },
  { cmd: 'make:factory', label: 'Factory', icon: Factory },
  { cmd: 'make:seeder', label: 'Seeder', icon: Sprout },
  { cmd: 'make:command', label: 'Command', icon: Terminal },
  { cmd: 'make:middleware', label: 'Middleware', icon: Puzzle },
  { cmd: 'make:livewire', label: 'Livewire', icon: Zap },
  { cmd: 'make:enum', label: 'Enum', icon: FileCode2 },
  { cmd: 'make:class', label: 'Class', icon: FileCode2 },
  { cmd: 'make:interface', label: 'Interface', icon: FileCode2 },
  { cmd: 'make:trait', label: 'Trait', icon: FileCode2 },
  { cmd: 'make:observer', label: 'Observer', icon: Workflow },
  { cmd: 'make:rule', label: 'Validation rule', icon: Shield },
  { cmd: 'make:component', label: 'Blade component', icon: Layers },
]

const arr = <T,>(x: Record<string, T> | T[] | undefined): T[] => (Array.isArray(x) ? x : Object.values(x ?? {}))

export function Artisan() {
  const { id, info } = useActive()
  const [refresh, setRefresh] = useState(0)
  const { data, error, loading } = useAsync(() => LaravelService.Commands(id!, refresh > 0) as Promise<any>, [id, refresh])
  const [q, setQ] = useState('')
  const [sel, setSel] = useState<string | null>(null)
  const [favs, setFavs] = useState<string[]>(() => load('artisan:favs', ['migrate', 'test', 'optimize:clear', 'db:seed']))
  useEffect(() => save('artisan:favs', favs), [favs])
  useEffect(() => setSel(null), [id])
  // Another view (e.g. Models' "New model") can open a command's form.
  const focusCmd = useStore((s) => s.focus.artisan)
  useEffect(() => {
    if (focusCmd) {
      setSel(focusCmd)
      useStore.setState({ focus: {} })
    }
  }, [focusCmd])

  const commands: Cmd[] = useMemo(() => ((data?.commands ?? []) as Cmd[]).filter((c) => !c.hidden && c.name !== '_complete' && c.name !== 'completion'), [data])
  const groups = useMemo(() => {
    const ql = q.toLowerCase()
    const list = commands.filter((c) => !ql || c.name.includes(ql) || c.description.toLowerCase().includes(ql))
    const map = new Map<string, Cmd[]>()
    for (const c of list) {
      const ns = c.name.includes(':') ? c.name.split(':')[0] : '_'
      map.set(ns, [...(map.get(ns) ?? []), c])
    }
    return [...map.entries()].sort((a, b) => (a[0] === '_' ? -1 : b[0] === '_' ? 1 : a[0].localeCompare(b[0])))
  }, [commands, q])
  const cmd = commands.find((c) => c.name === sel)
  const available = new Set(commands.map((c) => c.name))

  if (!info?.isLaravel) return <Empty icon={<Zap />} title="Not a Laravel project">Artisan needs an artisan file and laravel/framework.</Empty>

  return (
    <div className="view flush">
      <div className="split">
        <div className="pane-list" style={{ width: 300 }}>
          <div style={{ padding: 10, borderBottom: '1px solid var(--line)' }} className="row">
            <SearchInput value={q} onChange={setQ} placeholder={`Search ${commands.length || ''} commands…`} width="100%" autoFocus />
            <button className="icon-btn" title="Reload commands" onClick={() => setRefresh((r) => r + 1)}>
              <RefreshCw />
            </button>
          </div>
          <div className="list-scroll">
            {loading && <Loading />}
            {error && <div className="error-box" style={{ margin: 8 }}>{error}</div>}
            {!q && favs.length > 0 && (
              <>
                <div className="list-group">Favourites</div>
                {favs.filter((f) => available.has(f)).map((f) => (
                  <button key={f} className={cx('list-item', sel === f && 'active')} onClick={() => setSel(f)}>
                    <Star style={{ color: 'var(--yellow)' }} />
                    <span className="mono">{f}</span>
                  </button>
                ))}
              </>
            )}
            {groups.map(([ns, cmds]) => (
              <div key={ns}>
                <div className="list-group">
                  {ns === '_' ? 'General' : ns} <span>{cmds.length}</span>
                </div>
                {cmds.map((c) => (
                  <button key={c.name} className={cx('list-item', sel === c.name && 'active')} onClick={() => setSel(c.name)} title={c.description}>
                    <span className="mono ellipsis" style={{ fontSize: 12 }}>
                      {c.name}
                    </span>
                  </button>
                ))}
              </div>
            ))}
          </div>
        </div>
        <div className="pane-main" style={{ overflow: 'auto' }}>
          {cmd ? (
            <CommandForm
              key={cmd.name}
              cmd={cmd}
              fav={favs.includes(cmd.name)}
              onFav={() => setFavs((f) => (f.includes(cmd.name) ? f.filter((x) => x !== cmd.name) : [...f, cmd.name]))}
              projectId={id!}
            />
          ) : (
            <div className="view pad">
              <div className="page-head">
                <div>
                  <h1>Generator studio</h1>
                  <p>Scaffold anything Laravel can make. Pick a generator, fill the form, run.</p>
                </div>
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(150px, 1fr))', gap: 10 }}>
                {GENERATORS.filter((g) => loading || available.has(g.cmd)).map((g) => (
                  <button key={g.cmd} className="template" onClick={() => setSel(g.cmd)} style={{ alignItems: 'flex-start' }}>
                    <b>
                      <g.icon /> {g.label}
                    </b>
                    <span className="mono" style={{ fontSize: 11 }}>
                      {g.cmd}
                    </span>
                  </button>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function CommandForm({ cmd, fav, onFav, projectId }: { cmd: Cmd; fav: boolean; onFav: () => void; projectId: string }) {
  const args = arr(cmd.definition?.arguments)
  // `artisan list --format=json` names options with their dashes ("--all");
  // strip them so the form adds exactly one "--" and the global filter matches.
  const opts = arr(cmd.definition?.options)
    .map((o) => ({ ...o, name: o.name.replace(/^-+/, '') }))
    .filter((o) => !GLOBAL_OPTS.has(o.name))
  const [vals, setVals] = useState<Record<string, string>>({})
  const [flags, setFlags] = useState<Record<string, boolean>>({})
  const php = useStore((s) => s.settings?.phpBinary) || 'php'

  const argv: string[] = [cmd.name]
  for (const a of args) {
    const v = vals['arg:' + a.name]?.trim()
    if (v) argv.push(...(a.is_array ? v.split(/\s+/) : [v]))
  }
  for (const o of opts) {
    if (o.accept_value) {
      const v = vals['opt:' + o.name]?.trim()
      if (v) argv.push(`--${o.name}=${v}`)
    } else if (flags[o.name]) argv.push(`--${o.name}`)
  }
  const missing = args.some((a) => a.is_required && !vals['arg:' + a.name]?.trim())
  const line = `${php} artisan ${argv.map((a) => (/\s/.test(a) ? `"${a}"` : a)).join(' ')}`
  const run = () => artisanRun(projectId, argv)

  return (
    <div className="view pad" onKeyDown={(e) => e.key === 'Enter' && (e.ctrlKey || e.metaKey) && !missing && run()}>
      <div className="page-head">
        <div className="grow">
          <h1 className="mono" style={{ fontSize: 18 }}>
            {cmd.name}
          </h1>
          <p>{cmd.description}</p>
        </div>
        <button className="icon-btn" title={fav ? 'Remove favourite' : 'Add to favourites'} onClick={onFav}>
          <Star style={fav ? { color: 'var(--yellow)', fill: 'var(--yellow)' } : undefined} />
        </button>
      </div>

      <div className="card" style={{ marginBottom: 16 }}>
        <div className="row" style={{ padding: '10px 12px 10px 16px' }}>
          <span className="mono selectable grow" style={{ fontSize: 12.5, color: 'var(--text)', wordBreak: 'break-all' }}>
            <span style={{ color: 'var(--text-3)' }}>$ </span>
            {line}
          </span>
          <button className="btn sm ghost" onClick={() => copy(line).then(() => toast('success', 'Copied'))}>
            <Copy /> Copy
          </button>
          <button className="btn sm primary" disabled={missing} onClick={run}>
            <Play /> Run <kbd style={{ background: 'rgba(0,0,0,.2)', color: '#fff', borderColor: 'rgba(255,255,255,.25)' }}>{mod} ↵</kbd>
          </button>
        </div>
      </div>

      {args.length > 0 && (
        <>
          <div className="section-title">Arguments</div>
          <div className="col" style={{ gap: 12 }}>
            {args.map((a) => (
              <div className="field" key={a.name}>
                <label>
                  <span className="mono">{a.name}</span> {a.is_required && <span style={{ color: 'var(--accent)' }}>*</span>}
                  {a.is_array && <span className="faint"> (space separated)</span>}
                </label>
                <input
                  className="input"
                  autoFocus={args[0] === a}
                  placeholder={a.description || a.name}
                  value={vals['arg:' + a.name] ?? ''}
                  onChange={(e) => setVals((v) => ({ ...v, ['arg:' + a.name]: e.target.value }))}
                />
              </div>
            ))}
          </div>
        </>
      )}

      {opts.length > 0 && (
        <>
          <div className="section-title">Options</div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))', gap: 8 }}>
            {opts.map((o) =>
              o.accept_value ? (
                <div key={o.name} className="field" style={{ padding: '8px 10px', borderRadius: 9, border: '1px solid var(--line)' }}>
                  <label className="mono" style={{ fontSize: 11.5 }}>
                    --{o.name}
                    {o.shortcut && <span className="faint"> {o.shortcut}</span>}
                  </label>
                  <input
                    className="input"
                    placeholder={o.default != null && o.default !== false && !Array.isArray(o.default) ? String(o.default) : o.description}
                    title={o.description}
                    value={vals['opt:' + o.name] ?? ''}
                    onChange={(e) => setVals((v) => ({ ...v, ['opt:' + o.name]: e.target.value }))}
                  />
                </div>
              ) : (
                <label
                  key={o.name}
                  className="check"
                  style={{ padding: '8px 10px', borderRadius: 9, border: '1px solid ' + (flags[o.name] ? 'var(--accent-line)' : 'var(--line)'), background: flags[o.name] ? 'var(--accent-soft)' : undefined, alignItems: 'flex-start' }}
                >
                  <input type="checkbox" checked={!!flags[o.name]} onChange={(e) => setFlags((f) => ({ ...f, [o.name]: e.target.checked }))} style={{ marginTop: 2 }} />
                  <span>
                    <span className="mono" style={{ color: 'var(--text)', fontSize: 11.5 }}>
                      --{o.name}
                      {o.shortcut && <span className="faint"> {o.shortcut}</span>}
                    </span>
                    <br />
                    <span style={{ fontSize: 11.5 }}>{o.description}</span>
                  </span>
                </label>
              ),
            )}
          </div>
        </>
      )}

      {cmd.help && cmd.help !== cmd.description && (
        <>
          <div className="section-title">Help</div>
          <div className="code muted" style={{ whiteSpace: 'pre-wrap', fontSize: 12 }}>
            {cmd.help.replace(/<\/?[a-z]+>/g, '')}
          </div>
        </>
      )}
    </div>
  )
}
