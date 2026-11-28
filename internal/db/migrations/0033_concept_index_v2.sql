-- Concept index extraction v2 (#224): lessons are extracted side by side
-- and one failing no longer stops the rest, so each lesson has its own
-- status; an index where some failed is partial and still links. An index
-- records the prompt version and model it was extracted with, and is stale
-- once they are not the current ones; the indexes before this were
-- extracted with concept-index-1.
ALTER TABLE concept_indexes DROP CONSTRAINT concept_indexes_status_check;
ALTER TABLE concept_indexes ADD CONSTRAINT concept_indexes_status_check CHECK (status IN ('building', 'built', 'partial', 'failed'));
ALTER TABLE concept_indexes
    ADD COLUMN prompt_version text NOT NULL DEFAULT 'concept-index-1',
    ADD COLUMN model          text NOT NULL DEFAULT '',
    ADD COLUMN model_name     text NOT NULL DEFAULT '';

-- A lesson of an index: pending until its extraction starts, then running,
-- then done or failed (with a run error code). reused: its entries were
-- kept from an earlier extraction of the same text, prompt and model.
ALTER TABLE concept_index_lessons
    ADD COLUMN status         text    NOT NULL DEFAULT 'done' CHECK (status IN ('pending', 'running', 'done', 'failed')),
    ADD COLUMN error          text,
    ADD COLUMN reused         boolean NOT NULL DEFAULT false,
    ADD COLUMN prompt_version text    NOT NULL DEFAULT 'concept-index-1',
    ADD COLUMN model          text    NOT NULL DEFAULT '',
    -- the lesson's summary, as the extraction wrote it
    ADD COLUMN summary        text    NOT NULL DEFAULT '';

-- whether the lesson defines the concept or uses one defined elsewhere
ALTER TABLE concept_entries ADD COLUMN role text NOT NULL DEFAULT 'define' CHECK (role IN ('define', 'use'));
