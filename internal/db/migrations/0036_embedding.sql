-- Embeddings (#227, ADR 0015): one site-wide setting, off by default; an
-- OpenAI-compatible API (external) or a small model run in process
-- (local). generation goes up whenever the provider or model changes:
-- vectors of an older generation are stale and embedded again.
CREATE TABLE embedding_settings (
    id          integer          PRIMARY KEY CHECK (id = 1),
    provider    text             NOT NULL DEFAULT 'off' CHECK (provider IN ('off', 'external', 'local')),
    base_url    text             NOT NULL DEFAULT '',
    model       text             NOT NULL DEFAULT '',
    -- the external API's key, sealed with the master key
    api_key     bytea,
    -- credits per million tokens of the external API
    rate        double precision NOT NULL DEFAULT 0,
    local_model text             NOT NULL DEFAULT '',
    mirror      text             NOT NULL DEFAULT 'https://huggingface.co',
    generation  bigint           NOT NULL DEFAULT 0,
    dims        integer          NOT NULL DEFAULT 0,
    updated_at  timestamptz      NOT NULL DEFAULT now()
);
INSERT INTO embedding_settings (id) VALUES (1);

-- A passage's vector, of the generation that embedded it; kept in a plain
-- column and compared in memory (no pgvector).
CREATE TABLE passage_vectors (
    course_id  bigint  NOT NULL,
    files_dir  text    NOT NULL,
    lesson_key text    NOT NULL,
    position   integer NOT NULL,
    generation bigint  NOT NULL,
    vector     real[]  NOT NULL,
    PRIMARY KEY (course_id, files_dir, lesson_key, position),
    FOREIGN KEY (course_id, files_dir, lesson_key, position) REFERENCES passages (course_id, files_dir, lesson_key, position) ON DELETE CASCADE
);
