package keycloak

import (
	"context"
	"encoding/json"
	"fmt"
	"go-actions/internal/config"
	"go-actions/pkg/errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client handles communication with Keycloak Admin API
type Client struct {
	baseURL      string
	realm        string
	clientID     string
	clientSecret string

	// Token management
	accessToken string
	tokenExpiry time.Time
	mu          sync.RWMutex

	httpClient *http.Client
}

// NewClient creates a new Keycloak client
func NewClient(cfg *config.Config) *Client {
	return &Client{
		baseURL:      cfg.KeycloakURL,
		realm:        cfg.KeycloakRealm,
		clientID:     cfg.KeycloakClientID,
		clientSecret: cfg.KeycloakClientSecret,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetUser retrieves a user from Keycloak by ID
func (c *Client) GetUser(ctx context.Context, userID string) (*User, error) {
	if err := c.ensureAuthenticated(ctx); err != nil {
		return nil, err
	}

	c.mu.RLock()
	token := c.accessToken
	c.mu.RUnlock()

	url := fmt.Sprintf("%s/admin/realms/%s/users/%s", c.baseURL, c.realm, userID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeKeycloakAPI, "create request failed")
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeKeycloakAPI, "request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New(errors.ErrCodeUserNotFound, fmt.Sprintf("user %s not found in Keycloak", userID))
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(errors.ErrCodeKeycloakAPI, fmt.Sprintf("unexpected status code: %d", resp.StatusCode))
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeKeycloakAPI, "decode response failed")
	}

	return &user, nil
}

// ensureAuthenticated ensures the client has a valid access token
func (c *Client) ensureAuthenticated(ctx context.Context) error {
	c.mu.RLock()
	// Check if token is still valid (with 60 second buffer)
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second)) {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	// Need to acquire new token
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second)) {
		return nil
	}

	return c.authenticate(ctx)
}

// authenticate obtains a new access token using client credentials
func (c *Client) authenticate(ctx context.Context) error {
	tokenURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", c.baseURL, c.realm)

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeKeycloakAuth, "create auth request failed")
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeKeycloakAuth, "auth request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New(errors.ErrCodeKeycloakAuth, fmt.Sprintf("authentication failed with status: %d", resp.StatusCode))
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return errors.Wrap(err, errors.ErrCodeKeycloakAuth, "decode token response failed")
	}

	c.accessToken = tokenResp.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return nil
}
