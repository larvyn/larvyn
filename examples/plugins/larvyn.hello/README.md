# Hello

The smallest plugin with a view: it shows the open project and says hello. Copy it to start your own.

**Version** 0.1.0 · **Needs** Larvyn 0.1.0 or newer · **By** Larvyn

## Install

In Larvyn, open **Settings → Plugins → Marketplace** and install **Hello**.

Or download `larvyn.hello-0.1.0.zip` from [Releases](https://github.com/larvyn/plugin-examples/releases/tag/larvyn.hello-v0.1.0) and use **Settings → Plugins → Install .zip**.

## What it adds

- **Sidebar:** a **Hello** view with the open project's name and path, and a button that shows a Larvyn notification.

## Permissions

| Permission | Why |
|---|---|
| Read the project's name, path and stack | Shows the open project. |
| Show notifications | The button's notification. |

## Make your own

The whole plugin is [`larvyn.json`](larvyn.json) and [`ui/index.html`](ui/index.html). Copy the folder, change the `id`, and install it with **Settings → Plugins → Install from folder**. For Vite and TypeScript, start from [`plugin-starter`](../../plugin-starter) instead.
