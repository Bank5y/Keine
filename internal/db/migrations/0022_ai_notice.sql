-- The notice of what goes to third-party services (#132): when the reader
-- first started an analysis after reading it, on any device.
ALTER TABLE users ADD COLUMN ai_notice_read_at timestamptz;
