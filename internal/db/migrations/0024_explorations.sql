-- Explorations (#133, ADR 0007): an analysis the reader pinned. Like an
-- annotation it belongs to one user and one lesson, keyed by the lesson's
-- stable key, and remembers its place by the quoted text and a little text
-- around it (ADR 0003); it is no annotation, and the two may overlap.
-- The exploration document's title, intro and meaning are its own fields;
-- doc_version counts the document's versions (a sync applies against one).
-- snapshot is the request the analysis ran on (the messages with their
-- context, the prompt version), which later turns read instead of the
-- lesson as it is by then.
CREATE TABLE explorations (
    id          bigserial   PRIMARY KEY,
    user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    course_id   bigint      NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    lesson_key  text        NOT NULL,
    quote       text        NOT NULL,
    prefix      text        NOT NULL DEFAULT '',
    suffix      text        NOT NULL DEFAULT '',
    -- attached: shown in the lesson text; pending: its text changed in a
    -- course update and awaits migration; excerpt: kept out of the text
    state       text        NOT NULL DEFAULT 'attached' CHECK (state IN ('attached', 'pending', 'excerpt')),
    title       text        NOT NULL,
    intro       text        NOT NULL,
    meaning     text        NOT NULL,
    doc_version integer     NOT NULL DEFAULT 1,
    snapshot    jsonb       NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX explorations_lesson ON explorations (user_id, course_id, lesson_key);

-- The exploration dialogue is a tree of nodes, each a line of turns. The
-- main node starts with the analysis and has no parent; every other node
-- forks from a turn of its parent. A node's kind never changes.
CREATE TABLE exploration_nodes (
    id             bigserial   PRIMARY KEY,
    exploration_id bigint      NOT NULL REFERENCES explorations (id) ON DELETE CASCADE,
    kind           text        NOT NULL CHECK (kind IN ('main', 'child', 'branch', 'divergent')),
    parent_id      bigint      REFERENCES exploration_nodes (id) ON DELETE CASCADE,
    fork_turn_id   bigint,
    name           text        NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CHECK ((kind = 'main') = (parent_id IS NULL)),
    CHECK ((parent_id IS NULL) = (fork_turn_id IS NULL))
);

CREATE UNIQUE INDEX exploration_nodes_main ON exploration_nodes (exploration_id) WHERE kind = 'main';

-- A turn is a question and its reply. question holds the text and its
-- attachments (a selected passage); meta what the run records besides the
-- reply (own/site, model, prompt version, usage, sources, degraded, an
-- interruption). Within a node turns follow one another: a turn has at
-- most one successor.
CREATE TABLE exploration_turns (
    id         bigserial   PRIMARY KEY,
    node_id    bigint      NOT NULL REFERENCES exploration_nodes (id) ON DELETE CASCADE,
    parent_id  bigint      REFERENCES exploration_turns (id) ON DELETE CASCADE,
    question   jsonb       NOT NULL,
    reply      text        NOT NULL,
    meta       jsonb       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX exploration_turns_node ON exploration_turns (node_id);
CREATE UNIQUE INDEX exploration_turns_successor ON exploration_turns (parent_id) WHERE parent_id IS NOT NULL;

ALTER TABLE exploration_nodes
    ADD FOREIGN KEY (fork_turn_id) REFERENCES exploration_turns (id) ON DELETE CASCADE;
