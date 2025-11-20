# TODO - Go Actions Service

## Hasura Actions Handler 规范总结

### Hasura v2 Actions Handler 核心规范

#### 1. 请求格式 (Request Format)

当 Hasura 执行一个 Action 时，会向 handler 发送一个 POST 请求：

```json
{
  "action": {
    "name": "<action-name>"
  },
  "input": {
    "arg1": "<value>",
    "arg2": "<value>"
  },
  "session_variables": {
    "x-hasura-user-id": "<session-user-id>",
    "x-hasura-role": "<session-user-role>"
  },
  "request_query": "<request-query>"
}
```

**重要细节**：
- 所有 `session_variables` 的 key 都是**小写**的
- `input` 包含了 GraphQL mutation 传入的参数
- `session_variables` 包含了当前用户的会话信息（JWT claims）

#### 2. 成功响应格式

返回一个普通的 JSON 对象，HTTP 状态码应该是 `2xx`：

```json
{
  "field1": "value1",
  "field2": "value2"
}
```

#### 3. 错误响应格式

返回错误对象，HTTP 状态码应该是 `4xx`：

```json
{
  "message": "<必填的错误消息>",
  "extensions": {
    "code": "<可选的错误代码>",
    "optionalField1": "<自定义数据>"
  }
}
```

#### 4. Hasura Actions 配置示例

```yaml
- name: check_connection
  definition:
    kind: synchronous              # 同步或 asynchronous
    handler: '{{ACTIONS_URL}}/rpc/check_connection'
    forward_client_headers: true   # 是否转发客户端 headers
    timeout: 180                   # 超时时间（秒）
  permissions:
    - role: user                   # 哪些角色可以调用
```

---

## 安全性分析

### ❌ 当前安全状况

**问题**：项目**没有配置** Hasura Action Secret 来验证请求来源。

**安全风险**：
- 任何人只要知道 Actions 服务 URL，都可以直接调用
- 可以绕过 Hasura 的权限系统
- `session_variables` 可以被伪造

### ✅ 现有的部分安全措施

1. **网络隔离**（Docker 内部网络）
   - 如果 Actions 服务只在 Docker 内部网络中暴露，外部无法直接访问

2. **Session Variables 使用**
   - 代码中会使用 `session_variables` 来获取用户信息
   - 但无法验证其真实性

3. **使用 Admin Secret 访问 Hasura**
   - Actions 服务在调用 Hasura GraphQL 时使用了 Admin Secret
   - 这确保了 Actions → Hasura 的调用是安全的
   - 但不能保护 Client/攻击者 → Actions 的调用

### 信任程度评估

| 信任程度 | 前提条件 | 当前状态 |
|---------|---------|---------|
| **完全信任** | 配置了 Action Secret + 网络隔离 | ❌ 未配置 Action Secret |
| **部分信任** | 仅网络隔离（Docker 内部网络） | ⚠️ 取决于部署方式 |
| **不能信任** | Actions 服务暴露在公网 | ⚠️ 未知部署环境 |

---

## 安全改进建议

### 🔒 方案 1：配置 Action Secret（推荐）

#### Step 1: 在环境变量中添加 secret

```bash
# .env
ACTION_SECRET=your-random-secret-string-here
```

#### Step 2: 在 Hasura metadata 中配置

```yaml
# services/hasura/metadata/actions.yaml
- name: check_connection
  definition:
    handler: '{{ACTIONS_URL}}/rpc/check_connection'
    forward_client_headers: true
    headers:
      - name: x-action-secret
        value_from_env: ACTION_SECRET  # 从环境变量读取
```

#### Step 3: 在 Go Actions 服务中验证

```go
// 在 handler 中添加中间件验证
func actionSecretMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        expectedSecret := os.Getenv("ACTION_SECRET")
        actualSecret := r.Header.Get("x-action-secret")

        if actualSecret != expectedSecret {
            http.Error(w, `{"message":"Unauthorized: Invalid action secret"}`, http.StatusUnauthorized)
            return
        }

        next.ServeHTTP(w, r)
    })
}
```

### 🔒 方案 2：JWT 验证（更严格）

如果 `forward_client_headers: true`，可以验证原始的 JWT token：

```go
import (
    "github.com/golang-jwt/jwt/v5"
)

func validateJWT(authHeader string) (*jwt.Token, error) {
    tokenString := strings.TrimPrefix(authHeader, "Bearer ")

    token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
        // 根据算法返回密钥
        if _, ok := token.Method.(*jwt.SigningMethodHMAC); ok {
            return []byte(os.Getenv("JWT_KEY")), nil
        }
        return nil, fmt.Errorf("unexpected signing method")
    })

    return token, err
}

func jwtMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        authHeader := r.Header.Get("Authorization")

        if authHeader == "" {
            http.Error(w, `{"message":"Missing authorization"}`, http.StatusUnauthorized)
            return
        }

        token, err := validateJWT(authHeader)
        if err != nil || !token.Valid {
            http.Error(w, `{"message":"Invalid token"}`, http.StatusUnauthorized)
            return
        }

        next.ServeHTTP(w, r)
    })
}
```

---

## 实施建议

### 开发环境
- 如果只在 Docker 内部网络，可以部分信任
- 建议至少配置 Action Secret 作为基础防护

### 生产环境
- **必须**配置 Action Secret 或 JWT 验证
- 不能仅依赖网络隔离
- 考虑实施 rate limiting 和请求日志

### 优先级
1. **高优先级**：配置 Action Secret（方案 1）
2. **中优先级**：实施 JWT 验证（方案 2）
3. **低优先级**：添加请求日志和监控

---

## 参考资料

- [Hasura v2 Actions Handler Documentation](https://hasura.io/docs/2.0/actions/action-handlers/)
- [Hasura Action Security Best Practices](https://hasura.io/docs/2.0/actions/action-handlers/#action-secrets)
- 现有 Node.js Actions 实现：`/services/actions/`
