package execution

import (
	"errors"
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

func TestRunTransitionValidation(t *testing.T) {
	legal := [][2]string{{"queued", "provisioning"}, {"provisioning", "running"}, {"provisioning", "failed"}, {"provisioning", "stopped"}, {"running", "completed"}, {"running", "failed"}, {"running", "stopped"}}
	for _, transition := range legal {
		if err := ValidateRunTransition(transition[0], transition[1]); err != nil {
			t.Fatalf("%s -> %s: %v", transition[0], transition[1], err)
		}
	}
	for _, transition := range [][2]string{{"completed", "running"}, {"failed", "running"}, {"stopped", "running"}, {"running", "provisioning"}} {
		if err := ValidateRunTransition(transition[0], transition[1]); !errors.Is(err, identity.ErrConflict) {
			t.Fatalf("%s -> %s: %v", transition[0], transition[1], err)
		}
	}
}

func TestEnvironmentRefValidation(t *testing.T) {
	for _, value := range []string{"", "   ", strings.Repeat("x", maxEnvironmentRefLength+1)} {
		if _, err := ValidateEnvironmentRef(value); !errors.Is(err, identity.ErrValidation) {
			t.Fatalf("value length=%d error=%v", len(value), err)
		}
	}
	if value, err := ValidateEnvironmentRef("  opaque://environment  "); err != nil || value != "opaque://environment" {
		t.Fatalf("value=%q error=%v", value, err)
	}
}
