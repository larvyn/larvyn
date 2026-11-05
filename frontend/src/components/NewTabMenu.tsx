import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Bot, Columns2, Play, Rows2, Search, Server, Settings2, SquareTerminal, Terminal as TermIcon, Zap } from 'lucide-react'
import { AgentService, type AgentKind } from '../../bindings/larvyn/internal/agents'
import { type SessionSpec } from '../../bindings/larvyn/internal/terminal'
import { newTerminal, openInTab, setView, toast, useStore } from '../lib/store'
import { cx, errMsg } from '../lib/util'
import { Brand } from './Brand'

/**
 * The "+" menu of the terminal tab bar, modelled on Orca's TabBarCreateEntry:
 * a search box on top, the fixed "new …" actions, the installed agents and a
 * link to the agent settings. Typing filters all of that, offers to switch to
 * an open session whose title matches, and to run the text as a command.
 * Arrow keys move the selection (wrapping), Enter runs it, Esc closes.
 */

type Item = { id: string; label: string; detail?: string; icon: ReactNode; hint?: string; run: () => void; section: 'actions' | 'agents' | 'footer' }

// Agent detection shells out to `which` for every CLI: do it once per app run.
let kindsCache: Promise<AgentKind[]> | null = null
// Settings calls this after saving, as the user's agents may have changed.
export function resetAgentKinds() {
  kindsCache = null
}
function agentKinds() {
  kindsCache ??= AgentService.Kinds()
    .then((k) => k ?? [])
    .catch(() => {
      kindsCache = null
      return []
    })
  return kindsCache
}

const SESSION_ICON: Record<string, any> = { shell: SquareTerminal, service: Server, agent: Bot, run: Zap }

export function NewTabMenu({ projectId, anchor, onClose, canSplit }: { projectId: string; anchor: HTMLElement; onClose: () => void; canSplit: boolean }) {
  const [query, setQuery] = useState('')
  const [sel, setSel] = useState(0)
  const [kinds, setKinds] = useState<AgentKind[]>([])
  const [pos, setPos] = useState<{ top: number; left: number }>({ top: 0, left: 0 })
  const sessions = useStore((s) => s.sessions)
  const php = useStore((s) => s.settings?.phpBinary) || 'php'
  const box = useRef<HTMLDivElement>(null)
  const input = useRef<HTMLInputElement>(null)

  useEffect(() => {
    agentKinds().then(setKinds)
  }, [])

  // Open under the button, kept inside the window.
  useLayoutEffect(() => {
    const r = anchor.getBoundingClientRect()
    const w = box.current?.offsetWidth ?? 300
    setPos({ top: r.bottom + 6, left: Math.max(8, Math.min(r.left, window.innerWidth - w - 8)) })
    input.current?.focus()
  }, [anchor])

  useEffect(() => {
    const down = (e: MouseEvent) => {
      const t = e.target as Node
      if (!box.current?.contains(t) && !anchor.contains(t)) onClose()
    }
    const blur = () => onClose()
    window.addEventListener('mousedown', down, true)
    window.addEventListener('blur', blur)
    window.addEventListener('resize', blur)
    return () => {
      window.removeEventListener('mousedown', down, true)
      window.removeEventListener('blur', blur)
      window.removeEventListener('resize', blur)
    }
  }, [anchor, onClose])

  const shell = (opts: Partial<SessionSpec> = {}, split?: 'row' | 'col') => newTerminal(projectId, opts, split)
  const launchAgent = async (k: AgentKind) => {
    try {
      const sess = await AgentService.Launch({ projectId, agent: k.id, prompt: '', title: '', worktree: false, cwd: '', installDeps: false, base: '' })
      useStore.setState((s) => ({ sessions: s.sessions.some((x) => x.id === sess.id) ? s.sessions : [...s.sessions, sess] }))
      openInTab(projectId, sess.id)
    } catch (e) {
      toast('error', `Could not start ${k.name}`, errMsg(e))
    }
  }

  const items = useMemo<Item[]>(() => {
    const q = query.trim().toLowerCase()
    const match = (...s: (string | undefined)[]) => !q || s.some((x) => x?.toLowerCase().includes(q))
    const out: Item[] = []
    const actions: Item[] = [
      { id: 'new', label: 'New Terminal', icon: <TermIcon />, hint: 'Ctrl+Shift+T', run: () => shell(), section: 'actions' },
      ...(canSplit
        ? [
            { id: 'split-r', label: 'Split Right', icon: <Columns2 />, run: () => shell({}, 'row'), section: 'actions' as const },
            { id: 'split-d', label: 'Split Down', icon: <Rows2 />, run: () => shell({}, 'col'), section: 'actions' as const },
          ]
        : []),
      { id: 'tinker', label: 'Artisan Tinker', icon: <Play />, run: () => shell({ argv: [php, 'artisan', 'tinker'], title: 'tinker' }), section: 'actions' },
    ]
    out.push(...actions.filter((a) => match(a.label)))
    if (q) {
      for (const s of sessions) {
        // Titles only: commands carry binary paths (/home/…/bin/codex) that
        // would match almost any word.
        if (s.projectId !== projectId || !match(s.title)) continue
        const Icon = SESSION_ICON[s.kind] ?? TermIcon
        out.push({
          id: 'tab:' + s.id,
          label: 'Switch to tab',
          detail: s.title,
          icon: s.kind === 'agent' ? <Brand id={s.key} size={14} fallback={<Icon />} /> : <Icon />,
          run: () => openInTab(projectId, s.id),
          section: 'actions',
        })
      }
    }
    for (const k of kinds) {
      if (!k.installed || !match(k.name, k.id, k.binary)) continue
      out.push({ id: 'agent:' + k.id, label: k.name, icon: <Brand id={k.id} size={14} fallback={<Bot style={{ color: k.color }} />} />, run: () => launchAgent(k), section: 'agents' })
    }
    if (q) out.push({ id: 'cmd', label: 'Run command', detail: query.trim(), icon: <Zap />, run: () => shell({ command: query.trim(), title: query.trim() }), section: 'footer' })
    if (match('agent settings')) out.push({ id: 'settings', label: 'Agent settings…', icon: <Settings2 />, run: () => setView('settings'), section: 'footer' })
    return out
  }, [query, kinds, sessions, projectId, canSplit, php])

  useEffect(() => setSel(0), [query])
  useEffect(() => {
    box.current?.querySelector('.ntm-item.sel')?.scrollIntoView({ block: 'nearest' })
  }, [sel])

  const pick = (it: Item) => {
    onClose()
    it.run()
  }

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      if (items.length) setSel((i) => (i + (e.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length)
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (items[sel]) pick(items[sel])
    } else if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      onClose()
    }
  }

  return createPortal(
    <div ref={box} className="ntm" style={pos} onKeyDown={onKey} role="dialog" aria-label="New tab">
      <div className="ntm-search">
        <Search />
        <input
          ref={input}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search sessions, agents, commands…"
          role="combobox"
          aria-expanded
          aria-controls="ntm-list"
          aria-activedescendant={items[sel] ? 'ntm-' + items[sel].id : undefined}
          spellCheck={false}
        />
      </div>
      <div className="ntm-list" id="ntm-list" role="listbox">
        {items.length === 0 && <div className="ntm-empty">Nothing matches.</div>}
        {items.map((it, i) => (
          <div key={it.id}>
            {i > 0 && items[i - 1].section !== it.section && <div className="ntm-sep" />}
            <button
              id={'ntm-' + it.id}
              role="option"
              aria-selected={i === sel}
              className={cx('ntm-item', i === sel && 'sel', it.id === 'settings' && 'muted')}
              onMouseMove={() => i !== sel && setSel(i)}
              onClick={() => pick(it)}
            >
              <span className="ntm-icon">{it.icon}</span>
              <span className="ntm-label">{it.label}</span>
              {it.detail && (
                <>
                  <span className="ntm-dot">·</span>
                  <span className="ntm-detail ellipsis">{it.detail}</span>
                </>
              )}
              {it.hint && <span className="ntm-hint">{it.hint}</span>}
            </button>
          </div>
        ))}
      </div>
    </div>,
    document.body,
  )
}
