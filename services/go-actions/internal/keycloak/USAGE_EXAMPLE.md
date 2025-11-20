# Keycloak Client Usage Examples

This package provides a Keycloak client that supports two authentication methods:

1. **Client Credentials (Service Account)** - Uses client ID and client secret
2. **Password Grant (Admin User)** - Uses admin username and password

## Authentication Methods

### Method 1: Client Credentials (Service Account)

This method uses OAuth2 client credentials flow, ideal for service-to-service authentication.

**Prerequisites:**
- A Keycloak client with `service-accounts-enabled` set to `true`
- Client credentials (client ID and secret)
- Appropriate service account roles assigned

**Example:**

```go
package main

import (
    "context"
    "fmt"
    "log"
    "go-actions/internal/config"
    "go-actions/internal/keycloak"
)

func main() {
    cfg := &config.Config{
        KeycloakURL:          "http://localhost:8082",
        KeycloakRealm:        "master",
        KeycloakClientID:     "admin-cli",
        KeycloakClientSecret: "your-client-secret",
    }

    // Create client with service account
    client := keycloak.NewClient(cfg)

    // Get user information
    ctx := context.Background()
    user, err := client.GetUser(ctx, "user-id-here")
    if err != nil {
        log.Fatalf("Failed to get user: %v", err)
    }

    fmt.Printf("User: %s (%s)\n", user.Username, user.Email)
}
```

### Method 2: Password Grant (Admin User)

This method uses OAuth2 password flow with admin user credentials.

**Prerequisites:**
- Admin user credentials (username and password)
- A client that supports direct access grants (password flow)
- In Keycloak, the client must have "Direct access grants" enabled

**Example:**

```go
package main

import (
    "context"
    "fmt"
    "log"
    "go-actions/internal/keycloak"
)

func main() {
    // Create client with admin user credentials
    client := keycloak.NewClientWithAdminUser(
        "http://localhost:8082",  // Keycloak URL
        "master",                  // Realm
        "admin-cli",               // Client ID
        "admin",                   // Admin username
        "admin-password",          // Admin password
    )

    // Get user information
    ctx := context.Background()
    user, err := client.GetUser(ctx, "user-id-here")
    if err != nil {
        log.Fatalf("Failed to get user: %v", err)
    }

    fmt.Printf("User: %s (%s)\n", user.Username, user.Email)
}
```

## Keycloak Configuration

### For Client Credentials (Method 1):

1. Go to Keycloak Admin Console
2. Select your realm
3. Go to "Clients" → Select/Create your client
4. Under "Settings":
   - Enable "Service Accounts Enabled"
   - Save the client
5. Go to "Credentials" tab to get the client secret
6. Go to "Service Account Roles" tab to assign appropriate roles (e.g., `view-users`, `manage-users`)

### For Password Grant (Method 2):

1. Go to Keycloak Admin Console
2. Select your realm
3. Go to "Clients" → Select/Create your client (usually `admin-cli` for admin operations)
4. Under "Settings":
   - Enable "Direct Access Grants Enabled"
   - Save the client
5. Ensure your admin user has appropriate realm roles:
   - Go to "Users" → Select admin user
   - Go to "Role Mappings" tab
   - Assign roles like `admin`, `view-users`, `manage-users`, etc.

## Security Considerations

### Client Credentials (Recommended for Production)

**Pros:**
- More secure - no user passwords exposed
- Better for automation and service-to-service communication
- Can be easily rotated via secret regeneration
- Follows OAuth2 best practices

**Cons:**
- Requires initial setup of service account
- Requires role mapping for service account

### Password Grant (Use with Caution)

**Pros:**
- Simple to set up
- Works with existing admin users
- No need to configure service accounts

**Cons:**
- Less secure - exposes admin credentials in code/config
- Password rotation requires code/config updates
- Not recommended for production environments
- May be deprecated in future OAuth2 specifications

## Token Management

Both methods handle token management automatically:
- Tokens are cached and reused until expiration
- Automatic token refresh when expired (with 60-second buffer)
- Thread-safe token management with mutex locks
- Concurrent requests will share the same token

## Error Handling

The client provides detailed error information:

```go
user, err := client.GetUser(ctx, "user-id")
if err != nil {
    // Check error code
    if errors.IsCode(err, errors.ErrCodeUserNotFound) {
        log.Printf("User not found")
    } else if errors.IsCode(err, errors.ErrCodeKeycloakAuth) {
        log.Printf("Authentication failed")
    } else if errors.IsCode(err, errors.ErrCodeKeycloakAPI) {
        log.Printf("API call failed")
    }
    return
}
```

## Testing

Run the tests with:

```bash
go test -v ./internal/keycloak
```

Run specific tests:

```bash
go test -v ./internal/keycloak -run TestAuthenticate
go test -v ./internal/keycloak -run TestAuthenticateWithPassword
```

## Available Methods

Currently supported API methods:

- `GetUser(ctx context.Context, userID string) (*User, error)` - Retrieve user by ID

More methods can be added following the same pattern.
