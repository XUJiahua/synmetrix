package keycloak

import (
	"context"
	"encoding/json"
	"fmt"
	"go-actions/internal/config"
	"go-actions/pkg/errors"
	"go-actions/pkg/logger"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// AuthMethod represents the authentication method
type AuthMethod string

const (
	AuthMethodClientCredentials AuthMethod = "client_credentials"
	AuthMethodPassword          AuthMethod = "password"
)

// Client handles communication with Keycloak Admin API
type Client struct {
	baseURL      string
	realm        string
	clientID     string
	clientSecret string

	// Admin user credentials (for password grant type)
	adminUsername string
	adminPassword string

	// Authentication method
	authMethod AuthMethod

	// Token management
	accessToken string
	tokenExpiry time.Time
	mu          sync.RWMutex

	httpClient *http.Client
	logger     *logger.Logger
}

// NewClient creates a new Keycloak client based on the configuration
// It automatically selects the appropriate authentication method:
// - If admin username/password are provided, uses password grant
// - Otherwise, uses client credentials (service account)
func NewClient(cfg *config.Config) *Client {
	return NewClientWithLogger(cfg, logger.New())
}

// NewClientWithLogger creates a new Keycloak client with custom logger
func NewClientWithLogger(cfg *config.Config, log *logger.Logger) *Client {
	client := &Client{
		baseURL:  cfg.KeycloakURL,
		realm:    cfg.KeycloakRealm,
		clientID: cfg.KeycloakClientID,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: log,
	}

	// Choose authentication method based on available credentials
	if cfg.UseAdminUserAuth() {
		client.authMethod = AuthMethodPassword
		client.adminUsername = cfg.KeycloakAdminUsername
		client.adminPassword = cfg.KeycloakAdminPassword
		log.Debugf("Keycloak client initialized with password grant auth method")
	} else {
		client.authMethod = AuthMethodClientCredentials
		client.clientSecret = cfg.KeycloakClientSecret
		log.Debugf("Keycloak client initialized with service account auth method")
	}

	return client
}

// NewClientWithAdminUser creates a new Keycloak client using admin username and password
func NewClientWithAdminUser(baseURL, realm, clientID, adminUsername, adminPassword string) *Client {
	return &Client{
		baseURL:       baseURL,
		realm:         realm,
		clientID:      clientID,
		adminUsername: adminUsername,
		adminPassword: adminPassword,
		authMethod:    AuthMethodPassword,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger.New(),
	}
}

// GetUser retrieves a user from Keycloak by ID
func (c *Client) GetUser(ctx context.Context, userID string) (*User, error) {
	c.logger.Debugf("GetUser: fetching user %s from Keycloak", userID)

	if err := c.ensureAuthenticated(ctx); err != nil {
		c.logger.Errorf("GetUser: authentication failed: %v", err)
		return nil, err
	}

	c.mu.RLock()
	token := c.accessToken
	c.mu.RUnlock()

	url := fmt.Sprintf("%s/admin/realms/%s/users/%s", c.baseURL, c.realm, userID)
	c.logger.Debugf("GetUser: making request to %s", url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		c.logger.Errorf("GetUser: failed to create request: %v", err)
		return nil, errors.Wrap(err, errors.ErrCodeKeycloakAPI, "create request failed")
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Errorf("GetUser: request failed: %v", err)
		return nil, errors.Wrap(err, errors.ErrCodeKeycloakAPI, "request failed")
	}
	defer resp.Body.Close()

	c.logger.Debugf("GetUser: received status code %d", resp.StatusCode)

	if resp.StatusCode == http.StatusNotFound {
		c.logger.Warnf("GetUser: user %s not found in Keycloak", userID)
		return nil, errors.New(errors.ErrCodeUserNotFound, fmt.Sprintf("user %s not found in Keycloak", userID))
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		c.logger.Errorf("GetUser: unexpected status code %d, response body: %s", resp.StatusCode, string(bodyBytes))
		return nil, errors.New(errors.ErrCodeKeycloakAPI, fmt.Sprintf("unexpected status code: %d", resp.StatusCode))
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		c.logger.Errorf("GetUser: failed to decode response: %v", err)
		return nil, errors.Wrap(err, errors.ErrCodeKeycloakAPI, "decode response failed")
	}

	c.logger.Debugf("GetUser: successfully fetched user %s (%s, %s)", userID, user.Username, user.Email)
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

// authenticate obtains a new access token using the configured auth method
func (c *Client) authenticate(ctx context.Context) error {
	c.logger.Debugf("authenticate: starting authentication with method %s", c.authMethod)

	tokenURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", c.baseURL, c.realm)
	c.logger.Debugf("authenticate: using token endpoint %s", tokenURL)

	data := url.Values{}

	switch c.authMethod {
	case AuthMethodClientCredentials:
		c.logger.Debugf("authenticate: using client credentials method with client_id=%s", c.clientID)
		data.Set("grant_type", "client_credentials")
		data.Set("client_id", c.clientID)
		data.Set("client_secret", c.clientSecret)
	case AuthMethodPassword:
		c.logger.Debugf("authenticate: using password method with username=%s", c.adminUsername)
		data.Set("grant_type", "password")
		data.Set("client_id", c.clientID)
		data.Set("username", c.adminUsername)
		data.Set("password", c.adminPassword)
	default:
		c.logger.Errorf("authenticate: unsupported auth method: %s", c.authMethod)
		return errors.New(errors.ErrCodeKeycloakAuth, fmt.Sprintf("unsupported auth method: %s", c.authMethod))
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		c.logger.Errorf("authenticate: failed to create request: %v", err)
		return errors.Wrap(err, errors.ErrCodeKeycloakAuth, "create auth request failed")
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Errorf("authenticate: request failed: %v", err)
		return errors.Wrap(err, errors.ErrCodeKeycloakAuth, "auth request failed")
	}
	defer resp.Body.Close()

	c.logger.Debugf("authenticate: received status code %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		c.logger.Errorf("authenticate: authentication failed with status %d, response body: %s", resp.StatusCode, string(bodyBytes))
		return errors.New(errors.ErrCodeKeycloakAuth, fmt.Sprintf("authentication failed with status: %d", resp.StatusCode))
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		c.logger.Errorf("authenticate: failed to decode token response: %v", err)
		return errors.Wrap(err, errors.ErrCodeKeycloakAuth, "decode token response failed")
	}

	c.accessToken = tokenResp.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	c.logger.Debugf("authenticate: successfully obtained token, expires at %v", c.tokenExpiry)
	return nil
}
