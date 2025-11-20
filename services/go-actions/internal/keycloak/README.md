# Keycloak Client Package

A Go client for interacting with Keycloak Admin API, supporting multiple authentication methods.

## Features

- ✅ **Two Authentication Methods:**
  - Service Account (Client Credentials) - Recommended for production
  - Admin User (Password Grant) - For development/testing

- ✅ **Automatic Token Management:**
  - Token caching and reuse
  - Automatic refresh before expiration
  - Thread-safe with mutex locks

- ✅ **Comprehensive Error Handling:**
  - Typed error codes
  - Detailed error messages
  - Context-aware errors

## Quick Start

### Method 1: Service Account (Recommended)

```go
import (
    "context"
    "go-actions/internal/config"
    "go-actions/internal/keycloak"
)

cfg := &config.Config{
    KeycloakURL:          "http://localhost:8082",
    KeycloakRealm:        "master",
    KeycloakClientID:     "admin-cli",
    KeycloakClientSecret: "your-secret",
}

client := keycloak.NewClient(cfg)
user, err := client.GetUser(context.Background(), "user-id")
```

### Method 2: Admin User

```go
client := keycloak.NewClientWithAdminUser(
    "http://localhost:8082",  // Keycloak URL
    "master",                  // Realm
    "admin-cli",               // Client ID
    "admin",                   // Admin username
    "admin-password",          // Admin password
)

user, err := client.GetUser(context.Background(), "user-id")
```

## Documentation

- **[USAGE_EXAMPLE.md](./USAGE_EXAMPLE.md)** - Detailed usage guide with Keycloak configuration
- **[examples_comparison.go](./examples_comparison.go)** - Code examples comparing both methods
- **[README_TEST.md](./README_TEST.md)** - Integration testing guide

## API Methods

Currently supported:

- `GetUser(ctx context.Context, userID string) (*User, error)` - Retrieve user by ID

More methods can be added following the same pattern.

## Testing

Run all tests:
```bash
go test -v ./internal/keycloak
```

Run specific tests:
```bash
go test -v ./internal/keycloak -run TestAuthenticateWithPassword
```

Run integration tests (requires running Keycloak):
```bash
go test -v ./internal/keycloak -run Integration
```

## When to Use Which Method?

### Use Service Account (Client Credentials) when:
- ✅ Running in production
- ✅ Service-to-service authentication
- ✅ Need to rotate credentials easily
- ✅ Following OAuth2 best practices

### Use Admin User (Password Grant) when:
- ⚠️ Local development
- ⚠️ Testing environments
- ⚠️ Quick prototyping
- ⚠️ No service account configured yet

**Note:** Password grant is not recommended for production use.

## Architecture

```
keycloak.Client
├── Authentication
│   ├── Client Credentials (grant_type=client_credentials)
│   └── Password Grant (grant_type=password)
├── Token Management
│   ├── Automatic caching
│   ├── Refresh before expiry (60s buffer)
│   └── Thread-safe operations
└── API Methods
    └── GetUser(userID) → User
```

## Files

- `client.go` - Main client implementation
- `types.go` - Data structures (User, TokenResponse)
- `client_test.go` - Unit tests with mock server
- `integration_test.go` - Integration tests with real Keycloak
- `examples_comparison.go` - Usage examples
- `USAGE_EXAMPLE.md` - Detailed documentation
- `README_TEST.md` - Testing documentation

## Contributing

When adding new API methods:

1. Add the method to `client.go`
2. Use `ensureAuthenticated(ctx)` before API calls
3. Add unit tests to `client_test.go`
4. Add integration tests to `integration_test.go`
5. Update documentation

## License

Part of the Synmetrix project.
