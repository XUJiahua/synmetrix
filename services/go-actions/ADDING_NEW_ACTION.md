# 添加新的 Hasura Action

本文档介绍如何在 go-actions 服务中添加新的 Hasura Action handler。

## 快速开始

添加新 action 的步骤：

1. 在 `internal/rpc/` 目录下创建新的 handler 文件
2. 实现 `ActionHandler` 接口
3. 在 `cmd/serve.go` 中注册 handler
4. 更新 Hasura metadata

## 详细步骤

### 步骤 1: 创建 Handler 文件

在 `internal/rpc/` 目录下创建新文件，例如 `internal/rpc/my_action.go`:

```go
package rpc

import (
	"net/http"

	"go-actions/internal/handler"
	"go-actions/pkg/logger"

	"github.com/gin-gonic/gin"
)

// MyActionHandler handles the my_action action
type MyActionHandler struct {
	logger *logger.Logger
	// 添加你需要的依赖（services, clients, etc.）
}

// NewMyActionHandler creates a new handler
func NewMyActionHandler(logger *logger.Logger) *MyActionHandler {
	return &MyActionHandler{
		logger: logger,
	}
}

// Handle implements the ActionHandler interface
func (h *MyActionHandler) Handle(c *gin.Context) {
	// 1. 解析 Hasura Action 请求
	var req handler.HasuraActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorf("Failed to decode request: %v", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Message: "invalid request body",
			Code:    "INVALID_REQUEST",
		})
		return
	}

	// 2. 从 session_variables 中提取用户信息
	userID := req.SessionVariables["x-hasura-user-id"]
	if userID == "" {
		h.logger.Warn("Missing x-hasura-user-id in session variables")
		c.JSON(http.StatusUnauthorized, ErrorResponse{
			Message: "missing user id",
			Code:    "UNAUTHORIZED",
		})
		return
	}

	// 3. 从 input 中提取参数（如果需要）
	// someParam, ok := req.Input["some_param"].(string)
	// if !ok {
	//     c.JSON(http.StatusBadRequest, ErrorResponse{
	//         Message: "missing required parameter: some_param",
	//         Code:    "INVALID_INPUT",
	//     })
	//     return
	// }

	h.logger.Infof("Processing my_action for user: %s", userID)

	// 4. 执行你的业务逻辑
	// result, err := h.someService.DoSomething(c.Request.Context(), userID)
	// if err != nil {
	//     h.logger.Errorf("Failed to process: %v", err)
	//     c.JSON(http.StatusInternalServerError, ErrorResponse{
	//         Message: "processing failed",
	//         Code:    "PROCESSING_FAILED",
	//     })
	//     return
	// }

	// 5. 构建并返回响应
	response := map[string]interface{}{
		"success": true,
		// 添加你的响应字段
	}

	c.JSON(http.StatusOK, response)
	h.logger.Infof("Successfully processed my_action for user: %s", userID)
}
```

### 步骤 2: 在 serve.go 中注册

编辑 `cmd/serve.go`，在 RPC router 设置部分添加注册代码：

```go
// 7. Setup RPC router and register action handlers
rpcRouter := rpc.NewRouter(log)

// Register ensure_user action
ensureUserHandler := rpc.NewEnsureUserHandler(userSyncService, log)
rpcRouter.Register("ensure_user", ensureUserHandler)

// 添加你的新 action
myActionHandler := rpc.NewMyActionHandler(log)
rpcRouter.Register("my_action", myActionHandler)

// RPC endpoint - handles all actions
router.POST("/rpc/:method", rpcRouter.Handle)

// Specific routes for Swagger documentation
router.POST("/rpc/ensure_user", ensureUserSwagger(ensureUserHandler))
router.POST("/rpc/my_action", myActionSwagger(myActionHandler))
```

### 步骤 2.5: 添加 Swagger 包装函数

在 `cmd/serve.go` 中添加带有 Swagger 注解的包装函数：

```go
// myActionSwagger godoc
// @Summary      My Action 的简短描述
// @Description  My Action 的详细描述
// @Tags         actions
// @Accept       json
// @Produce      json
// @Param        request body handler.HasuraActionRequest true "Hasura Action Request"
// @Success      200 {object} map[string]interface{} "成功响应"
// @Failure      400 {object} handler.ErrorResponse "错误请求"
// @Failure      401 {object} handler.ErrorResponse "未授权"
// @Failure      500 {object} handler.ErrorResponse "服务器错误"
// @Router       /rpc/my_action [post]
func myActionSwagger(h *rpc.MyActionHandler) gin.HandlerFunc {
    return h.Handle
}
```

**重要提示**：这个包装函数只是为了生成 Swagger 文档，实际的请求会被路由到同一个 handler。详细的 Swagger 文档编写指南请参考 [SWAGGER_GUIDE.md](SWAGGER_GUIDE.md)。

### 步骤 3: 更新 Hasura Metadata

编辑 `services/hasura/metadata/actions.yaml`（或通过 Hasura Console 添加）：

```yaml
actions:
  # ... 现有的 actions ...

  - name: my_action
    definition:
      kind: synchronous  # 或 asynchronous
      handler: http://go-actions:3000/rpc/my_action
      forward_client_headers: true
      timeout: 30  # 可选，默认 30 秒
    permissions:
      - role: user

custom_types:
  objects:
    # ... 现有的 types ...

    - name: MyActionOutput
      fields:
        - name: success
          type: Boolean!
        # 添加你的输出字段
```

如果需要输入参数，也定义 input type：

```yaml
custom_types:
  input_objects:
    - name: MyActionInput
      fields:
        - name: some_param
          type: String!

  objects:
    - name: MyActionOutput
      # ...
```

然后在 GraphQL schema 中使用：

```graphql
type Mutation {
  my_action(input: MyActionInput!): MyActionOutput
}
```

### 步骤 4: 应用 Metadata

```bash
# 如果使用 metadata 文件
hasura metadata apply

# 或者在 Hasura Console 中手动创建 Action
```

### 步骤 5: 重新生成 Swagger 文档

```bash
swag init -g cmd/serve.go -o docs
```

这会更新 Swagger 文档，使其包含你的新 action。

### 步骤 6: 测试

```bash
# 健康检查
curl http://localhost:3000/health

# 查看 Swagger UI（浏览器打开）
open http://localhost:3000/swagger/index.html

# 测试新 action（需要有效的 JWT token）
curl -X POST "http://localhost:3000/rpc/my_action" \
  -H "Content-Type: application/json" \
  -d '{
    "session_variables": {
      "x-hasura-user-id": "test-user-id"
    },
    "input": {
      "some_param": "test"
    },
    "action": {
      "name": "my_action"
    }
  }'

# 或通过 Hasura GraphQL
curl -X POST "http://hasura:8080/v1/graphql" \
  -H "Authorization: Bearer <JWT_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "mutation { my_action(input: {some_param: \"test\"}) { success } }"
  }'
```

## 最佳实践

### 1. 错误处理

始终返回清晰的错误信息：

```go
c.JSON(http.StatusBadRequest, ErrorResponse{
    Message: "descriptive error message",
    Code:    "ERROR_CODE",
})
```

### 2. 日志记录

记录关键操作和错误：

```go
h.logger.Infof("Processing action for user: %s", userID)
h.logger.Errorf("Failed to process: %v", err)
```

### 3. Session Variables 验证

总是验证必需的 session variables：

```go
userID := req.SessionVariables["x-hasura-user-id"]
if userID == "" {
    c.JSON(http.StatusUnauthorized, ErrorResponse{
        Message: "missing user id",
        Code:    "UNAUTHORIZED",
    })
    return
}
```

### 4. Input 参数验证

验证所有必需的输入参数：

```go
param, ok := req.Input["param_name"].(string)
if !ok || param == "" {
    c.JSON(http.StatusBadRequest, ErrorResponse{
        Message: "invalid or missing parameter: param_name",
        Code:    "INVALID_INPUT",
    })
    return
}
```

### 5. 依赖注入

通过构造函数注入依赖，便于测试：

```go
type MyActionHandler struct {
    service *service.MyService
    client  *client.MyClient
    logger  *logger.Logger
}

func NewMyActionHandler(
    service *service.MyService,
    client *client.MyClient,
    logger *logger.Logger,
) *MyActionHandler {
    return &MyActionHandler{
        service: service,
        client:  client,
        logger:  logger,
    }
}
```

### 6. 编写测试

为你的 handler 编写单元测试：

```go
func TestMyActionHandler_Handle(t *testing.T) {
    log := logger.New()
    handler := NewMyActionHandler(log)

    gin.SetMode(gin.TestMode)
    w := httptest.NewRecorder()
    c, _ := gin.CreateTestContext(w)

    // 设置测试请求
    reqBody := handler.HasuraActionRequest{
        SessionVariables: map[string]string{
            "x-hasura-user-id": "test-user",
        },
        Input: map[string]interface{}{
            "some_param": "test",
        },
    }

    bodyBytes, _ := json.Marshal(reqBody)
    c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(bodyBytes))
    c.Request.Header.Set("Content-Type", "application/json")

    // 执行
    handler.Handle(c)

    // 验证
    assert.Equal(t, http.StatusOK, w.Code)
}
```

## 命名约定

- **Handler 文件**: `internal/rpc/{action_name}.go` (使用下划线)
- **Handler 结构体**: `{ActionName}Handler` (使用 PascalCase)
- **注册名称**: `{action_name}` (使用下划线)
- **Hasura Action 名称**: `{action_name}` (使用下划线)
- **URL 路径**: `/rpc/{action_name}` (支持连字符和下划线)

## 参考示例

完整的示例请参考：
- `internal/rpc/ensure_user.go` - ensure_user action 实现
- `internal/rpc/router.go` - RPC router 实现
- `cmd/serve.go` - 注册和启动服务

## 故障排查

### Action 返回 404

检查：
1. Handler 是否已在 `cmd/serve.go` 中注册
2. 注册的方法名是否与 URL 中的方法名匹配
3. Hasura metadata 中的 handler URL 是否正确

### Session Variables 为空

检查：
1. Hasura Action 配置中是否启用了 `forward_client_headers: true`
2. JWT token 是否包含正确的 Hasura claims
3. 请求是否携带了 Authorization header

### 响应格式错误

确保：
1. 返回的 JSON 结构与 Hasura 中定义的输出类型匹配
2. 所有必填字段都有值
3. 字段类型正确（例如，uuid 应该是字符串格式）
