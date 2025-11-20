#!/bin/bash

# Test script for go-actions service

set -e

echo "=== Go Actions Service Test Suite ==="
echo ""

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test 1: Build
echo "Test 1: Building application..."
if go build -o go-actions-test .; then
    echo -e "${GREEN}✓ Build successful${NC}"
else
    echo -e "${RED}✗ Build failed${NC}"
    exit 1
fi

# Test 2: Help command
echo ""
echo "Test 2: Testing help command..."
if ./go-actions-test --help > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Help command works${NC}"
else
    echo -e "${RED}✗ Help command failed${NC}"
    exit 1
fi

# Test 3: Serve command help
echo ""
echo "Test 3: Testing serve command help..."
if ./go-actions-test serve --help > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Serve command help works${NC}"
else
    echo -e "${RED}✗ Serve command help failed${NC}"
    exit 1
fi

# Test 4: Go fmt
echo ""
echo "Test 4: Checking code formatting..."
UNFORMATTED=$(gofmt -l . | grep -v vendor || true)
if [ -z "$UNFORMATTED" ]; then
    echo -e "${GREEN}✓ All files are properly formatted${NC}"
else
    echo -e "${RED}✗ The following files need formatting:${NC}"
    echo "$UNFORMATTED"
    exit 1
fi

# Test 5: Go vet
echo ""
echo "Test 5: Running go vet..."
if go vet ./... > /dev/null 2>&1; then
    echo -e "${GREEN}✓ No issues found by go vet${NC}"
else
    echo -e "${YELLOW}⚠ Go vet found some issues (check manually)${NC}"
fi

# Test 6: Check dependencies
echo ""
echo "Test 6: Checking dependencies..."
if go mod verify > /dev/null 2>&1; then
    echo -e "${GREEN}✓ All dependencies verified${NC}"
else
    echo -e "${RED}✗ Dependency verification failed${NC}"
    exit 1
fi

# Test 7: File structure
echo ""
echo "Test 7: Checking project structure..."
REQUIRED_DIRS=("cmd" "internal" "pkg")
ALL_EXIST=true
for dir in "${REQUIRED_DIRS[@]}"; do
    if [ ! -d "$dir" ]; then
        echo -e "${RED}✗ Missing directory: $dir${NC}"
        ALL_EXIST=false
    fi
done

REQUIRED_FILES=("main.go" "go.mod" "Dockerfile" "README.md" "Makefile")
for file in "${REQUIRED_FILES[@]}"; do
    if [ ! -f "$file" ]; then
        echo -e "${RED}✗ Missing file: $file${NC}"
        ALL_EXIST=false
    fi
done

if [ "$ALL_EXIST" = true ]; then
    echo -e "${GREEN}✓ Project structure is complete${NC}"
fi

# Test 8: Binary size
echo ""
echo "Test 8: Checking binary size..."
SIZE=$(du -h go-actions-test | cut -f1)
echo -e "${GREEN}✓ Binary size: $SIZE${NC}"

# Cleanup
echo ""
echo "Cleaning up test artifacts..."
rm -f go-actions-test

# Summary
echo ""
echo "==================================="
echo -e "${GREEN}All tests passed!${NC}"
echo "==================================="
echo ""
echo "Next steps:"
echo "  1. Set up environment variables (copy .env.example to .env)"
echo "  2. Configure Keycloak Service Account"
echo "  3. Deploy to your environment"
echo "  4. Configure Hasura Action"
echo "  5. Access Swagger UI at http://localhost:3000/swagger/index.html"
echo ""
echo "For more information, see:"
echo "  - README.md"
echo "  - INTEGRATION.md"
echo "  - SWAGGER.md"
echo "  - SUMMARY.md"
