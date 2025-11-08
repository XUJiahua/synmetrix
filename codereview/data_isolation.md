# Synmetrix 数据隔离机制总结

## 概述

Synmetrix 的 Cube.js 服务实现了**多层级、多维度的数据隔离机制**，确保不同用户、不同数据源之间的数据安全隔离。当前实现主要在**数据源层面**、**Schema 层面**和**权限层面**进行隔离。

## 隔离架构概览

```
┌─────────────────────────────────────────────────────────────────┐
│                         HTTP Request                             │
│  Headers: Authorization, x-hasura-datasource-id, etc.           │
└──────────────────────┬──────────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────────┐
│  Step 1: 认证与授权 (checkAuth.js)                               │
│  - JWT Token 验证                                                 │
│  - 提取 userId, dataSourceId, branchId                           │
│  - 查询用户权限和数据源列表                                        │
└──────────────────────┬──────────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────────┐
│  Step 2: 用户作用域定义 (defineUserScope.js)                     │
│  - 验证用户对数据源的访问权限                                      │
│  - 获取用户角色 (owner/admin/member)                              │
│  - 提取 dataSourceAccessList (cube 级别的权限)                    │
└──────────────────────┬──────────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────────┐
│  Step 3: 安全上下文构建 (buildSecurityContext.js)                │
│  - dataSourceVersion = SHA256(dataSourceId + dbType + dbParams) │
│  - schemaVersion = MD5(schemaFileIds)                            │
│  - preAggregationSchema = MD5(dataSourceId)                      │
└──────────────────────┬──────────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────────┐
│  Step 4: Orchestrator 隔离 (index.js)                            │
│  - orchestratorId = dataSourceVersion + schemaVersion            │
│  - 每个数据源版本使用独立的 Cube.js 实例                           │
│  - 独立的 pre-aggregation schema                                 │
└──────────────────────┬──────────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────────┐
│  Step 5: Schema 动态加载 (repositoryFactory.js)                  │
│  - 根据 securityContext 加载对应的 schema 文件                    │
│  - Schema 从数据库读取，按 branch/version 隔离                    │
└──────────────────────┬──────────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────────┐
│  Step 6: 查询重写与权限检查 (queryRewrite.js)                     │
│  - owner/admin: 完全访问                                          │
│  - member: 检查 dataSourceAccessList                              │
│  - 验证查询中的每个 cube/dimension/measure 是否有权限              │
└──────────────────────┬──────────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────────┐
│  Step 7: 数据库连接隔离 (driverFactory.js)                        │
│  - 根据 dataSource 配置创建独立的数据库连接                         │
│  - 不同数据源使用不同的 db_params                                  │
└──────────────────────┴──────────────────────────────────────────┘
```

## 隔离机制详解

### 1. 数据源隔离（Data Source Isolation）

**实现位置**: `services/cubejs/index.js:47-51`

```javascript
const contextToOrchestratorId = ({ securityContext }) =>
  `CUBEJS_APP_${securityContext?.userScope?.dataSource?.dataSourceVersion}_${securityContext?.userScope?.dataSource?.schemaVersion}`;

const contextToAppId = ({ securityContext }) =>
  `CUBEJS_APP_${securityContext?.userScope?.dataSource?.dataSourceVersion}_${securityContext?.userScope?.dataSource?.schemaVersion}`;
```

**隔离原理**:
- 每个数据源配置的唯一组合（`db_type` + `db_params`）生成唯一的 `dataSourceVersion` (SHA256)
- Cube.js 为每个 `dataSourceVersion` 创建独立的 **Orchestrator 实例**
- 不同数据源之间完全隔离，包括：
  - 独立的查询缓存
  - 独立的编译缓存
  - 独立的数据库连接池

**版本计算**: `services/cubejs/src/utils/buildSecurityContext.js:35`

```javascript
const dataSourceVersion = JSum.digest({
  dataSourceId: dataSource.id,
  dbType: dataSource.db_type?.toLowerCase(),
  dbParams: dataSource.db_params
}, "SHA256", "hex");
```

**隔离效果**:
- 用户 A 访问 PostgreSQL 数据源 → Orchestrator Instance #1
- 用户 B 访问 ClickHouse 数据源 → Orchestrator Instance #2
- 用户 A 和 B 的查询互不影响，完全隔离

---

### 2. Schema 版本隔离（Schema Version Isolation）

**实现位置**: `services/cubejs/index.js:53-54`, `buildSecurityContext.js:45-46`

```javascript
const schemaVersion = ({ securityContext }) =>
  securityContext?.userScope?.dataSource?.schemaVersion;

// 计算方式
const schemaVersion = createMd5Hex(files); // files 是 schema 文件 ID 列表
```

**隔离原理**:
- 基于 schema 文件列表（dataschemas 的 ID）计算 `schemaVersion` (MD5)
- 同一数据源的不同 schema 版本（branch/version）使用不同的 `schemaVersion`
- Schema 版本变化时，`orchestratorId` 也会变化，触发重新编译

**动态加载**: `services/cubejs/src/utils/repositoryFactory.js:16-20`

```javascript
dataSchemaFiles: async () => {
  const ids = securityContext?.userScope?.dataSource?.files;
  const dataSchemas = await findDataSchemasByIds({ ids });
  return dataSchemas.map(mapSchemaToFile);
}
```

**隔离效果**:
- 同一数据源，main branch → Schema Version A → Orchestrator #1
- 同一数据源，dev branch → Schema Version B → Orchestrator #2
- 不同 branch 的查询使用不同的 schema 定义，互不干扰

---

### 3. 预聚合隔离（Pre-Aggregation Isolation）

**实现位置**: `services/cubejs/index.js:56-57`

```javascript
const preAggregationsSchema = ({ securityContext }) =>
  `pre_aggregations_${securityContext?.userScope?.dataSource?.preAggregationSchema}`;
```

**隔离原理**:
- 每个数据源有独立的 pre-aggregation schema 名称
- 基于 `dataSourceId` 计算 MD5 生成唯一的 schema 后缀
- 预聚合表存储在 Cubestore 中，按 schema 隔离

**计算方式**: `buildSecurityContext.js:46`

```javascript
const preAggregationSchema = createMd5Hex(data.dataSourceId);
```

**隔离效果**:
- DataSource A → `pre_aggregations_abc123` schema
- DataSource B → `pre_aggregations_def456` schema
- 不同数据源的预聚合数据物理隔离

---

### 4. 权限级别隔离（Role-Based Access Control）

**实现位置**: `services/cubejs/src/utils/queryRewrite.js:22-47`

```javascript
const queryRewrite = async (query, { securityContext }) => {
  const { userScope } = securityContext;
  const { dataSourceAccessList, role } = userScope;

  // owner/admin 有完全访问权限
  if (["owner", "admin"].includes(role)) {
    return query;
  }

  // 检查是否有数据源访问权限
  if (!dataSourceAccessList) {
    throw new Error("403: You have no access to the datasource");
  }

  // 检查查询中的每个字段是否在访问列表中
  queryNames.forEach((cn) => {
    if (!accessNames.includes(cn)) {
      throw new Error(`403: You have no access to "${cn}" cube property`);
    }
  });

  return query;
};
```

**角色定义**:
1. **owner**: 数据源所有者，完全访问权限
2. **admin**: 团队管理员，完全访问权限
3. **member**: 普通成员，受 `dataSourceAccessList` 限制

**访问列表获取**: `defineUserScope.js:78-82`

```javascript
const dataSourceAccessList = getDataSourceAccessList(
  allMembers,
  selectedDataSourceId,
  dataSource.team_id
);
```

**访问列表结构**:
```javascript
{
  role: "member",
  dataSourceAccessList: {
    "Users": {
      dimensions: ["Users.id", "Users.name", "Users.email"],
      measures: ["Users.count"],
      segments: []
    },
    "Orders": {
      dimensions: ["Orders.id", "Orders.status"],
      measures: ["Orders.count", "Orders.totalAmount"],
      segments: []
    }
  }
}
```

**隔离效果**:
- Owner/Admin 可以查询所有 cube 和字段
- Member 只能查询 `access_list` 中允许的 cube 和字段
- 未授权的查询直接抛出 403 错误

---

### 5. 用户身份隔离（User Authentication & Authorization）

**实现位置**: `services/cubejs/src/utils/checkAuth.js:19-85`

**认证流程**:

```javascript
// 1. 提取并验证 JWT Token
const authHeader = req.headers.authorization;
const authToken = authHeader.split(" ")[1];
const jwtDecoded = jwt.verify(authToken, JWT_KEY, {
  algorithms: [JWT_ALGORITHM],
});

// 2. 提取用户 ID 和数据源信息
const { "x-hasura-user-id": userId } = jwtDecoded?.hasura || {};
const dataSourceId = req.headers["x-hasura-datasource-id"];
const branchId = req.headers["x-hasura-branch-id"];
const branchVersionId = req.headers["x-hasura-branch-version-id"];

// 3. 查询用户权限
const user = await findUser({ userId });

// 4. 构建安全上下文
const userScope = defineUserScope(
  user.dataSources,
  user.members,
  dataSourceId,
  branchId,
  branchVersionId
);

// 5. 设置到请求对象
req.securityContext = {
  authToken,
  userId,
  userScope,
};
```

**权限验证**: `defineUserScope.js:33-38`

```javascript
const dataSource = allDataSources.find(
  (source) => source.id === selectedDataSourceId
);

if (!dataSource) {
  throw new Error(`404: source "${selectedDataSourceId}" not found`);
}
```

**隔离效果**:
- 每个请求必须携带有效的 JWT Token
- 用户只能访问其有权限的数据源
- 无权限的数据源访问直接返回 404 错误

---

### 6. 分支版本隔离（Branch & Version Isolation）

**实现位置**: `defineUserScope.js:44-76`

**分支选择逻辑**:

```javascript
// 1. 如果指定了 branchId，使用指定分支
if (selectedBranchId) {
  const branch = dataSource.branches.find(
    (branch) => branch.id === selectedBranchId
  );
  if (!branch) {
    throw new Error(`404: branch "${selectedBranchId}" not found`);
  }
  selectedBranch = branch;
}
// 2. 否则使用默认分支（status = 'active'）
else {
  const defaultBranch = dataSource.branches.find(
    (branch) => branch.status === "active"
  );
  if (!defaultBranch) {
    throw new Error(`400: default branch not found`);
  }
  selectedBranch = defaultBranch;
}

// 3. 版本选择（可选）
if (selectedVersionId) {
  const version = selectedBranch.versions.find(
    (version) => version.id === selectedVersionId
  );
  if (!version) {
    throw new Error(`404: version "${selectedVersionId}" not found`);
  }
  selectedVersion = version;
}
```

**Schema 加载优先级**: `buildSecurityContext.js:37-41`

```javascript
const dataModels =
  version?.dataschemas ||                              // 1. 指定版本的 schemas
  branch?.versions?.[0]?.dataschemas ||                // 2. 分支最新版本的 schemas
  dataSource.branches?.[0]?.versions?.[0]?.dataschemas || // 3. 默认分支的最新版本
  [];
```

**隔离效果**:
- 开发分支（dev branch）和生产分支（main branch）使用不同的 schema
- 同一分支的不同版本（version）可以共存
- 用户可以查询历史版本的数据（通过指定 versionId）

---

### 7. 数据库连接隔离（Database Driver Isolation）

**实现位置**: `services/cubejs/src/utils/driverFactory.js`

```javascript
import ServerCore from "@cubejs-backend/server-core";
import prepareDbParams from "./prepareDbParams.js";

const driverFactory = async ({ securityContext }) => {
  const { dbType, dbParams } = securityContext.userScope.dataSource;

  const params = prepareDbParams(dbParams, dbType);

  return ServerCore.createDriver(dbType, params);
};
```

**参数准备**: `prepareDbParams.js`

根据不同的数据库类型（PostgreSQL, MySQL, ClickHouse 等），转换和准备连接参数。

**隔离效果**:
- 每个数据源使用独立的数据库连接配置
- 不同用户访问不同的数据库实例
- 连接参数（host, port, database, credentials）完全隔离

---

## 隔离层级总结

| 隔离层级 | 隔离维度 | 实现方式 | 隔离粒度 |
|---------|---------|---------|---------|
| **数据源隔离** | 数据库实例 | `contextToOrchestratorId` 基于 `dataSourceVersion` | 数据源级 |
| **Schema 隔离** | Schema 版本 | `contextToOrchestratorId` 基于 `schemaVersion` | Branch/Version 级 |
| **预聚合隔离** | 预聚合表 | `preAggregationsSchema` 基于 `dataSourceId` | 数据源级 |
| **权限隔离** | Cube/字段访问 | `queryRewrite` 检查 `dataSourceAccessList` | Cube/字段级 |
| **用户隔离** | 用户身份 | JWT Token + `checkAuth` | 用户级 |
| **分支隔离** | 开发/生产环境 | `defineUserScope` 选择 branch/version | Branch/Version 级 |
| **连接隔离** | 数据库连接 | `driverFactory` 独立连接配置 | 数据源级 |

---

## 隔离流程示例

### 场景 1: 两个用户访问不同的数据源

```
User A:
  - JWT Token → userId: "user-a"
  - x-hasura-datasource-id: "datasource-postgres"
  - PostgreSQL 数据源，team_id: "team-1"

User B:
  - JWT Token → userId: "user-b"
  - x-hasura-datasource-id: "datasource-clickhouse"
  - ClickHouse 数据源，team_id: "team-2"
```

**隔离效果**:
1. **Orchestrator 隔离**:
   - User A → `CUBEJS_APP_<postgres-hash>_<schema-hash-a>`
   - User B → `CUBEJS_APP_<clickhouse-hash>_<schema-hash-b>`
2. **Schema 隔离**: 加载不同的 dataschemas
3. **预聚合隔离**:
   - User A → `pre_aggregations_<postgres-id>`
   - User B → `pre_aggregations_<clickhouse-id>`
4. **数据库连接**: 连接到不同的数据库实例

---

### 场景 2: 两个用户访问同一数据源的不同分支

```
User A:
  - userId: "user-a"
  - x-hasura-datasource-id: "datasource-1"
  - x-hasura-branch-id: "main-branch"
  - role: "owner"

User B:
  - userId: "user-b"
  - x-hasura-datasource-id: "datasource-1"
  - x-hasura-branch-id: "dev-branch"
  - role: "member"
```

**隔离效果**:
1. **Orchestrator 隔离**:
   - User A → `CUBEJS_APP_<same-datasource>_<main-schema>`
   - User B → `CUBEJS_APP_<same-datasource>_<dev-schema>`
2. **Schema 隔离**:
   - User A 加载 main branch 的 schemas
   - User B 加载 dev branch 的 schemas
3. **权限隔离**:
   - User A (owner) 可以查询所有 cube
   - User B (member) 只能查询 access_list 中的 cube

---

### 场景 3: 同一用户的不同请求

```
Request 1:
  - x-hasura-datasource-id: "datasource-1"
  - x-hasura-branch-id: "main-branch"
  - Query: { measures: ["Users.count"] }

Request 2:
  - x-hasura-datasource-id: "datasource-1"
  - x-hasura-branch-id: "dev-branch"
  - Query: { measures: ["Users.count"] }
```

**隔离效果**:
- 两个请求使用不同的 Orchestrator 实例（因为 schemaVersion 不同）
- main-branch 和 dev-branch 的 schema 定义可能不同
- 查询结果可能不同（基于不同的 schema 定义）

---

## 数据隔离的安全性分析

### ✅ 已实现的安全保障

1. **多租户隔离**:
   - 不同团队（team）的数据源完全隔离
   - 通过 JWT Token 和 team_id 验证用户身份

2. **数据源级别隔离**:
   - 不同数据源使用独立的 Orchestrator 实例
   - 查询缓存、编译缓存、预聚合完全隔离

3. **Schema 版本控制**:
   - 支持多分支、多版本的 schema 管理
   - 开发环境和生产环境隔离

4. **细粒度权限控制**:
   - Cube 级别的访问控制
   - 字段级别的访问控制（dimensions, measures, segments）

5. **数据库连接隔离**:
   - 每个数据源使用独立的数据库连接配置
   - 防止跨数据源的数据泄露

---

### ⚠️ 未实现的隔离机制

1. **行级数据隔离（Row-Level Security）**:
   - ❌ 无法实现同一张表根据字段值（如 `tenant_id`, `user_id`）过滤数据
   - ❌ 多租户共享同一张表时，需要在应用层手动过滤
   - 📖 解决方案：参考 `codereview/Row-Level-Security-Implementation.md`

2. **列级数据隔离（Column-Level Security）**:
   - ❌ 无法对同一 cube 的不同用户隐藏特定列
   - ✅ 部分实现：通过 `dataSourceAccessList` 可以限制访问的 dimensions/measures

3. **数据脱敏（Data Masking）**:
   - ❌ 敏感字段（如手机号、邮箱、身份证）无自动脱敏
   - 需要在 schema 中手动定义脱敏逻辑

4. **动态数据过滤（Context-Based Filtering）**:
   - ❌ 无法根据用户上下文（如部门、地区）动态过滤数据
   - 需要在 schema 的 SQL 中硬编码过滤条件

---

## 数据隔离的性能影响

### 性能优势

1. **独立缓存**:
   - 不同数据源的查询缓存独立
   - 避免缓存污染和冲突

2. **独立编译**:
   - Schema 编译缓存独立
   - 一个数据源的 schema 变更不影响其他数据源

3. **预聚合隔离**:
   - 预聚合表按数据源隔离
   - 查询性能不受其他数据源影响

### 性能开销

1. **多实例开销**:
   - 每个数据源版本创建独立的 Orchestrator 实例
   - 内存开销：每个实例约 50-100MB
   - CPU 开销：多个实例并发编译时占用更多 CPU

2. **连接池开销**:
   - 每个数据源维护独立的数据库连接池
   - 连接数：默认每个连接池 2-5 个连接

3. **缓存碎片化**:
   - Redis 缓存按 orchestratorId 分片
   - 缓存命中率可能降低（相比全局缓存）

### 性能优化建议

1. **限制 Orchestrator 数量**:
   - 设置最大实例数（如 100 个）
   - LRU 策略淘汰不活跃的实例

2. **共享连接池**:
   - 相同 `dataSourceVersion` 的实例共享连接池
   - 减少数据库连接数

3. **预聚合缓存**:
   - 为常用查询配置 pre-aggregations
   - 减少实时查询压力

---

## 配置与调优

### 环境变量

```bash
# JWT 配置
JWT_KEY=your-secret-key
JWT_ALGORITHM=HS256

# Cube.js 配置
CUBEJS_SECRET=your-cubejs-secret
CUBEJS_TELEMETRY=false
CUBEJS_SCHEDULED_REFRESH=true
CUBEJS_REFRESH_TIMER=60

# SQL API 配置
CUBEJS_SQL_API=true
CUBEJS_SQL_PORT=13306
CUBEJS_PG_SQL_PORT=15432

# Cubestore 配置
CUBEJS_CUBESTORE_HOST=cubestore
CUBEJS_CUBESTORE_PORT=3030
```

### Hasura 权限配置

数据源访问权限存储在 `public.access_lists` 表中：

```sql
SELECT
  ar.id,
  ar.team_role,
  al.config
FROM public.access_roles ar
JOIN public.access_lists al ON ar.access_list_id = al.id
WHERE ar.team_role = 'member';
```

配置示例：

```json
{
  "datasources": {
    "715dfae1-1044-42ec-ac48-dd4cefa567e6": {
      "cubes": {
        "Users": {
          "dimensions": ["Users.id", "Users.name", "Users.email"],
          "measures": ["Users.count"],
          "segments": []
        },
        "Orders": {
          "dimensions": ["Orders.id", "Orders.status"],
          "measures": ["Orders.count"],
          "segments": []
        }
      }
    }
  }
}
```

---

## 安全审计与日志

### 审计点

1. **认证审计**:
   - JWT Token 验证失败
   - 用户访问未授权的数据源

2. **权限审计**:
   - 用户查询未授权的 cube/字段
   - 403 错误日志

3. **数据访问审计**:
   - 记录所有查询的 securityContext
   - 包括 userId, dataSourceId, branchId

### 日志记录

日志工具：`services/cubejs/src/utils/logging.js`

```javascript
import { logging } from "./src/utils/logging.js";

// 在 checkAuth 中记录认证信息
logging.info('Authentication', {
  userId,
  dataSourceId,
  branchId,
  role: userScope.role
});

// 在 queryRewrite 中记录权限检查
logging.warn('Access Denied', {
  userId,
  cube: deniedCube,
  field: deniedField
});
```

---

## 测试验证

### 单元测试

```bash
cd services/cubejs
npm test

# 关键测试文件
# - __tests__/checkAuth.test.js
# - __tests__/defineUserScope.test.js
# - __tests__/queryRewrite.test.js
```

### 集成测试

```bash
# 使用 stepci 测试隔离机制
cd services/tests
./cli.sh tests stepci

# 测试场景
# - 不同用户访问不同数据源
# - 同一用户访问不同分支
# - 权限限制测试（member vs owner）
```

### 手动测试

```bash
# 场景 1: 测试数据源隔离
curl -X POST http://localhost:4000/api/v1/load \
  -H "Authorization: Bearer <jwt-token>" \
  -H "x-hasura-datasource-id: datasource-1" \
  -H "Content-Type: application/json" \
  -d '{
    "query": {
      "measures": ["Users.count"]
    }
  }'

# 场景 2: 测试分支隔离
curl -X POST http://localhost:4000/api/v1/load \
  -H "Authorization: Bearer <jwt-token>" \
  -H "x-hasura-datasource-id: datasource-1" \
  -H "x-hasura-branch-id: dev-branch" \
  -H "Content-Type: application/json" \
  -d '{
    "query": {
      "measures": ["Users.count"]
    }
  }'

# 场景 3: 测试权限限制（应该返回 403）
curl -X POST http://localhost:4000/api/v1/load \
  -H "Authorization: Bearer <member-jwt-token>" \
  -H "x-hasura-datasource-id: datasource-1" \
  -H "Content-Type: application/json" \
  -d '{
    "query": {
      "measures": ["SecretData.count"]
    }
  }'
```

---

## 相关文档

- **行级数据隔离实现**: `codereview/Row-Level-Security-Implementation.md`
- **Cube.js 官方文档**: https://cube.dev/docs/security
- **多租户架构模式**: https://docs.aws.amazon.com/whitepapers/latest/saas-architecture-fundamentals/multi-tenancy-patterns.html

---

## 总结

Synmetrix 目前实现了以下隔离机制：

✅ **数据源级别隔离** - 不同数据源使用独立的 Cube.js 实例
✅ **Schema 版本隔离** - 支持多分支、多版本的 schema 管理
✅ **预聚合隔离** - 预聚合数据按数据源隔离存储
✅ **权限级别隔离** - Cube 和字段级别的访问控制
✅ **用户身份隔离** - JWT Token 认证和授权
✅ **分支版本隔离** - 开发和生产环境分离
✅ **数据库连接隔离** - 独立的数据库连接配置

❌ **未实现的隔离**:
- 行级数据隔离（Row-Level Security）
- 动态数据过滤（Context-Based Filtering）
- 数据脱敏（Data Masking）

这些隔离机制确保了在**表结构层面**的数据安全隔离，为多租户、多数据源场景提供了强大的支持。如需实现更细粒度的**行级数据隔离**，请参考 `Row-Level-Security-Implementation.md`。
