package keycloak

// This file contains example code showing both authentication methods.
// DO NOT run this file directly - it's for reference only.

import (
	"context"
	"fmt"
	"go-actions/internal/config"
	"log"
)

// ExampleClientCredentials demonstrates using service account (client credentials)
func ExampleClientCredentials() {
	// Method 1: Using service account (recommended for production)
	cfg := &config.Config{
		KeycloakURL:          "http://localhost:8082",
		KeycloakRealm:        "master",
		KeycloakClientID:     "admin-cli",
		KeycloakClientSecret: "your-client-secret-here",
	}

	client := NewClient(cfg)
	ctx := context.Background()

	// Get a user by ID
	userID := "some-user-id"
	user, err := client.GetUser(ctx, userID)
	if err != nil {
		log.Fatalf("Failed to get user: %v", err)
	}

	fmt.Printf("Retrieved user: %s (%s)\n", user.Username, user.Email)
	fmt.Printf("User enabled: %t\n", user.Enabled)
	fmt.Printf("Email verified: %t\n", user.EmailVerified)
}

// ExamplePasswordGrant demonstrates using admin username and password
func ExamplePasswordGrant() {
	// Method 2: Using admin username and password
	client := NewClientWithAdminUser(
		"http://localhost:8082", // Keycloak URL
		"master",                 // Realm
		"admin-cli",              // Client ID (must support direct access grants)
		"admin",                  // Admin username
		"admin-password",         // Admin password
	)

	ctx := context.Background()

	// Get a user by ID
	userID := "some-user-id"
	user, err := client.GetUser(ctx, userID)
	if err != nil {
		log.Fatalf("Failed to get user: %v", err)
	}

	fmt.Printf("Retrieved user: %s (%s)\n", user.Username, user.Email)
	fmt.Printf("User enabled: %t\n", user.Enabled)
	fmt.Printf("Email verified: %t\n", user.EmailVerified)
}

// ExampleSwitchingBetweenMethods shows how to choose between methods
func ExampleSwitchingBetweenMethods(useServiceAccount bool) {
	var client *Client

	if useServiceAccount {
		// Production-recommended approach: use service account
		cfg := &config.Config{
			KeycloakURL:          "http://localhost:8082",
			KeycloakRealm:        "master",
			KeycloakClientID:     "admin-cli",
			KeycloakClientSecret: "service-account-secret",
		}
		client = NewClient(cfg)
		fmt.Println("Using service account authentication")
	} else {
		// Alternative approach: use admin user credentials
		client = NewClientWithAdminUser(
			"http://localhost:8082",
			"master",
			"admin-cli",
			"admin",
			"admin-password",
		)
		fmt.Println("Using password grant authentication")
	}

	// Both clients have the same API - use them identically
	ctx := context.Background()
	user, err := client.GetUser(ctx, "user-id")
	if err != nil {
		log.Fatalf("Failed to get user: %v", err)
	}

	fmt.Printf("User: %s\n", user.Username)
}

// ExampleErrorHandling demonstrates proper error handling
func ExampleErrorHandling() {
	client := NewClientWithAdminUser(
		"http://localhost:8082",
		"master",
		"admin-cli",
		"admin",
		"wrong-password", // Intentionally wrong password
	)

	ctx := context.Background()

	user, err := client.GetUser(ctx, "user-id")
	if err != nil {
		// Handle authentication errors
		log.Printf("Error occurred: %v", err)

		// You can check error types using the errors package
		// if errors.IsCode(err, errors.ErrCodeKeycloakAuth) {
		//     log.Println("Authentication failed - check credentials")
		// }

		return
	}

	fmt.Printf("User: %s\n", user.Username)
}

// ExampleReusingClient demonstrates that tokens are cached
func ExampleReusingClient() {
	client := NewClient(&config.Config{
		KeycloakURL:          "http://localhost:8082",
		KeycloakRealm:        "master",
		KeycloakClientID:     "admin-cli",
		KeycloakClientSecret: "secret",
	})

	ctx := context.Background()

	// First call - will authenticate and cache token
	user1, err := client.GetUser(ctx, "user-id-1")
	if err != nil {
		log.Fatalf("Failed: %v", err)
	}
	fmt.Printf("First user: %s\n", user1.Username)

	// Second call - will reuse cached token (no new authentication)
	user2, err := client.GetUser(ctx, "user-id-2")
	if err != nil {
		log.Fatalf("Failed: %v", err)
	}
	fmt.Printf("Second user: %s\n", user2.Username)

	// Third call - will still reuse token if not expired
	user3, err := client.GetUser(ctx, "user-id-3")
	if err != nil {
		log.Fatalf("Failed: %v", err)
	}
	fmt.Printf("Third user: %s\n", user3.Username)

	fmt.Println("All three calls used the same token!")
}
