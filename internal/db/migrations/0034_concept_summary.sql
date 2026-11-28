-- The course-level summary of a concept index (#225): once its lessons are
-- extracted, the index merges their entries into concepts, picks each
-- concept's primary section, marks the generic ones, and writes a summary
-- of each chapter and of the course.

-- the stage a build is in: extracting its lessons, then summarizing
ALTER TABLE concept_indexes
    ADD COLUMN stage          text NOT NULL DEFAULT 'extract' CHECK (stage IN ('extract', 'summarize')),
    ADD COLUMN summary        text NOT NULL DEFAULT '',
    -- a hash of what the course summary was written from, and its prompt:
    -- a build whose chapters' summaries are the same keeps it
    ADD COLUMN summary_hash   text NOT NULL DEFAULT '',
    ADD COLUMN summary_error  text;

-- A concept of an index: entries of one name (or one's alias another's
-- name) merged, its primary section, and whether it is generic (in more
-- than 35% of the lessons, so left out of recall).
CREATE TABLE index_concepts (
    id                    bigserial PRIMARY KEY,
    index_id              bigint  NOT NULL REFERENCES concept_indexes (id) ON DELETE CASCADE,
    position              integer NOT NULL,
    name                  text    NOT NULL,
    aliases               text[]  NOT NULL DEFAULT '{}',
    summary               text    NOT NULL DEFAULT '',
    generic               boolean NOT NULL DEFAULT false,
    lessons               integer NOT NULL,
    primary_lesson_key    text    NOT NULL,
    primary_section_id    text    NOT NULL DEFAULT '',
    primary_section_title text    NOT NULL DEFAULT ''
);
CREATE INDEX index_concepts_index ON index_concepts (index_id, position);

-- the concept an entry was merged into
ALTER TABLE concept_entries ADD COLUMN concept_id bigint REFERENCES index_concepts (id) ON DELETE SET NULL;

-- A chapter's summary in an index, from its lessons' summaries; too_long:
-- the chapter has more lessons than one summary covers well, shown to the
-- admin and not split.
CREATE TABLE index_chapters (
    index_id     bigint  NOT NULL REFERENCES concept_indexes (id) ON DELETE CASCADE,
    chapter_key  text    NOT NULL,
    summary      text    NOT NULL DEFAULT '',
    summary_hash text    NOT NULL DEFAULT '',
    error        text,
    too_long     boolean NOT NULL DEFAULT false,
    PRIMARY KEY (index_id, chapter_key)
);
