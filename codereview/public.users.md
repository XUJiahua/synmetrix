# public.users 表使用分析

## 📊 概述

`public.users` 表是整个 Synmetrix 平台的**用户身份核心**，它作为轻量级用户信息表，通过外键关系连接认证系统和所有业务数据。

## 1. 表结构定义

**位置**: `services/hasura/migrations/1628430122975_create_table_public_users/up.sql:1-7`

```sql
CREATE TABLE IF NOT EXISTS public.users (
    id uuid DEFAULT public.gen_random_uuid() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    display_name text,
    avatar_url text
);
```

### 关键特性

- **主键**: `id` (UUID)
- **时间戳**: `created_at`, `updated_at` (自动维护)
- **用户信息**: `display_name`, `avatar_url`
- **触发器**: `set_public_users_updated_at` - 自动更新 `updated_at` 字段（`services/hasura/migrations/1628430993001_auth_constraints/up.sql:54-55`）

### 约束

**位置**: `services/hasura/migrations/1628430993001_auth_constraints/up.sql:39`

```sql
SELECT create_constraint_if_not_exists('public.users', 'users_pkey', 'PRIMARY KEY (id);');
```

---

## 2. 在认证体系中的核心作用

### 2.1 与 auth.accounts 的关系

`public.users` 是认证系统的核心用户表，与 `auth.accounts` 通过外键关联：

**位置**: `services/hasura/migrations/1628430993001_auth_constraints/up.sql:45`

```sql
SELECT create_constraint_if_not_exists('auth.accounts', 'accounts_user_id_fkey',
  'FOREIGN KEY (user_id) REFERENCES public.users(id) ON UPDATE CASCADE ON DELETE CASCADE;');
```

- `auth.accounts.user_id` → `public.users.id`
- 这是一对一关系（`UNIQUE` 约束，第34行）
- 删除级联：`ON DELETE CASCADE`

**认证流程**:
```
用户登录 → auth.accounts (包含 email, password)
         → public.users (包含 display_name, avatar_url)
         → members (团队成员关系)
```

### 2.2 JWT Token 中的使用

**位置**: `services/actions/src/utils/jwt.js:8-36`

生成 JWT token 时使用 `userId`：

```javascript
const hasuraCompatibleJwtPayload = {
  [JWT_CLAIMS_NAMESPACE]: {
    ["x-hasura-user-id"]: userId,  // 来自 public.users.id
    ["x-hasura-allowed-roles"]: ["user"],
    ["x-hasura-default-role"]: "user"
  }
};
```

### 2.3 权限验证流程

**位置**: `services/cubejs/src/utils/checkAuth.js:19-85`

每个 API 请求都会执行以下流程：

1. 从 JWT 中提取 `x-hasura-user-id` (第55行)
2. 调用 `findUser({ userId })` 查询用户数据 (第64行)
3. 构建 `securityContext` 包含：
   - `userId`
   - `userScope` (数据源权限、分支权限等)

```javascript
const checkAuth = async (req) => {
  const authHeader = req.headers.authorization;
  const authToken = authHeader.split(" ")[1];

  jwtDecoded = jwt.verify(authToken, JWT_KEY, {
    algorithms: [JWT_ALGORITHM],
  });

  const { "x-hasura-user-id": userId } = jwtDecoded?.hasura || {};

  const user = await findUser({ userId });

  const userScope = defineUserScope(
    user.dataSources,
    user.members,
    dataSourceId,
    branchId,
    branchVersionId
  );

  req.securityContext = {
    authToken,
    userId,
    userScope,
  };
};
```

---

## 3. GraphQL 查询使用

### 3.1 核心用户查询

**位置**: `services/cubejs/src/utils/dataSourceHelpers.js:59-72`

```graphql
query ($_or: [users_bool_exp!]) {
  users(where: {_or: $_or}, limit: 1) {
    datasources {
      id, name, db_type, db_params, team_id
      branches {
        id, name, status
        versions {
          dataschemas { id, name, code }
        }
      }
    }
    members {
      id, team_id
      member_roles {
        team_role
        access_list { config }
      }
    }
  }
}
```

### 3.2 findUser 函数逻辑

**位置**: `services/cubejs/src/utils/dataSourceHelpers.js:127-154`

```javascript
export const findUser = async ({ userId }) => {
  const where = {
    _or: [
      { id: { _eq: userId } },              // 查找用户本身的数据源
      {
        members: {                           // 或查找用户作为团队成员可访问的数据源
          team: {
            members: {
              user_id: { _eq: userId },
            },
          },
        },
      },
    ],
  };

  const res = await fetchGraphQL(userQuery, { ...where });

  const dataSources = res?.data?.users?.[0]?.datasources;
  const members = res?.data?.users?.[0]?.members;

  return {
    dataSources,
    members,
  };
};
```

**返回数据**:
- `dataSources`: 用户可访问的所有数据源（包括自己创建的和团队共享的）
- `members`: 用户的团队成员关系（用于权限控制）

### 3.3 SQL 凭证查询

**位置**: `services/cubejs/src/utils/dataSourceHelpers.js:99-115`

用于 SQL API 认证：

```graphql
query ($username: String!) {
  sql_credentials(where: {username: {_eq: $username}}) {
    id
    user_id
    user {
      members {
        id, team_id
        member_roles {
          id, team_role
          access_list { config }
        }
      }
    }
    password
    username
    datasource {
      id, name, db_type, db_params, team_id
      branches { ... }
    }
  }
}
```

---

## 4. Hasura 权限配置

**位置**: `services/hasura/metadata/tables.yaml:1457-1584`

### 4.1 查询权限 (select)

```yaml
select_permissions:
  - role: user
    permission:
      columns:
        - id
        - created_at
        - updated_at
        - display_name
        - avatar_url
      filter:
        _or:
          - id: { _eq: X-Hasura-User-Id }      # 用户可以查看自己
          - members:                            # 或查看同团队成员
              team:
                members:
                  user_id: { _eq: X-Hasura-User-Id }
```

### 4.2 更新权限 (update)

```yaml
update_permissions:
  - role: user
    permission:
      columns:
        - display_name           # 只能更新显示名称
      filter:
        id: { _eq: X-Hasura-User-Id }  # 只能更新自己
      check: {}
```

### 4.3 关联关系配置

**位置**: `services/hasura/metadata/tables.yaml:1459-1539`

#### Object Relationships (一对一)
```yaml
object_relationships:
  - name: account
    using:
      manual_configuration:
        column_mapping:
          id: user_id
        remote_table:
          name: accounts
          schema: auth
```

#### Array Relationships (一对多)
```yaml
array_relationships:
  - name: alerts           # 用户创建的告警
  - name: branches         # 用户创建的分支
  - name: dataschemas      # 用户创建的数据模型
  - name: datasources      # 用户创建的数据源
  - name: members          # 用户的团队成员关系
  - name: reports          # 用户创建的报告
  - name: request_logs     # 用户的请求日志
  - name: sql_credentials  # 用户的 SQL 凭证
  - name: teams            # 用户拥有的团队
  - name: versions         # 用户创建的版本
```

---

## 5. 关联关系（外键引用）

`public.users` 是众多表的外键目标，以下是完整的关联表清单：

| 关联表 | 外键字段 | 关系类型 | 删除策略 | 说明 |
|--------|----------|----------|----------|------|
| `auth.accounts` | `user_id` | 一对一 | CASCADE | 认证账户 |
| `datasources` | `user_id` | 一对多 | CASCADE | 数据源创建者 |
| `dataschemas` | `user_id` | 一对多 | - | 数据模型创建者 |
| `branches` | `user_id` | 一对多 | - | 分支创建者 |
| `members` | `user_id` | 一对多 | CASCADE | 团队成员关系 |
| `teams` | `user_id` | 一对多 | - | 团队拥有者 |
| `dashboards` | `user_id` | 一对多 | CASCADE | 仪表板创建者 |
| `explorations` | `user_id` | 一对多 | - | 数据探索创建者 |
| `alerts` | `user_id` | 一对多 | - | 告警创建者 |
| `reports` | `user_id` | 一对多 | - | 报告创建者 |
| `sql_credentials` | `user_id` | 一对多 | - | SQL API 凭证 |
| `request_logs` | `user_id` | 一对多 | - | 请求日志 |
| `versions` | `user_id` | 一对多 | - | 版本创建者 |

**注意**: 大多数表的外键都设置为 `ON DELETE CASCADE`，删除用户会级联删除所有相关数据。

### 关键外键示例

**datasources 表**:
```sql
FOREIGN KEY ("user_id") REFERENCES "public"."users"("id")
ON UPDATE cascade ON DELETE cascade
```
位置: `services/hasura/migrations/1628432034298_create_table_public_datasources/up.sql:1`

**members 表**:
```sql
FOREIGN KEY ("user_id") REFERENCES "public"."users"("id")
ON UPDATE no action ON DELETE cascade
```
位置: `services/hasura/migrations/1629920087716_create_table_public_members/up.sql:1`

---

## 6. Event Trigger（事件触发器）

### 6.1 自动创建团队

**位置**: `services/hasura/metadata/tables.yaml:1568-1584`

```yaml
event_triggers:
  - name: create_team
    definition:
      enable_manual: true
      insert:
        columns: '*'
    retry_conf:
      interval_sec: 10
      num_retries: 0
      timeout_sec: 60
    webhook: '{{ACTIONS_URL}}/rpc/create_team'
```

**触发时机**: 当新用户插入到 `public.users` 表时

**处理逻辑**: `services/actions/src/rpc/createTeam.js:39-69`

```javascript
export default async (session, input) => {
  const { name = "Default team" } = input || {};
  const userId = session?.["x-hasura-user-id"] || input?.event?.data?.new?.id;

  let newTeam;

  try {
    // 1. 创建默认团队
    newTeam = await createTeam({ userId, name });
    const { id: teamId } = newTeam;

    // 2. 将用户添加为团队 Owner
    await createTeamMember({
      userId,
      teamId,
      role: OWNER_ROLE,
    });

    // 3. 将用户的资产关联到团队
    await fetchGraphQL(updateAssetsMutation, { teamId, userId });

    return newTeam;
  } catch (err) {
    // 失败时回滚，删除已创建的团队
    if (newTeam?.id) {
      await fetchGraphQL(deleteTeamMutation, { id: newTeam?.id });
    }
    return apiError(err);
  }
};
```

**updateAssetsMutation**:
```graphql
mutation ($userId: uuid!, $teamId: uuid!) {
  update_datasources(
    where: {_and: {team_id: {_is_null: true}, user_id: {_eq: $userId}}},
    _set: {team_id: $teamId}
  ) {
    affected_rows
  }
  update_dashboards(
    where: {_and: {team_id: {_is_null: true}}, user_id: {_eq: $userId}},
    _set: {team_id: $teamId}
  ) {
    affected_rows
  }
}
```

---

## 7. 典型使用场景

### 场景 1：用户注册

```
1. hasura_plus 服务创建 auth.accounts 记录
2. 同时创建 public.users 记录
3. 触发 create_team event
4. 自动创建默认团队并设置用户为 owner
5. 将用户现有资产关联到该团队
```

### 场景 2：API 请求认证

**流程**: `services/cubejs/src/utils/checkAuth.js`

```
1. 客户端携带 JWT token (包含 x-hasura-user-id)
2. checkAuth 验证 token 并提取 userId
3. findUser 查询用户的数据源和成员关系
4. defineUserScope 构建权限上下文
5. 根据权限过滤可访问的数据
```

**代码示例**:
```javascript
// 1. 提取 userId
const { "x-hasura-user-id": userId } = jwtDecoded?.hasura || {};

// 2. 查询用户信息
const user = await findUser({ userId });

// 3. 构建权限范围
const userScope = defineUserScope(
  user.dataSources,    // 可访问的数据源
  user.members,        // 团队成员关系
  dataSourceId,        // 当前请求的数据源
  branchId,            // 当前分支
  branchVersionId      // 当前版本
);
```

### 场景 3：SQL API 认证

**位置**: `services/cubejs/src/utils/checkSqlAuth.js:38-50`

```
1. 用户使用 SQL 客户端连接（MySQL/PostgreSQL 协议）
2. checkSqlAuth 根据 username 查找 sql_credentials
3. 通过 sql_credentials.user_id 关联到 public.users
4. 获取用户的团队成员关系和权限
5. 构建 SQL 查询的安全上下文
```

**代码示例**:
```javascript
const checkSqlAuth = async (_, user) => {
  // 1. 查找 SQL 凭证
  const sqlCredentials = await findSqlCredentials(user);

  // 2. 构建安全上下文
  return {
    password: sqlCredentials?.password,
    securityContext: {
      userId: sqlCredentials?.user_id,
      userScope: buildSqlSecurityContext(sqlCredentials),
    },
  };
};
```

### 场景 4：团队协作

```
1. 用户 A 创建数据源 → datasources.user_id = A
2. 用户 A 邀请用户 B 加入团队 → members 表建立关联
3. 用户 B 访问时，findUser 通过 members 关系找到可访问的数据源
4. 权限基于 member_roles.team_role 控制（owner/admin/viewer）
```

**权限检查逻辑**: `services/cubejs/src/utils/defineUserScope.js:3-24`

```javascript
export const getDataSourceAccessList = (
  allMembers,
  selectedDataSourceId,
  selectedTeamId
) => {
  // 查找用户在该团队的角色
  const dataSourceMemberRole = allMembers.find(
    (member) => member.team_id === selectedTeamId
  )?.member_roles?.[0];

  if (!dataSourceMemberRole) {
    throw new Error(`403: member role not found`);
  }

  // 获取访问列表配置
  const { access_list: accessList } = dataSourceMemberRole;
  const dataSourceAccessList =
    accessList?.config?.datasources?.[selectedDataSourceId]?.cubes;

  return {
    role: dataSourceMemberRole?.team_role,
    dataSourceAccessList,
  };
};
```

---

## 8. 数据示例

**位置**: `services/hasura/seeds/1708465495269_demoSeed.sql:2`

```sql
INSERT INTO public.users
  (id, created_at, updated_at, display_name, avatar_url)
VALUES
  ('bd254cd6-ada3-4803-88ec-a47749459169',
   '2024-02-15 22:46:43.475355+00',
   '2024-02-15 22:46:43.475355+00',
   'demo@synmetrix.org',
   NULL)
ON CONFLICT DO NOTHING;
```

演示用户：
- ID: `bd254cd6-ada3-4803-88ec-a47749459169`
- 显示名称: `demo@synmetrix.org`
- 无头像 URL

---

## 9. 关键代码位置汇总

| 功能 | 文件路径 | 行号 |
|------|----------|------|
| **表定义** | `services/hasura/migrations/1628430122975_create_table_public_users/up.sql` | 1-7 |
| **主键约束** | `services/hasura/migrations/1628430993001_auth_constraints/up.sql` | 39 |
| **更新触发器** | `services/hasura/migrations/1628430993001_auth_constraints/up.sql` | 54-55 |
| **GraphQL 查询** | `services/cubejs/src/utils/dataSourceHelpers.js` | 59-72 |
| **findUser 函数** | `services/cubejs/src/utils/dataSourceHelpers.js` | 127-154 |
| **SQL 凭证查询** | `services/cubejs/src/utils/dataSourceHelpers.js` | 99-115 |
| **JWT 生成** | `services/actions/src/utils/jwt.js` | 8-36 |
| **认证检查** | `services/cubejs/src/utils/checkAuth.js` | 19-85 |
| **SQL 认证** | `services/cubejs/src/utils/checkSqlAuth.js` | 38-50 |
| **权限定义** | `services/cubejs/src/utils/defineUserScope.js` | 26-96 |
| **访问列表** | `services/cubejs/src/utils/defineUserScope.js` | 3-24 |
| **Hasura 元数据** | `services/hasura/metadata/tables.yaml` | 1457-1584 |
| **创建团队** | `services/actions/src/rpc/createTeam.js` | 39-69 |
| **演示数据** | `services/hasura/seeds/1708465495269_demoSeed.sql` | 2 |

---

## 10. 架构设计要点

### 10.1 轻量级设计

`public.users` 表只存储最基本的用户展示信息：
- ✅ `display_name`: 用户显示名称
- ✅ `avatar_url`: 用户头像 URL
- ❌ 不存储认证信息（email, password 在 `auth.accounts`）
- ❌ 不存储权限信息（通过 `members` 和 `member_roles` 管理）

### 10.2 中心枢纽模式

作为用户身份的中心节点：
- 所有业务表通过 `user_id` 引用 `public.users.id`
- 通过外键级联删除保证数据一致性
- 通过 Hasura 关系定义实现 GraphQL 导航

### 10.3 多租户隔离

通过以下机制实现多租户：

1. **直接所有权**: `datasources.user_id`
2. **团队共享**: `members.user_id` + `members.team_id`
3. **权限控制**: `member_roles.team_role` + `access_lists`

**查询过滤逻辑**:
```javascript
{
  _or: [
    { user_id: { _eq: userId } },           // 用户直接拥有
    { team: { members: { user_id: { _eq: userId } } } }  // 团队共享
  ]
}
```

### 10.4 自动化流程

- **自动创建团队**: 新用户注册时通过 Event Trigger 自动创建默认团队
- **自动更新时间**: 通过数据库触发器自动维护 `updated_at`
- **级联删除**: 删除用户时自动清理关联数据

---

## 总结

`public.users` 表是 Synmetrix 平台的**用户身份核心**，它：

1. ✅ **轻量级设计**：只存储基本的用户展示信息（display_name, avatar_url）
2. ✅ **中心枢纽**：通过外键被系统中几乎所有业务表引用
3. ✅ **权限基础**：所有权限检查都基于 `user_id` 进行
4. ✅ **多租户支持**：通过 `members` 表实现团队级别的多租户隔离
5. ✅ **自动化**：新用户创建时自动触发团队创建，提供开箱即用体验
6. ✅ **数据安全**：通过 Hasura 权限系统严格控制查询和更新权限

在整体架构中，`public.users` 与认证系统（`auth.*`）、团队系统（`teams`, `members`）和业务系统（`datasources`, `explorations` 等）紧密协作，形成了完整的用户管理和权限控制体系。
