import { useEffect, useRef, useState } from 'react'
import { ArrowUpRight, GitBranch, Lock, Play, Power, Unplug } from 'lucide-react'
import { ProjectService, type FlockInfo } from '../../bindings/larvyn/internal/project'
import { SystemService } from '../../bindings/larvyn/internal/system'
import { refreshInfo, runInConsole, toast } from '../lib/store'
import { cx, errMsg } from '../lib/util'
import { Toggle, confirmDialog } from './ui'
import { Brand } from './Brand'

/** The project's site in Flock (flockphp.com), an optional local server.
 * Renders nothing when Flock is not installed. Every step is one click: set
 * Flock up, start it, serve the app at <folder>.test, then HTTPS, the PHP
 * version and the worktrees' URLs. `revision` reloads it when the project
 * info changes. */
export function FlockCard({ id, revision }: { id: string; revision: string }) {
  const [f, setF] = useState<FlockInfo | null>(null)
  const [busy, setBusy] = useState('')
  const alive = useRef(true)

  const load = () =>
    ProjectService.Flock(id)
      .then((v) => alive.current && setF(v))
      .catch(() => {})

  useEffect(() => {
    alive.current = true
    load()
    return () => {
      alive.current = false
    }
  }, [id, revision])

  // While a PHP version downloads, follow its progress.
  const installing = f?.php?.find((v) => v.job?.state === 'running')
  useEffect(() => {
    if (!installing) return
    const t = setInterval(load, 1000)
    return () => clearInterval(t)
  }, [!!installing])

  if (!f || !f.state) return null

  /** Runs one action with the card marked busy, then reloads. */
  const act = (label: string, run: () => Promise<unknown>, done?: (r: any) => void) => async () => {
    setBusy(label)
    try {
      const r = await run()
      done?.(r)
    } catch (e) {
      toast('error', `Flock: ${label} failed`, errMsg(e))
    } finally {
      setBusy('')
      await refreshInfo(id)
      load()
    }
  }

  const site = f.site
  const phpSource = site?.php_source === 'isolated' ? site.php : ''
  const phpLabel = (v: { minor: string; installed: string; default: boolean }) =>
    `PHP ${v.minor}${v.default ? ' · default' : ''}${v.installed ? '' : ' · download'}`

  return (
    <div className="card">
      <div className="card-head">
        <h3>
          <Brand id="flock" size={15} /> Local site
        </h3>
        <span className={cx('badge', f.state === 'running' ? 'green' : 'yellow')}>
          {f.state === 'running' ? `Flock ${f.version}` : f.state === 'stopped' ? 'Flock stopped' : 'Flock not set up'}
        </span>
        <span className="spacer" />
        {busy && <div className="spinner" style={{ width: 12, height: 12 }} />}
      </div>

      {f.state === 'installed' && (
        <Row
          title="Set up Flock"
          detail=".test domains, trusted HTTPS and PHP for every project. It asks for your password once."
          action={
            <button className="btn sm primary" onClick={() => runInConsole(id, [f.bin || 'flk', 'install'], 'flk install')}>
              <Play /> Set up
            </button>
          }
        />
      )}

      {f.state === 'stopped' && (
        <Row
          title="Flock is not running"
          detail="Start it to serve this app and its worktrees."
          action={
            <button className="btn sm primary" disabled={!!busy} onClick={act('start', () => ProjectService.FlockStart(), () => toast('success', 'Flock started'))}>
              <Power /> Start
            </button>
          }
        />
      )}

      {f.state === 'running' && !site && (
        <Row
          title={`Serve at ${f.domain}`}
          detail="Open the app without artisan serve, and every agent's worktree at its own URL. APP_URL follows when it was localhost."
          action={
            <button className="btn sm primary" disabled={!!busy} onClick={act('serve', () => ProjectService.FlockServe(id), (url) => toast('success', 'Served by Flock', url))}>
              <Play /> Serve
            </button>
          }
        />
      )}

      {f.state === 'running' && site && (
        <>
          <div className="check-row">
            <span className={cx('status-dot', site.problem ? 'err' : 'on')} />
            <div className="t">
              <b className="mono">{site.url}</b>
              <span>{site.problem || (site.parked ? 'From a project folder in Flock' : 'Linked in Flock')}</span>
            </div>
            <button className="btn sm ghost" onClick={() => SystemService.OpenURL(site.url!)}>
              Open <ArrowUpRight />
            </button>
          </div>

          <div className="check-row">
            <Lock style={{ width: 14, height: 14, color: 'var(--text-3)' }} />
            <div className="t">
              <b>HTTPS</b>
              <span>Trusted certificate from Flock's local authority</span>
            </div>
            <Toggle
              on={site.secured}
              disabled={!!busy}
              onChange={(on) => act(on ? 'HTTPS on' : 'HTTPS off', () => ProjectService.FlockSecure(id, on))()}
            />
          </div>

          <div className="check-row">
            <Brand id="php" size={14} />
            <div className="t">
              <b>PHP version</b>
              <span>
                {installing
                  ? `${installing.job!.phase === 'installing' ? 'Installing' : 'Downloading'} PHP ${installing.minor}…`
                  : site.php_source === 'project'
                    ? 'Pinned by the project (.flockrc)'
                    : site.php_source === 'isolated'
                      ? 'Set for this site on this machine'
                      : "Flock's default"}
              </span>
              {installing && <Progress value={installing.job!.progress} />}
            </div>
            <select
              className="select"
              style={{ width: 'auto', minWidth: 150 }}
              disabled={!!busy || !!installing}
              value={phpSource}
              onChange={(e) => {
                const minor = e.target.value
                act(`PHP ${minor || 'default'}`, () => ProjectService.FlockUsePHP(id, minor), () => toast('success', `PHP ${minor || 'default'} for ${site.domain}`))()
                // Downloads take a while: show their progress right away.
                setTimeout(load, 800)
              }}
            >
              <option value="">{site.php_source === 'isolated' ? 'Default' : `PHP ${site.php}`}</option>
              {(f.php ?? []).map((v) => (
                <option key={v.minor} value={v.minor}>
                  {phpLabel(v)}
                </option>
              ))}
            </select>
          </div>

          {(f.worktrees ?? []).length > 0 && (
            <>
              <div className="section-title" style={{ margin: '10px 14px 2px' }}>
                <GitBranch style={{ width: 12, height: 12 }} /> Worktrees
              </div>
              {f.worktrees!.map((w) => (
                <div key={w.name} className="check-row" style={{ padding: '7px 14px' }}>
                  <div className="t ellipsis">
                    <b className="mono" style={{ fontSize: 12 }}>
                      {w.branch || w.name}
                    </b>
                    <span className="mono">{w.domain}</span>
                  </div>
                  <button className="btn sm ghost" onClick={() => SystemService.OpenURL(w.url!)}>
                    Open <ArrowUpRight />
                  </button>
                </div>
              ))}
            </>
          )}

          {!site.parked && (
            <div className="check-row" style={{ borderTop: '1px solid var(--line)', justifyContent: 'flex-end' }}>
              <button
                className="btn sm ghost"
                disabled={!!busy}
                onClick={async () => {
                  if (!(await confirmDialog({ title: `Stop serving ${site.domain}?`, message: 'Flock stops serving the app and its worktrees. Nothing in the project changes, and you can serve it again any time.', confirm: 'Stop serving' })))
                    return
                  act('stop serving', () => ProjectService.FlockUnserve(id))()
                }}
              >
                <Unplug /> Stop serving
              </button>
            </div>
          )}
        </>
      )}
    </div>
  )
}

function Row({ title, detail, action }: { title: string; detail: string; action: React.ReactNode }) {
  return (
    <div className="check-row">
      <div className="t">
        <b>{title}</b>
        <span>{detail}</span>
      </div>
      {action}
    </div>
  )
}

function Progress({ value }: { value: number }) {
  return (
    <div style={{ height: 3, borderRadius: 2, background: 'var(--line)', marginTop: 6, overflow: 'hidden' }}>
      <div style={{ height: '100%', width: `${Math.round(Math.min(1, Math.max(0, value)) * 100)}%`, background: 'var(--accent)', transition: 'width .4s' }} />
    </div>
  )
}
