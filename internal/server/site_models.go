package server

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/Bank5y/keine/internal/assistant"
)

// testFailureMessages say what a failed connection test means, by category
// only: the provider's own answer may echo the key and never leaves the
// server.
var testFailureMessages = map[assistant.Category]string{
	assistant.ErrAuth:           "认证失败, 检查 key 是否有效、是否有这个模型的权限",
	assistant.ErrNotFound:       "地址或模型名不对, 端点找不到它",
	assistant.ErrRateLimited:    "被限流了, 稍后再试",
	assistant.ErrQuotaExhausted: "账户余额或额度不足",
	assistant.ErrUnavailable:    "连不上端点, 或端点暂时不可用",
	assistant.ErrTimeout:        "端点太久没有回应",
	assistant.ErrInvalidRequest: "端点拒绝了请求, 可能不支持流式或工具调用",
	assistant.ErrBadResponse:    "端点的回应不是能读懂的流式格式",
}

func (s *Server) modelsState(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	ctx := r.Context()
	models, err := s.models.List(ctx)
	if err != nil {
		writeInternal(w, r, err)
		return nil, false
	}
	turns, err := s.models.Turns(ctx)
	if err != nil {
		writeInternal(w, r, err)
		return nil, false
	}
	out := map[string]any{}
	for _, k := range assistant.TurnKinds {
		if t, ok := turns[k]; ok {
			out[string(k)] = t
		} else {
			out[string(k)] = nil
		}
	}
	return map[string]any{
		"models":      models,
		"turns":       out,
		"aiAvailable": len(turns) == len(assistant.TurnKinds),
		"masterKey":   s.models.CanStoreKeys(),
		"providers":   assistant.ProviderPresets,
	}, true
}

func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	if st, ok := s.modelsState(w, r); ok {
		writeJSON(w, http.StatusOK, st)
	}
}

func (s *Server) createModel(w http.ResponseWriter, r *http.Request) {
	var in assistant.ModelInput
	if !readJSON(w, r, &in) {
		return
	}
	m, err := s.models.Create(r.Context(), in)
	if !writeModelError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) updateModel(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "model_not_found", "模型不存在")
	if !ok {
		return
	}
	var in assistant.ModelInput
	if !readJSON(w, r, &in) {
		return
	}
	m, err := s.models.Update(r.Context(), id, in)
	if !writeModelError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) deleteModel(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "model_not_found", "模型不存在")
	if !ok {
		return
	}
	if !writeModelError(w, r, s.models.Delete(r.Context(), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setTurnModel(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !assistant.ValidTurnKind(kind) {
		writeError(w, http.StatusNotFound, "not_found", "没有这种轮次类型")
		return
	}
	var in struct {
		ModelID  *int64 `json:"modelId"`
		Thinking bool   `json:"thinking"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !writeModelError(w, r, s.models.SetTurn(r.Context(), assistant.TurnKind(kind), in.ModelID, in.Thinking)) {
		return
	}
	s.listModels(w, r)
}

// writeModelError answers a site model error; it reports true when there
// was none, like the other write*Error helpers.
func writeModelError(w http.ResponseWriter, r *http.Request, err error) bool {
	var invalid *assistant.InvalidModelError
	var failed *assistant.TestFailedError
	switch {
	case err == nil:
		return true
	case errors.As(err, &invalid):
		writeProblems(w, http.StatusBadRequest, "invalid_model", "模型配置不完整", invalid.Problems)
	case errors.As(err, &failed):
		slog.Warn("model connection test failed", "err", failed.Cause)
		msg, ok := testFailureMessages[failed.Category]
		if !ok {
			msg = "测试连接没通过"
		}
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": apiError{
			Code: "model_test_failed", Message: "测试连接没通过: " + msg, Category: string(failed.Category),
		}})
	case errors.Is(err, assistant.ErrAssistantUnavailable):
		// before ErrKeyUnreadable, which it may wrap: readers learn no more
		slog.Warn("assistant unavailable", "err", err)
		writeError(w, http.StatusConflict, "ai_unavailable", "探索助手暂不可用")
	case errors.Is(err, assistant.ErrMasterKeyMissing):
		writeError(w, http.StatusBadRequest, "master_key_missing", "服务器没有配置加密主密钥 (KEINE_MASTER_KEY), 不能保存 key")
	case errors.Is(err, assistant.ErrKeyUnreadable):
		writeError(w, http.StatusConflict, "key_unreadable", "已保存的 key 无法用当前主密钥解开, 请重新填写 key")
	case errors.Is(err, assistant.ErrModelChanged):
		writeError(w, http.StatusConflict, "model_changed", "测试期间模型的 key 被改过了, 请刷新后再保存")
	case errors.Is(err, assistant.ErrModelNotFound):
		writeError(w, http.StatusNotFound, "model_not_found", "模型不存在")
	case errors.Is(err, assistant.ErrModelUnrated):
		writeError(w, http.StatusConflict, "model_unrated", "没配费率的模型不能用于轮次类型")
	case errors.Is(err, assistant.ErrModelInUse):
		writeError(w, http.StatusConflict, "model_in_use", "这个模型正用于轮次类型, 先换掉再删除")
	default:
		writeInternal(w, r, err)
	}
	return false
}
