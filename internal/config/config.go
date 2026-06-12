package config

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	Server        ServerConfig
	DB            DBConfig
	Workflows     WorkflowsConfig
	QueueTi       QueueTiConfig
	Notifications NotificationsConfig
}

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
	SSLMode  string `mapstructure:"sslmode"`
}

type WorkflowsConfig struct {
	Dir string
}

type QueueTiConfig struct {
	Enabled  bool
	GRPCAddr string `mapstructure:"grpc_addr"` // e.g. "localhost:50051"
	AdminURL string `mapstructure:"admin_url"` // HTTP admin API for auth, e.g. "http://localhost:8080"
	Username string
	Password string
}

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")

	viper.AutomaticEnv()

	viper.SetDefault("server.port", 8081)
	viper.SetDefault("db.sslmode", "disable")
	viper.SetDefault("workflows.dir", "./workflows")
	viper.SetDefault("queueti.enabled", false)
	viper.SetDefault("queueti.grpc_addr", "localhost:50051")
	viper.SetDefault("queueti.admin_url", "http://localhost:8080")

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
