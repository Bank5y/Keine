package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const (
	passwordProvider  = "password"
	minPasswordLength = 8
	maxUsernameLength = 32
)

// PasswordProvider signs users in with a username and password.
type PasswordProvider struct{ db *pgxpool.Pool }

func (*PasswordProvider) Name() string { return passwordProvider }

func (p *PasswordProvider) Authenticate(ctx context.Context, payload json.RawMessage) (int64, error) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(payload, &in); err != nil || in.Username == "" {
		return 0, ErrInvalidCredentials
	}
	var id int64
	var hash string
	err := p.db.QueryRow(ctx, "SELECT user_id, secret FROM identities WHERE provider = $1 AND subject = lower($2)",
		passwordProvider, in.Username).Scan(&id, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		// spend the same time as a real check so usernames can't be probed by timing
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(in.Password))
		return 0, ErrInvalidCredentials
	}
	if err != nil {
		return 0, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil {
		return 0, ErrInvalidCredentials
	}
	return id, nil
}

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("keine-dummy-password"), bcrypt.DefaultCost)

func hashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func validUsername(name string) error {
	if strings.TrimSpace(name) != name || name == "" || utf8.RuneCountInString(name) > maxUsernameLength {
		return ErrInvalidUsername
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || r == '/' || r == '@' {
			return ErrInvalidUsername
		}
	}
	return nil
}

func validPassword(password string) error {
	if utf8.RuneCountInString(password) < minPasswordLength {
		return ErrWeakPassword
	}
	return nil
}
