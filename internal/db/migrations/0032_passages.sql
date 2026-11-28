-- The passages of a course version (#223, ADR 0015): its lessons' sections
-- cut into overlapping pieces, for BM25 recall. Built for the course's
-- current version as soon as it has one, with no model and no admin; a
-- version keeps its own, so a new one leaves an older one's untouched. A
-- version with a set has its passages, maybe none.
CREATE TABLE passage_sets (
    course_id bigint      NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    files_dir text        NOT NULL,
    built_at  timestamptz NOT NULL,
    PRIMARY KEY (course_id, files_dir)
);

-- A passage: a piece of a section's text (its anchor and heading, '' for
-- the text above the lesson's first heading), in lesson order.
CREATE TABLE passages (
    course_id     bigint  NOT NULL,
    files_dir     text    NOT NULL,
    lesson_key    text    NOT NULL,
    position      integer NOT NULL,
    section_id    text    NOT NULL DEFAULT '',
    section_title text    NOT NULL DEFAULT '',
    body          text    NOT NULL,
    PRIMARY KEY (course_id, files_dir, lesson_key, position),
    FOREIGN KEY (course_id, files_dir) REFERENCES passage_sets (course_id, files_dir) ON DELETE CASCADE
);
