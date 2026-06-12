# queuetask

queuetask is a workflow orchestration service. You define ordered step sequences in YAML files; the engine tracks execution state in PostgreSQL and exposes a REST API and a browser UI for managing and advancing workflow instances. Steps activate automatically when their dependencies complete — manual steps pause for human action, auto steps complete immediately, HTTP steps call an external URL, and queue-ti steps wait for a message on a configured topic.

---

## Quick Start

Requires Docker Desktop (or Engine).

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

Three workflows load automatically. Run the **demo** workflow (no external dependencies):

```bash
# Start an instance
curl -s -X POST http://localhost:8081/api/v1/workflows/demo/instances | jq .

# Open the UI, click the instance, then click Trigger on each waiting_manual step.
# Auto steps complete on their own.
```

---

## How it works

A **workflow** is a directed acyclic graph of steps defined in a YAML file. When an instance is started, the engine evaluates which steps have all their `depends_on` dependencies satisfied and activates them:

- **manual** — transitions to `waiting_manual`; a human triggers it via the UI or API
- **auto** — completes immediately, passing its output as the next step's input
- **http** — makes an outbound HTTP request; the response becomes its output
- **queueti** — waits for a message on a queue-ti topic; the message payload becomes its output

After any step completes, the engine re-evaluates all pending steps recursively until no more can be activated.

Step outputs flow forward: when step B depends on step A, B receives `{"a": <A's output>}` as its input. Multiple dependencies are merged into a single object.

```
HTTP request
    │
    ▼
Fiber handlers  ──────────────────────────────────────────►  PostgreSQL
    │                                                        (queuetask schema)
    ▼
workflow.Engine  ◄──────────────────────────────────────────  queue-ti gRPC
    │                                                         (via Poller)
    ▼
publisher (queue-ti gRPC producer)
```

---

## Writing workflows

Workflows are YAML files in the configured `workflows.dir` (default `./workflows`). Changes are picked up without restart via `POST /api/v1/workflows/reload` or the builder UI.

See **[docs/workflow.md](docs/workflow.md)** for the complete YAML reference and annotated examples.

---

## REST API

See **[docs/api.md](docs/api.md)** for all endpoints with request/response shapes and `curl` examples.

---

## Configuration

Configured via `config.yaml` in the working directory and/or environment variables. See **[docs/configuration.md](docs/configuration.md)** for the full reference.

Minimal `config.yaml`:

```yaml
db:
  host: localhost
  port: 5432
  user: postgres
  password: postgres
  name: queueti
```

---

## Notifications

queuetask can send email and SMS alerts on workflow lifecycle events (`instance.completed`, `instance.failed`, `step.waiting_manual`). See **[docs/notifications.md](docs/notifications.md)** for provider setup.

---

## Development

Requires Go 1.25+ and Docker (for integration tests).

```bash
# Build
go build -o ./bin/queuetask ./cmd/server

# Run all tests (spins up a PostgreSQL container via testcontainers)
go test ./... -v -timeout 120s

# Run a single package's tests
go test ./internal/workflow/... -v -timeout 120s

# Run a specific Ginkgo spec
go test ./internal/workflow/... -v --ginkgo.focus "completes a manual step"

# Hot-reload workflows during development
curl -X POST http://localhost:8081/api/v1/workflows/reload
```

See [CLAUDE.md](CLAUDE.md) for detailed developer guidance including the queue-ti local dependency setup.
