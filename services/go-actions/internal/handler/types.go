package handler

// HasuraActionRequest represents the request from Hasura Action
type HasuraActionRequest struct {
	// Session variables from Hasura JWT
	SessionVariables map[string]string `json:"session_variables" example:"x-hasura-user-id:uuid-here"` // @description Session variables from Hasura JWT

	// Action input parameters (optional). Example: {"user_id": "550e8400-e29b-41d4-a716-446655440000"}
	Input map[string]interface{} `json:"input"` // @description Action input parameters, e.g., user_id for ensure_user action

	// Action metadata
	Action struct {
		Name string `json:"name" example:"ensure_user_exists"` // @description Action name
	} `json:"action"`
}

// HasuraActionResponse represents the response to Hasura Action
type HasuraActionResponse struct {
	ID          string  `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`             // User UUID
	DisplayName string  `json:"display_name" example:"John Doe"`                               // User display name
	AvatarURL   *string `json:"avatar_url,omitempty" example:"https://example.com/avatar.jpg"` // User avatar URL (optional)
	Email       string  `json:"email" example:"john.doe@example.com"`                          // User email
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Message string `json:"message" example:"invalid request body"`   // Error message
	Code    string `json:"code,omitempty" example:"INVALID_REQUEST"` // Error code (optional)
}

// HealthResponse represents a health check response
type HealthResponse struct {
	Status  string `json:"status" example:"ok"`          // Service status
	Version string `json:"version" example:"1.0.0"`      // Service version
	Service string `json:"service" example:"go-actions"` // Service name
}
