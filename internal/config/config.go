package config

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	Server    ServerConfig
	DB        DBConfig
	Workflows WorkflowsConfig
	QueueTi   QueueTiConfig
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
