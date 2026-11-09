---
name: larvyn
description: Drive the Larvyn app this session runs in (LARVYN_SESSION is set) - see the project and worktree, read and type into its terminals, start the dev stack and wait until it is ready, run artisan and the tests, read log errors, the models and the database, take locks, and start and wait for other agents. Use it instead of guessing ports, starting servers by hand or asking the user to copy output.
---

<!-- Installed by Larvyn: `larvyn agents remove` takes it out again. -->

# Larvyn

Larvyn is the Laravel workbench this terminal belongs to. The `larvyn`
command talks to the running app; `larvyn help` lists every command and
`--json` gives any of them as JSON. Commands that fail, test runs with
failures and waits that end without what they waited for exit non-zero.

## Where am I

```
larvyn context        # project, the folder you work in (often a git worktree), branch, local URL, dev stack
```

## Dev stack

The project's services (HTTP server, Vite, queue, scheduler, Reverb…) run in
Larvyn, where the user sees them. Start them there instead of in your own
shell, and wait until they are ready before using them:

```
larvyn stack list
larvyn stack start vite --wait            # exits 1 with its last lines if it crashes
larvyn stack wait serve --port 8000       # or --line '<regexp>'; --timeout seconds
larvyn stack restart queue
```

## Laravel

These run in your own folder, so in a worktree they see your changes:

```
larvyn artisan migrate --force            # prints artisan's output, exits with its code
larvyn test --filter LoginTest            # shows the run to the user, prints each failure with file:line
larvyn logs errors --since 10m --trace    # the app's latest log errors
larvyn models --name Order                # models with table, relationships, fillable, casts, row count
larvyn db tables                          # tables and row counts, through the app's own connection
larvyn db table orders                    # columns, indexes, foreign keys
larvyn db query "select status, count(*) from orders group by status"   # read-only: it can't change data
```

## Terminals

```
larvyn terminal list
larvyn terminal read <id> --lines 50      # an agent's terminal reads as its screen
larvyn terminal search <id> "error|failed" --context 2   # matching lines of the whole output, numbered
larvyn terminal write <id> "npm run build" --enter
larvyn terminal open --command "npm run build"
larvyn terminal close <id>                # only terminals you opened
```

## Other agents

Hand a separate task to another agent in its own worktree, then collect
its result:

```
larvyn agents start "add a CSV export to the orders page"   # starts from your branch; prints its id
larvyn agents wait <id>       # exit 0: done, 2: it needs attention (a question), 1: timeout or crash; prints its screen
larvyn agents wait <a>,<b>     # several: all of them, but back as soon as one needs attention
larvyn agents wait <a>,<b> --any   # back with the first one done
larvyn agents send <id> "also add a test for it"
larvyn agents status <id>
```

When `wait` exits 2, read the screen it printed: answer with
`larvyn agents send <id> "<answer>"`, or for a menu with the right option
already marked, `larvyn terminal write <id> "" --enter`.

## Locks

Agents working side by side share the database, the main branch, ports.
Take a lock before what would break another agent's work, and release it
after:

```
larvyn lock acquire db --note "migrating"   # waits up to 300s while another holds it; exit 1 if still held
larvyn artisan migrate
larvyn lock release db
larvyn lock list
```

A lock is a lease: it ends after `--ttl` seconds (600 by default; acquire
again to renew it) or when your terminal closes. Use `db` for migrations,
seeders and `migrate:fresh`, `main` before merging into the main branch.
