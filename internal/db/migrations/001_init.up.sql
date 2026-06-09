CREATE SCHEMA IF NOT EXISTS queuetask;

CREATE TABLE queuetask.workflow_definitions (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT        UNIQUE NOT NULL,
    version     INT         NOT NULL DEFAULT 1,
    definition  JSONB       NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE queuetask.workflow_instances (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_name  TEXT        NOT NULL,
    status         TEXT        NOT NULL DEFAULT 'pending',
    input          JSONB,
    context        JSONB,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE queuetask.step_executions (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    instance_id     UUID        NOT NULL REFERENCES queuetask.workflow_instances(id),
    step_name       TEXT        NOT NULL,
    step_order      INT         NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'pending',
    trigger_type    TEXT        NOT NULL DEFAULT 'manual',
    depends_on      TEXT[]      NOT NULL DEFAULT '{}',
    publish_topic   TEXT,
    queueti_topic   TEXT,
    queueti_group   TEXT,
    input           JSONB,
    output          JSONB,
    error_message   TEXT,
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(instance_id, step_name)
);

CREATE INDEX idx_step_executions_instance ON queuetask.step_executions(instance_id);
CREATE INDEX idx_step_executions_waiting  ON queuetask.step_executions(status, trigger_type)
    WHERE status IN ('waiting_manual', 'waiting_queueti');
