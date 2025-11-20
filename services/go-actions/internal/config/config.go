package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the application
type Config struct {
	// Server configuration
	ServerPort string

	// Keycloak configuration
	KeycloakURL          string
	KeycloakRealm        string
	KeycloakClientID     string
	KeycloakClientSecret string

	// Hasura configuration
	HasuraEndpoint    string
	HasuraAdminSecret string

	// JWT configuration
	JWTSecret          string
	JWTClaimsNamespace string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	// Try to load .env file (optional, ignore errors)
	_ = godotenv.Load()

	cfg := &Config{
		ServerPort:           getEnv("SERVER_PORT", "3000"),
		KeycloakURL:          getEnv("KEYCLOAK_URL", ""),
		KeycloakRealm:        getEnv("KEYCLOAK_REALM", ""),
		KeycloakClientID:     getEnv("KEYCLOAK_CLIENT_ID", ""),
		KeycloakClientSecret: getEnv("KEYCLOAK_CLIENT_SECRET", ""),
		HasuraEndpoint:       getEnv("HASURA_ENDPOINT", ""),
		HasuraAdminSecret:    getEnv("HASURA_ADMIN_SECRET", ""),
		JWTSecret:            getEnv("JWT_SECRET", ""),
		JWTClaimsNamespace:   getEnv("JWT_CLAIMS_NAMESPACE", "https://hasura.io/jwt/claims"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks if all required configuration is present
func (c *Config) Validate() error {
	if c.KeycloakURL == "" {
		return fmt.Errorf("KEYCLOAK_URL is required")
	}
	if c.KeycloakRealm == "" {
		return fmt.Errorf("KEYCLOAK_REALM is required")
	}
	if c.KeycloakClientID == "" {
		return fmt.Errorf("KEYCLOAK_CLIENT_ID is required")
	}
	if c.KeycloakClientSecret == "" {
		return fmt.Errorf("KEYCLOAK_CLIENT_SECRET is required")
	}
	if c.HasuraEndpoint == "" {
		return fmt.Errorf("HASURA_ENDPOINT is required")
	}
	if c.HasuraAdminSecret == "" {
		return fmt.Errorf("HASURA_ADMIN_SECRET is required")
	}
	return nil
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
