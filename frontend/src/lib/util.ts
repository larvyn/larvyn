export function errMsg(e: unknown): string {
  if (!e) return 'Unknown error'
  if (typeof e === 'string') return e
  const anyE = e as any
  if (anyE.message) {
    // Wails wraps Go errors as JSON in the message.
    try {
      const parsed = JSON.parse(anyE.message)
      if (parsed?.message) return parsed.message
    } catch {}
    return anyE.message
  }
  return String(e)
}

export function cx(...parts: (string | false | null | undefined)[]) {
  return parts.filter(Boolean).join(' ')
}

export function initials(name: string) {
  const words = name.replace(/[^A-Za-z0-9 ]/g, ' ').split(/\s+/).filter(Boolean)
  if (words.length === 0) return '?'
  if (words.length === 1) return words[0].slice(0, 2).toUpperCase()
  return (words[0][0] + words[1][0]).toUpperCase()
}

export function ago(ms: number) {
  if (!ms) return ''
  const s = Math.max(0, (Date.now() - ms) / 1000)
  if (s < 45) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)}m ago`
  if (s < 86400) return `${Math.round(s / 3600)}h ago`
  if (s < 86400 * 30) return `${Math.round(s / 86400)}d ago`
  return new Date(ms).toLocaleDateString()
}

export function duration(ms: number) {
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${s % 60}s`
  return `${Math.floor(m / 60)}h ${m % 60}m`
}

export function bytes(n: number | null | undefined) {
  if (n == null) return '—'
  if (n < 1024) return `${n} B`
  if (n < 1024 ** 2) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 ** 3) return `${(n / 1024 ** 2).toFixed(1)} MB`
  return `${(n / 1024 ** 3).toFixed(2)} GB`
}

export function num(n: number | null | undefined) {
  if (n == null) return '—'
  return new Intl.NumberFormat().format(n)
}

export function b64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

export const isMac = navigator.platform.toLowerCase().includes('mac')
export const mod = isMac ? '⌘' : 'Ctrl'

export function copy(text: string) {
  return navigator.clipboard.writeText(text)
}

export function load<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem('larvyn:' + key)
    return raw == null ? fallback : (JSON.parse(raw) as T)
  } catch {
    return fallback
  }
}

export function save(key: string, value: unknown) {
  try {
    localStorage.setItem('larvyn:' + key, JSON.stringify(value))
  } catch {}
}

/** Shell-like split that honours quotes, for "artisan …" lines typed by hand. */
export function splitArgs(line: string): string[] {
  const out: string[] = []
  let cur = ''
  let quote: string | null = null
  let has = false
  for (const ch of line) {
    if (quote) {
      if (ch === quote) quote = null
      else cur += ch
    } else if (ch === '"' || ch === "'") {
      quote = ch
      has = true
    } else if (/\s/.test(ch)) {
      if (cur || has) out.push(cur)
      cur = ''
      has = false
    } else {
      cur += ch
    }
  }
  if (cur || has) out.push(cur)
  return out
}

/** Cron expression in words for the common shapes Laravel produces. */
export function cronText(expr: string): string {
  const map: Record<string, string> = {
    '* * * * *': 'Every minute',
    '*/2 * * * *': 'Every 2 minutes',
    '*/5 * * * *': 'Every 5 minutes',
    '*/10 * * * *': 'Every 10 minutes',
    '*/15 * * * *': 'Every 15 minutes',
    '*/30 * * * *': 'Every 30 minutes',
    '0 * * * *': 'Hourly',
    '0 0 * * *': 'Daily at midnight',
    '0 0 * * 0': 'Weekly on Sunday',
    '0 0 1 * *': 'Monthly',
    '0 0 1 1 *': 'Yearly',
  }
  if (map[expr]) return map[expr]
  const m = expr.match(/^(\d+) (\d+) \* \* \*$/)
  if (m) return `Daily at ${m[2].padStart(2, '0')}:${m[1].padStart(2, '0')}`
  const h = expr.match(/^(\d+) \* \* \* \*$/)
  if (h) return `Hourly at :${h[1].padStart(2, '0')}`
  return expr
}
