package hasura

// GraphQL request/response types

// GraphQLRequest represents a GraphQL request
type GraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

// GraphQLResponse represents a GraphQL response
type GraphQLResponse struct {
	Data   interface{}    `json:"data"`
	Errors []GraphQLError `json:"errors,omitempty"`
}

// GraphQLError represents a GraphQL error
type GraphQLError struct {
	Message string `json:"message"`
}

// CreateUserInput represents the input for creating a user
type CreateUserInput struct {
	ID          string
	DisplayName string
	Email       string
}

// UserData represents user data returned from Hasura
type UserData struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
	Account     struct {
		Email string `json:"email"`
	} `json:"account"`
}

// CheckUserResponse represents the response for checking if user exists
type CheckUserResponse struct {
	UsersByPK *struct {
		ID string `json:"id"`
	} `json:"users_by_pk"`
}

// CreateUserResponse represents the response for creating a user
type CreateUserResponse struct {
	InsertUsersOne *struct {
		ID          string  `json:"id"`
		DisplayName string  `json:"display_name"`
		AvatarURL   *string `json:"avatar_url"`
	} `json:"insert_users_one"`
}

// CreateAccountResponse represents the response for creating an account
type CreateAccountResponse struct {
	InsertAuthAccountsOne *struct {
		ID     string `json:"id"`
		UserID string `json:"user_id"`
		Email  string `json:"email"`
	} `json:"insert_auth_accounts_one"`
}

// CreateTeamResponse represents the response for creating a team
type CreateTeamResponse struct {
	InsertTeamsOne *struct {
		ID string `json:"id"`
	} `json:"insert_teams_one"`
}

// GetUserResponse represents the response for getting a user
type GetUserResponse struct {
	UsersByPK *UserData `json:"users_by_pk"`
}
