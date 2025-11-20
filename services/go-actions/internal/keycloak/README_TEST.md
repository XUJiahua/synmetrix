# Keycloak Package Tests

This directory contains unit tests and integration tests for the Keycloak client.

## Test Files

- `client_test.go` - Unit tests using mock HTTP servers
- `integration_test.go` - Integration tests against a real Keycloak instance

## Running Tests

### Unit Tests

Unit tests use mock HTTP servers and don't require a running Keycloak instance:

```bash
# Run all unit tests
go test -v ./internal/keycloak

# Run specific test
go test -v ./internal/keycloak -run TestNewClient

# Run with coverage
go test -cover ./internal/keycloak

# Generate coverage report
go test -coverprofile=coverage.out ./internal/keycloak
go tool cover -html=coverage.out
```

### Integration Tests

Integration tests require a running Keycloak instance at `http://localhost:8082` with admin credentials `admin/admin`.

```bash
# Start Keycloak with Docker
docker run -d \
  --name keycloak-test \
  -p 8082:8080 \
  -e KEYCLOAK_ADMIN=admin \
  -e KEYCLOAK_ADMIN_PASSWORD=admin \
  quay.io/keycloak/keycloak:latest \
  start-dev

# Wait for Keycloak to start (usually takes 30-60 seconds)

# Run integration tests
go test -tags=integration -v ./internal/keycloak

# Or use custom Keycloak URL
KEYCLOAK_URL=http://localhost:8082 go test -tags=integration -v ./internal/keycloak

# Clean up
docker stop keycloak-test
docker rm keycloak-test
```

## Test Coverage

The test suite covers:

### Unit Tests
- ✅ Client initialization
- ✅ Authentication flow (client credentials grant)
- ✅ Authentication failure scenarios (401, 500)
- ✅ Token caching and refresh logic
- ✅ User retrieval
- ✅ User not found scenario (404)
- ✅ Context cancellation
- ✅ Concurrent authentication (thread safety)

### Integration Tests
- ✅ Real authentication against Keycloak
- ✅ API request validation
- ✅ Token refresh behavior
- ✅ Concurrent request handling

## Keycloak Setup for Integration Tests

If you're using the Keycloak instance at `http://localhost:8082` with credentials `admin/admin`, the tests should work out of the box.

### Configuration

The integration tests use the following defaults:
- **URL**: `http://localhost:8082` (can be overridden with `KEYCLOAK_URL` env var)
- **Realm**: `master`
- **Client ID**: `admin-cli`
- **Client Secret**: Empty (admin-cli uses direct access grant)

### Troubleshooting

If integration tests fail:

1. Verify Keycloak is running:
   ```bash
   curl http://localhost:8082/health/ready
   ```

2. Check admin credentials work:
   ```bash
   curl -X POST http://localhost:8082/realms/master/protocol/openid-connect/token \
     -H "Content-Type: application/x-www-form-urlencoded" \
     -d "grant_type=password" \
     -d "client_id=admin-cli" \
     -d "username=admin" \
     -d "password=admin"
   ```

3. Ensure the `admin-cli` client has proper permissions:
   - Log into Keycloak admin console
   - Navigate to Clients → admin-cli
   - Verify "Service Accounts Enabled" is ON
   - Check Service Account Roles include realm-management roles

## Continuous Integration

To run tests in CI:

```bash
# Unit tests only (no dependencies)
go test -v ./internal/keycloak

# All tests including integration (requires Keycloak)
docker-compose up -d keycloak
sleep 30  # Wait for Keycloak to be ready
go test -tags=integration -v ./internal/keycloak
docker-compose down
```

## Test Output Example

```
=== RUN   TestNewClient
--- PASS: TestNewClient (0.00s)
=== RUN   TestAuthenticate
--- PASS: TestAuthenticate (0.00s)
=== RUN   TestAuthenticateFailure
=== RUN   TestAuthenticateFailure/Unauthorized
=== RUN   TestAuthenticateFailure/Server_Error
--- PASS: TestAuthenticateFailure (0.00s)
    --- PASS: TestAuthenticateFailure/Unauthorized (0.00s)
    --- PASS: TestAuthenticateFailure/Server_Error (0.00s)
=== RUN   TestEnsureAuthenticated
--- PASS: TestEnsureAuthenticated (0.00s)
=== RUN   TestGetUser
--- PASS: TestGetUser (0.00s)
=== RUN   TestGetUserNotFound
--- PASS: TestGetUserNotFound (0.00s)
=== RUN   TestGetUserContextCancellation
--- PASS: TestGetUserContextCancellation (0.00s)
=== RUN   TestConcurrentAuthentication
--- PASS: TestConcurrentAuthentication (0.01s)
PASS
ok      go-actions/internal/keycloak    0.025s
```
