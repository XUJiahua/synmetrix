package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-actions/internal/config"
	"go-actions/internal/handler"
	"go-actions/internal/hasura"
	"go-actions/internal/keycloak"
	"go-actions/internal/rpc"
	"go-actions/internal/service"
	"go-actions/pkg/logger"

	_ "go-actions/docs" // Import generated docs

	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the JIT user sync HTTP server",
	Long: `Starts an HTTP server that handles Hasura Actions for just-in-time
user synchronization between Keycloak and Hasura.

The server exposes the following endpoints:
  - POST /rpc/:method: RPC-style action handlers (e.g., /rpc/ensure_user)
  - GET  /health: Health check endpoint
  - GET  /swagger/*: Swagger API documentation`,
	RunE: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

// @title           Go Actions - Keycloak JIT User Sync API
// @version         1.0.0
// @description     API for just-in-time user synchronization between Keycloak and Hasura
// @contact.name    Synmetrix Team
// @contact.url     https://github.com/synmetrix/synmetrix
// @host            localhost:3000
// @BasePath        /
func runServe(cmd *cobra.Command, args []string) error {
	// 1. Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// 2. Initialize logger
	log := logger.New()
	log.Info("Starting go-actions server")

	// 3. Set Gin mode (release in production)
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 4. Initialize clients
	kcClient := keycloak.NewClient(cfg)
	log.Info("Keycloak client initialized")

	hasuraClient := hasura.NewClient(cfg)
	log.Info("Hasura client initialized")

	// 5. Initialize service
	userSyncService := service.NewUserSyncService(kcClient, hasuraClient, log)
	log.Info("User sync service initialized")

	// 6. Setup Gin router
	router := gin.New()

	// Use custom logger middleware
	router.Use(ginLogger(log))
	router.Use(gin.Recovery())

	// 7. Setup RPC router and register action handlers
	rpcRouter := rpc.NewRouter(log)

	// Register ensure_user action
	ensureUserHandler := rpc.NewEnsureUserHandler(userSyncService, log)
	rpcRouter.Register("ensure_user", ensureUserHandler)

	// RPC endpoint - handles all actions via /rpc/:method
	router.POST("/rpc/:method", rpcRouter.Handle)
	log.Info("Registered POST /rpc/:method endpoint")

	// Additional explicit routes for Swagger documentation
	// These are the same handlers but with specific paths for better Swagger docs
	router.POST("/rpc/ensure_user", ensureUserSwagger(ensureUserHandler))
	log.Info("Registered POST /rpc/ensure_user endpoint (Swagger)")

	// Health check endpoint
	router.GET("/health", healthCheck)
	log.Info("Registered GET /health endpoint")

	// Swagger documentation
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	log.Info("Registered GET /swagger/* endpoint")

	// 8. Create HTTP server
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 9. Start server in a goroutine
	serverErrors := make(chan error, 1)
	go func() {
		log.Infof("Server listening on %s", addr)
		log.Infof("Swagger UI available at http://localhost:%s/swagger/index.html", cfg.ServerPort)
		serverErrors <- server.ListenAndServe()
	}()

	// 10. Setup graceful shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// 11. Wait for shutdown signal or server error
	select {
	case err := <-serverErrors:
		return fmt.Errorf("server error: %w", err)

	case sig := <-shutdown:
		log.Infof("Received signal %v, starting graceful shutdown", sig)

		// Give outstanding requests 5 seconds to complete
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Errorf("Graceful shutdown failed: %v", err)
			if err := server.Close(); err != nil {
				return fmt.Errorf("force close server: %w", err)
			}
		}

		log.Info("Server stopped gracefully")
	}

	return nil
}

// ginLogger creates a Gin logger middleware using our custom logger
func ginLogger(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		clientIP := c.ClientIP()
		method := c.Request.Method
		statusCode := c.Writer.Status()

		if raw != "" {
			path = path + "?" + raw
		}

		log.Infof("%s %s %d %v %s",
			method,
			path,
			statusCode,
			latency,
			clientIP,
		)
	}
}

// ensureUserSwagger godoc
// @Summary      Ensure user exists (JIT User Sync)
// @Description  Just-in-time user synchronization - creates user in Hasura if not exists, fetching data from Keycloak
// @Tags         actions
// @Accept       json
// @Produce      json
// @Param        request body handler.HasuraActionRequest true "Hasura Action Request with session_variables"
// @Success      200 {object} handler.HasuraActionResponse "User data (id, display_name, email, avatar_url)"
// @Failure      400 {object} handler.ErrorResponse "Invalid request body"
// @Failure      401 {object} handler.ErrorResponse "Missing or invalid user ID in session"
// @Failure      500 {object} handler.ErrorResponse "Failed to sync user from Keycloak"
// @Router       /rpc/ensure_user [post]
func ensureUserSwagger(h *rpc.EnsureUserHandler) gin.HandlerFunc {
	return h.Handle
}

// healthCheck godoc
// @Summary      Health check
// @Description  Check if the service is running
// @Tags         health
// @Produce      json
// @Success      200 {object} handler.HealthResponse
// @Router       /health [get]
func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, handler.HealthResponse{
		Status:  "ok",
		Version: "1.0.0",
		Service: "go-actions",
	})
}
