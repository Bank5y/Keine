-- Annotations belong to one user and one lesson, keyed by the lesson's
-- stable key like learning records, and remember their place by the quoted
-- text and a little text around it, never by offsets (ADR-0003).
CREATE TABLE annotations (
    id         bigserial   PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    course_id  bigint      NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    lesson_key text        NOT NULL,
    -- underline (one colour, the theme's) or highlight (in a colour)
    style      text        NOT NULL CHECK (style IN ('underline', 'highlight')),
    color      text        CHECK (color IN ('yellow', 'green', 'blue', 'pink')),
    quote      text        NOT NULL,
    prefix     text        NOT NULL DEFAULT '',
    suffix     text        NOT NULL DEFAULT '',
    note       text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((style = 'highlight') = (color IS NOT NULL))
);

CREATE INDEX annotations_lesson ON annotations (user_id, course_id, lesson_key);
