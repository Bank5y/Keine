# Coding standards

Read during review. Each rule is a judgement call the CI checks (gofmt, go vet, tests, tsc, `npm run lint`) cannot make. Use the domain words in `GLOSSARY.md`.

## Locks and storage

- **Hold a row lock for database work only.** Slow work (storage reads and copies, a site model's connection test) runs before the transaction; under the lock, re-read what it depended on (a course's `files_dir`, visibility, completion, the draft; a site model's stored key) and start over or refuse when it changed (`ErrCourseBusy`, `ErrModelChanged`). Copying large videos while the row is locked blocks every other update, approval and delete of that course.
- **Decide anything that depends on course state from what was read under the lock.** A mode or permission computed before the lock is a guess until re-checked there.
- **A file missing from storage degrades, it does not fail the whole operation.** Log it and go on (`lessonMarkdown` reads a missing text as empty); one lost file must not block a commit or hide every pending review.

## Untrusted input

- **Every archive extraction caps the total unpacked size**, not just the request body: sum each entry's declared `UncompressedSize64` against the upload limit before storing anything (archive/zip refuses to read past a declared size).

## Identity

- **Chapter keys and lesson keys are separate namespaces.** Keep them in separate maps and sets; `"2"` the chapter and `"2"` the lesson are unrelated.
- **Confirm by stable ID, never by position.** When the user confirms something from a list they saw (skipping a conflict), match it by an ID the system assigned, so an item that took the same index since is not confirmed unseen.

## Errors

- **A new error a user can hit is mapped end to end:** an exported error in its domain package (`internal/course`, `internal/assistant`), a status, code and Chinese message in that package's `write…Error` helper in `internal/server` (`writeCourseError`, `writeModelError`), and the frontend shows the server's message or prevents the action. A `write…Error` helper returns true when there was no error: `if !writeCourseError(w, r, err) { return }`.

## Harness documentation

- **A change to the exploration assistant's harness updates its report in the same PR.** Loop, runtime, retries, context and prompts, search, tools, model adapters, output checks or usage recording: edit `docs/tech/Keine_explore_harness_v1.md`, and the matching `docs/tech/selections/` document when a choice between architectures changed.
- **The report is a technical report for a review meeting**, written in Chinese:
  - no code details (function names, file paths, snippets); draw with Mermaid;
  - technical terms, never example objects (a sample term, lesson title or UI string);
  - written as the settled v1: no 「第一版」「当前实现」「以后」 and no status markers;
  - business first (intent recognition, information retrieval, knowledge capture), then the technical detail behind it;
  - upper-layer cost control (积分、套餐、订阅) stays out; usage is recorded in its own chapter without those words;
  - every tool gets its own third-level section: purpose, parameters, flow, result, errors, limits.
- **Selections are stated, not argued, in the report.** The body says what was chosen with a footnote to 附录 D; each `docs/tech/selections/` document covers the candidate architectures with a diagram each, their trade-offs, and why one was chosen.
- **`docs/reference-out/` is git-ignored** and holds write-ups for the maintainer that are not part of the project. An analysis there is based on source code or technical documentation actually read, never on search results or a README alone, and lists what was read and what stays unverified.
