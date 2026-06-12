// Package notify provides workflow lifecycle notification delivery via email and SMS.
package notify

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// EventType identifies the kind of workflow lifecycle event.
type EventType string

const (
	EventStepWaitingManual EventType = "step.waiting_manual" // a manual step is awaiting a trigger
	EventInstanceCompleted EventType = "instance.completed"  // all steps completed successfully
	EventInstanceFailed    EventType = "instance.failed"     // at least one step failed
)

// WorkflowNotifConfig holds the per-workflow notification settings derived from
// a workflow definition. On lists the event types to subscribe to; use "*" to
// receive all event types regardless of their name.
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
// Implementations must be safe for concurrent use. The engine calls Notify in a
// goroutine (fire-and-forget); implementations should always return nil and log
// delivery errors rather than surfacing them to the caller.
type Notifier interface {
	Notify(ctx context.Context, event Event) error
}

// Tester is an optional interface a Notifier may implement to support
// synchronous test delivery with aggregated error reporting. It is used by
// the POST /api/v1/notifications/test endpoint to verify provider credentials.
// Unlike Notify, Test returns a joined error from all failed deliveries.
type Tester interface {
	Test(ctx context.Context, emailTo, smsTo []string) error
}

// Noop is a no-op Notifier used when notifications are disabled.
type Noop struct{}

func (Noop) Notify(_ context.Context, _ Event) error { return nil }
