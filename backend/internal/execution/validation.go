package execution

import (
	"fmt"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

const maxEnvironmentRefLength = 2000

func ValidateRunTransition(from, to string) error {
	legal := map[string]map[string]bool{
		"queued":       {"provisioning": true},
		"provisioning": {"running": true, "failed": true, "stopped": true},
		"running":      {"completed": true, "failed": true, "stopped": true},
	}
	if legal[from][to] {
		return nil
	}
	return fmt.Errorf("%w: illegal run transition from %s to %s", identity.ErrConflict, from, to)
}

func ValidateEnvironmentRef(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return value, fmt.Errorf("%w: environment_ref is required", identity.ErrValidation)
	}
	if len(value) > maxEnvironmentRefLength {
		return value, fmt.Errorf("%w: environment_ref must be at most %d characters", identity.ErrValidation, maxEnvironmentRefLength)
	}
	return value, nil
}
