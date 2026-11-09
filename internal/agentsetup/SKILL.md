---
name: larvyn
description: Drive the Larvyn app this session runs in (LARVYN_SESSION is set) - see the project and worktree, read and type into its terminals, start the dev stack and wait until it is ready, run artisan and the tests, read log errors, and start and wait for other agents, and run the spec-driven Elephant-Goldfish workflow. Use it instead of guessing ports, starting servers by hand or asking the user to copy output.
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
```

## Terminals

```
larvyn terminal list
larvyn terminal read <id> --lines 50      # an agent's terminal reads as its screen
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
larvyn agents send <id> "also add a test for it"
larvyn agents status <id>
```

When `wait` exits 2, read the screen it printed: answer with
`larvyn agents send <id> "<answer>"`, or for a menu with the right option
already marked, `larvyn terminal write <id> "" --enter`.

## Specs (Elephant-Goldfish)

For a feature big enough to plan first: an Elephant writes a spec in
`.larvyn/specs/` (no code), a Goldfish with no memory reviews it cold, the
review goes back to the Elephant until a review says READY, then builders
implement it in worktrees and an auditor checks each one for drift.

```
larvyn specs list
larvyn specs draft "subscription billing with team seats"   # prints the Elephant's id
larvyn specs review <slug> --agent codex      # a different agent reviews better
larvyn specs show <slug> --review             # the verdict is on its first line
larvyn specs revise <slug>                    # the Elephant folds the review in
larvyn specs build <slug> --agents claude,codex
larvyn specs drift <builder-id> --agent codex
larvyn specs fix <builder-id>                 # the builder works through the drift report
```

Each of these starts or messages an agent: follow it with `larvyn agents wait <id>`.
Never edit files in `.larvyn/specs/` yourself unless you are the Elephant.
