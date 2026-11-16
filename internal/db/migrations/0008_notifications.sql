-- Notifications are kept forever. Each has a kind and a kind-specific JSON
-- payload that snapshots what it is about (course title, reason...), so it
-- still reads right after the course changes or is gone.
CREATE TABLE notifications (
    id         bigserial PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       text        NOT NULL,
    payload    jsonb       NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    read_at    timestamptz
);
CREATE INDEX notifications_by_user ON notifications (user_id, id DESC);
CREATE INDEX notifications_unread ON notifications (user_id) WHERE read_at IS NULL;
