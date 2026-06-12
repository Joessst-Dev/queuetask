// Package config loads and exposes the application configuration from a YAML
// file and/or environment variables via Viper.
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config is the top-level application configuration. It is populated by Load
// from config.yaml and/or environment variables (UPPER_UNDERSCORE form of
// dotted keys, e.g. DB_HOST for db.host).
type Config struct {
	Server        ServerConfig
	DB            DBConfig
	Workflows     WorkflowsConfig
	QueueTi       QueueTiConfig
	Notifications NotificationsConfig
}

// NotificationsConfig holds email and SMS notification settings.
// Both Email and SMS are optional; leave Provider empty to disable.
type NotificationsConfig struct {
	Email EmailNotifConfig `mapstructure:"email"`
	SMS   SMSNotifConfig   `mapstructure:"sms"`
}

type EmailNotifConfig struct {
	Provider string      `mapstructure:"provider"`
	SMTP     SMTPCfg     `mapstructure:"smtp"`
	SendGrid SendGridCfg `mapstructure:"sendgrid"`
	Mailgun  MailgunCfg  `mapstructure:"mailgun"`
}

type SMTPCfg struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`
}

type SendGridCfg struct {
	APIKey string `mapstructure:"api_key"`
	From   string `mapstructure:"from"`
}

type MailgunCfg struct {
	APIKey string `mapstructure:"api_key"`
	Domain string `mapstructure:"domain"`
	From   string `mapstructure:"from"`
}

type SMSNotifConfig struct {
	Provider string    `mapstructure:"provider"`
	Twilio   TwilioCfg `mapstructure:"twilio"`
	Vonage   VonageCfg `mapstructure:"vonage"`
}

type TwilioCfg struct {
	AccountSID string `mapstructure:"account_sid"`
	AuthToken  string `mapstructure:"auth_token"`
	From       string `mapstructure:"from"`
}

type VonageCfg struct {
	APIKey    string `mapstructure:"api_key"`
	APISecret string `mapstructure:"api_secret"`
	From      string `mapstructure:"from"`
}

type ServerConfig struct {
	Port int
}

type DBConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string `mapstructure:"sslmode"` // default: "disable"
}

type WorkflowsConfig struct {
	Dir string // directory scanned for *.yaml workflow files; default: "./workflows"
}

// QueueTiConfig controls integration with the queue-ti message broker.
// When Enabled is false (the default) all queue-ti functionality is skipped:
// queueti step triggers, queueti instance triggers, and the gRPC producer.
type QueueTiConfig struct {
	Enabled  bool
	GRPCAddr string `mapstructure:"grpc_addr"` // default: "localhost:50051"
	AdminURL string `mapstructure:"admin_url"` // HTTP admin API for JWT auth; default: "http://localhost:8080"
	Username string
	Password string
}

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")

	// Map dotted config keys to UPPER_UNDERSCORE env vars (e.g. db.host → DB_HOST).
	// SetDefault registers each key so AutomaticEnv can resolve it even when it is
	// absent from the config file (required for env-only deployments such as Docker).
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	viper.SetDefault("server.port", 8081)

	viper.SetDefault("db.host", "")
	viper.SetDefault("db.port", 5432)
	viper.SetDefault("db.user", "")
	viper.SetDefault("db.password", "")
	viper.SetDefault("db.name", "")
	viper.SetDefault("db.sslmode", "disable")

	viper.SetDefault("workflows.dir", "./workflows")

	viper.SetDefault("queueti.enabled", false)
	viper.SetDefault("queueti.grpc_addr", "localhost:50051")
	viper.SetDefault("queueti.admin_url", "http://localhost:8080")
	viper.SetDefault("queueti.username", "")
	viper.SetDefault("queueti.password", "")

	viper.SetDefault("notifications.email.provider", "")
	viper.SetDefault("notifications.email.smtp.host", "")
	viper.SetDefault("notifications.email.smtp.port", 0)
	viper.SetDefault("notifications.email.smtp.username", "")
	viper.SetDefault("notifications.email.smtp.password", "")
	viper.SetDefault("notifications.email.smtp.from", "")
	viper.SetDefault("notifications.email.sendgrid.api_key", "")
	viper.SetDefault("notifications.email.sendgrid.from", "")
	viper.SetDefault("notifications.email.mailgun.api_key", "")
	viper.SetDefault("notifications.email.mailgun.domain", "")
	viper.SetDefault("notifications.email.mailgun.from", "")
	viper.SetDefault("notifications.sms.provider", "")
	viper.SetDefault("notifications.sms.twilio.account_sid", "")
	viper.SetDefault("notifications.sms.twilio.auth_token", "")
	viper.SetDefault("notifications.sms.twilio.from", "")
	viper.SetDefault("notifications.sms.vonage.api_key", "")
	viper.SetDefault("notifications.sms.vonage.api_secret", "")
	viper.SetDefault("notifications.sms.vonage.from", "")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	return &cfg, nil
}

func (d DBConfig) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		d.User, d.Password, d.Host, d.Port, d.Name, d.SSLMode,
	)
}
