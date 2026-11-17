package course

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/notify"
)

// ErrUpdateNotAllowed means only the uploader may update a finished course.
var ErrUpdateNotAllowed = errors.New("only the uploader may update a finished course")

// SubmitUpdate asks for a public course's content to be replaced by a new
// course package. The uploader may always ask; anyone may while the course
// is ongoing or incomplete. The package is checked now and stored in its own
// folder; the course does not change until an admin approves.
func (s *Service) SubmitUpdate(ctx context.Context, viewer auth.User, id int64, zr *zip.Reader) (int64, error) {
	sm, err := s.loadVisible(ctx, viewer, id, "")
	if err != nil {
		return 0, err
	}
	if sm.Visibility != Public {
		return 0, ErrInvalidState // a private course is updated directly
	}
	if !sm.IsUploader && sm.Completion == Finished {
		return 0, ErrUpdateNotAllowed
	}
	pp, err := parsePackage(zr)
	if err != nil {
		return 0, err
	}
	dir := newRequestDir()
	if err := s.storeFiles(ctx, pp, filesPrefix(id, dir)); err != nil {
		s.dropFiles(ctx, id, dir)
		return 0, err
	}
	var f filedRequest
	err = s.tx(ctx, func(tx pgx.Tx) error {
		var err error
		f, err = fileUpdateRequest(ctx, tx, id, viewer, dir, sm.FilesDir, pp.raw, nil)
		return err
	})
	if err != nil {
		s.dropFiles(ctx, id, dir)
		return 0, err
	}
	s.dropReplaced(ctx, id, f)
	return f.id, nil
}

// newRequestDir names a fresh folder for an update request's files.
func newRequestDir() string { return fmt.Sprintf("r%d", time.Now().UnixNano()) }

// filedRequest is an update request just recorded, with the folders of the
// requester's earlier pending requests it replaced, to remove once the
// transaction commits.
type filedRequest struct {
	id       int64
	replaced []string
}

// fileUpdateRequest records an update request whose course (manifest) and
// files (in folder dir) are ready, made against the course version in
// folder base, and tells the admins. A request made from a draft also has
// the draft's operations (JSON), to replay on the latest version; nil for
// a whole package. The requester's earlier pending
// request for the course is withdrawn: admins only review the latest one.
func fileUpdateRequest(ctx context.Context, tx pgx.Tx, courseID int64, requester auth.User, dir, base string, manifest, ops []byte) (filedRequest, error) {
	var f filedRequest
	rows, err := tx.Query(ctx, `
		UPDATE review_requests SET state = 'withdrawn', decided_at = now()
		WHERE course_id = $1 AND requester_id = $2 AND kind = 'update' AND state = 'pending'
		RETURNING coalesce(files_dir, '')`, courseID, requester.ID)
	if err != nil {
		return f, err
	}
	if f.replaced, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return f, err
	}
	if len(f.replaced) > 0 {
		if err := closeRequestNotices(ctx, tx, courseID); err != nil {
			return f, err
		}
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO review_requests (kind, course_id, requester_id, files_dir, base_dir, manifest, ops)
		VALUES ('update', $1, $2, $3, $4, $5, $6) RETURNING id`, courseID, requester.ID, dir, base, manifest, ops).Scan(&f.id); err != nil {
		return f, err
	}
	return f, tellAdmins(ctx, tx, notify.UpdateRequested, courseID, f.id, requester)
}

// dropReplaced removes the folders of requests a new one replaced.
func (s *Service) dropReplaced(ctx context.Context, courseID int64, f filedRequest) {
	for _, dir := range f.replaced {
		if dir != "" {
			s.dropFiles(ctx, courseID, dir)
		}
	}
}

// UpdateRequest is one of the viewer's update requests for a course.
type UpdateRequest struct {
	ID        int64      `json:"id"`
	State     string     `json:"state"`
	Reason    string     `json:"reason"`
	CreatedAt time.Time  `json:"createdAt"`
	DecidedAt *time.Time `json:"decidedAt"`
}

// MyUpdateRequests lists the viewer's update requests for a course, newest
// first.
func (s *Service) MyUpdateRequests(ctx context.Context, viewer auth.User, id int64) ([]UpdateRequest, error) {
	if _, err := s.loadVisible(ctx, viewer, id, ""); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, state, reason, created_at, decided_at FROM review_requests
		WHERE kind = 'update' AND course_id = $1 AND requester_id = $2
		ORDER BY created_at DESC, id DESC`, id, viewer.ID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[UpdateRequest])
}

// WithdrawUpdate takes back the viewer's pending update request.
func (s *Service) WithdrawUpdate(ctx context.Context, viewer auth.User, courseID, reqID int64) error {
	var dir string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			UPDATE review_requests SET state = 'withdrawn', decided_at = now()
			WHERE id = $1 AND course_id = $2 AND requester_id = $3 AND kind = 'update' AND state = 'pending'
			RETURNING coalesce(files_dir, '')`, reqID, courseID, viewer.ID).Scan(&dir)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrReviewNotFound
		}
		if err != nil {
			return err
		}
		return closeRequestNotices(ctx, tx, courseID)
	})
	if err == nil && dir != "" {
		s.dropFiles(ctx, courseID, dir)
	}
	return err
}

// updateRequest is a pending update request as stored.
type updateRequest struct {
	courseID int64
	dir      string // the folder of its files
	base     string // the course's folder when it was filed
	manifest []byte // the course it makes of that version
	ops      []Op   // a draft request's operations; nil for a whole package
}

func (r *updateRequest) scanOps(raw []byte) error {
	if raw == nil {
		return nil
	}
	r.ops = []Op{}
	if err := json.Unmarshal(raw, &r.ops); err != nil {
		return err
	}
	nameOps(r.ops)
	return nil
}

// proposal is what an update request makes of a course's latest version.
type proposal struct {
	m     Manifest
	files draftFiles // where m's files are
	// conflicts are a draft request's operations that no longer apply.
	conflicts []Conflict
	// inPlace: the request's folder holds just the files m uses, so the
	// course can switch to it; otherwise they must be copied into a new
	// folder first.
	inPlace bool
}

// propose applies update request r to the course's latest version cur,
// with its files in curDir. A whole package replaces the course as it is;
// a draft request replays its operations on cur (ADR-0006), with the
// files of the lessons it gave content in the request's folder and the
// others where the course has them now.
func (r updateRequest) propose(cur Manifest, curDir string) (proposal, error) {
	snapshot, problems := ParseManifest(r.manifest)
	if r.ops == nil {
		if len(problems) > 0 {
			return proposal{}, &InvalidPackage{problems}
		}
		return proposal{m: snapshot, files: folderFiles(r.courseID, r.dir), inPlace: true}, nil
	}
	m, conflicts := replayOps(cur, r.ops)
	return proposal{
		m:         m,
		files:     draftFiles{courseID: r.courseID, dir: curDir, area: filesPrefix(r.courseID, r.dir), uploads: opUploads(r.ops)},
		conflicts: conflicts,
		// filed against this very version, it makes what was filed
		inPlace: r.base == curDir && len(problems) == 0 && sameFiles(m, snapshot),
	}, nil
}

// RequestConflicts means an update request made from a draft no longer
// applies to the course's latest version as a whole: someone else's update
// came first. Approving it would drop these of its operations unseen by
// the requester, so it can only be turned down.
type RequestConflicts struct{ Conflicts []Conflict }

func (e *RequestConflicts) Error() string {
	return fmt.Sprintf("%d operations of the request no longer apply", len(e.Conflicts))
}
