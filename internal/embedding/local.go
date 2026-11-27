package embedding

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
)

// LocalModel is a model the admin can download and run in process. Speed
// measured with the pure Go backend (#227): on 16 cores 0.55s a passage
// of 700 characters and 0.24s a query of 300; on 2 cores 1.3s and 0.62s.
type LocalModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Dims int    `json:"dims"`
	// Size is about how many bytes it downloads.
	Size  int64 `json:"size"`
	repo  string
	files []string
}

// Models are the local models on offer.
var Models = []LocalModel{{
	ID: "bge-small-zh-v1.5", Name: "bge-small-zh-v1.5", Dims: 512, Size: 95_400_000,
	repo:  "Xenova/bge-small-zh-v1.5",
	files: []string{"onnx/model.onnx", "tokenizer.json", "config.json", "tokenizer_config.json", "special_tokens_map.json"},
}}

func modelByID(id string) (LocalModel, bool) {
	for _, m := range Models {
		if m.ID == id {
			return m, true
		}
	}
	return LocalModel{}, false
}

// A download's status.
const (
	DownloadRunning = "running"
	DownloadDone    = "done"
	DownloadFailed  = "failed"
)

// Download is how a local model's download stands: bytes so far and in
// all, and why it failed. A model already on disk is done.
type Download struct {
	Model  string `json:"model"`
	Status string `json:"status"`
	Done   int64  `json:"done"`
	Total  int64  `json:"total"`
	Error  string `json:"error,omitempty"`
}

func (s *Service) modelDir(id string) string { return filepath.Join(s.dir, id) }

// downloaded tells whether every file of the model is on disk.
func (s *Service) downloaded(m LocalModel) bool {
	for _, f := range m.files {
		if _, err := os.Stat(filepath.Join(s.modelDir(m.ID), filepath.Base(f))); err != nil {
			return false
		}
	}
	return true
}

func (s *Service) downloads() []Download {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Download{}
	for _, m := range Models {
		if d, ok := s.loads[m.ID]; ok && d.Status != DownloadDone {
			out = append(out, *d)
		} else if s.downloaded(m) {
			out = append(out, Download{Model: m.ID, Status: DownloadDone, Done: m.Size, Total: m.Size})
		}
	}
	return out
}

// StartDownload downloads a local model from mirror (Hugging Face or a
// mirror of it) in the background; a file cut off before resumes where it
// stopped. It does nothing while the model is downloading already.
func (s *Service) StartDownload(ctx context.Context, id, mirror string) error {
	m, ok := modelByID(id)
	if !ok {
		return ErrUnknownModel
	}
	if err := s.SetMirror(ctx, mirror); err != nil {
		return err
	}
	st, err := s.read(ctx, s.db, false)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if d, ok := s.loads[id]; ok && d.Status == DownloadRunning {
		s.mu.Unlock()
		return nil
	}
	d := &Download{Model: id, Status: DownloadRunning, Total: m.Size}
	s.loads[id] = d
	s.mu.Unlock()
	go func() {
		err := s.download(context.WithoutCancel(ctx), m, st.mirror, d)
		s.mu.Lock()
		defer s.mu.Unlock()
		if err != nil {
			d.Status, d.Error = DownloadFailed, err.Error()
			return
		}
		d.Status, d.Done = DownloadDone, d.Total
	}()
	return nil
}

func (s *Service) download(ctx context.Context, m LocalModel, mirror string, d *Download) error {
	dir := s.modelDir(m.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var before int64
	for _, f := range m.files {
		dest := filepath.Join(dir, filepath.Base(f))
		if fi, err := os.Stat(dest); err == nil {
			before += fi.Size()
			s.progress(d, before)
			continue
		}
		n, err := s.fetch(ctx, fmt.Sprintf("%s/%s/resolve/main/%s", mirror, m.repo, f), dest, func(n int64) { s.progress(d, before+n) })
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		before += n
	}
	return nil
}

func (s *Service) progress(d *Download, done int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d.Done = done
	if done > d.Total {
		d.Total = done
	}
}

// fetch downloads url to dest through dest.part, asking for the rest of a
// part left from before, and tells how many bytes dest has.
func (s *Service) fetch(ctx context.Context, url, dest string, progress func(int64)) (int64, error) {
	part := dest + ".part"
	var have int64
	if fi, err := os.Stat(part); err == nil {
		have = fi.Size()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	if have > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(have, 10)+"-")
	}
	client := &http.Client{Timeout: 30 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	switch res.StatusCode {
	case http.StatusPartialContent:
	case http.StatusOK:
		have, flags = 0, os.O_CREATE|os.O_WRONLY|os.O_TRUNC
	default:
		return 0, fmt.Errorf("download answered %d", res.StatusCode)
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return 0, err
	}
	buf := make([]byte, 256<<10)
	for {
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				return 0, err
			}
			have += int64(n)
			progress(have)
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			f.Close()
			return 0, rerr
		}
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	return have, os.Rename(part, dest)
}

// localModel runs a downloaded model in process with the pure Go backend.
type localModel struct {
	mu       sync.Mutex
	session  *hugot.Session
	pipeline *pipelines.FeatureExtractionPipeline
}

// local is the downloaded model id, loaded once.
func (s *Service) local(id string) (*localModel, error) {
	m, ok := modelByID(id)
	if !ok {
		return nil, ErrUnknownModel
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.locals[id]; ok {
		return l, nil
	}
	if !s.downloaded(m) {
		return nil, ErrNotDownloaded
	}
	session, err := hugot.NewGoSession()
	if err != nil {
		return nil, err
	}
	p, err := hugot.NewPipeline(session, hugot.FeatureExtractionConfig{
		ModelPath: s.modelDir(id), Name: id, OnnxFilename: "model.onnx",
		Options: []hugot.FeatureExtractionOption{pipelines.WithNormalization()},
	})
	if err != nil {
		_ = session.Destroy()
		return nil, err
	}
	l := &localModel{session: session, pipeline: p}
	s.locals[id] = l
	return l, nil
}

// localBatch is how many texts the local model embeds at a time: it gains
// nothing from more, and a smaller batch lets a query in sooner.
const localBatch = 4

func (l *localModel) Embed(ctx context.Context, texts []string) ([][]float32, Usage, error) {
	var out [][]float32
	for from := 0; from < len(texts); from += localBatch {
		if err := ctx.Err(); err != nil {
			return nil, Usage{}, err
		}
		l.mu.Lock()
		r, err := l.pipeline.RunPipeline(texts[from:min(from+localBatch, len(texts))])
		l.mu.Unlock()
		if err != nil {
			return nil, Usage{}, err
		}
		out = append(out, r.Embeddings...)
	}
	return out, Usage{}, nil
}
