package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// Direction is which way a migration moves course files.
type Direction string

const (
	// Push moves files from local disk to the cloud and removes them
	// locally.
	Push Direction = "push"
	// Sync copies files from local disk to the cloud and keeps them.
	Sync Direction = "sync"
	// Pull copies the cloud's files to local disk and keeps them in the
	// cloud.
	Pull Direction = "pull"
)

func (d Direction) Valid() bool { return d == Push || d == Sync || d == Pull }

// Root holds every course file.
const Root = "courses/"

// Progress is how far a migration has got.
type Progress struct {
	Total   int   `json:"total"`
	Done    int   `json:"done"`
	Copied  int   `json:"copied"`
	Skipped int   `json:"skipped"`
	Failed  int   `json:"failed"`
	Bytes   int64 `json:"bytes"`
	// Errors are the first failures, for the admin to look into.
	Errors []string `json:"errors"`
}

const maxErrors = 20

// Sizer tells an object's size; a missing object is ErrNotFound.
type Sizer interface {
	Size(ctx context.Context, key string) (int64, error)
}

// Migrate moves the course files want covers between local disk and the
// cloud (all of the cloud's files on a pull). A file already on the target
// with the same size is not copied again. One file failing does not stop
// the rest; report, if not nil, sees the progress after every file.
func Migrate(ctx context.Context, local, cloud Store, dir Direction, want func(key string) bool, report func(Progress)) (Progress, error) {
	from, to := local, cloud
	if dir == Pull {
		from, to = cloud, local
	}
	var p Progress
	all, err := from.List(ctx, Root)
	if err != nil {
		return p, err
	}
	var keys []string
	for _, k := range all {
		if dir == Pull || want(k) {
			keys = append(keys, k)
		}
	}
	p.Total = len(keys)
	if report != nil {
		report(p)
	}
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return p, err
		}
		// a push deletes the source, so it never trusts a same-sized
		// object on the target to be the same file
		copied, n, err := migrateOne(ctx, from, to, key, dir != Push)
		if err == nil && dir == Push {
			err = from.Delete(ctx, key)
		}
		switch {
		case err != nil && ctx.Err() != nil:
			return p, ctx.Err()
		case err != nil:
			p.Failed++
			if len(p.Errors) < maxErrors {
				p.Errors = append(p.Errors, fmt.Sprintf("%s: %v", key, err))
			}
		case copied:
			p.Copied++
			p.Bytes += n
		default:
			p.Skipped++
		}
		p.Done++
		if report != nil {
			report(p)
		}
	}
	return p, nil
}

// migrateOne copies key, unless skipSame and to has it with the same size
// already.
func migrateOne(ctx context.Context, from, to Store, key string, skipSame bool) (bool, int64, error) {
	size, err := sizeOf(ctx, from, key)
	if err != nil {
		return false, 0, err
	}
	if skipSame {
		if have, err := sizeOf(ctx, to, key); err == nil && have == size {
			return false, 0, nil
		}
	}
	rc, err := from.Get(ctx, key)
	if err != nil {
		return false, 0, err
	}
	defer rc.Close()
	if err := to.Put(ctx, key, rc, size); err != nil {
		return false, 0, err
	}
	return true, size, nil
}

func sizeOf(ctx context.Context, s Store, key string) (int64, error) {
	if sz, ok := s.(Sizer); ok {
		return sz.Size(ctx, key)
	}
	return -1, errors.New("store cannot tell sizes")
}

// Size is the file's size.
func (l *Local) Size(_ context.Context, key string) (int64, error) {
	p, err := l.path(key)
	if err != nil {
		return 0, err
	}
	st, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) || err == nil && st.IsDir() {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

// ProbeStep is one step of a connection test.
type ProbeStep struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Ms    int64  `json:"ms"`
	Error string `json:"error,omitempty"`
}

type ProbeResult struct {
	OK    bool        `json:"ok"`
	Steps []ProbeStep `json:"steps"`
}

// Probe writes a small object, reads it back and deletes it, timing each
// step; it stops at the first step that fails.
func Probe(ctx context.Context, s Store) ProbeResult {
	key := ".keine-probe/" + rand.Text()
	body := []byte("keine probe " + time.Now().UTC().Format(time.RFC3339Nano))
	var res ProbeResult
	step := func(name string, f func() error) bool {
		start := time.Now()
		err := f()
		st := ProbeStep{Name: name, OK: err == nil, Ms: time.Since(start).Milliseconds()}
		if err != nil {
			st.Error = err.Error()
		}
		res.Steps = append(res.Steps, st)
		return err == nil
	}
	res.OK = step("write", func() error { return s.Put(ctx, key, bytes.NewReader(body), int64(len(body))) }) &&
		step("read", func() error {
			rc, err := s.Get(ctx, key)
			if err != nil {
				return err
			}
			defer rc.Close()
			got, err := io.ReadAll(rc)
			if err != nil {
				return err
			}
			if !bytes.Equal(got, body) {
				return errors.New("read back different content")
			}
			return nil
		}) &&
		step("delete", func() error { return s.Delete(ctx, key) })
	return res
}
