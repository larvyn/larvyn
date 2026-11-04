<img src="icon.svg" width="64" height="64" alt="">

# Mailpit

Catch the mail your Laravel app sends. Mailpit runs in your project's stack, its inbox appears in Larvyn's sidebar, and one click points your app's mail settings at it.

**Version** 0.2.1 · **Needs** Larvyn 0.1.0-beta.10 or newer · **By** Larvyn · [mailpit.axllent.org](https://mailpit.axllent.org)

## Install

In Larvyn, open **Settings → Plugins → Marketplace** and install **Mailpit**.

Or download `larvyn.mailpit-0.2.1.zip` from [Releases](https://github.com/larvyn/plugin-examples/releases/tag/larvyn.mailpit-v0.2.1) and use **Settings → Plugins → Install .zip**.

## What it adds

- **Stack:** a **Mailpit** item. SMTP on `127.0.0.1:1025`, web UI on `127.0.0.1:8025`.
- **Sidebar:** a **Mail** view with the inbox. Open a message to read it.
- **Settings** in the Mail view: point your app at Mailpit. This sets `MAIL_MAILER=smtp`, `MAIL_HOST=127.0.0.1` and `MAIL_PORT=1025` in `.env`. Larvyn backs up `.env` first.

## Permissions

| Permission | Why |
|---|---|
| Run commands in a terminal | Starts Mailpit in the stack. |
| Connect to `localhost:8025` | Reads the inbox from Mailpit's API. No other host. |
| Read and change `MAIL_MAILER`, `MAIL_HOST`, `MAIL_PORT` | Points your app's mail at Mailpit. No other `.env` value. |
| Show notifications | Says when the settings changed. |

## Requirements

[Mailpit](https://mailpit.axllent.org/docs/install/). If it isn't installed, the plugin's row in Settings → Plugins offers the install command:

- macOS: `brew install mailpit`
- Linux: `curl -sL https://raw.githubusercontent.com/axllent/mailpit/develop/install.sh | sudo bash`
- Windows: `scoop install mailpit`
