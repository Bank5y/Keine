-- Removing a library course from my courses keeps the enrollment (and every
-- learning record) and only marks it removed; starting again clears the mark.
ALTER TABLE enrollments ADD COLUMN removed_at timestamptz;
