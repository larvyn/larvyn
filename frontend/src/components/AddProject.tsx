import { useEffect, useState } from 'react'
import { AlertTriangle, Check, FolderOpen, FolderSearch, RefreshCw } from 'lucide-react'
import { ProjectService, type Found } from '../../bindings/larvyn/internal/project'
import { addProjectAt, browseProject, selectProject, useStore } from '../lib/store'
import { errMsg } from '../lib/util'
import { Modal, SearchInput } from './ui'

// The last scan, kept so opening the picker again is instant.
let cache: Found[] | null = null
// Each scan's number: only the newest one's answer is shown.
let scanSeq = 0

/** What makes two paths the same folder: case counts, except on Windows and
 * macOS, whose file systems ignore it by default (as the backend does). */
const pathKey = (path: string) => {
  const platform = useStore.getState().platform
  return platform === 'windows' || platform === 'darwin' ? path.toLowerCase() : path
}

/** "Open a project": the Laravel apps found on this computer, with the
 * system folder dialog for anything the scan missed. */
export function AddProject() {
  const open = useStore((s) => s.addProjectOpen)
  const projects = useStore((s) => s.projects)
  const [found, setFound] = useState<Found[] | null>(cache)
  const [scanning, setScanning] = useState(false)
  const [query, setQuery] = useState('')
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')

  const scan = () => {
    const seq = ++scanSeq
    setScanning(true)
    setError('')
    ProjectService.Discover()
      .then((r) => {
        if (seq !== scanSeq) return
        cache = r ?? []
        setFound(cache)
      })
      .catch((e) => seq === scanSeq && setError(errMsg(e)))
      .finally(() => seq === scanSeq && setScanning(false))
  }
  useEffect(() => {
    if (!open) return
    setQuery('')
    scan()
  }, [open])
  if (!open) return null

  const close = () => useStore.setState({ addProjectOpen: false })
  const byPath = new Map(projects.map((p) => [pathKey(p.path), p]))
  const q = query.trim().toLowerCase()
  const list = (found ?? []).filter((f) => !q || f.name.toLowerCase().includes(q) || f.path.toLowerCase().includes(q))

  const pick = async (f: Found) => {
    const existing = byPath.get(pathKey(f.path))
    if (existing) {
      selectProject(existing.id)
      close()
      return
    }
    setBusy(f.path)
    const ok = await addProjectAt(f.path)
    setBusy('')
    if (ok) close()
  }
  const browse = async () => {
    if (await browseProject()) close()
  }

  return (
    <Modal
      title="Open a Laravel project"
      subtitle="Laravel apps found on this computer. Not listed? Choose its folder by hand."
      onClose={close}
      footer={
        <>
          <button className="btn ghost" onClick={scan} disabled={scanning} title="Scan again">
            {scanning ? <div className="spinner" /> : <RefreshCw />} Rescan
          </button>
          <div className="grow" />
          <button className="btn" onClick={browse}>
            <FolderOpen /> Browse manually…
          </button>
        </>
      }
    >
      <SearchInput value={query} onChange={setQuery} placeholder="Filter by name or path…" autoFocus width="100%" />
      {error && (
        <div className="warn-box">
          <AlertTriangle />
          <div className="grow">
            <b>The scan failed</b>
            <div>{error}</div>
            <div className="row" style={{ marginTop: 8 }}>
              <button className="btn sm" onClick={scan}>
                <RefreshCw /> Try again
              </button>
              <button className="btn sm ghost" onClick={browse}>
                <FolderOpen /> Browse manually…
              </button>
            </div>
          </div>
        </div>
      )}
      {found === null ? (
        !error && (
          <div className="empty">
            <div className="spinner lg" />
            <p>Looking for Laravel projects…</p>
          </div>
        )
      ) : list.length === 0 ? (
        <div className="empty">
          <div className="ico">
            <FolderSearch />
          </div>
          <h3>{q ? 'No matches' : 'No Laravel projects found'}</h3>
          <p>{q ? 'Try another name, or choose the folder by hand.' : 'Larvyn looked in your home folder and the usual web roots. Choose the folder by hand instead.'}</p>
          <button className="btn primary" onClick={browse}>
            <FolderOpen /> Browse manually…
          </button>
        </div>
      ) : (
        <div className="add-project-list">
          {list.map((f) => {
            const added = byPath.has(pathKey(f.path))
            return (
              <button key={f.path} className="list-item add-project-item" title={f.path} disabled={!!busy} onClick={() => pick(f)}>
                <div className="grow" style={{ minWidth: 0 }}>
                  <div className="ellipsis">{f.name}</div>
                  <div className="faint mono ellipsis add-project-path">{f.path}</div>
                </div>
                {busy === f.path ? (
                  <div className="spinner" />
                ) : added ? (
                  <span className="badge green">
                    <Check /> Added
                  </span>
                ) : null}
              </button>
            )
          })}
        </div>
      )}
    </Modal>
  )
}
