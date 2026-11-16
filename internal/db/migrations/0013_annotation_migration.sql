-- A course update that changes a lesson's text keeps the text it replaced,
-- for good (ADR-0003): annotation migration shows it beside the new text.
CREATE TABLE previous_texts (
    id          bigserial   PRIMARY KEY,
    course_id   bigint      NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    lesson_key  text        NOT NULL,
    markdown    text        NOT NULL,
    replaced_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX previous_texts_lesson ON previous_texts (course_id, lesson_key);

-- Where an annotation stands after course updates:
--   attached  shown in the lesson text
--   pending   its text changed and the update could not put it back
--             (awaiting annotation migration); previous_text_id is the
--             text it was last placed in
--   changed   kept as an excerpt after its text changed
--   deleted   an excerpt whose lesson an update deleted
ALTER TABLE annotations
    ADD COLUMN state text NOT NULL DEFAULT 'attached'
        CHECK (state IN ('attached', 'pending', 'changed', 'deleted')),
    ADD COLUMN previous_text_id bigint REFERENCES previous_texts (id);
