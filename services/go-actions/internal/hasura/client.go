package hasura

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go-actions/internal/config"
	"go-actions/pkg/errors"
	"go-actions/pkg/logger"
	"io"
	"net/http"
	"time"
)

// Client handles communication with Hasura GraphQL API
type Client struct {
	endpoint    string
	adminSecret string
	httpClient  *http.Client
	logger      *logger.Logger
}

// NewClient creates a new Hasura client
func NewClient(cfg *config.Config) *Client {
	return NewClientWithLogger(cfg, logger.New())
}

// NewClientWithLogger creates a new Hasura client with custom logger
func NewClientWithLogger(cfg *config.Config, log *logger.Logger) *Client {
	log.Debugf("Hasura client initialized with endpoint %s", cfg.HasuraEndpoint)
	return &Client{
		endpoint:    cfg.HasuraEndpoint,
		adminSecret: cfg.HasuraAdminSecret,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: log,
	}
}

// CheckUserExists checks if a user exists in the database
func (c *Client) CheckUserExists(ctx context.Context, userID string) (bool, error) {
	var response CheckUserResponse
	if err := c.query(ctx, CheckUserExistsQuery, map[string]interface{}{
		"id": userID,
	}, &response); err != nil {
		return false, err
	}

	return response.UsersByPK != nil, nil
}

// GetUser retrieves user data
func (c *Client) GetUser(ctx context.Context, userID string) (*UserData, error) {
	var response GetUserResponse
	if err := c.query(ctx, GetUserQuery, map[string]interface{}{
		"id": userID,
	}, &response); err != nil {
		return nil, err
	}

	if response.UsersByPK == nil {
		return nil, errors.New(errors.ErrCodeUserNotFound, fmt.Sprintf("user %s not found", userID))
	}

	return response.UsersByPK, nil
}

// CreateUser creates a new user with account (in two steps)
func (c *Client) CreateUser(ctx context.Context, input CreateUserInput) (*UserData, error) {
	// Step 1: Create or update the user
	var userResponse CreateUserResponse
	if err := c.query(ctx, CreateUserMutation, map[string]interface{}{
		"id":           input.ID,
		"display_name": input.DisplayName,
	}, &userResponse); err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeUserCreateFailed, "create user mutation failed")
	}

	if userResponse.InsertUsersOne == nil {
		return nil, errors.New(errors.ErrCodeUserCreateFailed, "user creation returned null")
	}

	// Step 2: Create or update the account
	var accountResponse CreateAccountResponse
	if err := c.query(ctx, CreateAccountMutation, map[string]interface{}{
		"user_id": input.ID,
		"email":   input.Email,
	}, &accountResponse); err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeUserCreateFailed, "create account mutation failed")
	}

	if accountResponse.InsertAuthAccountsOne == nil {
		return nil, errors.New(errors.ErrCodeUserCreateFailed, "account creation returned null")
	}

	// Fetch and return the complete user data
	return c.GetUser(ctx, input.ID)
}

// CreateDefaultTeam creates a default team for a user
func (c *Client) CreateDefaultTeam(ctx context.Context, userID string) error {
	var response CreateTeamResponse
	if err := c.query(ctx, CreateDefaultTeamMutation, map[string]interface{}{
		"user_id": userID,
	}, &response); err != nil {
		return errors.Wrap(err, errors.ErrCodeHasuraAPI, "create default team failed")
	}

	if response.InsertTeamsOne == nil {
		return errors.New(errors.ErrCodeHasuraAPI, "team creation returned null")
	}

	return nil
}

// query executes a GraphQL query/mutation
func (c *Client) query(ctx context.Context, query string, variables map[string]interface{}, result interface{}) error {
	c.logger.Debugf("query: executing GraphQL query with variables: %v", variables)
	c.logger.Debugf("query: GraphQL query: %s", query)

	reqBody := GraphQLRequest{
		Query:     query,
		Variables: variables,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		c.logger.Errorf("query: failed to marshal request: %v", err)
		return errors.Wrap(err, errors.ErrCodeHasuraAPI, "marshal request failed")
	}

	c.logger.Debugf("query: sending request to %s", c.endpoint)

	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		c.logger.Errorf("query: failed to create request: %v", err)
		return errors.Wrap(err, errors.ErrCodeHasuraAPI, "create request failed")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-hasura-admin-secret", c.adminSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Errorf("query: request failed: %v", err)
		return errors.Wrap(err, errors.ErrCodeHasuraAPI, "request failed")
	}
	defer resp.Body.Close()

	c.logger.Debugf("query: received status code %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		c.logger.Errorf("query: unexpected status code %d, response body: %s", resp.StatusCode, string(bodyBytes))
		return errors.New(errors.ErrCodeHasuraAPI, fmt.Sprintf("unexpected status code: %d", resp.StatusCode))
	}

	var gqlResp GraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		c.logger.Errorf("query: failed to decode response: %v", err)
		return errors.Wrap(err, errors.ErrCodeHasuraAPI, "decode response failed")
	}

	if len(gqlResp.Errors) > 0 {
		c.logger.Errorf("query: GraphQL errors: %v", gqlResp.Errors)
		return errors.New(errors.ErrCodeHasuraAPI, fmt.Sprintf("GraphQL errors: %v", gqlResp.Errors))
	}

	c.logger.Debugf("query: GraphQL response data: %+v", gqlResp.Data)

	// Convert gqlResp.Data to result
	dataBytes, err := json.Marshal(gqlResp.Data)
	if err != nil {
		c.logger.Errorf("query: failed to marshal data: %v", err)
		return errors.Wrap(err, errors.ErrCodeHasuraAPI, "marshal data failed")
	}

	if err := json.Unmarshal(dataBytes, result); err != nil {
		c.logger.Errorf("query: failed to unmarshal data: %v", err)
		return errors.Wrap(err, errors.ErrCodeHasuraAPI, "unmarshal data failed")
	}

	c.logger.Debugf("query: successfully executed GraphQL query")
	return nil
}
