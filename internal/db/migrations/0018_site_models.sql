-- The models the exploration assistant runs on, configured by admins
-- (#124; spec #123 moved the rates here from the environment). The key is
-- sealed with the deployment's master key; key_last4 is all the API shows.
-- Rates are credits per million tokens (search: per request); they are all
-- set or all null, and a model without rates cannot serve a turn kind.
CREATE TABLE site_models (
    id               bigserial PRIMARY KEY,
    format           text NOT NULL CHECK (format IN ('openai')),
    base_url         text NOT NULL,
    api_key          bytea,
    key_last4        text NOT NULL DEFAULT '',
    model            text NOT NULL,
    display_name     text NOT NULL,
    context_window   integer CHECK (context_window > 0),
    rate_input       double precision,
    rate_cache_read  double precision,
    rate_cache_write double precision,
    rate_output      double precision,
    rate_search      double precision,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CHECK ((rate_input IS NULL) = (rate_output IS NULL)
       AND (rate_input IS NULL) = (rate_cache_read IS NULL)
       AND (rate_input IS NULL) = (rate_cache_write IS NULL)
       AND (rate_input IS NULL) = (rate_search IS NULL))
);

-- Which model serves each turn kind. The assistant is available once all
-- three kinds have a row.
CREATE TABLE turn_models (
    kind     text PRIMARY KEY CHECK (kind IN ('analysis', 'followup', 'sync')),
    model_id bigint NOT NULL REFERENCES site_models (id),
    thinking boolean NOT NULL DEFAULT false
);
