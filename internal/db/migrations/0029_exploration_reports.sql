-- Exploration reports (#160, ADR 0012): a write-up of the whole dialogue
-- tree, kept under its exploration. Each generation is a new version and
-- none is replaced. refs are the references, made by the server from the
-- turns' source lists; turn_ids the turns the version took in; meta the
-- model, prompt, answer style and usage of its run.
CREATE TABLE exploration_reports (
    id             bigserial   PRIMARY KEY,
    exploration_id bigint      NOT NULL REFERENCES explorations (id) ON DELETE CASCADE,
    version        integer     NOT NULL,
    title          text        NOT NULL,
    summary        text        NOT NULL,
    body           text        NOT NULL,
    points         text        NOT NULL,
    refs           jsonb       NOT NULL,
    turn_ids       bigint[]    NOT NULL,
    meta           jsonb       NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (exploration_id, version)
);
