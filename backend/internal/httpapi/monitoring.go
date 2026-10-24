package httpapi

import (
	"context"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/operational"
)

type monitoringService interface {
	ConfigureMonitoring(context.Context, auth.Principal, string, string, string, operational.MonitoringInput) (operational.MonitoringPlan, error)
	Monitoring(context.Context, auth.Principal, string, string, string) (operational.MonitoringView, error)
	ChangeMonitoring(context.Context, auth.Principal, string, string, string, string) (operational.MonitoringPlan, error)
}

type monitoringHandlers struct {
	service monitoringService
	operationalHandlers
}

func (h monitoringHandlers) scope(w http.ResponseWriter, r *http.Request) (string, string, string, bool) {
	workspace, environment, ok := h.ids(w, r)
	if !ok {
		return "", "", "", false
	}
	session, ok := pathUUID(w, r, "sessionID")
	return workspace, environment, session, ok
}
func (h monitoringHandlers) configure(w http.ResponseWriter, r *http.Request) {
	workspace, environment, session, ok := h.scope(w, r)
	if !ok {
		return
	}
	var in operational.MonitoringInput
	if !decodeLimited(w, r, &in, 8<<10) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ConfigureMonitoring(r.Context(), p, workspace, environment, session, in)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (h monitoringHandlers) get(w http.ResponseWriter, r *http.Request) {
	workspace, environment, session, ok := h.scope(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.Monitoring(r.Context(), p, workspace, environment, session)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (h monitoringHandlers) action(w http.ResponseWriter, r *http.Request) {
	workspace, environment, session, ok := h.scope(w, r)
	if !ok {
		return
	}
	var in struct {
		Action string `json:"action"`
	}
	if !decodeLimited(w, r, &in, 1024) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	out, err := h.service.ChangeMonitoring(r.Context(), p, workspace, environment, session, in.Action)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, out)
}
