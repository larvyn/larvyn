<img src="icon.svg" width="64" height="64" alt="">

# Horizon

Restart, pause and check [Laravel Horizon](https://laravel.com/docs/horizon) from ⌘K.

**Version** 0.1.1 · **Needs** Larvyn 0.1.0 or newer · **By** Larvyn

Larvyn already adds Horizon to the stack when your app uses `laravel/horizon`. This plugin adds the commands you run while it's up.

## Install

In Larvyn, open **Settings → Plugins → Marketplace** and install **Horizon**.

Or download `larvyn.horizon-0.1.1.zip` from [Releases](https://github.com/larvyn/plugin-examples/releases/tag/larvyn.horizon-v0.1.1) and use **Settings → Plugins → Install .zip**.

## What it adds

Four commands in ⌘K. Each runs in a project terminal:

| Command | Runs |
|---|---|
| Horizon: restart workers | `php artisan horizon:terminate` |
| Horizon: status | `php artisan horizon:status` |
| Horizon: pause | `php artisan horizon:pause` |
| Horizon: continue | `php artisan horizon:continue` |

## Permissions

| Permission | Why |
|---|---|
| Run commands in a terminal | Runs the four commands above. Larvyn shows each one and asks before its first run. |

## Requirements

A Laravel app with `laravel/horizon`.
