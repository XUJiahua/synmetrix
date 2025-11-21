package rpc

import (
	"net/http"

	"go-actions/internal/handler"
	"go-actions/internal/service"
	"go-actions/pkg/logger"

	"github.com/gin-gonic/gin"
)

// EnsureUserHandler handles the ensure_user action
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

// Handle implements the ActionHandler interface
// @Summary      Ensure user exists (JIT User Sync)
// @Description  Just-in-time user synchronization - creates user in Hasura if not exists, fetching data from Keycloak. Accepts user_id in input parameters or falls back to x-hasura-user-id from session variables.
// @Tags         actions
// @Accept       json
// @Produce      json
// @Param        request body handler.HasuraActionRequest true "Hasura Action Request with user_id in input"
// @Success      200 {object} handler.HasuraActionResponse "User data (id, display_name, email, avatar_url)"
// @Failure      400 {object} handler.ErrorResponse "Invalid request body"
// @Failure      401 {object} handler.ErrorResponse "Missing or invalid user ID in input or session"
// @Failure      500 {object} handler.ErrorResponse "Failed to sync user from Keycloak"
// @Router       /rpc/ensure_user [post]
func (h *EnsureUserHandler) Handle(c *gin.Context) {
	h.logger.Debugf("EnsureUserHandler.Handle: received request")

	// Parse request body
	var req handler.HasuraActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorf("EnsureUserHandler.Handle: failed to decode request: %v", err)
		h.logger.Debugf("EnsureUserHandler.Handle: request body content-type: %s", c.Request.Header.Get("Content-Type"))
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Message: "invalid request body",
			Code:    "INVALID_REQUEST",
		})
		return
	}

	h.logger.Debugf("EnsureUserHandler.Handle: request body parsed successfully")
	h.logger.Debugf("EnsureUserHandler.Handle: input data: %v", req.Input)
	h.logger.Debugf("EnsureUserHandler.Handle: session variables: %v", req.SessionVariables)

	// Extract user ID from input parameters or session variables
	userID, ok := req.Input["user_id"].(string)
	if !ok || userID == "" {
		// Fallback to session variables for backward compatibility
		userID = req.SessionVariables["x-hasura-user-id"]
		h.logger.Debugf("EnsureUserHandler.Handle: using user_id from session variables: %s", userID)
	} else {
		h.logger.Debugf("EnsureUserHandler.Handle: using user_id from input: %s", userID)
	}

	if userID == "" {
		h.logger.Warn("EnsureUserHandler.Handle: missing user_id in input or x-hasura-user-id in session variables")
		c.JSON(http.StatusUnauthorized, ErrorResponse{
			Message: "missing user id",
			Code:    "UNAUTHORIZED",
		})
		return
	}

	h.logger.Infof("EnsureUserHandler.Handle: processing ensure_user for user: %s", userID)

	// Call the user sync service
	userData, err := h.userSyncService.EnsureUserExists(c.Request.Context(), userID)
	if err != nil {
		h.logger.Errorf("EnsureUserHandler.Handle: EnsureUserExists failed for user %s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Message: "failed to sync user",
			Code:    "SYNC_FAILED",
		})
		return
	}

	// Build response
	response := handler.HasuraActionResponse{
		ID:          userData.ID,
		DisplayName: userData.DisplayName,
		AvatarURL:   userData.AvatarURL,
		Email:       userData.Account.Email,
	}

	h.logger.Debugf("EnsureUserHandler.Handle: response data: %+v", response)
	c.JSON(http.StatusOK, response)
	h.logger.Infof("EnsureUserHandler.Handle: successfully ensured user exists: %s", userID)
}
