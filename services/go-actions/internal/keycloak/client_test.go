package keycloak

import (
	"context"
	"go-actions/internal/config"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestNewClient tests the client creation with client credentials
func TestNewClient(t *testing.T) {
	cfg := &config.Config{
		KeycloakURL:          "http://localhost:8082",
		KeycloakRealm:        "master",
		KeycloakClientID:     "admin-cli",
		KeycloakClientSecret: "test-secret",
	}

	client := NewClient(cfg)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	if client.baseURL != cfg.KeycloakURL {
		t.Errorf("Expected baseURL %s, got %s", cfg.KeycloakURL, client.baseURL)
	}

	if client.realm != cfg.KeycloakRealm {
		t.Errorf("Expected realm %s, got %s", cfg.KeycloakRealm, client.realm)
	}

	if client.clientID != cfg.KeycloakClientID {
		t.Errorf("Expected clientID %s, got %s", cfg.KeycloakClientID, client.clientID)
	}

	if client.clientSecret != cfg.KeycloakClientSecret {
		t.Errorf("Expected clientSecret %s, got %s", cfg.KeycloakClientSecret, client.clientSecret)
	}

	if client.authMethod != AuthMethodClientCredentials {
		t.Errorf("Expected authMethod %s, got %s", AuthMethodClientCredentials, client.authMethod)
	}

	if client.httpClient == nil {
		t.Error("httpClient should not be nil")
	}

	if client.httpClient.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %s", client.httpClient.Timeout)
	}
}

// TestNewClientWithConfigAdminUser tests the client creation from config with admin user credentials
func TestNewClientWithConfigAdminUser(t *testing.T) {
	cfg := &config.Config{
		KeycloakURL:           "http://localhost:8082",
		KeycloakRealm:         "master",
		KeycloakClientID:      "admin-cli",
		KeycloakAdminUsername: "admin",
		KeycloakAdminPassword: "admin-password",
	}

	client := NewClient(cfg)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	if client.baseURL != cfg.KeycloakURL {
		t.Errorf("Expected baseURL %s, got %s", cfg.KeycloakURL, client.baseURL)
	}

	if client.realm != cfg.KeycloakRealm {
		t.Errorf("Expected realm %s, got %s", cfg.KeycloakRealm, client.realm)
	}

	if client.clientID != cfg.KeycloakClientID {
		t.Errorf("Expected clientID %s, got %s", cfg.KeycloakClientID, client.clientID)
	}

	if client.adminUsername != cfg.KeycloakAdminUsername {
		t.Errorf("Expected adminUsername %s, got %s", cfg.KeycloakAdminUsername, client.adminUsername)
	}

	if client.adminPassword != cfg.KeycloakAdminPassword {
		t.Errorf("Expected adminPassword %s, got %s", cfg.KeycloakAdminPassword, client.adminPassword)
	}

	if client.authMethod != AuthMethodPassword {
		t.Errorf("Expected authMethod %s, got %s", AuthMethodPassword, client.authMethod)
	}

	if client.httpClient == nil {
		t.Error("httpClient should not be nil")
	}

	if client.httpClient.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %s", client.httpClient.Timeout)
	}
}

// TestNewClientWithConfigBothCredentials tests that admin user takes precedence when both are provided
func TestNewClientWithConfigBothCredentials(t *testing.T) {
	cfg := &config.Config{
		KeycloakURL:           "http://localhost:8082",
		KeycloakRealm:         "master",
		KeycloakClientID:      "admin-cli",
		KeycloakClientSecret:  "test-secret",
		KeycloakAdminUsername: "admin",
		KeycloakAdminPassword: "admin-password",
	}

	client := NewClient(cfg)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	// Admin user should take precedence
	if client.authMethod != AuthMethodPassword {
		t.Errorf("Expected authMethod %s (admin user should take precedence), got %s", AuthMethodPassword, client.authMethod)
	}

	if client.adminUsername != cfg.KeycloakAdminUsername {
		t.Errorf("Expected adminUsername %s, got %s", cfg.KeycloakAdminUsername, client.adminUsername)
	}

	if client.adminPassword != cfg.KeycloakAdminPassword {
		t.Errorf("Expected adminPassword %s, got %s", cfg.KeycloakAdminPassword, client.adminPassword)
	}
}

// TestNewClientWithAdminUser tests the client creation with admin user credentials
func TestNewClientWithAdminUser(t *testing.T) {
	client := NewClientWithAdminUser(
		"http://localhost:8082",
		"master",
		"admin-cli",
		"admin",
		"admin-password",
	)

	if client == nil {
		t.Fatal("NewClientWithAdminUser returned nil")
	}

	if client.baseURL != "http://localhost:8082" {
		t.Errorf("Expected baseURL http://localhost:8082, got %s", client.baseURL)
	}

	if client.realm != "master" {
		t.Errorf("Expected realm master, got %s", client.realm)
	}

	if client.clientID != "admin-cli" {
		t.Errorf("Expected clientID admin-cli, got %s", client.clientID)
	}

	if client.adminUsername != "admin" {
		t.Errorf("Expected adminUsername admin, got %s", client.adminUsername)
	}

	if client.adminPassword != "admin-password" {
		t.Errorf("Expected adminPassword admin-password, got %s", client.adminPassword)
	}

	if client.authMethod != AuthMethodPassword {
		t.Errorf("Expected authMethod %s, got %s", AuthMethodPassword, client.authMethod)
	}

	if client.httpClient == nil {
		t.Error("httpClient should not be nil")
	}

	if client.httpClient.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %s", client.httpClient.Timeout)
	}
}

// TestAuthenticate tests the authentication flow with mock server (client credentials)
func TestAuthenticate(t *testing.T) {
	// Create mock server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/realms/master/protocol/openid-connect/token" {
			if r.Method != "POST" {
				t.Errorf("Expected POST request, got %s", r.Method)
			}

			if err := r.ParseForm(); err != nil {
				t.Errorf("Failed to parse form: %v", err)
			}

			if r.Form.Get("grant_type") != "client_credentials" {
				t.Errorf("Expected grant_type=client_credentials, got %s", r.Form.Get("grant_type"))
			}

			if r.Form.Get("client_id") != "admin-cli" {
				t.Errorf("Expected client_id=admin-cli, got %s", r.Form.Get("client_id"))
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"access_token": "test-token-12345",
				"expires_in": 3600,
				"refresh_expires_in": 0,
				"token_type": "Bearer"
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		KeycloakURL:          mockServer.URL,
		KeycloakRealm:        "master",
		KeycloakClientID:     "admin-cli",
		KeycloakClientSecret: "test-secret",
	}

	client := NewClient(cfg)
	ctx := context.Background()

	err := client.authenticate(ctx)
	if err != nil {
		t.Fatalf("authenticate() failed: %v", err)
	}

	client.mu.RLock()
	defer client.mu.RUnlock()

	if client.accessToken != "test-token-12345" {
		t.Errorf("Expected access token 'test-token-12345', got '%s'", client.accessToken)
	}

	if client.tokenExpiry.IsZero() {
		t.Error("Token expiry should be set")
	}

	// Token should expire approximately 1 hour from now
	expectedExpiry := time.Now().Add(3600 * time.Second)
	diff := client.tokenExpiry.Sub(expectedExpiry)
	if diff > 5*time.Second || diff < -5*time.Second {
		t.Errorf("Token expiry time difference too large: %v", diff)
	}
}

// TestAuthenticateWithPassword tests the authentication flow with password grant type
func TestAuthenticateWithPassword(t *testing.T) {
	// Create mock server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/realms/master/protocol/openid-connect/token" {
			if r.Method != "POST" {
				t.Errorf("Expected POST request, got %s", r.Method)
			}

			if err := r.ParseForm(); err != nil {
				t.Errorf("Failed to parse form: %v", err)
			}

			if r.Form.Get("grant_type") != "password" {
				t.Errorf("Expected grant_type=password, got %s", r.Form.Get("grant_type"))
			}

			if r.Form.Get("client_id") != "admin-cli" {
				t.Errorf("Expected client_id=admin-cli, got %s", r.Form.Get("client_id"))
			}

			if r.Form.Get("username") != "admin" {
				t.Errorf("Expected username=admin, got %s", r.Form.Get("username"))
			}

			if r.Form.Get("password") != "admin-password" {
				t.Errorf("Expected password=admin-password, got %s", r.Form.Get("password"))
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"access_token": "test-admin-token-67890",
				"expires_in": 3600,
				"refresh_expires_in": 0,
				"token_type": "Bearer"
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	client := NewClientWithAdminUser(
		mockServer.URL,
		"master",
		"admin-cli",
		"admin",
		"admin-password",
	)
	ctx := context.Background()

	err := client.authenticate(ctx)
	if err != nil {
		t.Fatalf("authenticate() with password failed: %v", err)
	}

	client.mu.RLock()
	defer client.mu.RUnlock()

	if client.accessToken != "test-admin-token-67890" {
		t.Errorf("Expected access token 'test-admin-token-67890', got '%s'", client.accessToken)
	}

	if client.tokenExpiry.IsZero() {
		t.Error("Token expiry should be set")
	}

	// Token should expire approximately 1 hour from now
	expectedExpiry := time.Now().Add(3600 * time.Second)
	diff := client.tokenExpiry.Sub(expectedExpiry)
	if diff > 5*time.Second || diff < -5*time.Second {
		t.Errorf("Token expiry time difference too large: %v", diff)
	}
}

// TestAuthenticateFailure tests authentication failure scenarios
func TestAuthenticateFailure(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		responseBody   string
		expectedErrMsg string
	}{
		{
			name:           "Unauthorized",
			statusCode:     http.StatusUnauthorized,
			responseBody:   `{"error": "invalid_client"}`,
			expectedErrMsg: "authentication failed with status: 401",
		},
		{
			name:           "Server Error",
			statusCode:     http.StatusInternalServerError,
			responseBody:   `{"error": "server_error"}`,
			expectedErrMsg: "authentication failed with status: 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer mockServer.Close()

			cfg := &config.Config{
				KeycloakURL:          mockServer.URL,
				KeycloakRealm:        "master",
				KeycloakClientID:     "admin-cli",
				KeycloakClientSecret: "wrong-secret",
			}

			client := NewClient(cfg)
			ctx := context.Background()

			err := client.authenticate(ctx)
			if err == nil {
				t.Fatal("Expected authentication to fail, got nil error")
			}
		})
	}
}

// TestEnsureAuthenticated tests the token caching and refresh logic
func TestEnsureAuthenticated(t *testing.T) {
	requestCount := 0

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"access_token": "test-token-12345",
			"expires_in": 3600,
			"refresh_expires_in": 0,
			"token_type": "Bearer"
		}`))
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		KeycloakURL:          mockServer.URL,
		KeycloakRealm:        "master",
		KeycloakClientID:     "admin-cli",
		KeycloakClientSecret: "test-secret",
	}

	client := NewClient(cfg)
	ctx := context.Background()

	// First call should authenticate
	err := client.ensureAuthenticated(ctx)
	if err != nil {
		t.Fatalf("First ensureAuthenticated() failed: %v", err)
	}

	if requestCount != 1 {
		t.Errorf("Expected 1 auth request, got %d", requestCount)
	}

	// Second call should use cached token
	err = client.ensureAuthenticated(ctx)
	if err != nil {
		t.Fatalf("Second ensureAuthenticated() failed: %v", err)
	}

	if requestCount != 1 {
		t.Errorf("Expected cached token to be used, but got %d requests", requestCount)
	}

	// Expire the token
	client.mu.Lock()
	client.tokenExpiry = time.Now().Add(-1 * time.Hour)
	client.mu.Unlock()

	// Third call should re-authenticate
	err = client.ensureAuthenticated(ctx)
	if err != nil {
		t.Fatalf("Third ensureAuthenticated() failed: %v", err)
	}

	if requestCount != 2 {
		t.Errorf("Expected token refresh, but got %d requests", requestCount)
	}
}

// TestGetUser tests user retrieval with mock server
func TestGetUser(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle token request
		if r.URL.Path == "/realms/master/protocol/openid-connect/token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"access_token": "test-token-12345",
				"expires_in": 3600,
				"refresh_expires_in": 0,
				"token_type": "Bearer"
			}`))
			return
		}

		// Handle user request
		if r.URL.Path == "/admin/realms/master/users/test-user-id" {
			if r.Method != "GET" {
				t.Errorf("Expected GET request, got %s", r.Method)
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader != "Bearer test-token-12345" {
				t.Errorf("Expected Bearer token, got %s", authHeader)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"id": "test-user-id",
				"email": "test@example.com",
				"emailVerified": true,
				"firstName": "Test",
				"lastName": "User",
				"username": "testuser",
				"enabled": true,
				"createdTimestamp": 1234567890000
			}`))
			return
		}

		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		KeycloakURL:          mockServer.URL,
		KeycloakRealm:        "master",
		KeycloakClientID:     "admin-cli",
		KeycloakClientSecret: "test-secret",
	}

	client := NewClient(cfg)
	ctx := context.Background()

	user, err := client.GetUser(ctx, "test-user-id")
	if err != nil {
		t.Fatalf("GetUser() failed: %v", err)
	}

	if user.ID != "test-user-id" {
		t.Errorf("Expected user ID 'test-user-id', got '%s'", user.ID)
	}

	if user.Email != "test@example.com" {
		t.Errorf("Expected email 'test@example.com', got '%s'", user.Email)
	}

	if !user.EmailVerified {
		t.Error("Expected EmailVerified to be true")
	}

	if user.FirstName != "Test" {
		t.Errorf("Expected first name 'Test', got '%s'", user.FirstName)
	}

	if user.LastName != "User" {
		t.Errorf("Expected last name 'User', got '%s'", user.LastName)
	}

	if user.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", user.Username)
	}

	if !user.Enabled {
		t.Error("Expected Enabled to be true")
	}

	if user.CreatedTimestamp != 1234567890000 {
		t.Errorf("Expected CreatedTimestamp 1234567890000, got %d", user.CreatedTimestamp)
	}
}

// TestGetUserNotFound tests user not found scenario
func TestGetUserNotFound(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle token request
		if r.URL.Path == "/realms/master/protocol/openid-connect/token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"access_token": "test-token-12345",
				"expires_in": 3600,
				"refresh_expires_in": 0,
				"token_type": "Bearer"
			}`))
			return
		}

		// Handle user request - return 404
		if r.URL.Path == "/admin/realms/master/users/nonexistent-user" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		KeycloakURL:          mockServer.URL,
		KeycloakRealm:        "master",
		KeycloakClientID:     "admin-cli",
		KeycloakClientSecret: "test-secret",
	}

	client := NewClient(cfg)
	ctx := context.Background()

	user, err := client.GetUser(ctx, "nonexistent-user")
	if err == nil {
		t.Fatal("Expected error for non-existent user, got nil")
	}

	if user != nil {
		t.Errorf("Expected nil user, got %+v", user)
	}
}

// TestGetUserContextCancellation tests context cancellation
func TestGetUserContextCancellation(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		KeycloakURL:          mockServer.URL,
		KeycloakRealm:        "master",
		KeycloakClientID:     "admin-cli",
		KeycloakClientSecret: "test-secret",
	}

	client := NewClient(cfg)

	// Create context with immediate cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.GetUser(ctx, "test-user-id")
	if err == nil {
		t.Fatal("Expected error due to context cancellation, got nil")
	}
}

// TestConcurrentAuthentication tests concurrent access to authentication
func TestConcurrentAuthentication(t *testing.T) {
	requestCount := 0
	var requestCountMu sync.Mutex

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCountMu.Lock()
		requestCount++
		requestCountMu.Unlock()

		// Simulate some processing time
		time.Sleep(10 * time.Millisecond)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"access_token": "test-token-12345",
			"expires_in": 3600,
			"refresh_expires_in": 0,
			"token_type": "Bearer"
		}`))
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		KeycloakURL:          mockServer.URL,
		KeycloakRealm:        "master",
		KeycloakClientID:     "admin-cli",
		KeycloakClientSecret: "test-secret",
	}

	client := NewClient(cfg)
	ctx := context.Background()

	// Run multiple concurrent authentication attempts
	const concurrency = 10
	errChan := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			errChan <- client.ensureAuthenticated(ctx)
		}()
	}

	// Collect results
	for i := 0; i < concurrency; i++ {
		if err := <-errChan; err != nil {
			t.Errorf("Concurrent ensureAuthenticated() failed: %v", err)
		}
	}

	// Should only have made one request due to locking
	requestCountMu.Lock()
	defer requestCountMu.Unlock()
	if requestCount > 2 {
		t.Errorf("Expected at most 2 auth requests due to locking, got %d", requestCount)
	}
}
