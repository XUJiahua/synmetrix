package keycloak

// User represents a Keycloak user
type User struct {
	ID               string `json:"id"`
	Email            string `json:"email"`
	EmailVerified    bool   `json:"emailVerified"`
	FirstName        string `json:"firstName"`
	LastName         string `json:"lastName"`
	Username         string `json:"username"`
	Enabled          bool   `json:"enabled"`
	CreatedTimestamp int64  `json:"createdTimestamp"`
}

// TokenResponse represents the OAuth2 token response
type TokenResponse struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
	TokenType        string `json:"token_type"`
}
