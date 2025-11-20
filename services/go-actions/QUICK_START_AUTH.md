# Quick Start: Keycloak Authentication

Two ways to authenticate with Keycloak - choose one:

## 🔐 Option 1: Service Account (Production) ✅

**Environment Variables:**
```env
KEYCLOAK_CLIENT_SECRET=your-client-secret
```

**Keycloak Setup:**
```
1. Clients → [Your Client] → Settings
2. ✅ Enable "Service Accounts Enabled"
3. Credentials tab → Copy secret
4. Service Account Roles → Assign roles
```

## 👤 Option 2: Admin User (Development) ⚠️

**Environment Variables:**
```env
KEYCLOAK_ADMIN_USERNAME=admin
KEYCLOAK_ADMIN_PASSWORD=admin-password
```

**Keycloak Setup:**
```
1. Clients → [Your Client] → Settings
2. ✅ Enable "Direct Access Grants Enabled"
3. Users → [Admin User] → Role Mappings → Assign roles
```

## Code Usage

**Both methods use the same code:**

```go
// Load config (auto-detects auth method)
cfg, err := config.Load()
if err != nil {
    log.Fatal(err)
}

// Create client (auto-selects client credentials or password grant)
client := keycloak.NewClient(cfg)

// Use it
user, err := client.GetUser(ctx, "user-id")
```

## Which to Use?

| Environment | Use |
|-------------|-----|
| Production  | ✅ Service Account |
| Staging     | ✅ Service Account |
| Development | ⚠️ Admin User (optional) |
| Testing     | ⚠️ Admin User (optional) |

## Quick Test

```bash
# Set environment variables
export KEYCLOAK_URL=http://localhost:8082
export KEYCLOAK_REALM=master
export KEYCLOAK_CLIENT_ID=admin-cli

# Option 1: Service Account
export KEYCLOAK_CLIENT_SECRET=your-secret

# OR Option 2: Admin User
export KEYCLOAK_ADMIN_USERNAME=admin
export KEYCLOAK_ADMIN_PASSWORD=admin

# Run the service
go run cmd/main.go
```

## Need Help?

- 📖 Full guide: `CONFIGURATION.md`
- 💡 Examples: `internal/keycloak/USAGE_EXAMPLE.md`
- 🔧 Package docs: `internal/keycloak/README.md`
