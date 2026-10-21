# Glossary

**Plugin**: An installable package that provides one or more Widget types.

**Widget type**: A kind of tile a Plugin provides, such as Weather or Review requests. It becomes visible only when placed as a Widget.

**Widget**: One placed, configured tile on the dashboard, an instance of a Widget type. The same Widget type can be placed more than once with different settings (weather for two cities, two Jira queries). Some Widgets list Items (pull requests, reminders); others are live views with no Items (clock, CPU gauge, weather).

**Secret setting**: A Widget type's setting that holds a credential, such as an API token. visor asks for it in a masked prompt through the Widget's "Set <name>" Action and keeps it in the OS keyring, never in config.toml. It belongs to the Plugin, not to one Widget, so every Widget of the Plugin shares it, and uninstalling the Plugin deletes it. A Plugin never stores a credential itself.

**Item**: A single row in a list Widget, such as a pull request, a ticket, a reminder or a meeting.

**Summary**: Lines a list Widget pins above its Items, such as headline numbers, a chart or a bar, so one Widget can show both. They stay put while the Items scroll. It isn't called a header because Group headers already exist.

**Action**: Something the user can do, either to an Item (open, approve, complete) or to a whole Widget (add a task, change the calendar month, edit the note). Some Actions ask for confirmation before they run because they can't be taken back.

**Layout**: The saved arrangement and sizes of Widgets on the dashboard, including which Widgets are hidden. Hiding a Widget changes only the Layout; removing a Widget takes it off the dashboard.

**Stack**: A cell of the Layout that holds its own rows instead of a single Widget, so Widgets can sit one above the other beside a taller neighbour. This is what makes a full-height column possible: one Widget in its cell, the rest stacked beside it.

**Status**: How current and healthy a Widget's data is: *loading* (nothing to show yet), *ok*, *stale* (the data shown is old or its last refresh failed), *error* (nothing to show because fetching failed), or *needs setup* (missing access or settings, with a way to fix it).
