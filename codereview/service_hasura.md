# Hasura 服务分析

Hasura 开创了“从数据库即时生成 GraphQL API”的模式。

https://hasura.io/docs/2.0/getting-started/docker-simple/

## 概述

`services/hasura/` 是 Synmetrix 架构中的 **GraphQL API 层**,基于 Hasura v2.46.0 构建,运行在端口 8080。作为系统的 API 网关,它为前端提供统一的 GraphQL 接口,并负责认证授权、权限控制和业务流程编排。

## 架构定位

```
Frontend (Client)
    ↓ JWT Auth
Hasura GraphQL (8080) ← GraphQL API 层
    ↓ RPC Calls
Actions Service (3000)
    ↓ API Calls
Cube.js (4000)
    ↓ SQL Queries
Data Sources (PostgreSQL/MySQL/etc.)
```

## 核心功能模块

### 1. 数据库模型与权限管理

#### 认证系统 (`auth` schema)
- `accounts` - 用户账户表
- `account_providers` - 账户与身份提供商关联
- `account_roles` - 账户角色关联
- `providers` - 身份提供商配置
- `roles` - 角色定义
- `refresh_tokens` - 刷新令牌管理

#### 核心业务实体 (`public` schema)

**团队协作模块**:
- `teams` - 团队表
- `members` - 成员表
- `member_roles` - 成员角色关联
- `access_lists` - 访问控制列表
- `team_roles` - 团队角色枚举

**数据源管理模块**:
- `datasources` - 数据源配置
- `dataschemas` - 数据模型代码
- `branches` - 分支管理
- `versions` - 版本控制
- `branch_statuses` - 分支状态枚举

**数据探索模块**:
- `explorations` - 数据探索查询
- `dashboards` - 仪表板
- `pinned_items` - 固定项(图表/查询)

**监控告警模块**:
- `alerts` - 告警配置
- `reports` - 报告配置
- `request_logs` - 请求日志
- `request_event_logs` - 请求事件日志

**SQL API 模块**:
- `sql_credentials` - SQL API 访问凭证

**事件追踪**:
- `events` - 分析事件记录

#### 细粒度权限控制

使用 Hasura 的行级安全(Row Level Security)实现多租户隔离:

**权限模式**:
```yaml
select_permissions:
  - role: user
    permission:
      columns: [...]
      filter:
        _or:
          - user_id:
              _eq: X-Hasura-User-Id
          - team:
              members:
                user_id:
                  _eq: X-Hasura-User-Id
```

**关键特性**:
- 基于 JWT claims (`X-Hasura-User-Id`, `X-Hasura-Role`) 的用户识别
- 通过关系图谱自动推断权限边界
- 支持团队共享资源访问控制
- 不同角色(owner/admin/member)的差异化权限

### 2. GraphQL Actions (业务逻辑接口)

定义了 20+ 个自定义 actions,转发到 Actions 服务 (http://localhost:3000/rpc/*):

#### 数据源操作
- `check_connection` - 测试数据源连接
  - 输入: `datasource_id`
  - 输出: `{ message, code }`

- `fetch_tables` - 获取数据源表结构
  - 输入: `datasource_id`
  - 输出: `{ schema: json }`
  - 超时: 180秒

- `fetch_meta` - 获取 Cube.js 元数据
  - 输入: `datasource_id, branch_id`
  - 输出: `{ cubes: json }`

- `gen_dataschemas` - 自动生成数据模型
  - 输入: `datasource_id, branch_id, tables[], overwrite, format`
  - 输出: `{ message, code }`
  - 超时: 180秒

- `export_data_models` - 导出数据模型
  - 输入: `branch_id`
  - 输出: `{ download_url }`

#### 查询与分析
- `run_query` - 执行任意查询
  - 输入: `datasource_id, branch_id, query, limit`
  - 输出: `{ result: json }`

- `fetch_dataset` - 获取探索数据集
  - 输入: `exploration_id, offset, limit`
  - 输出: `{ annotation, data, query, progress, hitLimit, slowQuery, external, dbType, dataSource, lastRefreshTime }`

- `gen_sql` - 生成 SQL
  - 输入: `exploration_id`
  - 输出: `{ result: json }`

- `pre_aggregations` - 管理预聚合
  - 输入: `datasource_id`
  - 输出: `{ data: json }`

- `pre_aggregation_preview` - 预览预聚合数据
  - 输入: `datasource_id, pre_aggregation_id, table_name`
  - 输出: `{ data: json }`

#### 团队协作
- `create_team` - 创建团队
  - 输入: `name`
  - 输出: `{ id, name }`

- `invite_team_member` - 邀请成员
  - 输入: `email, teamId, role, magicLink`
  - 输出: `{ code, message, memberId }`

#### 监控告警
- `send_test_alert` - 发送测试告警
  - 输入: `name, deliveryType, deliveryConfig, explorationId`
  - 输出: `{ error, result }`

#### 事件追踪
- `create_events` - 创建分析事件 (异步)
  - 输入: `objects[{ data, page_context, device_context, user }]`
  - 输出: `{ affected_rows }`
  - 权限: anonymous

### 3. Event Triggers (事件驱动自动化)

#### 告警/报告定时任务管理
配置在 `alerts` 和 `reports` 表上:

- `create_cron_task_by_alert` - 创建告警定时任务
  - 触发: INSERT 或 UPDATE (delivery_config, delivery_type, exploration_id, trigger_config, schedule)
  - Webhook: `/rpc/create_cron_task_by_alert`
  - 重试: 3次,间隔10秒

- `delete_cron_task_by_alert` - 删除告警定时任务
  - 触发: DELETE
  - Webhook: `/rpc/delete_cron_task_by_alert`

- `create_cron_task_by_report` - 创建报告定时任务
  - 触发: INSERT 或 UPDATE (delivery_config, exploration_id, schedule, delivery_type)
  - Webhook: `/rpc/create_cron_task_by_alert`

- `delete_cron_task_by_report` - 删除报告定时任务
  - 触发: DELETE
  - Webhook: `/rpc/delete_cron_task_by_alert`

#### 数据模型管理
- `recalculate_dataschemas` - 数据源变更时重算模型
  - 配置在: `datasources` 表
  - 触发: 手动触发 (enable_manual: true)
  - Webhook: `/rpc/recalculate_dataschemas`
  - 重试: 0次

#### 文档自动生成
- `generate_dataschemas_docs` - 自动生成模型文档
  - 配置在: `versions` 表
  - 触发: INSERT
  - Webhook: `/rpc/gen_schemas_docs`

#### 团队初始化
- `create_team` - 用户创建时自动创建默认团队
  - 配置在: `users` 表
  - 触发: INSERT
  - Webhook: `/rpc/create_team`

**Trigger 配置特点**:
- 支持手动触发 (`enable_manual: true`)
- 可配置重试策略 (次数、间隔、超时)
- 日志清理策略 (保留7天,每天凌晨清理)

### 4. 数据库迁移 (Migrations)

`migrations/` 目录包含 90+ 个迁移文件,按时间戳命名:

**迁移历史**:
```
1628429118205 - 创建 auth.account_providers
1628430047295 - 创建 auth.accounts
1628430075537 - 创建 auth.providers
1628430092590 - 创建 auth.refresh_tokens
1628430107013 - 创建 auth.roles
1628430122975 - 创建 public.users
1628430993001 - 添加认证约束
1628431626432 - 创建 public.teams
1628432034298 - 创建 public.datasources
1628432207371 - 创建 public.dataschemas
1628432650921 - 创建 public.explorations
1628432811866 - 创建 public.dashboards
1628432939628 - 创建 public.pinned_items
1629920087716 - 创建 public.members
1630346633662 - 添加 dataschemas.branch 列
1630605085565 - 添加 dataschemas.checksum 列
1630608398065 - 创建 set_dataschema_checksum 函数
1630608415500 - 创建 dataschemas checksum 触发器
...
```

**迁移特点**:
- 每个迁移包含 `up.sql` 和 `down.sql` (可回滚)
- 渐进式架构演进 (从单用户→团队→多租户)
- 包含函数和触发器定义
- 索引和约束管理

### 5. 种子数据 (Seeds)

`seeds/1708465495269_demoSeed.sql` 包含演示数据:

**演示账户**:
- 用户: `demo@synmetrix.org`
- 密码: `demodemo`
- 角色: user

**演示数据**:
- 演示团队
- 演示数据源配置
- SQL API 访问凭证
- 示例数据模型

用于快速启动和功能演示。

## 配置文件

### config.yaml
```yaml
version: 2
endpoint: http://localhost:8080
metadata_directory: metadata
seeds_directory: seeds
actions:
  kind: synchronous
  handler_webhook_baseurl: http://localhost:3000
```

定义了 Hasura 的基本配置:
- API 端点
- 元数据目录位置
- Actions 服务地址

### metadata/ 目录结构
```
metadata/
├── actions.graphql      # GraphQL schema 定义
├── actions.yaml         # Actions 配置
├── allow_list.yaml      # 查询白名单
├── cron_triggers.yaml   # 定时任务
├── functions.yaml       # SQL 函数
├── network.yaml         # 网络配置
├── query_collections.yaml  # 查询集合
├── remote_schemas.yaml  # 远程 schema
├── tables.yaml          # 表配置和权限 (1664行)
└── version.yaml         # 元数据版本
```

## 核心价值

### 1. 统一 API 网关
- 为前端提供单一的 GraphQL 接口
- 自动生成 CRUD API
- 支持复杂关系查询
- 实时订阅能力 (subscriptions)

### 2. 权限中枢
- 基于 JWT claims 的细粒度访问控制
- 行级安全(RLS)自动执行
- 多租户数据隔离
- 声明式权限定义

### 3. 事件编排
- 数据库事件触发业务逻辑
- 自动化工作流编排
- 异步任务管理
- 重试和错误处理

### 4. 类型安全
- GraphQL schema 提供强类型 API 契约
- 自动生成 TypeScript 类型
- API 文档自动生成
- 编译时类型检查

## 开发工作流

### 管理 Hasura
```bash
# 查看迁移状态
./cli.sh hasura cli "migrate status"

# 打开 Hasura Console
./cli.sh hasura cli "console"

# 应用迁移
./cli.sh hasura cli "migrate apply"

# 导出元数据
./cli.sh hasura cli "metadata export"
```

### 创建新迁移
```bash
# 创建新迁移
./cli.sh hasura cli "migrate create <migration_name>"

# 编辑 up.sql 和 down.sql
# 应用迁移
./cli.sh hasura cli "migrate apply"
```

### 添加新 Action
1. 在 `metadata/actions.graphql` 中定义 GraphQL schema
2. 在 `metadata/actions.yaml` 中配置 action
3. 在 `services/actions/src/rpc/<method>.js` 中实现业务逻辑
4. 应用元数据: `./cli.sh hasura cli "metadata apply"`

### 配置权限
1. 在 Hasura Console 中配置表权限
2. 或直接编辑 `metadata/tables.yaml`
3. 应用元数据

## 技术细节

### 多租户隔离策略

**用户级隔离**:
```yaml
filter:
  user_id:
    _eq: X-Hasura-User-Id
```

**团队级隔离**:
```yaml
filter:
  team:
    members:
      user_id:
        _eq: X-Hasura-User-Id
```

**混合隔离** (用户自有 + 团队共享):
```yaml
filter:
  _or:
    - user_id:
        _eq: X-Hasura-User-Id
    - team:
        members:
          user_id:
            _eq: X-Hasura-User-Id
```

### 计算字段

**数据源密码隐藏**:
```yaml
computed_fields:
  - name: db_params_computed
    definition:
      function:
        name: hide_password
        schema: public
```

**请求持续时间计算**:
```yaml
computed_fields:
  - name: duration
    definition:
      function:
        name: duration
        schema: public
```

### 触发器自动化

**数据模型校验和自动计算**:
```sql
-- 触发器函数
CREATE FUNCTION set_dataschema_checksum()

-- 触发器
CREATE TRIGGER set_public_dataschemas_checksum
  BEFORE INSERT OR UPDATE ON public.dataschemas
  FOR EACH ROW
  EXECUTE PROCEDURE set_dataschema_checksum();
```

## 环境变量

关键环境变量 (在 `.env` 和 `.dev.env` 中配置):

```bash
# Hasura 配置
HASURA_VERSION=v2.46.0
HASURA_GRAPHQL_ADMIN_SECRET=<admin_secret>
HASURA_GRAPHQL_JWT_SECRET=<jwt_secret>

# Actions 服务地址
ACTIONS_URL=http://actions:3000

# 数据库连接
POSTGRES_HOST=postgres
POSTGRES_PORT=5435
POSTGRES_DB=synmetrix
```

## 安全考虑

1. **JWT 验证**: 所有请求必须包含有效的 JWT token
2. **Admin Secret**: Hasura Console 访问需要 admin secret
3. **密码隐藏**: 数据源密码通过计算字段隐藏
4. **SQL 注入防护**: Hasura 自动参数化查询
5. **CORS 配置**: 限制允许的来源
6. **Rate Limiting**: 可配置请求速率限制

## 依赖关系

**上游依赖**:
- PostgreSQL (数据存储)
- Actions 服务 (业务逻辑)

**下游消费者**:
- Client 服务 (Web UI)
- 外部 API 客户端

## 性能优化

1. **查询缓存**: Hasura 自动缓存查询结果
2. **连接池**: 数据库连接池管理
3. **批量查询**: DataLoader 模式避免 N+1 查询
4. **索引优化**: 在常用查询字段上建立索引
5. **分页支持**: 支持 offset/limit 和 cursor-based 分页

## 监控与日志

1. **请求日志**: `request_logs` 和 `request_event_logs` 表记录所有查询
2. **Event Trigger 日志**: Hasura 自动记录触发器执行日志
3. **性能指标**: 记录查询持续时间和队列时间
4. **错误追踪**: 记录查询错误和堆栈信息

## 版本管理

- **Hasura 版本**: v2.46.0
- **元数据版本**: 在 `metadata/version.yaml` 中定义
- **迁移版本**: 基于时间戳的顺序迁移
- **向后兼容**: 迁移支持 up/down 回滚

## Hasura 与 Actions 层的交互机制

### 交互流程

```
用户请求 → Hasura GraphQL API → Actions Handler → 业务逻辑处理 → 返回结果
                ↓                      ↓
         1. JWT 验证           2. 转发请求到 Actions 服务
         2. 权限检查           3. 携带 session_variables
         3. 类型验证           4. 转发 client headers
```

### 实现细节

#### 1. Hasura 端配置 (actions.yaml)

```yaml
actions:
  - name: check_connection
    definition:
      kind: synchronous                         # 同步 action
      handler: '{{ACTIONS_URL}}/rpc/check_connection'  # HTTP 端点
      forward_client_headers: true              # 转发客户端 headers
    permissions:
      - role: user                              # 权限角色
```

**关键配置项**:
- `kind`: `synchronous` (同步) 或 `asynchronous` (异步)
- `handler`: Actions 服务的 HTTP endpoint (环境变量 `{{ACTIONS_URL}}`)
- `forward_client_headers`: 是否转发 JWT token 等客户端 headers
- `timeout`: 超时时间(默认 10 秒,长时间操作如 `fetch_tables` 设为 180 秒)
- `permissions`: 基于角色的访问控制

#### 2. Actions 服务端实现 (index.js)

**RPC 路由器** (服务发现模式):
```javascript
app.post("/rpc/:method", async (req, res) => {
  const { method } = req.params;
  const { session_variables: session, input } = req.body;

  // 动态加载处理函数 (hyphen-to-camelCase 转换)
  const modulePath = `./src/rpc/${hyphensToCamelCase(method)}.js`;
  const module = await import(modulePath);

  // 调用处理函数 (统一签名)
  const data = await module.default(session, input, req.headers);

  return res.json(data);
});
```

**请求体结构**:
```json
{
  "session_variables": {
    "x-hasura-user-id": "uuid",
    "x-hasura-role": "user"
  },
  "input": {
    "datasource_id": "uuid",
    "..."
  }
}
```

#### 3. 业务逻辑处理器 (统一接口)

**标准签名**:
```javascript
export default async (session, input, headers) => {
  const userId = session?.["x-hasura-user-id"];
  const { datasource_id } = input;

  // 业务逻辑...

  return result;
};
```

**示例: checkConnection.js**
```javascript
export default async (session, input, headers) => {
  const { datasource_id: dataSourceId } = input;
  const userId = session?.["x-hasura-user-id"];

  const result = await cubejsApi({
    dataSourceId,
    userId,
    authToken: headers?.authorization,  // 转发 JWT
  }).test();

  return result;
};
```

**示例: createTeam.js** (复杂业务逻辑)
```javascript
export default async (session, input) => {
  const userId = session?.["x-hasura-user-id"];

  try {
    // 1. 创建团队
    const newTeam = await createTeam({ userId, name });

    // 2. 添加团队成员
    await createTeamMember({ userId, teamId, role: OWNER_ROLE });

    // 3. 迁移用户资产到团队
    await fetchGraphQL(updateAssetsMutation, { teamId, userId });

    return newTeam;
  } catch (err) {
    // 4. 回滚操作
    if (newTeam?.id) {
      await fetchGraphQL(deleteTeamMutation, { id: newTeam.id });
    }
    return apiError(err);
  }
};
```

#### 4. 回调 Hasura (双向通信)

Actions 服务可以通过 GraphQL 客户端回调 Hasura:

```javascript
// graphql.js
export const fetchGraphQL = async (query, variables, authToken) => {
  const headers = authToken
    ? { authorization: `Bearer ${authToken}` }      // 使用用户 token
    : { "x-hasura-admin-secret": ADMIN_SECRET };    // 或使用 admin 权限

  const result = await fetch(HASURA_ENDPOINT, {
    method: "POST",
    body: JSON.stringify({ query, variables }),
    headers,
  });

  return parseResponse(result);
};
```

**权限模式**:
- **用户 token**: 继承用户权限,受 RLS 限制
- **Admin secret**: 绕过所有权限检查(谨慎使用)

### 交互模式对比

#### 模式 1: Hasura Actions (当前实现) ✅

```
Client → Hasura (GraphQL) → Actions (REST) → Business Logic
         ↓                    ↓
      权限检查            调用 Cube.js/外部服务
      类型验证            数据转换
```

**优点**:
- ✅ 统一 GraphQL 接口
- ✅ 权限在 Hasura 层集中管理
- ✅ 类型安全(GraphQL schema 验证)
- ✅ 自动转发 session variables
- ✅ 支持同步/异步模式
- ✅ 错误处理标准化

**缺点**:
- ⚠️ 多一层 HTTP 调用(轻微延迟)
- ⚠️ Actions 服务需要单独维护

#### 模式 2: Remote Schemas (备选方案)

```
Client → Hasura → Remote GraphQL Service
         ↓
      Schema Stitching
      Remote Joins
```

**适用场景**:
- 已有 GraphQL 服务
- 需要跨服务 join
- 复杂的微服务架构

#### 模式 3: Event Triggers (异步)

```
Database Change → Event Trigger → Webhook
                   ↓
              可靠投递
              重试机制
```

**适用场景**:
- 数据变更后处理
- 异步任务(发邮件、生成报告)
- 不阻塞用户请求

### Synmetrix 的设计决策分析

#### ✅ 符合最佳实践的方面

1. **职责分离清晰**
   - Hasura: API 网关 + 权限控制 + 数据 CRUD
   - Actions: 自定义业务逻辑 + 外部服务集成
   - 符合 Hasura 官方推荐的"命令与查询分离"模式

2. **统一的接口契约**
   ```javascript
   (session, input, headers) => Promise<result>
   ```
   - 所有 RPC 方法签名一致
   - 便于测试和维护
   - 符合函数式编程范式

3. **动态模块加载**
   ```javascript
   const module = await import(`./src/rpc/${method}.js`);
   ```
   - 约定优于配置(convention over configuration)
   - 新增 action 只需添加文件,无需修改路由
   - 支持热重载(开发模式)

4. **错误处理一致性**
   ```javascript
   return apiError({ code, message });
   ```
   - 统一错误格式
   - 便于前端解析和展示

5. **安全性考虑**
   - JWT token 透传
   - Session variables 自动注入
   - 支持双权限模式(user token vs admin secret)

6. **同步/异步混合模式**
   - 快速操作用同步 action
   - 长时间操作(fetch_tables: 180s timeout)
   - 后台任务用异步 action (create_events)

#### ⚠️ 可以改进的方面

1. **缺少 API 版本控制**
   ```javascript
   // 当前: /rpc/check_connection
   // 建议: /rpc/v1/check_connection
   ```

2. **错误处理可以更细粒度**
   ```javascript
   // 当前: 返回 400 + error object
   // 建议: 区分 4xx vs 5xx,支持错误码枚举
   ```

3. **缺少请求验证中间件**
   ```javascript
   // 建议: 在 Actions 层添加输入验证
   import { validateInput } from './validators';
   ```

4. **缺少 API 文档生成**
   - 建议: 添加 JSDoc 注释
   - 生成 OpenAPI/Swagger 文档

5. **监控和追踪可以增强**
   ```javascript
   // 当前: 基础日志
   // 建议: 添加分布式追踪(OpenTelemetry)
   ```

6. **Event Triggers 使用不够充分**
   - 当前主要用于 cron 任务管理
   - 可用于更多数据一致性检查和自动化流程

### Hasura 官方最佳实践对比

根据 Hasura 官方文档,推荐的业务逻辑集成方式:

| 场景 | 推荐方案 | Synmetrix 实现 | 评价 |
|------|---------|---------------|------|
| 数据验证 | Actions | ✅ Actions | 符合 |
| REST API 集成 | Actions | ✅ Actions (Cube.js) | 符合 |
| 外部数据源查询 | Query Actions | ✅ fetch_meta, fetch_tables | 符合 |
| 复杂事务逻辑 | Mutation Actions | ✅ createTeam, invite_member | 符合 |
| 异步后台任务 | Event Triggers | ✅ 用于告警/报告 | 符合 |
| 已有 GraphQL 服务 | Remote Schemas | ❌ 未使用 | 不适用 |
| 数据库级逻辑 | Postgres Functions | ⚠️ 仅用于计算字段 | 可扩展 |

### 推荐的架构模式

Hasura 官方推荐的"命令与查询"架构:

```
Commands (写操作)              Queries (读操作)
     ↓                            ↓
Actions (业务逻辑)         Hasura (直接查询)
     ↓                            ↓
Event Triggers (异步)      Computed Fields
     ↓
后台处理
```

**Synmetrix 的实现**:
- ✅ Commands: 使用 Mutation Actions (gen_dataschemas, create_team, etc.)
- ✅ Queries: 混合模式(部分用 Query Actions, 部分直接查询)
- ✅ Event Triggers: 用于定时任务和文档生成
- ⚠️ Computed Fields: 仅用于密码隐藏和持续时间计算

### 性能考虑

#### 当前架构的性能特征

**请求链路**:
```
Client → Hasura (8080) → Actions (3000) → Cube.js (4000) → Database
   50ms      +10ms          +50ms           +200ms
```

**优化策略**:
1. **内网通信**: 服务间使用 Docker 内部网络(低延迟)
2. **连接池**: Actions 服务复用 HTTP 连接
3. **并发处理**: Express.js 异步处理,支持高并发
4. **缓存**:
   - Hasura 查询缓存
   - Cube.js 预聚合缓存

#### Actions vs 直接暴露 Cube.js 的权衡

**为什么不直接暴露 Cube.js API?**

1. **统一认证**: Hasura 统一处理 JWT 验证
2. **权限控制**: 基于 Hasura RLS,细粒度权限
3. **类型安全**: GraphQL schema 提供编译时检查
4. **业务逻辑**: Actions 层处理复杂的编排逻辑
5. **协议转换**: GraphQL ↔ REST 转换
6. **错误标准化**: 统一错误格式

**这种分层的价值**:
- 前端只需对接单一的 GraphQL API
- 更换底层实现不影响 API 契约
- 便于添加中间件(限流、监控、日志)

## 总结

### Hasura 服务的核心价值

Hasura 服务是 Synmetrix 的 **API 网关和权限中枢**,通过声明式配置实现了:

1. **快速 API 开发**: 无需手写 CRUD 代码
2. **细粒度权限**: 行级安全自动执行
3. **事件驱动**: 数据变更自动触发业务流程
4. **类型安全**: GraphQL schema 提供强类型契约
5. **多租户支持**: 基于关系的数据隔离

### Hasura-Actions 交互模式评价

**符合 Hasura 官方最佳实践**: ⭐⭐⭐⭐⭐ (5/5)

**优点**:
- ✅ 职责分离清晰
- ✅ 统一接口契约
- ✅ 动态模块加载(约定优于配置)
- ✅ 安全性考虑周全
- ✅ 同步/异步混合模式
- ✅ 错误处理一致性

**可改进项**:
- ⚠️ 增加 API 版本控制
- ⚠️ 更细粒度的错误分类
- ⚠️ 输入验证中间件
- ⚠️ 更充分利用 Event Triggers
- ⚠️ 增加分布式追踪

这种架构设计大幅降低了后端开发复杂度,让团队可以专注于业务逻辑实现(在 Actions 服务中),而将通用的 API 层、权限控制和事件编排交给 Hasura 处理。是一个成熟、可扩展的生产级架构。
