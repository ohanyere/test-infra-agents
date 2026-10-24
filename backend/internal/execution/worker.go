package execution

import (
	"context"
	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/scenario"
)

// RunStatus is scoped to the workspace and includes the durable batch stop signal.
func (s *Service) RunStatus(ctx context.Context, workspaceID, runID string) (status string, stopped bool, err error) {
	err = s.pool.QueryRow(ctx, `SELECT r.status,(r.status='stopped' OR b.status='stopped') FROM runs r JOIN evaluation_batches b ON b.id=r.batch_id WHERE r.id=$1 AND b.workspace_id=$2`, runID, workspaceID).Scan(&status, &stopped)
	return status, stopped, mapDB(err)
}

// LoadVersions follows the persisted run relationships, never latest-version lookups.
func (s *Service) LoadVersions(ctx context.Context, workspaceID, runID string) (scenario.Version, agent.Version, error) {
	sv, err := scenario.NewRepository().GetRunVersion(ctx, s.pool, workspaceID, runID)
	if err != nil {
		return sv, agent.Version{}, err
	}
	var av agent.Version
	err = s.pool.QueryRow(ctx, `SELECT av.id,av.version_label,av.interface_kind,av.tool_access_mode,COALESCE(av.endpoint_url,''),av.capabilities,r.agent_implementation_identity,av.metadata,av.created_at FROM runs r JOIN evaluation_batches b ON b.id=r.batch_id JOIN agent_versions av ON av.id=r.agent_version_id WHERE r.id=$1 AND b.workspace_id=$2`, runID, workspaceID).Scan(&av.ID, &av.VersionLabel, &av.InterfaceKind, &av.ToolAccessMode, &av.EndpointURL, &av.Capabilities, &av.ImplementationIdentity, &av.Metadata, &av.CreatedAt)
	return sv, av, mapDB(err)
}
