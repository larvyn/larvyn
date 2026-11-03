# Example plugins

Plugins you can install to see what Larvyn plugins do, and copy to start your own. See [#56](https://github.com/larvyn/larvyn/issues/56) for the plugin design and its security model.

| Plugin | Kind | What it shows |
|---|---|---|
| [`larvyn.horizon`](larvyn.horizon) | No code, manifest only | ⌘K commands to restart, pause, continue and check Horizon. It adds no stack item, because Larvyn already adds Horizon to the stack when the app uses `laravel/horizon`. |
| [`larvyn.mailpit`](larvyn.mailpit) | Manifest + view | A stack item that runs Mailpit, and a sidebar view of its inbox. The view reads Mailpit's API through `network.fetch`, and Larvyn only fetches from `localhost:8025`, the address its manifest lists. |
| [`larvyn.hello`](larvyn.hello) | View | The smallest view: it reads the open project through `window.larvyn` and shows a notification. Copy it to start your own plugin. |

## Trying them

In Larvyn, open **Settings → Plugins → Install from folder** and pick one of these folders, such as `examples/plugins/larvyn.horizon`. Larvyn shows what the plugin may do, and copies it into its plugins folder only once you allow it.

While you are working on a plugin, you can link its folder instead of copying it. Larvyn then reads your changes when you click Refresh. The plugins folder holds one folder per plugin, named after its id; on macOS it is `~/Library/Application Support/larvyn/plugins`.

```sh
mkdir -p ~/Library/Application\ Support/larvyn/plugins
ln -s "$PWD/examples/plugins/larvyn.horizon" ~/Library/Application\ Support/larvyn/plugins/
```

A linked plugin starts off. Turn it on in Settings, which asks you to approve its permissions.

> **Status:** Larvyn installs these plugins and runs all three kinds of contribution:
> - **commands**, in ⌘K under Plugins;
> - **stack items**, in the sidebar's Stack;
> - **views**, in the sidebar's Plugins group.
>
> The first run of each command or stack item asks you to confirm the exact command line. Views have no network access of their own: they call `network.fetch`, and Larvyn makes the request only to the hosts in `network`. So the Mailpit view needs no CORS setting in Larvyn. Opened directly in a browser for testing, it falls back to `fetch()`, which needs `mailpit --api-cors`.

## The manifest

Every plugin has a `larvyn.json`:

| Field | Required | Meaning |
|---|---|---|
| `apiVersion` | yes | The plugin API version. It is `1` for now. |
| `id` | yes | `publisher.name`, using lowercase letters, digits and dashes. The plugin's folder must have the same name. |
| `name`, `version` | yes | The display name, and a semantic version such as `1.0.0`. |
| `larvyn` | yes | The Larvyn versions the plugin works with, such as `>=0.1.0` or `>=0.1.0 <0.3.0`. |
| `permissions` | yes | What the plugin may do. Users see this list before they install the plugin. |
| `network` | with `network` | The hosts its views may connect to, such as `api.example.com` or `localhost:8025`. |
| `contributes` | yes | The `commands`, `stack` items and `views` it adds. |
| `requires` | no | The programs it needs on PATH: `program`, an `install` command for `mac`, `linux` and `windows`, and a `url` with install instructions. If a program is missing, Larvyn says so when the plugin is installed and in its row. It offers to run the install command in a visible terminal, after the user confirms the exact command. |
| `description`, `author`, `homepage` | no | Shown in the plugin list. `homepage` must be an http(s) link. |

### Permissions

The permissions marked sensitive are shown in red when a user installs the plugin.

| Permission | Sensitive | Allows |
|---|---|---|
| `project.read` | | Reading the project's name, path and stack |
| `project.env.read` | yes | Reading `.env`, including secrets |
| `terminal.run` | yes | Running commands. Commands and stack items need it. |
| `artisan.run` | yes | Running Artisan commands |
| `stack.control` | | Starting and stopping the project's services |
| `ui.notify` | | Showing notifications |
| `storage` | | Keeping the plugin's own settings and data |
| `network` | yes | Fetching from the hosts listed in `network`, through `network.fetch`. Larvyn makes the request, and refuses any other host. |

## Views and `window.larvyn`

A view is an HTML page from the plugin's folder. Larvyn shows it in a sandboxed iframe, which runs scripts but has no access to Larvyn's page, its storage or its Go side. Its Content-Security-Policy keeps it to its own files:
- Scripts and styles may be inline or files in the plugin.
- Images and fonts may be files in the plugin, or `data:` URLs.
- There is no `fetch`, no forms and no frames.

Larvyn adds `window.larvyn` to every view page. Its only method, `call`, returns a promise:

```js
const project = await window.larvyn.call('project.info')
```

Larvyn checks each call against the plugin's granted permissions, in Go, before doing anything.

| Method | Permission | Params | Returns |
|---|---|---|---|
| `project.info` | `project.read` | — | `{ id, name, path }` of the open project |
| `project.env.read` | `project.env.read` | — | the project's `.env`, as an object |
| `ui.notify` | `ui.notify` | `{ message }` | shows a notification with the plugin's name |
| `storage.get` | `storage` | `{ key }` | the stored value, or `null` |
| `storage.set` | `storage` | `{ key, value }` | stores a JSON value; `null` deletes it. The limit is 1 MB per plugin. |
| `network.fetch` | `network` | `{ url }` | a GET request made by Larvyn, only to the hosts in `network` (redirects included). Returns `{ status, contentType, body }`. The limit is 2 MB. |
