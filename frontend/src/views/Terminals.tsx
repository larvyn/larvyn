import { useState } from 'react'
import {
  Bot,
  Columns2,
  Eraser,
  Pencil,
  Play,
  Plus,
  Rows2,
  Server,
  Square,
  SquareTerminal,
  Terminal as TermIcon,
  X,
  Zap,
} from 'lucide-react'
import { type SessionInfo } from '../../bindings/larvyn'
import { TerminalService } from '../../bindings/larvyn/internal/terminal'
import { newTerminal, openInTab, useActive, useStore, type TermTab } from '../lib/store'
import { clearTerm, focusTerm } from '../lib/terms'
import { cx, duration } from '../lib/util'
import { Empty, TermPane, promptDialog, useNow } from '../components/ui'
import { Brand } from '../components/Brand'
import { NewTabMenu } from '../components/NewTabMenu'

// A stable empty list: a selector returning a fresh [] re-renders forever.
const NO_TABS: TermTab[] = []
const KIND_ICON: Record<string, any> = { shell: SquareTerminal, service: Server, agent: Bot, run: Zap }
const KIND_LABEL: Record<string, string> = { shell: 'Shells', service: 'Services', agent: 'Agents', run: 'Commands' }

export function Terminals() {
  const { id, project } = useActive()
  const sessions = useStore((s) => s.sessions)
  const tabs = useStore((s) => (id ? s.tabs[id] : undefined)) ?? NO_TABS
  const activeTabId = useStore((s) => (id ? s.activeTab[id] : undefined))
  const [focusedPane, setFocusedPane] = useState<string | null>(null)
  const [menuAt, setMenuAt] = useState<HTMLElement | null>(null)
  const now = useNow(5000)
  if (!project || !id) return null

  const mine = sessions.filter((s) => s.projectId === id)
  const byId = new Map(mine.map((s) => [s.id, s]))
  const tab = tabs.find((t) => t.id === activeTabId) ?? tabs[0]
  const groups = ['shell', 'service', 'agent', 'run'].map((k) => ({ kind: k, items: mine.filter((s) => s.kind === k) })).filter((g) => g.items.length)

  const setActive = (tid: string) => useStore.setState((s) => ({ activeTab: { ...s.activeTab, [id]: tid } }))
  const closeTab = (tid: string) => {
    const t = tabs.find((x) => x.id === tid)
    t?.panes.forEach((p) => {
      const sess = byId.get(p)
      if (sess?.kind === 'shell') TerminalService.Close(p)
    })
    useStore.setState((s) => ({ tabs: { ...s.tabs, [id]: (s.tabs[id] ?? []).filter((x) => x.id !== tid) } }))
  }
  const removePane = (pid: string) => {
    const sess = byId.get(pid)
    if (sess?.kind === 'shell') TerminalService.Close(pid)
    else
      useStore.setState((s) => ({
        tabs: { ...s.tabs, [id]: (s.tabs[id] ?? []).map((t) => ({ ...t, panes: t.panes.filter((p) => p !== pid) })).filter((t) => t.panes.length) },
      }))
  }
  const split = (dir: 'row' | 'col') => newTerminal(id, {}, dir)
  const toggleMenu = (el: HTMLElement) => setMenuAt((cur) => (cur === el ? null : el))

  return (
    <div className="view flush">
      {menuAt && <NewTabMenu projectId={id} anchor={menuAt} canSplit={!!tab} onClose={() => setMenuAt(null)} />}
      <div className="split">
        <div className="pane-list" style={{ width: 240 }}>
          <div className="toolbar" style={{ padding: '8px 10px', minHeight: 44 }}>
            <span className="nav-label" style={{ padding: 0 }}>
              Sessions
            </span>
            <span className="spacer" />
            <button className={cx('icon-btn sm', menuAt?.dataset.menu === 'side' && 'active')} data-menu="side" title="New tab…" onClick={(e) => toggleMenu(e.currentTarget)}>
              <Plus />
            </button>
          </div>
          <div className="list-scroll">
            {groups.length === 0 && <div className="faint" style={{ padding: 12, fontSize: 12 }}>No sessions yet.</div>}
            {groups.map((g) => (
              <div key={g.kind}>
                <div className="list-group">
                  {KIND_LABEL[g.kind]} <span>{g.items.length}</span>
                </div>
                {g.items.map((s) => (
                  <SessionRow key={s.id} s={s} now={now} active={!!tab?.panes.includes(s.id)} onOpen={() => openInTab(id, s.id)} />
                ))}
              </div>
            ))}
          </div>
        </div>

        <div className="pane-main">
          <div className="drawer-tabs" style={{ height: 40, background: 'var(--bg-1)' }}>
            {tabs.map((t) => {
              const first = byId.get(t.panes[0])
              const Icon = KIND_ICON[first?.kind ?? 'shell'] ?? TermIcon
              const bell = t.panes.some((p) => byId.get(p)?.bell)
              return (
                <button key={t.id} className={cx('dtab', t.id === tab?.id && 'active')} onClick={() => setActive(t.id)} onAuxClick={(e) => e.button === 1 && closeTab(t.id)}>
                  {first?.kind === 'agent' && !bell ? (
                    <Brand id={first.key} size={13} fallback={<Icon size={13} style={{ flexShrink: 0 }} />} />
                  ) : (
                    <Icon size={13} style={{ flexShrink: 0, color: bell ? 'var(--yellow)' : undefined }} />
                  )}
                  <span className="ellipsis">{t.panes.map((p) => byId.get(p)?.title ?? '…').join(' · ')}</span>
                  {t.panes.length > 1 && <span className="pill-n">{t.panes.length}</span>}
                  <span
                    className="icon-btn sm x"
                    onClick={(e) => {
                      e.stopPropagation()
                      closeTab(t.id)
                    }}
                  >
                    <X />
                  </span>
                </button>
              )
            })}
            <button className={cx('icon-btn sm', menuAt?.dataset.menu === 'bar' && 'active')} data-menu="bar" title="New tab…" onClick={(e) => toggleMenu(e.currentTarget)}>
              <Plus />
            </button>
            <span className="spacer" />
            {tab && (
              <>
                <button className="icon-btn sm" title="Split right" onClick={() => split('row')}>
                  <Columns2 />
                </button>
                <button className="icon-btn sm" title="Split down" onClick={() => split('col')}>
                  <Rows2 />
                </button>
              </>
            )}
          </div>
          {!tab ? (
            <div style={{ flex: 1, display: 'grid', placeItems: 'center' }}>
              <Empty
                icon={<SquareTerminal />}
                title="Terminals that live with your project"
                action={
                  <div className="row">
                    <button className="btn primary" onClick={() => newTerminal(id)}>
                      <Plus /> New terminal
                    </button>
                    <button className="btn" onClick={() => newTerminal(id, { argv: [useStore.getState().settings?.phpBinary || 'php', 'artisan', 'tinker'], title: 'tinker' })}>
                      <Play /> artisan tinker
                    </button>
                  </div>
                }
              >
                Shells open in the project folder and keep running while you switch views or projects. Split them, and open any service, agent or command from the left.
              </Empty>
            </div>
          ) : (
            <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: tab.dir === 'row' ? 'row' : 'column', gap: 1, background: 'var(--line-2)' }}>
              {tab.panes.map((pid) => {
                const s = byId.get(pid)
                if (!s) return null
                return (
                  <div
                    key={pid}
                    style={{ flex: 1, minWidth: 0, minHeight: 0, display: 'flex', flexDirection: 'column', background: '#0a0a0d' }}
                    onMouseDown={() => {
                      setFocusedPane(pid)
                      if (s.bell) TerminalService.ClearBell(pid)
                    }}
                  >
                    {tab.panes.length > 1 || s.kind !== 'shell' ? (
                      <PaneHeader s={s} focused={focusedPane === pid} onClose={() => removePane(pid)} />
                    ) : null}
                    <TermPane id={pid} focus />
                  </div>
                )
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function SessionRow({ s, now, active, onOpen }: { s: SessionInfo; now: number; active: boolean; onOpen: () => void }) {
  const Icon = KIND_ICON[s.kind] ?? TermIcon
  return (
    <button className={cx('list-item', active && 'active')} onClick={onOpen} title={s.command || s.cwd}>
      {s.kind === 'agent' ? (
        <Brand id={s.key} size={14} fallback={<Icon style={s.meta?.color ? { color: s.meta.color } : undefined} />} />
      ) : (
        <Icon style={s.meta?.color ? { color: s.meta.color } : undefined} />
      )}
      <div className="grow">
        <div className="ellipsis" style={{ fontSize: 12.5 }}>
          {s.title}
        </div>
        <div className="faint ellipsis mono" style={{ fontSize: 10.5 }}>
          {s.running ? duration(now - s.startedAt) : s.stopped ? 'stopped' : `exited ${s.exitCode}`}
          {s.meta?.branch ? ` · ${s.meta.branch}` : ''}
        </div>
      </div>
      {s.bell && <span className="status-dot idle" />}
      <span className={cx('status-dot', s.running && 'on', !s.running && !s.stopped && s.exitCode !== 0 && 'err')} style={{ width: 6, height: 6, boxShadow: 'none' }} />
    </button>
  )
}

function PaneHeader({ s, focused, onClose }: { s: SessionInfo; focused: boolean; onClose: () => void }) {
  return (
    <div className="row" style={{ height: 30, padding: '0 6px 0 12px', borderBottom: '1px solid var(--line)', background: focused ? 'var(--bg-2)' : 'var(--bg-1)', flexShrink: 0 }}>
      <span className={cx('status-dot', s.running && 'on')} style={{ width: 6, height: 6, boxShadow: 'none' }} />
      <span className="ellipsis" style={{ fontSize: 12, fontWeight: 550 }}>
        {s.title}
      </span>
      <span className="faint mono ellipsis" style={{ fontSize: 10.5 }}>
        {s.cwd}
      </span>
      <span className="spacer" />
      <button
        className="icon-btn sm"
        title="Rename"
        onClick={async () => {
          const t = await promptDialog({ title: 'Rename terminal', value: s.title })
          if (t) TerminalService.Rename(s.id, t)
        }}
      >
        <Pencil />
      </button>
      <button
        className="icon-btn sm"
        title="Clear"
        onClick={() => {
          clearTerm(s.id)
          focusTerm(s.id)
        }}
      >
        <Eraser />
      </button>
      {s.running && s.kind !== 'shell' && (
        <button className="icon-btn sm" title="Stop" onClick={() => TerminalService.Kill(s.id)}>
          <Square />
        </button>
      )}
      <button className="icon-btn sm" title="Close pane" onClick={onClose}>
        <X />
      </button>
    </div>
  )
}
