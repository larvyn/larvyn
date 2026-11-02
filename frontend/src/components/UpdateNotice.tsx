import { ArrowUpCircle, ExternalLink, X } from 'lucide-react'
import { SystemService } from '../../bindings/larvyn/internal/system'
import { dismissUpdate, installUpdate, useUpdate } from '../lib/update'

/** Top-right card shown when a newer release is out. */
export function UpdateNotice() {
  const info = useUpdate((s) => s.info)
  const dismissed = useUpdate((s) => s.dismissed)
  const installing = useUpdate((s) => s.installing)
  if (!info?.available || dismissed === info.latest) return null

  return (
    <div className="update-notice" role="status">
      <ArrowUpCircle className="ic" />
      <div className="grow">
        <b>Larvyn {info.latest} is available</b>
        <div className="msg">
          You have {info.current}.{' '}
          {installing ? 'Downloading and installing… Larvyn will restart on its own.' : info.canInstall ? 'Update now and Larvyn restarts in a few seconds.' : info.reason}
        </div>
        <div className="row" style={{ gap: 6, marginTop: 10 }}>
          {info.canInstall && (
            <button className="btn sm primary" disabled={installing} onClick={installUpdate}>
              {installing ? <span className="spinner" /> : <ArrowUpCircle />} {installing ? 'Updating…' : 'Update now'}
            </button>
          )}
          <button className="btn sm" disabled={installing} onClick={() => SystemService.OpenURL(info.notesUrl)}>
            <ExternalLink /> What's new
          </button>
        </div>
      </div>
      {!installing && (
        <button className="icon-btn sm" title="Later" onClick={dismissUpdate}>
          <X />
        </button>
      )}
    </div>
  )
}
