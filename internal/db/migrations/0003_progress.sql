-- Learning records live on lessons only, keyed by the lesson's stable key
-- from the course manifest, so they survive a course update that recreates
-- the lesson rows. Chapter and course progress are computed from them.
CREATE TABLE lesson_progress (
    user_id    bigint           NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    course_id  bigint           NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    lesson_key text             NOT NULL,
    done       boolean          NOT NULL DEFAULT false,
    -- where the reader was: scroll ratio of a text, or seconds into a video
    position   double precision NOT NULL DEFAULT 0,
    updated_at timestamptz      NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, course_id, lesson_key)
);

ALTER TABLE enrollments ADD COLUMN last_lesson_key text;
