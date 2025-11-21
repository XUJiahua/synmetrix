package rpc

import (
	"net/http"
	"strings"

	"go-actions/pkg/logger"

	"github.com/gin-gonic/gin"
)

// ActionHandler defines the interface for RPC action handlers
type ActionHandler interface {
	Handle(c *gin.Context)
}

// Router manages RPC action routes
type Router struct {
	handlers map[string]ActionHandler
	logger   *logger.Logger
}

// NewRouter creates a new RPC router
func NewRouter(log *logger.Logger) *Router {
	return &Router{
		handlers: make(map[string]ActionHandler),
		logger:   log,
	}
}

// Register registers an action handler
func (r *Router) Register(method string, handler ActionHandler) {
	// Convert hyphenated method names to snake_case for consistency
	// e.g., "ensure-user" -> "ensure_user"
	normalizedMethod := strings.ReplaceAll(method, "-", "_")
	r.handlers[normalizedMethod] = handler
	r.logger.Infof("Registered RPC handler: %s", normalizedMethod)
}

// Handle handles RPC requests
// It looks up the handler by method name and delegates to it
func (r *Router) Handle(c *gin.Context) {
	method := c.Param("method")
	r.logger.Debugf("Handle: received RPC request for method: %s", method)

	if method == "" {
		r.logger.Warn("Handle: missing method parameter in RPC request")
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Message: "missing method parameter",
			Code:    "INVALID_REQUEST",
		})
		return
	}

	// Support both hyphenated and underscore method names
	// e.g., both "ensure-user" and "ensure_user" work
	normalizedMethod := strings.ReplaceAll(method, "-", "_")
	r.logger.Debugf("Handle: normalized method name: %s -> %s", method, normalizedMethod)

	handler, exists := r.handlers[normalizedMethod]
	if !exists {
		r.logger.Warnf("Handle: RPC method not found: %s", method)
		c.JSON(http.StatusNotFound, ErrorResponse{
			Message: "method not found",
			Code:    "METHOD_NOT_FOUND",
		})
		return
	}

	r.logger.Debugf("Handle: found handler for method %s, delegating request", normalizedMethod)

	// Delegate to the specific handler
	handler.Handle(c)
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Message string `json:"message" example:"method not found"`
	Code    string `json:"code,omitempty" example:"METHOD_NOT_FOUND"`
}
