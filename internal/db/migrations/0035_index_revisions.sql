-- An admin's revisions of a course's concept index (#226): they belong to
-- the course, not a version, and are laid over recall when it runs, so
-- they take effect at once and still do after a new version is indexed.
--   concept: a concept, by its name as extracted (normalized): renamed,
--            new aliases or summary, weighted, disabled, marked generic or
--            not (null: as extracted)
--   new:     a concept the extraction missed, at a lesson's section
--   exclude: a lesson (section_id '') or a section left out of recall
-- lesson_key and section_id are what the revision points at; text_hash is
-- the hash of that lesson's passages when it was made or last confirmed:
-- when it differs, or the section is gone, the revision may be outdated.
CREATE TABLE index_revisions (
    id            bigserial PRIMARY KEY,
    course_id     bigint      NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    kind          text        NOT NULL CHECK (kind IN ('concept', 'new', 'exclude')),
    concept_key   text        NOT NULL DEFAULT '',
    name          text,
    aliases       text[],
    summary       text,
    weight        real        NOT NULL DEFAULT 1 CHECK (weight IN (0.5, 1, 2, 4)),
    disabled      boolean     NOT NULL DEFAULT false,
    generic       boolean,
    lesson_key    text        NOT NULL DEFAULT '',
    section_id    text        NOT NULL DEFAULT '',
    section_title text        NOT NULL DEFAULT '',
    text_hash     text        NOT NULL DEFAULT '',
    updated_at    timestamptz NOT NULL
);
CREATE UNIQUE INDEX index_revisions_concept ON index_revisions (course_id, concept_key) WHERE kind = 'concept';
CREATE UNIQUE INDEX index_revisions_new ON index_revisions (course_id, concept_key) WHERE kind = 'new';
CREATE UNIQUE INDEX index_revisions_exclude ON index_revisions (course_id, lesson_key, section_id) WHERE kind = 'exclude';
