-- An update request made from a course draft keeps the draft's operations
-- (the ones that applied when it was filed), so approving it replays them on
-- the course's latest version (ADR-0006) instead of overwriting updates that
-- came in between. A whole-package request has none.
ALTER TABLE review_requests ADD COLUMN ops jsonb;

-- Every deleted draft leaves its temporary area to remove, whatever deleted
-- it: discarding, committing, expiry, or the cascade from a deleted user or
-- course. The area is removed right away; a row stays here until that worked,
-- so the periodic sweep retries without listing every course's files.
CREATE TABLE draft_area_drops (
    draft_id  bigint PRIMARY KEY,
    course_id bigint NOT NULL
);

CREATE FUNCTION record_draft_area_drop() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO draft_area_drops (draft_id, course_id) VALUES (OLD.id, OLD.course_id) ON CONFLICT DO NOTHING;
    RETURN OLD;
END $$;

CREATE TRIGGER course_drafts_area_drop AFTER DELETE ON course_drafts
    FOR EACH ROW EXECUTE FUNCTION record_draft_area_drop();
