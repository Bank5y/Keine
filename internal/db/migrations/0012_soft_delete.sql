-- Deleting a course whose subscribers retain lessons only marks it (ADR-0004):
-- the library and the uploader no longer see it, subscribers keep reading its
-- freeze, and it is removed for good once nobody retains any of its lessons.
ALTER TABLE courses ADD COLUMN deleted_at timestamptz;
