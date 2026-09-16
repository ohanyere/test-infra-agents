package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/go-chi/chi/v5"
)

type IdentityService interface {
	GetOrganization(context.Context, string) (identity.Organization, error)
	GetWorkspace(context.Context, string) (identity.Workspace, error)
	ListMembers(context.Context, string, identity.Pagination) ([]identity.Membership, error)
}
type WorkspaceAuthorization interface {
	CanViewWorkspace(context.Context, auth.Principal, string) error
	CanViewOrganization(context.Context, auth.Principal, string) error
	ListOrganizations(context.Context, auth.Principal, identity.Pagination) ([]identity.Organization, error)
	ListWorkspaces(context.Context, auth.Principal, string, identity.Pagination) ([]identity.Workspace, error)
	AddMember(context.Context, auth.Principal, string, string, string) (identity.Membership, error)
	ChangeMemberRole(context.Context, auth.Principal, string, string, string) (identity.Membership, error)
	RemoveMember(context.Context, auth.Principal, string, string) error
	UpdateWorkspaceName(context.Context, auth.Principal, string, string) (identity.Workspace, error)
}

type identityHandlers struct {
	service       IdentityService
	authorization WorkspaceAuthorization
	logger        *slog.Logger
}

type organizationRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}
type workspaceRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}
type userRequest struct {
	Email    string `json:"email"`
	FullName string `json:"full_name"`
}
type membershipRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}
type roleRequest struct {
	Role string `json:"role"`
}
type workspaceNameRequest struct {
	Name string `json:"name"`
}
type bootstrapRequest struct {
	Organization organizationRequest `json:"organization"`
	Workspace    workspaceRequest    `json:"workspace"`
	User         userRequest         `json:"user"`
}
type listResponse[T any] struct {
	Items      []T                `json:"items"`
	Pagination paginationResponse `json:"pagination"`
}

func (h identityHandlers) getOrganization(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "organizationID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	if err := h.authorization.CanViewOrganization(r.Context(), p, id); err != nil {
		writeAuthError(w, err)
		return
	}
	value, err := h.service.GetOrganization(r.Context(), id)
	if h.handleError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h identityHandlers) listOrganizations(w http.ResponseWriter, r *http.Request) {
	p, ok := parsePagination(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	items, err := h.authorization.ListOrganizations(r.Context(), principal, p)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse[identity.Organization]{Items: items, Pagination: paginationResponse{Limit: p.Limit, Offset: p.Offset}})
}
func (h identityHandlers) getWorkspace(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	if err := h.authorization.CanViewWorkspace(r.Context(), p, id); err != nil {
		writeAuthError(w, err)
		return
	}
	value, err := h.service.GetWorkspace(r.Context(), id)
	if h.handleError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h identityHandlers) updateWorkspace(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	var request workspaceNameRequest
	if !h.decode(w, r, &request) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.authorization.UpdateWorkspaceName(r.Context(), p, id, request.Name)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h identityHandlers) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "organizationID")
	if !ok {
		return
	}
	p, ok := parsePagination(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	items, err := h.authorization.ListWorkspaces(r.Context(), principal, id, p)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse[identity.Workspace]{Items: items, Pagination: paginationResponse{Limit: p.Limit, Offset: p.Offset}})
}
func (h identityHandlers) createMembership(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	var request membershipRequest
	if !h.decode(w, r, &request) {
		return
	}
	if err := identity.ValidateUUID(request.UserID); err != nil {
		writeDomainError(w, err)
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.authorization.AddMember(r.Context(), p, workspaceID, request.UserID, request.Role)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}
func (h identityHandlers) listMembers(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	p, ok := parsePagination(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	if err := h.authorization.CanViewWorkspace(r.Context(), principal, workspaceID); err != nil {
		writeAuthError(w, err)
		return
	}
	items, err := h.service.ListMembers(r.Context(), workspaceID, p)
	if h.handleError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, listResponse[identity.Membership]{Items: items, Pagination: paginationResponse{Limit: p.Limit, Offset: p.Offset}})
}
func (h identityHandlers) updateMembership(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	userID, ok := pathUUID(w, r, "userID")
	if !ok {
		return
	}
	var request roleRequest
	if !h.decode(w, r, &request) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	value, err := h.authorization.ChangeMemberRole(r.Context(), p, workspaceID, userID, request.Role)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h identityHandlers) deleteMembership(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := pathUUID(w, r, "workspaceID")
	if !ok {
		return
	}
	userID, ok := pathUUID(w, r, "userID")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	if err := h.authorization.RemoveMember(r.Context(), p, workspaceID, userID); err != nil {
		writeAuthError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h identityHandlers) decode(w http.ResponseWriter, r *http.Request, value any) bool {
	if err := decodeJSON(w, r, value); err != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return false
	}
	return true
}
func (h identityHandlers) handleError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if !errors.Is(err, identity.ErrValidation) && !errors.Is(err, identity.ErrNotFound) && !errors.Is(err, identity.ErrConflict) {
		h.logger.Error("business request failed", "error", err)
	}
	writeDomainError(w, err)
	return true
}
func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrValidation):
		writeAPIError(w, http.StatusBadRequest, "validation_error", strings.TrimPrefix(err.Error(), identity.ErrValidation.Error()+": "))
	case errors.Is(err, identity.ErrNotFound):
		writeAPIError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, identity.ErrConflict):
		writeAPIError(w, http.StatusConflict, "conflict", "resource already exists")
	default:
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
}
func pathUUID(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	value := chi.URLParam(r, name)
	if err := identity.ValidateUUID(value); err != nil {
		writeDomainError(w, err)
		return "", false
	}
	return value, true
}
func parsePagination(w http.ResponseWriter, r *http.Request) (identity.Pagination, bool) {
	p := identity.Pagination{Limit: 50}
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		p.Limit, err = strconv.Atoi(raw)
		if err != nil || p.Limit < 1 || p.Limit > 100 {
			writeAPIError(w, http.StatusBadRequest, "validation_error", "limit must be an integer between 1 and 100")
			return p, false
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		p.Offset, err = strconv.Atoi(raw)
		if err != nil || p.Offset < 0 {
			writeAPIError(w, http.StatusBadRequest, "validation_error", "offset must be a non-negative integer")
			return p, false
		}
	}
	return p, true
}
