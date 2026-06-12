package notify

import "context"

// EmailSender is the interface implemented by SMTP, SendGrid, and Mailgun senders.
type EmailSender interface {
	SendEmail(ctx context.Context, to, subject, body string) error
}

// SMSSender is the interface implemented by Twilio and Vonage senders.
type SMSSender interface {
	SendSMS(ctx context.Context, to, body string) error
}
