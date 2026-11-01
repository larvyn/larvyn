import type { CSSProperties } from 'react'

// Logos in src/assets/brands, by file name (see CREDITS.md there).
const FILES = import.meta.glob('../assets/brands/*.svg', { eager: true, query: '?url', import: 'default' }) as Record<string, string>
const URLS: Record<string, string> = {}
for (const [path, url] of Object.entries(FILES)) URLS[path.split('/').pop()!.replace(/\.svg$/, '')] = url

// Names the app shows (package labels, frontend chips) mapped to a logo file.
const ALIASES: Record<string, string> = {
  'inertia react': 'inertia',
  'inertia vue': 'inertia',
  'laravel installer': 'laravel',
  'node.js': 'node',
  postgres: 'postgresql',
  pgsql: 'postgresql',
  'cursor agent': 'cursor-agent',
  'claude code': 'claude',
  'antigravity cli': 'antigravity',
}

/** The logo URL for an agent id, tool id or label, if Larvyn has one. */
export function brandUrl(key?: string | null): string | undefined {
  if (!key) return undefined
  const k = key.toLowerCase()
  return URLS[k] ?? URLS[ALIASES[k] ?? '']
}

/** A brand logo, or the fallback when there is none for this key. */
export function Brand({ id, size = 14, fallback = null, style }: { id?: string | null; size?: number; fallback?: React.ReactNode; style?: CSSProperties }) {
  const url = brandUrl(id)
  if (!url) return <>{fallback}</>
  return <img src={url} alt="" width={size} height={size} draggable={false} style={{ objectFit: 'contain', flexShrink: 0, ...style }} />
}
