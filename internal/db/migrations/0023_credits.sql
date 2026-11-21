-- Credits, plans and usage (#130, ADR 0010). Every reader is on a plan,
-- whose daily credits (null: unlimited) reset at the server's midnight
-- without carrying over. "不限" is built in and where admins start; the
-- default plan is where new readers start.
CREATE TABLE plans (
    id            bigserial PRIMARY KEY,
    name          text             NOT NULL,
    daily_credits double precision CHECK (daily_credits >= 0),
    is_default    boolean          NOT NULL DEFAULT false,
    builtin       boolean          NOT NULL DEFAULT false,
    created_at    timestamptz      NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX plans_name_key ON plans (lower(name));
CREATE UNIQUE INDEX plans_default_key ON plans (is_default) WHERE is_default;

INSERT INTO plans (name, daily_credits, builtin) VALUES ('不限', NULL, true);
INSERT INTO plans (name, daily_credits, is_default) VALUES ('基础', 100, true);

ALTER TABLE users ADD COLUMN plan_id bigint REFERENCES plans (id);
UPDATE users SET plan_id = (SELECT id FROM plans WHERE CASE WHEN users.role = 'admin' THEN builtin ELSE is_default END);
ALTER TABLE users ALTER COLUMN plan_id SET NOT NULL;

-- A new account starts on the default plan, an admin on "不限", unless
-- one is given; an account made the admin later moves to "不限".
CREATE FUNCTION users_plan() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'INSERT' AND NEW.plan_id IS NULL THEN
        NEW.plan_id := (SELECT id FROM plans WHERE CASE WHEN NEW.role = 'admin' THEN builtin ELSE is_default END);
    ELSIF TG_OP = 'UPDATE' AND NEW.role = 'admin' AND OLD.role <> 'admin' THEN
        NEW.plan_id := (SELECT id FROM plans WHERE builtin);
    END IF;
    RETURN NEW;
END $$ LANGUAGE plpgsql;
CREATE TRIGGER users_plan BEFORE INSERT OR UPDATE OF role ON users FOR EACH ROW EXECUTE FUNCTION users_plan();

-- One row a run, apart from any message (no text is kept): the unified
-- usage, a null where the provider did not report it, and the credits at
-- the rates of the time. estimated: the usage was worked out locally, as
-- for a run cut short.
CREATE TABLE usage_records (
    id          bigserial PRIMARY KEY,
    user_id     bigint           NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind        text             NOT NULL,
    model       text             NOT NULL,
    input       bigint,
    cache_read  bigint,
    cache_write bigint,
    output      bigint,
    reasoning   bigint,
    searches    integer          NOT NULL DEFAULT 0,
    credits     double precision NOT NULL,
    estimated   boolean          NOT NULL DEFAULT false,
    created_at  timestamptz      NOT NULL
);
CREATE INDEX usage_records_user_time ON usage_records (user_id, created_at);
CREATE INDEX usage_records_time ON usage_records (created_at);
