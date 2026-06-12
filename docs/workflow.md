# Workflow YAML Reference

Workflow definitions live as `.yaml` files in the directory configured by `workflows.dir` (default `./workflows`). Each file describes one workflow.

---

## Top-level fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Unique workflow identifier. Used in API paths. |
| `version` | int | no | Optional version number, shown in the UI. |
| `description` | string | no | Human-readable description. |
| `triggers` | list | no | Automatic instance-creation rules. See [Triggers](#triggers). |
| `steps` | list | yes | Ordered list of steps. See [Steps](#steps). |
| `notifications` | object | no | Lifecycle event alerts. See [Notifications](#notifications). |

---

## Triggers

The `triggers` array defines how instances are created automatically. A workflow may have zero, one, or multiple triggers of different types.

### cron

Creates a new instance on a schedule.

| Field | Required | Description |
|---|---|---|
| `type` | yes | `cron` |
| `schedule` | yes | Standard 5-field cron expression (`minute hour dom month dow`) |
| `input` | no | Static JSON value passed as the instance input |

```yaml
triggers:
  - type: cron
    schedule: "0 9 * * 1-5"   # 09:00 Mon–Fri
    input:
      source: scheduled
```

### webhook

Creates a new instance when `POST /api/v1/workflows/:name/webhook` is called. The request body becomes the instance input.

| Field | Required | Description |
|---|---|---|
| `type` | yes | `webhook` |

```yaml
triggers:
  - type: webhook
```

```bash
curl -X POST http://localhost:8081/api/v1/workflows/my-workflow/webhook \
  -H 'Content-Type: application/json' \
  -d '{"ref": "abc123", "source": "github"}'
```

### queueti

Creates a new instance when a message arrives on a queue-ti topic. The message payload becomes the instance input.

| Field | Required | Description |
|---|---|---|
| `type` | yes | `queueti` |
| `topic` | yes | queue-ti topic name to consume |
| `consumer_group` | no | Consumer group name (recommended for reliable delivery) |

```yaml
triggers:
  - type: queueti
    topic: order-received
    consumer_group: queuetask-order-processing
```

---

## Steps

The `steps` array defines the work to be done. Steps activate when all their `depends_on` dependencies have completed. A step with no `depends_on` activates immediately when the instance is created.

### Step fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Unique within the workflow. No URL-unsafe characters: `/`, space, `?`, `#`, `&`, `%`, `+`. |
| `description` | string | no | Label shown in the UI. |
| `trigger` | string | no | `manual` \| `auto` \| `queueti` \| `http`. Defaults to `manual`. |
| `depends_on` | list | no | Names of steps that must complete before this one activates. |
| `input` | any | no | Static input value. Overrides the merged dependency outputs when set. |
| `publish_to_topic` | string | no | queue-ti topic to publish this step's output to on completion. |
| `queueti_topic` | string | yes (trigger: queueti) | Topic to consume a message from. |
| `queueti_consumer_group` | string | no | Consumer group for the queueti subscription. |
| `http` | object | yes (trigger: http) | HTTP step configuration. See below. |

### HTTP step fields (`http`)

| Field | Type | Required | Description |
|---|---|---|---|
| `url` | string | yes | Full URL with `http://` or `https://` scheme. Private/loopback IP ranges are blocked at execution time. |
| `method` | string | no | HTTP verb. Defaults to `POST`. Accepted: `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`. |
| `headers` | map | no | Additional request headers. |

```yaml
steps:
  - name: call-api
    trigger: http
    depends_on: [submit]
    http:
      url: https://api.example.com/process
      method: POST
      headers:
        Authorization: "Bearer secret-token"
        X-Source: queuetask
```

### Trigger type behaviour

| Trigger | What happens when activated |
|---|---|
| `manual` | Step enters `waiting_manual`. Advance it via the UI or `POST /api/v1/instances/:id/steps/:step/trigger`. |
| `auto` | Step completes immediately. Its merged dependency output becomes its own output. |
| `queueti` | Step enters `waiting_queueti`. Completes when a message arrives on `queueti_topic`. |
| `http` | Step makes the configured HTTP request. The response body (parsed as JSON, or wrapped as `{"body":"..."}`) becomes its output. A non-2xx response or network error marks the step failed. |

---

## Input / output data flow

When a step completes it produces an **output** (a JSON value). Downstream steps that list it in `depends_on` receive a merged object:

```
step-b receives: {"step-a": <step-a output>}
step-c (depends on a and b) receives: {"step-a": <a output>, "step-b": <b output>}
```

Setting `input` on a step overrides this merge with a static value. This is useful for the first step of a branch that should not inherit upstream outputs.

For `manual` steps the trigger API accepts an `output` body that flows forward:

```bash
curl -X POST http://localhost:8081/api/v1/instances/:id/steps/approve/trigger \
  -H 'Content-Type: application/json' \
  -d '{"output": {"decision": "approved", "reviewer": "alice"}}'
```

---

## Notifications

The `notifications` block configures email and SMS alerts for workflow lifecycle events.

```yaml
notifications:
  on:
    - instance.completed
    - instance.failed
    - step.waiting_manual
  email:
    to:
      - ops@example.com
      - oncall@example.com
  sms:
    to:
      - "+15550001234"
```

### Event types

| Event | When it fires |
|---|---|
| `step.waiting_manual` | A manual step has been activated and is waiting for a trigger |
| `instance.completed` | All steps completed successfully |
| `instance.failed` | Any step failed |
| `*` | Wildcard — matches all event types |

Email and SMS providers are configured at the service level. See [notifications.md](notifications.md).

---

## Annotated full example

```yaml
name: onboarding
version: 2
description: New employee onboarding pipeline

# ── Instance triggers ─────────────────────────────────────────────────────────
triggers:
  # Webhook: POST /api/v1/workflows/onboarding/webhook with new-hire JSON
  - type: webhook

# ── Steps ─────────────────────────────────────────────────────────────────────
steps:
  # First step — no depends_on, activates immediately on instance creation.
  - name: validate-request
    description: Auto-validate the incoming request
    trigger: auto

  # Manual approval gate — waits for HR to trigger via UI or API.
  - name: hr-approval
    description: HR approves the onboarding request
    trigger: manual
    depends_on: [validate-request]

  # HTTP step — calls an external provisioning API after HR approves.
  - name: provision-accounts
    description: Create accounts in downstream systems
    trigger: http
    depends_on: [hr-approval]
    http:
      url: https://provisioning.internal.example.com/onboard
      method: POST
      headers:
        Authorization: "Bearer ${PROVISIONING_TOKEN}"

  # queue-ti step — waits for provisioning confirmation message.
  - name: await-confirmation
    description: Wait for provisioning system to confirm via message queue
    trigger: queueti
    depends_on: [provision-accounts]
    queueti_topic: provisioning-results
    queueti_consumer_group: queuetask-onboarding

  # Final auto step — publishes a welcome event.
  - name: send-welcome
    description: Send welcome message to the new employee
    trigger: auto
    depends_on: [await-confirmation]
    publish_to_topic: welcome-events

# ── Notifications ─────────────────────────────────────────────────────────────
notifications:
  on:
    - step.waiting_manual   # Ping HR when the approval step is ready
    - instance.completed
    - instance.failed
  email:
    to:
      - hr@example.com
  sms:
    to:
      - "+15550001234"
```
