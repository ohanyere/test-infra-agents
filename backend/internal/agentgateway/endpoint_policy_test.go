package agentgateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEndpointValidationRejectsCredentialsAndProhibitedAddresses(t *testing.T) {
	for _, endpoint := range []string{"https://user:pass@example.com/agent", "http://169.254.169.254/latest", "http://0.0.0.0/agent", "https://example.com/agent#fragment"} {
		if _, err := ValidateEndpoint(endpoint); err == nil {
			t.Fatalf("accepted %s", endpoint)
		}
	}
}

func TestPrivateEndpointRequiresExplicitPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	if err := ValidateEndpointPolicy(context.Background(), server.URL, EndpointPolicy{}); err == nil {
		t.Fatal("production policy accepted loopback")
	}
	if err := ValidateEndpointPolicy(context.Background(), server.URL, EndpointPolicy{AllowPrivate: true}); err != nil {
		t.Fatalf("development policy: %v", err)
	}
}

func TestRedirectCannotEscapeEndpointPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest", http.StatusFound)
	}))
	defer server.Close()
	client := NewPolicyHTTPClient(EndpointPolicy{AllowPrivate: true}, time.Second)
	if _, err := client.Get(server.URL); err == nil {
		t.Fatal("followed redirect to prohibited address")
	}
}
