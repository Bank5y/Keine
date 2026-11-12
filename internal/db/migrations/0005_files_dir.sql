-- A course's files live under courses/{id}/{files_dir}/. A course update
-- stores the new files in a fresh folder beside the current one and switches
-- files_dir only once everything is stored, so a failed update leaves the
-- course as it was; an update request keeps its files in its own folder
-- until an admin approves it.
ALTER TABLE courses ADD COLUMN files_dir text NOT NULL DEFAULT 'v1';
