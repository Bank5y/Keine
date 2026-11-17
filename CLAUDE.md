## Navigation

- **Seeing the UI** (screenshots, PR evidence, design references): `scripts/demo/run.sh` starts a throwaway instance with sample data; drive it with `scripts/demo/shoot.py`, which keeps screenshots in `.scratch/shots/`. Never sign in to the :8080 instance.
- **Frontend tests** (pure logic in `web/src/lib`): `npm test` (vitest), next to the code as `*.test.ts`.
- **Server test helpers** (`newApp`, `uploadCourse`, `publishedCourse`, `addOp`, `putFile`…): `internal/server/helpers_test.go`, `import_test.go` (sample course packages), `draft_test.go`.
- **Course manifest format** (`course.json`): the validator in `internal/course/manifest.go` is the source of truth.
- **Draft operation format** (what a recorded operation carries, before claiming what the draft API returns): `Op` in `internal/course/draft_ops.go`.

## Agent skills

### Issue tracker

Issues are tracked in this repo's GitHub Issues, managed with the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

The five default labels are used as-is: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `GLOSSARY.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.
