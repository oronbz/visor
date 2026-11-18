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

Name domain concepts (identifiers, tests, tickets, PR text) with the terms in root `GLOSSARY.md`; never use a word from its _Avoid_ lists. ADRs live in `docs/adr/`; see `docs/agents/domain.md`.
