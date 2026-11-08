import { Fragment, useState } from 'react'
import { ChevronDown, ChevronRight, ExternalLink, MessageSquarePlus, Pencil, Trash2 } from 'lucide-react'
import { cx } from '../lib/util'

/** One line of a hunk, with its number on each side (0 when absent). */
type Line = { kind: 'ctx' | 'add' | 'del' | 'note'; text: string; old: number; new: number }
type Hunk = { header: string; lines: Line[] }
export type DiffFile = { path: string; status: 'added' | 'deleted' | 'renamed' | 'modified'; binary: boolean; adds: number; dels: number; hunks: Hunk[] }

/**
 * Git quotes paths with unusual characters (non-ASCII, quotes, control
 * characters) C-style: "caf\303\251.php". This turns one back into the real
 * name; an unquoted path is returned as is.
 */
export function unquotePath(p: string): string {
  if (!p.startsWith('"')) return p
  const bytes: number[] = []
  const esc: Record<string, number> = { a: 7, b: 8, t: 9, n: 10, v: 11, f: 12, r: 13, '"': 34, '\\': 92 }
  const enc = new TextEncoder()
  for (let i = 1; i < p.length; i++) {
    const c = p[i]
    if (c === '"') break
    if (c !== '\\') {
      bytes.push(...enc.encode(c))
      continue
    }
    const next = p[++i]
    if (next === undefined) break
    if (/[0-7]/.test(next)) {
      bytes.push(parseInt(p.slice(i, i + 3), 8))
      i += 2
    } else bytes.push(esc[next] ?? next.charCodeAt(0))
  }
  return new TextDecoder().decode(new Uint8Array(bytes))
}

/** The new-side path from a `diff --git a/x b/y` header, either side possibly quoted. */
function headerPath(rest: string): string {
  let b: string | null = null
  if (rest.startsWith('"')) {
    // Skip the quoted a/ path, honouring escaped quotes.
    let i = 1
    while (i < rest.length && rest[i] !== '"') i += rest[i] === '\\' ? 2 : 1
    b = rest.slice(i + 2)
  } else {
    const q = rest.indexOf(' "b/')
    if (q >= 0) b = rest.slice(q + 1)
    else return rest.match(/^a\/(.*) b\/(.*)$/)?.[2] ?? rest
  }
  return unquotePath(b).replace(/^b\//, '')
}

/** Splits a multi-file unified diff (git diff output) into files and hunks. */
export function parseDiff(text: string): DiffFile[] {
  const files: DiffFile[] = []
  let file: DiffFile | null = null
  let hunk: Hunk | null = null
  let o = 0
  let n = 0
  for (const l of text.replace(/\r\n/g, '\n').split('\n')) {
    if (l.startsWith('diff --git ')) {
      file = { path: headerPath(l.slice(11)), status: 'modified', binary: false, adds: 0, dels: 0, hunks: [] }
      files.push(file)
      hunk = null
      continue
    }
    if (!file) continue
    if (!hunk) {
      if (l.startsWith('new file')) file.status = 'added'
      else if (l.startsWith('deleted file')) file.status = 'deleted'
      else if (l.startsWith('rename to ')) {
        file.status = 'renamed'
        file.path = unquotePath(l.slice(10))
      } else if (l.startsWith('+++ ') && l !== '+++ /dev/null') file.path = unquotePath(l.slice(4).replace(/\t$/, '')).replace(/^b\//, '')
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

function Cell({ line, side, picked, gutter }: { line?: Line; side: 'old' | 'new'; picked?: boolean; gutter?: object }) {
  if (!line) return <><span className="dv-no blank" /><span className="dv-code blank" /></>
  const kind = line.kind === 'ctx' ? '' : line.kind
  return (
    <>
      <span className={cx('dv-no', kind, picked && 'picked')} {...gutter}>
        {side === 'old' ? line.old : line.new}
      </span>
      <span className={cx('dv-code', kind, picked && 'picked')}>{line.text || ' '}</span>
    </>
  )
}


/**
 * A review comment on lines of a file. Removed lines are on the old side,
 * with the old file's numbers; every other line is on the new side.
 */
export type Comment = { id: number; path: string; side: 'old' | 'new'; start: number; end: number; code: string[]; body: string }
type Pick = { side: 'old' | 'new'; start: number; end: number }

const sideOf = (l: Line): 'old' | 'new' => (l.kind === 'del' ? 'old' : 'new')
const numOf = (l: Line) => (l.kind === 'del' ? l.old : l.new)

/** The text of a file's lines from start to end on one side. */
function codeOf(f: DiffFile, p: Pick): string[] {
  const out: string[] = []
  for (const h of f.hunks)
    for (const l of h.lines) {
      if (l.kind === 'note' || (p.side === 'old' ? l.kind !== 'del' : l.kind === 'del')) continue
      const n = numOf(l)
      if (n >= p.start && n <= p.end) out.push(l.text)
    }
  return out
}

/** Where a comment points, as the agent reads it: "app/User.php:12-14". */
export function commentPlace(c: Pick & { path: string }) {
  const lines = c.start === c.end ? `${c.start}` : `${c.start}-${c.end}`
  return c.side === 'old' ? `${c.path}, removed line${c.start === c.end ? '' : 's'} ${lines} (old numbering)` : `${c.path}:${lines}`
}

/** All comments as one follow-up message, each with its file, lines and code. */
export function reviewMessage(comments: Comment[]) {
  const parts = comments.map((c, i) => {
    const code = c.code.length > 8 ? [...c.code.slice(0, 8), '…'] : c.code
    return [`${i + 1}. ${commentPlace(c)}`, ...code.map((l) => `   > ${l}`), ...c.body.trim().split('\n').map((l) => `   ${l}`)].join('\n')
  })
  return `I reviewed your changes. Please address these comments:\n\n${parts.join('\n\n')}`
}

/** Line comments a FileDiff shows and edits; without them it is read-only. */
export type Review = {
  comments: Comment[]
  onSave: (c: Omit<Comment, 'id'> & { id?: number }) => void
  onDelete: (id: number) => void
}

function Composer(props: { initial?: string; onSave: (body: string) => void; onCancel: () => void; place: string }) {
  const [body, setBody] = useState(props.initial ?? '')
  const save = () => body.trim() && props.onSave(body)
  return (
    <div className="dv-comment editing">
      <div className="dv-comment-place mono">{props.place}</div>
      <textarea
        autoFocus
        className="input"
        rows={3}
        placeholder="What should the agent change here?"
        value={body}
        onChange={(e) => setBody(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
            e.preventDefault()
            save()
          } else if (e.key === 'Escape') {
            e.stopPropagation()
            props.onCancel()
          }
        }}
      />
      <div className="row" style={{ gap: 6, justifyContent: 'flex-end' }}>
        <span className="faint" style={{ fontSize: 11, marginRight: 'auto' }}>
          Ctrl+Enter to save · Esc to cancel
        </span>
        <button className="btn sm ghost" onClick={props.onCancel}>
          Cancel
        </button>
        <button className="btn sm primary" disabled={!body.trim()} onClick={save}>
          {props.initial ? 'Save' : 'Add comment'}
        </button>
      </div>
    </div>
  )
}

function CommentBox(props: { c: Comment; review: Review }) {
  const { c, review } = props
  const [editing, setEditing] = useState(false)
  if (editing)
    return (
      <Composer
        place={commentPlace(c)}
        initial={c.body}
        onCancel={() => setEditing(false)}
        onSave={(body) => {
          review.onSave({ ...c, body })
          setEditing(false)
        }}
      />
    )
  return (
    <div className="dv-comment">
      <div className="row" style={{ gap: 6 }}>
        <span className="dv-comment-place mono grow">{commentPlace(c)}</span>
        <button className="icon-btn sm" title="Edit" onClick={() => setEditing(true)}>
          <Pencil />
        </button>
        <button className="icon-btn sm" title="Delete" onClick={() => review.onDelete(c.id)}>
          <Trash2 />
        </button>
      </div>
      <div className="dv-comment-body">{c.body}</div>
    </div>
  )
}

export function FileDiff(props: { file: DiffFile; mode: 'split' | 'unified'; onOpen?: () => void; id?: string; review?: Review }) {
  const { file: f, mode, review } = props
  const [open, setOpen] = useState(true)
  // The lines being commented on: a click picks one, shift-click extends it.
  const [pick, setPick] = useState<Pick | null>(null)
  const mine = review?.comments.filter((c) => c.path === f.path) ?? []

  const clickLine = (l: Line | undefined, e: React.MouseEvent) => {
    if (!review || !l || l.kind === 'note') return
    const side = sideOf(l)
    const n = numOf(l)
    const shift = e.shiftKey
    setPick((cur) => (shift && cur && cur.side === side ? { side, start: Math.min(cur.start, n), end: Math.max(cur.end, n) } : { side, start: n, end: n }))
  }
  const picked = (l?: Line) => !!(pick && l && l.kind !== 'note' && sideOf(l) === pick.side && numOf(l) >= pick.start && numOf(l) <= pick.end)
  const ends = (l: Line | undefined, p: Pick) => !!l && l.kind !== 'note' && sideOf(l) === p.side && numOf(l) === p.end

  /** The comments, and the open composer, that end on one of a row's lines. */
  const after = (lines: (Line | undefined)[]) => {
    if (!review) return null
    const here = mine.filter((c) => lines.some((l) => ends(l, c)))
    const composing = pick && lines.some((l) => ends(l, pick))
    if (!here.length && !composing) return null
    return (
      <div className="dv-comments">
        {here.map((c) => (
          <CommentBox key={c.id} c={c} review={review} />
        ))}
        {composing && (
          <Composer
            key={`${pick.side}:${pick.start}`}
            place={commentPlace({ ...pick, path: f.path })}
            onCancel={() => setPick(null)}
            onSave={(body) => {
              review.onSave({ path: f.path, ...pick, code: codeOf(f, pick), body })
              setPick(null)
            }}
          />
        )}
      </div>
    )
  }
  const gutter = (l: Line | undefined) =>
    review && l && l.kind !== 'note'
      ? { onClick: (e: React.MouseEvent) => clickLine(l, e), title: 'Comment on this line (shift-click for several)', role: 'button' as const }
      : {}

  return (
    <div className="dv-file" id={props.id}>
      <div className="dv-head" onClick={() => setOpen(!open)}>
        <button
          className="dv-toggle grow"
          aria-expanded={open}
          title={f.path}
          onClick={(e) => {
            e.stopPropagation()
            setOpen(!open)
          }}
        >
          {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
          <span className="mono ellipsis">{f.path}</span>
        </button>
        {mine.length > 0 && (
          <span className="badge blue" title="Comments on this file">
            <MessageSquarePlus size={11} /> {mine.length}
          </span>
        )}
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
          <div className={cx('dv-grid dv-split', review && 'commentable')}>
            {f.hunks.map((h, i) => (
              <div key={i} className="dv-hunk">
                <div className="dv-sep">{h.header || '···'}</div>
                {splitRows(h.lines).map((r, j) => (
                  <Fragment key={j}>
                    <div className="dv-row">
                      <Cell line={r.left} side="old" picked={picked(r.left)} gutter={gutter(r.left)} />
                      <Cell line={r.right} side="new" picked={picked(r.right)} gutter={gutter(r.right)} />
                    </div>
                    {after(r.left === r.right ? [r.right] : [r.left, r.right])}
                  </Fragment>
                ))}
              </div>
            ))}
          </div>
        ) : (
          <div className={cx('dv-grid dv-unified', review && 'commentable')}>
            {f.hunks.map((h, i) => (
              <div key={i} className="dv-hunk">
                <div className="dv-sep">{h.header || '···'}</div>
                {h.lines.map((l, j) =>
                  l.kind === 'note' ? null : (
                    <Fragment key={j}>
                      <div className={cx('dv-row', picked(l) && 'picked')}>
                        <span className={cx('dv-no', l.kind !== 'ctx' && l.kind)} {...gutter(l)}>
                          {l.old || ''}
                        </span>
                        <span className={cx('dv-no', l.kind !== 'ctx' && l.kind)} {...gutter(l)}>
                          {l.new || ''}
                        </span>
                        <span className={cx('dv-code', l.kind !== 'ctx' && l.kind)}>
                          <i>{l.kind === 'add' ? '+' : l.kind === 'del' ? '−' : ' '}</i>
                          {l.text || ' '}
                        </span>
                      </div>
                      {after([l])}
                    </Fragment>
                  ),
                )}
              </div>
            ))}
          </div>
        ))}
    </div>
  )
}
