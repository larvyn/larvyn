import { useEffect, useState } from 'react'
import { AlertTriangle, Check, FolderOpen, FolderSearch, RefreshCw, ShieldCheck } from 'lucide-react'
import { ProjectService, type Found } from '../../bindings/larvyn/internal/project'
import { addProjectAt, browseProject, useStore } from '../lib/store'
import { errMsg } from '../lib/util'
import { Modal, SearchInput } from './ui'

// The last scan, kept so opening the picker again is instant.
let cache: Found[] | null = null
// Whether that scan left out folders macOS guards (Desktop, Documents…).
let cacheNarrowed = false
// Each scan's number: only the newest one's answer is shown.
let scanSeq = 0

/** "Open a project": the Laravel apps found on this computer, with the
 * system folder dialog for anything the scan missed. */
export function AddProject() {
  const open = useStore((s) => s.addProjectOpen)
  const projects = useStore((s) => s.projects)
  const [found, setFound] = useState<Found[] | null>(cache)
  const [narrowed, setNarrowed] = useState(cacheNarrowed)
  const [scanning, setScanning] = useState(false)
  const [query, setQuery] = useState('')
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')

  // widen also looks in Desktop, Documents and Downloads on macOS, which
  // asks the user for permission once, and keeps doing so from then on.
  const scan = (widen = false) => {
    const seq = ++scanSeq
    setScanning(true)
    setError('')
    ProjectService.Discover(widen)
      .then((r) => {
        if (seq !== scanSeq) return
        cache = r?.projects ?? []
        cacheNarrowed = !!r?.narrowed
        setFound(cache)
        setNarrowed(cacheNarrowed)
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
  // The backend marks folders already added (it knows when two paths are
  // the same folder); a project added since the scan is matched by path.
  const isAdded = (f: Found) => f.added || projects.some((p) => p.path === f.path)
  const q = query.trim().toLowerCase()
  const list = (found ?? []).filter((f) => !q || f.name.toLowerCase().includes(q) || f.path.toLowerCase().includes(q))

  // Add returns the existing project for a folder already added.
  const pick = async (f: Found) => {
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
          <button className="btn ghost" onClick={() => scan()} disabled={scanning} title="Scan again">
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
              <button className="btn sm" onClick={() => scan()}>
                <RefreshCw /> Try again
              </button>
              <button className="btn sm ghost" onClick={browse}>
                <FolderOpen /> Browse manually…
              </button>
            </div>
          </div>
        </div>
      )}
      {narrowed && found !== null && (
        <div className="row add-project-widen">
          <ShieldCheck />
          <span className="grow faint">Desktop, Documents and Downloads were skipped: macOS asks before an app looks there.</span>
          <button className="btn sm" onClick={() => scan(true)} disabled={scanning}>
            Search them too
          </button>
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
        // After a failed rescan, the error says it all; "No matches" for a
        // filter still shows.
        (q || !error) && (
          <div className="empty">
            <div className="ico">
              <FolderSearch />
            </div>
            <h3>{q ? 'No matches' : 'No Laravel projects found'}</h3>
            <p>{q ? 'Try another name, or choose the folder by hand.' : 'Larvyn looked in your home folder, your Herd and Valet folders and the usual web roots. Choose the folder by hand instead.'}</p>
            <button className="btn primary" onClick={browse}>
              <FolderOpen /> Browse manually…
            </button>
          </div>
        )
      ) : (
        <div className="add-project-list">
          {list.map((f) => {
            const added = isAdded(f)
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
