#!/usr/bin/env bash
set -euo pipefail

./scripts/check-migrations.sh
./scripts/check-gofmt.sh
node scripts/check-frontend-integrity.mjs

(
  cd backend
  export GOMAXPROCS=2
  export GOCACHE="${GOCACHE:-/tmp/evalplane-go-cache}"
  go mod verify
  go test -p 1 ./internal/agent ./internal/agentconnection ./internal/evaluationbatch ./internal/evaluationsuite ./internal/execution ./internal/executionworker ./internal/environment
  go test -p 1 ./internal/httpapi ./internal/auth ./internal/identity ./internal/agentgateway ./internal/mcpserver ./internal/incidentsource
  go test -p 1 ./internal/agentcapability ./internal/competency ./internal/coverage ./internal/scenariocertification ./internal/srequal ./internal/gitopsqual ./internal/qualplan ./internal/qualification
  go test -p 1 ./internal/reliability ./internal/trajectory ./internal/evidence ./internal/evaluator ./internal/failuretaxonomy ./internal/actionexec ./internal/observation
  go test -p 1 ./internal/billing
)

npm run verify:frontend
