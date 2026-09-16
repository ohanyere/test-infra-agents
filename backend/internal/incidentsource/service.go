package incidentsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentconnection"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/scenarioauthoring"
	"github.com/jackc/pgx/v5/pgxpool"
)

type roleRepository interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}

type Service struct {
	pool      *pgxpool.Pool
	store     Store
	roles     roleRepository
	cipher    *agentconnection.Cipher
	factory   Factory
	authoring ScenarioAuthoring
	now       func() time.Time
}

func NewService(pool *pgxpool.Pool, store Store, roles roleRepository, cipher *agentconnection.Cipher, factory Factory, authoring ScenarioAuthoring) *Service {
	return &Service{pool: pool, store: store, roles: roles, cipher: cipher, factory: factory, authoring: authoring, now: time.Now}
}

func (s *Service) Create(ctx context.Context, principal auth.Principal, workspaceID string, input CreateInput) (Connection, error) {
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return Connection{}, err
	}
	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.DisplayName == "" || len(input.DisplayName) > 120 {
		return Connection{}, validation("display_name must be between 1 and 120 characters")
	}
	config, credentials, err := connectionDefinition(input)
	if err != nil {
		return Connection{}, err
	}
	if _, err = s.factory.Connector(input.Provider, config, credentials); err != nil {
		return Connection{}, err
	}
	envelope, err := encryptCredentials(s.cipher, credentials)
	clearCredentials(&credentials)
	clearCreateInput(&input)
	if err != nil {
		return Connection{}, errors.New("incident-source credential encryption unavailable")
	}
	return s.store.Create(ctx, workspaceID, input.Provider, input.DisplayName, config, envelope)
}

func (s *Service) List(ctx context.Context, principal auth.Principal, workspaceID string) ([]Connection, error) {
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return nil, err
	}
	return s.store.List(ctx, workspaceID)
}

func (s *Service) Get(ctx context.Context, principal auth.Principal, workspaceID, id string) (Connection, error) {
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return Connection{}, err
	}
	stored, err := s.store.Get(ctx, workspaceID, id)
	return stored.Connection, err
}

func (s *Service) RotateCredentials(ctx context.Context, principal auth.Principal, workspaceID, id string, input CredentialInput) (Connection, error) {
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return Connection{}, err
	}
	stored, err := s.store.Get(ctx, workspaceID, id)
	if err != nil || stored.DisabledAt != nil {
		return Connection{}, errOrNotFound(err)
	}
	credentials, err := validateCredentials(stored.Provider, input.APIToken, input.APIKey, input.ApplicationKey)
	if err != nil {
		return Connection{}, err
	}
	envelope, err := encryptCredentials(s.cipher, credentials)
	clearCredentials(&credentials)
	input = CredentialInput{}
	if err != nil {
		return Connection{}, errors.New("incident-source credential encryption unavailable")
	}
	return s.store.RotateCredentials(ctx, workspaceID, id, envelope)
}

func (s *Service) Test(ctx context.Context, principal auth.Principal, workspaceID, id string) (Connection, error) {
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return Connection{}, err
	}
	stored, connector, err := s.connector(ctx, workspaceID, id)
	if err != nil {
		return Connection{}, err
	}
	callErr := connector.TestConnection(ctx)
	status, code := statusFor(callErr)
	connection, persistErr := s.store.SetCheck(ctx, workspaceID, stored.ID, status, code, s.now().UTC())
	if persistErr != nil {
		return Connection{}, persistErr
	}
	return connection, nil
}

func (s *Service) ListIncidents(ctx context.Context, principal auth.Principal, workspaceID, id string, filter ListFilter) (IncidentPage, error) {
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return IncidentPage{}, err
	}
	filter = boundedFilter(filter)
	_, connector, err := s.connector(ctx, workspaceID, id)
	if err != nil {
		return IncidentPage{}, err
	}
	return connector.ListIncidents(ctx, filter)
}

func (s *Service) GetIncident(ctx context.Context, principal auth.Principal, workspaceID, id, externalID string) (NormalizedIncident, error) {
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return NormalizedIncident{}, err
	}
	if err := validateExternalID(externalID); err != nil {
		return NormalizedIncident{}, err
	}
	_, connector, err := s.connector(ctx, workspaceID, id)
	if err != nil {
		return NormalizedIncident{}, err
	}
	return connector.GetIncident(ctx, externalID)
}

func (s *Service) ImportIncident(ctx context.Context, principal auth.Principal, workspaceID, id, externalID string) (ImportResult, error) {
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return ImportResult{}, err
	}
	if s.authoring == nil {
		return ImportResult{}, errors.New("scenario authoring is unavailable")
	}
	if err := validateExternalID(externalID); err != nil {
		return ImportResult{}, err
	}
	stored, connector, err := s.connector(ctx, workspaceID, id)
	if err != nil {
		return ImportResult{}, err
	}
	incident, err := connector.GetIncident(ctx, externalID)
	if err != nil {
		return ImportResult{}, safeProviderFailure(err)
	}
	input := incidentInput(incident, stored)
	draft, err := s.authoring.ImportIncident(ctx, principal, workspaceID, input)
	if err != nil {
		return ImportResult{}, err
	}
	return ImportResult{Incident: safePreview(incident), Draft: draft}, nil
}

func (s *Service) connector(ctx context.Context, workspaceID, id string) (StoredConnection, Connector, error) {
	stored, err := s.store.Get(ctx, workspaceID, id)
	if err != nil {
		return StoredConnection{}, nil, err
	}
	if stored.DisabledAt != nil {
		return StoredConnection{}, nil, identity.ErrNotFound
	}
	plain, err := s.cipher.Decrypt(stored.CredentialEnvelope)
	if err != nil {
		return StoredConnection{}, nil, errors.New("incident-source credential unavailable")
	}
	var credentials Credentials
	if json.Unmarshal([]byte(plain), &credentials) != nil {
		plain = ""
		return StoredConnection{}, nil, errors.New("incident-source credential unavailable")
	}
	plain = ""
	connector, err := s.factory.Connector(stored.Provider, stored.ProviderConfig, credentials)
	clearCredentials(&credentials)
	if err != nil {
		return StoredConnection{}, nil, err
	}
	return stored, connector, nil
}

func (s *Service) role(ctx context.Context, principal auth.Principal, workspaceID string) (string, error) {
	role, err := s.roles.GetRole(ctx, s.pool, workspaceID, principal.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return "", auth.ErrForbidden
	}
	return role, err
}

func (s *Service) requireWrite(ctx context.Context, principal auth.Principal, workspaceID string) error {
	role, err := s.role(ctx, principal, workspaceID)
	if err != nil {
		return err
	}
	if role == "viewer" {
		return auth.ErrForbidden
	}
	return nil
}

func connectionDefinition(input CreateInput) (json.RawMessage, Credentials, error) {
	credentials, err := validateCredentials(input.Provider, input.APIToken, input.APIKey, input.ApplicationKey)
	if err != nil {
		return nil, Credentials{}, err
	}
	config := map[string]string{}
	if input.Provider == ProviderDatadog {
		if strings.TrimSpace(input.Site) == "" {
			return nil, Credentials{}, validation("site is required for Datadog")
		}
		config["site"] = strings.ToUpper(strings.TrimSpace(input.Site))
	}
	raw, _ := json.Marshal(config)
	return raw, credentials, nil
}

func validateCredentials(provider, apiToken, apiKey, applicationKey string) (Credentials, error) {
	trimmed := Credentials{APIToken: strings.TrimSpace(apiToken), APIKey: strings.TrimSpace(apiKey), ApplicationKey: strings.TrimSpace(applicationKey)}
	switch provider {
	case ProviderPagerDuty:
		if trimmed.APIToken == "" || len(trimmed.APIToken) > 8192 || trimmed.APIKey != "" || trimmed.ApplicationKey != "" {
			return Credentials{}, validation("PagerDuty requires only api_token")
		}
	case ProviderDatadog:
		if trimmed.APIKey == "" || trimmed.ApplicationKey == "" || len(trimmed.APIKey) > 8192 || len(trimmed.ApplicationKey) > 8192 || trimmed.APIToken != "" {
			return Credentials{}, validation("Datadog requires api_key and application_key")
		}
	default:
		return Credentials{}, validation("provider must be pagerduty or datadog")
	}
	return trimmed, nil
}

func encryptCredentials(cipher *agentconnection.Cipher, credentials Credentials) ([]byte, error) {
	raw, err := json.Marshal(credentials)
	if err != nil {
		return nil, err
	}
	envelope, err := cipher.Encrypt(string(raw))
	for index := range raw {
		raw[index] = 0
	}
	return envelope, err
}

func clearCredentials(credentials *Credentials) {
	credentials.APIToken = ""
	credentials.APIKey = ""
	credentials.ApplicationKey = ""
}

func clearCreateInput(input *CreateInput) {
	input.APIToken = ""
	input.APIKey = ""
	input.ApplicationKey = ""
}

func boundedFilter(filter ListFilter) ListFilter {
	filter.Query = strings.TrimSpace(filter.Query)
	if len(filter.Query) > 200 {
		filter.Query = filter.Query[:200]
	}
	if filter.Limit <= 0 {
		filter.Limit = DefaultPageSize
	}
	if filter.Limit > MaxPageSize {
		filter.Limit = MaxPageSize
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}

func validateExternalID(value string) error {
	if value == "" || len(value) > 200 || strings.ContainsAny(value, "/?#\\") {
		return validation("external incident id is invalid")
	}
	return nil
}

func statusFor(err error) (string, string) {
	if err == nil {
		return StatusHealthy, "CONNECTION_HEALTHY"
	}
	var providerError *ProviderError
	if errors.As(err, &providerError) {
		return providerError.Status, providerError.Code
	}
	return StatusProviderError, CodeProviderError
}

func safeProviderFailure(err error) error {
	var providerError *ProviderError
	if errors.As(err, &providerError) {
		return providerError
	}
	return &ProviderError{Code: CodeProviderError, Status: StatusProviderError}
}

func incidentInput(incident NormalizedIncident, connection StoredConnection) scenarioauthoring.IncidentInput {
	facts := []string{}
	addFact := func(name, value string) {
		if value != "" {
			facts = append(facts, name+"="+value)
		}
	}
	addFact("provider", incident.Provider)
	addFact("external_incident_id", incident.ExternalID)
	addFact("status", incident.Status)
	addFact("severity", incident.Severity)
	for _, service := range incident.Services {
		addFact("service", service)
	}
	for _, team := range incident.Teams {
		addFact("team", team)
	}
	if incident.StartedAt != nil {
		addFact("started_at", incident.StartedAt.UTC().Format(time.RFC3339))
	}
	if incident.ResolvedAt != nil {
		addFact("resolved_at", incident.ResolvedAt.UTC().Format(time.RFC3339))
	}
	sort.Strings(facts)
	timeline := make([]string, 0, len(incident.Timeline))
	for _, entry := range incident.Timeline {
		parts := []string{}
		if entry.Timestamp != nil {
			parts = append(parts, entry.Timestamp.UTC().Format(time.RFC3339))
		}
		parts = append(parts, entry.Type, entry.Summary, entry.Details)
		timeline = append(timeline, strings.TrimSpace(strings.Join(parts, " ")))
	}
	return scenarioauthoring.IncidentInput{
		Description:     incident.Title + "\n" + incident.Summary,
		Timeline:        strings.Join(timeline, "\n"),
		Facts:           facts,
		EvidenceSummary: []string{fmt.Sprintf("%s incident %s imported through a read-only connector", incident.Provider, incident.ExternalID)},
		Provenance: &scenarioauthoring.IncidentProvenance{
			SourceType: "connector", Provider: incident.Provider, ExternalIncidentID: incident.ExternalID,
			SourceConnectionID: connection.ID, SourceIncidentUpdatedAt: incident.UpdatedAt, ImportedAt: sTimeUTC(),
			ConnectorContractVersion: connection.ConnectorContract, RedactionContract: scenarioauthoring.RedactionContract,
		},
	}
}

var timeNow = time.Now

func sTimeUTC() time.Time { return timeNow().UTC() }

func safePreview(incident NormalizedIncident) NormalizedIncident {
	// The scenario-authoring sanitizer is authoritative for provider content.
	// Preview fields are redacted separately before they are returned.
	input := scenarioauthoring.IncidentInput{Description: incident.Title + "\n" + incident.Summary, Timeline: timelineText(incident.Timeline)}
	safe, err := scenarioauthoring.NormalizeIncident(input)
	if err == nil {
		lines := strings.SplitN(safe.Description, "\n", 2)
		incident.Title = lines[0]
		if len(lines) == 2 {
			incident.Summary = lines[1]
		} else {
			incident.Summary = ""
		}
		incident.Timeline = []TimelineEntry{{Summary: safe.Timeline}}
	} else {
		incident.Title = scenarioauthoring.RedactSecrets(incident.Title)
		incident.Summary = scenarioauthoring.RedactSecrets(incident.Summary)
		incident.Timeline = []TimelineEntry{{Summary: scenarioauthoring.RedactSecrets(timelineText(incident.Timeline))}}
	}
	for i := range incident.Services {
		incident.Services[i] = scenarioauthoring.RedactSecrets(incident.Services[i])
	}
	for i := range incident.Teams {
		incident.Teams[i] = scenarioauthoring.RedactSecrets(incident.Teams[i])
	}
	redactedMetadata := make(map[string]string, len(incident.Metadata))
	for key, value := range incident.Metadata {
		redactedMetadata[scenarioauthoring.RedactSecrets(key)] = scenarioauthoring.RedactSecrets(value)
	}
	incident.Metadata = redactedMetadata
	incident.SourceReference = scenarioauthoring.RedactSecrets(incident.SourceReference)
	return incident
}

func timelineText(entries []TimelineEntry) string {
	values := make([]string, 0, len(entries))
	for _, entry := range entries {
		values = append(values, strings.TrimSpace(entry.Type+" "+entry.Summary+" "+entry.Details))
	}
	return strings.Join(values, "\n")
}

func validation(message string) error {
	return errors.Join(identity.ErrValidation, errors.New(message))
}

func errOrNotFound(err error) error {
	if err != nil {
		return err
	}
	return identity.ErrNotFound
}
