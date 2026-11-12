-- An update request carries a whole new course package: its manifest and
-- the folder its files were stored in, plus the files folder the course used
-- when it was submitted (to tell when a request is based on stale content).
ALTER TABLE review_requests
    ADD COLUMN files_dir text,
    ADD COLUMN base_dir  text,
    ADD COLUMN manifest  bytea;

-- Users whose update requests were approved for a course they did not upload.
CREATE TABLE course_contributors (
    course_id  bigint      NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (course_id, user_id)
);
