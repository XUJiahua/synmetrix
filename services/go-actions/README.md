# Go Actions - Keycloak JIT User Sync Service

A Go-based microservice for just-in-time (JIT) user synchronization between Keycloak and Hasura for the Synmetrix platform.

## Overview

This service handles automatic user creation in Hasura when users first log in via Keycloak. It operates as a Hasura Action handler, ensuring users exist in the database without requiring full user synchronization.

### Features

- ✅ **RPC Architecture**: Extensible RPC-style routing for multiple Hasura Actions
- ✅ **JIT User Sync**: Automatically creates users on first login
- ✅ **Keycloak Integration**: Fetches user data from Keycloak Admin API
- ✅ **Hasura GraphQL**: Stores user data directly in Hasura via GraphQL
- ✅ **Auto Token Refresh**: Manages Keycloak Service Account tokens automatically
- ✅ **Default Team Creation**: Creates a default team for new users
- ✅ **Gin Web Framework**: High-performance HTTP router and middleware
- ✅ **Swagger Documentation**: Auto-generated API documentation
- ✅ **Graceful Shutdown**: Handles SIGTERM/SIGINT signals properly
- ✅ **Health Checks**: Built-in health endpoint for monitoring

## Architecture

```
┌─────────────┐
│  Keycloak   │ ← User authenticates
└──────┬──────┘
       │ JWT
       ▼
┌─────────────┐
│  Frontend   │ → GraphQL query (with JWT)
└──────┬──────┘
       │
       ▼
┌─────────────┐
│   Hasura    │ → Triggers Action if user not found
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Go Actions  │ 1. Check user exists
│  (This)     │ 2. Fetch from Keycloak if not
│             │ 3. Create in Hasura
│             │ 4. Create default team
└─────────────┘
```

## Project Structure

```
services/go-actions/
├── cmd/                    # CLI commands
│   ├── root.go            # Root command
│   └── serve.go           # HTTP server command (Gin routes)
├── internal/              # Private application code
│   ├── config/            # Configuration management
│   ├── keycloak/          # Keycloak Admin Client
│   ├── hasura/            # Hasura GraphQL Client
│   ├── handler/           # Common handler types
│   ├── rpc/               # RPC action handlers
│   │   ├── router.go      # RPC router
│   │   └── ensure_user.go # User sync action
│   └── service/           # Business logic
├── pkg/                   # Public packages
│   ├── logger/            # Structured logging
│   └── errors/            # Error handling
├── docs/                  # Swagger documentation
│   ├── docs.go            # Generated docs
│   ├── swagger.json       # Swagger JSON spec
│   └── swagger.yaml       # Swagger YAML spec
├── main.go                # Entry point
├── Dockerfile             # Docker build
├── Makefile               # Build automation
├── README.md              # This file
├── INTEGRATION.md         # Hasura integration guide
├── ADDING_NEW_ACTION.md   # Guide for adding new actions
├── SWAGGER_GUIDE.md       # Swagger documentation guide
├── MIGRATION_TO_RPC.md    # RPC architecture migration docs
└── TODO.md                # Hasura Actions spec and security notes
```

## Prerequisites

- Go 1.25+
- Keycloak instance with Service Account configured
- Hasura instance with GraphQL endpoint
- Access to Keycloak Admin API

## Configuration

### Environment Variables

Create a `.env` file or set environment variables:

```bash
# Server Configuration
SERVER_PORT=3000

# Keycloak Configuration
KEYCLOAK_URL=http://keycloak:8080
KEYCLOAK_REALM=synmetrix
KEYCLOAK_CLIENT_ID=hasura-sync-service
KEYCLOAK_CLIENT_SECRET=your-client-secret

# Hasura Configuration
HASURA_ENDPOINT=http://hasura:8080/v1/graphql
HASURA_ADMIN_SECRET=your-admin-secret

# JWT Configuration (optional)
JWT_SECRET=your-jwt-secret
JWT_CLAIMS_NAMESPACE=https://hasura.io/jwt/claims
```

See `.env.example` for a complete template.

### Keycloak Service Account Setup

1. **Create Client** in Keycloak Admin Console:
   - Client ID: `hasura-sync-service`
   - Client authentication: ON
   - Authentication flow: Enable "Service accounts roles"

2. **Configure Service Account Roles**:
   - Go to "Service account roles" tab
   - Assign role: `realm-management` → `view-users`
   - Assign role: `realm-management` → `query-users`

3. **Get Client Secret**:
   - Go to "Credentials" tab
   - Copy the Client Secret
   - Set as `KEYCLOAK_CLIENT_SECRET` environment variable

## Installation

### Local Development

```bash
# Clone the repository
cd services/go-actions

# Install dependencies
go mod download

# Run the server
go run main.go serve
```

### Using Docker

```bash
# Build the image
docker build -t synmetrix/go-actions:latest .

# Run the container
docker run -p 3000:3000 \
  -e KEYCLOAK_URL=http://keycloak:8080 \
  -e KEYCLOAK_REALM=synmetrix \
  -e KEYCLOAK_CLIENT_ID=hasura-sync-service \
  -e KEYCLOAK_CLIENT_SECRET=your-secret \
  -e HASURA_ENDPOINT=http://hasura:8080/v1/graphql \
  -e HASURA_ADMIN_SECRET=your-admin-secret \
  synmetrix/go-actions:latest
```

### Using Docker Compose

Add to `docker-compose.yml`:

```yaml
services:
  go-actions:
    build: ./services/go-actions
    ports:
      - "3000:3000"
    environment:
      - KEYCLOAK_URL=http://keycloak:8080
      - KEYCLOAK_REALM=synmetrix
      - KEYCLOAK_CLIENT_ID=hasura-sync-service
      - KEYCLOAK_CLIENT_SECRET=${KC_CLIENT_SECRET}
      - HASURA_ENDPOINT=http://hasura:8080/v1/graphql
      - HASURA_ADMIN_SECRET=${HASURA_ADMIN_SECRET}
    depends_on:
      - keycloak
      - hasura
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:3000/health"]
      interval: 30s
      timeout: 3s
      retries: 3
```

## API Endpoints

### POST /rpc/:method

RPC-style endpoint for handling Hasura Actions.

**Available Methods**:
- `ensure_user` - JIT user synchronization

**Request** (from Hasura):
```json
{
  "session_variables": {
    "x-hasura-user-id": "uuid-here"
  },
  "input": {},
  "action": {
    "name": "ensure_user_exists"
  }
}
```

**Response**:
```json
{
  "id": "uuid",
  "display_name": "John Doe",
  "avatar_url": null,
  "email": "john@example.com"
}
```

**Adding New Actions**: See [ADDING_NEW_ACTION.md](ADDING_NEW_ACTION.md) for a complete guide.

### GET /health

Health check endpoint.

**Response**:
```json
{
  "status": "ok",
  "version": "1.0.0",
  "service": "go-actions"
}
```

### GET /swagger/index.html

Swagger UI for interactive API documentation.

Access the Swagger UI at: `http://localhost:3000/swagger/index.html`

The Swagger documentation provides:
- Interactive API explorer
- Request/response examples
- Schema definitions
- Try-it-out functionality
- **Individual documentation for each RPC action**

Each action has its own documented endpoint with specific input/output schemas. See [SWAGGER_GUIDE.md](SWAGGER_GUIDE.md) for details on how to add Swagger documentation for new actions.

## Hasura Integration

### Action Configuration

Create a Hasura Action in `hasura/metadata/actions.yaml`:

```yaml
- name: ensure_user_exists
  definition:
    kind: synchronous
    handler: http://go-actions:3000/rpc/ensure_user
    forward_client_headers: true
  permissions:
    - role: user
```

For complete integration instructions, see [INTEGRATION.md](INTEGRATION.md).

### GraphQL Schema

The service expects these tables in Hasura:

```sql
-- users table
CREATE TABLE users (
  id UUID PRIMARY KEY,
  display_name TEXT NOT NULL,
  avatar_url TEXT
);

-- auth_accounts table
CREATE TABLE auth_accounts (
  user_id UUID REFERENCES users(id),
  email CITEXT UNIQUE NOT NULL
);

-- teams table
CREATE TABLE teams (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL
);

-- members table
CREATE TABLE members (
  user_id UUID REFERENCES users(id),
  team_id UUID REFERENCES teams(id)
);

-- member_roles table
CREATE TABLE member_roles (
  member_id UUID REFERENCES members(id),
  team_role TEXT NOT NULL
);
```

## Usage

### CLI Commands

```bash
# Start the server
go-actions serve

# Show help
go-actions --help

# Show version
go-actions version
```

### Testing the Service

```bash
# Health check
curl http://localhost:3000/health

# View Swagger documentation
open http://localhost:3000/swagger/index.html

# Test user sync (simulate Hasura Action)
curl -X POST http://localhost:3000/ensure-user \
  -H "Content-Type: application/json" \
  -d '{
    "session_variables": {
      "x-hasura-user-id": "your-user-uuid"
    },
    "input": {},
    "action": {"name": "ensure_user_exists"}
  }'
```

## Development

### Building

```bash
# Generate Swagger docs
make swagger
# or
swag init -g cmd/serve.go -o docs --parseDependency --parseInternal

# Build binary (will generate Swagger docs automatically)
make build
# or
go build -o go-actions .

# Run tests
go test ./...

# Run with live reload (using air)
air
```

### Code Structure

- **cmd/**: Command-line interface using Cobra
- **internal/config**: Environment variable configuration
- **internal/keycloak**: Keycloak Admin API client with auto token refresh
- **internal/hasura**: GraphQL client for Hasura mutations/queries
- **internal/service**: Business logic for user synchronization
- **internal/handler**: HTTP request handlers (Gin-based)
- **pkg/logger**: Structured logging with Zap
- **pkg/errors**: Custom error types with error codes
- **docs/**: Auto-generated Swagger documentation

## Monitoring

### Logs

The service uses structured JSON logging (production) or colored console logging (development):

```json
{
  "level": "info",
  "timestamp": "2025-11-20T10:30:00Z",
  "msg": "Ensuring user exists: uuid-here"
}
```

### Metrics

Health check endpoint is available at `/health` for monitoring tools like:
- Kubernetes liveness/readiness probes
- Docker healthchecks
- External monitoring systems

## Error Handling

The service uses typed errors with error codes:

- `KEYCLOAK_AUTH_ERROR`: Failed to authenticate with Keycloak
- `KEYCLOAK_API_ERROR`: Keycloak API request failed
- `HASURA_API_ERROR`: Hasura GraphQL request failed
- `USER_NOT_FOUND`: User not found in Keycloak
- `USER_CREATE_FAILED`: Failed to create user in Hasura
- `INVALID_REQUEST`: Invalid HTTP request
- `UNAUTHORIZED`: Missing or invalid authentication

## Security

- ✅ Service Account authentication (no user credentials)
- ✅ Minimum required permissions (view-users, query-users)
- ✅ Non-root Docker container
- ✅ Hasura Admin Secret for GraphQL
- ✅ Request timeout enforcement
- ✅ Graceful shutdown to prevent request loss

## Performance

- **Token Caching**: Keycloak tokens are cached and auto-refreshed
- **Concurrent Safe**: Thread-safe token management with RWMutex
- **Async Team Creation**: Default team creation doesn't block response
- **Connection Pooling**: HTTP clients reuse connections

## Troubleshooting

### User not found in Keycloak

Check that:
1. User exists in Keycloak realm
2. Service Account has `view-users` permission
3. `KEYCLOAK_REALM` is correct

### GraphQL errors

Check that:
1. Hasura admin secret is correct
2. GraphQL endpoint is accessible
3. Database tables exist with correct schema

### Authentication failures

Check that:
1. Client Secret is correct
2. Service Account is enabled
3. Keycloak URL is accessible

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## Related Documentation

- [Keycloak JIT Sync Design](./keycloak-jit-sync.md)
- [Synmetrix Main Documentation](../../README.md)
