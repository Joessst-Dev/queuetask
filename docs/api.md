# REST API Reference

Base URL: `http://localhost:8081` (default port; see `server.port` in [configuration.md](configuration.md)).

Error responses follow the shape `{"error": "description"}` with an appropriate HTTP status code.

---

## Health

### GET /health

Returns the service health status, including a database connectivity check.

**Response 200** — healthy:
```json
{
  "status": "ok",
  "checks": { "database": "ok" }
}
```

**Response 503** — degraded:
```json
{
  "status": "degraded",
  "checks": { "database": "connection refused" }
}
```

```bash
curl http://localhost:8081/health
```

---

## Workflows

### GET /api/v1/workflows

Returns all loaded workflow definitions.

**Response 200** — array of workflow definition objects.

```bash
curl http://localhost:8081/api/v1/workflows
```

---

### GET /api/v1/workflows/:name

Returns a single workflow definition by name.

**Response 200** — workflow definition object.
**Response 404** — `{"error": "workflow not found"}`

```bash
curl http://localhost:8081/api/v1/workflows/demo
```

---

### POST /api/v1/workflows/reload

Hot-reloads all `.yaml` files from `workflows.dir`. Any workflow with a validation error is skipped and logged; previously loaded valid definitions remain active.

**Response 200**:
```json
{ "loaded": 3 }
```

```bash
curl -X POST http://localhost:8081/api/v1/workflows/reload
```

---

## Instances

### POST /api/v1/workflows/:name/instances

Creates and starts a new instance of the named workflow.

**Request body** (optional):
```json
{ "input": <any JSON value> }
```

**Response 201** — the created instance:
```json
{
  "ID": "550e8400-e29b-41d4-a716-446655440000",
  "WorkflowName": "demo",
  "Status": "running",
  "Input": null,
  "Context": null,
  "CreatedAt": "2026-06-12T10:00:00Z",
  "UpdatedAt": "2026-06-12T10:00:00Z"
}
```

**Response 422** — workflow not found or engine error.

```bash
# No input
curl -s -X POST http://localhost:8081/api/v1/workflows/demo/instances | jq .

# With input
curl -s -X POST http://localhost:8081/api/v1/workflows/onboarding/instances \
  -H 'Content-Type: application/json' \
  -d '{"input": {"employee_id": "E-1042", "start_date": "2026-07-01"}}' | jq .
```

---

### POST /api/v1/workflows/:name/webhook

Creates a new instance using the request body directly as input. The workflow must declare a `webhook` trigger; returns 422 otherwise.

**Request body** — any JSON (becomes the instance `Input`).
**Response 201** — same shape as `StartInstance`.

```bash
curl -s -X POST http://localhost:8081/api/v1/workflows/triggers-example/webhook \
  -H 'Content-Type: application/json' \
  -d '{"source": "github", "ref": "refs/heads/main"}' | jq .
```

---

### GET /api/v1/instances

Returns all instances across all workflows.

**Response 200** — array of instance objects.

```bash
curl http://localhost:8081/api/v1/instances | jq .
```

---

### GET /api/v1/instances/:id

Returns a single instance with its steps.

**Response 200**:
```json
{
  "instance": {
    "ID": "550e8400-e29b-41d4-a716-446655440000",
    "WorkflowName": "demo",
    "Status": "running",
    "Input": null,
    "Context": null,
    "CreatedAt": "2026-06-12T10:00:00Z",
    "UpdatedAt": "2026-06-12T10:00:01Z"
  },
  "steps": [
    {
      "ID": "...",
      "InstanceID": "550e8400-e29b-41d4-a716-446655440000",
      "StepName": "submit",
      "StepOrder": 0,
      "Status": "waiting_manual",
      "TriggerType": "manual",
      "DependsOn": [],
      "PublishTopic": "",
      "QueueTiTopic": "",
      "QueueTiGroup": "",
      "HTTPMethod": "",
      "HTTPURL": "",
      "HTTPHeaders": null,
      "StaticInput": null,
      "Input": null,
      "Output": null,
      "ErrorMessage": "",
      "StartedAt": null,
      "CompletedAt": null,
      "CreatedAt": "2026-06-12T10:00:00Z",
      "UpdatedAt": "2026-06-12T10:00:01Z"
    }
  ]
}
```

**Response 404** — instance not found.

```bash
ID=550e8400-e29b-41d4-a716-446655440000
curl http://localhost:8081/api/v1/instances/$ID | jq .
```

---

### GET /api/v1/instances/:id/steps

Returns just the steps array for an instance.

**Response 200** — array of step execution objects.

```bash
curl http://localhost:8081/api/v1/instances/$ID/steps | jq .
```

---

### POST /api/v1/instances/:id/steps/:step/trigger

Advances a step that is in `waiting_manual` or `waiting_queueti` status.

**Request body** (optional):
```json
{ "output": <any JSON value> }
```

The `output` value flows forward as this step's output to downstream steps.

**Response 200** — updated steps array for the instance.
**Response 422** — step not in a waiting state, or engine error.

```bash
# Trigger with no output
curl -s -X POST http://localhost:8081/api/v1/instances/$ID/steps/submit/trigger | jq .

# Trigger with output (flows to next steps as {"submit": {...}})
curl -s -X POST http://localhost:8081/api/v1/instances/$ID/steps/hr-approval/trigger \
  -H 'Content-Type: application/json' \
  -d '{"output": {"decision": "approved", "reviewer": "alice@example.com"}}' | jq .
```

---

## Notifications

### POST /api/v1/notifications/test

Synchronously sends a test notification to the supplied addresses using the configured providers. Useful for verifying credentials before deploying a workflow with `notifications:`.

Returns 204 immediately if notifications are not configured (no provider set).

**Request body**:
```json
{
  "email": ["ops@example.com"],
  "sms": ["+15550001234"]
}
```

**Response 204** — delivery succeeded (or notifications are disabled).
**Response 500** — delivery failed:
```json
{ "error": "smtp: connection refused; vonage: status 9 Invalid number" }
```

```bash
curl -s -X POST http://localhost:8081/api/v1/notifications/test \
  -H 'Content-Type: application/json' \
  -d '{"email": ["me@example.com"]}' \
  -w "\nHTTP %{http_code}\n"
```

---

## Instance and step status values

**Instance status**

| Value | Meaning |
|---|---|
| `running` | Instance is active (this is also the initial status) |
| `completed` | All steps completed successfully |
| `failed` | At least one step failed |

**Step status**

| Value | Meaning |
|---|---|
| `pending` | Waiting for dependencies to complete |
| `waiting_manual` | Ready, waiting for a manual trigger |
| `waiting_queueti` | Waiting for a queue-ti message |
| `running` | Currently executing (http and auto steps mid-execution) |
| `completed` | Finished successfully |
| `failed` | Finished with an error |
