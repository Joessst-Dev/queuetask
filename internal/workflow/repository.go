package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type StepStatus string

const (
	StatusPending        StepStatus = "pending"
	StatusWaitingManual  StepStatus = "waiting_manual"
	StatusWaitingQueueTi StepStatus = "waiting_queueti"
	StatusRunning        StepStatus = "running"
	StatusCompleted      StepStatus = "completed"
	StatusFailed         StepStatus = "failed"
)

type InstanceStatus string

const (
	InstancePending   InstanceStatus = "pending"
	InstanceRunning   InstanceStatus = "running"
	InstanceCompleted InstanceStatus = "completed"
	InstanceFailed    InstanceStatus = "failed"
)

type Instance struct {
	ID           uuid.UUID
	WorkflowName string
	Status       InstanceStatus
	Input        json.RawMessage
	Context      json.RawMessage
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type StepExecution struct {
	ID           uuid.UUID
	InstanceID   uuid.UUID
	StepName     string
	StepOrder    int
	Status       StepStatus
	TriggerType  TriggerType
	DependsOn    []string
	PublishTopic string
	QueueTiTopic string
	QueueTiGroup string
	HTTPMethod   string
	HTTPURL      string
	HTTPHeaders  map[string]string
	Input        json.RawMessage
	Output       json.RawMessage
	ErrorMessage string
	StartedAt    *time.Time
	CompletedAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

func (r *Repository) CreateInstance(ctx context.Context, workflowName string, input json.RawMessage) (*Instance, error) {
	id := uuid.New()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO queuetask.workflow_instances (id, workflow_name, status, input)
		 VALUES ($1, $2, $3, $4)`,
		id, workflowName, InstanceRunning, input,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting instance: %w", err)
	}
	return r.GetInstance(ctx, id)
}

func (r *Repository) GetInstance(ctx context.Context, id uuid.UUID) (*Instance, error) {
	var inst Instance
	var input, ctxBytes []byte
	err := r.db.QueryRowContext(ctx,
		`SELECT id, workflow_name, status, input, context, created_at, updated_at
		 FROM queuetask.workflow_instances WHERE id = $1`, id,
	).Scan(&inst.ID, &inst.WorkflowName, &inst.Status, &input, &ctxBytes, &inst.CreatedAt, &inst.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("querying instance: %w", err)
	}
	inst.Input = input
	inst.Context = ctxBytes
	return &inst, nil
}

func (r *Repository) ListInstances(ctx context.Context) ([]*Instance, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, workflow_name, status, input, context, created_at, updated_at
		 FROM queuetask.workflow_instances ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}
	defer rows.Close()

	var out []*Instance
	for rows.Next() {
		var inst Instance
		var input, ctxBytes []byte
		if err := rows.Scan(&inst.ID, &inst.WorkflowName, &inst.Status, &input, &ctxBytes, &inst.CreatedAt, &inst.UpdatedAt); err != nil {
			return nil, err
		}
		inst.Input = input
		inst.Context = ctxBytes
		out = append(out, &inst)
	}
	return out, rows.Err()
}

func (r *Repository) UpdateInstanceStatus(ctx context.Context, id uuid.UUID, status InstanceStatus) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE queuetask.workflow_instances SET status = $1, updated_at = now() WHERE id = $2`,
		status, id,
	)
	return err
}

func (r *Repository) CreateSteps(ctx context.Context, instanceID uuid.UUID, steps []StepDef) error {
	for i, s := range steps {
		var httpMethod, httpURL string
		var httpHeaders []byte
		if s.HTTP != nil {
			httpMethod = s.HTTP.Method
			httpURL = s.HTTP.URL
			if len(s.HTTP.Headers) > 0 {
				httpHeaders, _ = json.Marshal(s.HTTP.Headers)
			}
		}
		_, err := r.db.ExecContext(ctx,
			`INSERT INTO queuetask.step_executions
			 (instance_id, step_name, step_order, status, trigger_type, depends_on,
			  publish_topic, queueti_topic, queueti_group, http_method, http_url, http_headers)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			instanceID, s.Name, i, StatusPending, s.Trigger,
			pq.Array(coalesceSlice(s.DependsOn)),
			nullStr(s.PublishToTopic), nullStr(s.QueueTiTopic), nullStr(s.QueueTiConsumerGrp),
			nullStr(httpMethod), nullStr(httpURL), nullBytes(httpHeaders),
		)
		if err != nil {
			return fmt.Errorf("inserting step %s: %w", s.Name, err)
		}
	}
	return nil
}

func (r *Repository) ListSteps(ctx context.Context, instanceID uuid.UUID) ([]*StepExecution, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, instance_id, step_name, step_order, status, trigger_type,
		        depends_on, publish_topic, queueti_topic, queueti_group,
		        http_method, http_url, http_headers,
		        input, output, error_message, started_at, completed_at, created_at, updated_at
		 FROM queuetask.step_executions WHERE instance_id = $1 ORDER BY step_order`,
		instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing steps: %w", err)
	}
	defer rows.Close()

	var out []*StepExecution
	for rows.Next() {
		s, err := scanStep(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) GetStep(ctx context.Context, instanceID uuid.UUID, stepName string) (*StepExecution, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, instance_id, step_name, step_order, status, trigger_type,
		        depends_on, publish_topic, queueti_topic, queueti_group,
		        http_method, http_url, http_headers,
		        input, output, error_message, started_at, completed_at, created_at, updated_at
		 FROM queuetask.step_executions WHERE instance_id = $1 AND step_name = $2`,
		instanceID, stepName,
	)
	return scanStep(row)
}

func (r *Repository) UpdateStepStatus(ctx context.Context, instanceID uuid.UUID, stepName string, status StepStatus, output json.RawMessage, errMsg string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE queuetask.step_executions
		 SET status = $1,
		     input  = CASE WHEN $1 IN ('running','waiting_manual','waiting_queueti') THEN $2 ELSE input END,
		     output = CASE WHEN $1 IN ('completed','failed') THEN $2 ELSE output END,
		     error_message = $3,
		     started_at    = CASE WHEN $1 IN ('running','completed') AND started_at IS NULL THEN now() ELSE started_at END,
		     completed_at  = CASE WHEN $1 IN ('completed','failed') THEN now() ELSE completed_at END,
		     updated_at    = now()
		 WHERE instance_id = $4 AND step_name = $5`,
		status, output, nullStr(errMsg), instanceID, stepName,
	)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanStep(row scanner) (*StepExecution, error) {
	var s StepExecution
	var publishTopic, queuetiTopic, queuetiGroup sql.NullString
	var httpMethod, httpURL sql.NullString
	var httpHeaders []byte
	var errMsg sql.NullString
	var input, output []byte
	if err := row.Scan(
		&s.ID, &s.InstanceID, &s.StepName, &s.StepOrder, &s.Status, &s.TriggerType,
		pq.Array(&s.DependsOn), &publishTopic, &queuetiTopic, &queuetiGroup,
		&httpMethod, &httpURL, &httpHeaders,
		&input, &output, &errMsg, &s.StartedAt, &s.CompletedAt, &s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scanning step: %w", err)
	}
	s.PublishTopic = publishTopic.String
	s.QueueTiTopic = queuetiTopic.String
	s.QueueTiGroup = queuetiGroup.String
	s.HTTPMethod = httpMethod.String
	s.HTTPURL = httpURL.String
	if len(httpHeaders) > 0 {
		_ = json.Unmarshal(httpHeaders, &s.HTTPHeaders)
	}
	s.ErrorMessage = errMsg.String
	s.Input = input
	s.Output = output
	return &s, nil
}

func coalesceSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
