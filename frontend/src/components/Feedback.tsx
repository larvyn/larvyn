import { useEffect, useRef, useState } from 'react'
import { Bug, Lightbulb, MessageCircle, MessageSquareHeart } from 'lucide-react'
import { SystemService } from '../../bindings/larvyn'
import { fail } from '../lib/store'

export const DISCUSSIONS_URL = 'https://github.com/larvyn/releases/discussions'

export const reportBug = () => SystemService.ReportBug('').catch(fail('Could not open the browser'))
export const requestFeature = () => SystemService.RequestFeature().catch(fail('Could not open the browser'))
export const openDiscussions = () => SystemService.OpenURL(DISCUSSIONS_URL).catch(fail('Could not open the browser'))

const ITEMS = [
  { icon: Bug, label: 'Report a bug', hint: 'Version and system are filled in', run: reportBug },
  { icon: Lightbulb, label: 'Request a feature', hint: 'Tell us what would help', run: requestFeature },
  { icon: MessageCircle, label: 'Ask or share an idea', hint: 'Beta discussions', run: openDiscussions },
]

/** Status bar button: bug reports, feature requests and discussions, one click away. */
export function FeedbackButton() {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent | KeyboardEvent) => {
      if (e instanceof KeyboardEvent ? e.key === 'Escape' : !ref.current?.contains(e.target as Node)) setOpen(false)
    }
    window.addEventListener('mousedown', close)
    window.addEventListener('keydown', close)
    return () => {
      window.removeEventListener('mousedown', close)
      window.removeEventListener('keydown', close)
    }
  }, [open])

  return (
    <div ref={ref} style={{ position: 'relative' }}>
      <button className="sb-item feedback-btn" onClick={() => setOpen(!open)} aria-expanded={open}>
        <MessageSquareHeart />
        Feedback
      </button>
      {open && (
        <div className="feedback-menu" role="menu">
          <div className="fm-head">Larvyn is in beta. Thanks for helping!</div>
          {ITEMS.map(({ icon: Icon, label, hint, run }) => (
            <button
              key={label}
              role="menuitem"
              className="fm-item"
              onClick={() => {
                setOpen(false)
                run()
              }}
            >
              <Icon />
              <span className="grow">
                <b>{label}</b>
                <small>{hint}</small>
              </span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

/** Title bar buttons: the most visible spot, one click to report or suggest. */
export function TitleFeedback() {
  return (
    <div className="tb-feedback">
      <button className="tb-report" onClick={reportBug} title="Report a bug: opens GitHub with your version and system filled in">
        <Bug />
        <span>Report a bug</span>
      </button>
      <button className="tb-idea" onClick={requestFeature} title="Request a feature">
        <Lightbulb />
      </button>
    </div>
  )
}
