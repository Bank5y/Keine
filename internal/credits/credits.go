// Package credits keeps what the exploration assistant costs each reader
// (ADR 0010): the plans and their daily credits, one usage row a run at
// the rates of its time, and the usage the admin page adds up.
package credits

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Bank5y/keine/internal/assistant"
)

var (
	ErrPlanNotFound = errors.New("plan not found")
	ErrUserNotFound = errors.New("user not found")
	ErrNameTaken    = errors.New("a plan has this name")
	ErrInvalidPlan  = errors.New("invalid plan")
	// ErrBuiltin: "不限" stays, so admins always have a plan to be on.
	ErrBuiltin = errors.New("the built-in plan cannot be deleted")
	// ErrDefault: new readers need a plan to start on.
	ErrDefault = errors.New("the default plan cannot be deleted")
	ErrInUse   = errors.New("readers are on the plan")
)

// InsufficientError: what a run is expected to cost is more than the
// reader has left today.
type InsufficientError struct {
	Estimate  float64 `json:"estimate"`
	Remaining float64 `json:"remaining"`
}

func (e *InsufficientError) Error() string {
	return fmt.Sprintf("a run of about %.2f credits with %.2f left", e.Estimate, e.Remaining)
}

// Afford lets a run expected to cost estimate start when the reader has
// that much left today, or is on an unlimited plan; once started, a run
// may overdraw (ADR 0010).
func (s *Service) Afford(ctx context.Context, userID int64, estimate float64) error {
	a, err := s.Account(ctx, userID)
	if err != nil {
		return err
	}
	if a.Remaining != nil && estimate > *a.Remaining {
		return &InsufficientError{Estimate: estimate, Remaining: *a.Remaining}
	}
	return nil
}

type Service struct {
	db  *pgxpool.Pool
	now func() time.Time
}

// NewService keeps credits by the given clock, whose local midnight is
// when the day's credits reset.
func NewService(db *pgxpool.Pool, now func() time.Time) *Service {
	return &Service{db: db, now: now}
}

// Plan is a plan as the admin page shows it. Daily is nil for unlimited.
type Plan struct {
	ID      int64    `json:"id"`
	Name    string   `json:"name"`
	Daily   *float64 `json:"daily"`
	Default bool     `json:"default"`
	Builtin bool     `json:"builtin"`
	Users   int      `json:"users"`
}

// PlanInput is a plan as an admin submits it; a nil Daily is unlimited.
type PlanInput struct {
	Name  string   `json:"name"`
	Daily *float64 `json:"daily"`
}

// Account is a reader's credits today: their plan, its daily credits and
// what is left of them (both nil when unlimited), and what they used
// since midnight. Remaining goes below zero when a run overdraws.
type Account struct {
	PlanID    int64    `json:"planId"`
	Plan      string   `json:"plan"`
	Daily     *float64 `json:"daily"`
	Used      float64  `json:"used"`
	Remaining *float64 `json:"remaining"`
}

func (a *Account) settle() {
	if a.Daily != nil {
		r := *a.Daily - a.Used
		a.Remaining = &r
	}
}

// today is when the current day began, at the server's midnight.
func (s *Service) today() time.Time {
	y, m, d := s.now().In(time.Local).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// Account is the reader's credits now.
func (s *Service) Account(ctx context.Context, userID int64) (Account, error) {
	var a Account
	err := s.db.QueryRow(ctx, `
		SELECT p.id, p.name, p.daily_credits,
			coalesce((SELECT sum(credits) FROM usage_records r WHERE r.user_id = u.id AND r.created_at >= $2), 0)
		FROM users u JOIN plans p ON p.id = u.plan_id WHERE u.id = $1`, userID, s.today()).
		Scan(&a.PlanID, &a.Plan, &a.Daily, &a.Used)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrUserNotFound
	}
	a.settle()
	return a, err
}

// Accounts are every reader's credits now, by user ID.
func (s *Service) Accounts(ctx context.Context) (map[int64]Account, error) {
	rows, err := s.db.Query(ctx, `
		SELECT u.id, p.id, p.name, p.daily_credits, coalesce(r.used, 0)
		FROM users u JOIN plans p ON p.id = u.plan_id
		LEFT JOIN (SELECT user_id, sum(credits) AS used FROM usage_records WHERE created_at >= $1 GROUP BY user_id) r ON r.user_id = u.id`,
		s.today())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]Account{}
	for rows.Next() {
		var id int64
		var a Account
		if err := rows.Scan(&id, &a.PlanID, &a.Plan, &a.Daily, &a.Used); err != nil {
			return nil, err
		}
		a.settle()
		out[id] = a
	}
	return out, rows.Err()
}

// Cost is what a run costs at rates: the four kinds of tokens apart,
// per million, and each search billed.
func Cost(u assistant.Usage, searches int, r assistant.Rates) float64 {
	n := func(p *int64) float64 {
		if p == nil {
			return 0
		}
		return float64(*p)
	}
	return (n(u.Input)*r.Input+n(u.CacheRead)*r.CacheRead+n(u.CacheWrite)*r.CacheWrite+n(u.Output)*r.Output)/1e6 +
		float64(searches)*r.Search
}

// Estimate is what a run is expected to cost before it starts (#123):
// the first input at the input rate and three times over as cache hits,
// 2,000 output tokens (4,000 thinking) and two searches.
func Estimate(input int, t assistant.Turn) float64 {
	out := int64(2000)
	if t.Thinking {
		out = 4000
	}
	in, hits := int64(input), int64(input)*3
	return Cost(assistant.Usage{Input: &in, CacheRead: &hits, Output: &out}, 2, t.Rates)
}

// Record is one run's usage, kept apart from any message.
type Record struct {
	// UserID is the reader charged; 0 for the site's own runs (building a
	// concept index), which count in the site's usage and no reader's.
	UserID    int64
	Kind      assistant.TurnKind
	Model     string
	Usage     assistant.Usage
	Searches  int
	Credits   float64
	Estimated bool
}

// RecordRun keeps what a run used, at the rates of its turn now; a run
// whose calls cost nothing leaves no row.
func (s *Service) RecordRun(ctx context.Context, userID int64, kind assistant.TurnKind, t assistant.Turn, m assistant.Meta) error {
	if !m.Charged {
		return nil
	}
	return s.Record(ctx, Record{
		UserID: userID, Kind: kind, Model: t.DisplayName, Usage: m.Usage, Searches: m.Searches,
		Credits: Cost(m.Usage, m.Searches, t.Rates), Estimated: m.Estimated,
	})
}

// Record keeps a run's usage as of now.
func (s *Service) Record(ctx context.Context, r Record) error {
	u := r.Usage
	_, err := s.db.Exec(ctx, `
		INSERT INTO usage_records (user_id, kind, model, input, cache_read, cache_write, output, reasoning, searches, credits, estimated, created_at)
		VALUES (NULLIF($1, 0), $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		r.UserID, r.Kind, r.Model, u.Input, u.CacheRead, u.CacheWrite, u.Output, u.Reasoning, r.Searches, r.Credits, r.Estimated, s.now())
	return err
}

// Plans lists the plans, the built-in one first, with how many readers
// are on each.
func (s *Service) Plans(ctx context.Context) ([]Plan, error) {
	rows, err := s.db.Query(ctx, `
		SELECT p.id, p.name, p.daily_credits, p.is_default, p.builtin, (SELECT count(*) FROM users u WHERE u.plan_id = p.id)
		FROM plans p ORDER BY p.builtin DESC, p.daily_credits NULLS LAST, p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Plan{}
	for rows.Next() {
		var p Plan
		if err := rows.Scan(&p.ID, &p.Name, &p.Daily, &p.Default, &p.Builtin, &p.Users); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) plan(ctx context.Context, id int64) (Plan, error) {
	ps, err := s.Plans(ctx)
	if err != nil {
		return Plan{}, err
	}
	for _, p := range ps {
		if p.ID == id {
			return p, nil
		}
	}
	return Plan{}, ErrPlanNotFound
}

func (in *PlanInput) check() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 40 || (in.Daily != nil && *in.Daily < 0) {
		return ErrInvalidPlan
	}
	return nil
}

func nameTaken(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" && pe.ConstraintName == "plans_name_key" {
		return ErrNameTaken
	}
	return err
}

// CreatePlan adds a plan.
func (s *Service) CreatePlan(ctx context.Context, in PlanInput) (Plan, error) {
	if err := in.check(); err != nil {
		return Plan{}, err
	}
	var id int64
	if err := s.db.QueryRow(ctx, "INSERT INTO plans (name, daily_credits) VALUES ($1, $2) RETURNING id", in.Name, in.Daily).Scan(&id); err != nil {
		return Plan{}, nameTaken(err)
	}
	return s.plan(ctx, id)
}

// UpdatePlan renames a plan or changes its daily credits, at once for
// everyone on it; today's usage stays.
func (s *Service) UpdatePlan(ctx context.Context, id int64, in PlanInput) (Plan, error) {
	if err := in.check(); err != nil {
		return Plan{}, err
	}
	tag, err := s.db.Exec(ctx, "UPDATE plans SET name = $2, daily_credits = $3 WHERE id = $1", id, in.Name, in.Daily)
	if err != nil {
		return Plan{}, nameTaken(err)
	}
	if tag.RowsAffected() == 0 {
		return Plan{}, ErrPlanNotFound
	}
	return s.plan(ctx, id)
}

// DeletePlan removes a plan no reader is on, other than the built-in and
// the default one.
func (s *Service) DeletePlan(ctx context.Context, id int64) error {
	p, err := s.plan(ctx, id)
	switch {
	case err != nil:
		return err
	case p.Builtin:
		return ErrBuiltin
	case p.Default:
		return ErrDefault
	case p.Users > 0:
		return ErrInUse
	}
	_, err = s.db.Exec(ctx, "DELETE FROM plans WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM users WHERE plan_id = $1)", id)
	return err
}

// SetDefault makes a plan the one new readers start on.
func (s *Service) SetDefault(ctx context.Context, id int64) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "UPDATE plans SET is_default = false WHERE is_default AND id <> $1", id); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, "UPDATE plans SET is_default = true WHERE id = $1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPlanNotFound
	}
	return tx.Commit(ctx)
}

// Assign puts a reader on a plan, which counts from now on: what they
// used today stays used.
func (s *Service) Assign(ctx context.Context, userID, planID int64) error {
	var ok bool
	if err := s.db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM plans WHERE id = $1)", planID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrPlanNotFound
	}
	tag, err := s.db.Exec(ctx, "UPDATE users SET plan_id = $2 WHERE id = $1", userID, planID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return err
}
