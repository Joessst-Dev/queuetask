package notify

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// EventType identifies the kind of workflow lifecycle event.
type EventType string

const (
	EventStepWaitingManual EventType = "step.waiting_manual"
	EventInstanceCompleted EventType = "instance.completed"
	EventInstanceFailed    EventType = "instance.failed"
)

// WorkflowNotifConfig holds the per-workflow notification settings derived from
// a workflow definition (via the engine's convertNotifConfig helper).
type WorkflowNotifConfig struct {
	On    []string
	Email *EmailTarget
	SMS   *SMSTarget
}

// EmailTarget lists the email addresses to notify.
type EmailTarget struct{ To []string }

// SMSTarget lists the phone numbers to notify.
type SMSTarget struct{ To []string }

// Event carries all information needed to render and dispatch a notification.
type Event struct {
	Type         EventType
	WorkflowName string
	InstanceID   uuid.UUID
	StepName     string
	Timestamp    time.Time
	Config       *WorkflowNotifConfig
}

// Notifier is the abstraction for sending workflow lifecycle notifications.
type Notifier interface {
	Notify(ctx context.Context, event Event) error
}

// Tester is an optional interface a Notifier may implement to support
// synchronous test delivery with aggregated error reporting.
type Tester interface {
	Test(ctx context.Context, emailTo, smsTo []string) error
}

// Noop is a no-op Notifier used when notifications are disabled.
type Noop struct{}

func (Noop) Notify(_ context.Context, _ Event) error { return nil }
