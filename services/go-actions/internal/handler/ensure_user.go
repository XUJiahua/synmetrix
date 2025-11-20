package handler

import (
	"net/http"

	"go-actions/internal/service"
	"go-actions/pkg/logger"

	"github.com/gin-gonic/gin"
)

// EnsureUserHandler handles the ensure_user_exists action
type EnsureUserHandler struct {
	userSyncService *service.UserSyncService
	logger          *logger.Logger
}

// NewEnsureUserHandler creates a new ensure user handler
func NewEnsureUserHandler(
	userSyncService *service.UserSyncService,
	logger *logger.Logger,
) *EnsureUserHandler {
	return &EnsureUserHandler{
		userSyncService: userSyncService,
		logger:          logger,
	}
}

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
	// Parse request body
	var req HasuraActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorf("Failed to decode request: %v", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Message: "invalid request body",
			Code:    "INVALID_REQUEST",
		})
		return
	}

	// Extract user ID from session variables
	userID := req.SessionVariables["x-hasura-user-id"]
	if userID == "" {
		h.logger.Warn("Missing x-hasura-user-id in session variables")
		c.JSON(http.StatusUnauthorized, ErrorResponse{
			Message: "missing user id",
			Code:    "UNAUTHORIZED",
		})
		return
	}

	h.logger.Infof("Processing ensure_user_exists for user: %s", userID)

	// Call the user sync service
	userData, err := h.userSyncService.EnsureUserExists(c.Request.Context(), userID)
	if err != nil {
		h.logger.Errorf("EnsureUserExists failed for user %s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Message: "failed to sync user",
			Code:    "SYNC_FAILED",
		})
		return
	}

	// Build response
	response := HasuraActionResponse{
		ID:          userData.ID,
		DisplayName: userData.DisplayName,
		AvatarURL:   userData.AvatarURL,
		Email:       userData.Account.Email,
	}

	c.JSON(http.StatusOK, response)
	h.logger.Infof("Successfully ensured user exists: %s", userID)
}
