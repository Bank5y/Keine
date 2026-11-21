package assistant

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Notice is what the reader is told about third-party services (#132):
// whether they have read it, and the services by the names the admin gave
// them. Provider is the display name of the model that analyzes, Search
// that of the service its searches go to; either is "" when there is none.
type Notice struct {
	Read     bool   `json:"aiNoticeRead"`
	Provider string `json:"aiProvider"`
	Search   string `json:"aiSearch"`
}

// searchNames are the search services of the Keine tool route by name.
var searchNames = map[SearchRoute]string{RouteTavily: "Tavily", RouteDeepSeek: "DeepSeek"}

// Notice is the notice as the reader sees it now.
func (s *Models) Notice(ctx context.Context, userID int64) (Notice, error) {
	var n Notice
	if err := s.db.QueryRow(ctx, "SELECT ai_notice_read_at IS NOT NULL FROM users WHERE id = $1", userID).Scan(&n.Read); err != nil {
		return Notice{}, err
	}
	var (
		route    SearchRoute
		format   string
		provider Provider
	)
	err := s.db.QueryRow(ctx, `
		SELECT m.display_name, m.search_route, m.format, m.provider
		FROM turn_models t JOIN site_models m ON m.id = t.model_id WHERE t.kind = $1`, TurnAnalysis).
		Scan(&n.Provider, &route, &format, &provider)
	if errors.Is(err, pgx.ErrNoRows) {
		return n, nil
	}
	if err != nil {
		return Notice{}, err
	}
	tv, err := s.Tavily(ctx)
	if err != nil {
		return Notice{}, err
	}
	switch resolved, ok := Resolve(route, format, provider, tv.Set); {
	case !ok, resolved == RouteOff:
	case resolved == RouteProvider:
		n.Search = n.Provider
	default:
		n.Search = searchNames[resolved]
	}
	return n, nil
}

// ReadNotice notes that the reader has read the notice; reading it again
// keeps the first time.
func (s *Models) ReadNotice(ctx context.Context, userID int64) error {
	_, err := s.db.Exec(ctx, "UPDATE users SET ai_notice_read_at = coalesce(ai_notice_read_at, now()) WHERE id = $1", userID)
	return err
}
