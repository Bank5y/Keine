-- Sync is gone; the report and the concept index are kinds of their own
-- (ADR 0012). A site that mapped sync keeps the assistant: both new kinds
-- take sync's model and thinking switch.
ALTER TABLE turn_models DROP CONSTRAINT turn_models_kind_check;

INSERT INTO turn_models (kind, model_id, thinking)
SELECT k.kind, t.model_id, t.thinking
FROM turn_models t, (VALUES ('report'), ('concept_index')) AS k (kind)
WHERE t.kind = 'sync';

DELETE FROM turn_models WHERE kind = 'sync';

ALTER TABLE turn_models ADD CONSTRAINT turn_models_kind_check
    CHECK (kind IN ('analysis', 'followup', 'report', 'concept_index'));
