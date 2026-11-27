package course

import (
	"archive/zip"
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/auth"
)

// ErrUpdateRequestRequired means a public course changes through an update
// request, not directly.
var ErrUpdateRequestRequired = errors.New("public courses are updated through an update request")

// Update replaces a course's content with a new course package: its
// information, chapters, lessons and files all follow the new manifest.
// The uploader updates their private course directly; admins update public
// courses directly, but never another user's private course.
// The new files are stored beside the old ones and the course switches over
// in one transaction, so a failed update leaves the course as it was.
func (s *Service) Update(ctx context.Context, viewer auth.User, id int64, zr *zip.Reader) error {
	sm, err := s.loadToChange(ctx, viewer, id)
	if err == nil {
		switch {
		case sm.Visibility == Private && !sm.IsUploader && !canView(viewer, sm):
			err = ErrNotFound // others' private courses stay invisible
		case sm.Visibility == Private && !sm.IsUploader:
			err = ErrForbidden // an admin reviewing it still may not change it
		case sm.Visibility == Public && !viewer.IsAdmin():
			err = ErrUpdateRequestRequired
		}
	}
	if err != nil {
		return err
	}
	pp, err := parsePackage(zr)
	if err != nil {
		return err
	}
	next := newFilesDir()
	if err := s.storeFiles(ctx, pp, filesPrefix(sm.ID, next)); err != nil {
		_ = s.files.DeletePrefix(context.WithoutCancel(ctx), filesPrefix(sm.ID, next))
		return err
	}
	var prevDir string
	err = s.tx(ctx, func(tx pgx.Tx) error {
		// Lock the course, so a concurrent update or approval cannot switch
		// its content between reading the old files and replacing them.
		if err := tx.QueryRow(ctx, "SELECT files_dir FROM courses WHERE id = $1 FOR UPDATE", sm.ID).Scan(&prevDir); err != nil {
			return err
		}
		return s.replaceContent(ctx, tx, sm.ID, viewer, pp.manifest, prevDir, next, folderFiles(sm.ID, next))
	})
	if err != nil {
		_ = s.files.DeletePrefix(context.WithoutCancel(ctx), filesPrefix(sm.ID, next))
		return err
	}
	s.dropUnused(ctx, sm.ID, prevDir) // a freeze may still hold it
	return nil
}

// replaceContent is a course update (the course row already locked): the
// course switches from its files in prevDir to manifest m with files in
// next, lessons keep their progress by key, annotations are carried over,
// and a public course's subscribers are told what changed. The new texts
// are read through files: next, or where next's files were copied from.
func (s *Service) replaceContent(ctx context.Context, tx pgx.Tx, courseID int64, actor auth.User, m Manifest, prevDir, next string, files draftFiles) error {
	before, _, err := currentManifest(ctx, tx, courseID)
	if err != nil {
		return err
	}
	var visibility string // as of the lock, not as the caller last saw it
	if err := tx.QueryRow(ctx, "SELECT visibility FROM courses WHERE id = $1", courseID).Scan(&visibility); err != nil {
		return err
	}
	diff, err := s.diffContent(ctx, courseID, before, prevDir, m, files)
	if err != nil {
		return err
	}
	if err := switchContent(ctx, tx, courseID, m, next); err != nil {
		return err
	}
	if err := carryAnnotations(ctx, tx, courseID, diff); err != nil {
		return err
	}
	if visibility != Public {
		return nil // only the uploader reads a private course as it is
	}
	return notifyUpdated(ctx, tx, courseID, actor.ID, diff.summary())
}

// switchContent makes a course follow manifest m, with its files in the
// already stored folder dir.
func switchContent(ctx context.Context, tx pgx.Tx, courseID int64, m Manifest, dir string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE courses SET title = $2, type = $3, completion = $4, intro = $5, author_name = $6, author_intro = $7,
			cover = NULLIF($8, ''), files_dir = $9, updated_at = now()
		WHERE id = $1`, courseID, m.Title, m.Type, m.Completion, m.Intro, m.AuthorName, m.AuthorIntro, m.Cover, dir); err != nil {
		return err
	}
	return writeStructure(ctx, tx, courseID, m)
}

// dropFiles removes a folder of course files no longer in use; leftovers
// only cost space, so a failure is logged.
func (s *Service) dropFiles(ctx context.Context, courseID int64, dir string) {
	if _, err := s.db.Exec(context.WithoutCancel(ctx), "DELETE FROM passage_sets WHERE course_id = $1 AND files_dir = $2", courseID, dir); err != nil {
		slog.Warn("passages not removed", "course", courseID, "dir", dir, "err", err)
	}
	if err := s.files.DeletePrefix(context.WithoutCancel(ctx), filesPrefix(courseID, dir)); err != nil {
		slog.Warn("course files not removed", "course", courseID, "dir", dir, "err", err)
	}
}
