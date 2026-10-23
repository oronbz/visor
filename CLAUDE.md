## Navigation

- Package map: `go list -f '{{.ImportPath}}: {{.Doc}}' ./...`; a package's API: `go doc ./internal/<pkg>`.
- Search with `git grep`; `grep -r` also walks `dist/` and agent worktrees under `.claude/`.
- Agent skills: `skills/`, embedded in the binary.
- Run from source: `scripts/dev.sh`.
- Release: `scripts/release.sh X.Y.Z`. Minor for a feature or changed default, patch for fixes.
- A change users or Plugin authors see may also need the READMEs in `oronbz/visor-plugins` (`~/Developer/visor-plugins`).

## Agent skills

### Issue tracker

Issues are tracked in this repo's GitHub Issues via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five-role vocabulary (needs-triage, needs-info, ready-for-agent, ready-for-human, wontfix). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one root `GLOSSARY.md` plus `docs/adr/`. See `docs/agents/domain.md`.
