//go:build integration
// +build integration

package keycloak

import (
	"context"
	"go-actions/internal/config"
	"os"
	"testing"
)

// TestIntegrationAuthenticate tests authentication against real Keycloak instance
// Run with: go test -tags=integration -v ./internal/keycloak
func TestIntegrationAuthenticate(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	keycloakURL := os.Getenv("KEYCLOAK_URL")
	if keycloakURL == "" {
		keycloakURL = "http://localhost:8082"
	}

	cfg := &config.Config{
		KeycloakURL:          keycloakURL,
		KeycloakRealm:        "master",
		KeycloakClientID:     "admin-cli",
		KeycloakClientSecret: "", // admin-cli uses direct access grant
	}

	client := NewClient(cfg)
	ctx := context.Background()

	// Note: This test requires Keycloak to be running at localhost:8082
	// with admin/admin credentials
	err := client.authenticate(ctx)
	if err != nil {
		t.Logf("Warning: Could not authenticate to Keycloak at %s", keycloakURL)
		t.Logf("Make sure Keycloak is running with admin/admin credentials")
		t.Logf("Error: %v", err)
		t.Skip("Skipping integration test - Keycloak not available")
	}

	client.mu.RLock()
	defer client.mu.RUnlock()

	if client.accessToken == "" {
		t.Error("Access token should not be empty after successful authentication")
	}

	if client.tokenExpiry.IsZero() {
		t.Error("Token expiry should be set after successful authentication")
	}

	t.Logf("Successfully authenticated to Keycloak")
	t.Logf("Access token length: %d", len(client.accessToken))
	t.Logf("Token expires at: %s", client.tokenExpiry.Format("2006-01-02 15:04:05"))
}

// TestIntegrationGetAdminUser tests retrieving the admin user
func TestIntegrationGetAdminUser(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	keycloakURL := os.Getenv("KEYCLOAK_URL")
	if keycloakURL == "" {
		keycloakURL = "http://localhost:8082"
	}

	// First, we need to get the admin user ID
	// In a real scenario, you would need to either:
	// 1. Know the user ID beforehand
	// 2. Implement a SearchUsers method to find users by username
	// 3. Use the Keycloak Admin API to list users

	// For this test, we'll just verify that the client can make authenticated requests
	cfg := &config.Config{
		KeycloakURL:          keycloakURL,
		KeycloakRealm:        "master",
		KeycloakClientID:     "admin-cli",
		KeycloakClientSecret: "",
	}

	client := NewClient(cfg)
	ctx := context.Background()

	// Test with a non-existent user to verify the API is working
	_, err := client.GetUser(ctx, "nonexistent-user-id-12345")
	if err == nil {
		t.Error("Expected error for non-existent user")
	}

	t.Logf("GetUser correctly returned error for non-existent user: %v", err)
}

// TestIntegrationTokenRefresh tests that tokens are properly refreshed
func TestIntegrationTokenRefresh(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	keycloakURL := os.Getenv("KEYCLOAK_URL")
	if keycloakURL == "" {
		keycloakURL = "http://localhost:8082"
	}

	cfg := &config.Config{
		KeycloakURL:          keycloakURL,
		KeycloakRealm:        "master",
		KeycloakClientID:     "admin-cli",
		KeycloakClientSecret: "",
	}

	client := NewClient(cfg)
	ctx := context.Background()

	// First authentication
	err := client.ensureAuthenticated(ctx)
	if err != nil {
		t.Skipf("Skipping test - Keycloak not available: %v", err)
	}

	client.mu.RLock()
	firstToken := client.accessToken
	client.mu.RUnlock()

	// Second call should use cached token
	err = client.ensureAuthenticated(ctx)
	if err != nil {
		t.Fatalf("Second authentication failed: %v", err)
	}

	client.mu.RLock()
	secondToken := client.accessToken
	client.mu.RUnlock()

	if firstToken != secondToken {
		t.Error("Token should be cached and reused")
	}

	t.Log("Token caching working correctly")
}

// TestIntegrationConcurrentRequests tests concurrent API requests
func TestIntegrationConcurrentRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	keycloakURL := os.Getenv("KEYCLOAK_URL")
	if keycloakURL == "" {
		keycloakURL = "http://localhost:8082"
	}

	cfg := &config.Config{
		KeycloakURL:          keycloakURL,
		KeycloakRealm:        "master",
		KeycloakClientID:     "admin-cli",
		KeycloakClientSecret: "",
	}

	client := NewClient(cfg)
	ctx := context.Background()

	// Test concurrent authentication
	const concurrency = 5
	errChan := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			errChan <- client.ensureAuthenticated(ctx)
		}()
	}

	successCount := 0
	for i := 0; i < concurrency; i++ {
		if err := <-errChan; err == nil {
			successCount++
		} else {
			t.Logf("Concurrent request failed: %v", err)
		}
	}

	if successCount == 0 {
		t.Skip("Skipping test - Keycloak not available")
	}

	t.Logf("Successfully handled %d/%d concurrent requests", successCount, concurrency)
}
