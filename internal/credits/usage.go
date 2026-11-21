package credits

import (
	"context"
	"errors"
	"time"
)

// ErrBadRange: a range that ends before it starts, or is not dates.
var ErrBadRange = errors.New("bad date range")

// Range is the days a usage report covers, both ends included, by the
// server's calendar.
type Range struct{ From, To time.Time }

const dateLayout = "2006-01-02"

// DefaultDays is how far back a report goes when no range is given.
const DefaultDays = 30

// ParseRange reads a range from dates ("" for the default: the last 30
// days, today included).
func (s *Service) ParseRange(from, to string) (Range, error) {
	today := s.today()
	r := Range{From: today.AddDate(0, 0, -(DefaultDays - 1)), To: today}
	var err error
	if from != "" {
		if r.From, err = time.ParseInLocation(dateLayout, from, time.Local); err != nil {
			return Range{}, ErrBadRange
		}
	}
	if to != "" {
		if r.To, err = time.ParseInLocation(dateLayout, to, time.Local); err != nil {
			return Range{}, ErrBadRange
		}
	}
	if r.To.Before(r.From) {
		return Range{}, ErrBadRange
	}
	return r, nil
}

// end is the midnight after the range's last day.
func (r Range) end() time.Time { return r.To.AddDate(0, 0, 1) }

// offset is how far the server's calendar is from UTC, in seconds, for
// telling which day a run fell on.
func (r Range) offset() int {
	_, off := r.From.Zone()
	return off
}

// Totals is the usage of some runs added up; Estimated is how many of
// them were worked out locally.
type Totals struct {
	Runs       int64   `json:"runs"`
	Input      int64   `json:"input"`
	CacheRead  int64   `json:"cacheRead"`
	CacheWrite int64   `json:"cacheWrite"`
	Output     int64   `json:"output"`
	Reasoning  int64   `json:"reasoning"`
	Searches   int64   `json:"searches"`
	Credits    float64 `json:"credits"`
	Estimated  int64   `json:"estimated"`
}

const totalsSQL = `count(*), coalesce(sum(input), 0), coalesce(sum(cache_read), 0), coalesce(sum(cache_write), 0),
	coalesce(sum(output), 0), coalesce(sum(reasoning), 0), coalesce(sum(searches), 0), coalesce(sum(credits), 0),
	count(*) FILTER (WHERE estimated)`

func (t *Totals) dest() []any {
	return []any{&t.Runs, &t.Input, &t.CacheRead, &t.CacheWrite, &t.Output, &t.Reasoning, &t.Searches, &t.Credits, &t.Estimated}
}

// Day is the usage of one day.
type Day struct {
	Date string `json:"date"`
	Totals
}

// Usage adds up the runs in the range, of one reader or (user 0) of
// everyone: in all, and day by day, the latest day first and days without
// runs left out.
func (s *Service) Usage(ctx context.Context, r Range, user int64) (Totals, []Day, error) {
	var total Totals
	if err := s.db.QueryRow(ctx, `SELECT `+totalsSQL+` FROM usage_records
		WHERE created_at >= $1 AND created_at < $2 AND ($3 = 0 OR user_id = $3)`, r.From, r.end(), user).Scan(total.dest()...); err != nil {
		return Totals{}, nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT to_char(created_at AT TIME ZONE 'UTC' + make_interval(secs => $4), 'YYYY-MM-DD') AS day, `+totalsSQL+`
		FROM usage_records WHERE created_at >= $1 AND created_at < $2 AND ($3 = 0 OR user_id = $3)
		GROUP BY day ORDER BY day DESC`, r.From, r.end(), user, r.offset())
	if err != nil {
		return Totals{}, nil, err
	}
	defer rows.Close()
	days := []Day{}
	for rows.Next() {
		var d Day
		if err := rows.Scan(append([]any{&d.Date}, d.dest()...)...); err != nil {
			return Totals{}, nil, err
		}
		days = append(days, d)
	}
	return total, days, rows.Err()
}

// ReaderUsage is one reader's usage in a range, with the plan they are on.
type ReaderUsage struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Plan     string `json:"plan"`
	Totals
}

// ByReader is each reader's usage in the range, the readers without runs
// left out, the most credits first.
func (s *Service) ByReader(ctx context.Context, r Range) ([]ReaderUsage, error) {
	rows, err := s.db.Query(ctx, `SELECT u.id, u.username, p.name, `+totalsSQL+`
		FROM usage_records JOIN users u ON u.id = usage_records.user_id JOIN plans p ON p.id = u.plan_id
		WHERE usage_records.created_at >= $1 AND usage_records.created_at < $2
		GROUP BY u.id, u.username, p.name ORDER BY coalesce(sum(credits), 0) DESC, u.username`, r.From, r.end())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReaderUsage{}
	for rows.Next() {
		var u ReaderUsage
		if err := rows.Scan(append([]any{&u.ID, &u.Username, &u.Plan}, u.dest()...)...); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Run is one usage row as the admin page shows it; a token count is nil
// where the provider did not report it.
type Run struct {
	ID         int64     `json:"id"`
	Kind       string    `json:"kind"`
	Model      string    `json:"model"`
	Input      *int64    `json:"input"`
	CacheRead  *int64    `json:"cacheRead"`
	CacheWrite *int64    `json:"cacheWrite"`
	Output     *int64    `json:"output"`
	Reasoning  *int64    `json:"reasoning"`
	Searches   int       `json:"searches"`
	Credits    float64   `json:"credits"`
	Estimated  bool      `json:"estimated"`
	At         time.Time `json:"at"`
}

// RunsShown is how many of a reader's latest runs a report lists.
const RunsShown = 100

// Runs are the reader's latest runs in the range, the latest first.
func (s *Service) Runs(ctx context.Context, r Range, user int64) ([]Run, error) {
	rows, err := s.db.Query(ctx, `SELECT id, kind, model, input, cache_read, cache_write, output, reasoning, searches, credits, estimated, created_at
		FROM usage_records WHERE user_id = $1 AND created_at >= $2 AND created_at < $3 ORDER BY created_at DESC, id DESC LIMIT $4`,
		user, r.From, r.end(), RunsShown)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var x Run
		if err := rows.Scan(&x.ID, &x.Kind, &x.Model, &x.Input, &x.CacheRead, &x.CacheWrite, &x.Output, &x.Reasoning, &x.Searches, &x.Credits, &x.Estimated, &x.At); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
