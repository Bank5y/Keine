package course

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"path"
	"slices"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/storage"
)

var (
	// ErrDraftEmpty means the draft holds no operation to undo or commit.
	ErrDraftEmpty = errors.New("the course draft is empty")
	// ErrNothingApplies means every operation of the draft conflicts with
	// the course's latest version, so committing it would change nothing.
	ErrNothingApplies = errors.New("no operation of the draft applies to the course")
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
	// Conflicts are the operations that no longer apply to the course's
	// latest version; Course is made without them.
	Conflicts []Conflict `json:"conflicts"`
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
// course now follows the draft; "requested": the draft became update
// request RequestID; or "conflicts": some operations no longer apply to the
// course's latest version and were not confirmed to be skipped, so nothing
// changed and Conflicts lists them all.
type CommitResult struct {
	Outcome   string     `json:"outcome"`
	RequestID int64      `json:"requestId,omitempty"`
	Conflicts []Conflict `json:"conflicts,omitempty"`
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
	m, conflicts := replayOps(base, ops)
	return newDraft(base, m, ops, mode, conflicts), nil
}

func newDraft(base, m Manifest, ops []Op, mode string, conflicts []Conflict) Draft {
	if conflicts == nil {
		conflicts = []Conflict{}
	}
	return Draft{Ops: ops, Mode: mode, Course: viewDraftCourse(base, m, ops), Conflicts: conflicts}
}

func viewDraftCourse(base, m Manifest, ops []Op) DraftCourse {
	changed := map[string]bool{}
	for _, op := range ops {
		if op.Content != nil {
			changed[op.Lesson] = true
		}
	}
	hadChapter, hadLesson := map[string]bool{}, map[string]bool{} // apart, as keys may coincide
	for _, ch := range base.Chapters {
		hadChapter[ch.Key] = true
		for _, l := range ch.Lessons {
			hadLesson[l.Key] = true
		}
	}
	c := DraftCourse{Title: m.Title, Type: m.Type, Completion: m.Completion, Intro: m.Intro,
		AuthorName: m.AuthorName, AuthorIntro: m.AuthorIntro, Chapters: []DraftChapter{}}
	for _, ch := range m.Chapters {
		dc := DraftChapter{Key: ch.Key, Title: ch.Title, Added: !hadChapter[ch.Key], Lessons: []DraftLesson{}}
		for _, l := range ch.Lessons {
			video := ""
			if l.VideoFile != "" {
				video = "file"
			} else if l.VideoURL != "" {
				video = "url"
			}
			dc.Lessons = append(dc.Lessons, DraftLesson{Key: l.Key, Title: l.Title, Video: video, HasText: l.File != "",
				Upcoming: l.Upcoming(), Added: !hadLesson[l.Key], Changed: changed[l.Key]})
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
		base, _, err := currentManifest(ctx, tx, id)
		if err != nil {
			return err
		}
		// The new operation must apply to the course as the draft has it;
		// earlier ones that no longer do stay listed as conflicts.
		m, conflicts := replayOps(base, ops)
		op.Target = m.target(op)
		op.From = m.from(op)
		if err := m.apply(op); err != nil {
			return err
		}
		ops = append(ops, op)
		d = newDraft(base, m, ops, mode, conflicts)
		raw, err := json.Marshal([]Op{op})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO course_drafts (course_id, user_id, base_dir, ops, updated_at) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (course_id, user_id) DO UPDATE SET ops = course_drafts.ops || EXCLUDED.ops, updated_at = EXCLUDED.updated_at`,
			id, viewer.ID, sm.FilesDir, raw, s.now())
		return err
	})
	return d, err
}

// UndoDraftOp drops the last operation of the viewer's draft, with its
// upload once nothing else uses it. Undoing the only one leaves the draft
// empty, but keeps it: uploads started for an operation not made yet stay
// usable.
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
		if _, err = tx.Exec(ctx, "UPDATE course_drafts SET ops = ops - -1, updated_at = $2 WHERE id = $1", row.ID, s.now()); err != nil {
			return err
		}
		d, err = s.draftView(ctx, tx, id, mode, ops)
		return err
	})
	if err == nil && unused != "" {
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
//
// Operations that no longer apply to the latest version are conflicts
// (ADR-0006): unless skip names every one of them by operation ID, which
// is the user confirming they be skipped, nothing changes and the result
// lists them.
//
// The files are copied with no lock held, as copying many large videos
// takes long; the course and the draft are checked again under the lock,
// and the commit starts over if either changed meanwhile.
func (s *Service) CommitDraft(ctx context.Context, viewer auth.User, id int64, skip []string) (CommitResult, error) {
	for attempt := 1; ; attempt++ {
		res, err := s.commitDraft(ctx, viewer, id, skip)
		if !errors.Is(err, ErrCourseBusy) || attempt == busyAttempts {
			return res, err
		}
	}
}

// ErrCourseBusy means the course kept changing while a change to it was
// being prepared; trying again later should work.
var ErrCourseBusy = errors.New("the course changed while the change was prepared")

// busyAttempts is how many times a change prepared without a lock is
// prepared again when the course changed meanwhile.
const busyAttempts = 3

func (s *Service) commitDraft(ctx context.Context, viewer auth.User, id int64, skip []string) (CommitResult, error) {
	sm, mode, err := s.loadToEdit(ctx, viewer, id)
	if err != nil {
		return CommitResult{}, err
	}
	seen, err := planDraft(ctx, s.db, id, viewer.ID, "")
	if err != nil {
		return CommitResult{}, err
	}
	if len(seen.row.Ops) == 0 {
		return CommitResult{}, ErrDraftEmpty
	}
	if len(unconfirmed(seen.conflicts, skip)) > 0 {
		// the course and the draft stay as they are
		return CommitResult{Outcome: "conflicts", Conflicts: seen.conflicts}, nil
	}
	if len(seen.conflicts) == len(seen.row.Ops) {
		return CommitResult{}, ErrNothingApplies
	}
	dir := newFilesDir()
	lock := " FOR UPDATE" // nothing else switches the content meanwhile
	if mode == EditRequest {
		// the request gets its own folder (ADR-0005), so approving it works
		// like approving an uploaded package; the course does not change,
		// but must not switch away from the folder the files came from
		dir, lock = newRequestDir(), " FOR SHARE"
	}
	from := ofDraft(id, seen.dir, seen.row)
	if err := s.copyFiles(ctx, from, dir, seen.m); err != nil {
		s.dropFiles(ctx, id, dir)
		return CommitResult{}, err
	}
	var filed filedRequest
	err = s.tx(ctx, func(tx pgx.Tx) error {
		now, err := planDraft(ctx, tx, id, viewer.ID, lock)
		if err != nil {
			return err
		}
		// mode was decided from the course as loadToEdit read it; under the
		// lock it must still be the course that decided it
		if !now.same(seen) || now.visibility != sm.Visibility || now.completion != sm.Completion {
			return ErrCourseBusy
		}
		if mode == EditDirect {
			if err := s.replaceContent(ctx, tx, id, viewer, now.m, now.dir, dir, from); err != nil {
				return err
			}
		} else {
			raw, err := now.m.encode()
			if err != nil {
				return err
			}
			// the operations that applied, to replay on whatever version
			// is the latest when the request is approved
			ops, err := json.Marshal(applied(now.row.Ops, now.conflicts))
			if err != nil {
				return err
			}
			if filed, err = fileUpdateRequest(ctx, tx, id, viewer, dir, now.dir, raw, ops); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, "DELETE FROM course_drafts WHERE id = $1", now.row.ID)
		return err
	})
	if err != nil {
		s.dropFiles(ctx, id, dir)
		return CommitResult{}, err
	}
	s.dropDraftFiles(ctx, id, seen.row.ID, "")
	if mode == EditRequest {
		s.dropReplaced(ctx, id, filed)
		return CommitResult{Outcome: "requested", RequestID: filed.id}, nil
	}
	s.dropUnused(ctx, id, seen.dir) // a freeze may still hold it
	return CommitResult{Outcome: "updated"}, nil
}

// draftPlan is a user's draft applied to a course's latest version.
type draftPlan struct {
	dir, visibility, completion string // the course's
	row                         draftRow
	m                           Manifest
	conflicts                   []Conflict
}

// planDraft reads a draft and the course it applies to, locking the course
// row with lock (" FOR UPDATE", " FOR SHARE" or "") and the draft with it.
func planDraft(ctx context.Context, q querier, courseID, userID int64, lock string) (draftPlan, error) {
	var p draftPlan
	if err := q.QueryRow(ctx, "SELECT files_dir, visibility, completion FROM courses WHERE id = $1"+lock, courseID).
		Scan(&p.dir, &p.visibility, &p.completion); err != nil {
		return p, err
	}
	var err error
	if p.row, err = loadDraft(ctx, q, courseID, userID, lock != ""); err != nil {
		return p, err
	}
	cur, _, err := currentManifest(ctx, q, courseID)
	if err != nil {
		return p, err
	}
	p.m, p.conflicts = replayOps(cur, p.row.Ops)
	return p, nil
}

// same tells whether a plan made later is still the one files were copied
// for: the course's content and the draft did not change, and neither did
// what decides how the draft takes effect. The course's information may
// have; the later plan has it right.
func (p draftPlan) same(earlier draftPlan) bool {
	ops, _ := json.Marshal(p.row.Ops)
	before, _ := json.Marshal(earlier.row.Ops)
	return p.dir == earlier.dir && p.visibility == earlier.visibility && p.completion == earlier.completion &&
		p.row.ID == earlier.row.ID && bytes.Equal(ops, before) && sameFiles(p.m, earlier.m) &&
		slices.EqualFunc(p.conflicts, earlier.conflicts, func(a, b Conflict) bool { return a.ID == b.ID })
}

// sameFiles tells whether two versions of a course use the same files:
// the same cover and every lesson the same text and video file.
func sameFiles(a, b Manifest) bool {
	files := func(m Manifest) map[string][2]string {
		out := map[string][2]string{"": {m.Cover}} // lesson keys are never empty
		for _, ch := range m.Chapters {
			for _, l := range ch.Lessons {
				out[l.Key] = [2]string{l.File, l.VideoFile}
			}
		}
		return out
	}
	return maps.Equal(files(a), files(b))
}

// copyConcurrency is how many files are read or copied at once.
const copyConcurrency = 8

// copyFiles fills folder to with the files course m needs, copied from
// where they are now (ADR-0005): a folder of the course, or an upload area
// for uploaded ones. Those are lesson texts, the images they reference,
// videos and the cover. A file already missing is left out: the new
// version cannot do better.
func (s *Service) copyFiles(ctx context.Context, from draftFiles, to string, m Manifest) error {
	need, err := s.neededFiles(ctx, from, m)
	if err != nil {
		return err
	}
	courseID := from.courseID
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(copyConcurrency)
	for _, name := range need {
		g.Go(func() error {
			err := s.files.Copy(gctx, from.key(name), filesPrefix(courseID, to)+name)
			if errors.Is(err, storage.ErrNotFound) {
				slog.Warn("course file missing", "course", courseID, "file", from.key(name))
				return nil
			}
			if err != nil {
				return fmt.Errorf("copy %s: %w", name, err)
			}
			return nil
		})
	}
	return g.Wait()
}

// neededFiles lists the files that course m uses.
func (s *Service) neededFiles(ctx context.Context, from draftFiles, m Manifest) ([]string, error) {
	var texts []string
	for _, ch := range m.Chapters {
		for _, l := range ch.Lessons {
			if l.File != "" {
				texts = append(texts, l.File)
			}
		}
	}
	images := make([][]string, len(texts))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(copyConcurrency)
	for i, file := range texts {
		g.Go(func() error {
			text, err := s.readFile(gctx, from.key(file))
			if errors.Is(err, storage.ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			for _, img := range localImages(string(text)) {
				if target, ok := resolveRef(file, img); ok && path.Clean(target) == target {
					images[i] = append(images[i], target)
				}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	add(m.Cover)
	i := 0
	for _, ch := range m.Chapters {
		for _, l := range ch.Lessons {
			add(l.VideoFile)
			if l.File == "" {
				continue
			}
			add(l.File)
			for _, img := range images[i] {
				add(img)
			}
			i++
		}
	}
	return out, nil
}
