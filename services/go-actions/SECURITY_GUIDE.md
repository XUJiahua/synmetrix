# Go Actions 服务权限控制策略指南

本文档详细说明 go-actions 服务在 Hasura Actions 环境中的权限控制策略和最佳实践。

## 目录

- [安全架构概览](#安全架构概览)
- [多层安全策略](#多层安全策略)
- [实施建议](#实施建议)
- [不同场景的权限策略](#不同场景的权限策略)
- [代码实现](#代码实现)
- [安全检查清单](#安全检查清单)

---

## 安全架构概览

### 当前架构的安全边界

```
┌────────────────┐
│   Frontend     │ ← 用户登录（Keycloak JWT）
└────────┬───────┘
         │ JWT Token
         ▼
┌────────────────┐
│    Hasura      │ ← 第一层安全：验证 JWT + 权限检查
└────────┬───────┘
         │ session_variables + headers
         ▼
┌────────────────┐
│  Go Actions    │ ← 第二层安全：验证请求来源？
└────────┬───────┘
         │ Admin Secret
         ▼
┌────────────────┐
│  Hasura (R/W)  │ ← 使用 Admin Secret，绕过权限
└────────────────┘
```

### 安全威胁模型

| 攻击场景 | 当前防护 | 风险等级 |
|---------|---------|---------|
| 外部攻击者直接调用 go-actions | ⚠️ 仅网络隔离 | 🔴 高 |
| 恶意内部服务伪造请求 | ❌ 无验证 | 🔴 高 |
| 伪造 session_variables | ❌ 无验证 | 🔴 高 |
| 中间人攻击（MITM） | ⚠️ 取决于 HTTPS | 🟡 中 |
| JWT 泄露后的滥用 | ✅ Hasura 验证 | 🟢 低 |
| DoS 攻击 | ❌ 无限流 | 🟡 中 |

---

## 多层安全策略

### 层级 1：网络隔离（基础）

**目标**：限制服务访问范围

**实施方式**：

```yaml
# docker-compose.yml
services:
  go-actions:
    networks:
      - internal  # 仅内部网络
    # 不暴露端口到宿主机
```

**优点**：
- ✅ 简单，无需代码修改
- ✅ 防止外部直接访问

**缺点**：
- ❌ 无法防御内部攻击
- ❌ 无法验证请求来源
- ❌ 依赖部署环境

**适用场景**：
- 开发环境
- 完全信任的内部网络

### 层级 2：Action Secret（推荐）

**目标**：验证请求来自 Hasura

**实施方式**：

```yaml
# Hasura metadata
actions:
  - name: ensure_user_exists
    definition:
      handler: http://go-actions:3000/rpc/ensure_user
      headers:
        - name: x-action-secret
          value_from_env: ACTION_SECRET
```

```go
// Go Actions middleware
func ActionSecretMiddleware(expectedSecret string) gin.HandlerFunc {
    return func(c *gin.Context) {
        actualSecret := c.GetHeader("x-action-secret")

        if actualSecret != expectedSecret {
            c.JSON(http.StatusUnauthorized, gin.H{
                "message": "Unauthorized: Invalid action secret",
                "code":    "INVALID_ACTION_SECRET",
            })
            c.Abort()
            return
        }

        c.Next()
    }
}
```

**优点**：
- ✅ 简单有效
- ✅ 防止直接调用
- ✅ 验证请求来自 Hasura

**缺点**：
- ❌ 无法防止 secret 泄露
- ❌ 所有 actions 共享同一 secret

**适用场景**：
- 生产环境（必须）
- 内部服务之间的调用

### 层级 3：Session Variables 验证（必须）

**目标**：验证用户身份和权限

**实施方式**：

```go
func ValidateSessionVariables(c *gin.Context, req *handler.HasuraActionRequest) error {
    // 1. 验证必需的 session variables
    userID := req.SessionVariables["x-hasura-user-id"]
    if userID == "" {
        return fmt.Errorf("missing x-hasura-user-id")
    }

    // 2. 验证 UUID 格式
    if _, err := uuid.Parse(userID); err != nil {
        return fmt.Errorf("invalid user id format: %w", err)
    }

    // 3. 验证角色
    role := req.SessionVariables["x-hasura-role"]
    if role == "" {
        return fmt.Errorf("missing x-hasura-role")
    }

    // 4. 存储到 context 供后续使用
    c.Set("user_id", userID)
    c.Set("user_role", role)

    return nil
}
```

**优点**：
- ✅ 验证用户身份
- ✅ 基于角色的访问控制
- ✅ 可追踪操作者

**缺点**：
- ⚠️ 依赖 Hasura 的 JWT 验证
- ⚠️ 无法防止 Hasura 被绕过的情况

**适用场景**：
- 所有 actions（必须）
- 涉及用户数据的操作

### 层级 4：JWT Token 验证（高安全）

**目标**：完全不信任 Hasura，独立验证 JWT

**实施方式**：

```go
import (
    "github.com/golang-jwt/jwt/v5"
)

type JWTClaims struct {
    jwt.RegisteredClaims
    HasuraClaims map[string]interface{} `json:"https://hasura.io/jwt/claims"`
}

func ValidateJWT(authHeader string, jwtSecret []byte) (*JWTClaims, error) {
    // 提取 token
    tokenString := strings.TrimPrefix(authHeader, "Bearer ")

    // 解析和验证
    token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
        // 验证签名算法
        if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
            return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
        }
        return jwtSecret, nil
    })

    if err != nil {
        return nil, fmt.Errorf("invalid token: %w", err)
    }

    claims, ok := token.Claims.(*JWTClaims)
    if !ok || !token.Valid {
        return nil, fmt.Errorf("invalid token claims")
    }

    return claims, nil
}

func JWTMiddleware(jwtSecret []byte) gin.HandlerFunc {
    return func(c *gin.Context) {
        authHeader := c.GetHeader("Authorization")
        if authHeader == "" {
            c.JSON(http.StatusUnauthorized, gin.H{
                "message": "Missing authorization header",
                "code":    "MISSING_AUTH",
            })
            c.Abort()
            return
        }

        claims, err := ValidateJWT(authHeader, jwtSecret)
        if err != nil {
            c.JSON(http.StatusUnauthorized, gin.H{
                "message": "Invalid JWT token",
                "code":    "INVALID_TOKEN",
            })
            c.Abort()
            return
        }

        // 存储 claims 供后续使用
        c.Set("jwt_claims", claims)
        c.Next()
    }
}
```

**优点**：
- ✅ 最高安全性
- ✅ 不依赖 Hasura 验证
- ✅ 防止 session_variables 伪造

**缺点**：
- ❌ 实现复杂
- ❌ 需要配置 JWT secret
- ❌ 性能开销（每次都验证）

**适用场景**：
- 高安全要求的操作
- 敏感数据访问
- 不完全信任 Hasura 的场景

### 层级 5：基于资源的权限控制（细粒度）

**目标**：确保用户只能访问自己的资源

**实施方式**：

```go
func CheckResourceOwnership(c *gin.Context, resourceUserID string) error {
    // 从 context 获取当前用户 ID
    currentUserID, exists := c.Get("user_id")
    if !exists {
        return fmt.Errorf("user id not found in context")
    }

    userID, ok := currentUserID.(string)
    if !ok {
        return fmt.Errorf("invalid user id type")
    }

    // 检查用户角色
    role, _ := c.Get("user_role")
    userRole, _ := role.(string)

    // 管理员可以访问所有资源
    if userRole == "admin" {
        return nil
    }

    // 普通用户只能访问自己的资源
    if userID != resourceUserID {
        return fmt.Errorf("access denied: user %s cannot access resource owned by %s",
            userID, resourceUserID)
    }

    return nil
}
```

**优点**：
- ✅ 细粒度权限控制
- ✅ 防止越权访问
- ✅ 支持不同角色

**缺点**：
- ❌ 需要为每个 action 实现
- ❌ 增加代码复杂度

**适用场景**：
- 操作用户资源的 actions
- 多租户场景

### 层级 6：Rate Limiting（防滥用）

**目标**：防止 DoS 攻击和滥用

**实施方式**：

```go
import (
    "github.com/ulule/limiter/v3"
    mgin "github.com/ulule/limiter/v3/drivers/middleware/gin"
    "github.com/ulule/limiter/v3/drivers/store/memory"
)

func RateLimitMiddleware() gin.HandlerFunc {
    // 定义速率限制：每分钟 60 个请求
    rate := limiter.Rate{
        Period: 1 * time.Minute,
        Limit:  60,
    }

    // 使用内存存储
    store := memory.NewStore()

    // 创建 limiter
    instance := limiter.New(store, rate, limiter.WithTrustForwardHeader(true))

    // 返回 Gin 中间件
    return mgin.NewMiddleware(instance)
}

// 或者基于用户 ID 的限流
func UserRateLimitMiddleware() gin.HandlerFunc {
    limiters := sync.Map{}

    return func(c *gin.Context) {
        userID, exists := c.Get("user_id")
        if !exists {
            c.Next()
            return
        }

        uid := userID.(string)

        // 获取或创建用户的 limiter
        limiterInstance, _ := limiters.LoadOrStore(uid, &rateLimiter{
            tokens: 60,
            lastRefill: time.Now(),
        })

        rl := limiterInstance.(*rateLimiter)

        if !rl.Allow() {
            c.JSON(http.StatusTooManyRequests, gin.H{
                "message": "Too many requests",
                "code":    "RATE_LIMIT_EXCEEDED",
            })
            c.Abort()
            return
        }

        c.Next()
    }
}
```

**优点**：
- ✅ 防止滥用
- ✅ 保护服务可用性
- ✅ 公平使用资源

**缺点**：
- ❌ 可能影响正常用户
- ❌ 需要分布式实现（多实例）

**适用场景**：
- 生产环境（推荐）
- 公开的 API
- 资源密集型操作

---

## 不同场景的权限策略

### 场景 1：用户自己的数据（ensure_user）

**安全要求**：
- 用户只能同步自己的账户
- 无需额外权限检查（JWT 已验证身份）

**推荐策略**：

```yaml
# Hasura metadata
- name: ensure_user_exists
  definition:
    handler: http://go-actions:3000/rpc/ensure_user
    headers:
      - name: x-action-secret
        value_from_env: ACTION_SECRET
    forward_client_headers: true
  permissions:
    - role: user  # 任何登录用户都可以调用
```

```go
// Handler 实现
func (h *EnsureUserHandler) Handle(c *gin.Context) {
    // 1. 验证请求格式
    var req handler.HasuraActionRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, ErrorResponse{Message: "invalid request"})
        return
    }

    // 2. 验证 session variables
    userID := req.SessionVariables["x-hasura-user-id"]
    if userID == "" {
        c.JSON(401, ErrorResponse{Message: "missing user id"})
        return
    }

    // 3. 只能同步自己的账户（userID 来自 JWT，不可伪造）
    userData, err := h.service.EnsureUserExists(c.Request.Context(), userID)
    // ...
}
```

**安全层级**：
- ✅ 层级 1：网络隔离
- ✅ 层级 2：Action Secret
- ✅ 层级 3：Session Variables 验证
- ✅ 层级 5：资源所有权（隐式：用户 ID 来自 JWT）

### 场景 2：管理员操作（删除用户）

**安全要求**：
- 只有管理员可以调用
- 需要验证目标用户存在
- 记录操作审计日志

**推荐策略**：

```yaml
# Hasura metadata
- name: delete_user
  definition:
    handler: http://go-actions:3000/rpc/delete_user
    headers:
      - name: x-action-secret
        value_from_env: ACTION_SECRET
    forward_client_headers: true
  permissions:
    - role: admin  # 仅管理员可以调用
```

```go
func (h *DeleteUserHandler) Handle(c *gin.Context) {
    var req DeleteUserRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, ErrorResponse{Message: "invalid request"})
        return
    }

    // 1. 验证操作者角色
    operatorRole := req.SessionVariables["x-hasura-role"]
    if operatorRole != "admin" {
        c.JSON(403, ErrorResponse{
            Message: "forbidden: admin role required",
            Code:    "INSUFFICIENT_PERMISSIONS",
        })
        return
    }

    // 2. 验证目标用户
    targetUserID := req.Input["user_id"].(string)
    if targetUserID == "" {
        c.JSON(400, ErrorResponse{Message: "missing target user id"})
        return
    }

    // 3. 防止删除自己
    operatorUserID := req.SessionVariables["x-hasura-user-id"]
    if targetUserID == operatorUserID {
        c.JSON(400, ErrorResponse{Message: "cannot delete yourself"})
        return
    }

    // 4. 执行删除
    if err := h.service.DeleteUser(c.Request.Context(), targetUserID); err != nil {
        c.JSON(500, ErrorResponse{Message: "failed to delete user"})
        return
    }

    // 5. 记录审计日志
    h.logger.Infof("User %s deleted user %s", operatorUserID, targetUserID)

    c.JSON(200, gin.H{"success": true})
}
```

**安全层级**：
- ✅ 层级 1：网络隔离
- ✅ 层级 2：Action Secret
- ✅ 层级 3：Session Variables 验证
- ✅ 层级 5：基于角色的访问控制（RBAC）
- ✅ 额外：审计日志

### 场景 3：跨用户资源访问（发送通知）

**安全要求**：
- 用户可以向团队成员发送通知
- 需要验证接收者在同一团队
- 限制发送频率

**推荐策略**：

```yaml
- name: send_notification
  definition:
    handler: http://go-actions:3000/rpc/send_notification
    headers:
      - name: x-action-secret
        value_from_env: ACTION_SECRET
    forward_client_headers: true
  permissions:
    - role: user
```

```go
func (h *SendNotificationHandler) Handle(c *gin.Context) {
    var req SendNotificationRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, ErrorResponse{Message: "invalid request"})
        return
    }

    senderID := req.SessionVariables["x-hasura-user-id"]
    receiverID := req.Input["receiver_id"].(string)

    // 1. 验证发送者和接收者在同一团队
    inSameTeam, err := h.service.CheckSameTeam(c.Request.Context(), senderID, receiverID)
    if err != nil {
        c.JSON(500, ErrorResponse{Message: "failed to verify team membership"})
        return
    }

    if !inSameTeam {
        c.JSON(403, ErrorResponse{
            Message: "forbidden: can only send notification to team members",
            Code:    "NOT_SAME_TEAM",
        })
        return
    }

    // 2. 检查发送频率限制（每分钟最多 5 条）
    if !h.rateLimiter.Allow(senderID, 5, time.Minute) {
        c.JSON(429, ErrorResponse{
            Message: "rate limit exceeded",
            Code:    "TOO_MANY_REQUESTS",
        })
        return
    }

    // 3. 发送通知
    message := req.Input["message"].(string)
    if err := h.service.SendNotification(c.Request.Context(), receiverID, message); err != nil {
        c.JSON(500, ErrorResponse{Message: "failed to send notification"})
        return
    }

    c.JSON(200, gin.H{"success": true})
}
```

**安全层级**：
- ✅ 层级 1：网络隔离
- ✅ 层级 2：Action Secret
- ✅ 层级 3：Session Variables 验证
- ✅ 层级 5：基于关系的访问控制（团队成员）
- ✅ 层级 6：Rate Limiting

### 场景 4：敏感操作（更改密码）

**安全要求**：
- 最高安全级别
- 需要验证原密码或 2FA
- 独立验证 JWT
- 记录审计日志

**推荐策略**：

```yaml
- name: change_password
  definition:
    handler: http://go-actions:3000/rpc/change_password
    headers:
      - name: x-action-secret
        value_from_env: ACTION_SECRET
    forward_client_headers: true
    timeout: 10
  permissions:
    - role: user
```

```go
func (h *ChangePasswordHandler) Handle(c *gin.Context) {
    var req ChangePasswordRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, ErrorResponse{Message: "invalid request"})
        return
    }

    // 1. 独立验证 JWT（不信任 session_variables）
    authHeader := c.GetHeader("Authorization")
    claims, err := h.jwtValidator.Validate(authHeader)
    if err != nil {
        c.JSON(401, ErrorResponse{Message: "invalid jwt token"})
        return
    }

    userID := claims.UserID

    // 2. 验证原密码
    currentPassword := req.Input["current_password"].(string)
    if !h.service.VerifyPassword(c.Request.Context(), userID, currentPassword) {
        h.logger.Warnf("Failed password verification for user %s", userID)
        c.JSON(401, ErrorResponse{Message: "incorrect current password"})
        return
    }

    // 3. 验证新密码强度
    newPassword := req.Input["new_password"].(string)
    if !h.passwordValidator.IsStrong(newPassword) {
        c.JSON(400, ErrorResponse{
            Message: "password too weak",
            Code:    "WEAK_PASSWORD",
        })
        return
    }

    // 4. 更改密码
    if err := h.service.ChangePassword(c.Request.Context(), userID, newPassword); err != nil {
        c.JSON(500, ErrorResponse{Message: "failed to change password"})
        return
    }

    // 5. 记录审计日志
    h.auditLogger.Log(AuditEvent{
        Type:      "PASSWORD_CHANGE",
        UserID:    userID,
        Timestamp: time.Now(),
        IP:        c.ClientIP(),
        UserAgent: c.GetHeader("User-Agent"),
    })

    // 6. 发送通知邮件
    h.notificationService.SendPasswordChangeEmail(userID)

    c.JSON(200, gin.H{"success": true})
}
```

**安全层级**：
- ✅ 层级 1：网络隔离
- ✅ 层级 2：Action Secret
- ✅ 层级 3：Session Variables 验证
- ✅ 层级 4：JWT Token 验证（独立）
- ✅ 层级 5：资源所有权验证
- ✅ 额外：原密码验证
- ✅ 额外：密码强度验证
- ✅ 额外：审计日志
- ✅ 额外：通知机制

---

## 实施建议

### 阶段 1：立即实施（开发环境）

**优先级：高**

1. **配置 Action Secret**
   - 添加环境变量 `ACTION_SECRET`
   - 在 Hasura metadata 中配置
   - 在 go-actions 中实现验证中间件

2. **Session Variables 验证**
   - 验证所有必需的 session variables
   - 验证 UUID 格式
   - 记录日志

**预期效果**：
- 防止直接调用 actions 服务
- 基本的用户身份验证

### 阶段 2：生产环境部署前

**优先级：高**

1. **网络隔离**
   - 确保 go-actions 不暴露到公网
   - 配置防火墙规则
   - 使用私有网络

2. **HTTPS/TLS**
   - 启用 HTTPS
   - 配置 TLS 证书
   - 强制使用安全连接

3. **监控和日志**
   - 记录所有 action 调用
   - 记录失败的认证尝试
   - 配置告警

**预期效果**：
- 生产级别的安全防护
- 可追踪和审计

### 阶段 3：高安全要求场景

**优先级：中**

1. **JWT 独立验证**
   - 为敏感操作启用 JWT 验证
   - 配置 JWT secret
   - 实现验证逻辑

2. **Rate Limiting**
   - 实现全局和用户级别的限流
   - 配置合理的阈值
   - 使用 Redis 作为分布式存储

3. **审计日志系统**
   - 记录敏感操作
   - 包含完整的上下文信息
   - 集成到日志聚合系统

**预期效果**：
- 最高级别的安全防护
- 满足合规要求

### 阶段 4：持续优化

**优先级：低**

1. **细粒度权限控制**
   - 实现基于资源的权限
   - 支持复杂的访问控制策略

2. **2FA/MFA**
   - 为敏感操作要求双因素认证

3. **安全扫描**
   - 定期进行安全审计
   - 使用自动化工具扫描漏洞

---

## 代码实现

### 完整的中间件实现

```go
// internal/middleware/security.go
package middleware

import (
    "fmt"
    "net/http"
    "os"
    "time"

    "go-actions/pkg/logger"

    "github.com/gin-gonic/gin"
    "github.com/google/uuid"
)

// SecurityMiddleware 组合所有安全中间件
func SecurityMiddleware(log *logger.Logger) gin.HandlerFunc {
    actionSecret := os.Getenv("ACTION_SECRET")

    return func(c *gin.Context) {
        // 1. 验证 Action Secret
        if actionSecret != "" {
            if c.GetHeader("x-action-secret") != actionSecret {
                log.Warnf("Invalid action secret from IP: %s", c.ClientIP())
                c.JSON(http.StatusUnauthorized, gin.H{
                    "message": "Unauthorized: Invalid action secret",
                    "code":    "INVALID_ACTION_SECRET",
                })
                c.Abort()
                return
            }
        } else {
            log.Warn("ACTION_SECRET not configured - service is vulnerable!")
        }

        // 2. 记录请求
        log.Infof("Action request: %s from IP: %s", c.Request.URL.Path, c.ClientIP())

        c.Next()
    }
}

// ValidateSessionVariables 验证 Hasura session variables
func ValidateSessionVariables(log *logger.Logger) gin.HandlerFunc {
    return func(c *gin.Context) {
        var req struct {
            SessionVariables map[string]string `json:"session_variables"`
        }

        // 尝试解析请求体
        if err := c.ShouldBindJSON(&req); err != nil {
            c.JSON(http.StatusBadRequest, gin.H{
                "message": "Invalid request body",
                "code":    "INVALID_REQUEST",
            })
            c.Abort()
            return
        }

        // 验证 user_id
        userID := req.SessionVariables["x-hasura-user-id"]
        if userID == "" {
            log.Warn("Missing x-hasura-user-id in session variables")
            c.JSON(http.StatusUnauthorized, gin.H{
                "message": "Missing user ID",
                "code":    "MISSING_USER_ID",
            })
            c.Abort()
            return
        }

        // 验证 UUID 格式
        if _, err := uuid.Parse(userID); err != nil {
            log.Warnf("Invalid user ID format: %s", userID)
            c.JSON(http.StatusBadRequest, gin.H{
                "message": "Invalid user ID format",
                "code":    "INVALID_USER_ID",
            })
            c.Abort()
            return
        }

        // 存储到 context
        c.Set("user_id", userID)
        c.Set("user_role", req.SessionVariables["x-hasura-role"])

        c.Next()
    }
}

// AuditLog 审计日志中间件
func AuditLog(log *logger.Logger) gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()

        // 处理请求
        c.Next()

        // 记录审计日志
        duration := time.Since(start)
        userID, _ := c.Get("user_id")

        log.Infof("Audit: user=%v, path=%s, method=%s, status=%d, duration=%v, ip=%s",
            userID,
            c.Request.URL.Path,
            c.Request.Method,
            c.Writer.Status(),
            duration,
            c.ClientIP(),
        )
    }
}
```

### 在 serve.go 中使用

```go
// cmd/serve.go
import (
    "go-actions/internal/middleware"
)

func runServe(cmd *cobra.Command, args []string) error {
    // ... 初始化 ...

    router := gin.New()

    // 使用安全中间件
    router.Use(middleware.SecurityMiddleware(log))
    router.Use(middleware.AuditLog(log))

    // RPC 路由
    router.POST("/rpc/:method", rpcRouter.Handle)

    // ...
}
```

---

## 安全检查清单

### 部署前检查

- [ ] **ACTION_SECRET 已配置**
  - [ ] 使用强随机字符串（至少 32 字符）
  - [ ] 在 Hasura metadata 中配置
  - [ ] 在 go-actions 中验证

- [ ] **网络隔离**
  - [ ] go-actions 不暴露到公网
  - [ ] 使用私有网络
  - [ ] 配置防火墙规则

- [ ] **HTTPS/TLS**
  - [ ] 启用 HTTPS
  - [ ] 配置有效的 TLS 证书
  - [ ] 禁用 HTTP

- [ ] **Session Variables 验证**
  - [ ] 验证所有必需的 session variables
  - [ ] 验证数据格式（UUID 等）
  - [ ] 记录验证失败

- [ ] **日志和监控**
  - [ ] 记录所有 action 调用
  - [ ] 记录认证失败
  - [ ] 配置告警

- [ ] **Hasura 权限配置**
  - [ ] 为每个 action 配置正确的角色权限
  - [ ] 测试不同角色的访问

### 敏感操作额外检查

- [ ] **JWT 独立验证**
  - [ ] 配置 JWT secret
  - [ ] 实现验证逻辑
  - [ ] 测试 token 过期

- [ ] **额外验证**
  - [ ] 原密码/2FA
  - [ ] 资源所有权
  - [ ] 业务规则验证

- [ ] **审计日志**
  - [ ] 记录操作者
  - [ ] 记录操作时间
  - [ ] 记录操作内容
  - [ ] 记录 IP 和 User-Agent

- [ ] **通知机制**
  - [ ] 敏感操作后发送通知
  - [ ] 告警异常行为

### 定期检查

- [ ] 审查访问日志
- [ ] 检查异常模式
- [ ] 更新依赖版本
- [ ] 运行安全扫描
- [ ] 审查权限配置

---

## 总结

### 最小安全配置（必须）

```yaml
✅ 网络隔离
✅ Action Secret
✅ Session Variables 验证
✅ HTTPS/TLS
✅ 基本日志记录
```

### 推荐生产配置

```yaml
✅ 最小安全配置
✅ Rate Limiting
✅ 审计日志
✅ 监控和告警
✅ 定期安全审计
```

### 高安全场景配置

```yaml
✅ 推荐生产配置
✅ JWT 独立验证
✅ 细粒度权限控制
✅ 2FA/MFA
✅ 自动安全扫描
```

### 关键原则

1. **深度防御**：多层安全策略，不依赖单一防线
2. **最小权限**：只授予必要的权限
3. **默认拒绝**：明确允许的才放行
4. **可审计性**：记录所有重要操作
5. **持续监控**：实时检测异常行为

---

## 参考资料

- [Hasura Actions Security](https://hasura.io/docs/latest/actions/action-handlers/#securing-action-handlers)
- [OWASP API Security Top 10](https://owasp.org/www-project-api-security/)
- [JWT Best Practices](https://tools.ietf.org/html/rfc8725)
- [TODO.md](TODO.md) - 项目安全建议
