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
// The uploader updates a private course directly; admins update any course.
// The new files are stored beside the old ones and the course switches over
// in one transaction, so a failed update leaves the course as it was.
func (s *Service) Update(ctx context.Context, viewer auth.User, id int64, zr *zip.Reader) error {
	sm, err := s.loadToChange(ctx, viewer, id)
	if err == nil && !viewer.IsAdmin() {
		switch {
		case !sm.IsUploader:
			err = ErrNotFound
		case sm.Visibility != Private:
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
	err = s.tx(ctx, func(tx pgx.Tx) error { return switchContent(ctx, tx, sm.ID, pp.manifest, next) })
	if err != nil {
		_ = s.files.DeletePrefix(context.WithoutCancel(ctx), filesPrefix(sm.ID, next))
		return err
	}
	s.dropFiles(ctx, sm.ID, sm.FilesDir)
	return nil
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
	if err := s.files.DeletePrefix(context.WithoutCancel(ctx), filesPrefix(courseID, dir)); err != nil {
		slog.Warn("course files not removed", "course", courseID, "dir", dir, "err", err)
	}
}
