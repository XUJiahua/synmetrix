# Swagger API Documentation

This document explains how to use and maintain the Swagger API documentation for the go-actions service.

## Overview

The service uses [swaggo/swag](https://github.com/swaggo/swag) to generate Swagger 2.0 documentation from Go annotations in the source code.

## Accessing Swagger UI

When the service is running, you can access the Swagger UI at:

```
http://localhost:3000/swagger/index.html
```

This provides an interactive interface to:
- View all API endpoints
- See request/response schemas
- Try out API calls directly from the browser
- Download OpenAPI specification (JSON/YAML)

## Generating Documentation

### Automatic Generation

The Makefile automatically generates Swagger docs when building:

```bash
make build
```

### Manual Generation

To regenerate Swagger docs manually:

```bash
# Using make
make swagger

# Or using swag directly
swag init -g cmd/serve.go -o docs --parseDependency --parseInternal
```

This generates three files in the `docs/` directory:
- `docs.go` - Go code for embedding the spec
- `swagger.json` - OpenAPI spec in JSON format
- `swagger.yaml` - OpenAPI spec in YAML format

## Adding API Documentation

### General API Information

The main API information is defined in `cmd/serve.go`:

```go
// @title           Go Actions - Keycloak JIT User Sync API
// @version         1.0.0
// @description     API for just-in-time user synchronization between Keycloak and Hasura
// @contact.name    Synmetrix Team
// @contact.url     https://github.com/synmetrix/synmetrix
// @host            localhost:3000
// @BasePath        /
func runServe(cmd *cobra.Command, args []string) error {
    // ...
}
```

### Endpoint Documentation

Each HTTP handler should have Swagger annotations. Example from `cmd/serve.go`:

```go
// healthCheck godoc
// @Summary      Health check
// @Description  Check if the service is running
// @Tags         health
// @Produce      json
// @Success      200 {object} handler.HealthResponse
// @Router       /health [get]
func healthCheck(c *gin.Context) {
    // ...
}
```

Example from `internal/handler/ensure_user.go`:

```go
// EnsureUser godoc
// @Summary      Ensure user exists (Hasura Action)
// @Description  JIT user synchronization - creates user in Hasura if not exists, fetching data from Keycloak
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        request body HasuraActionRequest true "Hasura Action Request"
// @Success      200 {object} HasuraActionResponse
// @Failure      400 {object} ErrorResponse
// @Failure      401 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /ensure-user [post]
func (h *EnsureUserHandler) EnsureUser(c *gin.Context) {
    // ...
}
```

### Model Documentation

Models are documented using struct tags in `internal/handler/types.go`:

```go
type HasuraActionResponse struct {
    ID          string  `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`       // User UUID
    DisplayName string  `json:"display_name" example:"John Doe"`                         // User display name
    AvatarURL   *string `json:"avatar_url,omitempty" example:"https://example.com/avatar.jpg"` // User avatar URL (optional)
    Email       string  `json:"email" example:"john.doe@example.com"`                    // User email
}
```

## Swagger Annotation Reference

### Common Annotations

| Annotation | Description | Example |
|------------|-------------|---------|
| `@Summary` | Short description | `@Summary User login` |
| `@Description` | Detailed description | `@Description Authenticates user with credentials` |
| `@Tags` | Group endpoints | `@Tags users` |
| `@Accept` | Request content type | `@Accept json` |
| `@Produce` | Response content type | `@Produce json` |
| `@Param` | Request parameters | `@Param id path string true "User ID"` |
| `@Success` | Success response | `@Success 200 {object} User` |
| `@Failure` | Error response | `@Failure 400 {object} ErrorResponse` |
| `@Router` | Route path and method | `@Router /users/{id} [get]` |

### Parameter Types

- `path` - URL path parameter
- `query` - URL query parameter
- `header` - HTTP header
- `body` - Request body
- `formData` - Form data

### Example: Complete Endpoint Documentation

```go
// CreateUser godoc
// @Summary      Create a new user
// @Description  Creates a new user in the system with the provided information
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        request body CreateUserRequest true "User creation data"
// @Success      201 {object} User "User created successfully"
// @Failure      400 {object} ErrorResponse "Invalid request"
// @Failure      409 {object} ErrorResponse "User already exists"
// @Failure      500 {object} ErrorResponse "Internal server error"
// @Router       /users [post]
func (h *UserHandler) CreateUser(c *gin.Context) {
    // Implementation
}
```

## Best Practices

### 1. Keep Annotations Up-to-Date

Always update Swagger annotations when:
- Adding new endpoints
- Modifying request/response structures
- Changing route paths
- Adding/removing parameters

### 2. Provide Examples

Use the `example` tag in struct fields for better documentation:

```go
type User struct {
    ID    string `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
    Name  string `json:"name" example:"John Doe"`
    Email string `json:"email" example:"john@example.com"`
}
```

### 3. Document All Response Codes

Include all possible HTTP status codes:

```go
// @Success 200 {object} User
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
```

### 4. Use Descriptive Tags

Group related endpoints with tags:

```go
// @Tags users
// @Tags authentication
// @Tags health
```

### 5. Add Field Descriptions

Use inline comments for struct field documentation:

```go
type User struct {
    ID          string `json:"id"`          // Unique user identifier (UUID)
    DisplayName string `json:"display_name"` // User's display name
    Email       string `json:"email"`        // User's email address
}
```

## Troubleshooting

### Swagger UI Not Loading

1. Check that the service is running
2. Verify the URL: `http://localhost:3000/swagger/index.html`
3. Check for errors in server logs
4. Ensure docs were generated: `ls -la docs/`

### Documentation Not Updating

1. Regenerate docs: `make swagger`
2. Rebuild the binary: `make build`
3. Restart the service
4. Clear browser cache (Ctrl+F5)

### Parse Errors

Common issues when running `swag init`:

1. **Missing import**: Ensure all referenced types are imported
   ```go
   import "go-actions/internal/handler"
   ```

2. **Invalid syntax**: Check annotation format
   ```go
   // Correct
   // @Param id path string true "User ID"

   // Incorrect (missing quotes)
   // @Param id path string true User ID
   ```

3. **Type not found**: Ensure struct is exported (capitalized)
   ```go
   // Correct
   type User struct { }

   // Incorrect (not exported)
   type user struct { }
   ```

## CI/CD Integration

### Pre-commit Hook

Add to `.git/hooks/pre-commit`:

```bash
#!/bin/bash
# Regenerate Swagger docs before commit
make swagger
git add docs/
```

### Dockerfile

The Dockerfile already includes Swagger generation:

```dockerfile
# Install swag
RUN go install github.com/swaggo/swag/cmd/swag@latest

# Generate docs
RUN swag init -g cmd/serve.go -o docs
```

## Additional Resources

- [Swaggo Documentation](https://github.com/swaggo/swag)
- [Swagger 2.0 Specification](https://swagger.io/specification/v2/)
- [Gin Swagger Middleware](https://github.com/swaggo/gin-swagger)
- [OpenAPI 3.0 (for future migration)](https://swagger.io/specification/)

## Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.0.0 | 2025-11-20 | Initial Swagger integration |
