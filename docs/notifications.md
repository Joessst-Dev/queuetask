# Notifications Setup

queuetask can send email and SMS alerts when workflow lifecycle events occur. Notifications are configured at two levels:

1. **Service level** — which provider(s) to use (in `config.yaml`)
2. **Workflow level** — which events to subscribe to and who to contact (in the workflow YAML)

---

## Event types

| Event | When it fires |
|---|---|
| `step.waiting_manual` | A manual step has activated and is waiting for a human trigger |
| `instance.completed` | All steps completed successfully |
| `instance.failed` | At least one step failed, halting the instance |

---

## Workflow YAML

Add a `notifications` block to any workflow:

```yaml
notifications:
  on:
    - instance.completed
    - instance.failed
    - step.waiting_manual
  email:
    to:
      - ops@example.com
      - oncall@example.com
  sms:
    to:
      - "+15550001234"
```

- `on` — list of event types to subscribe to. Omit an event to suppress it.
- `email.to` — list of email addresses. Omitted if no email provider is configured.
- `sms.to` — list of phone numbers in E.164 format (`+15550001234`). Omitted if no SMS provider is configured.

Notifications without recipients (empty `to`) are silently skipped.

You can configure the `notifications` block using the workflow builder UI (the Notifications section) or by editing the YAML directly.

---

## Email providers

Set `notifications.email.provider` in `config.yaml` to one of the values below. Only configure the sub-keys for the active provider.

### SMTP

Works with any SMTP server: Gmail, AWS SES SMTP endpoint, Mailhog for local dev, etc.

```yaml
notifications:
  email:
    provider: smtp
    smtp:
      host: smtp.gmail.com
      port: 587
      username: you@gmail.com
      password: your-app-password     # use an App Password, not your account password
      from: "queuetask <you@gmail.com>"
```

For **AWS SES** (SMTP interface):

```yaml
    smtp:
      host: email-smtp.us-east-1.amazonaws.com
      port: 587
      username: <SES SMTP username>
      password: <SES SMTP password>
      from: noreply@verified-domain.com
```

### SendGrid

Requires a SendGrid account and a verified sender address or domain.

```yaml
notifications:
  email:
    provider: sendgrid
    sendgrid:
      api_key: SG.xxxxxxxxxxxxxxxxxxxx
      from: noreply@example.com
```

### Mailgun

Requires a Mailgun account and a verified sending domain.

```yaml
notifications:
  email:
    provider: mailgun
    mailgun:
      api_key: key-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
      domain: mg.example.com
      from: noreply@mg.example.com
```

---

## SMS providers

Set `notifications.sms.provider` in `config.yaml`.

### Twilio

```yaml
notifications:
  sms:
    provider: twilio
    twilio:
      account_sid: ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
      auth_token:  xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
      from: "+15550001111"   # your Twilio number in E.164 format
```

### Vonage (formerly Nexmo)

```yaml
notifications:
  sms:
    provider: vonage
    vonage:
      api_key:    vkey123
      api_secret: vsecret456
      from: VONAGE             # sender ID (up to 11 alphanumeric chars) or phone number
```

> **Note:** Vonage's SMS API always returns HTTP 200. queuetask inspects the `messages[0].status` field in the response body and treats any non-`"0"` status as an error.

---

## Testing delivery

Before attaching notifications to a workflow, verify your credentials with the test endpoint:

```bash
curl -s -X POST http://localhost:8081/api/v1/notifications/test \
  -H 'Content-Type: application/json' \
  -d '{
    "email": ["you@example.com"],
    "sms":   ["+15550001234"]
  }' \
  -w "\nHTTP %{http_code}\n"
```

| Response | Meaning |
|---|---|
| `HTTP 204` | Delivery succeeded (or notifications are not configured) |
| `HTTP 500` + JSON body | Delivery failed; the `error` field contains provider error details |

The test endpoint sends synchronously and aggregates errors from all recipients, so you can test multiple addresses in one call.
