package httpapi

import (
	"log/slog"
	"net/http"
	"sort"

	"github.com/evalplane/evalplane/backend/internal/gitopsqual"
	"github.com/evalplane/evalplane/backend/internal/srequal"
	"github.com/go-chi/chi/v5"
)

// qualificationTasksHandlers serves the built-in qualification catalog
// reads: the Kubernetes/SRE catalog (srequal) plus the GitOps and
// progressive-delivery catalog (gitopsqual). The projection is agent-safe by
// construction (both SafeViews carry no ground truth, reference remediation,
// hidden variants, or certification internals), and availability is derived
// from environment scheduler truth per task, never from a separate hardcoded
// flag. There is exactly one task API: no second GitOps task API exists.
type qualificationTasksHandlers struct {
	logger *slog.Logger
}

// gitopsSafeView adapts the GitOps/delivery safe projection onto the shared
// qualification-task response shape. Field-by-field mapping keeps the API
// response type stable; no evaluator-only data crosses the boundary (the
// source SafeView carries none by construction).
func gitopsSafeView(view gitopsqual.SafeView) srequal.SafeView {
	return srequal.SafeView{
		ID: view.ID, Name: view.Name, Description: view.Description,
		Domain: view.Domain, TaskFamily: view.TaskFamily, CaseType: view.CaseType,
		Difficulty: view.Difficulty, Duration: view.Duration,
		Capabilities:       append([]string(nil), view.Capabilities...),
		RequiredComponents: append([]string(nil), view.RequiredComponents...),
		MinimumIsolation:   view.MinimumIsolation, RuntimeContract: view.RuntimeContract,
		Runnable: view.Runnable, EligibleProfile: view.EligibleProfile,
		SchedulerReason: view.SchedulerReason, CertificationNote: view.CertificationNote,
	}
}

func (h qualificationTasksHandlers) listTasks(w http.ResponseWriter, r *http.Request) {
	items := make([]srequal.SafeView, 0, len(srequal.List())+len(gitopsqual.List()))
	for _, task := range srequal.List() {
		items = append(items, task.Safe())
	}
	for _, task := range gitopsqual.List() {
		items = append(items, gitopsSafeView(task.Safe()))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	writeJSON(w, http.StatusOK, listResponse[srequal.SafeView]{Items: items, Pagination: paginationResponse{Limit: len(items), Offset: 0}})
}

func (h qualificationTasksHandlers) getTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "taskID")
	if task, ok := srequal.Get(id); ok {
		writeJSON(w, http.StatusOK, task.Safe())
		return
	}
	for _, task := range srequal.PlannedConcepts() {
		if task.ID == id {
			writeJSON(w, http.StatusOK, task.Safe())
			return
		}
	}
	if task, ok := gitopsqual.Get(id); ok {
		writeJSON(w, http.StatusOK, gitopsSafeView(task.Safe()))
		return
	}
	writeAPIError(w, http.StatusNotFound, "not_found", "qualification task not found")
}
