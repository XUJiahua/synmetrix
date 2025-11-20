# Keycloak 迁移：JIT 用户同步方案

## 执行摘要

**方案**: 🟢 **JIT (Just-In-Time) 同步**

通过分析代码中 `users` 表的所有依赖，采用 JIT 同步方案可以在避免完全用户同步的同时，保持最佳的用户体验和功能完整性。

---

## 目录

1. [用户表依赖分析](#1-用户表依赖分析)
2. [JIT 同步方案设计](#2-jit-同步方案设计)
3. [实施步骤](#3-实施步骤)
4. [总结](#4-总结)

---

## 1. 用户表依赖分析

### 1.1 数据库依赖关系

```sql
-- 核心依赖关系图
users (id, display_name, avatar_url)
  ├── auth_accounts (user_id FK, email)
  ├── members (user_id FK) → 团队成员关联
  ├── alerts (user_id FK) → 创建者记录
  ├── reports (user_id FK) → 创建者记录
  ├── sql_credentials (user_id FK) → 创建者记录
  └── request_logs (user_id FK) → 执行者记录
```

**关键统计**:
- **5 个表**通过 `user_id` 外键关联到 `users`
- **3 个字段**被频繁访问: `display_name`, `avatar_url`, `account.email`
- **6 个页面**直接展示用户信息

### 1.2 GraphQL 查询依赖

#### 高频查询 (每次页面加载)

```graphql
# 1. 当前用户查询 (所有页面)
query CurrentUser($id: uuid!) {
  users_by_pk(id: $id) {
    id
    display_name        # 🔴 必需 - 导航栏展示
    avatar_url          # 🔴 必需 - 头像展示
    account {
      email             # 🔴 必需 - 个人信息页
    }
  }
}

# 2. 团队数据查询 (团队相关页面)
query TeamData($team_id: uuid!) {
  teams_by_pk(id: $team_id) {
    members {
      user {
        id
        display_name    # 🔴 必需 - 成员列表
        avatar_url      # 🔴 必需 - 成员头像
        account { email }  # 🔴 必需 - 成员邮箱
      }
    }
  }
}

# 3. 审计日志查询 (Alerts/Reports/SQL Credentials)
query Alerts {
  alerts {
    user {
      display_name    # 🟡 重要 - 显示创建者
      avatar_url      # 🟡 重要 - 创建者头像
      account { email }  # 🟡 重要 - 创建者邮箱
    }
  }
}
```

#### 更新操作

```graphql
# 用户信息更新 (个人信息页)
mutation UpdateUserInfo(
  $user_id: uuid!
  $display_name: String
  $email: citext
) {
  update_users_by_pk(
    pk_columns: { id: $user_id }
    _set: { display_name: $display_name }  # 🔴 必需
  ) { id }

  update_auth_accounts(
    where: { user_id: { _eq: $user_id } }
    _set: { email: $email }               # 🔴 必需
  ) { affected_rows }
}
```

### 1.3 UI 组件依赖

| 组件/页面 | 依赖字段 | 调用频率 | 重要性 |
|----------|---------|---------|--------|
| `Navbar` | display_name, avatar_url | 每页 | 🔴 高 |
| `Members` 页面 | display_name, avatar_url, email | 高 | 🔴 高 |
| `PersonalInfo` 页面 | display_name, email | 中 | 🔴 高 |
| `QueryLogs` 页面 | display_name | 中 | 🟡 中 |
| `SqlApi` 页面 | display_name | 低 | 🟡 中 |
| `Avatar` 组件 | display_name, avatar_url | 每页 | 🔴 高 |

**关键发现**:
- `display_name` 和 `avatar_url` 被 **所有页面** 使用
- `email` 主要用于成员管理和个人信息编辑
- 审计日志中的用户信息可以降级显示

---

## 2. JIT 同步方案设计

### 2.1 核心思路

只在用户**首次登录**时从 Keycloak 同步到 Hasura，后续用户信息的更新直接在 Hasura 中进行，Keycloak 作为纯认证服务。

### 2.2 架构设计

```
┌─────────────────────────────────────────────────┐
│            用户注册/首次登录流程                  │
└─────────────────────────────────────────────────┘
                     │
                     ▼
              ┌────────────┐
              │ Keycloak   │
              │ 注册/登录   │
              └────────────┘
                     │
                     ▼
              ┌────────────┐
              │  生成 JWT  │
              │  + user_id │
              └────────────┘
                     │
                     ▼
              ┌────────────────────┐
              │  前端收到 JWT      │
              │  发起首次 GraphQL   │
              └────────────────────┘
                     │
                     ▼
         ┌───────────────────────────┐
         │ Hasura Action/Webhook      │
         │ "ensure_user_exists"       │
         │                            │
         │ 1. 检查 users 表           │
         │ 2. 如不存在，创建用户       │
         │ 3. 从 Keycloak 拉取信息    │
         └───────────────────────────┘
                     │
                     ▼
              ┌────────────┐
              │ 返回用户数据 │
              └────────────┘
```

### 2.3 实现细节

**1. Hasura Action: 确保用户存在**

```yaml
# hasura/actions.yaml
- name: ensure_user_exists
  definition:
    kind: synchronous
    handler: http://jit-sync-service:3000/ensure-user
  permissions:
    - role: user
```

```typescript
// jit-sync-service/src/ensure-user.ts
app.post('/ensure-user', async (req, res) => {
  const { user_id } = req.body.session_variables; // 从 JWT 获取

  // 1. 检查用户是否已存在
  const existingUser = await hasuraClient.request(`
    query CheckUser($id: uuid!) {
      users_by_pk(id: $id) { id }
    }
  `, { id: user_id });

  if (existingUser.users_by_pk) {
    // 用户已存在，直接返回
    return res.json(existingUser.users_by_pk);
  }

  // 2. 用户不存在，从 Keycloak 拉取
  const kcUser = await kcAdminClient.getUser(user_id);

  if (!kcUser) {
    return res.status(404).json({ error: 'User not found in Keycloak' });
  }

  // 3. 在 Hasura 创建用户
  const newUser = await hasuraClient.request(`
    mutation CreateUser($id: uuid!, $display_name: String!, $email: citext!) {
      insert_users_one(object: {
        id: $id,
        display_name: $display_name,
        account: {
          data: { email: $email }
        }
      }) {
        id
        display_name
        avatar_url
        account { email }
      }
    }
  `, {
    id: user_id,
    display_name: kcUser.firstName + ' ' + kcUser.lastName,
    email: kcUser.email,
  });

  // 4. 创建默认团队
  await hasuraClient.request(`
    mutation CreateDefaultTeam($user_id: uuid!) {
      insert_teams_one(object: {
        name: "Default team",
        members: {
          data: {
            user_id: $user_id,
            member_roles: { data: { team_role: owner } }
          }
        }
      }) { id }
    }
  `, { user_id });

  res.json(newUser.insert_users_one);
});
```

**2. 前端集成**

```typescript
// src/hooks/useUserData.ts (修改)
export default () => {
  const { JWTpayload } = AuthTokensStore();
  const userId = JWTpayload?.["x-hasura-user-id"];

  const [currentUserData, execQueryCurrentUser] = useCurrentUserQuery({
    variables: { id: userId },
    pause: true,
  });

  useEffect(() => {
    if (userId) {
      execQueryCurrentUser();
    }
  }, [userId]);

  // ✅ Hasura 会自动触发 ensure_user_exists Action
  // 用户体验无变化！

  return { currentUser: currentUserData };
};
```

**3. Keycloak 客户端配置**

```typescript
// jit-sync-service/src/keycloak-client.ts
import KcAdminClient from '@keycloak/keycloak-admin-client';

export class KeycloakClient {
  private client: KcAdminClient;
  private tokenExpiresAt: number = 0;

  constructor() {
    this.client = new KcAdminClient({
      baseUrl: process.env.KEYCLOAK_URL!,     // 例如: http://keycloak:8080
      realmName: process.env.KEYCLOAK_REALM!, // 例如: synmetrix
    });
  }

  // 确保 token 有效（自动刷新）
  private async ensureAuthenticated() {
    // Token 即将过期（提前 60 秒刷新）
    if (Date.now() >= this.tokenExpiresAt - 60000) {
      await this.client.auth({
        grantType: 'client_credentials',
        clientId: process.env.KEYCLOAK_CLIENT_ID!,       // 例如: hasura-sync-service
        clientSecret: process.env.KEYCLOAK_CLIENT_SECRET!, // 从 Keycloak 获取
      });

      // 更新过期时间（默认 60 秒）
      this.tokenExpiresAt = Date.now() + 60000;
    }
  }

  // 获取用户信息
  async getUser(userId: string) {
    await this.ensureAuthenticated();
    return this.client.users.findOne({ id: userId });
  }
}

// 导出单例
export const kcAdminClient = new KeycloakClient();
```

**环境变量配置**:

```bash
# .env
KEYCLOAK_URL=http://keycloak:8080
KEYCLOAK_REALM=synmetrix
KEYCLOAK_CLIENT_ID=hasura-sync-service
KEYCLOAK_CLIENT_SECRET=your-client-secret-from-keycloak
```

### 2.4 方案优势

✅ **用户体验完美**
- 首次登录自动创建，无感知
- 后续使用与原系统完全一致
- 所有 GraphQL 查询保持不变

✅ **性能优秀**
- 用户数据在 Hasura，查询速度快
- 无 N+1 问题
- 可以利用 GraphQL 的关联查询

✅ **功能无损**
- 所有现有功能保持不变
- 头像上传、个人信息编辑正常工作
- 审计日志完整

✅ **数据同步简单**
- 只在首次登录时同步（一次性）
- Keycloak 作为纯认证服务，简化架构
- 用户信息直接在 Hasura 中管理

✅ **开发工作量适中**
- 只需开发 JIT Sync Service (~300 行代码)
- 前端代码几乎不需要修改
- 可以渐进式部署

### 2.5 注意事项

⚠️ **需要 JIT Service**
- 额外的服务组件（但代码量很小，约 250 行代码）
- 需要部署和维护

⚠️ **Keycloak 不再是用户数据的唯一真实来源**
- 用户的个人信息（display_name, avatar_url）存储在 Hasura
- Keycloak 主要负责认证和授权

⚠️ **需要配置 Keycloak Service Account**
- 需要在 Keycloak 中创建专用 Client 并配置 Service Account
- 需要授予适当的权限（view-users, query-users）
- Client Secret 需要妥善保管

### 2.6 安全建议

🔒 **Service Account 最佳实践**:
- ✅ 使用 Service Account (Client Credentials) 而非 admin 用户凭证
- ✅ 遵循最小权限原则：只授予必要的角色（view-users, query-users）
- ✅ Client Secret 使用密钥管理服务（Kubernetes Secrets / AWS Secrets Manager）
- ✅ JIT Sync Service 与 Keycloak 部署在同一内网，避免公网暴露
- ✅ 启用 Token 自动刷新机制，避免 token 过期导致请求失败

---

## 3. 实施步骤

### 3.1 第 1 步: 开发 JIT Sync Service (1.5 天)

```bash
# 项目结构
jit-sync-service/
├── src/
│   ├── index.ts              # Express 服务器
│   ├── ensure-user.ts        # Hasura Action Handler
│   └── keycloak-client.ts    # Keycloak Admin Client
├── Dockerfile
├── package.json
└── tsconfig.json
```

**依赖包**:
```json
{
  "dependencies": {
    "express": "^4.18.0",
    "@keycloak/keycloak-admin-client": "^23.0.0",
    "graphql-request": "^6.1.0",
    "dotenv": "^16.0.0"
  },
  "devDependencies": {
    "@types/express": "^4.17.0",
    "@types/node": "^20.0.0",
    "typescript": "^5.0.0"
  }
}
```

### 3.2 第 2 步: 配置 Hasura Action (0.5 天)

```yaml
# hasura/metadata/actions.yaml
- name: ensure_user_exists
  definition:
    kind: synchronous
    handler: http://jit-sync-service:3000/ensure-user
    forward_client_headers: true
  permissions:
    - role: user
```

### 3.3 第 3 步: 配置 Keycloak Service Account (0.5 天)

#### 3.3.1 创建 Client

在 Keycloak Admin Console 中：

1. **导航到 Clients 页面**
   - 左侧菜单：Clients → Create client

2. **基本配置**
   ```
   Client type: OpenID Connect
   Client ID: hasura-sync-service
   Name: Hasura JIT Sync Service
   Description: Service account for JIT user synchronization
   ```

3. **Capability config**
   ```
   Client authentication: ON
   Authorization: OFF
   Authentication flow:
     ☐ Standard flow
     ☐ Direct access grants
     ☑ Service accounts roles
     ☐ OAuth 2.0 Device Authorization Grant
     ☐ OIDC CAPI Grant
   ```

4. **保存并获取 Client Secret**
   - 进入 Credentials 选项卡
   - 复制 Client Secret（用于环境变量 `KEYCLOAK_CLIENT_SECRET`）

#### 3.3.2 配置 Service Account 权限

1. **进入 Service Account Roles 选项卡**
   - 打开刚创建的 `hasura-sync-service` client
   - 点击 "Service account roles" 选项卡

2. **分配 Realm 管理权限**
   - 点击 "Assign role"
   - 选择 "Filter by realm roles" → "Filter by clients"
   - 搜索 `realm-management`
   - 勾选以下角色：
     - ☑ `view-users` - 查看用户信息
     - ☑ `query-users` - 查询用户
   - 点击 "Assign"

3. **验证权限配置**
   ```bash
   # 使用 Client Credentials 获取 token
   curl -X POST "http://localhost:8180/realms/synmetrix/protocol/openid-connect/token" \
     -H "Content-Type: application/x-www-form-urlencoded" \
     -d "grant_type=client_credentials" \
     -d "client_id=hasura-sync-service" \
     -d "client_secret=YOUR_CLIENT_SECRET"

   # 使用 token 查询用户（验证权限）
   curl -X GET "http://localhost:8180/admin/realms/synmetrix/users" \
     -H "Authorization: Bearer YOUR_ACCESS_TOKEN"
   ```

#### 3.3.3 更新环境变量

```bash
# jit-sync-service/.env
KEYCLOAK_URL=http://keycloak:8180
KEYCLOAK_REALM=synmetrix
KEYCLOAK_CLIENT_ID=hasura-sync-service
KEYCLOAK_CLIENT_SECRET=<从 Keycloak Credentials 获取>
```

### 3.4 第 4 步: 前端适配 (1 天)

```typescript
// src/hooks/useUserData.ts
// ✅ 保持不变！Hasura Action 会自动触发

// 唯一需要修改的：确保用户首次访问时查询 CurrentUser
useEffect(() => {
  if (userId && !currentUser) {
    execQueryCurrentUser(); // 会触发 ensure_user_exists
  }
}, [userId]);
```

### 3.5 第 5 步: 测试 (1 天)

- [ ] 新用户注册测试
- [ ] 首次登录自动创建测试
- [ ] Keycloak Service Account 权限验证
- [ ] 性能测试 (并发 100 用户)

### 3.6 工作量总结

| 任务 | 工作量 |
|------|--------|
| 开发 JIT Sync Service | 1.5 天 |
| 配置 Hasura Action | 0.5 天 |
| 配置 Keycloak Service Account | 0.5 天 |
| 前端适配 | 1 天 |
| 测试 | 1 天 |
| **总计** | **4.5 天** |

**对比完全同步方案**: 节省 ~15.5 天 (78% 的工作量)

---

## 4. 总结

### 4.3 下一步行动

**实施清单**:
1. [ ] 创建 JIT Sync Service 项目
2. [ ] 配置 Keycloak Service Account（创建 Client、分配权限）
3. [ ] 配置 Hasura Action
4. [ ] 单元测试和集成测试

