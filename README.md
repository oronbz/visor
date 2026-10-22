# visor

**A heads-up display for your terminal.**

![visor: clock, calendar, meetings, tasks, reminders, notes, CPU and memory, GitHub review requests, Jira issues and the weather in one terminal dashboard](docs/screenshot.png)

Stop jumping between Calendar, GitHub, Jira and your notes to figure out what's next. visor shows your next meeting, the PRs waiting for your review, your Jira tickets, today's tasks and reminders, and the weather on one screen, and keeps it fresh while you work.

And you can act on it, not just look at it. Approve a PR, move a ticket along, check off a task or open a meeting straight from the dashboard, without going to the browser.

- **Your agent sets it up.** Tell Claude Code, Codex or Cursor what you want on screen, and visor's [skills](#agent-skills) turn it into Widgets, Actions or a whole new Plugin.
- **Make it yours.** Drag Widgets around with the mouse or move them with vim keys, resize them, and hide what you don't need.
- **Works with the tools you already use.** GitHub, Jira, your macOS calendar and reminders, and any shell command. Missing something? Write a Plugin in any language.
- **Always one key away.** Run it in Ghostty, or open it over your tab in [herdr](https://herdr.dev) and close it with the same key.
- **Nothing to host.** It's a single app on your machine, and your tokens stay in your system keyring.

| | |
|---|---|
| [Install](#install) | Homebrew, Linux packages, Windows, from source, and [updating](#update) |
| [Getting started](#getting-started) | The default Widgets, the keys, and signing in |
| [Agent skills](#agent-skills) | Let your coding agent configure visor or write a Plugin |
| [Configure](#configure) | [Widgets](#widgets), [your own Actions](#your-own-actions) and [colours](#colours) in `config.toml` |
| [Plugins](#plugins) | [Install](#install-a-plugin) or [write](#write-a-plugin) a Plugin |
| [herdr](#herdr) | Open visor [with one key](#open-visor-with-one-key), [as a popup](#open-visor-as-a-popup) or [in a tab](#keep-visor-in-a-tab) |

## Install

On macOS, or Linux with [Homebrew on Linux](https://docs.brew.sh/Homebrew-on-Linux):

```sh
brew install oronbz/tap/visor
```

This installs `visor` and `visor-weather`, the Weather Plugin that ships with visor. On macOS it's a universal binary that needs no Xcode, and the cask clears the quarantine flag so macOS doesn't block the first run. I use and test on Apple silicon; Intel Macs get the same binary, but it's best effort there. The Meetings and Reminders Widgets ask for Calendar and Reminders access the first time they need it, and they're macOS only.

<details>
<summary>Linux packages (deb, rpm, apk)</summary>

Take a package from the [latest release](https://github.com/oronbz/visor/releases/latest) (amd64 and arm64). They install `visor` and `visor-weather` in `/usr/bin`.

```sh
sudo dpkg -i visor_X.Y.Z_amd64.deb      # Debian, Ubuntu
sudo rpm -i visor-X.Y.Z-1.x86_64.rpm    # Fedora, RHEL
sudo apk add --allow-untrusted visor_X.Y.Z_x86_64.apk   # Alpine
```

</details>

<details>
<summary>Windows</summary>

1. Download `visor_X.Y.Z_windows_amd64.zip` (or `arm64`) from the [latest release](https://github.com/oronbz/visor/releases/latest).
2. Extract `visor.exe` and `visor-weather.exe` into the same folder, for example `%LOCALAPPDATA%\Programs\visor`. Keep them together, visor looks for the Weather Plugin next to itself.
3. Add that folder to your `PATH` (Settings > System > About > Advanced system settings > Environment Variables), then open a new terminal and run `visor`.

Use Windows Terminal or another terminal with good Unicode and true colour support.

</details>

<details>
<summary>From source</summary>

```sh
go install github.com/oronbz/visor/cmd/visor@latest
go install github.com/oronbz/visor/cmd/visor-weather@latest
```

Both binaries need to end up in the same directory, which `go install` does for you.

</details>

### Update

With Homebrew, run `brew update && brew upgrade visor`. Otherwise, install the newer release the same way you installed the first one.

## Getting started

Run `visor`. With no config you get the default Widgets: Clock, Calendar, Meetings (macOS), Tasks, Reminders (macOS), Notes, CPU + memory, PRs, Jira and Weather. Script is opt-in, add it with `ctrl+k` and Add Widget.

- `?` shows every key. `ctrl+k` opens the Action palette, and the footer shows the keys for whatever is focused.
- `q` quits.
- Widgets that need a sign-in or a setting show *needs setup* with a way to fix it right there. GitHub uses your `gh` login or a pasted token. Jira is set up with `visor auth jira`. Tokens go only to your system keyring.

## Agent skills

visor is built to be set up by your coding agent. Install its two skills, then ask for what you want, such as "add a Widget for my team's Jira sprint", "give my PRs Widget a key that checks out the branch" or "make a Widget for our deploys":

```sh
visor skills install
```

- `visor-configure` sets up your config, Widgets, Actions and the herdr keybinds for you.
- `visor-make-plugin` writes a Plugin with you and tests it with `visor plugin check`.

This writes the skills to `~/.agents/skills/` (Codex, Cursor, Gemini CLI, opencode, Copilot, Pi) and links them into `~/.claude/skills/` when Claude Code is installed. They match your visor version: visor quietly refreshes them after an upgrade unless you edited them, and then it suggests running `visor skills install` again. `visor skills uninstall` removes only the skills visor installed.

Or use the [skills](https://github.com/vercel-labs/skills) CLI, pinned to your visor version (`visor --version`). Both write the same layout, so either can replace the other's copy:

```sh
npx skills add oronbz/visor#vX.Y.Z -g
```

## Configure

Your agent can do all of this with `visor-configure`. To do it by hand: your Widgets and settings live in `~/.config/visor/config.toml` (`$XDG_CONFIG_HOME` is respected, `%AppData%\visor` on Windows). `VISOR_CONFIG` or `--config <path>` point visor at another file. visor never rewrites it, so it's safe in your dotfiles. The arrangement and sizes you set with the mouse or in arrange mode go to a separate `layout.json` that visor owns.

```sh
visor config default   # print the full commented reference
visor config init      # write that reference to your config path, once
```

The reference lists every option and every Widget type, built-in and installed, with its settings, defaults and why you'd change them. Running instances reload the config as soon as you save it, so you can keep visor open while you edit.

### Widgets

Each `[[widget]]` block places one Widget. Give two Widgets of the same type their own `id`:

```toml
theme = "terminal"   # or "visor" for visor's own palette

[[widget]]
type = "jira"
site = "acme.atlassian.net"
email = "you@acme.com"

[[widget]]
type = "weather"
id = "paris"
city = "Paris, Texas"
units = "imperial"
```

### Your own Actions

Any Widget, built-in or from a Plugin, takes your own Actions. Each runs a command, and `{{name}}` fills in a value of the selected Item, shell-quoted. Every Item has `{{id}}`, `{{title}}` and, when it opens a page, `{{url}}`. Script Items have each key they print, and Plugins may name more. Your key wins over the Widget's own:

```toml
[[widget]]
type = "jira"
actions = [
  { key = "f", label = "fix bug", run = "~/bin/fix-bug {{url}}", confirm = true },
]
```

### Colours

Override any of the 13 colour tokens for dark or light backgrounds:

```toml
[colors.dark]
accent = "#8aa9ff"

[colors.light]
accent = "#3f63d8"
```

## Plugins

A Plugin is a program that adds one or more Widget types. visor starts it and talks to it over stdin and stdout (JSON-RPC), so you can write one in any language. Weather is a Plugin too, it ships with visor.

### Install a Plugin

```sh
visor plugin install bitrise               # an official Plugin, by name
visor plugin install owner/repo            # or owner/repo/subdir, with --ref <tag or branch>
visor plugin list
visor plugin update [name...]
visor plugin uninstall <name>
```

A bare name installs that folder of the official repo, [oronbz/visor-plugins](https://github.com/oronbz/visor-plugins). To find more, look for the [`visor-plugin` topic](https://github.com/topics/visor-plugin) on GitHub.

Before it builds anything, `install` shows the Plugin's Widget types and every build command it will run, and asks you to trust it (`-y` skips the question). Installing a Plugin means trusting its code, the same as `herdr plugin install`. Once it's built, `install` adds a Widget of the Plugin's type to `config.toml`, unless one is there already or you pass `--no-widget`. A Plugin with several Widget types gets none; add the ones you want with `ctrl+k` and Add Widget. Plugins go to `~/.local/share/visor/plugins/<name>/`, and running instances pick up an install, update or uninstall without a restart.

### Write a Plugin

The quickest way is to ask your agent to use the `visor-make-plugin` skill. By hand:

1. Make a directory with a `visor-plugin.toml` manifest:

   ```toml
   name = "hello"
   description = "Says hello"
   protocol = 1
   command = ["python3", "hello.py"]

   [[widget]]
   type = "hello"
   title = "Hello"
   description = "A greeting"
   refresh = "5m"

   [[widget.setting]]
   name = "greeting"
   kind = "string"
   default = "Hello"
   description = "What to say"
   ```

   If it needs building, add `[[build]]` steps such as `run = ["go", "build", "-o", "hello"]`; `visor plugin install` runs them in the Plugin's directory, with no shell. If it needs a credential, declare a setting with `kind = "secret"` instead of storing it yourself: visor asks for it, keeps it in your system keyring and hands it to the Plugin.

2. `visor plugin schema` prints the JSON Schema of the manifest and the protocol for your visor version. Your program answers `initialize`, then `widget/refresh` with a payload, and can push updates on its own. It draws only with visor's 13 colour tokens, never raw colours, so it follows the user's theme.
3. `visor plugin check <dir>` starts it, runs the whole protocol against it (handshake, a Widget's lifecycle, a refresh and an Action) and tells you what's wrong.
4. Load it while you work on it by adding its path to your config:

   ```toml
   [[plugin]]
   path = "~/code/hello"
   ```

5. To publish, push it to GitHub and add the `visor-plugin` topic to the repo. People install it with `visor plugin install you/hello`. To make it official, so it installs by name, open a pull request adding its folder to [oronbz/visor-plugins](https://github.com/oronbz/visor-plugins).

## herdr

The herdr plugin installs visor with Homebrew when it's missing, and gives herdr a `visor.toggle` action:

```sh
herdr plugin install oronbz/visor/herdr
herdr plugin update visor    # later, for the plugin's latest actions
```

When visor is already on your `PATH`, install does nothing. The plugin never upgrades visor and never asks anything; to upgrade visor, see [Update](#update). Without Homebrew it stops and prints the other ways to install.

### Open visor with one key

Bind the toggle in herdr's `config.toml` (`~/.config/herdr/config.toml`) and run `herdr server reload-config`:

```toml
[[keys.command]]
key = "cmd+shift+g"
type = "plugin_action"
command = "visor.toggle"
description = "visor"

[[keys.command]]
key = "prefix+d"
type = "plugin_action"
command = "visor.toggle"
description = "visor"
```

`cmd+shift+g` or `prefix+d` opens visor over the whole tab you're in, and the same key closes it and brings your panes back. herdr's sidebar stays visible and clickable, and right-click opens visor's Action menus. Press the key in another tab and visor moves there. `q` quits visor too.

### Open visor as a popup

Without the plugin, a herdr popup works too, but it covers the sidebar and takes every click until it closes:

```toml
[[keys.command]]
key = "cmd+shift+g"
type = "popup"
command = "visor --close-key cmd+shift+g --close-key 'ctrl+b d'"
description = "visor"
width = "100%"
height = "100%"
```

A popup sends every key to visor, so `--close-key` tells visor which keys close it. It takes herdr's key syntax, a chord (`cmd+shift+g`) or a sequence of two (`'ctrl+b d'`), and repeats. If you changed herdr's prefix, change `ctrl+b` to match. Right-click can't reach a popup, but everything in a right-click menu is on the keyboard too (`ctrl+k`).

### Keep visor in a tab

Open a new herdr tab, name it visor and run `visor` in it (`q` quits). The tab stays there when you detach and come back. visor turns on right-click for its pane while it runs and gives it back to herdr when it quits. visor in a tab and visor opened with a key can run at the same time, and they stay in sync.

## Inspiration

visor was inspired by [mirador](https://github.com/jchultarsky/mirador), a personal dashboard for your terminal by Julian Chultarsky (@jchultarsky).
