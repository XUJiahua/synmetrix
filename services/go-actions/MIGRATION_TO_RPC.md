# RPC 架构迁移总结

本文档记录了将 go-actions 服务从单一端点架构迁移到 RPC 风格架构的过程。

## 迁移概览

**日期**: 2025-11-20

**目标**: 建立可扩展的 RPC 架构，支持未来添加更多 Hasura Actions

## 架构变更

### 之前的架构

```
POST /ensure-user  -> handler.EnsureUserHandler.EnsureUser()
```

每个 action 都需要一个独立的路由端点。

### 现在的架构

```
POST /rpc/:method  -> rpc.Router.Handle() -> 具体的 ActionHandler
```

所有 actions 通过统一的 RPC 端点处理，Router 根据 method 参数分发到对应的 handler。

## 文件变更

### 新增文件

1. **`internal/rpc/router.go`**
   - RPC 路由器实现
   - 负责分发请求到具体的 handler
   - 支持方法名标准化（连字符 <-> 下划线）

2. **`internal/rpc/ensure_user.go`**
   - ensure_user action 的 handler 实现
   - 实现 `ActionHandler` 接口
   - 从 `internal/handler/ensure_user.go` 迁移而来

3. **`internal/rpc/router_test.go`**
   - RPC router 的单元测试
   - 覆盖路由注册、分发、错误处理等场景

4. **`ADDING_NEW_ACTION.md`**
   - 添加新 action 的完整指南
   - 包含代码示例和最佳实践

5. **`MIGRATION_TO_RPC.md`** (本文档)
   - 迁移总结和说明

### 修改文件

1. **`cmd/serve.go`**
   - 引入 `internal/rpc` 包
   - 创建 RPC router 并注册 handlers
   - 将路由从 `/ensure-user` 改为 `/rpc/:method`
   - 更新帮助文档

2. **`INTEGRATION.md`**
   - 更新所有 handler URL 为 RPC 格式
   - 添加 RPC 架构说明部分
   - 添加架构优势和可用 actions 列表

3. **`TODO.md`**
   - 添加 Hasura Actions 规范总结
   - 添加安全性分析和建议

### 保留文件（未来可删除）

这些文件目前保留，但在确认迁移成功后可以删除：

- `internal/handler/ensure_user.go` (已被 `internal/rpc/ensure_user.go` 替代)
- `internal/handler/types.go` 中的部分类型可以移到 `internal/rpc/`

## 技术实现

### ActionHandler 接口

```go
type ActionHandler interface {
    Handle(c *gin.Context)
}
```

所有 action handlers 必须实现此接口。

### Router 工作流程

1. 接收 `/rpc/:method` 请求
2. 提取 `method` 参数
3. 标准化方法名（连字符 -> 下划线）
4. 查找已注册的 handler
5. 委托给具体的 handler 处理

### 方法名标准化

Router 支持两种命名风格：

- URL 风格: `/rpc/ensure-user` (连字符)
- Go 风格: `/rpc/ensure_user` (下划线)

两者都会被标准化为 `ensure_user` 进行查找。

## 使用示例

### Hasura Metadata 配置

```yaml
actions:
  - name: ensure_user_exists
    definition:
      kind: synchronous
      handler: http://go-actions:3000/rpc/ensure_user
      forward_client_headers: true
    permissions:
      - role: user
```

### 直接调用 RPC 端点

```bash
curl -X POST "http://localhost:3000/rpc/ensure_user" \
  -H "Content-Type: application/json" \
  -d '{
    "session_variables": {
      "x-hasura-user-id": "test-user-id"
    },
    "input": {},
    "action": {
      "name": "ensure_user_exists"
    }
  }'
```

### 通过 Hasura GraphQL 调用

```bash
curl -X POST "http://hasura:8080/v1/graphql" \
  -H "Authorization: Bearer <JWT_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "mutation { ensure_user_exists { id display_name email } }"
  }'
```

## 添加新 Action

现在添加新的 action 非常简单：

### 1. 创建 Handler

```go
// internal/rpc/my_new_action.go
package rpc

type MyNewActionHandler struct {
    logger *logger.Logger
}

func NewMyNewActionHandler(logger *logger.Logger) *MyNewActionHandler {
    return &MyNewActionHandler{logger: logger}
}

func (h *MyNewActionHandler) Handle(c *gin.Context) {
    // 实现逻辑
}
```

### 2. 注册 Handler

```go
// cmd/serve.go
myNewActionHandler := rpc.NewMyNewActionHandler(log)
rpcRouter.Register("my_new_action", myNewActionHandler)
```

### 3. 配置 Hasura

```yaml
# services/hasura/metadata/actions.yaml
- name: my_new_action
  definition:
    handler: http://go-actions:3000/rpc/my_new_action
    forward_client_headers: true
  permissions:
    - role: user
```

完整指南见 `ADDING_NEW_ACTION.md`。

## 测试结果

所有测试通过：

```bash
$ go test ./...
ok      go-actions/internal/config      0.002s
ok      go-actions/internal/keycloak    0.018s
ok      go-actions/internal/rpc         0.006s
```

构建成功：

```bash
$ go build -v ./...
go-actions
```

Swagger 文档已更新：

```bash
$ swag init -g cmd/serve.go -o docs
2025/11/20 16:21:22 Generate swagger docs....
2025/11/20 16:21:22 create docs.go at docs/docs.go
2025/11/20 16:21:22 create swagger.json at docs/swagger.json
2025/11/20 16:21:22 create swagger.yaml at docs/swagger.yaml
```

## 向后兼容性

### ⚠️ 重要：URL 变更

**旧端点**: `POST /ensure-user`
**新端点**: `POST /rpc/ensure_user`

需要更新：

1. ✅ Hasura metadata (`services/hasura/metadata/actions.yaml`)
2. ✅ 集成文档 (`INTEGRATION.md`)
3. ⚠️ 任何直接调用旧端点的客户端代码

### 迁移清单

- [x] 创建 RPC router
- [x] 迁移 ensure_user handler
- [x] 更新 serve.go 路由配置
- [x] 编写单元测试
- [x] 更新文档
- [x] 重新生成 Swagger 文档
- [ ] 更新 Hasura metadata (需要在实际环境中执行)
- [ ] 验证前端集成 (需要在实际环境中测试)

## 优势

1. **可扩展性**: 添加新 action 只需 3 步（创建、注册、配置）
2. **一致性**: 所有 actions 遵循相同的接口和模式
3. **灵活性**: 支持多种命名风格
4. **易维护**: 清晰的文件组织结构
5. **易测试**: 每个组件都有清晰的接口

## 与 Node.js Actions 服务对比

### 相似之处

- 都使用 RPC 风格的路由 (`/rpc/:method`)
- 都支持动态路由分发
- 都遵循 Hasura Actions 规范

### 差异

| 特性 | Node.js (actions) | Go (go-actions) |
|------|------------------|-----------------|
| 语言 | JavaScript | Go |
| 框架 | Express.js | Gin |
| Handler 加载 | 动态 import | 静态注册 |
| 命名转换 | `hyphensToCamelCase` | `hyphen-to_underscore` |
| 类型安全 | 运行时 | 编译时 |

## 后续计划

1. **迁移更多 actions**
   - 参考 Node.js actions 服务的 `/rpc` 目录
   - 按需迁移常用的 actions

2. **增强中间件**
   - 添加 Action Secret 验证（见 `TODO.md`）
   - 添加 JWT 验证
   - 添加 rate limiting

3. **监控和日志**
   - 添加请求追踪
   - 添加性能指标

4. **文档完善**
   - 添加更多代码示例
   - 添加故障排查指南

## 参考资料

- [Hasura Actions Documentation](https://hasura.io/docs/2.0/actions/)
- [Node.js Actions Service](../../actions/)
- [Go Gin Framework](https://gin-gonic.com/)
- 项目文档：
  - `INTEGRATION.md` - Hasura 集成指南
  - `ADDING_NEW_ACTION.md` - 添加新 action 指南
  - `TODO.md` - Hasura Actions 规范和安全建议

## 问题反馈

如有问题，请参考：
- `ADDING_NEW_ACTION.md` 的故障排查部分
- 项目 issue tracker
- 团队内部文档
