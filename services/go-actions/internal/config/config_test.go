package config

import (
	"os"
	"testing"
)

func TestConfigValidate_ClientCredentials(t *testing.T) {
	cfg := &Config{
		KeycloakURL:          "http://localhost:8082",
		KeycloakRealm:        "master",
		KeycloakClientID:     "admin-cli",
		KeycloakClientSecret: "test-secret",
		HasuraEndpoint:       "http://localhost:8080/v1/graphql",
		HasuraAdminSecret:    "admin-secret",
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Expected no error for valid client credentials config, got: %v", err)
	}

	if cfg.UseAdminUserAuth() {
		t.Error("Expected UseAdminUserAuth to return false for client credentials config")
	}
}

func TestConfigValidate_AdminUserCredentials(t *testing.T) {
	cfg := &Config{
		KeycloakURL:           "http://localhost:8082",
		KeycloakRealm:         "master",
		KeycloakClientID:      "admin-cli",
		KeycloakAdminUsername: "admin",
		KeycloakAdminPassword: "admin-password",
		HasuraEndpoint:        "http://localhost:8080/v1/graphql",
		HasuraAdminSecret:     "admin-secret",
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Expected no error for valid admin user credentials config, got: %v", err)
	}

	if !cfg.UseAdminUserAuth() {
		t.Error("Expected UseAdminUserAuth to return true for admin user credentials config")
	}
}

func TestConfigValidate_BothCredentials(t *testing.T) {
	// Having both credentials is valid - client credentials takes precedence
	cfg := &Config{
		KeycloakURL:           "http://localhost:8082",
		KeycloakRealm:         "master",
		KeycloakClientID:      "admin-cli",
		KeycloakClientSecret:  "test-secret",
		KeycloakAdminUsername: "admin",
		KeycloakAdminPassword: "admin-password",
		HasuraEndpoint:        "http://localhost:8080/v1/graphql",
		HasuraAdminSecret:     "admin-secret",
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Expected no error when both credentials are provided, got: %v", err)
	}

	// Admin user auth should be preferred when both are available
	if !cfg.UseAdminUserAuth() {
		t.Error("Expected UseAdminUserAuth to return true when both credentials are provided")
	}
}

func TestConfigValidate_NoCredentials(t *testing.T) {
	cfg := &Config{
		KeycloakURL:       "http://localhost:8082",
		KeycloakRealm:     "master",
		KeycloakClientID:  "admin-cli",
		HasuraEndpoint:    "http://localhost:8080/v1/graphql",
		HasuraAdminSecret: "admin-secret",
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error when no Keycloak credentials are provided")
	}

	expectedMsg := "KEYCLOAK_CLIENT_SECRET or (KEYCLOAK_ADMIN_USERNAME and KEYCLOAK_ADMIN_PASSWORD) is required"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestConfigValidate_IncompleteAdminCredentials(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{
			name:     "Username only",
			username: "admin",
			password: "",
		},
		{
			name:     "Password only",
			username: "",
			password: "admin-password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				KeycloakURL:           "http://localhost:8082",
				KeycloakRealm:         "master",
				KeycloakClientID:      "admin-cli",
				KeycloakAdminUsername: tt.username,
				KeycloakAdminPassword: tt.password,
				HasuraEndpoint:        "http://localhost:8080/v1/graphql",
				HasuraAdminSecret:     "admin-secret",
			}

			err := cfg.Validate()
			if err == nil {
				t.Error("Expected error for incomplete admin credentials")
			}
		})
	}
}

func TestConfigValidate_MissingRequired(t *testing.T) {
	tests := []struct {
		name          string
		cfg           *Config
		expectedError string
	}{
		{
			name: "Missing KeycloakURL",
			cfg: &Config{
				KeycloakRealm:        "master",
				KeycloakClientID:     "admin-cli",
				KeycloakClientSecret: "secret",
				HasuraEndpoint:       "http://localhost:8080/v1/graphql",
				HasuraAdminSecret:    "admin-secret",
			},
			expectedError: "KEYCLOAK_URL is required",
		},
		{
			name: "Missing KeycloakRealm",
			cfg: &Config{
				KeycloakURL:          "http://localhost:8082",
				KeycloakClientID:     "admin-cli",
				KeycloakClientSecret: "secret",
				HasuraEndpoint:       "http://localhost:8080/v1/graphql",
				HasuraAdminSecret:    "admin-secret",
			},
			expectedError: "KEYCLOAK_REALM is required",
		},
		{
			name: "Missing KeycloakClientID",
			cfg: &Config{
				KeycloakURL:          "http://localhost:8082",
				KeycloakRealm:        "master",
				KeycloakClientSecret: "secret",
				HasuraEndpoint:       "http://localhost:8080/v1/graphql",
				HasuraAdminSecret:    "admin-secret",
			},
			expectedError: "KEYCLOAK_CLIENT_ID is required",
		},
		{
			name: "Missing HasuraEndpoint",
			cfg: &Config{
				KeycloakURL:          "http://localhost:8082",
				KeycloakRealm:        "master",
				KeycloakClientID:     "admin-cli",
				KeycloakClientSecret: "secret",
				HasuraAdminSecret:    "admin-secret",
			},
			expectedError: "HASURA_ENDPOINT is required",
		},
		{
			name: "Missing HasuraAdminSecret",
			cfg: &Config{
				KeycloakURL:          "http://localhost:8082",
				KeycloakRealm:        "master",
				KeycloakClientID:     "admin-cli",
				KeycloakClientSecret: "secret",
				HasuraEndpoint:       "http://localhost:8080/v1/graphql",
			},
			expectedError: "HASURA_ADMIN_SECRET is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil {
				t.Errorf("Expected error for %s", tt.name)
			}
			if err.Error() != tt.expectedError {
				t.Errorf("Expected error '%s', got '%s'", tt.expectedError, err.Error())
			}
		})
	}
}

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue string
		envValue     string
		expected     string
	}{
		{
			name:         "Env var exists",
			key:          "TEST_VAR_EXISTS",
			defaultValue: "default",
			envValue:     "from-env",
			expected:     "from-env",
		},
		{
			name:         "Env var doesn't exist",
			key:          "TEST_VAR_NOT_EXISTS",
			defaultValue: "default",
			envValue:     "",
			expected:     "default",
		},
		{
			name:         "Empty default",
			key:          "TEST_VAR_EMPTY_DEFAULT",
			defaultValue: "",
			envValue:     "",
			expected:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up env var
			os.Unsetenv(tt.key)

			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			}

			result := getEnv(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestUseAdminUserAuth(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *Config
		expected bool
	}{
		{
			name: "Both username and password provided",
			cfg: &Config{
				KeycloakAdminUsername: "admin",
				KeycloakAdminPassword: "password",
			},
			expected: true,
		},
		{
			name: "Only username provided",
			cfg: &Config{
				KeycloakAdminUsername: "admin",
				KeycloakAdminPassword: "",
			},
			expected: false,
		},
		{
			name: "Only password provided",
			cfg: &Config{
				KeycloakAdminUsername: "",
				KeycloakAdminPassword: "password",
			},
			expected: false,
		},
		{
			name: "Neither provided",
			cfg: &Config{
				KeycloakAdminUsername: "",
				KeycloakAdminPassword: "",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.cfg.UseAdminUserAuth()
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}
