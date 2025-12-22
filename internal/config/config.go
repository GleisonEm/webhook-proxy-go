package config

import (
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server ServerConfig `mapstructure:"server"`
	Log    LogConfig    `mapstructure:"log"`
	NATS   NATSConfig   `mapstructure:"nats"`
}

type ServerConfig struct {
	Port           string `mapstructure:"port"`
	DestinationURL string `mapstructure:"destination_url"`
}

type LogConfig struct {
	Level    string `mapstructure:"level"`
	AppName  string `mapstructure:"app_name"`
	LokiURL  string `mapstructure:"loki_url"`
	Encoding string `mapstructure:"encoding"`
}

type NATSConfig struct {
	URL                    string  `mapstructure:"url"`
	ClusterID              string  `mapstructure:"cluster_id"`
	MaxRetries             int     `mapstructure:"max_retries"`
	AckWaitSeconds         int     `mapstructure:"ack_wait_seconds"`
	Concurrency            int     `mapstructure:"concurrency"`
	RetryBackoffSeconds    int     `mapstructure:"retry_backoff_seconds"`
	RetryBackoffMultiplier float64 `mapstructure:"retry_backoff_multiplier"`
}

func LoadConfig() (*Config, error) {
	v := viper.New()

	v.SetEnvPrefix("APP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Defaults
	v.SetDefault("server.port", "8095")
	v.SetDefault("server.destination_url", "https://peaceful-comet-58.webhook.cool") // Default for testing
	v.SetDefault("log.level", "info")
	v.SetDefault("log.app_name", "webhook-proxy")
	v.SetDefault("log.encoding", "json")
	v.SetDefault("log.loki_url", "https://loki.gemanuel.site")
	v.SetDefault("nats.url", "nats://localhost:4222")
	v.SetDefault("nats.url", "nats://localhost:4222")
	v.SetDefault("nats.url", "nats://localhost:4222")
	v.SetDefault("nats.max_retries", 5)
	v.SetDefault("nats.ack_wait_seconds", 30)
	v.SetDefault("nats.concurrency", 10)
	v.SetDefault("nats.retry_backoff_seconds", 3)
	v.SetDefault("nats.retry_backoff_multiplier", 2.0)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
