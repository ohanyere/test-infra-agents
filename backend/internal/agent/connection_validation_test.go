package agent

import (
	"errors"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

func TestHTTPVersionRejectsUnknownCapabilityAndUnsafeEndpointForms(t *testing.T) {
	value := validCreate()
	value.InitialVersion.Capabilities = []string{"made_up_capability"}
	if _, err := ValidateCreate(value); !errors.Is(err, identity.ErrValidation) {
		t.Fatalf("unknown capability error=%v", err)
	}
	for _, endpoint := range []string{"ftp://agent.example/invoke", "https://user:pass@agent.example/invoke", "http://169.254.169.254/latest", "https://agent.example/invoke?token=no"} {
		value = validCreate()
		value.InitialVersion.EndpointURL = endpoint
		if _, err := ValidateCreate(value); !errors.Is(err, identity.ErrValidation) {
			t.Fatalf("endpoint %q error=%v", endpoint, err)
		}
	}
}
