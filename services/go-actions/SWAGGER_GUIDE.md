# Swagger 文档指南

本文档介绍如何为 RPC actions 添加 Swagger 文档注解。

## 架构设计

为了在保持 RPC 架构灵活性的同时，提供清晰的 Swagger 文档，我们采用了**双路由**策略：

### 1. 通用 RPC 路由（运行时）

```go
router.POST("/rpc/:method", rpcRouter.Handle)
```

- 处理所有 RPC 请求
- 支持动态方法分发
- 用于实际的 Hasura Actions 调用

### 2. 具体方法路由（Swagger 文档）

```go
router.POST("/rpc/ensure_user", ensureUserSwagger(ensureUserHandler))
```

- 为每个 action 提供独立的 Swagger 端点
- 允许详细的入参/出参文档
- 实际路由到同一个 handler

## 工作原理

```
客户端请求
    ↓
POST /rpc/ensure_user  ← 精确匹配（优先）
    OR
POST /rpc/:method      ← 通配符匹配（fallback）
    ↓
同一个 Handler.Handle()
```

Gin 路由器会**优先匹配精确路径**，如果没有精确匹配才使用参数化路径。因此：

- `POST /rpc/ensure_user` → 匹配精确路由
- `POST /rpc/any_other_method` → 匹配通用路由

## 为新 Action 添加 Swagger 文档

### 步骤 1: 创建 Handler

```go
// internal/rpc/my_action.go
package rpc

type MyActionHandler struct {
    logger *logger.Logger
}

func NewMyActionHandler(logger *logger.Logger) *MyActionHandler {
    return &MyActionHandler{logger: logger}
}

func (h *MyActionHandler) Handle(c *gin.Context) {
    // 实现逻辑...
}
```

### 步骤 2: 在 serve.go 中注册

```go
// cmd/serve.go

// 1. 注册到 RPC router（运行时）
myActionHandler := rpc.NewMyActionHandler(log)
rpcRouter.Register("my_action", myActionHandler)

// 2. 添加精确路由（Swagger 文档）
router.POST("/rpc/my_action", myActionSwagger(myActionHandler))
```

### 步骤 3: 创建 Swagger 包装函数

在 `cmd/serve.go` 中添加带有 Swagger 注解的包装函数：

```go
// myActionSwagger godoc
// @Summary      My Action 的简短描述
// @Description  My Action 的详细描述，可以多行
// @Tags         actions
// @Accept       json
// @Produce      json
// @Param        request body MyActionInput true "请求参数说明"
// @Success      200 {object} MyActionOutput "成功响应说明"
// @Failure      400 {object} handler.ErrorResponse "错误请求"
// @Failure      401 {object} handler.ErrorResponse "未授权"
// @Failure      500 {object} handler.ErrorResponse "服务器错误"
// @Router       /rpc/my_action [post]
func myActionSwagger(h *rpc.MyActionHandler) gin.HandlerFunc {
    return h.Handle
}
```

### 步骤 4: 定义输入/输出类型（如果需要）

如果需要自定义的输入/输出类型，在 `internal/handler/types.go` 中添加：

```go
// MyActionInput represents the input for my_action
type MyActionInput struct {
    Param1 string `json:"param1" example:"value1"`
    Param2 int    `json:"param2" example:"123"`
}

// MyActionOutput represents the output for my_action
type MyActionOutput struct {
    Result  string `json:"result" example:"success"`
    Message string `json:"message" example:"Operation completed"`
}
```

或者，如果使用通用的 Hasura Action 请求/响应，直接使用现有的类型：

```go
// @Param        request body handler.HasuraActionRequest true "Hasura Action Request"
// @Success      200 {object} map[string]interface{} "自定义 JSON 响应"
```

### 步骤 5: 重新生成 Swagger 文档

```bash
swag init -g cmd/serve.go -o docs
```

## Swagger 注解说明

### 基本注解

| 注解 | 说明 | 示例 |
|------|------|------|
| `@Summary` | 简短摘要（单行） | `Ensure user exists` |
| `@Description` | 详细描述（可多行） | `JIT user synchronization...` |
| `@Tags` | 分组标签 | `actions`, `users`, `admin` |
| `@Accept` | 接受的内容类型 | `json`, `xml`, `multipart/form-data` |
| `@Produce` | 返回的内容类型 | `json`, `xml`, `plain` |

### 参数注解

```go
// @Param {name} {paramType} {dataType} {required} {description}

// Path 参数
// @Param id path int true "User ID"

// Query 参数
// @Param page query int false "Page number" default(1)

// Body 参数（JSON）
// @Param request body MyActionInput true "Request body"

// Header 参数
// @Param Authorization header string true "Bearer token"
```

### 响应注解

```go
// @Success {status} {dataType} {description}
// @Failure {status} {dataType} {description}

// 成功响应
// @Success 200 {object} MyActionOutput "Success"
// @Success 201 {object} MyActionOutput "Created"

// 错误响应
// @Failure 400 {object} handler.ErrorResponse "Bad Request"
// @Failure 401 {object} handler.ErrorResponse "Unauthorized"
// @Failure 404 {object} handler.ErrorResponse "Not Found"
// @Failure 500 {object} handler.ErrorResponse "Internal Server Error"

// 数组响应
// @Success 200 {array} MyActionOutput "List of results"

// 基本类型响应
// @Success 200 {string} string "Plain text response"
// @Success 200 {integer} int "Numeric response"
```

### 路由注解

```go
// @Router {path} [{httpMethod}]

// @Router /rpc/my_action [post]
// @Router /users/{id} [get]
// @Router /users [post]
```

## 示例：完整的 Action 文档

### 示例 1: 简单 Action（无特殊输入）

```go
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
```

### 示例 2: 带自定义输入的 Action

假设我们要创建一个 `send_notification` action：

```go
// 1. 定义类型 (internal/handler/types.go)
type SendNotificationInput struct {
    UserID  string `json:"user_id" example:"550e8400-e29b-41d4-a716-446655440000"`
    Message string `json:"message" example:"Hello, world!"`
    Type    string `json:"type" example:"email" enums:"email,sms,push"`
}

type SendNotificationOutput struct {
    Success     bool   `json:"success" example:"true"`
    MessageID   string `json:"message_id" example:"msg_123456"`
    SentAt      string `json:"sent_at" example:"2025-11-20T16:30:00Z"`
}

// 2. Swagger 包装函数 (cmd/serve.go)
// sendNotificationSwagger godoc
// @Summary      Send notification to user
// @Description  Sends a notification (email, SMS, or push) to the specified user
// @Tags         actions
// @Accept       json
// @Produce      json
// @Param        request body SendNotificationInput true "Notification details"
// @Success      200 {object} SendNotificationOutput "Notification sent successfully"
// @Failure      400 {object} handler.ErrorResponse "Invalid input"
// @Failure      500 {object} handler.ErrorResponse "Failed to send notification"
// @Router       /rpc/send_notification [post]
func sendNotificationSwagger(h *rpc.SendNotificationHandler) gin.HandlerFunc {
    return h.Handle
}
```

### 示例 3: 带数组响应的 Action

```go
// listUsersSwagger godoc
// @Summary      List users in team
// @Description  Returns a list of all users in the specified team
// @Tags         actions
// @Accept       json
// @Produce      json
// @Param        request body ListUsersInput true "Team ID"
// @Success      200 {array} UserSummary "List of users"
// @Failure      400 {object} handler.ErrorResponse "Invalid team ID"
// @Failure      404 {object} handler.ErrorResponse "Team not found"
// @Router       /rpc/list_users [post]
func listUsersSwagger(h *rpc.ListUsersHandler) gin.HandlerFunc {
    return h.Handle
}
```

## 最佳实践

### 1. 使用有意义的 Tags

将相关的 actions 分组：

```go
// @Tags actions        // 所有 Hasura Actions
// @Tags users          // 用户相关
// @Tags teams          // 团队相关
// @Tags admin          // 管理员操作
// @Tags notifications  // 通知相关
```

### 2. 提供详细的描述

```go
// 不好的示例
// @Description Creates user

// 好的示例
// @Description Just-in-time user synchronization - creates user in Hasura if not exists, fetching data from Keycloak. This is called automatically when a user logs in for the first time.
```

### 3. 使用 Example 标签

在类型定义中使用 `example` 标签：

```go
type MyInput struct {
    Email string `json:"email" example:"user@example.com"`
    Age   int    `json:"age" example:"25"`
}
```

### 4. 文档化所有可能的错误

```go
// @Failure 400 {object} handler.ErrorResponse "Invalid request body"
// @Failure 401 {object} handler.ErrorResponse "Missing user ID in session"
// @Failure 403 {object} handler.ErrorResponse "User does not have permission"
// @Failure 404 {object} handler.ErrorResponse "Resource not found"
// @Failure 500 {object} handler.ErrorResponse "Internal server error"
```

### 5. 保持一致的命名

- Handler: `MyActionHandler`
- Swagger 函数: `myActionSwagger`
- 路由路径: `/rpc/my_action`
- Hasura Action: `my_action`

## 查看 Swagger 文档

### 访问 Swagger UI

启动服务后，访问：

```
http://localhost:3000/swagger/index.html
```

### Swagger JSON

原始 JSON 规范：

```
http://localhost:3000/swagger/doc.json
```

### 在 Swagger UI 中测试

1. 打开 Swagger UI
2. 找到你的 action（按 Tag 分组）
3. 点击 "Try it out"
4. 输入请求参数
5. 点击 "Execute"
6. 查看响应

## 故障排查

### Swagger 文档没有更新

```bash
# 重新生成文档
swag init -g cmd/serve.go -o docs

# 重新构建
go build
```

### 类型没有在文档中显示

确保：
1. 类型是导出的（首字母大写）
2. 字段有 `json` 标签
3. 在 Swagger 注解中正确引用了包名

```go
// 正确
// @Success 200 {object} handler.MyType

// 错误
// @Success 200 {object} MyType  // 缺少包名
```

### 路由冲突

确保精确路由在通配符路由**之前**注册：

```go
// 正确的顺序
router.POST("/rpc/ensure_user", ensureUserSwagger(handler))  // 精确路由先
router.POST("/rpc/:method", rpcRouter.Handle)                // 通配符后

// 错误的顺序（精确路由永远不会被匹配）
router.POST("/rpc/:method", rpcRouter.Handle)
router.POST("/rpc/ensure_user", ensureUserSwagger(handler))
```

### Example 不显示

确保在结构体字段上使用了 `example` 标签：

```go
type MyType struct {
    Name string `json:"name" example:"John Doe"`  // ✓ 正确
}
```

## 参考资料

- [Swaggo Documentation](https://github.com/swaggo/swag)
- [Swagger 2.0 Specification](https://swagger.io/specification/v2/)
- [Gin Web Framework](https://gin-gonic.com/)
- 项目示例: `cmd/serve.go` 中的 `ensureUserSwagger` 函数
