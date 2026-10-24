package operational

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestReachabilityControlledLocalTLSFixture(t *testing.T) {
	listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
	if listenErr != nil {
		t.Skipf("loopback fixture unavailable: %v", listenErr)
	}
	server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("unexpected method %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})}}
	server.StartTLS()
	defer server.Close()
	cert := server.Certificate()
	if len(cert.DNSNames) == 0 {
		t.Skip("TLS fixture certificate has no DNS name")
	}
	host := cert.DNSNames[0]
	if !validReachabilityTarget("https://" + host) {
		t.Skip("fixture DNS name is outside registered target policy")
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	pin := sha256.Sum256(cert.Raw)
	dialer := &net.Dialer{Timeout: time.Second}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, VerifyConnection: func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return errors.New("missing certificate")
		}
		actual := sha256.Sum256(state.PeerCertificates[0].Raw)
		if subtle.ConstantTimeCompare(actual[:], pin[:]) != 1 {
			return errors.New("pin mismatch")
		}
		return nil
	}}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != host+":443" {
			return nil, errUnsafeProbeDestination
		}
		return dialer.DialContext(ctx, network, server.Listener.Addr().String())
	}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errProbeRedirect }}
	ip := "8.8.8.8"
	source := DirectSource{EndpointURL: "https://" + host, Namespace: "checkout", ResourceName: "web", Configuration: DirectSourceConfig{ProbeVantage: "external-fixture"}}
	_, payload, _, err := acquireReachability(context.Background(), client, source, "external-fixture", &ip, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(payload)
	var value struct {
		Reachability ReachabilityObservation `json:"reachability"`
	}
	if json.Unmarshal(data, &value) != nil || value.Reachability.Outcome != "reachable" || value.Reachability.HTTPStatus != 204 {
		t.Fatalf("TLS fixture not observed: %s", data)
	}
}

func TestReachabilityTargetAndDestinationPolicy(t *testing.T) {
	for _, target := range []string{"http://checkout.example.org", "https://127.0.0.1", "https://localhost", "https://checkout.internal", "https://checkout.example.org:8443", "https://user:pass@checkout.example.org", "https://checkout.example.org/private", "https://checkout.example.org?url=foo"} {
		if validReachabilityTarget(target) {
			t.Fatalf("unsafe target accepted: %s", target)
		}
	}
	if !validReachabilityTarget("https://checkout.example.org") {
		t.Fatal("exact HTTPS target rejected")
	}
	if !validDirectConfig(DirectSource{Kind: "external_reachability", EndpointURL: "https://checkout.example.org/", ServerCertificateSHA256: strings.Repeat("a", 64), Namespace: "checkout", ResourceName: "web", Configuration: DirectSourceConfig{ProbeVantage: "external-west"}}) {
		t.Fatal("root-path HTTPS target rejected at source registration")
	}
	for _, value := range []string{"127.0.0.1", "10.0.0.2", "169.254.169.254", "100.64.0.1", "192.0.2.1", "198.51.100.1", "203.0.113.1", "::1", "fe80::1", "fc00::1", "2001:db8::1"} {
		if publicProbeIP(netip.MustParseAddr(value)) {
			t.Fatalf("prohibited IP accepted: %s", value)
		}
	}
	if !publicProbeIP(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public IP rejected")
	}
}

func TestProbeDialRejectsDNSRebindingAndUnsafeAddress(t *testing.T) {
	calls := 0
	dials := 0
	resolve := func(context.Context, string, string) ([]net.IP, error) {
		calls++
		if calls == 1 {
			return []net.IP{net.ParseIP("8.8.8.8")}, nil
		}
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	dial := func(context.Context, string, string) (net.Conn, error) {
		dials++
		a, b := net.Pipe()
		b.Close()
		return a, nil
	}
	dialContext := probeDialContext(resolve, dial, "checkout.example.org", nil)
	conn, err := dialContext(context.Background(), "tcp", "checkout.example.org:443")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if _, err = dialContext(context.Background(), "tcp", "checkout.example.org:443"); !errors.Is(err, errUnsafeProbeDestination) || dials != 1 {
		t.Fatalf("rebinding reached destination: dials=%d err=%v", dials, err)
	}
	if _, err = dialContext(context.Background(), "tcp", "other.example.org:443"); !errors.Is(err, errUnsafeProbeDestination) {
		t.Fatalf("host switch accepted: %v", err)
	}
	if _, err = dialContext(context.Background(), "tcp", "checkout.example.org:80"); !errors.Is(err, errUnsafeProbeDestination) {
		t.Fatalf("port switch accepted: %v", err)
	}
}

type probeRoundTrip func(*http.Request) (*http.Response, error)

func (f probeRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestReachabilityAttemptDoesNotProvePrivacy(t *testing.T) {
	source := DirectSource{Kind: "external_reachability", EndpointURL: "https://checkout.example.org", Namespace: "checkout", ResourceName: "web", Configuration: DirectSourceConfig{ProbeVantage: "external-west"}}
	for _, tc := range []struct {
		name     string
		response *http.Response
		err      error
		want     string
	}{{"reachable", &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("secret body"))}, nil, "reachable"}, {"refused", nil, errors.New("connection refused"), "connection_failed"}, {"dns", nil, errProbeDNS, "dns_failed"}} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: probeRoundTrip(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodHead || r.Header.Get("Authorization") != "" {
					t.Fatalf("unexpected request: %+v", r)
				}
				return tc.response, tc.err
			})}
			ip := "8.8.8.8"
			raw, payload, _, err := acquireReachability(context.Background(), client, source, "external-west", &ip, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "secret body") {
				t.Fatal("response content persisted")
			}
			data, _ := json.Marshal(payload)
			var value struct {
				Reachability ReachabilityObservation `json:"reachability"`
			}
			if json.Unmarshal(data, &value) != nil || value.Reachability.Outcome != tc.want {
				t.Fatalf("result: %s", data)
			}
		})
	}
	redirect := &http.Client{Transport: probeRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"http://169.254.169.254/"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	}), CheckRedirect: func(*http.Request, []*http.Request) error { return errProbeRedirect }}
	ip := "8.8.8.8"
	if _, _, _, err := acquireReachability(context.Background(), redirect, source, "external-west", &ip, time.Now()); !errors.Is(err, errProbeRedirect) {
		t.Fatalf("unsafe redirect accepted: %v", err)
	}
}

func TestReachabilityRequiresBoundIngressEvidence(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	ingress := KubernetesResourceState{Kind: "Ingress", Namespace: "checkout", Name: "web", Present: true, IngressHosts: []string{"checkout.example.org"}, LoadBalancerIngress: []string{"8.8.8.8"}}
	ingressPayload, _ := json.Marshal(map[string]any{"resource_state": ingress})
	in := Input{EvaluatedAt: now, Freshness: time.Minute, DirectObservations: []DirectObservation{{ID: "ingress", SourceID: "ingress-source", SourceDigest: "sha256:a", Kind: "kubernetes_ingress", ObservedAt: now.Add(-time.Second), AcquiredAt: now.Add(-time.Second), Payload: ingressPayload}}}
	rule := Rule{Type: "no_public_exposure", Namespace: "checkout", ResourceName: "web"}
	if f := assessExposureConfiguration(in, rule); f.Status != Inconclusive {
		t.Fatalf("configuration alone passed: %+v", f)
	}
	makeProbe := func(id, outcome, ip string, at time.Time) DirectObservation {
		p := ReachabilityObservation{TargetHost: "checkout.example.org", ResolvedIP: ip, Namespace: "checkout", IngressName: "web", Vantage: "external-west", Outcome: outcome, HTTPStatus: 403, ObservedAt: at}
		raw, _ := json.Marshal(map[string]any{"reachability": p})
		return DirectObservation{ID: id, SourceID: "probe-source", SourceDigest: "sha256:b", Kind: "external_reachability", ObservedAt: at, AcquiredAt: at, Payload: raw}
	}
	in.DirectObservations = append(in.DirectObservations, makeProbe("probe", "reachable", "8.8.8.8", now))
	if f := assessExposureConfiguration(in, rule); f.Status != Fail || f.Code != "PUBLIC_REACHABILITY_OBSERVED" || len(f.Evidence) < 2 {
		t.Fatalf("bound reachability not violation: %+v", f)
	}
	in.DirectObservations[1] = makeProbe("wrong-ip", "reachable", "1.1.1.1", now)
	if f := assessExposureConfiguration(in, rule); f.Status != Inconclusive || f.Code != "TARGET_INGRESS_BINDING_UNESTABLISHED" {
		t.Fatalf("unbound target promoted: %+v", f)
	}
	in.DirectObservations[1] = makeProbe("refused", "connection_failed", "8.8.8.8", now)
	if f := assessExposureConfiguration(in, rule); f.Status != Inconclusive || f.Code != "REACHABILITY_NOT_ESTABLISHED" {
		t.Fatalf("refusal proved privacy: %+v", f)
	}
	in.DirectObservations[1] = makeProbe("stale", "reachable", "8.8.8.8", now.Add(-2*time.Minute))
	if f := assessExposureConfiguration(in, rule); f.Status != Inconclusive {
		t.Fatalf("stale probe promoted: %+v", f)
	}
	in.DirectObservations[1] = makeProbe("one", "reachable", "8.8.8.8", now)
	in.DirectObservations = append(in.DirectObservations, makeProbe("two", "connection_failed", "8.8.8.8", now))
	if f := assessExposureConfiguration(in, rule); f.Status != Conflicting {
		t.Fatalf("conflict promoted: %+v", f)
	}
	in.EvaluatedAt = now.Add(time.Second)
	in.DirectObservations = append(in.DirectObservations, makeProbe("later-refused", "connection_failed", "8.8.8.8", in.EvaluatedAt))
	if f := assessExposureConfiguration(in, rule); f.Status != Inconclusive || f.Code != "REACHABILITY_NOT_ESTABLISHED" {
		t.Fatalf("later observation failed to resolve current conflict: %+v", f)
	}
}
