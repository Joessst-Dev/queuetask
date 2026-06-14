CREATE INDEX idx_workflow_instances_created_id
    ON queuetask.workflow_instances (created_at DESC, id DESC);
