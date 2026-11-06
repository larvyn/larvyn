import { useState } from 'react'
import { ChevronDown, ChevronRight, ExternalLink } from 'lucide-react'
import { cx } from '../lib/util'

/** One line of a hunk, with its number on each side (0 when absent). */
type Line = { kind: 'ctx' | 'add' | 'del' | 'note'; text: string; old: number; new: number }
type Hunk = { header: string; lines: Line[] }
export type DiffFile = { path: string; status: 'added' | 'deleted' | 'renamed' | 'modified'; binary: boolean; adds: number; dels: number; hunks: Hunk[] }

/** Splits a multi-file unified diff (git diff output) into files and hunks. */
export function parseDiff(text: string): DiffFile[] {
  const files: DiffFile[] = []
  let file: DiffFile | null = null
  let hunk: Hunk | null = null
  let o = 0
  let n = 0
  for (const l of text.replace(/\r\n/g, '\n').split('\n')) {
    if (l.startsWith('diff --git ')) {
      const m = l.match(/^diff --git a\/(.*) b\/(.*)$/)
      file = { path: m?.[2] ?? l.slice(11), status: 'modified', binary: false, adds: 0, dels: 0, hunks: [] }
      files.push(file)
      hunk = null
      continue
    }
    if (!file) continue
    if (!hunk) {
      if (l.startsWith('new file')) file.status = 'added'
      else if (l.startsWith('deleted file')) file.status = 'deleted'
      else if (l.startsWith('rename to ')) file.status = 'renamed'
      else if (l.startsWith('Binary files')) file.binary = true
    }
    const h = l.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)$/)
    if (h) {
      o = +h[1]
      n = +h[2]
      hunk = { header: h[3].trim(), lines: [] }
      file.hunks.push(hunk)
      continue
    }
    if (!hunk) continue
    if (l.startsWith('+')) {
      hunk.lines.push({ kind: 'add', text: l.slice(1), old: 0, new: n++ })
      file.adds++
    } else if (l.startsWith('-')) {
      hunk.lines.push({ kind: 'del', text: l.slice(1), old: o++, new: 0 })
      file.dels++
    } else if (l.startsWith(' ')) hunk.lines.push({ kind: 'ctx', text: l.slice(1), old: o++, new: n++ })
    else if (l.startsWith('\\')) hunk.lines.push({ kind: 'note', text: l, old: 0, new: 0 })
  }
  return files
}

/** First changed line on the new side, for opening the file there. */
export function firstLine(f: DiffFile) {
  for (const h of f.hunks) for (const l of h.lines) if (l.kind === 'add' || l.kind === 'del') return l.new || l.old || 1
  return 1
}

type Row = { left?: Line; right?: Line }

/** Pairs removed and added runs so each change sits beside its replacement. */
function splitRows(lines: Line[]): Row[] {
  const rows: Row[] = []
  let dels: Line[] = []
  let adds: Line[] = []
  const flush = () => {
    for (let i = 0; i < Math.max(dels.length, adds.length); i++) rows.push({ left: dels[i], right: adds[i] })
    dels = []
    adds = []
  }
  for (const l of lines) {
    if (l.kind === 'del') {
      if (adds.length) flush()
      dels.push(l)
    } else if (l.kind === 'add') adds.push(l)
    else {
      flush()
      if (l.kind === 'ctx') rows.push({ left: l, right: l })
    }
  }
  flush()
  return rows
}

function Cell({ line, side }: { line?: Line; side: 'old' | 'new' }) {
  if (!line) return <><span className="dv-no blank" /><span className="dv-code blank" /></>
  const kind = line.kind === 'ctx' ? '' : line.kind
  return (
    <>
      <span className={cx('dv-no', kind)}>{side === 'old' ? line.old : line.new}</span>
      <span className={cx('dv-code', kind)}>{line.text || ' '}</span>
    </>
  )
}

export function FileDiff(props: { file: DiffFile; mode: 'split' | 'unified'; onOpen?: () => void; id?: string }) {
  const { file: f, mode } = props
  const [open, setOpen] = useState(true)
  return (
    <div className="dv-file" id={props.id}>
      <div className="dv-head" onClick={() => setOpen(!open)}>
        {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        <span className="mono ellipsis grow" title={f.path}>
          {f.path}
        </span>
        {f.status !== 'modified' && <span className={cx('badge', f.status === 'added' ? 'green' : f.status === 'deleted' ? 'red' : '')}>{f.status}</span>}
        <span className="dv-count add">+{f.adds}</span>
        <span className="dv-count del">−{f.dels}</span>
        {props.onOpen && f.status !== 'deleted' && (
          <button
            className="icon-btn sm"
            title="Open in editor"
            onClick={(e) => {
              e.stopPropagation()
              props.onOpen!()
            }}
          >
            <ExternalLink />
          </button>
        )}
      </div>
      {open &&
        (f.binary ? (
          <div className="dv-note">Binary file</div>
        ) : !f.hunks.length ? (
          <div className="dv-note">No content changes</div>
        ) : mode === 'split' ? (
          <div className="dv-grid dv-split">
            {f.hunks.map((h, i) => (
              <div key={i} className="dv-hunk">
                <div className="dv-sep">{h.header || '···'}</div>
                {splitRows(h.lines).map((r, j) => (
                  <div key={j} className="dv-row">
                    <Cell line={r.left} side="old" />
                    <Cell line={r.right} side="new" />
                  </div>
                ))}
              </div>
            ))}
          </div>
        ) : (
          <div className="dv-grid dv-unified">
            {f.hunks.map((h, i) => (
              <div key={i} className="dv-hunk">
                <div className="dv-sep">{h.header || '···'}</div>
                {h.lines.map((l, j) =>
                  l.kind === 'note' ? null : (
                    <div key={j} className="dv-row">
                      <span className={cx('dv-no', l.kind !== 'ctx' && l.kind)}>{l.old || ''}</span>
                      <span className={cx('dv-no', l.kind !== 'ctx' && l.kind)}>{l.new || ''}</span>
                      <span className={cx('dv-code', l.kind !== 'ctx' && l.kind)}>
                        <i>{l.kind === 'add' ? '+' : l.kind === 'del' ? '−' : ' '}</i>
                        {l.text || ' '}
                      </span>
                    </div>
                  ),
                )}
              </div>
            ))}
          </div>
        ))}
    </div>
  )
}
