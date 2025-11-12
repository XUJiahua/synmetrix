# 移除 public.users 表的可行性分析

## 问题

**核心问题**: 既然 `user_id` 可以从 JWT 的 `x-hasura-user-id` claim 中获取，是否还需要 `public.users` 表？

**简短回答**:
- ✅ **理论上可以移除**，但需要重构大量代码
- ⚠️ **实际上不推荐**，因为 `users` 表在当前架构中扮演着关键的**关联查询枢纽**角色

---

## 1. public.users 表的实际作用分析

### 1.1 作用 1: GraphQL 关联查询枢纽

**核心查询** (`services/cubejs/src/utils/dataSourceHelpers.js:59-72`):

```graphql
query ($_or: [users_bool_exp!]) {
  users(where: {_or: $_or}, limit: 1) {
    datasources {              # 👈 通过 users 表关联
      id, name, db_type
      branches {
        versions {
          dataschemas { ... }
        }
      }
    }
    members {                  # 👈 通过 users 表关联
      team_id
      member_roles {
        team_role
        access_list { config }
      }
    }
  }
}
```

**依赖的 Hasura 关联关系** (`services/hasura/metadata/tables.yaml:1457-1539`):

```yaml
- table:
    name: users
    schema: public
  array_relationships:
    - name: datasources      # users.id ← datasources.user_id
    - name: members          # users.id ← members.user_id
    - name: alerts
    - name: branches
    - name: dataschemas
    - name: reports
    - name: sql_credentials
    - name: teams
    - name: versions
```

**为什么需要这个查询？**

在 `checkAuth` 流程中 (`services/cubejs/src/utils/checkAuth.js:64-78`):

```javascript
const user = await findUser({ userId });

// 返回:
// {
//   dataSources: [...],  // 用户可访问的所有数据源（直接拥有 + 团队共享）
//   members: [...]       // 用户的团队成员关系和权限
// }

const userScope = defineUserScope(
  user.dataSources,    // 用于查找当前请求的数据源
  user.members,        // 用于构建权限访问列表
  dataSourceId,
  branchId,
  branchVersionId
);
```

**核心逻辑**:
- 通过 `users` 表一次查询，获取用户所有可访问的数据源（包括团队共享的）
- 如果没有 `users` 表，需要**手动 JOIN** 多个表才能实现相同功能

---

### 1.2 作用 2: 外键数据完整性

**13 个表引用 `public.users.id`**:

```sql
-- 示例 1: datasources 表
CREATE TABLE datasources (
  user_id uuid NOT NULL,
  FOREIGN KEY (user_id) REFERENCES public.users(id)
    ON DELETE CASCADE  -- 删除用户时级联删除数据源
);

-- 示例 2: members 表
CREATE TABLE members (
  user_id uuid NOT NULL,
  FOREIGN KEY (user_id) REFERENCES public.users(id)
    ON DELETE CASCADE  -- 删除用户时级联删除成员关系
);
```

**外键的作用**:
1. **数据完整性**: 防止插入不存在的 `user_id`
2. **级联删除**: 删除用户时自动清理所有关联数据
3. **数据库约束**: 在数据库层面保证数据一致性

**如果移除 users 表**:
- ❌ 失去外键约束，需要在应用层手动保证数据完整性
- ❌ 失去级联删除功能，需要手动删除所有关联数据
- ❌ 可能出现"孤儿数据"（user_id 指向不存在的用户）

---

### 1.3 作用 3: 用户元数据存储

**当前字段**:
```sql
CREATE TABLE public.users (
    id uuid PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    display_name text,        -- 用户显示名称
    avatar_url text           -- 用户头像 URL
);
```

**问题**: 如果使用 Keycloak，`display_name` 和 `avatar_url` 存在哪里？

**选项**:
1. 存储在 Keycloak User Attributes
2. 每次从 Keycloak API 实时获取
3. 存储在其他表（如新建 `user_profiles` 表）

---

### 1.4 作用 4: 团队协作的权限查询

**复杂的权限逻辑** (`services/cubejs/src/utils/dataSourceHelpers.js:128-141`):

```javascript
const where = {
  _or: [
    { id: { _eq: userId } },           // 查找用户自己的数据源
    {
      members: {                        // 或查找团队共享的数据源
        team: {
          members: {
            user_id: { _eq: userId },   // 通过 members 表间接关联
          },
        },
      },
    },
  ],
};
```

**Hasura 自动生成的 GraphQL 查询路径**:
```
users (user_id = X)
  → datasources (直接拥有)
  → members
      → team
          → members (同事)
              → user
                  → datasources (团队共享的数据源)
```

**如果没有 users 表**，这个复杂的关联查询需要：
1. 先查 `members` 表获取 `team_id`
2. 再查 `datasources` 表 WHERE `team_id IN (...)`
3. 手动合并两次查询结果

---

## 2. 移除 users 表的可行方案

### 方案 1: 完全移除 + 重构查询逻辑 ❌

**变更内容**:

1. **修改所有表的 user_id 字段**

```sql
-- 旧设计
CREATE TABLE datasources (
  user_id uuid NOT NULL,
  FOREIGN KEY (user_id) REFERENCES public.users(id)
);

-- 新设计 (无外键)
CREATE TABLE datasources (
  user_id uuid NOT NULL  -- 来自 Keycloak 的 user UUID
);
```

2. **重构 findUser 函数**

```javascript
// 旧代码: 一次 GraphQL 查询
const user = await findUser({ userId });
// 返回: { dataSources: [...], members: [...] }

// 新代码: 多次独立查询
const findUserResources = async ({ userId }) => {
  // 1. 查找用户直接拥有的数据源
  const ownedDataSources = await fetchGraphQL(`
    query {
      datasources(where: {user_id: {_eq: "${userId}"}}) {
        id, name, db_type, branches { ... }
      }
    }
  `);

  // 2. 查找用户所在的团队
  const userTeams = await fetchGraphQL(`
    query {
      members(where: {user_id: {_eq: "${userId}"}}) {
        team_id
        member_roles { team_role, access_list }
      }
    }
  `);

  // 3. 查找团队共享的数据源
  const teamIds = userTeams.data.members.map(m => m.team_id);
  const sharedDataSources = await fetchGraphQL(`
    query {
      datasources(where: {team_id: {_in: ${JSON.stringify(teamIds)}}}) {
        id, name, db_type, branches { ... }
      }
    }
  `);

  // 4. 手动合并结果
  return {
    dataSources: [...ownedDataSources.data.datasources, ...sharedDataSources.data.datasources],
    members: userTeams.data.members
  };
};
```

**问题**:
- ❌ 性能下降：从 1 次查询变为 3 次查询
- ❌ N+1 查询问题：如果有多个团队，查询次数会更多
- ❌ 代码复杂度增加
- ❌ 失去 Hasura 的关联查询优化

**评估**: ⭐☆☆☆☆ (不推荐)

---

### 方案 2: 保留精简版 users 表（推荐）⭐⭐⭐⭐⭐

**核心思想**:
- 保留 `users` 表作为**虚拟用户注册表**
- 只存储必要的关联关系字段
- 用户数据从 Keycloak 同步

**精简的表结构**:

```sql
CREATE TABLE public.users (
    id uuid PRIMARY KEY,                    -- Synmetrix 内部 UUID
    keycloak_id uuid UNIQUE NOT NULL,       -- Keycloak 用户 UUID
    created_at timestamptz DEFAULT now(),
    updated_at timestamptz DEFAULT now()
    -- 移除: display_name, avatar_url (从 Keycloak 获取)
);
```

**或者更激进的精简**:

```sql
-- 只保留 ID，其他信息都从 Keycloak 获取
CREATE TABLE public.users (
    id uuid PRIMARY KEY,                    -- 对应 Keycloak User UUID
    synced_at timestamptz DEFAULT now()     -- 最后同步时间
);
```

**优势**:
- ✅ 保留所有外键约束和数据完整性
- ✅ 保留 Hasura 的关联查询能力
- ✅ 保留现有代码逻辑，改动最小
- ✅ 用户元数据从 Keycloak 动态获取（单一数据源）

**实现细节**:

1. **用户同步时创建记录**

```javascript
// Keycloak Event Listener 触发
app.post('/sync-user', async (req, res) => {
  const { keycloak_id } = req.body;

  // 在 Synmetrix 创建占位记录
  await fetchGraphQL(`
    mutation {
      insert_users_one(
        object: { id: "${keycloak_id}" },
        on_conflict: {
          constraint: users_pkey,
          update_columns: [synced_at]
        }
      ) {
        id
      }
    }
  `);
});
```

2. **获取用户元数据时从 Keycloak 查询**

```javascript
// 新增工具函数
const getUserProfile = async (userId) => {
  const kcUser = await kcAdminClient.users.findOne({ id: userId });

  return {
    id: userId,
    display_name: `${kcUser.firstName} ${kcUser.lastName}`.trim() || kcUser.username,
    avatar_url: kcUser.attributes?.avatar_url?.[0] || null,
    email: kcUser.email
  };
};

// 在 GraphQL 查询后附加用户信息
const user = await findUser({ userId });
const profile = await getUserProfile(userId);

return {
  ...user,
  profile  // { display_name, avatar_url, email }
};
```

3. **或使用 Hasura Remote Schema** (更优雅)

```yaml
# Hasura metadata
remote_schemas:
  - name: keycloak
    definition:
      url: http://keycloak-graphql-adapter:4000/graphql
      forward_client_headers: true

# 关联到 users 表
- table:
    name: users
    schema: public
  remote_relationships:
    - name: keycloak_profile
      definition:
        remote_schema: keycloak
        hasura_fields: [id]
        remote_field:
          user:
            arguments:
              id: $id
```

**GraphQL 查询**:

```graphql
query {
  users(where: {id: {_eq: "user-uuid"}}) {
    id
    datasources { ... }
    members { ... }
    keycloak_profile {      # 从 Keycloak 远程 schema 获取
      display_name
      avatar_url
      email
    }
  }
}
```

**评估**: ⭐⭐⭐⭐⭐ (强烈推荐)

---

## 3. 性能对比

| 方案 | 查询次数 | 响应时间 | 数据一致性 | 代码复杂度 |
|------|----------|----------|------------|------------|
| **当前设计 (保留 users 表)** | 1 次 GraphQL | ~50ms | ✅ 强 (外键) | 低 |
| **方案 1: 完全移除** | 3+ 次 GraphQL | ~150ms | ⚠️ 应用层保证 | 高 |
| **方案 2: 精简 users 表** | 1 次 GraphQL + 1 次 Keycloak API | ~80ms | ✅ 强 (外键) | 中 |
| **方案 3: Computed Fields** | 1 次 GraphQL + N 次 Keycloak API | ~200ms | ✅ 强 (外键) | 高 |
| **方案 4: 视图** | 1 次查询 (UNION) | ~100ms | ❌ 无外键 | 中 |

---

## 4. 最终推荐

### 推荐方案: **保留精简版 users 表 (方案 2)**

**理由**:
1. ✅ **最小改动**: 保留现有架构和代码逻辑
2. ✅ **数据完整性**: 保留所有外键约束
3. ✅ **性能最优**: 保留 Hasura 的关联查询能力
4. ✅ **单一数据源**: 用户元数据从 Keycloak 获取，避免数据不一致
5. ✅ **灵活性**: 未来可以轻松添加字段或缓存

**实施步骤**:

#### 步骤 1: 精简 users 表

```sql
-- 新增迁移文件
-- services/hasura/migrations/YYYYMMDD_simplify_users_table.sql

-- 删除不再需要的字段（可选）
ALTER TABLE public.users DROP COLUMN IF EXISTS display_name;
ALTER TABLE public.users DROP COLUMN IF EXISTS avatar_url;

-- 或保留字段但标记为废弃，从 Keycloak 获取最新数据
COMMENT ON COLUMN public.users.display_name IS 'DEPRECATED: Fetch from Keycloak';
COMMENT ON COLUMN public.users.avatar_url IS 'DEPRECATED: Fetch from Keycloak';
```

#### 步骤 2: 添加 Keycloak 同步

```javascript
// services/keycloak-sync/index.js
app.post('/sync-user', async (req, res) => {
  const { keycloak_id, action } = req.body;

  switch (action) {
    case 'CREATE':
      // 在 Synmetrix 创建用户记录
      await fetchGraphQL(`
        mutation {
          insert_users_one(
            object: { id: "${keycloak_id}" },
            on_conflict: {
              constraint: users_pkey,
              update_columns: []
            }
          ) {
            id
          }
        }
      `);
      break;

    case 'DELETE':
      // 删除用户记录（会级联删除所有关联数据）
      await fetchGraphQL(`
        mutation {
          delete_users_by_pk(id: "${keycloak_id}") {
            id
          }
        }
      `);
      break;
  }

  res.json({ success: true });
});
```

#### 步骤 3: 获取用户元数据

**选项 A: 按需从 Keycloak 获取** (推荐用于低频访问)

```javascript
// services/cubejs/src/utils/keycloakHelper.js
import KeycloakAdminClient from '@keycloak/keycloak-admin-client';

const kcAdminClient = new KeycloakAdminClient({
  baseUrl: process.env.KEYCLOAK_URL,
  realmName: process.env.KEYCLOAK_REALM
});

// 初始化认证
await kcAdminClient.auth({
  grantType: 'client_credentials',
  clientId: process.env.KEYCLOAK_CLIENT_ID,
  clientSecret: process.env.KEYCLOAK_CLIENT_SECRET
});

export const getUserProfile = async (userId) => {
  try {
    const user = await kcAdminClient.users.findOne({ id: userId });

    return {
      display_name: `${user.firstName || ''} ${user.lastName || ''}`.trim() || user.username,
      avatar_url: user.attributes?.avatar_url?.[0] || null,
      email: user.email
    };
  } catch (error) {
    console.error('Failed to fetch user from Keycloak:', error);
    return {
      display_name: 'Unknown User',
      avatar_url: null,
      email: null
    };
  }
};
```

**选项 B: Hasura Remote Schema** (推荐用于高频访问)

需要创建一个 Keycloak GraphQL Adapter 服务：

```javascript
// services/keycloak-graphql-adapter/index.js
const { ApolloServer, gql } = require('apollo-server');
const KeycloakAdminClient = require('@keycloak/keycloak-admin-client').default;

const typeDefs = gql`
  type UserProfile {
    id: ID!
    display_name: String
    avatar_url: String
    email: String
    username: String
  }

  type Query {
    user(id: ID!): UserProfile
  }
`;

const resolvers = {
  Query: {
    user: async (_, { id }) => {
      const user = await kcAdminClient.users.findOne({ id });

      return {
        id: user.id,
        display_name: `${user.firstName || ''} ${user.lastName || ''}`.trim() || user.username,
        avatar_url: user.attributes?.avatar_url?.[0] || null,
        email: user.email,
        username: user.username
      };
    }
  }
};

const server = new ApolloServer({ typeDefs, resolvers });
server.listen(4001);
```

然后在 Hasura 中配置 Remote Schema:

```yaml
# services/hasura/metadata/remote_schemas.yaml
- name: keycloak
  definition:
    url: http://keycloak-graphql-adapter:4001/graphql
    timeout_seconds: 60
```

配置 Remote Relationship:

```yaml
# services/hasura/metadata/tables.yaml
- table:
    name: users
    schema: public
  remote_relationships:
    - name: profile
      definition:
        remote_schema: keycloak
        hasura_fields: [id]
        remote_field:
          user:
            arguments:
              id: $id
```

使用方式:

```graphql
query {
  users(where: {id: {_eq: "uuid"}}) {
    id
    datasources { ... }
    members { ... }
    profile {              # 从 Keycloak 动态获取
      display_name
      avatar_url
      email
    }
  }
}
```

---

## 5. 总结

### 问题回答

**是否可以不用 public.users 表？**

- **技术上可以**，但需要大量重构
- **实际上不推荐**，因为会失去：
  1. Hasura 的关联查询能力
  2. 数据库级别的数据完整性（外键约束）
  3. 查询性能优化
  4. 代码简洁性

### 最佳实践

**推荐做法**:
- ✅ 保留 `public.users` 表作为**用户注册表**
- ✅ 精简字段，只保留 `id` (对应 Keycloak UUID)
- ✅ 用户元数据（display_name, avatar_url）从 Keycloak 动态获取
- ✅ 通过 Hasura Remote Schema 或 Computed Fields 集成

**核心原则**:
> `public.users` 表不是用来存储用户数据，而是用来**建立关联关系**的枢纽表。

### 架构对比

**当前架构**:
```
Hasura Backend Plus (认证 + 用户数据)
  → public.users (完整用户信息)
    → datasources, members, alerts... (关联数据)
```

**迁移到 Keycloak 后的推荐架构**:
```
Keycloak (认证 + 用户数据)
  ↓ (同步)
public.users (仅 ID 注册表)
  → datasources, members, alerts... (关联数据)
  ↑ (远程关联)
Keycloak (通过 Remote Schema 获取 profile)
```

这种设计既保留了 Hasura 的强大关联查询能力，又实现了用户数据的单一数据源（Keycloak）。
