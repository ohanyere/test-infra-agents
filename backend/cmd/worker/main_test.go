package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/agent"
)

func TestConnectionResolverPinsWorkspaceAndVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	if err := os.WriteFile(path, []byte(`[{"workspace_id":"workspace-one","agent_version_id":"version-one","endpoint":"http://reference-agent:8081"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	resolve, err := resolver(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = resolve(context.Background(), "workspace-one", agent.Version{ID: "version-one"}); err != nil {
		t.Fatal(err)
	}
	for _, key := range [][2]string{{"workspace-two", "version-one"}, {"workspace-one", "version-two"}} {
		if _, err = resolve(context.Background(), key[0], agent.Version{ID: key[1]}); err == nil {
			t.Fatalf("unconfigured connection accepted: %v", key)
		}
	}
}
