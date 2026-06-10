ALTER TABLE queuetask.step_executions
    DROP COLUMN IF EXISTS http_method,
    DROP COLUMN IF EXISTS http_url,
    DROP COLUMN IF EXISTS http_headers;
