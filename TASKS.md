# Open tasks

Each task is independent: hand it to an agent as is or do it by hand. Follow
`AGENTS.md` and check `[x]` when done.

## 1. Code signing

- [ ] Get a code signing certificate (Authenticode).
- [ ] Use the `windows:sign` and `windows:sign:installer` tasks in `build/windows/Taskfile.yml`.
- [ ] Check that SmartScreen no longer shows "unknown publisher".

## 2. Spanish interface

- [ ] Add lightweight i18n: one strings file per language (`frontend/src/i18n/es.ts`, `en.ts`) and a `t()` hook.
- [ ] Route every string in the views, the palette and the toasts through `t()`.
- [ ] Language picker in Settings (defaults to the system language), saved in the settings.

## 3. More Herd integration

- [ ] Read Herd's sites (`herd sites` / `herd links`) and show them in Overview.
- [ ] Change a site's PHP version (`herd isolate <version>`) from the UI.
- [ ] Turn HTTPS on and off per site (`herd secure` / `herd unsecure`).

## 4. Light theme

- [ ] Define the light theme's color tokens in `frontend/src/styles/app.css` next to the dark ones.
- [ ] Adjust the xterm theme so terminals stay readable.
- [ ] Picker in Settings: dark, light or follow the system.

## 5. Back to stable xterm

The terminals use xterm betas (#103): `@xterm/xterm` 6.1.0-beta.300 and `@xterm/addon-webgl` 0.20.0-beta.297, plus fit, search and web-links from the same date. They are the only versions with the shared WebGL atlas fixes (xtermjs/xterm.js#6042 and #6043). The stable 0.19.0 can show garbled text with several terminals.

- [ ] When xterm 6.1.0 and addon-webgl 0.20.0 ship as stable, move the five `@xterm/*` packages to those versions (`npm view @xterm/xterm dist-tags`).
- [ ] Test with several agents in the grid: correct text, WebGL on and no lost contexts when switching views.
- [ ] Check whether unfocused terminals show their cursor again (the beta doesn't draw the outline).

## 6. Spec-first workflow, next steps

The Elephant-Goldfish workflow (`internal/agents/egm.go`, `components/Specs.tsx`) is complete. Possible next steps:

- [ ] Warn when an Elephant changed files outside `.larvyn/specs/` (compare `git status` from before it started with after).
- [ ] Show the review history (`.larvyn/specs/history/`) in the panel, with a diff between rounds.
- [ ] Option to commit specs with the code, for teams that keep them in the repository.
