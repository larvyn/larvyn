import { useEffect, useRef, useState, type ReactNode } from 'react'
import { create } from 'zustand'
import { AlertTriangle, Bug, CheckCircle2, Info, Search, X, XCircle } from 'lucide-react'
import { SystemService } from '../../bindings/larvyn/internal/system'
import { attach } from '../lib/terms'
import { useStore } from '../lib/store'
import { cx } from '../lib/util'

export function Modal(props: {
  title: ReactNode
  subtitle?: ReactNode
  icon?: ReactNode
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  wide?: boolean
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && props.onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [props.onClose])
  return (
    <div className="overlay center" onMouseDown={(e) => e.target === e.currentTarget && props.onClose()}>
      <div className={cx('modal', props.wide && 'wide')}>
        <div className="modal-head">
          {props.icon}
          <div className="grow">
            <h2>{props.title}</h2>
            {props.subtitle && <p>{props.subtitle}</p>}
          </div>
          <button className="icon-btn" onClick={props.onClose}>
            <X />
          </button>
        </div>
        <div className="modal-body" style={props.wide ? { flex: 1, padding: 0 } : undefined}>
          {props.children}
        </div>
        {props.footer && <div className="modal-foot">{props.footer}</div>}
      </div>
    </div>
  )
}

// ---- confirm ---------------------------------------------------------------

interface ConfirmReq {
  title: string
  message?: ReactNode
  confirm?: string
  danger?: boolean
  /** Replaces the warning sign, e.g. with the icon of what is installed. */
  icon?: ReactNode
  resolve: (ok: boolean) => void
}
const useConfirm = create<{ req: ConfirmReq | null }>(() => ({ req: null }))

export function confirmDialog(opts: Omit<ConfirmReq, 'resolve'>): Promise<boolean> {
  return new Promise((resolve) => useConfirm.setState({ req: { ...opts, resolve } }))
}

export function ConfirmHost() {
  const req = useConfirm((s) => s.req)
  if (!req) return null
  const done = (ok: boolean) => {
    req.resolve(ok)
    useConfirm.setState({ req: null })
  }
  return (
    <Modal
      title={req.title}
      onClose={() => done(false)}
      icon={
        req.icon ?? (
        <div className="empty" style={{ padding: 0 }}>
          <div className="ico" style={{ width: 34, height: 34, margin: 0, borderRadius: 10, color: req.danger ? 'var(--red)' : 'var(--text-2)' }}>
            <AlertTriangle style={{ width: 16, height: 16 }} />
          </div>
        </div>
        )
      }
      footer={
        <>
          <button className="btn ghost" onClick={() => done(false)}>
            Cancel
          </button>
          <button className={cx('btn', req.danger ? 'primary' : 'primary')} autoFocus onClick={() => done(true)}>
            {req.confirm ?? 'Continue'}
          </button>
        </>
      }
    >
      {req.message && <div className="muted" style={{ fontSize: 13, lineHeight: 1.6 }}>{req.message}</div>}
    </Modal>
  )
}

// ---- prompt ----------------------------------------------------------------

interface PromptReq {
  title: string
  label?: string
  value?: string
  placeholder?: string
  confirm?: string
  resolve: (v: string | null) => void
}
const usePrompt = create<{ req: PromptReq | null }>(() => ({ req: null }))

export function promptDialog(opts: Omit<PromptReq, 'resolve'>): Promise<string | null> {
  return new Promise((resolve) => usePrompt.setState({ req: { ...opts, resolve } }))
}

export function PromptHost() {
  const req = usePrompt((s) => s.req)
  const [value, setValue] = useState('')
  useEffect(() => setValue(req?.value ?? ''), [req])
  if (!req) return null
  const done = (v: string | null) => {
    req.resolve(v)
    usePrompt.setState({ req: null })
  }
  return (
    <Modal
      title={req.title}
      onClose={() => done(null)}
      footer={
        <>
          <button className="btn ghost" onClick={() => done(null)}>
            Cancel
          </button>
          <button className="btn primary" onClick={() => done(value)}>
            {req.confirm ?? 'Save'}
          </button>
        </>
      }
    >
      <div className="field">
        {req.label && <label>{req.label}</label>}
        <input
          className="input"
          autoFocus
          value={value}
          placeholder={req.placeholder}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && done(value)}
        />
      </div>
    </Modal>
  )
}

// ---- misc ------------------------------------------------------------------

export function Empty(props: { icon: ReactNode; title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="empty">
      <div className="ico">{props.icon}</div>
      <h3>{props.title}</h3>
      {props.children && <p>{props.children}</p>}
      {props.action && <div style={{ marginTop: 8 }}>{props.action}</div>}
    </div>
  )
}

export function Toggle(props: { on: boolean; onChange: (v: boolean) => void; disabled?: boolean; title?: string }) {
  return (
    <button
      className={cx('toggle', props.on && 'on')}
      disabled={props.disabled}
      title={props.title}
      onClick={(e) => {
        e.stopPropagation()
        props.onChange(!props.on)
      }}
    />
  )
}

export function SearchInput(props: { value: string; onChange: (v: string) => void; placeholder?: string; autoFocus?: boolean; width?: number | string }) {
  return (
    <div className="search" style={{ width: props.width ?? 260 }}>
      <Search />
      <input
        className="input"
        value={props.value}
        autoFocus={props.autoFocus}
        placeholder={props.placeholder ?? 'Search…'}
        onChange={(e) => props.onChange(e.target.value)}
        onKeyDown={(e) => e.key === 'Escape' && props.onChange('')}
      />
    </div>
  )
}

export function Loading({ label }: { label?: string }) {
  return (
    <div className="empty">
      <div className="spinner lg" />
      {label && <p>{label}</p>}
    </div>
  )
}

export function TermPane({ id, focus, className, style }: { id: string; focus?: boolean; className?: string; style?: React.CSSProperties }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!ref.current) return
    return attach(id, ref.current, focus)
  }, [id])
  return <div ref={ref} className={cx('term-slot', className)} style={style} />
}

export function Toasts() {
  const toasts = useStore((s) => s.toasts)
  return (
    <div className="toasts">
      {toasts.map((t) => (
        <div key={t.id} className={cx('toast', t.kind)}>
          {t.kind === 'success' ? <CheckCircle2 /> : t.kind === 'error' ? <XCircle /> : <Info />}
          <div className="grow">
            <b>{t.title}</b>
            {t.message && <div className="msg">{t.message}</div>}
            {t.kind === 'error' && (
              <button className="toast-report" onClick={() => SystemService.ReportBug(t.title + (t.message ? '\n' + t.message : '')).catch(() => {})}>
                <Bug /> Report this
              </button>
            )}
          </div>
          <button
            className="icon-btn sm"
            onClick={() => useStore.setState((s) => ({ toasts: s.toasts.filter((x) => x.id !== t.id) }))}
          >
            <X />
          </button>
        </div>
      ))}
    </div>
  )
}

export function useAsync<T>(fn: () => Promise<T>, deps: unknown[]) {
  const [state, setState] = useState<{ data: T | null; error: string | null; loading: boolean }>({ data: null, error: null, loading: true })
  const [tick, setTick] = useState(0)
  useEffect(() => {
    let alive = true
    setState((s) => ({ ...s, loading: true, error: null }))
    fn()
      .then((data) => alive && setState({ data, error: null, loading: false }))
      .catch((e) => {
        if (!alive) return
        let msg = e?.message ?? String(e)
        try {
          msg = JSON.parse(msg).message ?? msg
        } catch {}
        setState({ data: null, error: msg, loading: false })
      })
    return () => {
      alive = false
    }
  }, [...deps, tick])
  return { ...state, reload: () => setTick((t) => t + 1), setData: (d: T) => setState((s) => ({ ...s, data: d })) }
}

export function useNow(interval = 1000) {
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), interval)
    return () => clearInterval(t)
  }, [interval])
  return now
}

export function Logo() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
      <path d="M5 4v13.5L12 21l7-3.5" />
      <path d="M12 21V11l7-3.5L12 4 8.5 5.75" />
    </svg>
  )
}

export function Diff({ text }: { text: string }) {
  if (!text.trim()) return <Empty icon={<CheckCircle2 />} title="No changes" />
  const lines = text.replace(/\r\n/g, '\n').split('\n')
  return (
    <div className="diff">
      {lines.map((l, i) => {
        let cls = ''
        if (l.startsWith('diff --git')) {
          cls = 'file'
          l = l.replace(/^diff --git a\/(.*) b\/.*$/, '$1')
        } else if (l.startsWith('+++') || l.startsWith('---') || l.startsWith('index ') || l.startsWith('new file') || l.startsWith('deleted file')) cls = 'meta'
        else if (l.startsWith('@@')) cls = 'hunk'
        else if (l.startsWith('+')) cls = 'add'
        else if (l.startsWith('-')) cls = 'del'
        if (cls === 'meta' && (l.startsWith('index ') || l.startsWith('+++') || l.startsWith('---'))) return null
        return (
          <div key={i} className={cx('ln', cls)}>
            {l || ' '}
          </div>
        )
      })}
    </div>
  )
}
