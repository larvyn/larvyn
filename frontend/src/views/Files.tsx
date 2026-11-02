import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { ChevronDown, ChevronRight, Code2, File, FileCode2, Folder, FolderOpen, FolderTree, RefreshCw, RotateCcw, Save, Search } from 'lucide-react'
import { SystemService } from '../../bindings/larvyn'
import { FileService, type FileContent, type FileEntry } from '../../bindings/larvyn/internal/files'
import { fail, toast, useActive, useStore } from '../lib/store'
import { cx, errMsg, isMac, load, mod, save } from '../lib/util'
import { Empty, Loading, confirmDialog } from '../components/ui'
import { CodeEditor } from '../components/CodeEditor'
import { CODE_EXT, FileFinder } from '../components/FileFinder'

// Unsaved edits outlive the view (switching to Terminals and back keeps them).
const drafts = new Map<string, { text: string; version: string }>()
let openSeq = 0

interface Open {
  file: FileContent
  /** Bumped on every (re)load, so the editor starts a fresh document. */
  seq: number
  text: string
  saved: string
}

export function Files() {
  const { id, project } = useActive()
  const focusFile = useStore((s) => s.focus.file)
  const [children, setChildren] = useState<Record<string, FileEntry[]>>({})
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [rootError, setRootError] = useState<string | null>(null)
  const [open, setOpen] = useState<Open | null>(null)
  const [opening, setOpening] = useState(false)
  const [saving, setSaving] = useState(false)
  const dirty = !!open && open.text !== open.saved
  // Quick open: every file of the project, loaded when the finder opens and
  // refreshed after a save, a tree refresh or a project switch.
  const [finder, setFinder] = useState(false)
  const finderRef = useRef(finder)
  finderRef.current = finder
  // Focus goes back where it was (usually the editor) when the finder closes.
  const finderReturn = useRef<HTMLElement | null>(null)
  const showFinder = useCallback((show: boolean) => {
    if (show && !finderRef.current) finderReturn.current = document.activeElement as HTMLElement | null
    setFinder(show)
  }, [])
  const [index, setIndex] = useState<{ id: string; files: string[]; truncated: boolean } | null>(null)
  const [indexError, setIndexError] = useState<string | null>(null)
  const idRef = useRef(id)
  idRef.current = id

  const loadDir = useCallback(
    async (rel: string) => {
      if (!id) return
      try {
        const list = (await FileService.List(id, rel)) ?? []
        setChildren((c) => ({ ...c, [rel]: list }))
        if (rel === '') setRootError(null)
      } catch (e) {
        if (rel === '') setRootError(errMsg(e))
        else toast('error', 'Could not open folder', errMsg(e))
      }
    },
    [id],
  )

  const openFile = useCallback(
    async (path: string, force = false) => {
      if (!id) return
      if (!force && dirty && open?.file.path !== path) {
        const ok = await confirmDialog({ title: `Discard changes to ${open!.file.path}?`, message: 'Your edits are not saved.', confirm: 'Discard', danger: true })
        if (!ok) return
        drafts.delete(id + ':' + open!.file.path)
      }
      setOpening(true)
      try {
        const file = await FileService.Read(id, path)
        const draft = drafts.get(id + ':' + path)
        const text = draft && draft.version === file.version && !force ? draft.text : file.content
        setOpen({ file, seq: ++openSeq, text, saved: file.content })
        save('files:last:' + id, path)
        // Reveal it in the tree.
        const parts = path.split('/')
        const parents = parts.slice(0, -1).map((_, i) => parts.slice(0, i + 1).join('/'))
        setExpanded((s) => new Set([...s, ...parents]))
        for (const p of parents) if (!children[p]) loadDir(p)
      } catch (e) {
        toast('error', 'Could not open file', errMsg(e))
      } finally {
        setOpening(false)
      }
    },
    [id, dirty, open, children, loadDir],
  )

  const loadIndex = useCallback(async () => {
    const pid = idRef.current
    if (!pid) return
    try {
      const idx = await FileService.AllFiles(pid)
      if (idRef.current !== pid) return
      setIndex({ id: pid, files: idx.files ?? [], truncated: idx.truncated })
      setIndexError(null)
    } catch (e) {
      if (idRef.current === pid) setIndexError(errMsg(e))
    }
  }, [])

  // Load (or refresh, keeping the old list on screen) whenever the finder
  // opens, and again if the project changes while it is open.
  useEffect(() => {
    if (finder && id) loadIndex()
  }, [finder, id, loadIndex])

  // Cmd+P (Ctrl+P elsewhere) opens the finder, also from inside the editor:
  // a capture listener runs before CodeMirror sees the key.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const modKey = isMac ? e.metaKey && !e.ctrlKey : e.ctrlKey && !e.metaKey
      if (!modKey || e.shiftKey || e.altKey || e.key.toLowerCase() !== 'p') return
      if (useStore.getState().paletteOpen) return
      e.preventDefault()
      e.stopPropagation()
      showFinder(!finderRef.current)
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [showFinder])

  useEffect(() => {
    if (finder) return
    const el = finderReturn.current
    finderReturn.current = null
    if (el?.isConnected) el.focus()
  }, [finder])

  // New project: fresh tree, and the file that was open there last.
  useEffect(() => {
    setIndex(null)
    setIndexError(null)
    setChildren({})
    setExpanded(new Set())
    setOpen(null)
    if (!id) return
    loadDir('')
    const last = load<string | null>('files:last:' + id, null)
    if (last && !useStore.getState().focus.file) openFile(last, true).catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id])

  // Other views hand a file over with handoff('files', { file }).
  useEffect(() => {
    if (focusFile) {
      useStore.setState({ focus: {} })
      openFile(focusFile)
    }
  }, [focusFile, openFile])

  // Keep the unsaved buffer when the view goes away.
  useEffect(() => {
    if (!id || !open) return
    const key = id + ':' + open.file.path
    if (open.text !== open.saved) drafts.set(key, { text: open.text, version: open.file.version })
    else drafts.delete(key)
  }, [id, open])

  const toggle = (dir: string) => {
    setExpanded((s) => {
      const n = new Set(s)
      if (n.has(dir)) n.delete(dir)
      else {
        n.add(dir)
        if (!children[dir]) loadDir(dir)
      }
      return n
    })
  }

  const doSave = async () => {
    if (!id || !open || saving || open.file.binary || open.file.tooLarge) return
    setSaving(true)
    const text = open.text
    try {
      const version = await FileService.Write(id, open.file.path, text, open.file.version)
      setOpen((o) => (o ? { ...o, saved: text, file: { ...o.file, version, content: text } } : o))
      drafts.delete(id + ':' + open.file.path)
      toast('success', 'Saved', open.file.path)
      if (index) loadIndex()
    } catch (e) {
      toast('error', 'Could not save', errMsg(e))
    } finally {
      setSaving(false)
    }
  }

  const reload = async () => {
    if (!open) return
    if (dirty && !(await confirmDialog({ title: 'Reload from disk?', message: 'Your unsaved edits are lost.', confirm: 'Reload', danger: true }))) return
    if (id) drafts.delete(id + ':' + open.file.path)
    openFile(open.file.path, true)
  }

  const refreshTree = () => {
    setChildren({})
    loadDir('')
    for (const d of expanded) loadDir(d)
    if (index) loadIndex()
  }

  const renderDir = (rel: string, depth: number): ReactNode =>
    (children[rel] ?? []).map((e) => (
      <div key={e.path}>
        <button
          className={cx('list-item', 'file-row', open?.file.path === e.path && 'active')}
          style={{ paddingLeft: 8 + depth * 14 }}
          title={e.path}
          onClick={() => (e.dir ? toggle(e.path) : openFile(e.path))}
        >
          {e.dir ? (expanded.has(e.path) ? <ChevronDown className="chev" /> : <ChevronRight className="chev" />) : <span className="chev" />}
          {e.dir ? expanded.has(e.path) ? <FolderOpen /> : <Folder /> : CODE_EXT.test(e.name) ? <FileCode2 /> : <File />}
          <span className="grow ellipsis">{e.name}</span>
          {open?.file.path === e.path && dirty && <span className="dot-dirty" title="Unsaved changes" />}
        </button>
        {e.dir && expanded.has(e.path) && (children[e.path] ? renderDir(e.path, depth + 1) : <div className="faint" style={{ paddingLeft: 30 + depth * 14, fontSize: 11 }}>…</div>)}
      </div>
    ))

  if (!project) return <div className="view pad" />

  return (
    <div className="view flush">
      <div className="split">
        <div className="pane-list" style={{ width: 280 }}>
          <div className="row" style={{ padding: '10px 10px 10px 14px', borderBottom: '1px solid var(--line)' }}>
            <span className="grow ellipsis" style={{ fontWeight: 600 }}>
              {project.name}
            </span>
            <button className="icon-btn" title={`Go to file (${mod}P)`} onClick={() => showFinder(true)}>
              <Search />
            </button>
            <button className="icon-btn" title="Refresh the tree" onClick={refreshTree}>
              <RefreshCw />
            </button>
          </div>
          <div className="list-scroll files-tree">
            {rootError ? <div className="error-box" style={{ margin: 6 }}>{rootError}</div> : children[''] ? renderDir('', 0) : <Loading />}
          </div>
        </div>
        <div className="pane-main">
          {open ? (
            <>
              <div className="row files-head">
                <span className="grow ellipsis mono" title={open.file.abs}>
                  {open.file.path}
                  {dirty && <span className="faint"> ● modified</span>}
                </span>
                <button className="btn sm ghost" title="Reload from disk" onClick={reload}>
                  <RotateCcw /> Reload
                </button>
                <button className="btn sm ghost" title="Open in your editor" onClick={() => SystemService.OpenInEditor(open.file.abs, 1).catch(fail('Could not open the editor'))}>
                  <Code2 /> Open in editor
                </button>
                <button className="btn sm primary" disabled={!dirty || saving} onClick={doSave} title={`Save (${mod}S)`}>
                  <Save /> Save
                </button>
              </div>
              {open.file.binary || open.file.tooLarge ? (
                <Empty icon={<File />} title={open.file.binary ? 'Binary file' : 'File too large to edit here'}>
                  {open.file.binary ? 'This file is not text.' : `${(open.file.size / 1048576).toFixed(1)} MB; the editor opens files up to 2 MB.`} Use Open in editor.
                </Empty>
              ) : (
                <CodeEditor
                  docKey={open.file.path + ':' + open.seq}
                  path={open.file.path}
                  value={open.text}
                  onChange={(text) => setOpen((o) => (o ? { ...o, text } : o))}
                  onSave={doSave}
                />
              )}
            </>
          ) : opening ? (
            <Loading />
          ) : (
            <Empty icon={<FolderTree />} title="Browse and edit your code">
              Pick a file on the left or press {mod}P to find one. {mod}S saves, {mod}F searches inside the file.
            </Empty>
          )}
        </div>
      </div>
      {finder && (
        <FileFinder
          files={index?.id === id ? index.files : null}
          truncated={index?.id === id && index.truncated}
          error={indexError}
          onPick={(p) => openFile(p)}
          onClose={() => showFinder(false)}
        />
      )}
    </div>
  )
}
