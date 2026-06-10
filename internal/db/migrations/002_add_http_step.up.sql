ALTER TABLE queuetask.step_executions
    ADD COLUMN http_method  TEXT,
    ADD COLUMN http_url     TEXT,
    ADD COLUMN http_headers JSONB;
