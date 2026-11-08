import { AlertTriangle, Download, FileCode2, Pencil, Play, Plus, Terminal, Trash2, User, Users } from 'lucide-react'
import { type ProjectCommand, type ProjectFileInfo } from '../../bindings/larvyn/internal/project'
import { SystemService } from '../../bindings/larvyn/internal/system'
import { fail } from '../lib/store'
import { cx } from '../lib/util'
import { confirmDialog } from './ui'
import { deleteProjectCommand, editProjectCommand, findPlugin, moveProjectCommand, runProjectCommand } from './projectCommands'

/** The Overview's Commands card: the user's own commands and the team's,
 * from .larvyn/project.json, with what in that file needs attention. See #106. */
export function ProjectCommandsCard({ id, file, commands }: { id: string; file: ProjectFileInfo | null; commands: ProjectCommand[] }) {
  const plugins = (file?.plugins ?? []).filter((p) => p.problem)
  const warnings = file?.warnings ?? []
  const setup = file?.setup ?? []
  const openFile = () => file && SystemService.OpenInEditor(file.path, 1).catch(fail('Could not open the editor'))

  return (
    <div className="card">
      <div className="card-head">
        <h3>
          <Terminal /> Commands
        </h3>
        {commands.length > 0 && <span className="pill-n">{commands.length}</span>}
        <span className="spacer" />
        {file?.exists && (
          <button className="btn sm ghost" title={file.path} onClick={openFile}>
            <FileCode2 /> project.json
          </button>
        )}
        <button className="icon-btn sm" title="Add a command" onClick={() => editProjectCommand(id)}>
          <Plus />
        </button>
      </div>

      {file?.error && (
        <div className="check-row">
          <div className="ic bad">
            <AlertTriangle />
          </div>
          <div className="t">
            <b>.larvyn/project.json has an error</b>
            <span>
              {file.error}
              {(file.commands?.length || file.stack?.length || file.setup?.length) ? ' · Larvyn keeps using the last version that loaded.' : ''}
            </span>
          </div>
        </div>
      )}
      {warnings.length > 0 && (
        <div className="check-row">
          <div className="ic wait">
            <AlertTriangle />
          </div>
          <div className="t">
            <b>
              {warnings.length} {warnings.length > 1 ? 'entries' : 'entry'} skipped in .larvyn/project.json
            </b>
            {warnings.map((w) => (
              <span key={w} className="mono" style={{ fontSize: 11.5 }}>
                {w}
              </span>
            ))}
          </div>
        </div>
      )}
      {plugins.map((p) => (
        <div key={p.id} className="check-row">
          <div className="ic wait">
            <AlertTriangle />
          </div>
          <div className="t">
            <b>
              The project uses <span className="mono">{p.id}</span>
              {p.version && <span className="faint mono"> {p.version}</span>}
            </b>
            <span>{p.problem === 'missing' ? 'It is not installed.' : p.problem}</span>
          </div>
          <button className="btn sm" onClick={() => findPlugin(p.id)}>
            <Download /> {p.problem === 'missing' ? 'Install' : 'Open plugins'}
          </button>
        </div>
      ))}

      {commands.map((c) => (
        <div key={c.id} className="check-row">
          <span className={cx('badge', c.shared && 'blue')} title={c.shared ? 'Shared with the team in .larvyn/project.json' : 'Only you: kept in your Larvyn settings'}>
            {c.shared ? <Users /> : <User />}
          </span>
          <div className="t">
            <b>{c.title}</b>
            <span className="mono ellipsis">{c.run}</span>
          </div>
          <button className="icon-btn sm" title={c.shared ? 'Make it only yours' : 'Share it with the team (.larvyn/project.json)'} onClick={() => moveProjectCommand(id, c)}>
            {c.shared ? <User /> : <Users />}
          </button>
          <button className="icon-btn sm" title="Edit" onClick={() => editProjectCommand(id, c)}>
            <Pencil />
          </button>
          <button
            className="icon-btn sm danger"
            title="Delete"
            onClick={async () => {
              if (await confirmDialog({ title: `Delete ${c.title}?`, message: c.shared ? 'It is removed from .larvyn/project.json, for the whole team once you commit it.' : 'Only your own copy is removed.', confirm: 'Delete', danger: true }))
                deleteProjectCommand(id, c)
            }}
          >
            <Trash2 />
          </button>
          <button className="btn sm" onClick={() => runProjectCommand(id, c)}>
            <Play /> Run
          </button>
        </div>
      ))}
      {commands.length === 0 && (
        <div className="check-row faint">
          <div className="t">
            <span>
              Add the commands you run often. Share one and it goes to <span className="mono">.larvyn/project.json</span>, for everyone who opens the repo.
            </span>
          </div>
        </div>
      )}
      {setup.length > 0 && (
        <div className="check-row" style={{ borderTop: '1px solid var(--line)' }}>
          <div className="t">
            <span>
              New agent worktrees run {setup.length} setup step{setup.length > 1 ? 's' : ''} first: <span className="mono">{setup.join(' · ')}</span>
            </span>
          </div>
        </div>
      )}
    </div>
  )
}
