-- Subscribing replaces starting to learn. A course reader is anyone who has
-- read or subscribed to a course: the row keeps where they last were, and
-- subscribed_at tells whether the course is in their my courses. Reading a
-- lesson no longer subscribes. Every enrollment not removed becomes a
-- subscription; removed ones stay as plain readers.
ALTER TABLE enrollments RENAME TO course_readers;
ALTER TABLE course_readers ADD COLUMN subscribed_at timestamptz;
UPDATE course_readers SET subscribed_at = created_at WHERE removed_at IS NULL;
ALTER TABLE course_readers DROP COLUMN removed_at;
