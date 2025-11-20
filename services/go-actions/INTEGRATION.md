# Hasura Integration Guide

本文档介绍如何将 go-actions 服务集成到 Hasura 中，实现 JIT 用户同步功能。

## RPC 架构说明

go-actions 服务采用 RPC 风格的路由架构，所有 Hasura Actions 都通过统一的 `/rpc/:method` 端点处理。

### 架构优势

1. **可扩展性**: 添加新的 action 只需注册新的 handler，无需修改路由配置
2. **一致性**: 所有 actions 遵循相同的请求/响应格式
3. **灵活性**: 支持连字符和下划线命名（`ensure-user` 和 `ensure_user` 都可用）
4. **易维护**: 类似于 Node.js actions 服务的实现模式

### 当前可用的 Actions

| Method Name | Endpoint | 描述 |
|------------|----------|------|
| `ensure_user` | `/rpc/ensure_user` | JIT 用户同步 |

### 添加新的 Action

要添加新的 action，只需在 `internal/rpc/` 目录下创建新的 handler 文件，实现 `ActionHandler` 接口，然后在 `cmd/serve.go` 中注册即可。

参考示例：`internal/rpc/ensure_user.go`

## 前置条件

1. **Keycloak Service Account 已配置**
   - Client ID: `hasura-sync-service`
   - 已授予权限: `view-users`, `query-users`
   - 已获取 Client Secret

2. **Hasura 数据库表已创建**
   - users
   - auth_accounts
   - teams
   - members
   - member_roles

3. **go-actions 服务已部署并运行**
   - 可通过 `http://go-actions:3000` 访问

## 步骤 1: 配置 Hasura Action

### 1.1 创建 Action 定义

在 Hasura Console 或通过 metadata 文件配置：

**方法 A: 使用 Hasura Console**

1. 打开 Hasura Console
2. 进入 "Actions" 标签
3. 点击 "Create"
4. 输入以下配置：

```graphql
# Action 定义
type Mutation {
  ensure_user_exists: EnsureUserOutput
}

# 输入类型（可选，当前不需要输入）
# input EnsureUserInput {}

# 输出类型
type EnsureUserOutput {
  id: uuid!
  display_name: String!
  avatar_url: String
  email: String!
}
```

**Handler 配置**:
- Handler URL: `http://go-actions:3000/rpc/ensure_user`
- Forward client headers: ✅ 启用

**方法 B: 使用 Metadata 文件**

编辑 `hasura/metadata/actions.yaml`:

```yaml
actions:
  - name: ensure_user_exists
    definition:
      kind: synchronous
      handler: http://go-actions:3000/rpc/ensure_user
      forward_client_headers: true
    permissions:
      - role: user

custom_types:
  objects:
    - name: EnsureUserOutput
      fields:
        - name: id
          type: uuid!
        - name: display_name
          type: String!
        - name: avatar_url
          type: String
        - name: email
          type: String!
```

然后应用 metadata:

```bash
hasura metadata apply
```

### 1.2 配置权限

为 `user` 角色授予执行权限：

1. 在 Action 的 "Permissions" 标签
2. 勾选 `user` 角色
3. 保存

## 步骤 2: 前端集成

### 2.1 修改用户查询

在前端代码中，确保用户首次访问时触发 Action：

```typescript
// src/graphql/queries/currentUser.graphql
query CurrentUser($id: uuid!) {
  users_by_pk(id: $id) {
    id
    display_name
    avatar_url
    account {
      email
    }
  }
}

# 或者直接调用 Action（如果用户可能不存在）
mutation EnsureUserExists {
  ensure_user_exists {
    id
    display_name
    avatar_url
    email
  }
}
```

### 2.2 Hook 使用示例

```typescript
// src/hooks/useCurrentUser.ts
import { useEffect } from 'react';
import { useCurrentUserQuery, useEnsureUserExistsMutation } from '../generated/graphql';
import { useAuthToken } from './useAuthToken';

export function useCurrentUser() {
  const { userId } = useAuthToken();

  // 首先尝试查询用户
  const [{ data, error }, refetch] = useCurrentUserQuery({
    variables: { id: userId },
    pause: !userId,
  });

  // 如果用户不存在，调用 Action 创建
  const [, ensureUser] = useEnsureUserExistsMutation();

  useEffect(() => {
    if (error && userId) {
      // 用户不存在，触发 JIT 同步
      ensureUser().then(() => {
        refetch(); // 重新查询用户数据
      });
    }
  }, [error, userId]);

  return {
    user: data?.users_by_pk,
    loading: !data && !error,
    error,
  };
}
```

## 步骤 3: 自动触发（可选）

### 方法 A: 使用 Event Trigger

如果希望在每次 GraphQL 查询时自动检查用户，可以使用 Hasura Event Trigger：

```yaml
# hasura/metadata/databases/default/tables/users.yaml
table:
  name: users
  schema: public
select_permissions:
  - role: user
    permission:
      columns:
        - id
        - display_name
        - avatar_url
      filter:
        id:
          _eq: X-Hasura-User-Id
```

### 方法 B: 使用 Webhook 拦截

在 Hasura 的 JWT 验证后添加自定义 webhook：

```yaml
# docker-compose.yml
services:
  hasura:
    environment:
      HASURA_GRAPHQL_AUTH_HOOK: http://go-actions:3000/auth-hook
```

然后在 go-actions 中添加 `/auth-hook` 端点（需要额外实现）。

## 步骤 4: 测试集成

### 4.1 测试 Action

使用 Hasura Console 的 GraphiQL：

```graphql
mutation TestEnsureUser {
  ensure_user_exists {
    id
    display_name
    email
  }
}
```

### 4.2 测试完整流程

1. **用户注册/登录 Keycloak**
   ```bash
   # 获取 JWT token
   curl -X POST "http://keycloak:8080/realms/synmetrix/protocol/openid-connect/token" \
     -d "grant_type=password" \
     -d "client_id=frontend" \
     -d "username=testuser" \
     -d "password=password"
   ```

2. **使用 JWT 调用 GraphQL**
   ```bash
   curl -X POST "http://hasura:8080/v1/graphql" \
     -H "Authorization: Bearer <JWT_TOKEN>" \
     -H "Content-Type: application/json" \
     -d '{
       "query": "mutation { ensure_user_exists { id display_name email } }"
     }'
   ```

3. **验证用户已创建**
   ```bash
   curl -X POST "http://hasura:8080/v1/graphql" \
     -H "x-hasura-admin-secret: your-secret" \
     -H "Content-Type: application/json" \
     -d '{
       "query": "query { users { id display_name account { email } } }"
     }'
   ```

## 步骤 5: 监控和日志

### 5.1 查看 go-actions 日志

```bash
# Docker Compose
docker-compose logs -f go-actions

# Kubernetes
kubectl logs -f deployment/go-actions
```

### 5.2 监控健康状态

```bash
# 健康检查
curl http://go-actions:3000/health

# 应返回: OK
```

## 故障排查

### 问题 1: Action 调用失败 (500 错误)

**原因**: go-actions 服务无法连接到 Keycloak 或 Hasura

**解决方案**:
1. 检查环境变量配置
   ```bash
   docker exec go-actions env | grep KEYCLOAK
   docker exec go-actions env | grep HASURA
   ```

2. 测试网络连通性
   ```bash
   docker exec go-actions ping keycloak
   docker exec go-actions ping hasura
   ```

### 问题 2: 用户创建失败 (GraphQL 错误)

**原因**: Hasura 数据库表结构不匹配

**解决方案**:
1. 检查数据库表是否存在
   ```sql
   SELECT table_name FROM information_schema.tables
   WHERE table_schema = 'public'
   AND table_name IN ('users', 'auth_accounts', 'teams', 'members', 'member_roles');
   ```

2. 验证外键关系
   ```sql
   SELECT * FROM information_schema.table_constraints
   WHERE table_name IN ('users', 'auth_accounts');
   ```

### 问题 3: Keycloak 认证失败

**原因**: Service Account 配置不正确

**解决方案**:
1. 验证 Client Secret
2. 检查 Service Account Roles
3. 测试手动获取 token:
   ```bash
   curl -X POST "http://keycloak:8080/realms/synmetrix/protocol/openid-connect/token" \
     -d "grant_type=client_credentials" \
     -d "client_id=hasura-sync-service" \
     -d "client_secret=your-secret"
   ```

## 高级配置

### 自定义用户创建逻辑

如果需要在用户创建时添加额外逻辑（如发送欢迎邮件），可以修改 `internal/service/user_sync.go`:

```go
// EnsureUserExists - 添加自定义逻辑
func (s *UserSyncService) EnsureUserExists(ctx context.Context, userID string) (*hasura.UserData, error) {
    // ... 现有代码 ...

    // 用户创建成功后的自定义逻辑
    if !exists {
        // 发送欢迎邮件
        go s.sendWelcomeEmail(userData.Email)

        // 记录审计日志
        s.logger.Infof("New user created: %s (%s)", userID, userData.Email)
    }

    return userData, nil
}
```

### 批量同步历史用户

如果需要一次性同步所有 Keycloak 用户到 Hasura：

```bash
# 添加新命令到 cmd/sync_all.go
go-actions sync-all --realm synmetrix
```

（需要额外实现此功能）

## 相关文档

- [主 README](./README.md)
- [Keycloak JIT 同步设计文档](./keycloak-jit-sync.md)
- [Hasura Actions 文档](https://hasura.io/docs/latest/actions/overview/)
- [Keycloak Admin API 文档](https://www.keycloak.org/docs-api/latest/rest-api/)
