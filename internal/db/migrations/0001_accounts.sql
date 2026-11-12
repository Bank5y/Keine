CREATE TABLE users (
    id         bigserial PRIMARY KEY,
    username   text        NOT NULL,
    role       text        NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
    disabled   boolean     NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_username_key ON users (lower(username));

-- One user can sign in through several providers. The password provider keeps
-- the username as subject and the bcrypt hash as secret; OIDC providers will
-- keep the issuer's subject and no secret.
CREATE TABLE identities (
    id         bigserial PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider   text        NOT NULL,
    subject    text        NOT NULL,
    secret     text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, subject)
);
CREATE INDEX identities_user_id ON identities (user_id);

CREATE TABLE sessions (
    token_hash bytea       PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX sessions_user_id ON sessions (user_id);

CREATE TABLE settings (
    key   text  PRIMARY KEY,
    value jsonb NOT NULL
);
