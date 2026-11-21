-- An exploration document is its title, concept and concept explanation,
-- fixed once pinned (ADR 0012): the intro is the concept and the meaning
-- the concept explanation, kept as they are; with no sync, the document
-- has no versions.
ALTER TABLE explorations RENAME COLUMN intro TO concept;
ALTER TABLE explorations RENAME COLUMN meaning TO explanation;
ALTER TABLE explorations DROP COLUMN doc_version;
