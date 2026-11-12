-- A course's files live under courses/{id}/v{files_version}/. A course
-- update writes the next version beside the current one and switches over
-- only once everything is stored, so a failed update leaves the course as it
-- was.
ALTER TABLE courses ADD COLUMN files_version integer NOT NULL DEFAULT 1;
