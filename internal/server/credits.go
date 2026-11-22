package server

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/Bank5y/keine/internal/credits"
)

// writeCreditsError answers a credits failure; it returns true for none.
func writeCreditsError(w http.ResponseWriter, r *http.Request, err error) bool {
	return writeRunCreditsError(w, r, err, "分析")
}

// writeRunCreditsError is writeCreditsError for a run named run (分析,
// 生成报告).
func writeRunCreditsError(w http.ResponseWriter, r *http.Request, err error, run string) bool {
	var short *credits.InsufficientError
	switch {
	case err == nil:
		return true
	case errors.As(err, &short):
		msg := fmt.Sprintf("这次%s预计用 %s 积分，今天还剩 %s 积分。积分在每天 0 点重置。", run, creditText(short.Estimate), creditText(short.Remaining))
		if short.Remaining <= 0 {
			msg = "今天的积分已经用完。积分在每天 0 点重置。"
		}
		writeJSON(w, http.StatusPaymentRequired, map[string]any{"error": apiError{Code: "credits_insufficient", Message: msg, Credits: short}})
	case errors.Is(err, credits.ErrPlanNotFound):
		writeError(w, http.StatusNotFound, "plan_not_found", "套餐不存在")
	case errors.Is(err, credits.ErrUserNotFound):
		writeError(w, http.StatusNotFound, "user_not_found", "账号不存在")
	case errors.Is(err, credits.ErrInvalidPlan):
		writeError(w, http.StatusBadRequest, "invalid_plan", "套餐需要名称（不超过 40 个字），每日额度不能为负")
	case errors.Is(err, credits.ErrNameTaken):
		writeError(w, http.StatusConflict, "plan_name_taken", "已经有同名的套餐")
	case errors.Is(err, credits.ErrBuiltin):
		writeError(w, http.StatusConflict, "plan_builtin", "内置套餐不能删除")
	case errors.Is(err, credits.ErrDefault):
		writeError(w, http.StatusConflict, "plan_default", "默认套餐不能删除")
	case errors.Is(err, credits.ErrInUse):
		writeError(w, http.StatusConflict, "plan_in_use", "还有用户在这个套餐里，先把他们改到别的套餐")
	case errors.Is(err, credits.ErrBadRange):
		writeError(w, http.StatusBadRequest, "bad_range", "日期范围不正确")
	default:
		writeInternal(w, r, err)
	}
	return false
}

// creditText is an amount of credits as the reader reads it: to two
// places at most.
func creditText(c float64) string {
	return strconv.FormatFloat(math.Round(c*100)/100, 'f', -1, 64)
}

func planParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "plan_not_found", "套餐不存在")
		return 0, false
	}
	return id, true
}

func (s *Server) listPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := s.credits.Plans(r.Context())
	if !writeCreditsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": plans})
}

func (s *Server) createPlan(w http.ResponseWriter, r *http.Request) {
	var in credits.PlanInput
	if !readJSON(w, r, &in) {
		return
	}
	p, err := s.credits.CreatePlan(r.Context(), in)
	if !writeCreditsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) updatePlan(w http.ResponseWriter, r *http.Request) {
	id, ok := planParam(w, r)
	if !ok {
		return
	}
	var in credits.PlanInput
	if !readJSON(w, r, &in) {
		return
	}
	p, err := s.credits.UpdatePlan(r.Context(), id, in)
	if !writeCreditsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deletePlan(w http.ResponseWriter, r *http.Request) {
	id, ok := planParam(w, r)
	if !ok {
		return
	}
	if !writeCreditsError(w, r, s.credits.DeletePlan(r.Context(), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setDefaultPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := planParam(w, r)
	if !ok {
		return
	}
	if !writeCreditsError(w, r, s.credits.SetDefault(r.Context(), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// usageRange is the range a usage report asks for (?from=&to=, dates).
func (s *Server) usageRange(w http.ResponseWriter, r *http.Request) (credits.Range, bool) {
	rng, err := s.credits.ParseRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	return rng, writeCreditsError(w, r, err)
}

func dates(rng credits.Range) map[string]string {
	return map[string]string{"from": rng.From.Format("2006-01-02"), "to": rng.To.Format("2006-01-02")}
}

// siteUsage is everyone's usage in a range, in all and day by day.
func (s *Server) siteUsage(w http.ResponseWriter, r *http.Request) {
	rng, ok := s.usageRange(w, r)
	if !ok {
		return
	}
	total, days, err := s.credits.Usage(r.Context(), rng, 0)
	if !writeCreditsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"range": dates(rng), "total": total, "days": days})
}

// readersUsage is each reader's usage in a range.
func (s *Server) readersUsage(w http.ResponseWriter, r *http.Request) {
	rng, ok := s.usageRange(w, r)
	if !ok {
		return
	}
	users, err := s.credits.ByReader(r.Context(), rng)
	if !writeCreditsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"range": dates(rng), "users": users})
}

// readerUsage is one reader's usage in a range: their credits today, in
// all and day by day, and their latest runs.
func (s *Server) readerUsage(w http.ResponseWriter, r *http.Request) {
	id, ok := userIDParam(w, r)
	if !ok {
		return
	}
	rng, ok := s.usageRange(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	u, err := s.auth.User(ctx, id)
	if !writeAccountError(w, r, err) {
		return
	}
	account, err := s.credits.Account(ctx, id)
	if !writeCreditsError(w, r, err) {
		return
	}
	total, days, err := s.credits.Usage(ctx, rng, id)
	if !writeCreditsError(w, r, err) {
		return
	}
	runs, err := s.credits.Runs(ctx, rng, id)
	if !writeCreditsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"range": dates(rng), "user": u, "credits": account, "total": total, "days": days, "runs": runs,
	})
}
