package mcpserver

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidToken = errors.New("invalid or inactive MCP grant")
	ErrCallBudget   = errors.New("MCP tool-call budget exhausted")
)

type GrantStore interface {
	Create(context.Context, Grant) (Grant, error)
	Resolve(context.Context, []byte) (Grant, error)
	Consume(context.Context, string) error
	RevokeRun(context.Context, string) error
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Create(ctx context.Context, in Grant) (Grant, error) {
	var out Grant
	err := r.pool.QueryRow(ctx, `INSERT INTO mcp_run_grants
		(workspace_id,run_id,agent_version_id,token_hash,expires_at,max_tool_calls)
		SELECT $1,$2,$3,$4,$5,$6
		FROM runs r JOIN evaluation_batches b ON b.id=r.batch_id
		WHERE r.id=$2 AND b.workspace_id=$1 AND r.agent_version_id=$3 AND r.status='running'
		ON CONFLICT (run_id,agent_version_id) DO UPDATE SET
		 token_hash=EXCLUDED.token_hash,expires_at=EXCLUDED.expires_at,revoked_at=NULL,
		 tool_calls=0,max_tool_calls=EXCLUDED.max_tool_calls
		RETURNING id,workspace_id,run_id,agent_version_id,token_hash,expires_at,revoked_at,tool_calls,max_tool_calls`,
		in.WorkspaceID, in.RunID, in.AgentVersionID, in.TokenHash, in.ExpiresAt, in.MaxToolCalls).
		Scan(&out.ID, &out.WorkspaceID, &out.RunID, &out.AgentVersionID, &out.TokenHash, &out.ExpiresAt, &out.RevokedAt, &out.ToolCalls, &out.MaxToolCalls)
	return out, err
}

func (r *Repository) Resolve(ctx context.Context, hash []byte) (Grant, error) {
	var out Grant
	err := r.pool.QueryRow(ctx, `SELECT g.id,g.workspace_id,g.run_id,g.agent_version_id,g.token_hash,g.expires_at,g.revoked_at,g.tool_calls,g.max_tool_calls
		FROM mcp_run_grants g JOIN runs r ON r.id=g.run_id JOIN evaluation_batches b ON b.id=r.batch_id
		WHERE g.token_hash=$1 AND g.revoked_at IS NULL AND g.expires_at>now()
		  AND b.workspace_id=g.workspace_id AND r.agent_version_id=g.agent_version_id
		  AND r.status='running'`, hash).
		Scan(&out.ID, &out.WorkspaceID, &out.RunID, &out.AgentVersionID, &out.TokenHash, &out.ExpiresAt, &out.RevokedAt, &out.ToolCalls, &out.MaxToolCalls)
	if errors.Is(err, pgx.ErrNoRows) {
		return Grant{}, ErrInvalidToken
	}
	return out, err
}

func (r *Repository) Consume(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE mcp_run_grants g SET tool_calls=tool_calls+1
		FROM runs r WHERE g.id=$1 AND r.id=g.run_id AND r.status='running'
		 AND g.revoked_at IS NULL AND g.expires_at>now() AND g.tool_calls<g.max_tool_calls`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrCallBudget
	}
	return err
}

func (r *Repository) RevokeRun(ctx context.Context, runID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE mcp_run_grants SET revoked_at=COALESCE(revoked_at,now()) WHERE run_id=$1`, runID)
	return err
}
