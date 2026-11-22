-- Readers' own models (#139, ADR 0010): at most one a reader. The keys are
-- sealed with the master key and shown by their last four only. in_use is
-- the reader's switch, on the row so that deleting the model turns it off.
-- search is how the model's runs reach the web: 'provider', 'tavily' (on
-- the reader's own Tavily key), 'deepseek' (DeepSeek Search / Fetch on the
-- model's key) or 'off'.
CREATE TABLE own_models (
    user_id        bigint      PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    format         text        NOT NULL,
    provider       text        NOT NULL DEFAULT '',
    base_url       text        NOT NULL,
    api_key        bytea,
    key_last4      text        NOT NULL DEFAULT '',
    model          text        NOT NULL,
    context_window integer     NOT NULL,
    search         text        NOT NULL DEFAULT 'off',
    tavily_key     bytea,
    tavily_last4   text        NOT NULL DEFAULT '',
    in_use         boolean     NOT NULL DEFAULT false,
    updated_at     timestamptz NOT NULL DEFAULT now()
);
