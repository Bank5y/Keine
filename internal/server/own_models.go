package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/Bank5y/keine/internal/assistant"
)

// writeOwnModelError answers an own model error; it returns true for none.
func writeOwnModelError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, assistant.ErrOwnModelsClosed):
		writeError(w, http.StatusNotFound, "own_models_disabled", "这个站点没有开放自带模型。")
	case errors.Is(err, assistant.ErrOwnModelNotFound):
		writeError(w, http.StatusNotFound, "own_model_not_found", "你还没有配置自带模型。")
	case errors.Is(err, assistant.ErrOwnTavilyKeyMissing):
		writeError(w, http.StatusBadRequest, "tavily_key_empty", "选 Tavily 搜索要填你自己的 Tavily key。")
	case errors.Is(err, assistant.ErrOwnKeyUnreadable):
		writeError(w, http.StatusConflict, "own_key_unreadable", "你的自带模型的 key 无法用服务器当前的主密钥解开, 请在「我的」页面重新填写 key。")
	default:
		return writeModelError(w, r, err)
	}
	return false
}

func (s *Server) getOwnModel(w http.ResponseWriter, r *http.Request) {
	m, err := s.own.Get(r.Context(), currentUser(r).ID)
	if !writeOwnModelError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// saveOwnModel tests the reader's own model and saves it (ADR 0010).
func (s *Server) saveOwnModel(w http.ResponseWriter, r *http.Request) {
	if !writeOwnModelError(w, r, s.ownOpen()) {
		return
	}
	var in assistant.OwnModelInput
	if !readJSON(w, r, &in) {
		return
	}
	m, err := s.own.Save(r.Context(), currentUser(r).ID, in)
	if !writeOwnModelError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// deleteOwnModel deletes the reader's own model, which turns it off too.
func (s *Server) deleteOwnModel(w http.ResponseWriter, r *http.Request) {
	if !writeOwnModelError(w, r, s.own.Delete(r.Context(), currentUser(r).ID)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// useOwnModel switches the reader's runs to their own model or back.
func (s *Server) useOwnModel(w http.ResponseWriter, r *http.Request) {
	if !writeOwnModelError(w, r, s.ownOpen()) {
		return
	}
	var in struct {
		On bool `json:"on"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	m, err := s.own.Use(r.Context(), currentUser(r).ID, in.On)
	if !writeOwnModelError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) ownOpen() error {
	if !s.own.Open() {
		return assistant.ErrOwnModelsClosed
	}
	return nil
}

// readerTurn is the model a reader's run of the kind takes, read as it
// starts: their own model when they switched to it (ADR 0010), else the
// site's for the kind.
func (s *Server) readerTurn(ctx context.Context, user int64, kind assistant.TurnKind) (assistant.Turn, error) {
	t, own, err := s.own.ForReader(ctx, user)
	if err != nil || own {
		return t, err
	}
	return s.models.ForTurn(ctx, kind)
}

// ownDetail adds to the error event of a run on the reader's own model
// what kind of failure it was (only the category leaves the server), so
// they can fix their settings.
func ownDetail(ev map[string]any, turn assistant.Turn, err error) {
	var re *assistant.RunError
	if !turn.Own || !errors.As(err, &re) {
		return
	}
	c := assistant.CategoryOf(re.Cause)
	if c == "" {
		return
	}
	ev["category"] = c
	if why, ok := testFailureMessages[c]; ok && ev["code"] == assistant.CodeOwnModelError {
		ev["message"] = "你的自带模型出错了（" + why + "），没有改用站点的模型。"
	}
}

// ownFailure is the code a run ends with: on the reader's own model, a
// provider failure is their model's (own_model_error), never told as the
// site's, and the run never falls back to the site's model.
func ownFailure(t assistant.Turn, code string) string {
	if t.Own && (code == assistant.CodeProviderError || code == assistant.CodeRateLimited) {
		return assistant.CodeOwnModelError
	}
	return code
}
