import { Terminal, type ITheme } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { SearchAddon } from '@xterm/addon-search'
import '@xterm/xterm/css/xterm.css'
import { Events } from '@wailsio/runtime'
import { SystemService, TerminalService } from '../../bindings/larvyn'
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
      return false // the browser paste event reaches xterm by itself
    }
    return true
  })
  term.onData((data) => TerminalService.Write(id, data))
  term.onBinary((data) => TerminalService.Write(id, data))
  term.onResize(({ cols, rows }) => TerminalService.Resize(id, cols, rows))
  const el = document.createElement('div')
  el.className = 'term-host'
  e = { term, fit, search, el, opened: false, ready: false, lastSeq: 0, pending: [] }
  entries.set(id, e)
  const entry = e
  TerminalService.Snapshot(id)
    .then((snap) => {
      if (snap.data) term.write(b64ToBytes(snap.data))
      entry.lastSeq = snap.seq
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
