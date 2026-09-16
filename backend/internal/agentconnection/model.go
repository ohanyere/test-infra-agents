package agentconnection

import "time"

const (
	AuthNone   = "none"
	AuthBearer = "bearer"

	StatusUntested             = "untested"
	StatusHealthy              = "healthy"
	StatusUnreachable          = "unreachable"
	StatusAuthenticationFailed = "authentication_failed"
	StatusProtocolError        = "protocol_error"
	StatusTimeout              = "timeout"
	StatusConfigurationError   = "configuration_error"
)

type Info struct {
	AgentVersionID      string     `json:"agent_version_id"`
	Protocol            string     `json:"protocol"`
	EndpointURL         string     `json:"endpoint_url"`
	AuthType            string     `json:"auth_type"`
	HasCredential       bool       `json:"has_credential"`
	CredentialUpdatedAt *time.Time `json:"credential_updated_at,omitempty"`
	LastCheckedAt       *time.Time `json:"last_checked_at,omitempty"`
	LastCheckStatus     string     `json:"last_check_status"`
	LastDiagnosticCode  *string    `json:"last_diagnostic_code,omitempty"`
}

type CredentialInput struct {
	AuthType    string
	BearerToken string
}

type TestConfigurationInput struct {
	EndpointURL string
	AuthType    string
	BearerToken string
}

type TestResult struct {
	Status         string `json:"status"`
	DiagnosticCode string `json:"diagnostic_code"`
}

type Compatibility struct {
	Compatible           bool     `json:"compatible"`
	DeclaredCapabilities []string `json:"declared_capabilities"`
	RequiredCapabilities []string `json:"required_capabilities"`
	MissingCapabilities  []string `json:"missing_capabilities"`
}

type storedConnection struct {
	Info
	CredentialEnvelope []byte
}
