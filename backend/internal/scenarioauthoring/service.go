package scenarioauthoring

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RoleRepository interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}
type Service struct {
	generator ScenarioDraftGenerator
	pool      *pgxpool.Pool
	roles     RoleRepository
	sanitizer Sanitizer
}

func NewService(generator ScenarioDraftGenerator, pool *pgxpool.Pool, roles RoleRepository) *Service {
	return &Service{generator: generator, pool: pool, roles: roles, sanitizer: NewSanitizer()}
}
func (s *Service) authorize(ctx context.Context, principal auth.Principal, workspace string) error {
	role, err := s.roles.GetRole(ctx, s.pool, workspace, principal.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return identity.ErrNotFound
	}
	if err != nil {
		return err
	}
	if role == "viewer" {
		return auth.ErrForbidden
	}
	return nil
}
func (s *Service) Describe(ctx context.Context, principal auth.Principal, workspace string, input DescriptionInput) (DraftResult, error) {
	if err := s.authorize(ctx, principal, workspace); err != nil {
		return DraftResult{}, err
	}
	if s.generator == nil {
		return DraftResult{}, errors.New("scenario AI generator is not configured")
	}
	input.Description = strings.TrimSpace(input.Description)
	if input.Description == "" || len(input.Description) > 20000 {
		return DraftResult{}, errors.New("description must be between 1 and 20000 characters")
	}
	input.Description = s.sanitizer.redactText(input.Description, newRedactionContext())
	draft, err := s.generator.FromDescription(ctx, input)
	if err != nil {
		return DraftResult{}, safeGeneratorError(err)
	}
	draft = s.sanitizer.SanitizeDraft(draft, nil)
	draft.SourceMetadata = mergeMetadata(draft.SourceMetadata, map[string]any{"authoring_method": "description"})
	draft, err = CompileDraft(draft)
	if err != nil {
		return DraftResult{}, errors.New("generated scenario draft failed validation")
	}
	return DraftResult{Draft: draft}, err
}
func (s *Service) ImportIncident(ctx context.Context, principal auth.Principal, workspace string, input IncidentInput) (DraftResult, error) {
	if err := s.authorize(ctx, principal, workspace); err != nil {
		return DraftResult{}, err
	}
	if s.generator == nil {
		return DraftResult{}, errors.New("scenario AI generator is not configured")
	}
	evidenceTypes := make([]string, 0, len(input.Files))
	for _, file := range input.Files {
		evidenceTypes = append(evidenceTypes, strings.TrimPrefix(strings.ToLower(filepath.Ext(file.Name)), "."))
	}
	provenance := input.Provenance
	normalized, redactions, err := s.sanitizer.SanitizeIncident(input)
	input = IncidentInput{}
	if err != nil {
		return DraftResult{}, err
	}
	draft, err := s.generator.FromIncident(ctx, normalized)
	if err != nil {
		return DraftResult{}, safeGeneratorError(err)
	}
	draft = s.sanitizer.SanitizeDraft(draft, redactions)
	// Raw files are intentionally absent here and are never retained. The
	// provenance records only non-secret parsing metadata.
	metadata := map[string]any{"authoring_method": "incident_import", "file_count": len(evidenceTypes), "evidence_types": evidenceTypes, "redaction_contract": RedactionContract}
	if provenance != nil {
		safeProvenance := map[string]any{
			"source_type":                provenance.SourceType,
			"provider":                   provenance.Provider,
			"external_incident_id":       provenance.ExternalIncidentID,
			"source_connection_id":       provenance.SourceConnectionID,
			"connector_contract_version": provenance.ConnectorContractVersion,
			"imported_at":                provenance.ImportedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			"redaction_contract":         RedactionContract,
		}
		if provenance.SourceIncidentUpdatedAt != nil {
			safeProvenance["source_incident_updated_at"] = provenance.SourceIncidentUpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		metadata["incident_provenance"] = safeProvenance
	}
	draft.SourceMetadata = mergeMetadata(draft.SourceMetadata, metadata)
	draft, err = CompileDraft(draft)
	if err != nil {
		return DraftResult{}, errors.New("generated scenario draft failed validation")
	}
	return DraftResult{Draft: draft, EvidenceSummary: normalized.EvidenceSummary, Facts: normalized.Facts, Inferences: draft.Assumptions, NeedsConfirmation: draft.Warnings}, err
}

func safeGeneratorError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return errors.New("scenario provider request timed out or was cancelled")
	}
	return errors.New("scenario provider request failed")
}

func mergeMetadata(raw json.RawMessage, values map[string]any) json.RawMessage {
	metadata := map[string]any{}
	_ = json.Unmarshal(raw, &metadata)
	for key, value := range values {
		metadata[key] = value
	}
	result, _ := json.Marshal(metadata)
	return result
}
