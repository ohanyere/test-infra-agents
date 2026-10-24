package mcpserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/evalplane/evalplane/backend/internal/actionexec"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/observablecatalog"
)

type Observer interface {
	Observe(context.Context, Scope) (agentgateway.Observation, error)
}
type Actions interface {
	Authorize(context.Context, Scope, agentgateway.ProposedAction) actionexec.ActionResult
	Execute(context.Context, Scope, agentgateway.ProposedAction) actionexec.ActionResult
}
type Evidence interface {
	AppendRunEvent(context.Context, evidence.AppendRunEventInput) (evidence.RunEvent, error)
	RecordAgentAction(context.Context, evidence.RecordAgentActionInput) (evidence.AgentAction, error)
	UpdateAgentAction(context.Context, evidence.UpdateActionInput) error
}

type Config struct {
	PublicEndpoint string
	GrantTTL       time.Duration
	MaxToolCalls   int
}

func DefaultConfig() Config {
	return Config{GrantTTL: 10 * time.Minute, MaxToolCalls: 64}
}

type Service struct {
	store    GrantStore
	observer Observer
	actions  Actions
	evidence Evidence
	config   Config
	now      func() time.Time
	mu       sync.RWMutex
	scopes   map[string]Scope
}

func NewService(store GrantStore, observer Observer, actions Actions, evidence Evidence, config Config) (*Service, error) {
	config.PublicEndpoint = strings.TrimSpace(config.PublicEndpoint)
	if store == nil || observer == nil || actions == nil || evidence == nil || config.PublicEndpoint == "" || config.GrantTTL <= 0 || config.MaxToolCalls < 1 || config.MaxToolCalls > 1000 {
		return nil, errors.New("valid MCP grant store, tool dependencies, endpoint, TTL, and call budget are required")
	}
	return &Service{store: store, observer: observer, actions: actions, evidence: evidence, config: config, now: func() time.Time { return time.Now().UTC() }, scopes: map[string]Scope{}}, nil
}

func (s *Service) CreateGrant(ctx context.Context, scope Scope) (Access, error) {
	if scope.WorkspaceID == "" || scope.RunID == "" || scope.Version.ID == "" || scope.Version.ToolAccessMode != "mcp" || scope.Environment.Identity.RunID != scope.RunID || scope.Environment.Identity.WorkspaceID != scope.WorkspaceID {
		return Access{}, errors.New("invalid MCP run scope")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Access{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	expires := s.now().Add(s.config.GrantTTL)
	grant, err := s.store.Create(ctx, Grant{WorkspaceID: scope.WorkspaceID, RunID: scope.RunID, AgentVersionID: scope.Version.ID, TokenHash: hash[:], ExpiresAt: expires, MaxToolCalls: s.config.MaxToolCalls})
	if err != nil {
		return Access{}, err
	}
	s.mu.Lock()
	s.scopes[grant.ID] = scope
	s.mu.Unlock()
	return Access{Endpoint: s.config.PublicEndpoint, Token: token, ExpiresAt: expires}, nil
}

func (s *Service) RevokeRun(ctx context.Context, runID string) error {
	err := s.store.RevokeRun(ctx, runID)
	s.mu.Lock()
	for id, scope := range s.scopes {
		if scope.RunID == runID {
			delete(s.scopes, id)
		}
	}
	s.mu.Unlock()
	return err
}

func tokenHash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

func (s *Service) resolve(ctx context.Context, token string, consume bool) (Grant, Scope, error) {
	if len(token) < 32 || len(token) > 200 {
		return Grant{}, Scope{}, ErrInvalidToken
	}
	grant, err := s.store.Resolve(ctx, tokenHash(token))
	if err != nil {
		return Grant{}, Scope{}, err
	}
	s.mu.RLock()
	scope, ok := s.scopes[grant.ID]
	s.mu.RUnlock()
	if !ok || scope.RunID != grant.RunID || scope.WorkspaceID != grant.WorkspaceID || scope.Version.ID != grant.AgentVersionID || subtle.ConstantTimeCompare(grant.TokenHash, tokenHash(token)) != 1 {
		return Grant{}, Scope{}, ErrInvalidToken
	}
	if consume {
		if err := s.store.Consume(ctx, grant.ID); err != nil {
			return Grant{}, Scope{}, err
		}
	}
	return grant, scope, nil
}

func (s *Service) effectiveTools(scope Scope) []string {
	allowed := map[string]bool{}
	for _, capability := range scope.Version.Capabilities {
		allowed[capability] = true
	}
	viewObs, viewActions := observablecatalog.Resolve(scope.AgentView.AllowedObservations), map[string]bool{}
	for _, capability := range scope.AgentView.AllowedActions {
		viewActions[capability] = true
	}
	var out []string
	if viewObs[observablecatalog.Workloads] && observablecatalog.HasCapability(observablecatalog.Workloads, allowed) {
		out = append(out, ToolObserveWorkloads)
	}
	if viewObs[observablecatalog.Pods] && observablecatalog.HasCapability(observablecatalog.Pods, allowed) {
		out = append(out, ToolObservePods)
	}
	if viewObs[observablecatalog.Events] && observablecatalog.HasCapability(observablecatalog.Events, allowed) {
		out = append(out, ToolObserveEvents)
	}
	for _, definition := range observablecatalog.Definitions() {
		if definition.MetricKey != "" && viewObs[definition.ID] && observablecatalog.HasCapability(definition.ID, allowed) {
			out = append(out, ToolGetTelemetry)
			break
		}
	}
	if allowed["scale_workload"] && viewActions["scale_workload"] {
		out = append(out, ToolScaleWorkload)
	}
	if allowed["restart_pod"] && viewActions["restart_pod"] {
		out = append(out, ToolRestartPod)
	}
	if allowed["no_op"] && viewActions["no_op"] {
		out = append(out, ToolNoOp)
	}
	return out
}

func (s *Service) record(ctx context.Context, runID, code, tool string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["code"], fields["tool"] = code, tool
	payload, _ := json.Marshal(fields)
	_, _ = s.evidence.AppendRunEvent(ctx, evidence.AppendRunEventInput{RunID: runID, EventType: "mcp", OccurredAt: s.now(), Summary: code, Payload: payload})
}
