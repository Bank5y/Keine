// Package storage keeps course files behind one interface so the backend can
// move between local disk and S3-compatible object storage (ADR-0001).
package storage

import (
	"context"
	"errors"
	"io"
	"strings"
)

var (
	ErrNotFound   = errors.New("object not found")
	ErrInvalidKey = errors.New("invalid object key")
)

// Store holds course files by key. Keys are slash-separated relative paths
// such as "courses/12/images/a.png".
type Store interface {
	// Put stores the object, replacing any previous one with the same key.
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	// Get streams the object; a missing object is ErrNotFound.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes the object; a missing object is not an error.
	Delete(ctx context.Context, key string) error
	// DeletePrefix removes every object whose key starts with prefix.
	DeletePrefix(ctx context.Context, prefix string) error
}

// CheckKey rejects keys that are empty, absolute, contain empty, "." or ".."
// segments or backslashes, so no key can escape its prefix.
func CheckKey(key string) error {
	if key == "" || strings.ContainsRune(key, '\\') {
		return ErrInvalidKey
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return ErrInvalidKey
		}
	}
	return nil
}

// checkPrefix accepts a key followed by "/".
func checkPrefix(prefix string) error {
	p, ok := strings.CutSuffix(prefix, "/")
	if !ok {
		return ErrInvalidKey
	}
	return CheckKey(p)
}
