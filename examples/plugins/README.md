# Example plugins

Plugins you can install to see what Larvyn plugins do, and copy to start your own. See [#56](https://github.com/larvyn/larvyn/issues/56) for the plugin design and its security model.

| Plugin | Kind | What it shows |
|---|---|---|
| [`larvyn.horizon`](larvyn.horizon) | No code, manifest only | A stack item that runs `php artisan horizon`, and ⌘K commands to restart, pause and check Horizon |
| [`larvyn.mailpit`](larvyn.mailpit) | Manifest + view | A stack item that runs Mailpit, and a sidebar view of its inbox. The view may only connect to `localhost:8025`, the address its manifest lists. |
| [`larvyn.hello`](larvyn.hello) | View | The smallest view: it reads the open project through `window.larvyn` and shows a notification. Copy it to start your own plugin. |

## Trying them

Plugins live in Larvyn's plugins folder, one folder per plugin, named after its id. On macOS that is `~/Library/Application Support/larvyn/plugins`.

Until Settings > Plugins can install from a folder, link an example there:

```sh
mkdir -p ~/Library/Application\ Support/larvyn/plugins
ln -s "$PWD/examples/plugins/larvyn.horizon" ~/Library/Application\ Support/larvyn/plugins/
```

> **Status:** Larvyn already reads and checks these manifests. Running their commands, stack items and views comes in the next v1 steps of #56. The `window.larvyn` API used by `larvyn.hello` is the planned API and may change before v1 ships.

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
| `network` | yes | Connecting to the hosts listed in `network` |
