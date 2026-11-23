package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
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
	if !checkPassword(hash, in.Password) {
		return 0, ErrInvalidCredentials
	}
	return id, nil
}

func checkPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// hashCost is bcrypt's work factor: the default, except in a test binary,
// where the API tests sign in thousands of times and the cost buys nothing.
// A hash keeps the cost it was made with, so either checks the other.
var hashCost = func() int {
	if testing.Testing() {
		return bcrypt.MinCost
	}
	return bcrypt.DefaultCost
}()

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("keine-dummy-password"), hashCost)

func hashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), hashCost)
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
