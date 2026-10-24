package operational

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

var errUnsafeProbeDestination = errors.New("unsafe probe destination")
var errProbeDNS = errors.New("probe DNS unavailable")
var errProbeRedirect = errors.New("probe redirect forbidden")

// ReachabilityObservation describes one HTTPS HEAD attempt from the named
// operator-controlled vantage. It contains no response body, headers or token.
type ReachabilityObservation struct {
	TargetHost  string    `json:"target_host"`
	ResolvedIP  string    `json:"resolved_ip,omitempty"`
	Namespace   string    `json:"namespace"`
	IngressName string    `json:"ingress_name"`
	Vantage     string    `json:"vantage"`
	Outcome     string    `json:"outcome"`
	HTTPStatus  int       `json:"http_status,omitempty"`
	ObservedAt  time.Time `json:"observed_at"`
}

func validProbeVantage(v string) bool {
	if len(v) < 3 || len(v) > 64 {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func validReachabilityTarget(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := u.Hostname()
	if len(host) < 4 || len(host) > 253 || net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return false
	}
	lower := strings.ToLower(host)
	for _, suffix := range []string{".localhost", ".local", ".internal", ".test", ".invalid", ".example"} {
		if strings.HasSuffix(lower, suffix) {
			return false
		}
	}
	if strings.HasSuffix(lower, ".") {
		return false
	}
	for _, label := range strings.Split(lower, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

var disallowedProbeIPv4 = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"), netip.MustParsePrefix("240.0.0.0/4"),
}

func publicProbeIP(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() {
		return false
	}
	if ip.Is4() {
		for _, block := range disallowedProbeIPv4 {
			if block.Contains(ip) {
				return false
			}
		}
		return true
	}
	if !netip.MustParsePrefix("2000::/3").Contains(ip) || netip.MustParsePrefix("2001:db8::/32").Contains(ip) || netip.MustParsePrefix("2001::/32").Contains(ip) || netip.MustParsePrefix("2001:10::/28").Contains(ip) || netip.MustParsePrefix("2001:20::/28").Contains(ip) || netip.MustParsePrefix("2002::/16").Contains(ip) {
		return false
	}
	return true
}

// The transport calls this for each connection. DNS results are checked at
// dial time, then the connection is made to the checked IP while TLS still
// validates the original hostname and the registered certificate pin.
func probeDialContext(resolve func(context.Context, string, string) ([]net.IP, error), dial func(context.Context, string, string) (net.Conn, error), expectedHost string, onResolved func(string)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || host != expectedHost || port != "443" || network != "tcp" {
			return nil, errUnsafeProbeDestination
		}
		addresses, err := resolve(ctx, "ip", host)
		if err != nil || len(addresses) == 0 {
			return nil, errProbeDNS
		}
		var chosen netip.Addr
		for _, raw := range addresses {
			ip, ok := netip.AddrFromSlice(raw)
			if !ok || !publicProbeIP(ip) {
				return nil, errUnsafeProbeDestination
			}
			if !chosen.IsValid() {
				chosen = ip.Unmap()
			}
		}
		if onResolved != nil {
			onResolved(chosen.String())
		}
		return dial(ctx, "tcp", net.JoinHostPort(chosen.String(), port))
	}
}

func (s *Service) reachabilityTargetConfigured(ctx context.Context, source DirectSource, sessionID string, now time.Time) error {
	host, _ := url.Parse(source.EndpointURL)
	var bound bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customer_direct_outcome_observations d JOIN customer_outcome_sources o ON o.id=d.source_id AND o.workspace_id=d.workspace_id AND o.environment_id=d.environment_id WHERE d.workspace_id=$1 AND d.environment_id=$2 AND d.session_id=$3 AND d.source_kind='kubernetes_ingress' AND o.namespace=$4 AND o.resource_name=$5 AND d.payload->'resource_state'->>'present'='true' AND d.payload->'resource_state'->'ingress_hosts' ? $6 AND d.observed_at BETWEEN $7::timestamptz-interval '2 minutes' AND $7 AND d.acquired_at BETWEEN d.observed_at AND $7)`, source.WorkspaceID, source.EnvironmentID, sessionID, source.Namespace, source.ResourceName, host.Hostname(), now).Scan(&bound)
	if err != nil {
		return err
	}
	if !bound {
		return auth.ErrForbidden
	}
	return nil
}

func acquireReachability(ctx context.Context, client *http.Client, source DirectSource, vantage string, resolvedIP *string, started time.Time) ([]byte, any, time.Time, error) {
	if !validReachabilityTarget(source.EndpointURL) || !validProbeVantage(vantage) || vantage != source.Configuration.ProbeVantage {
		return nil, nil, time.Time{}, identity.ErrValidation
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, source.EndpointURL, nil)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	response, err := client.Do(request)
	observed := time.Now().UTC()
	outcome, status := "reachable", 0
	if err != nil {
		switch {
		case errors.Is(err, errUnsafeProbeDestination):
			return nil, nil, time.Time{}, errUnsafeProbeDestination
		case errors.Is(err, errProbeRedirect):
			return nil, nil, time.Time{}, errProbeRedirect
		case errors.Is(err, errProbeDNS):
			outcome = "dns_failed"
		default:
			outcome = "connection_failed"
		}
	} else {
		status = response.StatusCode
		response.Body.Close()
		if status < 100 || status > 599 {
			outcome = "http_failed"
		}
	}
	target, _ := url.Parse(source.EndpointURL)
	ip := ""
	if resolvedIP != nil {
		ip = *resolvedIP
	}
	result := ReachabilityObservation{TargetHost: target.Hostname(), ResolvedIP: ip, Namespace: source.Namespace, IngressName: source.ResourceName, Vantage: vantage, Outcome: outcome, HTTPStatus: status, ObservedAt: observed}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	return raw, map[string]any{"reachability": result, "probe_source": "operator_external_https_head", "started_at": started}, observed, nil
}
