.PHONY: build run clean test docker-build docker-run help

# Variables
BINARY_NAME=go-actions
DOCKER_IMAGE=synmetrix/go-actions
VERSION?=latest

# Build the application
build: swagger
	@echo "Building $(BINARY_NAME)..."
	@go build -o $(BINARY_NAME) .
	@echo "Build complete: ./$(BINARY_NAME)"

# Generate Swagger documentation
swagger:
	@echo "Generating Swagger documentation..."
	@swag init -g cmd/serve.go -o docs --parseDependency --parseInternal
	@echo "Swagger docs generated"

# Run the application
run: build
	@echo "Running $(BINARY_NAME)..."
	@./$(BINARY_NAME) serve

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -f $(BINARY_NAME)
	@go clean
	@echo "Clean complete"

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test -v -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...

# Lint code
lint:
	@echo "Linting code..."
	@golangci-lint run

# Tidy dependencies
tidy:
	@echo "Tidying dependencies..."
	@go mod tidy

# Build Docker image
docker-build:
	@echo "Building Docker image: $(DOCKER_IMAGE):$(VERSION)"
	@docker build -t $(DOCKER_IMAGE):$(VERSION) .
	@echo "Docker image built successfully"

# Run Docker container
docker-run:
	@echo "Running Docker container..."
	@docker run --rm -p 3000:3000 \
		--env-file .env \
		$(DOCKER_IMAGE):$(VERSION)

# Show help
help:
	@echo "Available targets:"
	@echo "  build          - Build the application"
	@echo "  run            - Build and run the application"
	@echo "  clean          - Clean build artifacts"
	@echo "  test           - Run tests"
	@echo "  test-coverage  - Run tests with coverage report"
	@echo "  fmt            - Format code"
	@echo "  lint           - Lint code"
	@echo "  tidy           - Tidy dependencies"
	@echo "  docker-build   - Build Docker image"
	@echo "  docker-run     - Run Docker container"
	@echo "  help           - Show this help message"
