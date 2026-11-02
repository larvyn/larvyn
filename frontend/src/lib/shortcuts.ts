import { useEffect, useRef, type RefObject } from 'react'

/**
 * Runs `run` on ⌘↵ (Ctrl↵ on Windows and Linux) while the view is mounted,
 * wherever the focus is: after clicking a snippet or a button too, not only
 * inside the editor. It stays out of the way while a dialog or the palette is
 * open, inside a terminal, and while typing in another text field than
 * `editor` (an agent's reply box, a search input).
 */
export function useRunShortcut(run: () => void, opts: { enabled?: boolean; editor?: RefObject<HTMLElement | null> } = {}) {
  const latest = useRef(run)
  latest.current = run
  const editor = opts.editor
  const enabled = opts.enabled ?? true
  useEffect(() => {
    if (!enabled) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Enter' || !(e.metaKey || e.ctrlKey) || e.altKey || e.shiftKey) return
      if (e.defaultPrevented || e.isComposing) return
      if (document.querySelector('.overlay')) return
      const t = e.target instanceof HTMLElement ? e.target : null
      if (t?.closest('.xterm')) return
      const editable = !!t && (t.isContentEditable || t.tagName === 'TEXTAREA' || t.tagName === 'INPUT' || t.tagName === 'SELECT')
      if (editable && t !== editor?.current) return
      e.preventDefault()
      latest.current()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [enabled, editor])
}
