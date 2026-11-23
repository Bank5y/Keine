-- How many tokens one call of a long output (a concept extraction, a
-- report) may write on a site model (#169); null leaves it to a default
-- every model takes. What a call leaves unwritten, the next continues.
ALTER TABLE site_models ADD COLUMN max_output integer CHECK (max_output > 0);
