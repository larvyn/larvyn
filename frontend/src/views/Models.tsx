import { useEffect, useMemo, useState } from 'react'
import { ArrowRight, Box, Code2, EyeOff, FlaskConical, Lock, Plus, RefreshCw, Rows3, Shapes, Sparkles } from 'lucide-react'
import { LaravelService, SystemService } from '../../bindings/larvyn'
import { fail, handoff, useActive } from '../lib/store'
import { cx, num } from '../lib/util'
import { Empty, Loading, SearchInput, useAsync } from '../components/ui'

const REL_COLOR: Record<string, string> = {
  HasMany: 'green', HasOne: 'green', BelongsTo: 'blue', BelongsToMany: 'purple', MorphMany: 'orange', MorphTo: 'orange',
  MorphOne: 'orange', MorphToMany: 'orange', HasManyThrough: 'cyan', HasOneThrough: 'cyan',
}

export function Models() {
  const { id, info } = useActive()
  const res = useAsync(async () => {
    const r: any = await LaravelService.Bridge(id!, 'models', {})
    if (!r?.ok) throw new Error(r?.error?.message ?? 'Could not load models')
    return r.models as any[]
  }, [id])
  const [sel, setSel] = useState<string | null>(null)
  const [q, setQ] = useState('')
  const models = res.data ?? []
  useEffect(() => {
    if (!sel && models.length) setSel(models[0].class)
  }, [models])
  const model = models.find((m) => m.class === sel)
  const shown = models.filter((m) => !q || m.name.toLowerCase().includes(q.toLowerCase()))
  // Reverse relations: who points at the selected model.
  const inbound = useMemo(
    () => models.flatMap((m) => (m.relations ?? []).filter((r: any) => r.related === sel).map((r: any) => ({ from: m.name, ...r }))),
    [models, sel],
  )

  if (!info?.isLaravel) return <Empty icon={<Box />} title="Not a Laravel project" />
  if (!info.hasVendor) return <Empty icon={<Box />} title="Install dependencies first">Model inspection boots your app. Run composer install.</Empty>

  return (
    <div className="view flush">
      <div className="split">
        <div className="pane-list" style={{ width: 260 }}>
          <div className="row" style={{ padding: 10, borderBottom: '1px solid var(--line)' }}>
            <SearchInput value={q} onChange={setQ} placeholder="Filter models…" width="100%" />
            <button className="icon-btn" title="New model (make:model)" onClick={() => handoff('artisan', { artisan: 'make:model' })}>
              <Plus />
            </button>
            <button className="icon-btn" title="Reload models" onClick={res.reload}>
              <RefreshCw />
            </button>
          </div>
          <div className="list-scroll">
            {res.loading && <Loading />}
            {res.error && <div className="error-box" style={{ margin: 6 }}>{res.error}</div>}
            {shown.map((m) => (
              <button key={m.class} className={cx('list-item', sel === m.class && 'active')} onClick={() => setSel(m.class)}>
                <Box />
                <div className="grow">
                  <div style={{ fontWeight: 560 }}>{m.name}</div>
                  <div className="faint mono" style={{ fontSize: 10.5 }}>
                    {m.table ?? m.error}
                  </div>
                </div>
                <span className="faint" style={{ fontSize: 11 }}>
                  {m.count != null ? num(m.count) : ''}
                </span>
              </button>
            ))}
          </div>
        </div>
        <div className="pane-main" style={{ overflow: 'auto' }}>
          {model ? (
            <div className="view pad">
              <div className="page-head">
                <div className="grow">
                  <h1>{model.name}</h1>
                  <p className="mono" style={{ fontSize: 12 }}>
                    {model.class}
                  </p>
                </div>
                <button className="btn sm" onClick={() => SystemService.OpenInEditor(model.file, 1).catch(fail('Editor'))}>
                  <Code2 /> Open
                </button>
                <button className="btn sm" onClick={() => handoff('database', { table: model.table })}>
                  <Rows3 /> Browse {model.table}
                </button>
                <button className="btn sm" onClick={() => handoff('tinker', { tinker: `${model.class.replace(/^App\\Models\\/, '\\App\\Models\\')}::latest()->take(5)->get()` })}>
                  <FlaskConical /> Tinker
                </button>
              </div>
              {model.error && <div className="error-box">{model.error}</div>}
              <div className="stats" style={{ marginBottom: 6 }}>
                <div className="stat">
                  <div className="v">{model.count != null ? num(model.count) : '—'}</div>
                  <div className="k">Records</div>
                </div>
                <div className="stat">
                  <div className="v">{model.relations?.length ?? 0}</div>
                  <div className="k">Relations</div>
                </div>
                <div className="stat">
                  <div className="v">{model.fillable?.length ?? 0}</div>
                  <div className="k">Fillable</div>
                </div>
                <div className="stat">
                  <div className="v" style={{ fontSize: 15, paddingTop: 6 }}>
                    {model.primaryKey}
                  </div>
                  <div className="k">Primary key</div>
                </div>
              </div>
              <div className="row wrap" style={{ gap: 6, marginTop: 10 }}>
                {model.timestamps && <span className="badge">timestamps</span>}
                {model.softDeletes && <span className="badge orange">soft deletes</span>}
                {model.connection && <span className="badge blue">{model.connection}</span>}
                {model.traits?.map((t: string) => (
                  <span key={t} className="badge purple">
                    {t}
                  </span>
                ))}
              </div>

              <div className="section-title">
                <Shapes size={13} /> Relationships
              </div>
              {(model.relations?.length ?? 0) + inbound.length === 0 ? (
                <div className="faint">No relationship methods with return types found.</div>
              ) : (
                <div className="card">
                  {model.relations?.map((r: any) => (
                    <div key={r.name} className="check-row">
                      <span className={cx('badge', REL_COLOR[r.type])} style={{ minWidth: 110, justifyContent: 'center' }}>
                        {r.type}
                      </span>
                      <span className="mono" style={{ fontWeight: 600 }}>
                        {r.name}()
                      </span>
                      <ArrowRight size={13} className="faint" />
                      {r.related ? (
                        <button className="link mono" style={{ background: 'none', border: 0, padding: 0 }} onClick={() => setSel(r.related)}>
                          {r.related.split('\\').pop()}
                        </button>
                      ) : (
                        <span className="faint">?</span>
                      )}
                    </div>
                  ))}
                  {inbound.map((r: any) => (
                    <div key={r.from + r.name} className="check-row" style={{ opacity: 0.75 }}>
                      <span className="badge" style={{ minWidth: 110, justifyContent: 'center' }}>
                        inbound
                      </span>
                      <button className="link mono" style={{ background: 'none', border: 0, padding: 0 }} onClick={() => setSel(models.find((m) => m.name === r.from)?.class)}>
                        {r.from}
                      </button>
                      <span className="mono faint">::{r.name}()</span>
                      <span className={cx('badge', REL_COLOR[r.type])}>{r.type}</span>
                    </div>
                  ))}
                </div>
              )}

              <ModelAttributes projectId={id!} cls={model.class} model={model} />
            </div>
          ) : !res.loading && models.length === 0 ? (
            <Empty icon={<Sparkles />} title="No models yet">
              Create one with make:model, or ask an agent.
              <div style={{ marginTop: 12 }}>
                <button className="btn primary" onClick={() => handoff('artisan', { artisan: 'make:model' })}>
                  <Plus /> New model
                </button>
              </div>
            </Empty>
          ) : null}
        </div>
      </div>
    </div>
  )
}

function ModelAttributes({ projectId, cls, model }: { projectId: string; cls: string; model: any }) {
  const res = useAsync(() => LaravelService.ModelDetail(projectId, cls) as Promise<any>, [projectId, cls])
  const attrs: any[] = res.data?.attributes ?? []
  return (
    <>
      <div className="section-title">Attributes</div>
      {res.loading ? (
        <div className="skeleton" style={{ height: 120 }} />
      ) : res.error ? (
        <div className="card">
          <div className="card-body row wrap" style={{ gap: 6 }}>
            <span className="faint" style={{ width: '100%', fontSize: 12 }}>
              model:show failed ({res.error.slice(0, 120)}); showing model properties instead.
            </span>
            {model.fillable?.map((f: string) => (
              <span key={f} className="badge mono">
                {f}
              </span>
            ))}
          </div>
        </div>
      ) : (
        <div className="card" style={{ overflow: 'hidden' }}>
          <table className="grid">
            <thead>
              <tr>
                <th>Attribute</th>
                <th>Type</th>
                <th>Cast</th>
                <th>Flags</th>
              </tr>
            </thead>
            <tbody>
              {attrs.map((a) => (
                <tr key={a.name}>
                  <td className="mono" style={{ fontWeight: 600 }}>
                    {a.name}
                  </td>
                  <td className="mono" style={{ color: 'var(--cyan)', fontSize: 12 }}>
                    {a.type ?? ''}
                  </td>
                  <td className="mono" style={{ fontSize: 12, color: 'var(--purple)' }}>
                    {a.cast ?? ''}
                  </td>
                  <td>
                    <div className="row" style={{ gap: 4 }}>
                      {a.fillable && <span className="badge green">fillable</span>}
                      {a.hidden && (
                        <span className="badge">
                          <EyeOff /> hidden
                        </span>
                      )}
                      {a.unique && <span className="badge purple">unique</span>}
                      {a.increments && <span className="badge yellow">increments</span>}
                      {a.nullable && <span className="badge">nullable</span>}
                      {a.appended && <span className="badge blue">appended</span>}
                      {!a.fillable && !a.increments && model.guarded?.includes?.('*') && (
                        <span className="badge faint">
                          <Lock /> guarded
                        </span>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}
