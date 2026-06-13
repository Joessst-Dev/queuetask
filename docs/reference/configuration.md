# Configuration Reference

queuetask is configured via a YAML file and/or environment variables.

## Config file

Place `config.yaml` in the working directory (or `./config/config.yaml`). Environment variables override file values — Viper maps dotted keys to `UPPER_UNDERSCORE` names (e.g. `db.host` → `DB_HOST`, `queueti.grpc_addr` → `QUEUETI_GRPC_ADDR`).

---

## Server

| Key | Env | Type | Default | Description |
|---|---|---|---|---|
| `server.port` | `SERVER_PORT` | int | `8081` | Port the HTTP server listens on |

---

## Database

queuetask requires PostgreSQL 14+ (tested against 16). All tables are created in the `queuetask` schema automatically on startup.

| Key | Env | Type | Default | Description |
|---|---|---|---|---|
| `db.host` | `DB_HOST` | string | — | Database host |
| `db.port` | `DB_PORT` | int | — | Database port |
| `db.user` | `DB_USER` | string | — | Database user |
| `db.password` | `DB_PASSWORD` | string | — | Database password |
| `db.name` | `DB_NAME` | string | — | Database name |
| `db.sslmode` | `DB_SSLMODE` | string | `disable` | PostgreSQL sslmode (`disable`, `require`, `verify-full`, …) |

---

## Workflows

| Key | Env | Type | Default | Description |
|---|---|---|---|---|
| `workflows.dir` | `WORKFLOWS_DIR` | string | `./workflows` | Directory scanned for `*.yaml` workflow files |

---

## Queue-ti (optional)

All queue-ti integration is disabled when `queueti.enabled` is `false` (the default). Enable it when you need `queueti` step triggers or instance triggers.

| Key | Env | Type | Default | Description |
|---|---|---|---|---|
| `queueti.enabled` | `QUEUETI_ENABLED` | bool | `false` | Enable queue-ti integration |
| `queueti.grpc_addr` | `QUEUETI_GRPC_ADDR` | string | `localhost:50051` | queue-ti gRPC server address |
| `queueti.admin_url` | `QUEUETI_ADMIN_URL` | string | `http://localhost:8080` | queue-ti HTTP admin API URL (used for JWT auth) |
| `queueti.username` | `QUEUETI_USERNAME` | string | — | queue-ti admin username |
| `queueti.password` | `QUEUETI_PASSWORD` | string | — | queue-ti admin password |

---

## Notifications (optional)

Notifications are disabled when `provider` is empty. Only the active provider's sub-keys need to be set. See [notifications.md](./notifications) for provider-specific setup.

### Email

| Key | Env | Type | Description |
|---|---|---|---|
| `notifications.email.provider` | `NOTIFICATIONS_EMAIL_PROVIDER` | string | `smtp`, `sendgrid`, or `mailgun`. Empty = disabled. |

**SMTP** (`provider: smtp`):

| Key | Type | Description |
|---|---|---|
| `notifications.email.smtp.host` | string | SMTP server hostname |
| `notifications.email.smtp.port` | int | SMTP port (typically 587 or 465) |
| `notifications.email.smtp.username` | string | SMTP auth username |
| `notifications.email.smtp.password` | string | SMTP auth password |
| `notifications.email.smtp.from` | string | Sender address (`Name <addr>` or bare address) |

**SendGrid** (`provider: sendgrid`):

| Key | Type | Description |
|---|---|---|
| `notifications.email.sendgrid.api_key` | string | SendGrid API key |
| `notifications.email.sendgrid.from` | string | Verified sender address |

**Mailgun** (`provider: mailgun`):

| Key | Type | Description |
|---|---|---|
| `notifications.email.mailgun.api_key` | string | Mailgun private API key |
| `notifications.email.mailgun.domain` | string | Mailgun sending domain |
| `notifications.email.mailgun.from` | string | Sender address |

### SMS

| Key | Env | Type | Description |
|---|---|---|---|
| `notifications.sms.provider` | `NOTIFICATIONS_SMS_PROVIDER` | string | `twilio` or `vonage`. Empty = disabled. |

**Twilio** (`provider: twilio`):

| Key | Type | Description |
|---|---|---|
| `notifications.sms.twilio.account_sid` | string | Twilio Account SID |
| `notifications.sms.twilio.auth_token` | string | Twilio Auth Token |
| `notifications.sms.twilio.from` | string | Twilio phone number in E.164 format (`+15550001111`) |

**Vonage** (`provider: vonage`):

| Key | Type | Description |
|---|---|---|
| `notifications.sms.vonage.api_key` | string | Vonage API key |
| `notifications.sms.vonage.api_secret` | string | Vonage API secret |
| `notifications.sms.vonage.from` | string | Sender ID or phone number |

---

## Example config.yaml

```yaml
server:
  port: 8081

db:
  host: localhost
  port: 5432
  user: postgres
  password: postgres
  name: queueti
  sslmode: disable

workflows:
  dir: ./workflows

# queue-ti integration (disabled by default)
queueti:
  enabled: false
  grpc_addr: localhost:50051
  admin_url: http://localhost:8080
  username: ""
  password: ""

# Notifications (omit or leave provider empty to disable)
notifications:
  email:
    provider: sendgrid
    sendgrid:
      api_key: SG.xxxxxxxxxxxxxxxxxxxx
      from: noreply@example.com
  sms:
    provider: twilio
    twilio:
      account_sid: ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
      auth_token: xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
      from: "+15550001111"
```
