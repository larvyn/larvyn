// A typed client for window.larvyn, the bridge Larvyn puts in every plugin
// view. Each call goes to Larvyn, which checks it against the permissions in
// larvyn.json before doing anything; a refused call rejects with an Error.
//
// The table below is Larvyn's view API v1. Keep it next to the docs:
// https://github.com/larvyn/larvyn/blob/master/examples/plugins/README.md

export interface Project {
  id: string
  name: string
  path: string
}

export interface FetchResult {
  status: number
  contentType: string
  body: string
}

export interface Session {
  sessionId: string
}

/** Every method a view can call: its params, what it returns, and the
 * permission larvyn.json must list for it. */
export interface Methods {
  /** permission: project.read */
  'project.info': { params: void; result: Project }
  /** permission: project.env.read. The whole .env, as an object. */
  'project.env.read': { params: void; result: Record<string, string> }
  /** permission: project.env.keys. The keys listed in larvyn.json's `env`. */
  'project.env.get': { params: void; result: Record<string, string | null> }
  /** permission: project.env.keys. Writes listed keys only, after a backup. */
  'project.env.set': { params: { values: Record<string, string> }; result: true }
  /** permission: ui.notify */
  'ui.notify': { params: { message: string }; result: true }
  /** permission: storage. A JSON value, or null if nothing is stored. */
  'storage.get': { params: { key: string }; result: unknown }
  /** permission: storage. null deletes the key. 1 MB per plugin. */
  'storage.set': { params: { key: string; value: unknown }; result: true }
  /** permission: network. GET, only to the hosts larvyn.json lists. 2 MB. */
  'network.fetch': { params: { url: string }; result: FetchResult }
  /** permission: terminal.run. One of larvyn.json's `commands`, by id. */
  'terminal.run': { params: { id: string }; result: Session }
  /** permission: artisan.run. `php artisan ...args` in a terminal. */
  'artisan.run': { params: { args: [string, ...string[]] }; result: Session }
  /** permission: stack.control. A project service, or your own stack item. */
  'stack.start': { params: { key: string }; result: Session }
  /** permission: stack.control */
  'stack.restart': { params: { key: string }; result: Session }
  /** permission: stack.control */
  'stack.stop': { params: { key: string }; result: true }
}

export type Method = keyof Methods

type Args<M extends Method> = Methods[M]['params'] extends void ? [] : [params: Methods[M]['params']]

interface Bridge {
  call(method: string, params?: object): Promise<unknown>
}

declare global {
  interface Window {
    /** Only set inside Larvyn. */
    larvyn?: Bridge
  }
}

/** True when the page runs inside Larvyn, false in a plain browser
 * (`npm run dev`), where calls go to the stand-in below. */
export const inLarvyn = typeof window !== 'undefined' && !!window.larvyn

/** Calls Larvyn: `await call('project.info')`,
 * `await call('ui.notify', { message: 'Hi' })`. */
export function call<M extends Method>(method: M, ...args: Args<M>): Promise<Methods[M]['result']> {
  const bridge = window.larvyn ?? standIn
  return bridge.call(method, args[0] ?? {}) as Promise<Methods[M]['result']>
}

// standIn answers calls when the page is opened outside Larvyn, so the UI
// can be built with Vite's dev server. Change its answers to suit your view.
const stored = new Map<string, unknown>()
const standIn: Bridge = {
  async call(method, params = {}) {
    const p = params as Record<string, unknown>
    console.info('[larvyn stand-in]', method, p)
    switch (method) {
      case 'project.info':
        return { id: 'dev', name: 'demo-app', path: '/Users/you/Code/demo-app' } satisfies Project
      case 'project.env.read':
      case 'project.env.get':
        return { APP_NAME: 'Demo', APP_ENV: 'local' }
      case 'storage.get':
        return stored.get(String(p.key)) ?? null
      case 'storage.set':
        if (p.value === null) stored.delete(String(p.key))
        else stored.set(String(p.key), p.value)
        return true
      case 'ui.notify':
        console.info('[larvyn stand-in] notification:', p.message)
        return true
      case 'stack.stop':
      case 'project.env.set':
        return true
      case 'terminal.run':
      case 'artisan.run':
      case 'stack.start':
      case 'stack.restart':
        return { sessionId: 'dev' } satisfies Session
      case 'network.fetch':
        throw new Error('network.fetch only works inside Larvyn')
    }
    throw new Error(`unknown method ${method}`)
  },
}
