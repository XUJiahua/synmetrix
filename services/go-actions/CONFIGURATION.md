# Configuration Guide

This document describes the configuration options for the go-actions service.

## Environment Variables

Configuration is loaded from environment variables. You can also use a `.env` file in the project root.

### Server Configuration

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `SERVER_PORT` | HTTP server port | `3000` | No |

### Keycloak Configuration

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `KEYCLOAK_URL` | Keycloak base URL | - | Yes |
| `KEYCLOAK_REALM` | Keycloak realm name | - | Yes |
| `KEYCLOAK_CLIENT_ID` | Keycloak client ID | - | Yes |

### Keycloak Authentication Methods

You can authenticate with Keycloak using one of two methods:

#### Method 1: Client Credentials (Service Account) ✅ RECOMMENDED

Use this method for production environments. It's more secure and follows OAuth2 best practices.

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `KEYCLOAK_CLIENT_SECRET` | Client secret for service account | - | Yes* |

**Keycloak Setup:**
1. Go to Clients → Select your client
2. Enable "Service Accounts Enabled"
3. Go to "Credentials" tab → Copy the secret
4. Go to "Service Account Roles" tab → Assign required roles (e.g., `view-users`, `manage-users`)

#### Method 2: Admin User (Password Grant) ⚠️ DEVELOPMENT ONLY

Use this method only for local development or testing. Not recommended for production.

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `KEYCLOAK_ADMIN_USERNAME` | Admin username | - | Yes* |
| `KEYCLOAK_ADMIN_PASSWORD` | Admin password | - | Yes* |

**Keycloak Setup:**
1. Go to Clients → Select your client (e.g., `admin-cli`)
2. Enable "Direct Access Grants Enabled"
3. Ensure your admin user has appropriate realm roles

**Note:** You must provide either `KEYCLOAK_CLIENT_SECRET` or both `KEYCLOAK_ADMIN_USERNAME` and `KEYCLOAK_ADMIN_PASSWORD`. If both are provided, admin user authentication takes precedence.

### Hasura Configuration

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `HASURA_ENDPOINT` | Hasura GraphQL endpoint | - | Yes |
| `HASURA_ADMIN_SECRET` | Hasura admin secret | - | Yes |

### JWT Configuration

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `JWT_SECRET` | JWT signing secret | - | No |
| `JWT_CLAIMS_NAMESPACE` | Hasura JWT claims namespace | `https://hasura.io/jwt/claims` | No |

## Configuration Examples

### Example 1: Production with Service Account

```env
SERVER_PORT=3000

# Keycloak with service account
KEYCLOAK_URL=http://keycloak:8080
KEYCLOAK_REALM=synmetrix
KEYCLOAK_CLIENT_ID=hasura-sync-service
KEYCLOAK_CLIENT_SECRET=abc123xyz789

# Hasura
HASURA_ENDPOINT=http://hasura:8080/v1/graphql
HASURA_ADMIN_SECRET=my-admin-secret

# JWT
JWT_SECRET=my-jwt-secret
JWT_CLAIMS_NAMESPACE=https://hasura.io/jwt/claims
```

### Example 2: Development with Admin User

```env
SERVER_PORT=3000

# Keycloak with admin user
KEYCLOAK_URL=http://localhost:8082
KEYCLOAK_REALM=master
KEYCLOAK_CLIENT_ID=admin-cli
KEYCLOAK_ADMIN_USERNAME=admin
KEYCLOAK_ADMIN_PASSWORD=admin

# Hasura
HASURA_ENDPOINT=http://localhost:8080/v1/graphql
HASURA_ADMIN_SECRET=dev-admin-secret

# JWT
JWT_SECRET=dev-jwt-secret
```

### Example 3: Both Credentials (Admin User Takes Precedence)

```env
# If both are provided, admin user authentication is used
KEYCLOAK_URL=http://localhost:8082
KEYCLOAK_REALM=master
KEYCLOAK_CLIENT_ID=admin-cli
KEYCLOAK_CLIENT_SECRET=service-account-secret
KEYCLOAK_ADMIN_USERNAME=admin
KEYCLOAK_ADMIN_PASSWORD=admin-password

# Result: Will use admin/admin-password (password grant)
```

## Authentication Method Selection

The service automatically selects the authentication method based on available credentials:

```
IF KEYCLOAK_ADMIN_USERNAME AND KEYCLOAK_ADMIN_PASSWORD are set:
    → Use Password Grant (Admin User)
ELSE IF KEYCLOAK_CLIENT_SECRET is set:
    → Use Client Credentials (Service Account)
ELSE:
    → Configuration Error
```

## Configuration Loading

The service loads configuration in the following order:

1. Environment variables
2. `.env` file (if present)

To load configuration:

```go
cfg, err := config.Load()
if err != nil {
    log.Fatalf("Failed to load config: %v", err)
}

// Create Keycloak client (automatically selects auth method)
keycloakClient := keycloak.NewClient(cfg)
```

## Validation

Configuration is validated on load. The following validations are performed:

- `KEYCLOAK_URL` must be set
- `KEYCLOAK_REALM` must be set
- `KEYCLOAK_CLIENT_ID` must be set
- Either `KEYCLOAK_CLIENT_SECRET` or both `KEYCLOAK_ADMIN_USERNAME` and `KEYCLOAK_ADMIN_PASSWORD` must be set
- `HASURA_ENDPOINT` must be set
- `HASURA_ADMIN_SECRET` must be set

If validation fails, the service will not start and will return a descriptive error.

## Security Best Practices

### Production Environments

✅ **DO:**
- Use service account authentication (client credentials)
- Store secrets in a secure vault (e.g., HashiCorp Vault, AWS Secrets Manager)
- Use environment variables, not `.env` files
- Rotate secrets regularly
- Use minimal required permissions for service accounts

❌ **DON'T:**
- Use admin username/password authentication
- Commit `.env` files with real credentials
- Use weak or default passwords
- Share secrets across environments

### Development Environments

✅ **DO:**
- Use `.env` files for local configuration
- Add `.env` to `.gitignore`
- Use separate credentials from production
- Document setup steps for team members

❌ **DON'T:**
- Use production credentials in development
- Commit real credentials to version control
- Share credentials in plain text

## Troubleshooting

### Error: "KEYCLOAK_CLIENT_SECRET or (KEYCLOAK_ADMIN_USERNAME and KEYCLOAK_ADMIN_PASSWORD) is required"

**Cause:** No authentication credentials provided.

**Solution:** Set either:
- `KEYCLOAK_CLIENT_SECRET`, or
- Both `KEYCLOAK_ADMIN_USERNAME` and `KEYCLOAK_ADMIN_PASSWORD`

### Error: "authentication failed with status: 401"

**Cause:** Invalid credentials.

**Solutions:**
- For client credentials: Verify `KEYCLOAK_CLIENT_SECRET` is correct
- For admin user: Verify username/password are correct
- Check that the client has appropriate service account roles or direct access grants enabled

### Error: "user not found in Keycloak"

**Cause:** The service account or admin user doesn't have permission to view users.

**Solutions:**
- For service account: Add `view-users` role to service account
- For admin user: Verify user has realm-management roles

## Migration Guide

### Migrating from Client Credentials Only

If you're currently using only client credentials and want to support admin user authentication:

1. Update your `.env.example`:
   ```env
   # Add these lines
   # KEYCLOAK_ADMIN_USERNAME=admin
   # KEYCLOAK_ADMIN_PASSWORD=admin-password
   ```

2. No code changes required - the service automatically detects and uses the appropriate method

3. Update documentation for your team

### Switching Authentication Methods

To switch between methods, simply change your environment variables:

**From Service Account to Admin User:**
```bash
# Before
KEYCLOAK_CLIENT_SECRET=abc123

# After
# KEYCLOAK_CLIENT_SECRET=abc123  # Comment out or remove
KEYCLOAK_ADMIN_USERNAME=admin
KEYCLOAK_ADMIN_PASSWORD=admin-password
```

**From Admin User to Service Account:**
```bash
# Before
KEYCLOAK_ADMIN_USERNAME=admin
KEYCLOAK_ADMIN_PASSWORD=admin-password

# After
KEYCLOAK_CLIENT_SECRET=abc123
# KEYCLOAK_ADMIN_USERNAME=admin       # Comment out or remove
# KEYCLOAK_ADMIN_PASSWORD=admin-password  # Comment out or remove
```

No code changes or restarts required beyond setting the new environment variables.
