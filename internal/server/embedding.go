package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/course"
	"github.com/Bank5y/keine/internal/credits"
	"github.com/Bank5y/keine/internal/embedding"
)

// TurnEmbedding is the kind of the usage the site's embedding API costs
// (#227): no run of the assistant, charged to the site.
const TurnEmbedding assistant.TurnKind = "embedding"

// embedBatch is how many passages the embedder takes at a time.
const embedBatch = 32

// recallOf is what recalls from a version of a course: with embedding on,
// the passages' vectors and a way to embed the answer, charged to the
// site (#227).
func (s *Server) recallOf(ctx context.Context, courseID int64, filesDir string) (course.CourseRecall, error) {
	cur, err := s.embedding.Current(ctx)
	if err != nil {
		slog.Warn("embedding not available", "err", err)
		cur = embedding.Current{}
	}
	var embed func(context.Context, string) ([]float32, error)
	if cur.Embedder != nil {
		embed = func(ctx context.Context, text string) ([]float32, error) {
			vs, used, err := cur.Embedder.Embed(ctx, []string{text})
			s.recordEmbedding(ctx, cur, used)
			if err != nil {
				return nil, err
			}
			return vs[0], nil
		}
	}
	return s.courses.RecallOf(ctx, courseID, filesDir, cur.Generation, embed)
}

// recordEmbedding charges the site for what an external API used.
func (s *Server) recordEmbedding(ctx context.Context, cur embedding.Current, used embedding.Usage) {
	if cur.Provider != embedding.External || used.Tokens == 0 {
		return
	}
	tokens := used.Tokens
	err := s.credits.Record(context.WithoutCancel(ctx), credits.Record{
		Kind: TurnEmbedding, Model: cur.Model, Usage: assistant.Usage{Input: &tokens},
		Credits: float64(tokens) * cur.Rate / 1e6,
	})
	if err != nil {
		slog.Error("embedding usage not recorded", "err", err)
	}
}

// embedInBackground starts the embedder (#227): it embeds the passages of
// every course's current version that have no vector of the current
// generation, once now and again each time embedSoon asks.
func (s *Server) embedInBackground() {
	s.embedding.Kick = make(chan struct{}, 1)
	s.embedSoon()
	s.building.Add(1)
	go func() {
		defer s.building.Done()
		for {
			select {
			case <-s.buildCtx.Done():
				return
			case <-s.embedding.Kick:
				s.embedMissing(s.buildCtx)
			}
		}
	}()
}

// embedSoon asks the embedder for a round.
func (s *Server) embedSoon() {
	select {
	case s.embedding.Kick <- struct{}{}:
	default:
	}
}

// embedMissing embeds passages missing a vector until none is, or one
// batch fails (to be tried again on the next round).
func (s *Server) embedMissing(ctx context.Context) {
	for ctx.Err() == nil {
		cur, err := s.embedding.Current(ctx)
		if err != nil {
			slog.Warn("embedding not available", "err", err)
			return
		}
		if cur.Embedder == nil {
			return
		}
		batch, err := s.courses.PassagesToEmbed(ctx, cur.Generation, embedBatch)
		if err != nil || len(batch) == 0 {
			if err != nil && ctx.Err() == nil {
				slog.Error("passages to embed not read", "err", err)
			}
			return
		}
		texts := make([]string, len(batch))
		for i, p := range batch {
			texts[i] = p.Body
		}
		vs, used, err := cur.Embedder.Embed(ctx, texts)
		s.recordEmbedding(ctx, cur, used)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("passages not embedded", "err", err)
			}
			return
		}
		if err := s.courses.KeepVectors(context.WithoutCancel(ctx), cur.Generation, batch, vs); err != nil {
			slog.Error("vectors not kept", "err", err)
			return
		}
	}
}

// showEmbedding is the site's embedding settings, and how far each
// library course is embedded.
func (s *Server) showEmbedding(w http.ResponseWriter, r *http.Request) {
	st, err := s.embedding.Settings(r.Context())
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	progress := []course.EmbeddingProgress{}
	if st.Provider != embedding.Off {
		if progress, err = s.courses.EmbeddingProgressOf(r.Context(), st.Generation); err != nil {
			writeInternal(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": st, "courses": progress})
}

// saveExternalEmbedding switches the site to an external API, once it
// embeds a test sentence.
func (s *Server) saveExternalEmbedding(w http.ResponseWriter, r *http.Request) {
	var in embedding.ExternalInput
	if !readJSON(w, r, &in) {
		return
	}
	s.answerEmbedding(w, r, s.embedding.SaveExternal(r.Context(), in))
}

// useLocalEmbedding switches the site to a downloaded local model.
func (s *Server) useLocalEmbedding(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Model string `json:"model"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	s.answerEmbedding(w, r, s.embedding.UseLocal(r.Context(), in.Model))
}

// turnOffEmbedding turns embedding off.
func (s *Server) turnOffEmbedding(w http.ResponseWriter, r *http.Request) {
	s.answerEmbedding(w, r, s.embedding.TurnOff(r.Context()))
}

// downloadEmbeddingModel downloads a local model in the background.
func (s *Server) downloadEmbeddingModel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Model  string `json:"model"`
		Mirror string `json:"mirror"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !writeEmbeddingError(w, r, s.embedding.StartDownload(s.buildCtx, in.Model, in.Mirror)) {
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// answerEmbedding answers a change of the settings, and has the embedder
// catch up with it.
func (s *Server) answerEmbedding(w http.ResponseWriter, r *http.Request, err error) {
	if !writeEmbeddingError(w, r, err) {
		return
	}
	s.embedSoon()
	s.showEmbedding(w, r)
}

// writeEmbeddingError answers the embedding settings' errors and reports
// whether err was nil.
func writeEmbeddingError(w http.ResponseWriter, r *http.Request, err error) bool {
	var failed *embedding.TestFailed
	switch {
	case err == nil:
		return true
	case errors.As(err, &failed):
		writeError(w, http.StatusBadRequest, "embedding_test_failed", "试着嵌入一句话失败了, 设置没有保存: "+failed.Cause.Error())
	case errors.Is(err, embedding.ErrMasterKeyMissing):
		writeError(w, http.StatusConflict, "master_key_missing", "部署没有配置主密钥 (KEINE_MASTER_KEY), 不能保存嵌入 API 的 Key")
	case errors.Is(err, embedding.ErrNotDownloaded):
		writeError(w, http.StatusConflict, "model_not_downloaded", "这个本地模型还没有下载完")
	case errors.Is(err, embedding.ErrUnknownModel):
		writeError(w, http.StatusBadRequest, "unknown_model", "没有这个本地模型")
	case errors.Is(err, embedding.ErrBadSettings):
		writeError(w, http.StatusBadRequest, "bad_request", "填写的内容不合法")
	default:
		writeInternal(w, r, err)
	}
	return false
}
