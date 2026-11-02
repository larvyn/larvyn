import { useMemo } from 'react'
import { ArrowUpCircle, ExternalLink, Package, RefreshCw, ShieldAlert, ShieldCheck } from 'lucide-react'
import { SystemService } from '../../bindings/larvyn'
import { LaravelService } from '../../bindings/larvyn/internal/laravel'
import { composerRun, useActive } from '../lib/store'
import { cx } from '../lib/util'
import { Empty, Loading, useAsync } from '../components/ui'

interface Outdated {
  name: string
  version: string
  latest: string
  'latest-status': 'up-to-date' | 'semver-safe-update' | 'update-possible'
  description?: string
  'release-age'?: string
  'latest-release-date'?: string
  abandoned?: boolean | string
}
interface Advisory {
  advisoryId: string
  packageName: string
  title: string
  cve: string | null
  link: string
  severity: string | null
  reportedAt: string
}

const SEVERITY: Record<string, string> = { critical: 'red', high: 'red', medium: 'orange', low: 'yellow' }

export function Deps() {
  const { id, info } = useActive()
  const outdated = useAsync(() => LaravelService.Outdated(id!) as Promise<any>, [id])
  const audit = useAsync(() => LaravelService.Audit(id!) as Promise<any>, [id])
  const packages: Outdated[] = useMemo(() => (outdated.data?.installed ?? []).filter((p: Outdated) => p['latest-status'] !== 'up-to-date'), [outdated.data])
  const advisories: Advisory[] = useMemo(() => Object.values((audit.data?.advisories ?? {}) as Record<string, Advisory[]>).flat(), [audit.data])
  const safe = packages.filter((p) => p['latest-status'] === 'semver-safe-update')

  if (!info?.isLaravel && !info?.composerScripts) return <Empty icon={<Package />} title="No composer.json here" />

  return (
    <div className="view pad">
      <div className="page-head">
        <div className="grow">
          <h1>Dependencies</h1>
          <p>Direct Composer packages with newer releases, and known security advisories for the locked versions.</p>
        </div>
        <button className="btn" disabled={!safe.length} onClick={() => composerRun(id!, ['update', ...safe.map((p) => p.name), '-W'])}>
          <ArrowUpCircle /> Apply {safe.length} safe update{safe.length === 1 ? '' : 's'}
        </button>
        <button className="icon-btn" title="Check again" onClick={() => (outdated.reload(), audit.reload())}>
          <RefreshCw />
        </button>
      </div>

      <div className="card" style={{ marginBottom: 18, borderColor: advisories.length ? 'rgba(255,90,82,.35)' : undefined }}>
        <div className="card-head">
          <h3 style={advisories.length ? { color: 'var(--red)' } : undefined}>
            {advisories.length ? <ShieldAlert style={{ color: 'var(--red)' }} /> : <ShieldCheck style={{ color: 'var(--green)' }} />} Security audit
          </h3>
          {audit.data && (advisories.length ? <span className="badge red">{advisories.length} advisor{advisories.length === 1 ? 'y' : 'ies'}</span> : <span className="badge green">no known vulnerabilities</span>)}
        </div>
        {audit.loading ? (
          <div className="check-row faint">
            <div className="spinner" /> Running composer audit…
          </div>
        ) : audit.error ? (
          <div className="error-box" style={{ margin: 14 }}>{audit.error}</div>
        ) : (
          advisories.map((a) => (
            <div key={a.advisoryId} className="check-row">
              <span className={cx('badge', SEVERITY[a.severity ?? ''] ?? '')} style={{ minWidth: 64, justifyContent: 'center', textTransform: 'capitalize' }}>
                {a.severity ?? 'unknown'}
              </span>
              <div className="t">
                <b>{a.title}</b>
                <span className="mono">
                  {a.packageName}
                  {a.cve && ` · ${a.cve}`} · reported {a.reportedAt?.slice(0, 10)}
                </span>
              </div>
              <button className="btn sm" onClick={() => composerRun(id!, ['update', a.packageName, '-W'])}>
                <ArrowUpCircle /> Update
              </button>
              {a.link && (
                <button className="icon-btn sm" title="Advisory" onClick={() => SystemService.OpenURL(a.link)}>
                  <ExternalLink />
                </button>
              )}
            </div>
          ))
        )}
      </div>

      <div className="card" style={{ overflow: 'hidden' }}>
        <div className="card-head">
          <h3>
            <Package /> Outdated packages
          </h3>
          {outdated.data && <span className="pill-n">{packages.length}</span>}
        </div>
        {outdated.loading ? (
          <Loading label="Asking Packagist for newer versions…" />
        ) : outdated.error ? (
          <div className="error-box" style={{ margin: 14 }}>{outdated.error}</div>
        ) : packages.length === 0 ? (
          <div className="check-row faint">Every direct dependency is on its latest version.</div>
        ) : (
          <table className="grid">
            <thead>
              <tr>
                <th>Package</th>
                <th>Installed</th>
                <th>Latest</th>
                <th>Kind</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {packages.map((p) => {
                const major = p['latest-status'] === 'update-possible'
                return (
                  <tr key={p.name}>
                    <td>
                      <div className="mono" style={{ fontWeight: 600, fontSize: 12 }}>
                        {p.name}
                      </div>
                      {p.description && (
                        <div className="faint ellipsis" style={{ fontSize: 11, maxWidth: 420 }}>
                          {p.description}
                        </div>
                      )}
                    </td>
                    <td className="mono faint" style={{ fontSize: 12 }}>
                      {p.version}
                    </td>
                    <td className="mono" style={{ fontSize: 12, color: major ? 'var(--orange)' : 'var(--green)' }}>
                      {p.latest}
                    </td>
                    <td>
                      {major ? <span className="badge orange">major: read the upgrade guide</span> : <span className="badge green">safe update</span>}
                      {p.abandoned && <span className="badge red" style={{ marginLeft: 4 }}>abandoned</span>}
                    </td>
                    <td style={{ textAlign: 'right' }}>
                      <button className="btn sm" onClick={() => composerRun(id!, major ? ['require', `${p.name}:^${p.latest.replace(/^v/, '').split('.')[0]}`, '-W'] : ['update', p.name, '-W'])}>
                        <ArrowUpCircle /> {major ? 'Upgrade' : 'Update'}
                      </button>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
