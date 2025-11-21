package service

import (
	"context"
	"fmt"
	"go-actions/internal/hasura"
	"go-actions/internal/keycloak"
	"go-actions/pkg/logger"
	"strings"
)

// UserSyncService handles user synchronization between Keycloak and Hasura
type UserSyncService struct {
	keycloakClient *keycloak.Client
	hasuraClient   *hasura.Client
	logger         *logger.Logger
}

// NewUserSyncService creates a new user sync service
func NewUserSyncService(
	kcClient *keycloak.Client,
	hasuraClient *hasura.Client,
	logger *logger.Logger,
) *UserSyncService {
	return &UserSyncService{
		keycloakClient: kcClient,
		hasuraClient:   hasuraClient,
		logger:         logger,
	}
}

// EnsureUserExists ensures a user exists in Hasura, creating if necessary
func (s *UserSyncService) EnsureUserExists(ctx context.Context, userID string) (*hasura.UserData, error) {
	s.logger.Infof("EnsureUserExists: ensuring user exists: %s", userID)
	s.logger.Debugf("EnsureUserExists: starting user sync flow for %s", userID)

	// 1. Check if user already exists in Hasura
	s.logger.Debugf("EnsureUserExists: checking if user %s exists in Hasura", userID)
	exists, err := s.hasuraClient.CheckUserExists(ctx, userID)
	if err != nil {
		s.logger.Errorf("EnsureUserExists: failed to check if user exists: %v", err)
		return nil, fmt.Errorf("check user exists: %w", err)
	}

	s.logger.Debugf("EnsureUserExists: user %s exists in Hasura: %v", userID, exists)

	if exists {
		s.logger.Infof("EnsureUserExists: user %s already exists, fetching data", userID)
		userData, err := s.hasuraClient.GetUser(ctx, userID)
		if err != nil {
			s.logger.Errorf("EnsureUserExists: failed to get user data: %v", err)
			return nil, fmt.Errorf("get user: %w", err)
		}
		s.logger.Debugf("EnsureUserExists: fetched user data: %+v", userData)
		return userData, nil
	}

	s.logger.Infof("EnsureUserExists: user %s does not exist in Hasura, syncing from Keycloak", userID)

	// 2. Fetch user from Keycloak
	s.logger.Debugf("EnsureUserExists: fetching user %s from Keycloak", userID)
	kcUser, err := s.keycloakClient.GetUser(ctx, userID)
	if err != nil {
		s.logger.Errorf("EnsureUserExists: failed to get user from Keycloak: %v", err)
		return nil, fmt.Errorf("get keycloak user: %w", err)
	}

	s.logger.Infof("EnsureUserExists: fetched user from Keycloak: %s (%s)", kcUser.Username, kcUser.Email)
	s.logger.Debugf("EnsureUserExists: keycloak user data: %+v", kcUser)

	// 3. Create display name
	displayName := s.buildDisplayName(kcUser)
	s.logger.Debugf("EnsureUserExists: built display name: %s", displayName)

	// 4. Create user in Hasura
	s.logger.Debugf("EnsureUserExists: creating user in Hasura with display_name=%s, email=%s", displayName, kcUser.Email)
	userData, err := s.hasuraClient.CreateUser(ctx, hasura.CreateUserInput{
		ID:          userID,
		DisplayName: displayName,
		Email:       kcUser.Email,
	})
	if err != nil {
		s.logger.Errorf("EnsureUserExists: failed to create user in Hasura: %v", err)
		return nil, fmt.Errorf("create user in hasura: %w", err)
	}

	s.logger.Infof("EnsureUserExists: created user in Hasura: %s", userID)
	s.logger.Debugf("EnsureUserExists: created user data: %+v", userData)

	// 5. Create default team (asynchronously, don't block on failure)
	// go func() {
	// 	bgCtx := context.Background()
	// 	if err := s.hasuraClient.CreateDefaultTeam(bgCtx, userID); err != nil {
	// 		s.logger.Errorf("Failed to create default team for user %s: %v", userID, err)
	// 	} else {
	// 		s.logger.Infof("Created default team for user %s", userID)
	// 	}
	// }()

	return userData, nil
}

// buildDisplayName constructs a display name from Keycloak user data
func (s *UserSyncService) buildDisplayName(user *keycloak.User) string {
	// Try firstName + lastName
	if user.FirstName != "" || user.LastName != "" {
		parts := []string{}
		if user.FirstName != "" {
			parts = append(parts, user.FirstName)
		}
		if user.LastName != "" {
			parts = append(parts, user.LastName)
		}
		return strings.Join(parts, " ")
	}

	// Fall back to username
	if user.Username != "" {
		return user.Username
	}

	// Last resort: email prefix
	if user.Email != "" {
		emailParts := strings.Split(user.Email, "@")
		if len(emailParts) > 0 {
			return emailParts[0]
		}
	}

	return "Unknown User"
}
