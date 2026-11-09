import { Fragment } from 'react'
import { ShieldCheck } from 'lucide-react'

/** What Larvyn connects to, and why. Keep it in step with the README's Privacy section. */
const CONNECTIONS: [string, string][] = [
  ['Updates', 'Asks GitHub for the newest release a few seconds after it starts and every 6 hours, and downloads it when you update. The checkbox above turns the check off.'],
  ['Plugins', 'Reads the marketplace list from GitHub when it starts, to turn off plugins found harmful, and when you open Settings. A plugin is downloaded only when you install it, and connects only to the domains its network permission lists.'],
  ['Composer', "Only when it isn't on your PATH: downloads its own from getcomposer.org and checks its signature."],
  ['Project icons', "For a project with a GitHub remote, the owner's avatar from github.com, once a week at most."],
  ['Feedback', 'Bug reports and feature requests open a form on GitHub in your browser; no report is sent until you submit the form there.'],
]

export function PrivacyCard() {
  return (
    <div className="card" style={{ marginTop: 18 }}>
      <div className="card-head">
        <h3>
          <ShieldCheck /> Privacy
        </h3>
      </div>
      <div className="card-body">
        <p style={{ margin: '0 0 12px', fontSize: 12.5, color: 'var(--text-2)' }}>
          Larvyn is free and has no telemetry, accounts or analytics. Larvyn itself sends nothing about your projects, code, .env files or terminals. It
          connects to the internet only for these:
        </p>
        <div className="kv">
          {CONNECTIONS.map(([what, why]) => (
            <Fragment key={what}>
              <div>{what}</div>
              <div style={{ whiteSpace: 'normal' }}>{why}</div>
            </Fragment>
          ))}
        </div>
        <span style={{ display: 'block', marginTop: 10, fontSize: 11.5, color: 'var(--text-3)' }}>
          The agents you start and the tools Larvyn runs for you are other programs, with their own rules: an agent sends your prompts and the code it
          reads to its provider's service, and composer outdated and audit in the Dependencies view ask Packagist.
        </span>
      </div>
    </div>
  )
}
