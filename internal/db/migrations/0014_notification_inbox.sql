-- Some notifications (annotations awaiting migration) stay in the bell's
-- inbox after they are read, until the user dismisses them or there is
-- nothing left for them to point at.
ALTER TABLE notifications ADD COLUMN dismissed_at timestamptz;
