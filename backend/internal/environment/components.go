package environment

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

// EnvironmentComponentInstance is a planned/provisioned child of one
// aggregate EnvironmentInstance. ProviderRef is safe opaque metadata only.
type EnvironmentComponentInstance struct {
	ID                      string          `json:"id"`
	WorkspaceID             string          `json:"workspace_id"`
	EnvironmentInstanceID   string          `json:"environment_instance_id"`
	ComponentID             string          `json:"component_id"`
	ComponentType           string          `json:"component_type"`
	ComponentVersion        string          `json:"component_version,omitempty"`
	AdapterID               string          `json:"adapter_id"`
	AdapterContractVersion  string          `json:"adapter_contract_version"`
	ProviderID              string          `json:"provider_id"`
	DependencyOrder         int             `json:"dependency_order"`
	Status                  string          `json:"status"`
	ProviderRef             string          `json:"provider_ref,omitempty"`
	ReproducibilityMetadata json.RawMessage `json:"reproducibility_metadata"`
	CreatedAt               time.Time       `json:"created_at"`
	ReadyAt                 *time.Time      `json:"ready_at,omitempty"`
	DestroyedAt             *time.Time      `json:"destroyed_at,omitempty"`
}

func validateComponentInstance(in EnvironmentComponentInstance) error {
	for _, v := range []string{in.WorkspaceID, in.EnvironmentInstanceID, in.ComponentID, in.ComponentType, in.AdapterID, in.AdapterContractVersion, in.ProviderID} {
		if strings.TrimSpace(v) == "" {
			return errValidation("component instance identifiers must not be blank")
		}
	}
	if in.DependencyOrder < 0 {
		return errValidation("component dependency order must not be negative")
	}
	l := strings.ToLower(in.ProviderRef)
	for _, bad := range []string{"token=", "password=", "secret=", "authorization:", "bearer ", "kubeconfig", "private_key"} {
		if strings.Contains(l, bad) {
			return errValidation("component provider_ref contains secret-like data")
		}
	}
	if parsed, err := url.Parse(in.ProviderRef); err == nil && parsed.User != nil {
		return errValidation("component provider_ref must not contain URL credentials")
	}
	if len(in.ProviderRef) > 2000 {
		return errValidation("component provider_ref is too long")
	}
	return nil
}

func (r *Repository) InsertComponentInstance(ctx context.Context, db DBTX, in EnvironmentComponentInstance) (EnvironmentComponentInstance, error) {
	if err := validateComponentInstance(in); err != nil {
		return EnvironmentComponentInstance{}, err
	}
	if len(in.ReproducibilityMetadata) == 0 {
		in.ReproducibilityMetadata = json.RawMessage(`{}`)
	}
	var out EnvironmentComponentInstance
	err := db.QueryRow(ctx, `INSERT INTO environment_component_instances(workspace_id,environment_instance_id,component_id,component_type,component_version,adapter_id,adapter_contract_version,provider_id,dependency_order,status,provider_ref,reproducibility_metadata) SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,'planned',$10,$11 FROM environment_instances e WHERE e.id=$2 AND e.workspace_id=$1 RETURNING id,workspace_id,environment_instance_id,component_id,component_type,component_version,adapter_id,adapter_contract_version,provider_id,dependency_order,status,provider_ref,reproducibility_metadata,created_at,ready_at,destroyed_at`, in.WorkspaceID, in.EnvironmentInstanceID, in.ComponentID, in.ComponentType, in.ComponentVersion, in.AdapterID, in.AdapterContractVersion, in.ProviderID, in.DependencyOrder, in.ProviderRef, in.ReproducibilityMetadata).Scan(&out.ID, &out.WorkspaceID, &out.EnvironmentInstanceID, &out.ComponentID, &out.ComponentType, &out.ComponentVersion, &out.AdapterID, &out.AdapterContractVersion, &out.ProviderID, &out.DependencyOrder, &out.Status, &out.ProviderRef, &out.ReproducibilityMetadata, &out.CreatedAt, &out.ReadyAt, &out.DestroyedAt)
	if err != nil {
		var noRows interface{ SQLState() string }
		if errors.As(err, &noRows) && noRows.SQLState() == "02000" {
			return EnvironmentComponentInstance{}, identity.ErrNotFound
		}
		return EnvironmentComponentInstance{}, mapDB(err)
	}
	return out, nil
}

func (r *Repository) ListComponentInstances(ctx context.Context, db DBTX, workspaceID, environmentID string) ([]EnvironmentComponentInstance, error) {
	rows, err := db.Query(ctx, `SELECT c.id,c.workspace_id,c.environment_instance_id,c.component_id,c.component_type,c.component_version,c.adapter_id,c.adapter_contract_version,c.provider_id,c.dependency_order,c.status,c.provider_ref,c.reproducibility_metadata,c.created_at,c.ready_at,c.destroyed_at FROM environment_component_instances c JOIN environment_instances e ON e.id=c.environment_instance_id AND e.workspace_id=c.workspace_id WHERE c.workspace_id=$1 AND c.environment_instance_id=$2 ORDER BY c.dependency_order,c.component_id`, workspaceID, environmentID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []EnvironmentComponentInstance{}
	for rows.Next() {
		var v EnvironmentComponentInstance
		if err := rows.Scan(&v.ID, &v.WorkspaceID, &v.EnvironmentInstanceID, &v.ComponentID, &v.ComponentType, &v.ComponentVersion, &v.AdapterID, &v.AdapterContractVersion, &v.ProviderID, &v.DependencyOrder, &v.Status, &v.ProviderRef, &v.ReproducibilityMetadata, &v.CreatedAt, &v.ReadyAt, &v.DestroyedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, mapDB(rows.Err())
}
