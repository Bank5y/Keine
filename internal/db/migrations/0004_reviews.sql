-- Requests an admin reviews: making a private course public, and (later)
-- replacing a public course's content.
CREATE TABLE review_requests (
    id           bigserial PRIMARY KEY,
    kind         text        NOT NULL CHECK (kind IN ('publish', 'update')),
    course_id    bigint      NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    requester_id bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    state        text        NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'approved', 'rejected', 'withdrawn')),
    reason       text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    decided_at   timestamptz,
    decided_by   bigint      REFERENCES users (id) ON DELETE SET NULL
);
CREATE INDEX review_requests_pending ON review_requests (created_at) WHERE state = 'pending';
CREATE UNIQUE INDEX review_requests_one_pending_publish ON review_requests (course_id) WHERE kind = 'publish' AND state = 'pending';
