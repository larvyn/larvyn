# Larvyn

The Laravel workbench: your Laravel apps, their dev services, terminals, data
and AI coding agents in one native window. Built with Go + Wails 3 and React.

It takes the terminal workspace idea from Herdr (persistent terminals, tabs,
splits) and the agent orchestration idea from Orca (several agents in
parallel, each in its own git worktree, compare and merge the winner) and
points both at Laravel.

## What it does

| View | |
| --- | --- |
| **Overview** | Laravel/PHP versions, detected ecosystem (Filament, Livewire, Inertia, Horizon, Pest…), config summary, recent commits, and a **Doctor** that finds and fixes setup problems (missing vendor, `.env`, `APP_KEY`, pending migrations, storage link, debug in production). |
| **Dev stack** | One toggle each for `artisan serve`, Vite, queue (or Horizon), scheduler, Pail, Reverb, Octane. Output in the console drawer; processes are killed with their children. |
| **Agents** | Launch Claude Code, Codex, OpenCode, Antigravity CLI, Cursor Agent, Aider or Pi. Pick several and the same prompt fans out, each agent on its own `larvyn/<task>` branch in a worktree with `.env` and SQLite copied. Live status (working / waiting / needs attention), follow-ups, diff review, one-click merge or discard. Laravel playbooks (CRUD, tests, N+1, security review, upgrade…). |
| **Terminals** | Real PTYs (ConPTY on Windows) with tabs and splits that keep running across views and projects. |
| **Artisan** | Every command with a generated form for its arguments and options, favourites, and a generator studio for every `make:*`. |
| **Routes** | Searchable route table with method filters, middleware, open-controller-at-method, open in browser, copy `route()`. |
| **Models** | Every Eloquent model with table, record count, casts, traits, relationships (both directions) and `model:show` attributes. |
| **Database** | Tables with row counts, paginated/searchable/sortable data grid, cell editor, structure (columns, indexes, foreign keys), SQL console with history, migrations status with migrate/rollback/fresh. Uses the app's own connection, so every driver Laravel supports works. |
| **Tinker** | Run PHP in the booted app: dump, JSON view, timing, memory and the query log. |
| **Tests** | Runs `artisan test` (optionally in parallel) and shows every Pest/PHPUnit test with its result, time, failure message and failing line. Re-run one test or all failing ones, open the failing line, or hand a failure to an agent. |
| **Queues & Schedule** | Worker status, pending jobs, failed jobs with retry/forget/flush, scheduled tasks with next run. |
| **Logs** | Live tail of `storage/logs`, level filters, parsed exceptions, stack frames that open in your editor, and **Ask an agent to fix it**. New errors raise a notification in any view. |
| **Source control** | Changes with diffs, commit, discard, branches, history and worktrees. |
| **Dependencies** | `composer outdated` for direct packages (safe vs major updates, one-click update) and `composer audit` security advisories with severity and CVE. |
| **Environment** | `.env` as a grouped form with masked secrets or raw text, keys missing from `.env.example`, automatic backups. |

`Ctrl K` opens the command palette: navigate, start/stop services, run any
artisan line (`> migrate:fresh --seed`), a shell line (`$ npm run build`),
composer/npm scripts, or hand a prompt to an agent or Tinker.

## How it works

- `terminal.go`: PTY sessions, streamed to the UI as base64 chunks with
  sequence numbers; a 1 MB scrollback per session lets a view re-attach
  without losing output.
- `bridge.php`: embedded PHP script that boots the project's Laravel app and
  answers one JSON request (database, models, migrations, queues, schedule,
  tinker). Answers follow a marker so boot noise never breaks parsing.
- `stack.go`, `agents.go`, `git.go`, `laravel.go`, `projects.go`,
  `system.go`: the services the frontend calls through generated bindings.
- State lives in `%APPDATA%\larvyn` (`~/.config/larvyn`); set `LARVYN_HOME`
  to use another folder. For scripts: `LARVYN_VIEW=<view>` opens a view,
  `LARVYN_RUN=<command>` opens a terminal running it, `LARVYN_AGENT=<id>`
  starts an agent (claude, codex, …) in the last opened project, and
  `LARVYN_PALETTE=<text>` opens the command palette with that text typed.
- Composer: when it is not on PATH, Larvyn installs its own verified
  `composer.phar` on first use (Settings → Toolchain, or any composer action).
- Errors in the window (and a crashed view) are written to `larvyn.log` in
  the state folder; a crashed view shows the error instead of a blank window.

## Develop

```
wails3 dev        # hot reload
wails3 build      # bin/larvyn.exe
go test ./...     # LARVYN_TEST_PROJECT=<laravel app> also runs the bridge against it
```

Requires Go 1.25+, Node 20+, PHP 8.2+ on PATH (or set in Settings), and git.
