CREATE TABLE queuetask.cron_locks (
    workflow_name  TEXT        NOT NULL,
    trigger_idx    INT         NOT NULL,
    scheduled_at   TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (workflow_name, trigger_idx, scheduled_at)
);
