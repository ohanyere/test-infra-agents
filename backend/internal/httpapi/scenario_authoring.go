package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/scenarioauthoring"
)

type ScenarioAuthoringService interface {
	Describe(context.Context, auth.Principal, string, scenarioauthoring.DescriptionInput) (scenarioauthoring.DraftResult, error)
	ImportIncident(context.Context, auth.Principal, string, scenarioauthoring.IncidentInput) (scenarioauthoring.DraftResult, error)
}

type scenarioAuthoringHandlers struct {
	service ScenarioAuthoringService
	logger  *slog.Logger
}

func (h scenarioAuthoringHandlers) describe(w http.ResponseWriter, r *http.Request) {
	workspace, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	var input scenarioauthoring.DescriptionInput
	if decodeJSON(w, r, &input) != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain a valid description")
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	result, err := h.service.Describe(r.Context(), principal, workspace, input)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h scenarioAuthoringHandlers) importIncident(w http.ResponseWriter, r *http.Request) {
	workspace, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, scenarioauthoring.MaxTotalSize+(1<<20))
	if err := r.ParseMultipartForm(scenarioauthoring.MaxTotalSize); err != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "invalid or oversized multipart incident request")
		return
	}
	defer r.MultipartForm.RemoveAll()
	input := scenarioauthoring.IncidentInput{Description: r.FormValue("description"), Timeline: r.FormValue("timeline")}
	headers := r.MultipartForm.File["files"]
	if len(headers) > scenarioauthoring.MaxFiles {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "at most 10 files are allowed")
		return
	}
	for _, header := range headers {
		file, err := header.Open()
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "validation_error", "incident file could not be read")
			return
		}
		content, readErr := io.ReadAll(io.LimitReader(file, scenarioauthoring.MaxFileSize+1))
		_ = file.Close()
		if readErr != nil || len(content) > scenarioauthoring.MaxFileSize {
			writeAPIError(w, http.StatusBadRequest, "validation_error", "incident file is invalid or exceeds 5 MiB")
			return
		}
		input.Files = append(input.Files, scenarioauthoring.EvidenceFile{Name: header.Filename, Content: content})
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	result, err := h.service.ImportIncident(r.Context(), principal, workspace, input)
	if h.handle(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h scenarioAuthoringHandlers) handle(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
		writeAuthError(w, err)
		return true
	}
	if errors.Is(err, identity.ErrNotFound) {
		writeDomainError(w, err)
		return true
	}
	h.logger.Error("scenario authoring request failed", "error", err)
	writeAPIError(w, http.StatusBadRequest, "validation_error", err.Error())
	return true
}
