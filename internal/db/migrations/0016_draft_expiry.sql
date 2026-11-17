-- Draft expiry (ADR-0006): a draft untouched for 30 days is deleted with its
-- temporary area; its owner is reminded once on day 25. reminded_at is when
-- that reminder went out; a reminder older than the draft's last change
-- belongs to an earlier idle spell, so the next one reminds again.
ALTER TABLE course_drafts ADD COLUMN reminded_at timestamptz;
CREATE INDEX course_drafts_updated_at ON course_drafts (updated_at);
