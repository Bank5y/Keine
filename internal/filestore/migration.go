package filestore

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/Bank5y/keine/internal/storage"
)

// Migration is the last migration started, running or not.
type Migration struct {
	Direction storage.Direction `json:"direction"`
	// State is running, done, failed or cancelled.
	State    string     `json:"state"`
	Started  time.Time  `json:"started"`
	Finished *time.Time `json:"finished,omitempty"`
	Error    string     `json:"error,omitempty"`
	storage.Progress
}

type jobs struct {
	mu      sync.Mutex
	last    *Migration
	cancel  context.CancelFunc
	running sync.WaitGroup
}

// Migrate starts moving files in the background (one migration at a time)
// and returns its first state. A push needs the bucket active, or the files
// it moves could not be read; a sync and a pull need it connected.
func (s *Service) Migrate(dir storage.Direction) (Migration, error) {
	if !dir.Valid() {
		return Migration{}, Invalid{"迁移方向只能是推送、同步或拉取"}
	}
	st := s.Status()
	cloud := s.Cloud()
	if cloud == nil {
		return Migration{}, ErrNotConnected
	}
	if dir == storage.Push && !st.Active {
		return Migration{}, Invalid{"推送会删除本地文件, 需要先启用对象存储"}
	}
	media := s.files.Route().Media
	if !media.Valid() {
		// a sync while the bucket is off copies what it would hold
		cur, err := s.Load(context.Background())
		if err != nil {
			return Migration{}, err
		}
		media = cur.Media
	}

	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	if s.jobs.last != nil && s.jobs.last.State == "running" {
		return *s.jobs.last, ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Migration{Direction: dir, State: "running", Started: time.Now()}
	s.jobs.last, s.jobs.cancel = m, cancel
	s.jobs.running.Add(1)
	go func() {
		defer s.jobs.running.Done()
		defer cancel()
		p, err := storage.Migrate(ctx, s.files.Local(), cloud, dir, media.Covers, func(p storage.Progress) {
			s.jobs.mu.Lock()
			m.Progress = p
			s.jobs.mu.Unlock()
		})
		s.jobs.mu.Lock()
		defer s.jobs.mu.Unlock()
		now := time.Now()
		m.Progress, m.Finished = p, &now
		switch {
		case errors.Is(err, context.Canceled):
			m.State = "cancelled"
		case err != nil:
			m.State, m.Error = "failed", err.Error()
		default:
			m.State = "done"
		}
		slog.Info("file migration finished", "direction", dir, "state", m.State, "copied", p.Copied, "failed", p.Failed, "err", err)
	}()
	return *m, nil
}

// LastMigration is the last migration started, if any.
func (s *Service) LastMigration() *Migration {
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	if s.jobs.last == nil {
		return nil
	}
	m := *s.jobs.last
	m.Errors = append([]string(nil), m.Errors...)
	return &m
}

// CancelMigration stops a running migration; the files it has moved stay
// moved.
func (s *Service) CancelMigration() {
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	if s.jobs.cancel != nil {
		s.jobs.cancel()
	}
}

// Close stops a running migration and waits for it.
func (s *Service) Close() {
	s.CancelMigration()
	s.jobs.running.Wait()
}
