package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// WorkflowNotifier fans out notifications to email and SMS senders.
// Delivery failures are logged but never propagated — Notify always returns nil.
type WorkflowNotifier struct {
	email EmailSender
	sms   SMSSender
}

// NewWorkflowNotifier creates a WorkflowNotifier. Either sender may be nil;
// the corresponding channel is silently skipped when nil.
func NewWorkflowNotifier(email EmailSender, sms SMSSender) *WorkflowNotifier {
	return &WorkflowNotifier{email: email, sms: sms}
}

// Notify dispatches the event to all configured recipients concurrently and
// waits for all deliveries to complete. It is fire-and-forget in spirit:
// delivery errors are logged and Notify always returns nil.
func (n *WorkflowNotifier) Notify(ctx context.Context, event Event) error {
	if event.Config == nil {
		return nil
	}
	if !matchesOn(event.Config.On, string(event.Type)) {
		return nil
	}

	subject, emailBody, smsBody, err := renderTemplates(event)
	if err != nil {
		slog.Warn("notify: template render failed", "error", err)
		return nil
	}

	var wg sync.WaitGroup

	if n.email != nil && event.Config.Email != nil {
		for _, to := range event.Config.Email.To {
			wg.Go(func() {
				if err := n.email.SendEmail(ctx, to, subject, emailBody); err != nil {
					slog.Warn("notify: email send failed", "to", to, "error", err)
				}
			})
		}
	}

	if n.sms != nil && event.Config.SMS != nil {
		for _, to := range event.Config.SMS.To {
			wg.Go(func() {
				if err := n.sms.SendSMS(ctx, to, smsBody); err != nil {
					slog.Warn("notify: sms send failed", "to", to, "error", err)
				}
			})
		}
	}

	wg.Wait()
	return nil
}

// Test sends a notification synchronously to the given recipients and returns
// aggregated delivery errors. It is intended for operator verification and
// implements the Tester interface. Unlike Notify, errors are not swallowed.
func (n *WorkflowNotifier) Test(ctx context.Context, emailTo, smsTo []string) error {
	subject, emailBody, smsBody, err := renderTemplates(Event{
		Type:         EventInstanceCompleted,
		WorkflowName: "test",
		Timestamp:    time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("rendering templates: %w", err)
	}
	var errs []error
	if n.email != nil {
		for _, to := range emailTo {
			if err := n.email.SendEmail(ctx, to, subject, emailBody); err != nil {
				errs = append(errs, fmt.Errorf("email to %s: %w", to, err))
			}
		}
	}
	if n.sms != nil {
		for _, to := range smsTo {
			if err := n.sms.SendSMS(ctx, to, smsBody); err != nil {
				errs = append(errs, fmt.Errorf("sms to %s: %w", to, err))
			}
		}
	}
	return errors.Join(errs...)
}

// matchesOn reports whether eventType is covered by the on slice.
// The special value "*" matches any event type.
func matchesOn(on []string, eventType string) bool {
	for _, s := range on {
		if s == "*" || s == eventType {
			return true
		}
	}
	return false
}
