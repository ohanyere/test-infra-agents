package main

import (
	"context"

	"github.com/evalplane/evalplane/backend/internal/evaluator"
	"github.com/evalplane/evalplane/backend/internal/verification"
)

// customerCollectorFinalizer deliberately delegates to the canonical run
// evaluator and verifier. It contains no customer-specific grading logic.
type customerCollectorFinalizer struct {
	evaluator *evaluator.Service
	verifier  *verification.Service
}

func (f customerCollectorFinalizer) FinalizeRun(ctx context.Context, workspaceID, runID string) (string, error) {
	if _, err := f.evaluator.EvaluateRun(ctx, workspaceID, runID); err != nil {
		return "", err
	}
	result, err := f.verifier.DeriveRun(ctx, workspaceID, runID)
	if err != nil {
		return "", err
	}
	return result.OverallStatus, nil
}
