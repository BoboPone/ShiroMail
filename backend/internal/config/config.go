package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AppPort               string
	AppEnv                string
	CORSAllowedOrigins    []string
	MySQLDSN              string
	RedisAddr             string
	JWTSecret             string
	MetricsToken          string
	CloudflareAPIBaseURL  string
	SpaceshipAPIBaseURL   string
	LegacyMailSyncAPIURL  string
	LegacyMailSyncEnabled bool
	MailStoragePath       string
}

func MustLoadConfig() Config {
	return Config{
		AppPort:               envOrDefault("APP_PORT", "8080"),
		AppEnv:                envOrDefault("APP_ENV", "development"),
		CORSAllowedOrigins:    splitEnv("CORS_ALLOWED_ORIGINS", "http://127.0.0.1:5173,http://localhost:5173"),
		MySQLDSN:              envOrDefault("MYSQL_DSN", "root:root@tcp(mysql:3306)/shiro_email?parseTime=true"),
		RedisAddr:             envOrDefault("REDIS_ADDR", "redis:6379"),
		JWTSecret:             envOrDefault("JWT_SECRET", "dev-secret"),
		MetricsToken:          envOrDefault("METRICS_TOKEN", ""),
		CloudflareAPIBaseURL:  envOrDefault("CLOUDFLARE_API_BASE_URL", "https://api.cloudflare.com/client/v4"),
		SpaceshipAPIBaseURL:   envOrDefault("SPACESHIP_API_BASE_URL", "https://spaceship.dev/api"),
		LegacyMailSyncAPIURL:  envOrDefault("LEGACY_MAIL_SYNC_API_URL", ""),
		LegacyMailSyncEnabled: boolEnvOrDefault("LEGACY_MAIL_SYNC_ENABLED", false),
		MailStoragePath:       envOrDefault("MAIL_STORAGE_PATH", "./data/mail"),
	}
}

func envOrDefault(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func boolEnvOrDefault(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func intEnvOrDefault(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func splitEnv(key string, fallback string) []string {
	value := envOrDefault(key, fallback)
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func (c Config) IsProduction() bool {
	return c.AppEnv == "production"
}
