package course

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/auth"
)

// ErrReportNotFound: the exploration has no report of that version.
var ErrReportNotFound = errors.New("report not found")

// Reference is an item of a report's references: what a turn of the
// dialogue actually looked up, never what the model wrote (ADR 0012).
type Reference struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// ReportVersion is a version of an exploration's report as a list shows it.
type ReportVersion struct {
	ID        int64     `json:"id"`
	Version   int       `json:"version"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
}

// Report is a version of an exploration report whole (ADR 0012): its
// parts, its references, and the turns it took in.
type Report struct {
	ReportVersion
	Summary    string      `json:"summary"`
	Body       string      `json:"body"`
	Points     string      `json:"points"`
	References []Reference `json:"references"`
	TurnIDs    []int64     `json:"turnIds"`
}

// NewReport is a report to keep, with what its run records.
type NewReport struct {
	Title, Summary, Body, Points string
	References                   []Reference
	TurnIDs                      []int64
	Meta                         any
}

// AddReport keeps a report as the exploration's next version.
func (s *Service) AddReport(ctx context.Context, viewer auth.User, explorationID int64, in NewReport) (Report, error) {
	refs := in.References
	if refs == nil {
		refs = []Reference{}
	}
	rawRefs, err := json.Marshal(refs)
	if err != nil {
		return Report{}, err
	}
	meta, err := json.Marshal(in.Meta)
	if err != nil {
		return Report{}, err
	}
	turns := in.TurnIDs
	if turns == nil {
		turns = []int64{}
	}
	r := Report{ReportVersion: ReportVersion{Title: in.Title}, Summary: in.Summary, Body: in.Body, Points: in.Points, References: refs, TurnIDs: turns}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		var id int64
		err := tx.QueryRow(ctx, "SELECT id FROM explorations WHERE id = $1 AND user_id = $2 FOR UPDATE", explorationID, viewer.ID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrExplorationNotFound
		}
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `INSERT INTO exploration_reports (exploration_id, version, title, summary, body, points, refs, turn_ids, meta)
			VALUES ($1, (SELECT coalesce(max(version), 0) + 1 FROM exploration_reports WHERE exploration_id = $1), $2, $3, $4, $5, $6, $7, $8)
			RETURNING id, version, created_at`, explorationID, in.Title, in.Summary, in.Body, in.Points, rawRefs, turns, meta).
			Scan(&r.ID, &r.Version, &r.CreatedAt)
	})
	return r, err
}

// Reports lists the versions of an exploration's report, newest first.
func (s *Service) Reports(ctx context.Context, viewer auth.User, explorationID int64) ([]ReportVersion, error) {
	if err := s.ownExploration(ctx, viewer, explorationID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT id, version, title, created_at FROM exploration_reports
		WHERE exploration_id = $1 ORDER BY version DESC`, explorationID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ReportVersion])
}

// Report reads a version of an exploration's report.
func (s *Service) Report(ctx context.Context, viewer auth.User, explorationID int64, version int) (Report, error) {
	if err := s.ownExploration(ctx, viewer, explorationID); err != nil {
		return Report{}, err
	}
	var r Report
	var refs []byte
	err := s.db.QueryRow(ctx, `SELECT id, version, title, created_at, summary, body, points, refs, turn_ids FROM exploration_reports
		WHERE exploration_id = $1 AND version = $2`, explorationID, version).
		Scan(&r.ID, &r.Version, &r.Title, &r.CreatedAt, &r.Summary, &r.Body, &r.Points, &refs, &r.TurnIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrReportNotFound
	}
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(refs, &r.References)
}

func (s *Service) ownExploration(ctx context.Context, viewer auth.User, id int64) error {
	var ok bool
	if err := s.db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM explorations WHERE id = $1 AND user_id = $2)", id, viewer.ID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrExplorationNotFound
	}
	return nil
}

// Markdown is the report as a Markdown file: the title, the summary, the
// body, the points and the references.
func (r Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n## 摘要\n\n%s\n\n%s\n\n## 要点\n\n%s\n", r.Title, strings.TrimSpace(r.Summary), strings.TrimSpace(r.Body), strings.TrimSpace(r.Points))
	if len(r.References) > 0 {
		b.WriteString("\n## 参考\n\n")
		for _, ref := range r.References {
			title := ref.Title
			if strings.TrimSpace(title) == "" {
				title = ref.URL
			}
			fmt.Fprintf(&b, "- [%s](%s)\n", escapeLinkText(title), ref.URL)
		}
	}
	return b.String()
}

// escapeLinkText keeps a title from closing its link's text.
func escapeLinkText(s string) string {
	return strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`, "\n", " ").Replace(s)
}
