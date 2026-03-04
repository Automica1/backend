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
