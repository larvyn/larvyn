import { useStore } from './store'

/** The theme the app is drawn in. */
export type Theme = 'light' | 'dark'

// The chosen preference ("light", "dark" or "" for the system), kept so the
// first paint of the next launch is already right instead of flashing dark
// while the settings load.
const KEY = 'larvyn.theme'
const media = window.matchMedia('(prefers-color-scheme: light)')
const listeners = new Set<(t: Theme) => void>()

let pref = ''
try {
  pref = localStorage.getItem(KEY) ?? ''
} catch {}

/** The theme in use now. */
export function currentTheme(): Theme {
  return document.documentElement.dataset.theme === 'light' ? 'light' : 'dark'
}

/** Calls fn whenever the theme changes; returns the unsubscribe. */
export function onThemeChange(fn: (t: Theme) => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

function apply() {
  const t: Theme = pref === 'light' || pref === 'dark' ? pref : media.matches ? 'light' : 'dark'
  if (document.documentElement.dataset.theme === t) return
  document.documentElement.dataset.theme = t
  for (const fn of listeners) fn(t)
}

/** Sets the preference: "light", "dark", or anything else to follow the system. */
export function setThemePref(p: string) {
  pref = p === 'light' || p === 'dark' ? p : ''
  try {
    localStorage.setItem(KEY, pref)
  } catch {}
  apply()
}

apply()
media.addEventListener('change', apply)
useStore.subscribe((s, prev) => {
  if (s.settings && s.settings.theme !== prev.settings?.theme) setThemePref(s.settings.theme)
})
