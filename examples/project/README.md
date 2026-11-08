# Shared project settings: `.larvyn/project.json`

A `.larvyn/` folder at the root of a project, committed with the code, holds what everyone on the team should get when they open the repository in Larvyn. Your own settings stay in Larvyn's state folder, as before. See [#106](https://github.com/larvyn/larvyn/issues/106).

Copy [`.larvyn/project.json`](.larvyn/project.json) into your project and change it. Larvyn reads it again when it changes on disk, after a `git pull` or a branch switch, for example.

## What it holds

| Field | What Larvyn does with it |
|---|---|
| `apiVersion` | Required, `1`. A file for a newer schema asks you to update Larvyn. |
| `commands` | Shell commands in ⌘K (under **Project**) and in the Overview's **Commands** card. Each has an `id` (lowercase letters, digits and dashes), a `title` and a `run` line, the same shape as a plugin command. They run in a project terminal, as written. |
| `stack` | Extra dev-stack items, next to the built-in ones: a `key`, a `title` and an `argv`. They show in the Overview's Dev stack and in ⌘K. |
| `plugins` | The plugins the project uses: an id (`"larvyn.mailpit"`) or `{ "id": …, "version": ">=1.0.0" }`. When one is missing, off or the wrong version, the Overview says so and opens it in the Marketplace. |
| `worktree.setup` | Commands run one after another in each new agent worktree before the agent starts. Their output shows in the agent's terminal, and the agent starts when the last one succeeds. If one fails, the agent doesn't start: fix the problem and press **Relaunch**, which starts the agent without the setup. |

Never put terminals, agents' state, secrets, `.env` values or plugin permissions here: the file is committed.

## Only me or the team

Every command is either yours (kept in Larvyn's state folder) or the team's (in `.larvyn/project.json`). In the Overview's **Commands** card, the people icon moves a command from one to the other; commit the file to share the change. When one of your commands has the same `id` as a shared one, yours replaces it, for you only.

Larvyn rewrites only the `commands` list when it moves or edits a command. The other fields, their order and any entries it skipped stay as they were. It reindents the file with two spaces.

## Trust

Commands from `.larvyn/` run like the rest of the project's code (composer scripts, artisan): adding a project to Larvyn means trusting it, so there is no prompt before they run.

## When something is wrong

- A wrong entry (a bad id, a missing title, a field Larvyn doesn't know) is skipped, and the Overview lists it. The rest still loads.
- A file that doesn't parse keeps the last version that loaded, and the Doctor shows the error.

## Flock

When [Flock](../../README.md#flock-optional) serves the project, it prepares each agent worktree itself (dependencies, `.env`, database), so `worktree.setup` doesn't run there: both would run `composer install` at the same time.
