-- How each site model's runs reach the web (#128, ADR 0008): '' resolves
-- from the provider and what is configured (DeepSeek Search / Fetch for a
-- DeepSeek model, else Tavily when the site has a key), 'tavily' and
-- 'deepseek' name the Keine tool route's search service, 'off' is the
-- deployer's choice of no web at all. A model whose route resolves to
-- nothing leaves the assistant unavailable.
ALTER TABLE site_models ADD COLUMN search_route text NOT NULL DEFAULT ''
    CHECK (search_route IN ('', 'tavily', 'deepseek', 'off'));

-- The site's Tavily key, sealed with the master key like the model keys;
-- one row or none.
CREATE TABLE site_tavily (
    id         boolean PRIMARY KEY DEFAULT true CHECK (id),
    api_key    bytea NOT NULL,
    key_last4  text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now()
);
