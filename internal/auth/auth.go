// Package auth owns users, their sign-in identities and sessions.
//
// Signing in goes through a Provider. The first version ships only the
// password provider; single sign-on providers plug in later by implementing
// Provider and resolving to the same users through the identities table.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUnknownProvider    = errors.New("unknown login provider")
	ErrUnauthenticated    = errors.New("not signed in")
	ErrDisabled           = errors.New("account disabled")
	ErrUsernameTaken      = errors.New("username taken")
	ErrInvalidUsername    = errors.New("invalid username")
	ErrWeakPassword       = errors.New("password too short")
	ErrNotFound           = errors.New("user not found")
)

const (
	RoleUser  = "user"
	RoleAdmin = "admin"

	sessionTTL = 30 * 24 * time.Hour
)

type User struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"createdAt"`
}

func (u User) IsAdmin() bool { return u.Role == RoleAdmin }

// Provider turns a sign-in request into a user.
type Provider interface {
	Name() string
	// Authenticate returns the id of the user the request proves to be, or
	// ErrInvalidCredentials.
	Authenticate(ctx context.Context, payload json.RawMessage) (userID int64, err error)
}

type Service struct {
	db        *pgxpool.Pool
	providers map[string]Provider
}

func NewService(db *pgxpool.Pool) *Service {
	s := &Service{db: db, providers: map[string]Provider{}}
	s.Register(&PasswordProvider{db: db})
	return s
}

func (s *Service) Register(p Provider) { s.providers[p.Name()] = p }

// Login authenticates through the named provider and opens a session. It
// returns the session token to hand to the client.
func (s *Service) Login(ctx context.Context, provider string, payload json.RawMessage) (string, User, error) {
	p, ok := s.providers[provider]
	if !ok {
		return "", User{}, ErrUnknownProvider
	}
	id, err := p.Authenticate(ctx, payload)
	if err != nil {
		return "", User{}, err
	}
	u, err := s.User(ctx, id)
	if err != nil {
		return "", User{}, err
	}
	if u.Disabled {
		return "", User{}, ErrDisabled
	}
	token, err := s.openSession(ctx, id)
	return token, u, err
}

func (s *Service) openSession(ctx context.Context, userID int64) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	_, err := s.db.Exec(ctx, "INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)",
		hashToken(token), userID, time.Now().Add(sessionTTL))
	return token, err
}

// Authenticate resolves a session token to its user.
func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, ErrUnauthenticated
	}
	var u User
	err := s.db.QueryRow(ctx, `
		SELECT u.id, u.username, u.role, u.disabled, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, hashToken(token)).
		Scan(&u.ID, &u.Username, &u.Role, &u.Disabled, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && u.Disabled) {
		return User{}, ErrUnauthenticated
	}
	return u, err
}

func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.db.Exec(ctx, "DELETE FROM sessions WHERE token_hash = $1", hashToken(token))
	return err
}

func (s *Service) User(ctx context.Context, id int64) (User, error) {
	var u User
	err := s.db.QueryRow(ctx, "SELECT id, username, role, disabled, created_at FROM users WHERE id = $1", id).
		Scan(&u.ID, &u.Username, &u.Role, &u.Disabled, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// EnsureAdmin creates the admin account named by the deployment. An existing
// account keeps its password, so a password changed in the app survives a
// restart; it is only made sure to be an admin.
func (s *Service) EnsureAdmin(ctx context.Context, username, password string) error {
	var id int64
	err := s.db.QueryRow(ctx, "SELECT id FROM users WHERE lower(username) = lower($1)", username).Scan(&id)
	if err == nil {
		_, err = s.db.Exec(ctx, "UPDATE users SET role = $2 WHERE id = $1", id, RoleAdmin)
		return err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = s.CreateUser(ctx, username, password, RoleAdmin)
	return err
}

// CreateUser adds a user that signs in with a username and password.
func (s *Service) CreateUser(ctx context.Context, username, password, role string) (User, error) {
	if err := validUsername(username); err != nil {
		return User{}, err
	}
	if err := validPassword(password); err != nil {
		return User{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var u User
	err = tx.QueryRow(ctx, `
		INSERT INTO users (username, role) VALUES ($1, $2)
		ON CONFLICT DO NOTHING
		RETURNING id, username, role, disabled, created_at`, username, role).
		Scan(&u.ID, &u.Username, &u.Role, &u.Disabled, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUsernameTaken
	}
	if err != nil {
		return User{}, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO identities (user_id, provider, subject, secret) VALUES ($1, $2, lower($3), $4)",
		u.ID, passwordProvider, username, hash); err != nil {
		return User{}, err
	}
	return u, tx.Commit(ctx)
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// ChangePassword replaces a user's password after checking the current one.
func (s *Service) ChangePassword(ctx context.Context, userID int64, current, next string) error {
	var hash string
	err := s.db.QueryRow(ctx, "SELECT secret FROM identities WHERE user_id = $1 AND provider = $2", userID, passwordProvider).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidCredentials
	}
	if err != nil {
		return err
	}
	if !checkPassword(hash, current) {
		return ErrInvalidCredentials
	}
	return s.SetPassword(ctx, userID, next)
}

// SetPassword replaces a user's password without checking the old one.
func (s *Service) SetPassword(ctx context.Context, userID int64, password string) error {
	if err := validPassword(password); err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, "UPDATE identities SET secret = $3 WHERE user_id = $1 AND provider = $2", userID, passwordProvider, hash)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
