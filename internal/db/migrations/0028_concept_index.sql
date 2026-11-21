-- The concept index of a course version (#158, ADR 0011): one a version
-- of the course's files, built only when an admin asks. A version without
-- a row has none yet.
CREATE TABLE concept_indexes (
    id          bigserial PRIMARY KEY,
    course_id   bigint      NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    files_dir   text        NOT NULL,
    status      text        NOT NULL CHECK (status IN ('building', 'built', 'failed')),
    -- why the last build failed: a run error code
    error       text,
    started_at  timestamptz NOT NULL,
    finished_at timestamptz,
    UNIQUE (course_id, files_dir)
);

-- The lessons an index has extracted (or copied), by a hash of the text
-- they were extracted from: a later version whose lesson has the same
-- text copies its entries instead of extracting it again (ADR 0005).
CREATE TABLE concept_index_lessons (
    index_id   bigint NOT NULL REFERENCES concept_indexes (id) ON DELETE CASCADE,
    lesson_key text   NOT NULL,
    text_hash  text   NOT NULL,
    PRIMARY KEY (index_id, lesson_key)
);

-- An entry: a concept of a lesson, the section it is in (its anchor and
-- heading, '' for the lesson's top), and a summary, from the course text
-- only.
CREATE TABLE concept_entries (
    id            bigserial PRIMARY KEY,
    index_id      bigint  NOT NULL,
    lesson_key    text    NOT NULL,
    position      integer NOT NULL,
    name          text    NOT NULL,
    aliases       text[]  NOT NULL DEFAULT '{}',
    section_id    text    NOT NULL DEFAULT '',
    section_title text    NOT NULL DEFAULT '',
    summary       text    NOT NULL,
    FOREIGN KEY (index_id, lesson_key) REFERENCES concept_index_lessons (index_id, lesson_key) ON DELETE CASCADE
);
CREATE INDEX concept_entries_index ON concept_entries (index_id, lesson_key, position);

-- The site's own runs (building an index) belong to no reader.
ALTER TABLE usage_records ALTER COLUMN user_id DROP NOT NULL;
