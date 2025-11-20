package rpc

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-actions/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// MockHandler is a mock implementation of ActionHandler for testing
type MockHandler struct {
	called bool
}

func (m *MockHandler) Handle(c *gin.Context) {
	m.called = true
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func TestNewRouter(t *testing.T) {
	log := logger.New()
	router := NewRouter(log)

	assert.NotNil(t, router)
	assert.NotNil(t, router.handlers)
	assert.NotNil(t, router.logger)
}

func TestRegister(t *testing.T) {
	log := logger.New()
	router := NewRouter(log)
	handler := &MockHandler{}

	router.Register("test_method", handler)

	assert.Contains(t, router.handlers, "test_method")
}

func TestRegisterWithHyphenatedName(t *testing.T) {
	log := logger.New()
	router := NewRouter(log)
	handler := &MockHandler{}

	// Register with hyphenated name
	router.Register("test-method", handler)

	// Should be normalized to underscore
	assert.Contains(t, router.handlers, "test_method")
}

func TestHandle_Success(t *testing.T) {
	log := logger.New()
	router := NewRouter(log)
	handler := &MockHandler{}

	router.Register("test_method", handler)

	// Create test request
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{gin.Param{Key: "method", Value: "test_method"}}

	router.Handle(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, handler.called)
}

func TestHandle_HyphenatedMethod(t *testing.T) {
	log := logger.New()
	router := NewRouter(log)
	handler := &MockHandler{}

	// Register with underscore
	router.Register("test_method", handler)

	// Call with hyphen
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{gin.Param{Key: "method", Value: "test-method"}}

	router.Handle(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, handler.called)
}

func TestHandle_MissingMethod(t *testing.T) {
	log := logger.New()
	router := NewRouter(log)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{} // No method parameter

	router.Handle(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response ErrorResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "missing method parameter", response.Message)
	assert.Equal(t, "INVALID_REQUEST", response.Code)
}

func TestHandle_MethodNotFound(t *testing.T) {
	log := logger.New()
	router := NewRouter(log)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{gin.Param{Key: "method", Value: "nonexistent_method"}}

	router.Handle(c)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var response ErrorResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "method not found", response.Message)
	assert.Equal(t, "METHOD_NOT_FOUND", response.Code)
}

func TestHandle_Integration(t *testing.T) {
	log := logger.New()
	router := NewRouter(log)

	// Register multiple handlers
	handler1 := &MockHandler{}
	handler2 := &MockHandler{}

	router.Register("method_one", handler1)
	router.Register("method-two", handler2) // hyphenated

	// Test method_one
	gin.SetMode(gin.TestMode)
	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Params = gin.Params{gin.Param{Key: "method", Value: "method_one"}}

	router.Handle(c1)

	assert.Equal(t, http.StatusOK, w1.Code)
	assert.True(t, handler1.called)
	assert.False(t, handler2.called)

	// Test method-two (hyphenated)
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Params = gin.Params{gin.Param{Key: "method", Value: "method-two"}}

	router.Handle(c2)

	assert.Equal(t, http.StatusOK, w2.Code)
	assert.True(t, handler2.called)
}

func TestErrorResponse(t *testing.T) {
	err := ErrorResponse{
		Message: "test error",
		Code:    "TEST_ERROR",
	}

	jsonBytes, marshalErr := json.Marshal(err)
	assert.NoError(t, marshalErr)

	var decoded ErrorResponse
	unmarshalErr := json.Unmarshal(jsonBytes, &decoded)
	assert.NoError(t, unmarshalErr)
	assert.Equal(t, err.Message, decoded.Message)
	assert.Equal(t, err.Code, decoded.Code)
}

func TestFullRequestFlow(t *testing.T) {
	log := logger.New()
	router := NewRouter(log)
	handler := &MockHandler{}

	router.Register("test_action", handler)

	// Create a full HTTP test
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/rpc/:method", router.Handle)

	requestBody := map[string]interface{}{
		"session_variables": map[string]string{
			"x-hasura-user-id": "test-user-id",
		},
		"input": map[string]interface{}{},
	}

	bodyBytes, _ := json.Marshal(requestBody)
	req := httptest.NewRequest(http.MethodPost, "/rpc/test_action", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, handler.called)
}
