package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// ---- mock helpers ----

type mockEmailSender struct {
	mu    sync.Mutex
	calls []emailSendCall
}

type emailSendCall struct {
	to, subject, body string
}

func (m *mockEmailSender) SendEmail(_ context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, emailSendCall{to, subject, body})
	return nil
}

func (m *mockEmailSender) getCalls() []emailSendCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]emailSendCall, len(m.calls))
	copy(out, m.calls)
	return out
}

type mockSMSSender struct {
	mu    sync.Mutex
	calls []smsSendCall
}

type smsSendCall struct {
	to, body string
}

func (m *mockSMSSender) SendSMS(_ context.Context, to, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, smsSendCall{to, body})
	return nil
}

func (m *mockSMSSender) getCalls() []smsSendCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]smsSendCall, len(m.calls))
	copy(out, m.calls)
	return out
}

// capturedRequest holds the details recorded by mockRoundTripper.
type capturedRequest struct {
	method string
	rawURL string
	header http.Header
	body   string
}

// mockRoundTripper captures outbound HTTP requests and returns a synthetic response.
type mockRoundTripper struct {
	captured *capturedRequest
	status   int    // defaults to 200 when zero
	respBody string // response body returned to the caller; defaults to ""
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		data, _ := io.ReadAll(req.Body)
		body = string(data)
	}
	m.captured = &capturedRequest{
		method: req.Method,
		rawURL: req.URL.String(),
		header: req.Header.Clone(),
		body:   body,
	}
	status := m.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(m.respBody)),
		Header:     make(http.Header),
	}, nil
}

func baseEvent(eventType EventType) Event {
	return Event{
		Type:         eventType,
		WorkflowName: "my-workflow",
		InstanceID:   uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		Timestamp:    time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	}
}

// ---- specs ----

var _ = Describe("WorkflowNotifier", func() {
	var (
		emailMock *mockEmailSender
		smsMock   *mockSMSSender
		notifier  *WorkflowNotifier
	)

	BeforeEach(func() {
		emailMock = &mockEmailSender{}
		smsMock = &mockSMSSender{}
		notifier = NewWorkflowNotifier(emailMock, smsMock)
	})

	Context("when Config is nil", func() {
		It("sends nothing and returns nil", func() {
			event := baseEvent(EventInstanceCompleted)
			event.Config = nil

			Expect(notifier.Notify(context.Background(), event)).To(Succeed())
			Expect(emailMock.getCalls()).To(BeEmpty())
			Expect(smsMock.getCalls()).To(BeEmpty())
		})
	})

	Context("when event type is not in On list", func() {
		It("sends nothing", func() {
			event := baseEvent(EventInstanceCompleted)
			event.Config = &WorkflowNotifConfig{
				On:    []string{string(EventInstanceFailed)},
				Email: &EmailTarget{To: []string{"user@example.com"}},
			}

			Expect(notifier.Notify(context.Background(), event)).To(Succeed())
			Expect(emailMock.getCalls()).To(BeEmpty())
		})
	})

	Context("when event type matches the On list", func() {
		It("calls email sender once per recipient and SMS sender once per number", func() {
			event := baseEvent(EventInstanceCompleted)
			event.Config = &WorkflowNotifConfig{
				On:    []string{string(EventInstanceCompleted)},
				Email: &EmailTarget{To: []string{"a@example.com", "b@example.com"}},
				SMS:   &SMSTarget{To: []string{"+10000000001"}},
			}

			Expect(notifier.Notify(context.Background(), event)).To(Succeed())
			Expect(emailMock.getCalls()).To(HaveLen(2))
			Expect(smsMock.getCalls()).To(HaveLen(1))
		})

		It("includes the recipient address in the email call", func() {
			event := baseEvent(EventStepWaitingManual)
			event.StepName = "review"
			event.Config = &WorkflowNotifConfig{
				On:    []string{string(EventStepWaitingManual)},
				Email: &EmailTarget{To: []string{"ops@example.com"}},
			}

			Expect(notifier.Notify(context.Background(), event)).To(Succeed())
			calls := emailMock.getCalls()
			Expect(calls).To(HaveLen(1))
			Expect(calls[0].to).To(Equal("ops@example.com"))
			Expect(calls[0].subject).To(ContainSubstring("step.waiting_manual"))
			Expect(calls[0].body).To(ContainSubstring("my-workflow"))
		})
	})

	Context("when On contains the wildcard '*'", func() {
		It("sends regardless of event type", func() {
			event := baseEvent(EventStepWaitingManual)
			event.Config = &WorkflowNotifConfig{
				On:    []string{"*"},
				Email: &EmailTarget{To: []string{"admin@example.com"}},
			}

			Expect(notifier.Notify(context.Background(), event)).To(Succeed())
			Expect(emailMock.getCalls()).To(HaveLen(1))
		})
	})

	Context("when email sender is nil", func() {
		It("skips email and only sends SMS", func() {
			notifier = NewWorkflowNotifier(nil, smsMock)
			event := baseEvent(EventInstanceCompleted)
			event.Config = &WorkflowNotifConfig{
				On:    []string{"*"},
				Email: &EmailTarget{To: []string{"user@example.com"}},
				SMS:   &SMSTarget{To: []string{"+10000000001"}},
			}

			Expect(notifier.Notify(context.Background(), event)).To(Succeed())
			Expect(emailMock.getCalls()).To(BeEmpty())
			Expect(smsMock.getCalls()).To(HaveLen(1))
		})
	})

	Context("when SMS sender is nil", func() {
		It("skips SMS and only sends email", func() {
			notifier = NewWorkflowNotifier(emailMock, nil)
			event := baseEvent(EventInstanceCompleted)
			event.Config = &WorkflowNotifConfig{
				On:    []string{"*"},
				Email: &EmailTarget{To: []string{"user@example.com"}},
				SMS:   &SMSTarget{To: []string{"+10000000001"}},
			}

			Expect(notifier.Notify(context.Background(), event)).To(Succeed())
			Expect(emailMock.getCalls()).To(HaveLen(1))
			Expect(smsMock.getCalls()).To(BeEmpty())
		})
	})
})

var _ = Describe("SendGridSender", func() {
	It("posts to the SendGrid v3 endpoint with the correct auth header and JSON body", func() {
		rt := &mockRoundTripper{}
		sender := NewSendGridSender("sg-test-key", "from@example.com")
		sender.client = &http.Client{Transport: rt}

		err := sender.SendEmail(context.Background(), "to@example.com", "Hello", "World")
		Expect(err).NotTo(HaveOccurred())

		Expect(rt.captured.method).To(Equal(http.MethodPost))
		Expect(rt.captured.rawURL).To(Equal("https://api.sendgrid.com/v3/mail/send"))
		Expect(rt.captured.header.Get("Authorization")).To(Equal("Bearer sg-test-key"))

		var payload map[string]any
		Expect(json.Unmarshal([]byte(rt.captured.body), &payload)).To(Succeed())
		personalizations, ok := payload["personalizations"].([]any)
		Expect(ok).To(BeTrue())
		first := personalizations[0].(map[string]any)
		toList := first["to"].([]any)
		firstTo := toList[0].(map[string]any)
		Expect(firstTo["email"]).To(Equal("to@example.com"))
	})

	It("returns an error on a non-2xx response", func() {
		rt := &mockRoundTripper{status: http.StatusUnauthorized}
		sender := NewSendGridSender("bad-key", "from@example.com")
		sender.client = &http.Client{Transport: rt}

		err := sender.SendEmail(context.Background(), "to@example.com", "subj", "body")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("401"))
	})
})

var _ = Describe("MailgunSender", func() {
	It("posts form data to the domain messages endpoint with Basic auth", func() {
		rt := &mockRoundTripper{}
		sender := NewMailgunSender("mg-key", "example.com", "from@example.com")
		sender.client = &http.Client{Transport: rt}

		err := sender.SendEmail(context.Background(), "to@example.com", "Hello", "World")
		Expect(err).NotTo(HaveOccurred())

		Expect(rt.captured.method).To(Equal(http.MethodPost))
		Expect(rt.captured.rawURL).To(Equal("https://api.mailgun.net/v3/example.com/messages"))

		user, pass, ok := parseBasicAuth(rt.captured.header.Get("Authorization"))
		Expect(ok).To(BeTrue())
		Expect(user).To(Equal("api"))
		Expect(pass).To(Equal("mg-key"))

		Expect(rt.captured.body).To(ContainSubstring("to=to%40example.com"))
	})
})

var _ = Describe("TwilioSender", func() {
	It("posts form data to the account messages endpoint with Basic auth", func() {
		rt := &mockRoundTripper{}
		sender := NewTwilioSender("AC123", "auth-token", "+15550001111")
		sender.client = &http.Client{Transport: rt}

		err := sender.SendSMS(context.Background(), "+15559998888", "Hello")
		Expect(err).NotTo(HaveOccurred())

		Expect(rt.captured.method).To(Equal(http.MethodPost))
		Expect(rt.captured.rawURL).To(
			Equal("https://api.twilio.com/2010-04-01/Accounts/AC123/Messages.json"))

		user, pass, ok := parseBasicAuth(rt.captured.header.Get("Authorization"))
		Expect(ok).To(BeTrue())
		Expect(user).To(Equal("AC123"))
		Expect(pass).To(Equal("auth-token"))

		Expect(rt.captured.body).To(ContainSubstring("To="))
		Expect(rt.captured.body).To(ContainSubstring("From="))
	})
})

var _ = Describe("VonageSender", func() {
	It("posts form data to the Nexmo SMS endpoint with api_key and to fields", func() {
		rt := &mockRoundTripper{
			respBody: `{"messages":[{"status":"0","message-id":"abc123"}]}`,
		}
		sender := NewVonageSender("vkey", "vsecret", "VONAGE")
		sender.client = &http.Client{Transport: rt}

		err := sender.SendSMS(context.Background(), "+15559998888", "Hello")
		Expect(err).NotTo(HaveOccurred())

		Expect(rt.captured.method).To(Equal(http.MethodPost))
		Expect(rt.captured.rawURL).To(Equal("https://rest.nexmo.com/sms/json"))
		Expect(rt.captured.body).To(ContainSubstring("api_key=vkey"))
		Expect(rt.captured.body).To(ContainSubstring("to="))
	})

	It("returns an error when the message is rejected (non-zero status)", func() {
		rt := &mockRoundTripper{
			respBody: `{"messages":[{"status":"9","error-text":"Invalid number"}]}`,
		}
		sender := NewVonageSender("vkey", "vsecret", "VONAGE")
		sender.client = &http.Client{Transport: rt}

		err := sender.SendSMS(context.Background(), "+1invalid", "Hello")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("status 9"))
	})
})

var _ = Describe("SMTPSender", func() {
	It("constructs without panicking", func() {
		s := NewSMTPSender("smtp.example.com", 587, "user", "pass", "from@example.com")
		Expect(s).NotTo(BeNil())
	})
})

// parseBasicAuth decodes the Authorization header value produced by req.SetBasicAuth.
func parseBasicAuth(header string) (user, pass string, ok bool) {
	req, err := http.NewRequest(http.MethodGet, "http://x", nil)
	if err != nil {
		return "", "", false
	}
	req.Header.Set("Authorization", header)
	return req.BasicAuth()
}
