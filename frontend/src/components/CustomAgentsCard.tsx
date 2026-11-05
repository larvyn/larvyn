import { useState } from 'react'
import { Bot, Plus, Trash2 } from 'lucide-react'
import { type AgentKind } from '../../bindings/larvyn/internal/agents'
import { type CustomAgent } from '../../bindings/larvyn/internal/store'

/**
 * The agents the user added: a command that works like a built-in agent,
 * with its own environment. Saved with the rest of Settings.
 */
export function CustomAgentsCard({ value, builtins, onChange }: { value: CustomAgent[]; builtins: AgentKind[]; onChange: (v: CustomAgent[]) => void }) {
  const put = (i: number, a: CustomAgent) => onChange(value.map((x, j) => (j === i ? a : x)))
  const add = () => onChange([...value, { id: 'custom-' + Math.random().toString(36).slice(2, 10), name: '', base: 'claude', command: '' }])
  return (
    <div className="card" style={{ marginBottom: 18 }}>
      <div className="card-head">
        <h3>
          <Bot /> Agents
        </h3>
        <button className="btn sm" style={{ marginLeft: 'auto' }} onClick={add}>
          <Plus /> Add agent
        </button>
      </div>
      {value.length > 0 && (
        <div className="card-body" style={{ display: 'grid', gap: 18 }}>
          {value.map((a, i) => (
            <Row key={a.id} agent={a} builtins={builtins} onChange={(x) => put(i, x)} onRemove={() => onChange(value.filter((_, j) => j !== i))} />
          ))}
        </div>
      )}
    </div>
  )
}

function Row({ agent: a, builtins, onChange, onRemove }: { agent: CustomAgent; builtins: AgentKind[]; onChange: (a: CustomAgent) => void; onRemove: () => void }) {
  // The text as typed: a line without "=" yet is kept here, not in the map.
  const [env, setEnv] = useState(() => Object.entries(a.env ?? {}).map(([k, v]) => `${k}=${v}`).join('\n'))
  const base = builtins.find((b) => b.id === a.base)
  return (
    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr auto', gap: 12, alignItems: 'start' }}>
      <div className="field">
        <label>Name</label>
        <input className="input" placeholder={base?.name} value={a.name} onChange={(e) => onChange({ ...a, name: e.target.value })} />
      </div>
      <div className="field">
        <label>Works like</label>
        <select className="select" value={a.base} onChange={(e) => onChange({ ...a, base: e.target.value })}>
          {builtins.map((b) => (
            <option key={b.id} value={b.id}>
              {b.name}
            </option>
          ))}
        </select>
      </div>
      <button className="icon-btn danger" style={{ alignSelf: 'end' }} title="Remove" onClick={onRemove}>
        <Trash2 />
      </button>
      <div className="field">
        <label>Command</label>
        <input className="input mono" placeholder={base?.binary} value={a.command} onChange={(e) => onChange({ ...a, command: e.target.value })} />
      </div>
      <div className="field" style={{ gridColumn: '2 / 4' }}>
        <label>Environment</label>
        <textarea
          className="textarea mono"
          style={{ padding: '6px 10px', lineHeight: '16px', resize: 'none' }}
          rows={Math.max(1, env.split('\n').length)}
          placeholder="KEY=value"
          value={env}
          onChange={(e) => {
            setEnv(e.target.value)
            onChange({ ...a, env: parseEnv(e.target.value) })
          }}
        />
      </div>
    </div>
  )
}

function parseEnv(text: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const line of text.split('\n')) {
    const i = line.indexOf('=')
    if (i > 0) out[line.slice(0, i).trim()] = line.slice(i + 1).trim()
  }
  return out
}
