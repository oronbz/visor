# Coding Standards

Judgement calls for review. gofmt, `go vet` and the tests run in CI; the rules below are what they can't check.

## Vocabulary

- Domain terms in identifiers, comments, test names and UI text use `GLOSSARY.md`'s spelling and capitalisation: Widget, Item, Action, Stack, Summary, Payload.
- One name per concept across code, tests and fakes. A concept called `issueType` in code is `issueType` in the fixture too, not `kind` or `typ`.
- A change that contradicts an ADR in `docs/adr/` says so.

## Comments

- A comment says why, never what the code does. Doc comments on non-obvious types and members only.
- A doc comment fits in a few lines. Behaviour that needs more belongs in a test that pins it.

## Duplication

- A second copy of a helper, loop or parse is extracted, not repeated: one config editor setup, one secret-settings loop, one `secret:` id parser.

## Tests

- Test names read as the behaviour they pin, such as `TestPluginInstallPlacesAWidgetOfItsType`.
- Tests drive the real seam: `hosttest` for the TUI, the `plugintest` fixture for the protocol, `cli.Run` with a temp home for subcommands.
- Every branch of a mapping or table has a case, not only the first few.

## Docs

- Behaviour users see changes the docs that describe it in the same diff: `README.md`, `skills/`, and the config reference in `internal/config/reference.go`.
