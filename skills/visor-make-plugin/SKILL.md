---
name: visor-make-plugin
description: Build a Widget type for visor, the terminal dashboard. Use when writing a visor script Widget or a visor Plugin, testing one against the protocol, or publishing one for others to install.
---

# Make a visor Widget type

visor draws every Widget itself. A Widget type only describes what to show (Items with typed fields, or styled lines) and which Actions exist; the host renders it, so it looks native and follows the user's theme.

Two routes. Take the lighter one that fits.

## Route 1: script Widget

Fits when a shell command can print the data. No Plugin, just a `[[widget]]` block with `type = "script"` in the user's `config.toml`. Its settings (`command`, `interval`) and output formats are in `visor config default` under the Script Widget, and the `actions` every block takes, with `{{key}}` templates, under "Every block takes"; read them there.

Done when: the command, run in a shell, prints one of the formats the reference names, and every `{{key}}` in `actions` is a key of the printed objects.

## Route 2: Plugin

Fits when the Widget needs state, a long-lived connection, credentials, choice or secret input, or pushing updates. A Plugin is a folder holding `visor-plugin.toml` and any executable that speaks JSON-RPC 2.0 over stdio, in any language.

1. **Read the contract.** Run `visor plugin schema`. It prints one JSON Schema with `$defs/manifest` (the `visor-plugin.toml` format) and `$defs/protocol` (every message, with the call sequence in its description). The descriptions carry the semantics; read all of it before writing code.
2. **Write the manifest** at the folder's root: `protocol = 1`, the `command`, and one `[[widget]]` table per Widget type. Declare every setting as a `[[widget.setting]]`: visor validates the user's values against it and lists it in `visor config default`. Declare a credential (API token, password) as a setting with `kind = "secret"` and a `target` page where the user creates one; never store it yourself (no files, no `security` or other keyring calls). visor asks for it in a masked prompt through its own "Set <name>" Action (id `secret:<name>`), keeps it in the OS keyring, and sends it in `widget/open`'s `settings`, absent until set. Make it `required` and visor shows the Widget in needs setup without refreshing it until it's set; leave it optional when an environment variable can stand in, and report `setup` yourself.
3. **Write the process.** One JSON message per line on stdout, which belongs to the protocol alone; logs go to stderr. Answer an Action id you don't know with a JSON-RPC error. Report trouble through the refresh result's `status` (`error`, `setup` with an Action that fixes it, `rate_limited`), never as text in the payload. When a secret is missing or rejected, report `setup` with `"action": "secret:<name>"` to offer visor's "Set <name>" Action. Never give an Action of your own an id starting `secret:`, and never echo a secret in a message or on stderr.
4. **Colour with tokens.** Every `token` and `bg` is one of the 13 theme tokens below; visor rejects anything else, raw colours included. For big digits, braille charts and bars use the payload's `digits`, `chart` and `bar` fields instead of drawing them.
5. **Shape the payload.** `lines` make a live view; `columns` and `items` make a list. A list may also carry `lines`: they become its summary, pinned above the items (headline digits, a chart, a bar), so one Widget type can show both. Give a summary `chart` a `rows` height; without one it takes 1 row. Item fields may hold `bar` spans too, but not digits or charts. Give each item `values` naming what it is (`{"key": "GETT-123"}`): never drawn, they fill the `{{key}}` templates of the Actions users add in `config.toml`, so a user who wants one more Action doesn't rebuild your Plugin as a script. A live view may carry one `image`, a PNG or JPEG as a `path` (relative to the Plugin's folder unless absolute) or base64 `data`, with a `side` (`left`, `right`, `top`, `bottom`); visor draws it with kitty graphics or truecolor half-blocks, whichever the terminal supports, so never write graphics escapes to the terminal yourself.
6. **Check it.** Run `visor plugin check <folder>` and fix what it reports until it ends in `PASS`. It starts the process, validates every message against the schema, refreshes and closes each Widget type with no secrets set (so a required one shows needs setup and that type isn't refreshed), and invokes the made-up Action id `visor-plugin-check`, so none of your real Actions run.
7. **Try it in visor.** Add `[[plugin]] path = "<folder>"` and a `[[widget]]` block of your type to the user's `config.toml`, then run `visor`.

Done when: `visor plugin check` prints `PASS`, and the Widget renders in visor with its Actions listed in the `^K` palette.

### Theme tokens

| token | colours |
|---|---|
| `bg` | background |
| `sel` | selected row background |
| `line` | borders |
| `faint` | least important text, separators |
| `dim` | secondary text |
| `fg` | body text and titles |
| `bright` | emphasis |
| `accent` | links, highlights |
| `warn` | warnings |
| `ok` | success |
| `bad` | errors and failures |
| `info` | informational highlights |
| `alt` | a second accent |

### Publish

Push the folder to GitHub, at the repository root or in a subfolder, and add the `visor-plugin` topic to the repository: that topic is how people find visor Plugins. Users install with `visor plugin install <owner/repo[/subfolder]>` (`--ref <ref>` pins a tag or branch), which also adds a Widget to `config.toml` when the Plugin has a single Widget type and none is placed yet. Official Plugins live one per folder in `oronbz/visor-plugins` and install by bare name: `visor plugin install bitrise` is `oronbz/visor-plugins/bitrise`. To make a Plugin official, open a pull request adding its folder there. Install runs each `[[build]]` step's `run` argv in the Plugin's folder, with no shell and no stdin, so a compiled Plugin builds itself there; list every build command, since visor shows them to the user before asking for trust.
