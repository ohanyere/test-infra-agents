package agentgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

var allowedActions = map[string]struct{}{"inspect": {}, "scale_workload": {}, "restart_pod": {}, "no_op": {}}

type EndpointPolicy struct {
	AllowPrivate bool
	Resolver     *net.Resolver
	Dialer       *net.Dialer
}

func ValidateEndpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("%w: endpoint must be an absolute HTTP(S) URL", ErrInvalidConnection)
	}
	if u.User != nil {
		return nil, fmt.Errorf("%w: embedded URL credentials are not allowed", ErrInvalidConnection)
	}
	if u.Fragment != "" || u.RawQuery != "" || u.Hostname() == "" {
		return nil, fmt.Errorf("%w: endpoint query strings and fragments are not allowed", ErrInvalidConnection)
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "metadata.google.internal" || host == "metadata.google" || host == "instance-data.ec2.internal" {
		return nil, fmt.Errorf("%w: endpoint is prohibited by network policy", ErrInvalidConnection)
	}
	if ip := net.ParseIP(host); ip != nil {
		if err := validateIP(ip, true); err != nil {
			return nil, err
		}
	}
	return u, nil
}

func ValidateConnection(connection Connection) error {
	if _, err := ValidateEndpoint(connection.EndpointURL); err != nil {
		return err
	}
	if connection.Timeout < 0 {
		return fmt.Errorf("%w: timeout cannot be negative", ErrInvalidConnection)
	}
	return nil
}

func ValidateEndpointPolicy(ctx context.Context, raw string, policy EndpointPolicy) error {
	u, err := ValidateEndpoint(raw)
	if err != nil {
		return err
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		return validateIP(ip, policy.AllowPrivate)
	}
	resolver := policy.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	ips, err := resolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("%w: endpoint hostname could not be resolved", ErrInvalidConnection)
	}
	if len(ips) == 0 {
		return fmt.Errorf("%w: endpoint hostname has no addresses", ErrInvalidConnection)
	}
	for _, ip := range ips {
		if err := validateIP(ip, policy.AllowPrivate); err != nil {
			return err
		}
	}
	return nil
}

func validateIP(ip net.IP, allowPrivate bool) error {
	if ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || isMetadataIP(ip) {
		return fmt.Errorf("%w: endpoint is prohibited by network policy", ErrInvalidConnection)
	}
	if !allowPrivate && (ip.IsLoopback() || ip.IsPrivate()) {
		return fmt.Errorf("%w: private endpoints are disabled", ErrInvalidConnection)
	}
	return nil
}

func isMetadataIP(ip net.IP) bool {
	return ip.Equal(net.ParseIP("169.254.169.254")) || ip.Equal(net.ParseIP("169.254.170.2")) || ip.Equal(net.ParseIP("100.100.100.200"))
}

func NewPolicyHTTPClient(policy EndpointPolicy, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	dialer := policy.Dialer
	if dialer == nil {
		dialer = &net.Dialer{Timeout: timeout}
	}
	resolver := policy.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("%w: malformed destination", ErrInvalidConnection)
		}
		ips, err := resolver.LookupIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, fmt.Errorf("%w: endpoint hostname could not be resolved", ErrInvalidConnection)
		}
		for _, ip := range ips {
			if err := validateIP(ip, policy.AllowPrivate); err != nil {
				return nil, err
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}}
	client := &http.Client{Transport: transport, Timeout: timeout}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("%w: too many redirects", ErrInvalidConnection)
		}
		return ValidateEndpointPolicy(req.Context(), req.URL.String(), policy)
	}
	return client
}

func ValidateRequest(request Request) error {
	if request.ProtocolVersion != ProtocolV1 {
		return ErrUnsupportedProtocol
	}
	if _, err := uuid.Parse(request.RunID); err != nil {
		return fmt.Errorf("%w: invalid run_id", ErrInvalidRequest)
	}
	if _, err := uuid.Parse(request.AgentVersionID); err != nil {
		return fmt.Errorf("%w: invalid agent_version_id", ErrInvalidRequest)
	}
	if len(request.SessionID) > 200 || strings.TrimSpace(request.Task.Message) == "" || len(request.Task.Message) > 4000 {
		return fmt.Errorf("%w: invalid session or task", ErrInvalidRequest)
	}
	if request.Observation.Timestamp.IsZero() {
		return fmt.Errorf("%w: observation timestamp is required", ErrInvalidRequest)
	}
	if len(request.AvailableActions) == 0 || len(request.AvailableActions) > len(allowedActions) {
		return fmt.Errorf("%w: invalid available_actions", ErrInvalidRequest)
	}
	seen := map[string]struct{}{}
	for _, action := range request.AvailableActions {
		if _, ok := allowedActions[action]; !ok {
			return fmt.Errorf("%w: unsupported available action", ErrInvalidRequest)
		}
		if _, ok := seen[action]; ok {
			return fmt.Errorf("%w: duplicate available action", ErrInvalidRequest)
		}
		seen[action] = struct{}{}
	}
	if _, err := json.Marshal(request.Observation); err != nil {
		return fmt.Errorf("%w: observation is not JSON-compatible", ErrInvalidRequest)
	}
	if request.ToolAccess != nil {
		if request.ToolAccess.Mode != "mcp" || len(request.ToolAccess.Token) < 32 || len(request.ToolAccess.Token) > 200 || request.ToolAccess.ExpiresAt.IsZero() {
			return fmt.Errorf("%w: invalid MCP tool access grant", ErrInvalidRequest)
		}
		if _, err := ValidateEndpoint(request.ToolAccess.Endpoint); err != nil {
			return fmt.Errorf("%w: invalid MCP tool endpoint", ErrInvalidRequest)
		}
	}
	return nil
}

func ValidateResponse(response Response, available []string) error {
	if response.ProtocolVersion != ProtocolV1 {
		return ErrUnsupportedProtocol
	}
	if len(response.ReasoningSummary) > 2000 {
		return fmt.Errorf("%w: reasoning_summary too long", ErrInvalidResponse)
	}
	if len(response.Actions) > MaxActions {
		return fmt.Errorf("%w: too many actions", ErrInvalidResponse)
	}
	allowed := map[string]struct{}{}
	for _, action := range available {
		allowed[action] = struct{}{}
	}
	for _, action := range response.Actions {
		if _, ok := allowed[action.Type]; !ok {
			return fmt.Errorf("%w: action is unavailable", ErrInvalidResponse)
		}
		if err := validateAction(action); err != nil {
			return err
		}
	}
	return nil
}

func validateAction(action ProposedAction) error {
	if _, ok := allowedActions[action.Type]; !ok {
		return fmt.Errorf("%w: unsupported action type", ErrInvalidResponse)
	}
	if action.Type == "no_op" {
		if action.Target != nil || len(action.Parameters) != 0 {
			return fmt.Errorf("%w: no_op cannot have a target or parameters", ErrInvalidResponse)
		}
		return nil
	}
	if action.Target == nil || strings.TrimSpace(action.Target.Kind) == "" || strings.TrimSpace(action.Target.Namespace) == "" || strings.TrimSpace(action.Target.Name) == "" {
		return fmt.Errorf("%w: action target is required", ErrInvalidResponse)
	}
	switch action.Type {
	case "inspect":
		if action.Target.Kind != "Deployment" && action.Target.Kind != "Pod" && action.Target.Kind != "Service" {
			return fmt.Errorf("%w: invalid inspect target", ErrInvalidResponse)
		}
	case "scale_workload":
		if action.Target.Kind != "Deployment" {
			return fmt.Errorf("%w: scale target must be a Deployment", ErrInvalidResponse)
		}
		replicas, ok := action.Parameters["replicas"].(float64)
		if !ok || replicas < 1 || replicas > 100 || replicas != float64(int(replicas)) {
			return fmt.Errorf("%w: replicas must be an integer from 1 to 100", ErrInvalidResponse)
		}
	case "restart_pod":
		if action.Target.Kind != "Pod" {
			return fmt.Errorf("%w: restart target must be a Pod", ErrInvalidResponse)
		}
	}
	return nil
}
