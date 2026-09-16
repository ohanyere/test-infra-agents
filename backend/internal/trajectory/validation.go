package trajectory

import (
	"fmt"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

// ValidateFilters checks safe server-side filters. No arbitrary SQL or
// query-language filtering is accepted.
func ValidateFilters(filters Filters) error {
	if filters.Limit < 0 || filters.Limit > 1000 {
		return fmt.Errorf("%w: limit must be between 0 and 1000", identity.ErrValidation)
	}
	if filters.Offset < 0 {
		return fmt.Errorf("%w: offset must be non-negative", identity.ErrValidation)
	}
	if filters.Limit == 0 {
		filters.Limit = 200
	}
	if filters.Phase != "" && !ValidPhase(filters.Phase) {
		return fmt.Errorf("%w: phase filter is invalid", identity.ErrValidation)
	}
	if filters.EventType != "" && !ValidEventType(filters.EventType) {
		return fmt.Errorf("%w: event_type filter is invalid", identity.ErrValidation)
	}
	if filters.Actor != "" && !ValidActor(filters.Actor) {
		return fmt.Errorf("%w: actor filter is invalid", identity.ErrValidation)
	}
	if filters.Status != "" {
		switch filters.Status {
		case StatusSuccess, StatusFailed, StatusDenied, StatusInfo, StatusFinding, StatusInvalid:
		default:
			return fmt.Errorf("%w: status filter is invalid", identity.ErrValidation)
		}
	}
	return nil
}

// DefaultLimit is applied when callers omit pagination.
const DefaultLimit = 200
