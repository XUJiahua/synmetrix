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
	s.logger.Infof("Ensuring user exists: %s", userID)

	// 1. Check if user already exists in Hasura
	exists, err := s.hasuraClient.CheckUserExists(ctx, userID)
	if err != nil {
		s.logger.Errorf("Failed to check if user exists: %v", err)
		return nil, fmt.Errorf("check user exists: %w", err)
	}

	if exists {
		s.logger.Infof("User %s already exists, fetching data", userID)
		userData, err := s.hasuraClient.GetUser(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("get user: %w", err)
		}
		return userData, nil
	}

	s.logger.Infof("User %s does not exist, syncing from Keycloak", userID)

	// 2. Fetch user from Keycloak
	kcUser, err := s.keycloakClient.GetUser(ctx, userID)
	if err != nil {
		s.logger.Errorf("Failed to get user from Keycloak: %v", err)
		return nil, fmt.Errorf("get keycloak user: %w", err)
	}

	s.logger.Infof("Fetched user from Keycloak: %s (%s)", kcUser.Username, kcUser.Email)

	// 3. Create display name
	displayName := s.buildDisplayName(kcUser)

	// 4. Create user in Hasura
	userData, err := s.hasuraClient.CreateUser(ctx, hasura.CreateUserInput{
		ID:          userID,
		DisplayName: displayName,
		Email:       kcUser.Email,
	})
	if err != nil {
		s.logger.Errorf("Failed to create user in Hasura: %v", err)
		return nil, fmt.Errorf("create user in hasura: %w", err)
	}

	s.logger.Infof("Created user in Hasura: %s", userID)

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
