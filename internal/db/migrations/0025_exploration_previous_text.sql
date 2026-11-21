-- Explorations through a course update (#135): like an annotation, one that
-- cannot be put back in a lesson's new text awaits migration against the
-- text it was shown in, kept in previous_texts.
ALTER TABLE explorations
    ADD COLUMN previous_text_id bigint REFERENCES previous_texts (id);
