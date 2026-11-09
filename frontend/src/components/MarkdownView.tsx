import { type ReactNode } from 'react'
import { Check, Copy, Minus, Square } from 'lucide-react'
import { SystemService } from '../../bindings/larvyn/internal/system'
import { toast } from '../lib/store'
import { copy, cx } from '../lib/util'

/**
 * Lightweight, zero-dependency Markdown viewer tailored for technical
 * specifications, Goldfish reviews, and Spec Drift reports. Renders pure React
 * nodes (no innerHTML) with support for headings, tables, checklists
 * ([x] / [~] / [ ]), fenced code blocks, blockquotes, lists, and inline code.
 */
export function MarkdownView({ content, className }: { content: string; className?: string }) {
  const blocks = parseBlocks(content)
  return (
    <div className={cx('md-view', className)}>
      {blocks.map((b, i) => renderBlock(b, i))}
    </div>
  )
}

type ListItem = {
  text: string
  check?: 'done' | 'partial' | 'todo'
}

type Block =
  | { kind: 'heading'; level: 1 | 2 | 3 | 4; text: string }
  | { kind: 'code'; lang: string; code: string }
  | { kind: 'quote'; lines: string[] }
  | { kind: 'ul'; items: ListItem[] }
  | { kind: 'ol'; items: string[] }
  | { kind: 'table'; headers: string[]; rows: string[][] }
  | { kind: 'hr' }
  | { kind: 'p'; text: string }

function parseBlocks(src: string): Block[] {
  const lines = src.replace(/\r\n/g, '\n').split('\n')
  const out: Block[] = []
  let i = 0

  while (i < lines.length) {
    const raw = lines[i]
    const trimmed = raw.trim()

    if (!trimmed) {
      i++
      continue
    }

    // Fenced code block: ```lang ... ```
    if (trimmed.startsWith('```')) {
      const lang = trimmed.slice(3).trim()
      const buf: string[] = []
      i++
      while (i < lines.length && !lines[i].trim().startsWith('```')) {
        buf.push(lines[i])
        i++
      }
      if (i < lines.length) i++
      out.push({ kind: 'code', lang, code: buf.join('\n') })
      continue
    }

    // Horizontal rule
    if (/^(-{3,}|\*{3,}|_{3,})$/.test(trimmed)) {
      out.push({ kind: 'hr' })
      i++
      continue
    }

    // Heading: # .. ####
    const hm = /^(#{1,4})\s+(.+)$/.exec(trimmed)
    if (hm) {
      out.push({
        kind: 'heading',
        level: Math.min(4, hm[1].length) as 1 | 2 | 3 | 4,
        text: hm[2].trim(),
      })
      i++
      continue
    }

    // Table: | a | b | followed by | --- | --- |
    if (
      trimmed.startsWith('|') &&
      i + 1 < lines.length &&
      /^\s*\|?[\s:-]+\|[\s|:-]*$/.test(lines[i + 1])
    ) {
      const headers = splitTableRow(trimmed)
      i += 2
      const rows: string[][] = []
      while (i < lines.length && lines[i].trim().startsWith('|')) {
        rows.push(splitTableRow(lines[i].trim()))
        i++
      }
      out.push({ kind: 'table', headers, rows })
      continue
    }

    // Blockquote: > ...
    if (trimmed.startsWith('>')) {
      const q: string[] = []
      while (i < lines.length && lines[i].trim().startsWith('>')) {
        q.push(lines[i].trim().replace(/^>\s?/, ''))
        i++
      }
      out.push({ kind: 'quote', lines: q })
      continue
    }

    // Unordered list / Checklist: - ... or * ...
    if (/^[-*+]\s+/.test(trimmed)) {
      const items: ListItem[] = []
      while (i < lines.length) {
        const l = lines[i].trim()
        const m = /^[-*+]\s+(.*)$/.exec(l)
        if (!m) break
        const body = m[1]
        const cm = /^\[([ xX~])\]\s+(.*)$/.exec(body)
        if (cm) {
          const mark = cm[1].toLowerCase()
          items.push({
            check: mark === 'x' ? 'done' : mark === '~' ? 'partial' : 'todo',
            text: cm[2],
          })
        } else {
          items.push({ text: body })
        }
        i++
      }
      out.push({ kind: 'ul', items })
      continue
    }

    // Ordered list: 1. ...
    if (/^\d+\.\s+/.test(trimmed)) {
      const items: string[] = []
      while (i < lines.length) {
        const l = lines[i].trim()
        const m = /^\d+\.\s+(.*)$/.exec(l)
        if (!m) break
        items.push(m[1])
        i++
      }
      out.push({ kind: 'ol', items })
      continue
    }

    // Paragraph: collect consecutive regular lines
    const p: string[] = [trimmed]
    i++
    while (i < lines.length) {
      const nxt = lines[i].trim()
      if (
        !nxt ||
        nxt.startsWith('```') ||
        nxt.startsWith('#') ||
        nxt.startsWith('>') ||
        nxt.startsWith('|') ||
        /^[-*+]\s+/.test(nxt) ||
        /^\d+\.\s+/.test(nxt) ||
        /^(-{3,}|\*{3,}|_{3,})$/.test(nxt)
      ) {
        break
      }
      p.push(nxt)
      i++
    }
    out.push({ kind: 'p', text: p.join(' ') })
  }

  return out
}

function splitTableRow(row: string): string[] {
  const stripped = row.replace(/^\|/, '').replace(/\|$/, '')
  return stripped.split('|').map((c) => c.trim())
}

function renderBlock(b: Block, key: number): ReactNode {
  switch (b.kind) {
    case 'heading': {
      const Tag = (`h${b.level}` as 'h1' | 'h2' | 'h3' | 'h4')
      return <Tag key={key}>{renderInline(b.text)}</Tag>
    }
    case 'hr':
      return <hr key={key} />
    case 'quote':
      return (
        <blockquote key={key}>
          {b.lines.map((l, idx) => (
            <p key={idx}>{renderInline(l)}</p>
          ))}
        </blockquote>
      )
    case 'code':
      return (
        <div key={key} className="md-code">
          <div className="md-code-head">
            <span>{b.lang || 'code'}</span>
            <button
              type="button"
              className="btn sm ghost"
              style={{ height: 22, padding: '0 7px', fontSize: 11 }}
              onClick={() => copy(b.code).then(() => toast('success', 'Copied to clipboard'))}
            >
              <Copy size={11} /> Copy
            </button>
          </div>
          <pre>
            <code>{b.code}</code>
          </pre>
        </div>
      )
    case 'table':
      return (
        <div key={key} className="md-table-wrap">
          <table className="md-table">
            <thead>
              <tr>
                {b.headers.map((h, hi) => (
                  <th key={hi}>{renderInline(h)}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {b.rows.map((r, ri) => (
                <tr key={ri}>
                  {b.headers.map((_, ci) => (
                    <td key={ci}>{renderInline(r[ci] ?? '')}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )
    case 'ul':
      return (
        <ul key={key} className={cx(b.items.some((it) => it.check) && 'md-checklist')}>
          {b.items.map((it, idx) => (
            <li key={idx} className={cx(it.check && `check-${it.check}`)}>
              {it.check === 'done' && (
                <span className="md-check done">
                  <Check size={11} strokeWidth={3} />
                </span>
              )}
              {it.check === 'partial' && (
                <span className="md-check partial">
                  <Minus size={11} strokeWidth={3} />
                </span>
              )}
              {it.check === 'todo' && (
                <span className="md-check todo">
                  <Square size={11} />
                </span>
              )}
              <span>{renderInline(it.text)}</span>
            </li>
          ))}
        </ul>
      )
    case 'ol':
      return (
        <ol key={key}>
          {b.items.map((it, idx) => (
            <li key={idx}>{renderInline(it)}</li>
          ))}
        </ol>
      )
    case 'p':
      return <p key={key}>{renderInline(b.text)}</p>
  }
}

const inlineRe = /(`[^`]+`|\*\*[^*]+\*\*|\*[^*\s][^*]*\*|_[^_\s][^_]*_|\[[^\]]+\]\([^)]+\))/g

function renderInline(text: string): ReactNode[] {
  const parts = text.split(inlineRe)
  return parts.map((part, i) => {
    if (!part) return null
    if (part.startsWith('`') && part.endsWith('`') && part.length >= 2) {
      return (
        <code key={i} className="md-inline-code">
          {part.slice(1, -1)}
        </code>
      )
    }
    if (part.startsWith('**') && part.endsWith('**') && part.length >= 4) {
      return <strong key={i}>{part.slice(2, -2)}</strong>
    }
    if (
      ((part.startsWith('*') && part.endsWith('*')) || (part.startsWith('_') && part.endsWith('_'))) &&
      part.length >= 3
    ) {
      return <em key={i}>{part.slice(1, -1)}</em>
    }
    if (part.startsWith('[')) {
      const m = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(part)
      if (m) {
        const [, label, href] = m
        const safe = /^https?:\/\//i.test(href)
        return (
          <a
            key={i}
            href={safe ? href : '#'}
            onClick={(e) => {
              e.preventDefault()
              if (safe) SystemService.OpenURL(href)
            }}
          >
            {label}
          </a>
        )
      }
    }
    return part
  })
}
