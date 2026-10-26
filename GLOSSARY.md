# Glossary

**Plugin**: An installable package that provides one or more Widget types.

**Official Plugin**: A Plugin in a folder of `oronbz/visor-plugins`. It installs by its bare name, so `visor plugin install bitrise` installs `oronbz/visor-plugins/bitrise`.

**Widget type**: A kind of tile a Plugin provides, such as Weather or Review requests. It becomes visible only when placed as a Widget.

**Widget**: One placed, configured tile on the dashboard, an instance of a Widget type. The same Widget type can be placed more than once with different settings (weather for two cities, two Jira queries). Some Widgets list Items (pull requests, reminders); others are live views with no Items (clock, system gauges, weather).

**Secret setting**: A Widget type's setting that holds a credential, such as an API token. visor asks for it in a masked prompt through the Widget's "Set <name>" Action and keeps it in the OS keyring, never in config.toml. It belongs to the Plugin, not to one Widget, so every Widget of the Plugin shares it, and uninstalling the Plugin deletes it. A Plugin never stores a credential itself.

**Payload**: Everything a Widget gives visor to draw at one moment: either a live view's lines, or a list Widget's Items under its columns with an optional Summary. Either kind may carry Widget Actions. A Plugin sends one in reply to each refresh.

**Image**: A PNG or JPEG a live view shows inside its Payload, kept at its aspect ratio. visor draws it with the terminal's own graphics where it can, and in half blocks where it can't.

**Item**: A single row in a list Widget, such as a pull request, a ticket, a reminder or a meeting.

**Summary**: Lines a list Widget pins above its Items, such as headline numbers, a chart or a bar, so one Widget can show both. They stay put while the Items scroll. It isn't called a header because Group headers already exist.

**Action**: Something the user can do, either to an Item (open, approve, complete), to a whole Widget (add a task, change the calendar month, edit the note), or to visor itself (add a Widget, upgrade visor). Some Actions ask for confirmation before they run because they can't be taken back. The user can add their own to any Widget in its `[[widget]]` block: commands visor runs itself, filled from the selected Item's values.

**Action palette**: The one searchable list of every Action visor offers right now: each visible Widget's and visor's own.

**Layout**: The saved arrangement and sizes of Widgets on the dashboard, including which Widgets are hidden. Hiding a Widget changes only the Layout; removing a Widget takes it off the dashboard.

**Stack**: A cell of the Layout that holds its own rows instead of a single Widget, so Widgets can sit one above the other beside a taller neighbour. This is what makes a full-height column possible: one Widget in its cell, the rest stacked beside it.

**Status**: How current and healthy a Widget's data is: *loading* (nothing to show yet), *ok*, *stale* (the data shown is old or its last refresh failed), *error* (nothing to show because fetching failed), or *needs setup* (missing access or settings, with a way to fix it).

**Upgrade**: Moving visor itself to a newer release. Plugins are updated, not upgraded, and a Widget's data is refreshed.
