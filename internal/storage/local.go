package storage

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Local keeps objects as files under a root directory.
type Local struct{ root string }

func NewLocal(root string) *Local { return &Local{root: root} }

func (l *Local) path(key string) (string, error) {
	if err := CheckKey(key); err != nil {
		return "", err
	}
	return filepath.Join(l.root, filepath.FromSlash(key)), nil
}

// Put writes to a temporary file first so readers never see a half-written
// object.
func (l *Local) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".put-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func (l *Local) Get(_ context.Context, key string) (io.ReadCloser, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if st, err := f.Stat(); err == nil && st.IsDir() {
		f.Close()
		return nil, ErrNotFound
	}
	return f, nil
}

func (l *Local) Delete(_ context.Context, key string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (l *Local) DeletePrefix(_ context.Context, prefix string) error {
	if err := checkPrefix(prefix); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(l.root, filepath.FromSlash(prefix)))
}
