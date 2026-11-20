-- Anthropic Messages models (#129, ADR 0008), and the provider route: a
-- model of a format whose provider runs web_search and web_fetch itself
-- searches with those instead of a Keine tool.
ALTER TABLE site_models DROP CONSTRAINT site_models_format_check;
ALTER TABLE site_models ADD CONSTRAINT site_models_format_check CHECK (format IN ('openai', 'anthropic'));
ALTER TABLE site_models DROP CONSTRAINT site_models_search_route_check;
ALTER TABLE site_models ADD CONSTRAINT site_models_search_route_check
    CHECK (search_route IN ('', 'tavily', 'deepseek', 'provider', 'off'));
