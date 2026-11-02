import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, Braces, Eye, EyeOff, FileText, Plus, RotateCcw, Save, Rows3 } from 'lucide-react'
import { LaravelService, type EnvFile } from '../../bindings/larvyn/internal/laravel'
import { refreshInfo, toast, useActive } from '../lib/store'
import { cx, errMsg } from '../lib/util'
import { Empty, Loading, SearchInput, promptDialog } from '../components/ui'

const SECRET = /(KEY|SECRET|PASSWORD|TOKEN|PRIVATE|DSN)/i
const GROUP_LABEL: Record<string, string> = {
  APP: 'Application', DB: 'Database', MAIL: 'Mail', REDIS: 'Redis', CACHE: 'Cache', QUEUE: 'Queue', SESSION: 'Session',
  AWS: 'AWS', LOG: 'Logging', BROADCAST: 'Broadcasting', VITE: 'Vite', PUSHER: 'Pusher', REVERB: 'Reverb', FILESYSTEM: 'Filesystem',
}

interface Line {
  raw: string
  key?: string
  value?: string
}

function parse(raw: string): Line[] {
  return raw.split(/\r?\n/).map((l) => {
    const m = l.match(/^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_.]*)\s*=(.*)$/)
    if (!m) return { raw: l }
    let v = m[2].trim()
    if ((v.startsWith('"') && v.endsWith('"')) || (v.startsWith("'") && v.endsWith("'"))) v = v.slice(1, -1)
    return { raw: l, key: m[1], value: v }
  })
}

function serialize(lines: Line[]) {
  return lines
    .map((l) => {
      if (!l.key) return l.raw
      const v = l.value ?? ''
      return `${l.key}=${/[\s#"'=$]/.test(v) && !/^\$\{/.test(v) ? `"${v.replace(/"/g, '\\"')}"` : v}`
    })
    .join('\n')
}

export function Env() {
  const { id } = useActive()
  const [name, setName] = useState('.env')
  const [file, setFile] = useState<EnvFile | null>(null)
  const [lines, setLines] = useState<Line[]>([])
  const [raw, setRaw] = useState('')
  const [mode, setMode] = useState<'form' | 'raw'>('form')
  const [q, setQ] = useState('')
  const [reveal, setReveal] = useState<Record<string, boolean>>({})
  const [dirty, setDirty] = useState(false)
  const [error, setError] = useState('')

  const load = () =>
    LaravelService.Env(id!, name)
      .then((f) => {
        setFile(f)
        setLines(parse(f.raw))
        setRaw(f.raw)
        setDirty(false)
        setError('')
      })
      .catch((e) => setError(errMsg(e)))
  useEffect(() => {
    if (id) load()
  }, [id, name])

  const text = mode === 'raw' ? raw : serialize(lines)
  const save = async () => {
    try {
      await LaravelService.SaveEnv(id!, name, text)
      toast('success', `${name} saved`, 'A backup of the previous version was kept.')
      await load()
      refreshInfo(id!)
    } catch (e) {
      toast('error', 'Could not save', errMsg(e))
    }
  }
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === 's' && dirty) {
        e.preventDefault()
        save()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  const groups = useMemo(() => {
    const map = new Map<string, number[]>()
    lines.forEach((l, i) => {
      if (!l.key) return
      if (q && !`${l.key} ${l.value}`.toLowerCase().includes(q.toLowerCase())) return
      const g = l.key.split('_')[0]
      const k = GROUP_LABEL[g] ? g : 'OTHER'
      map.set(k, [...(map.get(k) ?? []), i])
    })
    return [...map.entries()].sort((a, b) => (a[0] === 'APP' ? -1 : b[0] === 'APP' ? 1 : a[0] === 'OTHER' ? 1 : b[0] === 'OTHER' ? -1 : 0))
  }, [lines, q])

  const setVal = (i: number, v: string) => {
    setLines((ls) => ls.map((l, j) => (j === i ? { ...l, value: v } : l)))
    setDirty(true)
  }
  const addKey = (k: string) => {
    setLines((ls) => [...ls.filter((l, i) => !(i === ls.length - 1 && l.raw === '')), { raw: '', key: k, value: '' }, { raw: '' }])
    setDirty(true)
  }

  if (error) return <div className="error-box" style={{ margin: 20 }}>{error}</div>
  if (!file) return <Loading />

  return (
    <div className="view flush">
      <div className="toolbar">
        <h1>
          <Braces /> Environment
        </h1>
        <select className="select" value={name} onChange={(e) => setName(e.target.value)} style={{ width: 170 }}>
          {(file.files?.length ? file.files : ['.env']).map((f) => (
            <option key={f}>{f}</option>
          ))}
        </select>
        {mode === 'form' && <SearchInput value={q} onChange={setQ} placeholder="Filter keys…" width={220} />}
        <span className="spacer" />
        <div className="tabs">
          <button
            className={cx('tab', mode === 'form' && 'active')}
            onClick={() => {
              if (mode === 'raw') setLines(parse(raw))
              setMode('form')
            }}
          >
            <Rows3 /> Form
          </button>
          <button
            className={cx('tab', mode === 'raw' && 'active')}
            onClick={() => {
              if (mode === 'form') setRaw(serialize(lines))
              setMode('raw')
            }}
          >
            <FileText /> Raw
          </button>
        </div>
        <button className="btn sm ghost" disabled={!dirty} onClick={load}>
          <RotateCcw /> Revert
        </button>
        <button className="btn sm primary" disabled={!dirty} onClick={save}>
          <Save /> Save <kbd style={{ background: 'rgba(0,0,0,.2)', color: '#fff', borderColor: 'rgba(255,255,255,.25)' }}>Ctrl S</kbd>
        </button>
      </div>
      {!file.exists ? (
        <Empty icon={<Braces />} title={`${name} does not exist`}>
          Create it from the Doctor on the Overview page, or switch to Raw and save.
        </Empty>
      ) : mode === 'raw' ? (
        <div className="editor">
          <textarea
            spellCheck={false}
            value={raw}
            onChange={(e) => {
              setRaw(e.target.value)
              setDirty(true)
            }}
            style={{ paddingLeft: 18 }}
          />
        </div>
      ) : (
        <div className="view pad" style={{ paddingTop: 16 }}>
          {!!file.missing?.length && name === '.env' && (
            <div className="card" style={{ marginBottom: 16, borderColor: 'rgba(245,197,66,.3)' }}>
              <div className="card-head">
                <h3 style={{ color: 'var(--yellow)' }}>
                  <AlertTriangle style={{ color: 'var(--yellow)' }} /> {file.missing.length} key{file.missing.length > 1 ? 's' : ''} from .env.example missing here
                </h3>
              </div>
              <div className="card-body row wrap" style={{ gap: 6 }}>
                {file.missing.map((k) => (
                  <button key={k} className="chip mono" onClick={() => addKey(k)} disabled={lines.some((l) => l.key === k)}>
                    <Plus /> {k}
                  </button>
                ))}
              </div>
            </div>
          )}
          {groups.map(([g, idx]) => (
            <div key={g} style={{ marginBottom: 18 }}>
              <div className="section-title" style={{ marginTop: 0 }}>
                {GROUP_LABEL[g] ?? 'Other'} <span className="pill-n">{idx.length}</span>
              </div>
              <div className="card">
                {idx.map((i) => {
                  const l = lines[i]
                  const secret = SECRET.test(l.key!)
                  const shown = !secret || reveal[l.key!]
                  return (
                    <div key={i} className="row" style={{ padding: '6px 10px 6px 14px', borderBottom: '1px solid var(--line)' }}>
                      <span className="mono" style={{ width: 240, fontSize: 12, color: 'var(--text-2)', flexShrink: 0 }} title={l.key}>
                        {l.key}
                      </span>
                      <input
                        className="input mono grow"
                        style={{ fontSize: 12 }}
                        type={shown ? 'text' : 'password'}
                        value={l.value ?? ''}
                        onChange={(e) => setVal(i, e.target.value)}
                        spellCheck={false}
                      />
                      {secret ? (
                        <button className="icon-btn sm" onClick={() => setReveal((r) => ({ ...r, [l.key!]: !r[l.key!] }))}>
                          {shown ? <EyeOff /> : <Eye />}
                        </button>
                      ) : (
                        <span style={{ width: 22 }} />
                      )}
                    </div>
                  )
                })}
              </div>
            </div>
          ))}
          <button
            className="btn sm"
            onClick={async () => {
              const k = await promptDialog({ title: 'Add variable', label: 'Key', placeholder: 'STRIPE_SECRET', confirm: 'Add' })
              if (k) addKey(k.trim().toUpperCase().replace(/[^A-Z0-9_]/g, '_'))
            }}
          >
            <Plus /> Add variable
          </button>
        </div>
      )}
    </div>
  )
}
