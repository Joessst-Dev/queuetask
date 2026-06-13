# Getting Started

## Prerequisites

- [Docker Desktop](https://www.docker.com/products/docker-desktop/) (or Docker Engine)
- Git

## Quick start with Docker Compose

The fastest way to run queuetask — no Go toolchain needed.

```bash
git clone https://github.com/Joessst-Dev/queuetask
cd queuetask
docker compose up --build
```

After startup:

| Service | URL |
|---|---|
| queuetask UI & API | http://localhost:8081 |
| queue-ti admin API | http://localhost:8080 |
| queue-ti gRPC | localhost:50051 |

Three workflows load automatically. Try the **demo** workflow (no external dependencies):

```bash
# 1. Start an instance
curl -s -X POST http://localhost:8081/api/v1/workflows/demo/instances | jq .

# 2. Open http://localhost:8081, click the instance,
#    then click Trigger on each waiting_manual step.
#    Auto steps complete on their own.
```

## Running locally (Go toolchain)

Requires Go 1.25+, Docker (for tests), and a running PostgreSQL instance.

```bash
# Clone the queue-ti dependency (required by go.mod replace directives)
git clone https://github.com/Joessst-Dev/queue-ti ../queue-ti

# Start a local PostgreSQL instance (or use docker compose up postgres)
# then create a config.yaml — see Configuration for the minimal example

# Build and run
go build -o ./bin/queuetask ./cmd/server
./bin/queuetask
```

## Running tests

Integration tests spin up a fresh PostgreSQL container via testcontainers. Docker must be running.

```bash
# All packages
go test ./... -v -timeout 120s

# Single package
go test ./internal/workflow/... -v -timeout 120s

# Specific spec
go test ./internal/workflow/... -v --ginkgo.focus "completes a manual step"
```

## Next steps

- [How It Works](./how-it-works) — understand the execution model before writing workflows
- [Workflow YAML Reference](../reference/workflow) — write your first workflow
- [Configuration Reference](../reference/configuration) — connect to your own database and providers
