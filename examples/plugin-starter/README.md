# Plugin starter: Vite + TypeScript

A Larvyn plugin with one sidebar view, built with Vite and TypeScript. `src/larvyn.ts` is a typed `window.larvyn` client, so every call gets autocomplete and its params are type-checked.

## Start

```sh
mkdir -p ~/Code
cp -r examples/plugin-starter ~/Code/my-plugin
cd ~/Code/my-plugin
npm install
```

1. In `larvyn.json`, change `id` (for example `yourname.my-plugin`), `name` and `author`.
2. List only the permissions your view calls. Larvyn refuses a call whose permission isn't listed. The comment above each method in `src/larvyn.ts` names its permission.
3. Build your view in `index.html` and `src/main.ts`.

## Work on it

| Command | What it does |
|---|---|
| `npm run dev` | Opens the view in a browser with hot reload. Outside Larvyn, calls go to a stand-in at the bottom of `src/larvyn.ts`, which returns sample data. The page is unstyled there, because only Larvyn serves its stylesheet. |
| `npm run build` | Type-checks, then builds the view into `dist/`. `larvyn.json` points at `dist/index.html`. |
| `npm run check` | Type-checks only. |

## Try it in Larvyn

1. `npm run build`
2. In Larvyn, open **Settings → Plugins → Install from folder** and pick this folder. Larvyn copies it without `node_modules`.
3. Open the view from the sidebar.

After a change, build again and install again. Your plugin's stored data is kept.

To share it, use **Export** in Settings → Plugins. It makes a .zip that others install with **Install .zip**.

## Calling Larvyn

```ts
import { call } from './larvyn'

const project = await call('project.info')            // needs project.read
await call('ui.notify', { message: 'Done' })          // needs ui.notify
await call('artisan.run', { args: ['queue:restart'] }) // needs artisan.run
```

A refused call (a missing permission, or a command the user didn't allow) rejects with an `Error`. The full list of methods is in [`examples/plugins/README.md`](../plugins/README.md#views-and-windowlarvyn).
