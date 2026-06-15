package notify

import (
	"log/slog"

	"github.com/Joessst-Dev/queuetask/internal/config"
)

// Build constructs a Notifier from cfg. Returns Noop when no providers are configured.
func Build(cfg config.NotificationsConfig) Notifier {
	if cfg.Email.Provider == "" && cfg.SMS.Provider == "" {
		return Noop{}
	}

	var emailSender EmailSender
	switch cfg.Email.Provider {
	case "smtp":
		emailSender = NewSMTPSender(
			cfg.Email.SMTP.Host,
			cfg.Email.SMTP.Port,
			cfg.Email.SMTP.Username,
			cfg.Email.SMTP.Password,
			cfg.Email.SMTP.From,
		)
	case "sendgrid":
		emailSender = NewSendGridSender(cfg.Email.SendGrid.APIKey, cfg.Email.SendGrid.From)
	case "mailgun":
		emailSender = NewMailgunSender(cfg.Email.Mailgun.APIKey, cfg.Email.Mailgun.Domain, cfg.Email.Mailgun.From)
	default:
		if cfg.Email.Provider != "" {
			slog.Warn("notifications: unknown email provider, email disabled", "provider", cfg.Email.Provider)
		}
	}

	var smsSender SMSSender
	switch cfg.SMS.Provider {
	case "twilio":
		smsSender = NewTwilioSender(cfg.SMS.Twilio.AccountSID, cfg.SMS.Twilio.AuthToken, cfg.SMS.Twilio.From)
	case "vonage":
		smsSender = NewVonageSender(cfg.SMS.Vonage.APIKey, cfg.SMS.Vonage.APISecret, cfg.SMS.Vonage.From)
	default:
		if cfg.SMS.Provider != "" {
			slog.Warn("notifications: unknown SMS provider, SMS disabled", "provider", cfg.SMS.Provider)
		}
	}

	return NewWorkflowNotifier(emailSender, smsSender)
}
