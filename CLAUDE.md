# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build -o ./bin/queuetask ./cmd/server

# Run
go run ./cmd/server

# Test all packages (spins up Docker containers via testcontainers)
go test ./... -v

# Test a single package
go test ./internal/workflow/... -v -timeout 120s

# Run a single Ginkgo spec by label or description substring
go test ./internal/workflow/... -v --ginkgo.focus "completes a manual step"

# Tidy modules
go mod tidy
```

## Architecture

The application is a workflow orchestration service. Users define ordered step sequences in YAML files; the engine tracks state in PostgreSQL and exposes a Fiber HTTP API.

### Request / data flow

```
HTTP request → internal/api (Fiber handlers)
                    ↓
              internal/workflow.Engine   ←→   internal/publisher.Publisher
                    ↓                              (queue-ti gRPC producer)
              internal/workflow.Repository
                    ↓
              PostgreSQL (queuetask schema)

queue-ti gRPC stream → internal/workflow.Poller
                              ↓
                        Engine.TriggerStep
```

### Workflow execution model

Steps are defined in `workflows/*.yaml` and loaded into the in-memory `workflow.Registry` at startup (hot-reloadable via `POST /api/v1/workflows/reload`).

Each step has one of three trigger types:
- `manual` — transitions to `waiting_manual`; advanced by `POST /api/v1/instances/:id/steps/:step/trigger`
- `auto` — completes immediately when all `depends_on` steps are done; output flows forward as the next step's input
- `queueti` — transitions to `waiting_queueti`; the `Poller` opens a gRPC Subscribe stream to the configured queue-ti topic and calls `Engine.TriggerStep` when a message arrives

`Engine.advance` is the core recursive function. After any step completes it re-evaluates all `pending` steps to find newly unblocked ones.

Step output is merged into the next step's input as `{"step-name": <output>}`.

### queue-ti integration

The queue-ti Go client (`github.com/Joessst-Dev/queue-ti/clients/go-client`) communicates over gRPC (default port 50051). Auth is handled by `queueti.NewAuth`, which calls the queue-ti HTTP admin API and wires automatic JWT refresh.

**Dependency note:** queue-ti is a go.work monorepo and is not published to the Go module proxy. `go.mod` uses `replace` directives pointing to a local clone at `../queue-ti`:
```
replace (
    github.com/Joessst-Dev/queue-ti           => ../queue-ti/backend
    github.com/Joessst-Dev/queue-ti/clients/go-client => ../queue-ti/clients/go-client
)
```
The local clone must be present for `go build` and `go mod tidy` to work.

### Database

All tables live in the `queuetask` schema (isolated from the queue-ti `public` schema). Migrations are embedded in `internal/db/migrations/` and run automatically on startup. The `queuetask` schema is created manually before golang-migrate runs because the migration driver needs the schema to exist to create its own tracking table (`queuetask.schema_migrations`).

PostgreSQL arrays (`depends_on TEXT[]`) require `pq.Array()` from `github.com/lib/pq` for both insert and scan — the `jackc/pgx` stdlib adapter does not handle `[]string` natively.

### Testing

Both test suites (`internal/db` and `internal/workflow`) use Ginkgo + testcontainers. Each suite spins up a fresh `postgres:16-alpine` container in `BeforeSuite` and runs `db.Migrate` against it. Docker must be running locally.

The `publisher.Noop{}` and a `nil` poller are used in engine tests to avoid any queue-ti dependency.
