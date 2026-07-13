// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Auth     AuthConfig
	Razorpay RazorpayConfig
	Email    EmailConfig
	Logs     LogsConfig
	Worker   WorkerConfig
}

type WorkerConfig struct {
	PollIntervalSec int
	LockTimeoutSec  int
	JobTimeoutSec   int
	AutomicaRoot    string
	GracePeriodMin  int
	UseWarmBoot     bool
	// GPU pool session billing (Phase 2)
	CreditsPerMin    int
	StartupCredits   int
	MinStartCredits  int
	MeterIntervalSec      int
	ReconnectCooldownSec  int
}

type RazorpayConfig struct {
	KeyID         string
	KeySecret     string
	WebhookSecret string
}

type EmailConfig struct {
	FromEmail string
	FromName  string
	Password  string
}

type LogsConfig struct {
	AccessPath      string
	ErrorPath       string
	NginxAccessPath string
	NginxErrorPath  string
}

type ServerConfig struct {
	Port string
	Host string
}

type DatabaseConfig struct {
	URI      string
	Database string
}

type AuthConfig struct {
	KindeIssuerURL string
}

func Load() (*Config, error) {
	// Load .env file if it exists (for local development)
	_ = godotenv.Load()

	config := &Config{
		Server: ServerConfig{
			Port: getEnvOrDefault("PORT", "8080"),
			Host: getEnvOrDefault("HOST", "0.0.0.0"),
		},
		Database: DatabaseConfig{
			URI:      os.Getenv("MONGODB_URI"),
			Database: getEnvOrDefault("MONGODB_DATABASE", "creditapp"),
		},
		Auth: AuthConfig{
			KindeIssuerURL: os.Getenv("KINDE_ISSUER_URL"),
		},
		Razorpay: RazorpayConfig{
			KeyID:         os.Getenv("RAZORPAY_KEY_ID"),
			KeySecret:     os.Getenv("RAZORPAY_KEY_SECRET"),
			WebhookSecret: os.Getenv("RAZORPAY_WEBHOOK_SECRET"),
		},
		Email: EmailConfig{
			FromEmail: getEnvOrDefault("GMAIL_FROM_EMAIL", "automicaai@gmail.com"),
			FromName:  getEnvOrDefault("GMAIL_FROM_NAME", "Automica"),
			Password:  os.Getenv("GMAIL_APP_PASSWORD"),
		},
		Logs: LogsConfig{
			AccessPath:      getEnvOrDefault("BACKEND_ACCESS_LOG_PATH", "/home/ec2-user/.pm2/logs/automica-backend-out.log"),
			ErrorPath:       getEnvOrDefault("BACKEND_ERROR_LOG_PATH", "/home/ec2-user/.pm2/logs/automica-backend-error.log"),
			NginxAccessPath: getEnvOrDefault("NGINX_ACCESS_LOG_PATH", "/home/ec2-user/.pm2/logs/automica-nginx-access.log"),
			NginxErrorPath:  getEnvOrDefault("NGINX_ERROR_LOG_PATH", "/home/ec2-user/.pm2/logs/automica-nginx-error.log"),
		},
		Worker: WorkerConfig{
			PollIntervalSec:  getEnvAsInt("WORKER_POLL_INTERVAL_SEC", 5),
			LockTimeoutSec:   getEnvAsInt("WORKER_LOCK_TIMEOUT_SEC", 1800),
			JobTimeoutSec:    getEnvAsInt("WORKER_JOB_TIMEOUT_SEC", 3600),
			AutomicaRoot:     getEnvOrDefault("AUTOMICA_ROOT", ""),
			GracePeriodMin:   getEnvAsInt("GPU_POOL_GRACE_MIN", 5),
			UseWarmBoot:      os.Getenv("E2E_USE_SAVED_IMAGE") == "1",
			CreditsPerMin:    getEnvAsInt("GPU_POOL_CREDITS_PER_MIN", 2),
			StartupCredits:   getEnvAsInt("GPU_POOL_STARTUP_CREDITS", 20),
			MinStartCredits:  getEnvAsInt("GPU_POOL_MIN_START_CREDITS", 30),
			MeterIntervalSec: getEnvAsInt("GPU_POOL_METER_INTERVAL_SEC", 60),
			ReconnectCooldownSec: getEnvAsInt("GPU_POOL_RECONNECT_COOLDOWN_SEC", 300),
		},
	}

	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return config, nil
}

func (c *Config) validate() error {
	if c.Database.URI == "" {
		return fmt.Errorf("MONGODB_URI is required")
	}
	if c.Auth.KindeIssuerURL == "" {
		return fmt.Errorf("KINDE_ISSUER_URL is required")
	}
	return nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
