import { useDeferredValue, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { File, FileCode2, Search } from 'lucide-react'
import { rankPaths } from '../lib/fuzzy'
import { cx, isMac, mod, num } from '../lib/util'

export const CODE_EXT = /\.(php|js|mjs|cjs|jsx|ts|tsx|vue|svelte|css|scss|json|ya?ml|md|xml|html?|sh|env.*)$/i
const MAX_RESULTS = 50

/** Text with the characters at positions (offset into the full path) highlighted. */
function highlight(text: string, offset: number, positions: Set<number>): ReactNode[] {
  const out: ReactNode[] = []
  let run = ''
  let marked = false
  const flush = (key: number) => {
    if (run) out.push(marked ? <mark key={key}>{run}</mark> : run)
    run = ''
  }
  for (let i = 0; i < text.length; i++) {
    const m = positions.has(offset + i)
    if (m !== marked) {
      flush(i)
      marked = m
    }
    run += text[i]
  }
  flush(text.length)
  return out
}

/**
 * Quick open for the Files view: fuzzy search over every file of the project.
 * files is null while the list loads.
 */
export function FileFinder(props: { files: string[] | null; truncated: boolean; error: string | null; onPick: (path: string) => void; onClose: () => void }) {
  const { files, onPick, onClose } = props
  const [q, setQ] = useState('')
  const [sel, setSel] = useState(0)
  const query = useDeferredValue(q)
  const list = useRef<HTMLDivElement>(null)

  const results = useMemo(() => (files ? rankPaths(query, files, MAX_RESULTS) : []), [files, query])
  useEffect(() => setSel(0), [query, files])
  useEffect(() => {
    list.current?.querySelector('.finder-item.selected')?.scrollIntoView({ block: 'nearest' })
  }, [sel, results])

  const pick = (i: number) => {
    const r = results[i]
    if (!r) return
    onClose()
    onPick(r.path)
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown' || (isMac && e.ctrlKey && e.key === 'n')) {
      e.preventDefault()
      setSel((s) => (results.length ? (s + 1) % results.length : 0))
    } else if (e.key === 'ArrowUp' || (isMac && e.ctrlKey && e.key === 'p')) {
      e.preventDefault()
      setSel((s) => (results.length ? (s - 1 + results.length) % results.length : 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      pick(sel)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      onClose()
    }
  }

  let body: ReactNode
  if (props.error) body = <div className="finder-empty">{props.error}</div>
  else if (!files) body = <div className="finder-empty">Loading files…</div>
  else if (!results.length) body = <div className="finder-empty">{files.length ? 'No file matches.' : 'This project has no files.'}</div>
  else
    body = results.map((r, i) => {
      const slash = r.path.lastIndexOf('/')
      const name = r.path.slice(slash + 1)
      const dir = slash >= 0 ? r.path.slice(0, slash) : ''
      const pos = new Set(r.positions)
      return (
        <button
          key={r.path}
          className={cx('finder-item', i === sel && 'selected')}
          title={r.path}
          onMouseMove={() => i !== sel && setSel(i)}
          onClick={() => pick(i)}
        >
          {CODE_EXT.test(name) ? <FileCode2 /> : <File />}
          <span className="name ellipsis">{highlight(name, slash + 1, pos)}</span>
          {dir && <span className="dir ellipsis mono">{highlight(dir, 0, pos)}</span>}
        </button>
      )
    })

  return (
    <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="palette" onKeyDown={onKeyDown}>
        <div className="pal-search">
          <Search />
          <input
            className="finder-input"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            autoFocus
            spellCheck={false}
            autoComplete="off"
            placeholder="Go to file by name or path…"
          />
        </div>
        <div className="finder-list" ref={list}>
          {body}
        </div>
        <div className="pal-foot">
          <span>
            <kbd>↑</kbd>
            <kbd>↓</kbd> navigate
          </span>
          <span>
            <kbd>↵</kbd> open
          </span>
          <span>
            <kbd>esc</kbd> close
          </span>
          <span className="spacer" />
          {files && (
            <span title={props.truncated ? 'The project has more files than the finder lists; the rest are not searched.' : undefined}>
              {num(files.length)} files{props.truncated && ' (truncated)'}
            </span>
          )}
          <span>
            <kbd>{mod}</kbd>
            <kbd>P</kbd>
          </span>
        </div>
      </div>
    </div>
  )
}
