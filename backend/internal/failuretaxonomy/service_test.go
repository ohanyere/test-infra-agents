package failuretaxonomy

import (
	"context"
	"errors"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

type testRoles struct{ err error }

func (r testRoles) GetRole(context.Context, identity.DBTX, string, string) (string, error) {
	return "viewer", r.err
}

type testOccurrences struct{ rows []Occurrence }

func (r testOccurrences) List(context.Context, DBTX, string, string) ([]Occurrence, error) {
	return r.rows, nil
}
func TestServiceEnforcesWorkspaceMembership(t *testing.T) {
	service := NewService(nil, testOccurrences{}, testRoles{err: identity.ErrNotFound})
	_, err := service.ForExecution(context.Background(), auth.Principal{UserID: "user"}, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("err=%v", err)
	}
}
