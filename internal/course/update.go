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
	var sm Summary
	var err error
	if viewer.IsAdmin() {
		sm, err = s.load(ctx, viewer, id, "")
	} else {
		sm, err = s.loadVisible(ctx, viewer, id, "")
		if err == nil && !sm.IsUploader {
			err = ErrNotFound
		}
		if err == nil && sm.Visibility != Private {
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
	return s.replaceContent(ctx, sm, pp)
}

func (s *Service) replaceContent(ctx context.Context, sm Summary, pp parsedPackage) error {
	m := pp.manifest
	next := sm.FilesVersion + 1
	if err := s.storeFiles(ctx, pp, filesPrefix(sm.ID, next)); err != nil {
		_ = s.files.DeletePrefix(context.WithoutCancel(ctx), filesPrefix(sm.ID, next))
		return err
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE courses SET title = $2, type = $3, completion = $4, intro = $5, author_name = $6, author_intro = $7,
				cover = NULLIF($8, ''), files_version = $9, updated_at = now()
			WHERE id = $1`, sm.ID, m.Title, m.Type, m.Completion, m.Intro, m.AuthorName, m.AuthorIntro, m.Cover, next); err != nil {
			return err
		}
		return writeStructure(ctx, tx, sm.ID, m)
	})
	if err != nil {
		_ = s.files.DeletePrefix(context.WithoutCancel(ctx), filesPrefix(sm.ID, next))
		return err
	}
	if err := s.files.DeletePrefix(ctx, filesPrefix(sm.ID, sm.FilesVersion)); err != nil {
		slog.Warn("old course files not removed", "course", sm.ID, "version", sm.FilesVersion, "err", err)
	}
	return nil
}
