-- When a course leaves the library (ADR-0004), the folder of files it used
-- then and its structure are frozen for the subscribers who keep retained
-- lessons. A course has at most one freeze: it lasts until the course is
-- public again or nobody retains any of its lessons. While it lasts,
-- files_dir is never dropped, even if the course moves on to new files.
CREATE TABLE course_freezes (
    course_id  bigint      PRIMARY KEY REFERENCES courses (id) ON DELETE CASCADE,
    -- why the course left the library: 'unpublished' (or 'deleted')
    reason     text        NOT NULL,
    files_dir  text        NOT NULL,
    -- the course information, chapters and lessons as they were (JSON)
    snapshot   jsonb       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- The lessons a subscriber keeps reading from a course's freeze.
CREATE TABLE retained_lessons (
    course_id  bigint NOT NULL REFERENCES course_freezes (course_id) ON DELETE CASCADE,
    user_id    bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    lesson_key text   NOT NULL,
    PRIMARY KEY (course_id, user_id, lesson_key)
);
CREATE INDEX retained_lessons_user_id ON retained_lessons (user_id);
