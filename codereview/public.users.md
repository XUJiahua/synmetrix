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

**核心结论**: **JWT 中的 `x-hasura-user-id` 来源于 `public.users.id`**

#### 2.2.1 数据流向图

```
┌─────────────────────────────────────────────────────────────────┐
│                      数据表关系                                   │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────────────┐          ┌──────────────────┐            │
│  │  public.users    │          │  auth.accounts   │            │
│  ├──────────────────┤          ├──────────────────┤            │
│  │ id (PK) ←────────┼──────────┼─ user_id (FK)    │            │
│  │ display_name     │  引用     │ email            │            │
│  │ avatar_url       │          │ password_hash    │            │
│  │ created_at       │          │ active           │            │
│  └──────────────────┘          │ default_role     │            │
│         ↑                      └──────────────────┘            │
│         │                              ↑                        │
│         │ JWT 使用此 ID                │ 通过 GraphQL          │
│         │                              │ 关系查询              │
│         └──────────────────────────────┘                        │
└─────────────────────────────────────────────────────────────────┘
```

#### 2.2.2 用户注册时的 ID 生成流程

**位置**: hasura-backend-plus (nhost/hasura-backend-plus:v2.7.1)

```typescript
// 1. hasura-backend-plus 执行嵌套插入 GraphQL mutation
// 文件: src/routes/auth/register.ts
accounts = await request(insertAccount, {
  account: {
    email: "user@example.com",
    password_hash: "$2a$10...",
    user: {
      data: {
        display_name: "user@example.com"
      }
    }
  }
})

// Hasura 自动处理：
// Step 1: 先插入 public.users → 生成 UUID (如 "550e8400-...")
// Step 2: 再插入 auth.accounts → 使用上述 UUID 作为 user_id

// 2. 提取返回的 user.id
const account = accounts.insert_auth_accounts.returning[0]

const user = {
  id: account.user.id,  // ← 这就是 public.users.id
  display_name: account.user.display_name,
  email: account.email,
  avatar_url: account.user.avatar_url
}

// 3. 生成 JWT
const jwt_token = createHasuraJwt(account)
```

**Hasura 嵌套插入机制**:

```graphql
# GraphQL Mutation
mutation($account: auth_accounts_insert_input!) {
  insert_auth_accounts(objects: [$account]) {
    affected_rows
    returning {
      id
      email
      user {
        id              # ← public.users.id
        display_name
        avatar_url
      }
    }
  }
}
```

**数据库执行顺序**:

```
Transaction BEGIN
  ↓
1. INSERT INTO public.users (id, display_name, ...)
   VALUES (gen_random_uuid(), 'user@example.com', ...)
   RETURNING id;
   -- 结果: id = '550e8400-e29b-41d4-a716-446655440000'
  ↓
2. INSERT INTO auth.accounts (user_id, email, password_hash, ...)
   VALUES ('550e8400-e29b-41d4-a716-446655440000', 'user@example.com', '$2a$10...', ...);
  ↓
Transaction COMMIT
```

#### 2.2.3 JWT 生成函数

**位置**: hasura-backend-plus (nhost/hasura-backend-plus:v2.7.1/src/shared/jwt.ts)

```typescript
export const createHasuraJwt = (accountData: AccountData): string =>
  sign(
    {
      [CONFIG_JWT.CLAIMS_NAMESPACE]: generatePermissionVariables(accountData, true)
    },
    accountData
  )

// generatePermissionVariables 函数内部：
const generatePermissionVariables = (accountData, jwt) => {
  const { user } = accountData
  const prefix = jwt ? 'x-hasura-' : ''

  return {
    [`${prefix}user-id`]: user.id,  // ← 设置 x-hasura-user-id = public.users.id
    [`${prefix}allowed-roles`]: roles,
    [`${prefix}default-role`]: accountData.default_role
  }
}
```

#### 2.2.4 最终 JWT 结构

```json
{
  "sub": "550e8400-e29b-41d4-a716-446655440000",
  "iat": 1642248600,
  "exp": 1642259400,
  "iss": "hasura-auth",
  "aud": "hasura",
  "hasura": {
    "x-hasura-user-id": "550e8400-e29b-41d4-a716-446655440000",
    "x-hasura-allowed-roles": ["user"],
    "x-hasura-default-role": "user"
  }
}
```

**Claim 说明**:
- `sub`: Subject (用户ID) = `public.users.id`
- `hasura.x-hasura-user-id`: Hasura 权限系统使用的用户 ID = `public.users.id`
- 两者值相同，都是 `public.users.id`

#### 2.2.5 Synmetrix Actions 服务的 JWT 生成

**位置**: `services/actions/src/utils/jwt.js:8-36`

Synmetrix 的 actions 服务也可以生成 JWT（用于某些特殊场景）：

```javascript
const generateUserAccessToken = async (userId) => {
  const hasuraCompatibleJwtPayload = {
    [JWT_CLAIMS_NAMESPACE]: {
      ["x-hasura-user-id"]: userId,  // userId 参数必须传入 public.users.id
      ["x-hasura-allowed-roles"]: ["user"],
      ["x-hasura-default-role"]: "user"
    }
  };

  const secret = new TextEncoder().encode(JWT_KEY);
  const accessToken = await new SignJWT(hasuraCompatibleJwtPayload)
    .setProtectedHeader({ alg: JWT_ALGORITHM })
    .setIssuedAt()
    .setIssuer("services:actions")
    .setAudience("services:hasura")
    .setExpirationTime(`${JWT_EXPIRES_IN}m`)
    .setSubject(userId)  // ← userId = public.users.id
    .sign(secret);

  return accessToken;
};
```

**注意**: 这个函数接收的 `userId` 参数必须是 `public.users.id`。

#### 2.2.6 ID 来源验证方法

你可以通过以下方式验证 JWT 中的 user_id 确实来自 `public.users.id`：

```bash
# 1. 登录获取 JWT
curl -X POST http://localhost:8081/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"demo@synmetrix.org","password":"demodemo"}' \
  | jq -r '.jwt_token' > token.txt

# 2. 解码 JWT 查看 user_id
JWT_TOKEN=$(cat token.txt)
echo $JWT_TOKEN | cut -d. -f2 | base64 -d | jq .

# 输出示例：
# {
#   "sub": "bd254cd6-ada3-4803-88ec-a47749459169",
#   "hasura": {
#     "x-hasura-user-id": "bd254cd6-ada3-4803-88ec-a47749459169"
#   }
# }

# 3. 在数据库中验证此 ID 存在于 public.users 表
docker exec -it postgres psql -U postgres -d default_db -c \
  "SELECT u.id, u.display_name, a.email
   FROM public.users u
   JOIN auth.accounts a ON a.user_id = u.id
   WHERE u.id = 'bd254cd6-ada3-4803-88ec-a47749459169';"

# 结果示例：
#                   id                  | display_name |       email
# --------------------------------------+--------------+--------------------
#  bd254cd6-ada3-4803-88ec-a47749459169 | demo@...     | demo@synmetrix.org
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

### 6.1 create_team 事件触发器

**核心作用**: 当新用户创建时自动创建默认团队，实现开箱即用的团队协作体验。

#### 6.1.1 Event Trigger 配置

**位置**: `services/hasura/metadata/tables.yaml:1567-1584`

```yaml
- table:
    name: users
    schema: public
  event_triggers:
    - name: create_team
      definition:
        enable_manual: true      # 允许手动触发
        insert:
          columns: '*'           # 监听所有列的 INSERT 操作
      retry_conf:
        interval_sec: 10         # 重试间隔 10 秒
        num_retries: 0           # 不自动重试
        timeout_sec: 60          # 超时时间 60 秒
      webhook: '{{ACTIONS_URL}}/rpc/create_team'
      cleanup_config:
        batch_size: 10000
        clean_invocation_logs: false
        clear_older_than: 168
        paused: true
        schedule: 0 0 * * *
        timeout: 60
```

**环境变量配置** (`.env:50`):
```bash
ACTIONS_URL=http://actions:3000
```

**完整 Webhook URL**: `http://actions:3000/rpc/create_team`

#### 6.1.2 触发时机

**自动触发条件**:
```sql
-- 当执行以下 SQL 时自动触发
INSERT INTO public.users (id, display_name, avatar_url, created_at, updated_at)
VALUES (gen_random_uuid(), 'user@example.com', NULL, NOW(), NOW());
```

**触发流程**:
```
1. hasura-backend-plus 执行用户注册
   ↓
2. Hasura 嵌套插入:
   INSERT INTO public.users (...)  ← 🔥 触发点
   RETURNING id;
   ↓
3. Hasura Event System 检测到 INSERT 操作
   ↓
4. Hasura 发送 POST 请求到 Webhook:
   POST http://actions:3000/rpc/create_team
   ↓
5. actions 服务接收并处理事件
```

#### 6.1.3 Event Payload 结构

**Event Trigger 自动触发时的 Payload**:
```json
{
  "event": {
    "session_variables": {},
    "op": "INSERT",
    "data": {
      "old": null,
      "new": {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "created_at": "2024-11-12T10:30:00.000Z",
        "updated_at": "2024-11-12T10:30:00.000Z",
        "display_name": "user@example.com",
        "avatar_url": null
      }
    }
  },
  "created_at": "2024-11-12T10:30:00.000Z",
  "id": "event-uuid",
  "trigger": {
    "name": "create_team"
  },
  "table": {
    "schema": "public",
    "name": "users"
  }
}
```

**GraphQL Action 手动触发时的 Payload**:
```json
{
  "session_variables": {
    "x-hasura-user-id": "550e8400-e29b-41d4-a716-446655440000",
    "x-hasura-role": "user"
  },
  "input": {
    "name": "My Team"
  }
}
```

#### 6.1.4 Webhook 路由机制

**位置**: `services/actions/index.js:35-66`

```javascript
app.post("/rpc/:method", async (req, res) => {
  const { method } = req.params;  // method = "create_team"

  const { session_variables: session, input } = req.body;
  const requestInput = input ?? req.body;

  // create_team → createTeam (下划线转驼峰)
  const modulePath = `./src/rpc/${hyphensToCamelCase(method)}.js`;
  const module = await import(modulePath);  // 动态导入 ./src/rpc/createTeam.js

  if (!module) {
    return res.status(404).json({
      code: "method_not_found",
      message: `Module "${modulePath}" not found`
    });
  }

  // 执行 createTeam.js 的 default export
  const data = await module.default(session, requestInput, req.headers);

  if (data) {
    if (data.error) {
      return res.status(400).json(data);
    }
    return res.json(data);
  }

  return res.status(400).json({
    code: "method_has_no_output",
    message: `No output from the method "${method}"`
  });
});
```

**路径转换逻辑** (`services/actions/src/utils/hyphensToCamelCase.js:1-10`):
```javascript
const hyphensToCamelCase = (str) => {
  const arr = str.split(/[_-]/);  // ['create', 'team']
  let newStr = "";
  for (let i = 1; i < arr.length; i += 1) {
    newStr += arr[i].charAt(0).toUpperCase() + arr[i].slice(1);  // 'Team'
  }
  return arr[0] + newStr;  // 'createTeam'
};
```

#### 6.1.5 createTeam 实现逻辑

**位置**: `services/actions/src/rpc/createTeam.js:39-69`

```javascript
export default async (session, input) => {
  const { name = "Default team" } = input || {};

  // 🔑 关键：userId 来源
  // 1. 从 session 中获取（手动调用时）
  // 2. 从 event.data.new.id 获取（Event Trigger 自动触发时）
  const userId = session?.["x-hasura-user-id"] || input?.event?.data?.new?.id;

  let newTeam;

  try {
    // 1. 创建默认团队
    newTeam = await createTeam({ userId, name });
    const { id: teamId } = newTeam;

    if (!teamId) {
      throw new Error("No team created. Contact Administrator");
    }

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

**GraphQL Mutations**:

```javascript
// 创建团队
const createTeamMutation = `
  mutation ($user_id: uuid, $name: String) {
    insert_teams_one(object: { user_id: $user_id, name: $name }) {
      id
      name
    }
  }
`;

// 关联用户资产到团队
const updateAssetsMutation = `
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
`;

// 删除团队（回滚时使用）
const deleteTeamMutation = `
  mutation ($id: uuid!) {
    delete_teams_by_pk(id: $id) {
      id
    }
  }
`;
```

#### 6.1.6 userId 提取逻辑

```javascript
const userId = session?.["x-hasura-user-id"] || input?.event?.data?.new?.id;
```

**提取优先级**:

| 来源 | 触发方式 | 值 | 说明 |
|------|---------|-----|------|
| `session["x-hasura-user-id"]` | GraphQL Action 手动调用 | JWT token 中的 user_id | 优先使用 |
| `input.event.data.new.id` | Event Trigger 自动触发 | INSERT 操作的新记录 ID | 用户注册时 |

#### 6.1.7 触发方式对比

| 特性 | Event Trigger (自动) | GraphQL Action (手动) |
|------|---------------------|----------------------|
| **触发时机** | `INSERT INTO public.users` | 用户主动调用 `create_team` mutation |
| **userId 来源** | `event.data.new.id` | `session["x-hasura-user-id"]` |
| **团队名称** | `"Default team"` | 用户指定 |
| **使用场景** | 用户注册时自动创建 | 用户手动创建额外团队 |
| **请求示例** | N/A（Hasura 自动发送） | 见测试用例 `tests/stepci/owner_flow.yml:121-148` |

**手动触发示例** (`tests/stepci/owner_flow.yml:121-148`):
```yaml
- name: create_team
  http:
    url: ${{env.HASURA_ENDPOINT}}
    method: POST
    headers:
      Authorization: Bearer ${{captures.accessToken}}
      x-hasura-user-id: ${{captures.userId}}
    graphql:
      query: |
        mutation ($name: String!) {
          create_team(name: $name) {
            id
            name
          }
        }
      variables:
        name: "My Team"
```

#### 6.1.8 完整数据流图

```
┌─────────────────────────────────────────────────────────────────┐
│              用户注册自动创建团队流程                              │
└─────────────────────────────────────────────────────────────────┘

1. 前端: POST /auth/register
   ↓
2. hasura-backend-plus:
   执行嵌套插入 GraphQL mutation
   ↓
3. PostgreSQL 事务:
   BEGIN;
     INSERT INTO public.users (id, display_name, ...)
     VALUES (gen_random_uuid(), 'user@example.com', ...)
     RETURNING id;  ← 🔥 Event Trigger 触发点

     INSERT INTO auth.accounts (user_id, email, password_hash, ...)
     VALUES ('<上述UUID>', 'user@example.com', '$2a$10...', ...);
   COMMIT;
   ↓
4. Hasura Event System:
   检测到 public.users INSERT 操作
   构建 Event Payload
   ↓
5. Hasura 发送 Webhook:
   POST http://actions:3000/rpc/create_team
   Content-Type: application/json
   Body: {
     "event": {
       "data": {
         "new": {
           "id": "550e8400-...",
           "display_name": "user@example.com",
           ...
         }
       }
     }
   }
   ↓
6. actions 服务:
   /rpc/:method 路由接收请求
   method = "create_team"
   ↓
7. 动态加载模块:
   hyphensToCamelCase("create_team") → "createTeam"
   import('./src/rpc/createTeam.js')
   ↓
8. createTeam 执行:
   a. userId = input.event.data.new.id
   b. 创建团队:
      INSERT INTO teams (user_id, name)
      VALUES (userId, 'Default team')
   c. 创建成员关系:
      INSERT INTO members (user_id, team_id)
      VALUES (userId, teamId)
   d. 创建成员角色:
      INSERT INTO member_roles (member_id, team_role)
      VALUES (memberId, 'owner')
   e. 关联用户资产:
      UPDATE datasources SET team_id = teamId
      WHERE user_id = userId AND team_id IS NULL
   ↓
9. 返回团队信息:
   {
     "id": "team-uuid",
     "name": "Default team"
   }
   ↓
10. 用户注册完成，拥有默认团队
```

#### 6.1.9 错误处理与回滚

```javascript
try {
  // 1. 创建团队
  newTeam = await createTeam({ userId, name });
  const { id: teamId } = newTeam;

  // 2. 添加成员
  await createTeamMember({ userId, teamId, role: OWNER_ROLE });

  // 3. 关联资产
  await fetchGraphQL(updateAssetsMutation, { teamId, userId });

  return newTeam;
} catch (err) {
  // 🔄 失败时回滚：删除已创建的团队
  if (newTeam?.id) {
    await fetchGraphQL(deleteTeamMutation, { id: newTeam?.id });
  }
  return apiError(err);
}
```

**回滚机制**:
- 如果创建团队后，添加成员或关联资产失败
- 自动删除已创建的团队记录
- 避免产生孤立的团队数据

#### 6.1.10 关键特性总结

| 特性 | 说明 |
|------|------|
| **触发表** | `public.users` |
| **触发操作** | `INSERT` |
| **监听列** | `*` (所有列) |
| **自动重试** | 否 (`num_retries: 0`) |
| **超时时间** | 60 秒 |
| **手动触发** | 支持 (`enable_manual: true`) |
| **默认团队名** | `"Default team"` |
| **默认角色** | `OWNER_ROLE` |
| **资产关联** | 自动关联用户的 datasources 和 dashboards |
| **错误回滚** | 支持（删除已创建的团队） |

---

## 7. 典型使用场景

### 场景 1：用户注册

**完整流程**:

```
1. 前端 POST /auth/register → hasura_plus 服务
   ↓
2. hasura_plus 执行嵌套插入 GraphQL mutation
   ↓
3. Hasura 在一个事务中执行：
   a. 先插入 public.users 记录 (生成 UUID)
      ← 🔥 触发 create_team Event Trigger
   b. 再插入 auth.accounts 记录 (user_id = 上述 UUID)
   ↓
4. hasura_plus 提取 account.user.id 生成 JWT
   - x-hasura-user-id = public.users.id
   ↓
5. Hasura Event System 检测到 INSERT 操作
   ↓
6. Hasura 发送 Webhook:
   POST http://actions:3000/rpc/create_team
   Body: { event: { data: { new: { id: "user_id", ... } } } }
   ↓
7. actions 服务执行 createTeam.js:
   a. userId = event.data.new.id (public.users.id)
   b. 创建团队 "Default team"
   c. 添加用户为 OWNER 角色
   d. 关联用户现有资产到团队
   ↓
8. 返回 JWT token + user 对象给前端
```

**关键点**:
- `public.users` 记录**先于** `auth.accounts` 创建
- `public.users.id` 是整个系统的用户身份标识
- JWT 中的 `x-hasura-user-id` 始终等于 `public.users.id`
- INSERT 操作触发 `create_team` Event Trigger（详见 [6.1 节](#61-create_team-事件触发器)）
- Event Trigger 从 `event.data.new.id` 提取 `public.users.id`
- 整个过程在一个数据库事务中完成，保证原子性
- Event Trigger 异步执行，不阻塞用户注册响应

**Event Trigger 详细说明**: 参见 [第 6.1 节 - create_team 事件触发器](#61-create_team-事件触发器)

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
| **外键约束** | `services/hasura/migrations/1628430993001_auth_constraints/up.sql` | 45 |
| **更新触发器** | `services/hasura/migrations/1628430993001_auth_constraints/up.sql` | 54-55 |
| **Event Trigger 配置** | `services/hasura/metadata/tables.yaml` | 1567-1584 |
| **用户注册逻辑** | hasura-backend-plus (nhost/hasura-backend-plus:v2.7.1) | `src/routes/auth/register.ts` |
| **JWT 生成逻辑** | hasura-backend-plus (nhost/hasura-backend-plus:v2.7.1) | `src/shared/jwt.ts` |
| **嵌套插入 Mutation** | hasura-backend-plus (nhost/hasura-backend-plus:v2.7.1) | `src/shared/queries.ts` |
| **RPC 路由** | `services/actions/index.js` | 35-66 |
| **路径转换函数** | `services/actions/src/utils/hyphensToCamelCase.js` | 1-10 |
| **createTeam 实现** | `services/actions/src/rpc/createTeam.js` | 39-69 |
| **createTeam userId 提取** | `services/actions/src/rpc/createTeam.js` | 41 |
| **createTeam Mutations** | `services/actions/src/rpc/createTeam.js` | 6-32 |
| **手动触发测试用例** | `tests/stepci/owner_flow.yml` | 121-148 |
| **GraphQL 查询** | `services/cubejs/src/utils/dataSourceHelpers.js` | 59-72 |
| **findUser 函数** | `services/cubejs/src/utils/dataSourceHelpers.js` | 127-154 |
| **SQL 凭证查询** | `services/cubejs/src/utils/dataSourceHelpers.js` | 99-115 |
| **JWT 生成 (Actions)** | `services/actions/src/utils/jwt.js` | 8-36 |
| **认证检查** | `services/cubejs/src/utils/checkAuth.js` | 19-85 |
| **JWT 解析提取 user_id** | `services/cubejs/src/utils/checkAuth.js` | 55 |
| **SQL 认证** | `services/cubejs/src/utils/checkSqlAuth.js` | 38-50 |
| **权限定义** | `services/cubejs/src/utils/defineUserScope.js` | 26-96 |
| **访问列表** | `services/cubejs/src/utils/defineUserScope.js` | 3-24 |
| **Hasura 元数据** | `services/hasura/metadata/tables.yaml` | 1457-1584 |
| **auth.accounts 关系** | `services/hasura/metadata/tables.yaml` | 21-30 |
| **ACTIONS_URL 配置** | `.env` | 50 |
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
4. ✅ **JWT 身份标识**：`public.users.id` 是 JWT 中 `x-hasura-user-id` 的唯一来源
5. ✅ **嵌套插入机制**：用户注册时先创建 `public.users` 记录，再创建 `auth.accounts` 记录
6. ✅ **多租户支持**：通过 `members` 表实现团队级别的多租户隔离
7. ✅ **自动化**：新用户创建时自动触发团队创建，提供开箱即用体验
8. ✅ **数据安全**：通过 Hasura 权限系统严格控制查询和更新权限

### 关键架构要点

**用户身份流转路径**:
```
public.users.id (生成)
    ↓
auth.accounts.user_id (存储引用)
    ↓
JWT.hasura.x-hasura-user-id (认证令牌)
    ↓
所有服务的权限验证 (统一身份标识)
```

**数据表关系**:
- `auth.accounts.user_id` → `public.users.id` (一对一，外键约束)
- `members.user_id` → `public.users.id` (一对多，团队成员)
- `datasources.user_id` → `public.users.id` (一对多，数据源所有者)
- 其他业务表 → `public.users.id` (一对多，创建者追踪)

**注册时的创建顺序**:
1. 先创建 `public.users` 记录（生成 UUID）
2. 再创建 `auth.accounts` 记录（使用上述 UUID）
3. 整个过程在一个数据库事务中完成
4. JWT token 使用 `public.users.id` 作为用户标识

在整体架构中，`public.users` 与认证系统（`auth.*`）、团队系统（`teams`, `members`）和业务系统（`datasources`, `explorations` 等）紧密协作，形成了完整的用户管理和权限控制体系。`public.users.id` 是整个系统中用户的**唯一身份标识**，贯穿于认证、授权、数据隔离的全过程。
