# Lightweight WSL development

This path runs the customer application without Docker or Kubernetes:

```text
Windows browser -> Next.js :3000 -> Go API :8080 -> PostgreSQL :5432 in WSL
```

## One-time PostgreSQL setup

Install the native Ubuntu server package (the client package alone is not enough):

```sh
sudo apt update
sudo apt install -y postgresql-16
sudo service postgresql start
sudo -u postgres psql -c "CREATE ROLE evalplane_dev LOGIN PASSWORD 'evalplane_local_only';"
sudo -u postgres createdb -O evalplane_dev evalplane_dev
```

Use an ignored root `.env.local` for Next.js and for shell-sourced backend values:

```dotenv
NEXT_PUBLIC_API_BASE_URL=http://localhost:8080
PORT=8080
DATABASE_URL=postgres://evalplane_dev:evalplane_local_only@localhost:5432/evalplane_dev?sslmode=disable
APP_ORIGIN=http://localhost:3000
ENVIRONMENT=development
LOG_LEVEL=info
COOKIE_SECURE=false
ALLOW_PRIVATE_AGENT_ENDPOINTS=true
AGENT_CREDENTIAL_ENCRYPTION_KEY=<standard-base64 encoding of exactly 32 random bytes>
```

Generate the encryption value once with `openssl rand -base64 32`. Keep the file
local: `.env.local` is ignored. The API and any later worker process must source
the same file.

Apply and inspect migrations:

```sh
cd ~/projects/test-infra-agents
set -a; source .env.local; set +a
cd backend
go run ./cmd/migrate up
go run ./cmd/migrate version
```

## Normal startup

Terminal 1 — PostgreSQL and API:

```sh
sudo service postgresql start
cd ~/projects/test-infra-agents
set -a; source .env.local; set +a
cd backend
go run ./cmd/api
```

Terminal 2 — frontend:

```sh
cd ~/projects/test-infra-agents
npm run dev
```

Check `http://localhost:8080/healthz`, then
`http://localhost:8080/readyz`, then visit `http://localhost:3000/signup`.

The worker is not needed for signup, login, workspace/billing reads, Agents,
capability claims, task browsing, or qualification planning. It is required to
advance queued Trials, and its current host process requires Kubernetes. Do not
start it for ordinary authenticated UI review.

For a real local connection target while reviewing Agent creation, run the
deterministic reference fixture in a third terminal:

```sh
cd ~/projects/test-infra-agents/backend
go run ./cmd/referenceagent
```

Then use `http://localhost:8081` as the Agent endpoint. This fixture validates
connection and UI plumbing; it is not competence or qualification evidence.
