# How It Works

## Workflows and instances

A **workflow** is a directed acyclic graph of steps defined in a YAML file. You start a new **instance** of a workflow via the UI or REST API — each instance runs independently with its own state tracked in PostgreSQL.

## The advance loop

When an instance is created (or whenever any step completes), the engine runs `advance`:

1. Collect all completed steps.
2. For every `pending` step whose `depends_on` list is fully satisfied, activate it according to its trigger type.
3. If a step completes synchronously (auto, http), repeat from step 1.

This continues recursively until no more steps can be activated — at which point the instance either stays `running` (waiting on a manual or queueti step) or transitions to `completed` / `failed`.

## Step trigger types

| Trigger | What happens when activated |
|---|---|
| `manual` | Enters `waiting_manual`. A human triggers it via the UI or `POST /api/v1/instances/:id/steps/:step/trigger`. |
| `auto` | Completes immediately. The merged dependency output becomes its own output. |
| `http` | Makes the configured HTTP request. The response body (JSON or wrapped as `{"body":"..."}`) becomes its output. A non-2xx response marks the step failed. |
| `queueti` | Enters `waiting_queueti`. Completes when a message arrives on the configured queue-ti topic. |

## Output data flow

Every step produces an **output** (a JSON value). Downstream steps receive a merged object keyed by the names of their completed dependencies:

```
Step A output: {"status": "ok"}

Step B (depends_on: [a]) receives input: {"a": {"status": "ok"}}

Step C (depends_on: [a, b]) receives input: {"a": {...}, "b": {...}}
```

For `manual` steps, you supply the output in the trigger request body:

```bash
curl -X POST http://localhost:8081/api/v1/instances/$ID/steps/approve/trigger \
  -H 'Content-Type: application/json' \
  -d '{"output": {"decision": "approved", "reviewer": "alice"}}'
```

Setting `input` statically in the workflow YAML overrides this merge for a specific step.

## Request flow

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

- The **Poller** opens a long-lived gRPC Subscribe stream to queue-ti and calls `Engine.TriggerStep` when a message arrives.
- The **publisher** sends step output to a queue-ti topic after the step completes (`publish_to_topic` field).
- All workflow state lives in the `queuetask` PostgreSQL schema; the `public` schema is used by queue-ti.

## Automatic instance creation

You don't have to call the API to start instances. Workflows can declare **triggers** that create instances automatically:

| Trigger type | How |
|---|---|
| `cron` | On a 5-field cron schedule |
| `webhook` | When `POST /api/v1/workflows/:name/webhook` is called |
| `queueti` | When a message arrives on a configured topic |

See the [Workflow YAML Reference](../reference/workflow#triggers) for details.
