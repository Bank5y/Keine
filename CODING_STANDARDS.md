# Coding standards

Read during review. Each rule is a judgement call the CI checks (gofmt, go vet, tests, tsc) cannot make. Use the domain words in `GLOSSARY.md`.

## Locks and storage

- **Hold a course row lock for database work only.** Storage reads and copies run before the transaction; under the lock, re-read what they depended on (`files_dir`, visibility, completion, the draft) and start over when it changed (`ErrCourseBusy`). Copying large videos while the row is locked blocks every other update, approval and delete of that course.
- **Decide anything that depends on course state from what was read under the lock.** A mode or permission computed before the lock is a guess until re-checked there.
- **A file missing from storage degrades, it does not fail the whole operation.** Log it and go on (`lessonMarkdown` reads a missing text as empty); one lost file must not block a commit or hide every pending review.

## Untrusted input

- **Every archive extraction caps the total unpacked size**, not just the request body: sum each entry's declared `UncompressedSize64` against the upload limit before storing anything (archive/zip refuses to read past a declared size).

## Identity

- **Chapter keys and lesson keys are separate namespaces.** Keep them in separate maps and sets; `"2"` the chapter and `"2"` the lesson are unrelated.
- **Confirm by stable ID, never by position.** When the user confirms something from a list they saw (skipping a conflict), match it by an ID the system assigned, so an item that took the same index since is not confirmed unseen.

## Errors

- **A new error a user can hit is mapped end to end:** an exported error in `internal/course`, a status, code and Chinese message in `internal/server` (`writeCourseError` / `writeDraftError`), and the frontend shows the server's message or prevents the action.
