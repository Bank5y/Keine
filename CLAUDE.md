## Navigation

- **Seeing the UI** (screenshots, PR evidence, design references): `scripts/demo/run.sh` starts a throwaway instance with sample data; never sign in to the :8080 instance.
- **Server test helpers** (`newApp`, `uploadCourse`, `publishedCourse`, `addOp`, `putFile`…): `internal/server/helpers_test.go`, `import_test.go` (sample course packages), `draft_test.go`.
- **Course manifest format** (`course.json`): the validator in `internal/course/manifest.go` is the source of truth.

## Agent skills

### Issue tracker

Issues are tracked in this repo's GitHub Issues, managed with the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

The five default labels are used as-is: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `GLOSSARY.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.
