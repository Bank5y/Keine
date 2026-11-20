-- A site model's provider configuration (#126, ADR 0008): '' for a plain
-- endpoint of its format, or a provider whose rules the adapter applies.
ALTER TABLE site_models ADD COLUMN provider text NOT NULL DEFAULT '' CHECK (provider IN ('', 'deepseek'));
