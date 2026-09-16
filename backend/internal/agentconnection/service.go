package agentconnection

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/agentcapability"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type roleRepository interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}

type Service struct {
	pool    *pgxpool.Pool
	repo    *Repository
	roles   roleRepository
	cipher  *Cipher
	policy  agentgateway.EndpointPolicy
	timeout time.Duration
	now     func() time.Time
}

func NewService(pool *pgxpool.Pool, repo *Repository, roles roleRepository, cipher *Cipher, policy agentgateway.EndpointPolicy, timeout time.Duration) *Service {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Service{pool: pool, repo: repo, roles: roles, cipher: cipher, policy: policy, timeout: timeout, now: time.Now}
}

func (s *Service) Get(ctx context.Context, p auth.Principal, workspaceID, versionID string) (Info, error) {
	if _, err := s.role(ctx, p, workspaceID); err != nil {
		return Info{}, err
	}
	value, err := s.repo.Get(ctx, s.pool, workspaceID, versionID)
	return value.Info, err
}

func (s *Service) UpdateCredential(ctx context.Context, p auth.Principal, workspaceID, versionID string, in CredentialInput) (Info, error) {
	if err := s.write(ctx, p, workspaceID); err != nil {
		return Info{}, err
	}
	in.AuthType = strings.TrimSpace(in.AuthType)
	var envelope []byte
	switch in.AuthType {
	case AuthNone:
		if in.BearerToken != "" {
			return Info{}, validation("bearer_token must be empty when auth_type is none")
		}
	case AuthBearer:
		if strings.TrimSpace(in.BearerToken) == "" || len(in.BearerToken) > 8192 {
			return Info{}, validation("bearer_token is required and must be at most 8192 bytes")
		}
		var err error
		envelope, err = s.cipher.Encrypt(in.BearerToken)
		if err != nil {
			return Info{}, errors.New("credential encryption unavailable")
		}
	default:
		return Info{}, validation("auth_type must be none or bearer")
	}
	info, err := s.repo.UpsertCredential(ctx, s.pool, workspaceID, versionID, in.AuthType, envelope)
	if err != nil {
		return Info{}, err
	}
	stored, err := s.repo.Get(ctx, s.pool, workspaceID, versionID)
	if err != nil {
		return Info{}, err
	}
	info.Protocol, info.EndpointURL = stored.Protocol, stored.EndpointURL
	return info, nil
}

func (s *Service) Test(ctx context.Context, p auth.Principal, workspaceID, versionID string) (Info, error) {
	if err := s.write(ctx, p, workspaceID); err != nil {
		return Info{}, err
	}
	stored, err := s.repo.Get(ctx, s.pool, workspaceID, versionID)
	if err != nil {
		return Info{}, err
	}
	status, code := s.probe(ctx, stored)
	return s.repo.SetCheck(ctx, s.pool, workspaceID, versionID, status, code, s.now().UTC())
}

func (s *Service) TestConfiguration(ctx context.Context, p auth.Principal, workspaceID string, in TestConfigurationInput) (TestResult, error) {
	if err := s.write(ctx, p, workspaceID); err != nil {
		return TestResult{}, err
	}
	in.EndpointURL = strings.TrimSpace(in.EndpointURL)
	if _, err := agentgateway.ValidateEndpoint(in.EndpointURL); err != nil {
		return TestResult{}, validation("endpoint_url is invalid")
	}
	stored := storedConnection{Info: Info{AgentVersionID: uuid.NewString(), Protocol: "http", EndpointURL: in.EndpointURL, AuthType: in.AuthType}}
	switch in.AuthType {
	case AuthNone:
		if in.BearerToken != "" {
			return TestResult{}, validation("bearer_token must be empty when auth_type is none")
		}
	case AuthBearer:
		if strings.TrimSpace(in.BearerToken) == "" || len(in.BearerToken) > 8192 {
			return TestResult{}, validation("bearer_token is required")
		}
		var err error
		stored.CredentialEnvelope, err = s.cipher.Encrypt(in.BearerToken)
		if err != nil {
			return TestResult{}, errors.New("credential encryption unavailable")
		}
		stored.HasCredential = true
	default:
		return TestResult{}, validation("auth_type must be none or bearer")
	}
	status, code := s.probe(ctx, stored)
	in.BearerToken = ""
	return TestResult{Status: status, DiagnosticCode: code}, nil
}

func (s *Service) probe(ctx context.Context, stored storedConnection) (string, string) {
	if stored.Protocol != "http" || stored.EndpointURL == "" {
		return StatusConfigurationError, "unsupported_or_missing_http_endpoint"
	}
	if err := agentgateway.ValidateEndpointPolicy(ctx, stored.EndpointURL, s.policy); err != nil {
		return StatusConfigurationError, "endpoint_blocked"
	}
	var token string
	if stored.AuthType == AuthBearer {
		if !stored.HasCredential {
			return StatusConfigurationError, "credential_missing"
		}
		var err error
		token, err = s.cipher.Decrypt(stored.CredentialEnvelope)
		if err != nil {
			return StatusConfigurationError, "credential_unavailable"
		}
	}
	defer func() { token = "" }()
	checkCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	request := agentgateway.Request{ProtocolVersion: agentgateway.ProtocolV1, RunID: stored.AgentVersionID, AgentVersionID: stored.AgentVersionID,
		Task:             agentgateway.AgentTask{Message: "Evalplane connection check. Return no_op; no infrastructure action will be executed."},
		Observation:      agentgateway.Observation{Timestamp: s.now().UTC(), Workloads: []agentgateway.WorkloadObservation{}, Pods: []agentgateway.PodObservation{}, Metrics: map[string]any{}, RecentEvents: []agentgateway.RecentEvent{}},
		AvailableActions: []string{"no_op"}, VisibleContext: json.RawMessage(`{"connection_check":true}`), AllowedResources: json.RawMessage(`[]`)}
	_, err := agentgateway.NewHTTPGateway(agentgateway.NewPolicyHTTPClient(s.policy, s.timeout), nil).Invoke(checkCtx, agentgateway.Connection{EndpointURL: stored.EndpointURL, BearerToken: token, Timeout: s.timeout}, request)
	switch {
	case err == nil:
		return StatusHealthy, "connection_healthy"
	case errors.Is(err, agentgateway.ErrAuthentication):
		return StatusAuthenticationFailed, "authentication_rejected"
	case errors.Is(err, agentgateway.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return StatusTimeout, "connection_timed_out"
	case errors.Is(err, agentgateway.ErrInvalidResponse), errors.Is(err, agentgateway.ErrUnsupportedProtocol), errors.Is(err, agentgateway.ErrResponseTooLarge):
		return StatusProtocolError, "invalid_protocol_response"
	case errors.Is(err, agentgateway.ErrInvalidConnection):
		return StatusConfigurationError, "endpoint_blocked"
	default:
		return StatusUnreachable, "endpoint_unreachable"
	}
}

func (s *Service) Resolve(ctx context.Context, workspaceID string, version agent.Version) (agentgateway.Connection, error) {
	stored, err := s.repo.Get(ctx, s.pool, workspaceID, version.ID)
	if err != nil {
		return agentgateway.Connection{}, err
	}
	if stored.Protocol != "http" || stored.EndpointURL != version.EndpointURL {
		return agentgateway.Connection{}, errors.New("pinned agent connection identity mismatch")
	}
	if err := agentgateway.ValidateEndpointPolicy(ctx, stored.EndpointURL, s.policy); err != nil {
		return agentgateway.Connection{}, err
	}
	var token string
	if stored.AuthType == AuthBearer {
		token, err = s.cipher.Decrypt(stored.CredentialEnvelope)
		if err != nil {
			return agentgateway.Connection{}, errors.New("agent credential unavailable")
		}
	}
	return agentgateway.Connection{EndpointURL: stored.EndpointURL, BearerToken: token}, nil
}

func (s *Service) Compatibility(ctx context.Context, p auth.Principal, workspaceID, versionID, scenarioVersionID string) (Compatibility, error) {
	if _, err := s.role(ctx, p, workspaceID); err != nil {
		return Compatibility{}, err
	}
	declared, required, err := s.repo.GetCompatibility(ctx, s.pool, workspaceID, versionID, scenarioVersionID)
	if err != nil {
		return Compatibility{}, err
	}
	sort.Strings(declared)
	sort.Strings(required)
	missing := agentcapability.Missing(declared, required)
	return Compatibility{Compatible: len(missing) == 0, DeclaredCapabilities: declared, RequiredCapabilities: required, MissingCapabilities: missing}, nil
}

func (s *Service) role(ctx context.Context, p auth.Principal, workspaceID string) (string, error) {
	role, err := s.roles.GetRole(ctx, s.pool, workspaceID, p.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return "", auth.ErrForbidden
	}
	return role, err
}
func (s *Service) write(ctx context.Context, p auth.Principal, workspaceID string) error {
	role, err := s.role(ctx, p, workspaceID)
	if err != nil {
		return err
	}
	if role == "viewer" {
		return auth.ErrForbidden
	}
	return nil
}
func validation(message string) error {
	return errors.Join(identity.ErrValidation, errors.New(message))
}
