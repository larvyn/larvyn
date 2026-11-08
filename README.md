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
| **Dev stack** | One toggle each for `artisan serve`, Vite, queue (or Horizon), scheduler, Pail, Reverb, Octane. Output in the console drawer; processes are killed with their children. When [Flock](#flock-optional) serves the app, it takes the place of `artisan serve`. |
| **Agents** | Launch Claude Code, Codex, OpenCode, Antigravity CLI, Cursor Agent, Aider or Pi. Pick several and the same prompt fans out, each agent on its own `larvyn/<task>` branch in a worktree with `.env` and SQLite copied (with Flock, each attempt also gets its own `.test` URL to try in the browser). Live status (working / waiting / needs attention), follow-ups, diff review, one-click merge or discard. Laravel playbooks (CRUD, tests, N+1, security review, upgrade…). |
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
composer/npm scripts, the project's own commands, or hand a prompt to an
agent or Tinker.

A team can commit a `.larvyn/project.json` with shared commands, extra
dev-stack items, the plugins the project needs and the steps that prepare
each new agent worktree (`composer install`, migrations…). See
[examples/project](examples/project).

### Flock (optional)

[Flock](https://flockphp.com) is a free local PHP environment for Windows,
macOS and Linux: PHP, Node, databases and `*.test` sites with trusted HTTPS,
without Docker. Larvyn works without it; once it is installed, Larvyn uses it:

- **Local site** card in Overview, one click per step: set Flock up, start it,
  serve the app at `<folder>.test`, switch HTTPS, pick the PHP version (a
  missing one downloads with its progress shown) and open each worktree's URL.
  `APP_URL` follows the site when it was empty or `localhost`, with a backup
  of `.env`; the Doctor flags one that doesn't match.
- Laravel apps added to Larvyn are served by Flock right away (Settings →
  Flock turns this off).
- Agent worktrees are served by Flock at `larvyn-<task>.<site>.test`, set up
  by Flock itself (dependencies, `.env`, database), with a 🌐 button on each
  agent to compare attempts in the browser before merging.
- The dev stack drops `artisan serve`; Vite, the queue and the rest still run
  from Larvyn, since Flock doesn't run them.

Larvyn reads Flock's daemon API on its unix socket and makes changes with
Flock's `flk` command, as a user would.

### The `larvyn` command

Agents running in Larvyn can use it too (#107). Every terminal Larvyn
opens has the `larvyn` command on its PATH, and it talks to the running app:

```
larvyn context                         # project, worktree, branch, local URL, dev stack
larvyn terminal list                   # this project's terminals
larvyn terminal read <id> --lines 50   # a terminal's last lines as plain text
larvyn terminal write <id> "php artisan migrate" --enter
larvyn terminal open --command "npm run build"
larvyn terminal close <id>             # only terminals it opened itself
larvyn stack list                      # serve, vite, queue… and what runs
larvyn stack start vite --wait         # start it and wait until it is ready
larvyn stack wait serve --port 8000    # or --line '<regexp>' in its output; --timeout seconds
larvyn artisan migrate --force         # runs in the caller's folder (its worktree), exits with artisan's code
larvyn test --filter LoginTest         # runs in a terminal Larvyn shows, prints the failures
larvyn logs errors --since 1h --trace  # the app's latest log errors
larvyn agents start "add a CSV export" # another agent, in a new worktree from the caller's branch
larvyn agents send <id> "also add a test"
larvyn agents wait <id>                # until it is done (exit 0) or needs attention (exit 2), then its screen
larvyn agents status <id>              # its status and screen now
larvyn help                            # every command; --json on any of them for JSON
```

A command that fails, a test run with failures and a wait that ends without
the item being ready exit non-zero. The stack is the project's: it runs in
its main folder, as in the sidebar. `artisan`, `test` and `logs` work on the
folder they are called from, so an agent sees its own worktree.

An agent's report is its screen: `agents wait` returns it to the agent that
waits, and nothing is typed into that agent's terminal. `terminal read`
shows an agent's screen too, drawn by a terminal emulator, since a TUI's raw
output is scrambled. Agents can start agents two levels deep below the ones
you start.

`larvyn api list` lists the methods behind the commands and `larvyn api
call <method> '<json>'` calls one with JSON params. Outside Larvyn's
terminals it works from a project's folder or one of its worktrees, while
the app runs.

## How it works

- `main.go` only wires the app: it registers the services with Wails,
  connects them through hooks and opens the window. Services don't import
  each other; the one exception is `terminal`, which the others use to open
  terminals. `integration_test.go` checks that wiring against a real
  Laravel app (`LARVYN_TEST_PROJECT=/path/to/app go test -run Bridge .`).
- Services, the Go side of the frontend, each in `internal/<pkg>` with its
  bindings generated into `frontend/bindings/larvyn/internal/<pkg>`:
  `project` (projects, inspection, Doctor fixes), `terminal` (PTY sessions,
  streamed as base64 chunks with sequence numbers and a 1 MB scrollback, plus
  the workspace restore), `stack` (dev services), `agents`, `laravel`
  (artisan, routes, models, env, logs, tests and the PHP bridge), `git`,
  `files` (the editor and fuzzy finder), `system` (toolchain, settings,
  New app, updates, feedback, open in editor) and `plugins` (the plugin
  host: reads and checks the `larvyn.json` of each plugin in the plugins
  folder; see #56).
- `projectfile` reads and writes a project's `.larvyn/project.json` (see
  #106): `project` lists and moves its commands, `stack` adds its items and
  `agents` runs its `worktree.setup` through `terminal.StartAfterSetup`.
- Base packages the services build on, which never import a service:
  `store` (projects, settings, the state folder), `projectfile`, `proc` (running PHP, artisan
  and git, finding programs, the login-shell PATH, killing process trees),
  `composer` (Composer from PATH or Larvyn's own phar), `dotenv`, `flock`
  (the optional Flock local server: its daemon's API and `flk`), `updater`
  (self-update) and `platform` (OS start-up tweaks).
- The local API (`internal/api`, #107): the app listens on `larvyn.sock` in
  the state folder (a unix socket on every system, only the user may
  connect) and checks the token it writes to `api.token` at each start;
  never an HTTP server. Methods are registered in `agentapi.go` from the
  services with a description per param and describe themselves, so the
  CLI (`internal/cli`) has no list of commands. On Linux and macOS the app's
  binary runs the CLI when given a command; Windows builds the app as a GUI
  program, so it carries the console build of `cmd/larvyn` instead
  (`internal/clibin`, built by `build:cli`). Either way it is put in the
  state folder's `bin`, first on every terminal's PATH, and each terminal
  gets `LARVYN_SESSION` so the CLI knows which one it runs in.
- `internal/laravel/bridge.php`: embedded PHP script that boots the project's
  Laravel app and answers one JSON request (database, models, migrations,
  queues, schedule, tinker). Answers follow a marker so boot noise never
  breaks parsing.
- State lives in `%APPDATA%\larvyn` (`~/.config/larvyn`); set `LARVYN_HOME`
  to use another folder. For scripts: `LARVYN_VIEW=<view>` opens a view,
  `LARVYN_RUN=<command>` opens a terminal running it, `LARVYN_AGENT=<id>`
  starts an agent (claude, codex, …) in the last opened project, and
  `LARVYN_PALETTE=<text>` opens the command palette with that text typed, and
  `LARVYN_FILE=<path>` opens a project file in the Files editor.
- `internal/system/create.php`: embedded stand-in for `laravel new`, used
  when the Laravel installer is missing or too old. It applies the New app choices (starter kit,
  database, git, Pest, npm) the same way the installer does.
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

Without `task`/`wails3` on macOS, `scripts/build-macos.sh --run` builds the
frontend if needed and then `bin/larvyn`, with the same deployment target as
`build/darwin/Taskfile.yml` (12.0) and Apple's clang. A plain `go build`
leaves the target unset and ld warns "object file was built for newer
'macOS' version". If WebKit logs "could not create directory
~/Library/WebKit/larvyn/...", that folder is not owned by you and the app
cannot keep its settings: `sudo chown -R $(id -un):staff ~/Library/WebKit`.

`scripts/ci-local.sh` runs the CI checks of `.github/workflows/test.yml` on your machine: the Linux jobs in Docker with [act](https://nektosact.com), and the Go job natively on a Mac. Use it before pushing, or when GitHub Actions can't run. Pass `linux` or `mac` to run only one side.
