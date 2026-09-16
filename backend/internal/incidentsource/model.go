package incidentsource

import (
	"context"
	"encoding/json"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/scenarioauthoring"
)

const (
	ProviderPagerDuty = "pagerduty"
	ProviderDatadog   = "datadog"

	PagerDutyContract = "pagerduty-incidents:v1"
	DatadogContract   = "datadog-incidents:v1"

	StatusUntested             = "untested"
	StatusHealthy              = "healthy"
	StatusAuthenticationFailed = "authentication_failed"
	StatusPermissionDenied     = "permission_denied"
	StatusUnreachable          = "unreachable"
	StatusRateLimited          = "rate_limited"
	StatusProviderError        = "provider_error"
	StatusConfigurationError   = "configuration_error"
	StatusTimeout              = "timeout"

	DefaultPageSize = 20
	MaxPageSize     = 50
)

type Connection struct {
	ID                  string          `json:"id"`
	WorkspaceID         string          `json:"workspace_id"`
	Provider            string          `json:"provider"`
	DisplayName         string          `json:"display_name"`
	ProviderConfig      json.RawMessage `json:"provider_config"`
	AuthConfigured      bool            `json:"auth_configured"`
	Status              string          `json:"status"`
	LastDiagnosticCode  *string         `json:"last_diagnostic_code,omitempty"`
	LastCheckedAt       *time.Time      `json:"last_checked_at,omitempty"`
	CredentialUpdatedAt time.Time       `json:"credential_updated_at"`
	ConnectorContract   string          `json:"connector_contract_version"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

type StoredConnection struct {
	Connection
	CredentialEnvelope []byte
	DisabledAt         *time.Time
}

type CreateInput struct {
	Provider       string
	DisplayName    string
	Site           string
	APIToken       string
	APIKey         string
	ApplicationKey string
}

type CredentialInput struct {
	APIToken       string
	APIKey         string
	ApplicationKey string
}

type Credentials struct {
	APIToken       string `json:"api_token,omitempty"`
	APIKey         string `json:"api_key,omitempty"`
	ApplicationKey string `json:"application_key,omitempty"`
}

type ListFilter struct {
	Query  string
	Limit  int
	Offset int
}

type TimelineEntry struct {
	Timestamp *time.Time `json:"timestamp,omitempty"`
	Type      string     `json:"type,omitempty"`
	Summary   string     `json:"summary,omitempty"`
	Details   string     `json:"details,omitempty"`
}

type NormalizedIncident struct {
	Provider        string            `json:"provider"`
	ExternalID      string            `json:"external_id"`
	Title           string            `json:"title"`
	Summary         string            `json:"summary,omitempty"`
	Status          string            `json:"status,omitempty"`
	Severity        string            `json:"severity,omitempty"`
	StartedAt       *time.Time        `json:"started_at,omitempty"`
	ResolvedAt      *time.Time        `json:"resolved_at,omitempty"`
	UpdatedAt       *time.Time        `json:"updated_at,omitempty"`
	Services        []string          `json:"services"`
	Teams           []string          `json:"teams"`
	Timeline        []TimelineEntry   `json:"timeline"`
	Metadata        map[string]string `json:"metadata"`
	SourceReference string            `json:"source_reference,omitempty"`
}

type IncidentPage struct {
	Items      []NormalizedIncident `json:"items"`
	Limit      int                  `json:"limit"`
	Offset     int                  `json:"offset"`
	NextOffset *int                 `json:"next_offset,omitempty"`
}

type Connector interface {
	TestConnection(context.Context) error
	ListIncidents(context.Context, ListFilter) (IncidentPage, error)
	GetIncident(context.Context, string) (NormalizedIncident, error)
}

type Factory interface {
	Connector(provider string, config json.RawMessage, credentials Credentials) (Connector, error)
}

type ScenarioAuthoring interface {
	ImportIncident(context.Context, auth.Principal, string, scenarioauthoring.IncidentInput) (scenarioauthoring.DraftResult, error)
}

type ImportResult struct {
	Incident NormalizedIncident            `json:"incident"`
	Draft    scenarioauthoring.DraftResult `json:"draft_result"`
}
