---
name: visor-configure
description: Set up or change visor, the terminal dashboard. Use when adding, removing or tuning visor Widgets and their settings, changing its theme or colours, or binding a herdr key that opens visor.
---

# Configure visor

visor reads one hand-edited file, `config.toml`, and owns one machine-written file, `layout.json`, beside it. You edit `config.toml` only, the way a careful person would: surgically, keeping every comment and blank line.

## 1. Find the file

The first that applies:

1. the `--config <path>` the user runs visor with;
2. `$VISOR_CONFIG`;
3. `$XDG_CONFIG_HOME/visor/config.toml`, which defaults to `~/.config/visor/config.toml` on every OS (`%AppData%\visor\config.toml` on Windows).

No file yet: `visor config init` writes the full reference there, every line commented out. Uncomment what the user wants.

## 2. Read the reference

Run `visor config default`. It prints every global option, every Widget type (built-in and from installed Plugins) with each setting's kind, default, choices and the reason to change it. It is the source of truth: read it before every edit and take keys and values from it, never from memory. It reflects the visor on this machine, Plugins included.

## 3. Edit

- **Change a setting:** edit or uncomment that one line.
- **Add a Widget:** append a `[[widget]]` block with `type` and only the settings that differ from their defaults.
- **Remove a Widget:** comment out its block with `# ` on each line.
- **The default set:** with no `[[widget]]` blocks, visor shows one Widget of every default type. The first block you add replaces that set, so when adding the first block, also add blocks for the defaults the user wants to keep.
- **Two Widgets of one type** (two Jira queries, weather for two cities) each need their own `id`. `title` names them in the dashboard.
- **One more Action on a Widget** (run a script on the selected ticket, open it elsewhere): add `actions` to its block, as "Every block takes" in the reference describes, on any Widget type. Name only values the reference says its Items have; an Item without a named value doesn't get the Action. Don't rebuild a Widget as a script just to add one.
- **Colours:** `theme` is a string (`"terminal"`, the default, or `"visor"`). Per-colour overrides go in `[colors.dark]` and `[colors.light]` tables, keyed by the token names the reference lists.
- **Credentials** stay out of `config.toml`. GitHub uses `gh auth login` or `GH_TOKEN`; Jira uses `visor auth jira`, which prompts for a masked token, so ask the user to run it in their own terminal.
- **Arrangement** (order, sizes, hidden Widgets) lives in `layout.json`, which visor rewrites. Leave it to visor and tell the user the keys: `m` arranges the focused Widget (`←↓↑→` move, `Shift + ←↓↑→` resize, `⏎` keep), dragging a title or border with the mouse does the same, `w` shows or hides Widgets.

Done when: the file parses as TOML, every key you wrote appears for its Widget type in `visor config default`, and nothing outside your edit changed (diff it).

Running instances pick up the change at once. A key visor can't use shows as a footer toast `config.toml:LINE: …` and puts that Widget in *needs setup*, which tells you what to fix.

## herdr key

When the user runs herdr and wants a key that opens visor: check `herdr plugin list` shows the `visor` plugin, and have the user run `herdr plugin install oronbz/visor/herdr` (or `herdr plugin update visor` when `herdr plugin action list --plugin visor` lacks `toggle`) if not. Then append to herdr's `~/.config/herdr/config.toml`, after checking that file doesn't bind the key already, and run `herdr server reload-config`:

```toml
[[keys.command]]
key = "cmd+shift+g"
type = "plugin_action"
command = "visor.toggle"
description = "visor"
```

The action opens visor over the current tab and closes it on the next press, while herdr's sidebar stays clickable. herdr handles the key, so visor needs no `--close-key` here.

Only when the user wants no herdr plugin, bind a popup instead. It covers the sidebar and keeps all input until visor exits, so `--close-key` must repeat the same key:

```toml
[[keys.command]]
key = "cmd+shift+g"
type = "popup"
command = "visor --close-key cmd+shift+g"
description = "visor"
width = "100%"
height = "100%"
```

`--close-key` takes herdr's key syntax and repeats; a two-key sequence such as `--close-key 'ctrl+b d'` also works.
