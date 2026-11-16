package course

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/storage"
)

var (
	// ErrDraftEmpty means the draft holds no operation to undo or commit.
	ErrDraftEmpty = errors.New("the course draft is empty")
)

// How a user's committed draft takes effect.
const (
	// EditDirect: the draft becomes a course update at once (the uploader
	// of a private course; an admin on a public one).
	EditDirect = "direct"
	// EditRequest: the draft becomes an update request (the uploader of a
	// public course; anyone while it is ongoing or incomplete).
	EditRequest = "request"
)

// Draft is a user's course draft as the editor shows it: the operations so
// far and the course they make of its latest version.
type Draft struct {
	Ops    []Op        `json:"ops"`
	Mode   string      `json:"mode"`
	Course DraftCourse `json:"course"`
}

type DraftCourse struct {
	Title       string         `json:"title"`
	Type        string         `json:"type"`
	Completion  string         `json:"completion"`
	Intro       string         `json:"intro"`
	AuthorName  string         `json:"authorName"`
	AuthorIntro string         `json:"authorIntro"`
	Chapters    []DraftChapter `json:"chapters"`
}

type DraftChapter struct {
	Key     string        `json:"key"`
	Title   string        `json:"title"`
	Added   bool          `json:"added,omitempty"` // new in this draft
	Lessons []DraftLesson `json:"lessons"`
}

type DraftLesson struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Video    string `json:"video"` // "file", "url" or ""
	HasText  bool   `json:"hasText"`
	Upcoming bool   `json:"upcoming,omitempty"`
	Added    bool   `json:"added,omitempty"` // new in this draft
	// Changed: the draft gives the lesson uploaded content, to preview.
	Changed bool `json:"changed,omitempty"`
}

// CommitResult is what committing a draft did. Outcome is "updated": the
// course now follows the draft; or "requested": the draft became update
// request RequestID.
type CommitResult struct {
	Outcome   string `json:"outcome"`
	RequestID int64  `json:"requestId,omitempty"`
}

// loadToEdit loads a course the viewer may change through the editor and
// says how their draft would take effect.
func (s *Service) loadToEdit(ctx context.Context, viewer auth.User, id int64) (Summary, string, error) {
	sm, err := s.loadToChange(ctx, viewer, id)
	if err != nil {
		return sm, "", err
	}
	switch {
	case sm.Visibility == Private && sm.IsUploader:
		return sm, EditDirect, nil
	case sm.Visibility == Private && !canView(viewer, sm):
		return sm, "", ErrNotFound // others' private courses stay invisible
	case sm.Visibility == Private:
		return sm, "", ErrForbidden // an admin reviewing it still may not change it
	case viewer.IsAdmin():
		return sm, EditDirect, nil
	case sm.IsUploader || sm.Completion != Finished:
		return sm, EditRequest, nil
	}
	return sm, "", ErrUpdateNotAllowed
}

// querier is a database connection or transaction.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// currentManifest reads a course as it is now, as the manifest that would
// describe it, with the folder its files are in.
func currentManifest(ctx context.Context, q querier, courseID int64) (Manifest, string, error) {
	var m Manifest
	var dir string
	if err := q.QueryRow(ctx, `
		SELECT title, type, completion, intro, author_name, author_intro, coalesce(cover, ''), files_dir
		FROM courses WHERE id = $1`, courseID).Scan(&m.Title, &m.Type, &m.Completion, &m.Intro, &m.AuthorName, &m.AuthorIntro, &m.Cover, &dir); err != nil {
		return m, "", err
	}
	rows, err := q.Query(ctx, `
		SELECT ch.key, ch.title, l.key, l.title, coalesce(l.file, ''), coalesce(l.video_file, ''), coalesce(l.video_url, ''), coalesce(l.duration, '')
		FROM chapters ch LEFT JOIN lessons l ON l.chapter_id = ch.id
		WHERE ch.course_id = $1 ORDER BY ch.position, l.position`, courseID)
	if err != nil {
		return m, "", err
	}
	var chKey, chTitle string
	var key, title *string
	var l Lesson
	_, err = pgx.ForEachRow(rows, []any{&chKey, &chTitle, &key, &title, &l.File, &l.VideoFile, &l.VideoURL, &l.Duration}, func() error {
		if n := len(m.Chapters); n == 0 || m.Chapters[n-1].Key != chKey {
			m.Chapters = append(m.Chapters, Chapter{Key: chKey, Title: chTitle})
		}
		if key != nil {
			l.Key, l.Title = *key, *title
			last := &m.Chapters[len(m.Chapters)-1]
			last.Lessons = append(last.Lessons, l)
		}
		return nil
	})
	return m, dir, err
}

// draftOps reads the viewer's draft operations for a course; none if they
// have no draft.
func draftOps(ctx context.Context, q querier, courseID, userID int64, lock bool) ([]Op, error) {
	d, err := loadDraft(ctx, q, courseID, userID, lock)
	return d.Ops, err
}

// GetDraft returns the viewer's draft for a course; an empty one if they
// have not changed anything yet.
func (s *Service) GetDraft(ctx context.Context, viewer auth.User, id int64) (Draft, error) {
	_, mode, err := s.loadToEdit(ctx, viewer, id)
	if err != nil {
		return Draft{}, err
	}
	ops, err := draftOps(ctx, s.db, id, viewer.ID, false)
	if err != nil {
		return Draft{}, err
	}
	return s.draftView(ctx, s.db, id, mode, ops)
}

// draftView applies ops to the course's latest version for the editor.
func (s *Service) draftView(ctx context.Context, q querier, id int64, mode string, ops []Op) (Draft, error) {
	base, _, err := currentManifest(ctx, q, id)
	if err != nil {
		return Draft{}, err
	}
	m, err := applyOps(base, ops)
	if err != nil {
		return Draft{}, err
	}
	return Draft{Ops: ops, Mode: mode, Course: viewDraftCourse(base, m, ops)}, nil
}

func viewDraftCourse(base, m Manifest, ops []Op) DraftCourse {
	changed := map[string]bool{}
	for _, op := range ops {
		if op.Content != nil {
			changed[op.Lesson] = true
		}
	}
	had := map[string]bool{}
	for _, ch := range base.Chapters {
		had[ch.Key] = true
		for _, l := range ch.Lessons {
			had[l.Key] = true
		}
	}
	c := DraftCourse{Title: m.Title, Type: m.Type, Completion: m.Completion, Intro: m.Intro,
		AuthorName: m.AuthorName, AuthorIntro: m.AuthorIntro, Chapters: []DraftChapter{}}
	for _, ch := range m.Chapters {
		dc := DraftChapter{Key: ch.Key, Title: ch.Title, Added: !had[ch.Key], Lessons: []DraftLesson{}}
		for _, l := range ch.Lessons {
			video := ""
			if l.VideoFile != "" {
				video = "file"
			} else if l.VideoURL != "" {
				video = "url"
			}
			dc.Lessons = append(dc.Lessons, DraftLesson{Key: l.Key, Title: l.Title, Video: video, HasText: l.File != "",
				Upcoming: l.Upcoming(), Added: !had[l.Key], Changed: changed[l.Key]})
		}
		c.Chapters = append(c.Chapters, dc)
	}
	return c
}

// AddDraftOp appends one operation to the viewer's draft, starting the
// draft if they have none. An operation that cannot apply to the course as
// the draft has it is *OpRejected and is not recorded.
func (s *Service) AddDraftOp(ctx context.Context, viewer auth.User, id int64, op Op) (Draft, error) {
	sm, mode, err := s.loadToEdit(ctx, viewer, id)
	if err != nil {
		return Draft{}, err
	}
	if op, err = prepareOp(op); err != nil {
		return Draft{}, err
	}
	if op.Content != nil {
		row, err := loadDraft(ctx, s.db, id, viewer.ID, false)
		if err != nil {
			return Draft{}, err
		}
		c := *op.Content
		if err := s.prepareContent(ctx, id, row, &c); err != nil {
			return Draft{}, err
		}
		op.Content = &c
	}
	var d Draft
	err = s.tx(ctx, func(tx pgx.Tx) error {
		ops, err := draftOps(ctx, tx, id, viewer.ID, true)
		if err != nil {
			return err
		}
		ops = append(ops, op)
		if d, err = s.draftView(ctx, tx, id, mode, ops); err != nil {
			return err
		}
		raw, err := json.Marshal([]Op{op})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO course_drafts (course_id, user_id, base_dir, ops) VALUES ($1, $2, $3, $4)
			ON CONFLICT (course_id, user_id) DO UPDATE SET ops = course_drafts.ops || EXCLUDED.ops, updated_at = now()`,
			id, viewer.ID, sm.FilesDir, raw)
		return err
	})
	return d, err
}

// UndoDraftOp drops the last operation of the viewer's draft; undoing the
// only one leaves no draft at all.
func (s *Service) UndoDraftOp(ctx context.Context, viewer auth.User, id int64) (Draft, error) {
	_, mode, err := s.loadToEdit(ctx, viewer, id)
	if err != nil {
		return Draft{}, err
	}
	var d Draft
	var row draftRow
	var unused string
	err = s.tx(ctx, func(tx pgx.Tx) error {
		if row, err = loadDraft(ctx, tx, id, viewer.ID, true); err != nil {
			return err
		}
		ops := row.Ops
		if len(ops) == 0 {
			return ErrDraftEmpty
		}
		ops = ops[:len(ops)-1]
		unused = unusedUpload(row.Ops[len(ops)], ops)
		if len(ops) == 0 {
			_, err = tx.Exec(ctx, "DELETE FROM course_drafts WHERE course_id = $1 AND user_id = $2", id, viewer.ID)
		} else {
			_, err = tx.Exec(ctx, "UPDATE course_drafts SET ops = ops - -1, updated_at = now() WHERE course_id = $1 AND user_id = $2", id, viewer.ID)
		}
		if err != nil {
			return err
		}
		d, err = s.draftView(ctx, tx, id, mode, ops)
		return err
	})
	switch {
	case err != nil:
	case len(d.Ops) == 0:
		s.dropDraftFiles(ctx, id, row.ID, "") // the draft is gone
	case unused != "":
		s.dropDraftFiles(ctx, id, row.ID, unused)
	}
	return d, err
}

// DiscardDraft throws the viewer's draft away with the files uploaded to
// it; having none is fine.
func (s *Service) DiscardDraft(ctx context.Context, viewer auth.User, id int64) error {
	if _, _, err := s.loadToEdit(ctx, viewer, id); err != nil {
		return err
	}
	var draftID int64
	err := s.db.QueryRow(ctx, "DELETE FROM course_drafts WHERE course_id = $1 AND user_id = $2 RETURNING id", id, viewer.ID).Scan(&draftID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err == nil {
		s.dropDraftFiles(ctx, id, draftID, "")
	}
	return err
}

// CommitDraft makes the viewer's draft take effect. Its operations are
// applied in order to the course's latest version, and the result becomes
// a course update like a whole-package one: progress stays with lesson
// keys, annotations are carried over and subscribers are told. The new
// version's files are copied from the current one (ADR-0005); nothing is
// written anew. The draft is gone afterwards.
func (s *Service) CommitDraft(ctx context.Context, viewer auth.User, id int64) (CommitResult, error) {
	sm, mode, err := s.loadToEdit(ctx, viewer, id)
	if err != nil {
		return CommitResult{}, err
	}
	if mode == EditRequest {
		return s.requestDraft(ctx, viewer, id)
	}
	next := newFilesDir()
	var prevDir string
	var row draftRow
	err = s.tx(ctx, func(tx pgx.Tx) error {
		// Lock the course first, so nothing else switches its content while
		// the draft is applied to it.
		if err := tx.QueryRow(ctx, "SELECT files_dir FROM courses WHERE id = $1 FOR UPDATE", id).Scan(&prevDir); err != nil {
			return err
		}
		var err error
		if row, err = loadDraft(ctx, tx, id, viewer.ID, true); err != nil {
			return err
		}
		if len(row.Ops) == 0 {
			return ErrDraftEmpty
		}
		cur, _, err := currentManifest(ctx, tx, id)
		if err != nil {
			return err
		}
		m, err := applyOps(cur, row.Ops)
		if err != nil {
			return err
		}
		from := draftFiles{courseID: id, dir: prevDir, draftID: row.ID, uploads: row.uploads()}
		if err := s.copyFiles(ctx, from, next, m); err != nil {
			return err
		}
		if err := s.replaceContent(ctx, tx, sm, viewer, m, prevDir, next); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "DELETE FROM course_drafts WHERE course_id = $1 AND user_id = $2", id, viewer.ID)
		return err
	})
	if err != nil {
		s.dropFiles(ctx, id, next)
		return CommitResult{}, err
	}
	s.dropUnused(ctx, id, prevDir) // a freeze may still hold it
	s.dropDraftFiles(ctx, id, row.ID, "")
	return CommitResult{Outcome: "updated"}, nil
}

// requestDraft turns the viewer's draft into an update request: the course
// the draft makes of the latest version, with its files copied into the
// request's own folder (ADR-0005), so approving it works like approving an
// uploaded package. The course itself does not change.
func (s *Service) requestDraft(ctx context.Context, viewer auth.User, id int64) (CommitResult, error) {
	dir := newRequestDir()
	var filed filedRequest
	err := s.tx(ctx, func(tx pgx.Tx) error {
		// Hold the course's content still while its files are copied: an
		// approval switching it would drop the folder they come from.
		var base string
		if err := tx.QueryRow(ctx, "SELECT files_dir FROM courses WHERE id = $1 FOR SHARE", id).Scan(&base); err != nil {
			return err
		}
		ops, err := draftOps(ctx, tx, id, viewer.ID, true)
		if err != nil {
			return err
		}
		if len(ops) == 0 {
			return ErrDraftEmpty
		}
		cur, _, err := currentManifest(ctx, tx, id)
		if err != nil {
			return err
		}
		m, err := applyOps(cur, ops)
		if err != nil {
			return err
		}
		raw, err := m.encode()
		if err != nil {
			return err
		}
		if err := s.copyFiles(ctx, id, base, dir, m); err != nil {
			return err
		}
		if filed, err = fileUpdateRequest(ctx, tx, id, viewer, dir, base, raw); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "DELETE FROM course_drafts WHERE course_id = $1 AND user_id = $2", id, viewer.ID)
		return err
	})
	if err != nil {
		s.dropFiles(ctx, id, dir)
		return CommitResult{}, err
	}
	s.dropReplaced(ctx, id, filed)
	return CommitResult{Outcome: "requested", RequestID: filed.id}, nil
}

// copyFiles fills folder to with the files course m needs, copied from
// where they are now (ADR-0005): the course's current folder, or the
// draft's temporary area for uploaded ones. Those are lesson texts, the
// images they reference, videos and the cover.
func (s *Service) copyFiles(ctx context.Context, from draftFiles, to string, m Manifest) error {
	need, err := s.neededFiles(ctx, from, m)
	if err != nil {
		return err
	}
	courseID := from.courseID
	for _, name := range need {
		err := s.files.Copy(ctx, from.key(name), filesPrefix(courseID, to)+name)
		if errors.Is(err, storage.ErrNotFound) {
			// missing where it is now already; the new version cannot do better
			slog.Warn("course file missing", "course", courseID, "file", from.key(name))
			continue
		}
		if err != nil {
			return fmt.Errorf("copy %s: %w", name, err)
		}
	}
	return nil
}

// neededFiles lists the files that course m uses.
func (s *Service) neededFiles(ctx context.Context, from draftFiles, m Manifest) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	add(m.Cover)
	for _, ch := range m.Chapters {
		for _, l := range ch.Lessons {
			add(l.VideoFile)
			if l.File == "" {
				continue
			}
			add(l.File)
			text, err := s.readFile(ctx, from.key(l.File))
			if errors.Is(err, storage.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, img := range localImages(string(text)) {
				if target, ok := resolveRef(l.File, img); ok && path.Clean(target) == target {
					add(target)
				}
			}
		}
	}
	return out, nil
}
