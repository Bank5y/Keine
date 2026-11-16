-- A course draft (ADR-0006): one user's pending changes to one course, kept
-- as the ordered list of operations they made, not as a snapshot. Undo drops
-- the last operation; committing applies them all to the course's latest
-- version. A user has at most one draft per course.
CREATE TABLE course_drafts (
    id         bigserial PRIMARY KEY,
    course_id  bigint      NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- the course's files_dir when the draft was started
    base_dir   text        NOT NULL,
    -- JSON array of operations, oldest first
    ops        jsonb       NOT NULL DEFAULT '[]',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (course_id, user_id)
);
CREATE INDEX course_drafts_user_id ON course_drafts (user_id);
