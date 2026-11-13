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

## 7. API socket in a long LARVYN_HOME

The local API listens on a unix socket at `LARVYN_HOME/larvyn.sock`
(`internal/api/server.go`, `SocketPath`). A unix socket path can't be much
longer than about 104 characters (108 on Linux), so with a long home the API
doesn't start: `bind: invalid argument`, seen on Windows with a temporary
home whose socket path was 125 characters. The real home is short enough,
but a test or a user with a long profile path hits it.

- [ ] When `SocketPath()` is too long, use a short path instead: e.g. the
      system temp dir plus a hash of the home (`larvyn-<hash>.sock`), keeping
      the socket's permissions owner-only.
- [ ] The CLI (`api.Dial`) and `larvyn mcp` must find the same path: derive it
      in one function both use.
- [ ] Test with a home over 120 characters on Linux, macOS and Windows.

## 8. Retire the larvyn/releases mirror

Since beta.16 releases are published in larvyn/larvyn, and `release.yml` also
copies each one to larvyn/releases, where builds up to beta.15 look for
updates. Once those builds have updated, the copy is no longer needed.

- [ ] Re-deploy the site so the beta pill shows the newest release (it is read at build time).
- [ ] Wait until the `stats` branch shows almost no downloads of beta.15 and older from larvyn/releases.
- [ ] Remove the mirror steps (checkout, script sync, "Publish the mirror copy") and `RELEASES_TOKEN` from `release.yml`.
- [ ] Keep `larvyn/releases` in `scripts/stats.sh` and `download-stats.yml`, so the history adds up.
- [ ] Update the larvyn/releases README to say releases moved to larvyn/larvyn, then archive the repo. Archived releases still download, so `LARVYN_VERSION` up to beta.14 keeps working.

## 9. Precise agent states

`agents.Status` (`internal/agents/agents.go`) guesses: output in the last 2.5 s
is "working", a bell is "attention", anything else "idle". It can't tell an
agent waiting for an approval from one that finished, and a long silent step
looks idle.

- [x] Parse an OSC 7501 sequence in `internal/terminal` (`\e]7501;state=working|blocked|done|idle\e\\`), so any agent or script can report its state and keep the last reported state on the session.
- [x] Add `larvyn agents state <working|blocked|done>` so hooks can report it through the local API.
- [x] In `agentsetup`, wire the agents' own hooks to it where they exist (Claude Code `Notification` / `Stop`, Codex `notify`…).
- [x] `Status` prefers a reported state and falls back to the current heuristic; add "blocked" and "done" to `agentStatus` in `frontend/src/lib/store.ts`.

## 10. Desktop notifications

- [ ] Use Wails 3's notification service to notify when an agent becomes blocked, done or exits, only while the Larvyn window is not focused.
- [ ] Clicking the notification focuses the window on that agent.
- [ ] Setting to turn them off, per kind (blocked / done / log errors).
- [ ] Check macOS, Windows and Linux (notification permission on macOS).

## 11. Local checks after an agent finishes

When an agent is done, run the project's checks in its worktree and send the
failures back to it, before a person reviews.

- [ ] `checks` in `.larvyn/project.json`: a list of commands (e.g. `vendor/bin/pint --test`, `vendor/bin/phpstan`, `php artisan test`), run through `internal/proc`; update `examples/project`.
- [ ] Run them when the agent turns done (needs task 9) and show pass/fail on the agent's card.
- [ ] On failure, send the trimmed output to the agent (`agents send`), with a cap on rounds (e.g. 3) before it asks for a person.
- [ ] Option to run them by hand, and to turn the automatic loop off.

## 12. Push and pull request in one click

- [ ] On an agent with commits: push its `larvyn/<task>` branch and open a PR with `gh pr create`, through `proc.Run`.
- [ ] Title and body from the task's prompt and the diff (editable before sending); no AI attribution in them.
- [ ] Doctor check when `gh` is missing or not logged in; the button explains why it is off.
- [ ] Show the PR link and its state on the agent's card.

## 13. CI and review feedback to the agent

Needs task 12.

- [ ] Poll `gh pr checks` and the PR's review comments for agents that have a PR.
- [ ] Send failed checks (with the failing job's log tail) and new review comments to the agent that owns the branch.
- [ ] Same round cap and off switch as task 11.

## 14. Merge conflicts handled by the agent

- [ ] When merging an agent's branch conflicts, abort cleanly and offer "Ask the agent to rebase".
- [ ] Send it the base branch and the conflicting files; once it is done, try the merge again.

## 15. Agents board

- [ ] Board layout in Agents next to the grid, with lanes: Working, Needs you, In review (checks or PR pending), Ready to merge.
- [ ] Lanes come from the agent's state (task 9), its local checks (task 11) and its PR (task 12); lanes whose source is missing are hidden.
- [ ] Show how long each agent has been in its state ("working 4m", "blocked 2m").

## 16. Orchestrator mode

The local API already lets an agent start, message and wait for others, and
records the `parent` of each one (`internal/agentapi/agents.go`).

- [ ] Playbook "Orchestrate": the main agent splits the task and starts one worker per piece with `larvyn agents start`, then waits for them and reports.
- [ ] Show the parent → children tree in Agents.
- [ ] Optionally start from a spec: the Elephant writes it, the orchestrator splits it.

## 17. Checkpoints per turn

- [ ] Each time an agent goes from working to blocked/done, snapshot its worktree (including untracked files) to `refs/larvyn/checkpoints/<task>/<n>`.
- [ ] List the turns with their diff on the agent, and restore one.
- [ ] Remove the refs when the agent is merged or discarded.

## 18. Agents that survive closing Larvyn (design first)

Today closing, crashing or updating Larvyn ends running agents; the workspace
is restored and conversations resume, but work in flight stops.

- [ ] Write a design: a `larvyn daemon` owning the PTYs, the window as a client, scrollback on reconnect, a versioned protocol, and how updates replace the daemon.
- [ ] Cover both sides (ConPTY on Windows, PTY elsewhere) and how `proc.KillTree` fits.
- [ ] Only then split it into tasks.
