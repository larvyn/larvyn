import { Terminal, type ITheme } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { SearchAddon } from '@xterm/addon-search'
import '@xterm/xterm/css/xterm.css'
import { Events } from '@wailsio/runtime'
import { SystemService } from '../../bindings/larvyn/internal/system'
import { TerminalService } from '../../bindings/larvyn/internal/terminal'
import { b64ToBytes } from './util'
import { useStore } from './store'

export const termTheme: ITheme = {
  background: '#0a0a0d',
  foreground: '#e4e4e8',
  cursor: '#ff4136',
  cursorAccent: '#0a0a0d',
  selectionBackground: 'rgba(255, 65, 54, 0.3)',
  black: '#1c1c22',
  red: '#ff5a52',
  green: '#3ddc84',
  yellow: '#f5c542',
  blue: '#4c8dff',
  magenta: '#c084fc',
  cyan: '#2dd4bf',
  white: '#d4d4d8',
  brightBlack: '#5a5a66',
  brightRed: '#ff7b74',
  brightGreen: '#6ee7a8',
  brightYellow: '#ffd970',
  brightBlue: '#7aa9ff',
  brightMagenta: '#d8b4fe',
  brightCyan: '#5eead4',
  brightWhite: '#fafafa',
}

interface Entry {
  term: Terminal
  fit: FitAddon
  search: SearchAddon
  el: HTMLDivElement
  opened: boolean
  ready: boolean
  lastSeq: number
  pending: { seq: number; data: string }[]
}

const entries = new Map<string, Entry>()

/** Last time each terminal printed something, for agent "working" states. */
export const activity = new Map<string, number>()

let listening = false
function listen() {
  if (listening) return
  listening = true
  Events.On('term:data', (ev: any) => {
    const d = ev.data as { id: string; seq: number; data: string }
    activity.set(d.id, Date.now())
    const e = entries.get(d.id)
    if (!e) return
    if (!e.ready) {
      e.pending.push(d)
      return
    }
    if (d.seq > e.lastSeq) {
      e.lastSeq = d.seq
      e.term.write(b64ToBytes(d.data))
    }
  })
  // Files dropped on a terminal: their paths, quoted, as a paste.
  Events.On('term:dropped', (ev: any) => {
    const d = ev.data as { session: string; paths: string[] }
    const e = entries.get(d.session)
    if (!e || !d.paths?.length) return
    e.term.paste(quotePaths(d.paths))
    e.term.focus()
  })
  useStore.subscribe((s, prev) => {
    if (s.sessions === prev.sessions) return
    const ids = new Set(s.sessions.map((x) => x.id))
    for (const [id, e] of entries) {
      if (!ids.has(id)) {
        e.term.dispose()
        e.el.remove()
        entries.delete(id)
        activity.delete(id)
      }
    }
  })
}
listen()

const isWindows = () => useStore.getState().platform === 'windows'

type Quoting = 'posix' | 'powershell' | 'cmd'
// How the shell new terminals start reads quotes; the backend knows which
// shell that is. Until it answers, the platform's usual one.
let shellQuoting: Quoting | null = null
TerminalService.Quoting()
  .then((q) => (shellQuoting = q as Quoting))
  .catch(() => {})
const quoting = (): Quoting => shellQuoting ?? (isWindows() ? 'powershell' : 'posix')

/** Paths as the shell (and an agent) reads them, with a trailing space so
 * the next word doesn't run into them. A path that needs quoting is quoted
 * so nothing in a file name ($(…), `…`, &, |) is run:
 * - POSIX shells and PowerShell: single quotes, which neither expands;
 * - cmd.exe: double quotes, inside which & | < > ^ are plain characters
 *   (and Windows file names cannot contain a double quote). */
export function quotePaths(paths: string[], q: Quoting = quoting()) {
  const quote = (p: string) => {
    if (q === 'posix') return /^[A-Za-z0-9/._\-+=,:@%~]+$/.test(p) ? p : `'${p.replace(/'/g, `'\\''`)}'`
    if (/^[A-Za-z0-9/\\._\-+:@]+$/.test(p)) return p
    return q === 'cmd' ? `"${p.replace(/"/g, '')}"` : `'${p.replace(/'/g, "''")}'`
  }
  return paths.filter(Boolean).map(quote).join(' ') + ' '
}

/** Paths in a text/uri-list (files copied in a file manager). A file URL
 * with a host is a network share: a UNC path on Windows, skipped elsewhere,
 * where no path names it. */
export function fileURIs(list: string, windows = isWindows()) {
  const out: string[] = []
  for (const line of list.split(/\r?\n/)) {
    const l = line.trim()
    if (!l.startsWith('file://')) continue
    try {
      const url = new URL(l)
      let path = decodeURIComponent(url.pathname)
      const host = url.hostname && url.hostname !== 'localhost' ? url.hostname : ''
      if (windows) {
        if (/^\/[A-Za-z]:\//.test(path)) path = path.slice(1) // file:///C:/x
        path = (host ? `//${host}${path}` : path).replace(/\//g, '\\')
      } else if (host) continue
      out.push(path)
    } catch {}
  }
  return out
}

function blobToBase64(blob: Blob) {
  return new Promise<string>((resolve, reject) => {
    const r = new FileReader()
    r.onload = () => resolve(String(r.result).replace(/^data:[^,]*,/, ''))
    r.onerror = () => reject(r.error)
    r.readAsDataURL(blob)
  })
}

/** A paste with no text in it, as other terminals handle one:
 * - copied files are pasted as their quoted paths;
 * - a screenshot: Ctrl+V goes to the program, and agents such as Claude
 *   Code and Codex read the image from the clipboard themselves. Windows
 *   console programs expect the terminal to do the pasting, so there the
 *   image is saved to a file and its path pasted instead.
 * Returns false otherwise (text, or only formats a terminal can't paste,
 * such as text/html), and xterm handles the paste as usual. */
function pasteNonText(id: string, term: Terminal, data: DataTransfer | null) {
  if (!data || data.getData('text/plain')) return false
  const files = fileURIs(data.getData('text/uri-list'))
  if (files.length) {
    term.paste(quotePaths(files))
    return true
  }
  const image = [...data.items].find((it) => it.kind === 'file' && it.type.startsWith('image/'))
  if (!isWindows()) {
    // A shell takes a stray ^V as "insert the next key literally", so it is
    // only sent for an image. The webview may also hand over an image-only
    // clipboard with no formats at all; the program reads it all the same.
    if (!image && !data.types.some((t) => t.startsWith('image/')) && data.types.length) return false
    TerminalService.Write(id, '\x16')
    return true
  }
  const file = image?.getAsFile()
  if (!file) return false
  blobToBase64(file)
    .then((b64) => TerminalService.SaveImage(b64))
    .then((path) => term.paste(quotePaths([path])))
    .catch((e) => console.error('could not paste the image', e))
  return true
}

// Keys the app handles even while a terminal has focus.
function isAppShortcut(ev: KeyboardEvent) {
  const mod = ev.ctrlKey || ev.metaKey
  if (!mod) return false
  const k = ev.key.toLowerCase()
  if (ev.shiftKey && ['k', 'p', 'a', 't', 'j'].includes(k)) return true
  return k === 'k' || k === 'j' || k === ',' || k === 'b' || (/^[1-9]$/.test(k) && ev.altKey)
}

function getEntry(id: string): Entry {
  let e = entries.get(id)
  if (e) return e
  const fontSize = useStore.getState().settings?.terminalFontSize || 13
  const term = new Terminal({
    theme: termTheme,
    fontFamily: '"JetBrains Mono Variable", "Cascadia Code", Consolas, monospace',
    fontSize,
    lineHeight: 1.22,
    cursorBlink: true,
    cursorStyle: 'bar',
    allowProposedApi: true,
    scrollback: 20000,
    macOptionIsMeta: true,
    drawBoldTextInBrightColors: false,
    fontWeightBold: '600',
  })
  const fit = new FitAddon()
  const search = new SearchAddon()
  term.loadAddon(fit)
  term.loadAddon(search)
  term.loadAddon(new WebLinksAddon((_ev, uri) => SystemService.OpenURL(uri)))
  term.attachCustomKeyEventHandler((ev) => {
    if (ev.type !== 'keydown') return true
    if (isAppShortcut(ev)) return false
    // Ctrl+C copies when there is a selection, Ctrl+V pastes (Windows habits).
    const mod = ev.ctrlKey || ev.metaKey
    if (mod && ev.key.toLowerCase() === 'c' && term.hasSelection()) {
      navigator.clipboard.writeText(term.getSelection())
      term.clearSelection()
      return false
    }
    if (mod && ev.key.toLowerCase() === 'v' && !ev.shiftKey) {
      return false // the browser paste event reaches xterm (and pasteNonText) by itself
    }
    return true
  })
  // While the saved output is replayed, xterm answers the queries in it
  // (device attributes, cursor position…) as if they were new; those answers
  // would be typed into the shell as ^[[?1;2c, so they are dropped.
  let replaying = false
  term.onData((data) => !replaying && TerminalService.Write(id, data))
  term.onBinary((data) => !replaying && TerminalService.Write(id, data))
  term.onResize(({ cols, rows }) => TerminalService.Resize(id, cols, rows))
  const el = document.createElement('div')
  el.className = 'term-host'
  // A Wails file drop target: main hands files dropped here back as
  // term:dropped for this session.
  el.setAttribute('data-file-drop-target', '')
  el.setAttribute('data-session', id)
  // Before xterm's own listener, which only knows text.
  el.addEventListener(
    'paste',
    (ev) => {
      if (!pasteNonText(id, term, ev.clipboardData)) return
      ev.preventDefault()
      ev.stopImmediatePropagation()
    },
    true,
  )
  e = { term, fit, search, el, opened: false, ready: false, lastSeq: 0, pending: [] }
  entries.set(id, e)
  const entry = e
  TerminalService.Snapshot(id)
    .then((snap) => {
      entry.lastSeq = snap.seq
      if (!snap.data) return
      replaying = true
      return new Promise<void>((done) =>
        term.write(b64ToBytes(snap.data), () => {
          replaying = false
          done()
        }),
      )
    })
    .catch(() => {})
    .finally(() => {
      entry.ready = true
      for (const p of entry.pending) {
        if (p.seq > entry.lastSeq) {
          entry.lastSeq = p.seq
          term.write(b64ToBytes(p.data))
        }
      }
      entry.pending = []
    })
  return e
}

/** Mounts a session's terminal into a container; returns the detach function. */
export function attach(id: string, container: HTMLElement, focus = false): () => void {
  const e = getEntry(id)
  container.appendChild(e.el)
  if (!e.opened) {
    e.term.open(e.el)
    e.opened = true
  }
  const doFit = () => {
    if (!container.isConnected || container.clientWidth === 0) return
    try {
      e.fit.fit()
    } catch {}
  }
  requestAnimationFrame(() => {
    doFit()
    if (focus) e.term.focus()
  })
  const ro = new ResizeObserver(() => requestAnimationFrame(doFit))
  ro.observe(container)
  return () => {
    ro.disconnect()
    if (e.el.parentElement === container) container.removeChild(e.el)
  }
}

export function focusTerm(id: string) {
  entries.get(id)?.term.focus()
}

export function clearTerm(id: string) {
  entries.get(id)?.term.clear()
}

export function findInTerm(id: string, q: string, back = false) {
  const e = entries.get(id)
  if (!e) return
  if (back) e.search.findPrevious(q)
  else e.search.findNext(q)
}

export function setFontSize(size: number) {
  for (const e of entries.values()) {
    e.term.options.fontSize = size
    try {
      e.fit.fit()
    } catch {}
  }
}
