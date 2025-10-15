# Cubejs 服务依赖的其他服务分析

根据代码分析，**cubejs** 服务与以下其他服务进行交互：

## 1. Cubestore (必需)

**用途**: 分布式缓存和查询引擎

**连接方式**:
- Host: `CUBEJS_CUBESTORE_HOST=cubestore`
- Port: `CUBEJS_CUBESTORE_PORT=3030`

**代码位置**: `services/cubejs/index.js:49-53`

**功能**:
- 作为外部数据库驱动 (`externalDbType: "cubestore"`)
- 存储预聚合数据 (`cacheAndQueueDriver: "cubestore"`)
- 每个数据源隔离的预聚合 schema: `pre_aggregations_{dataSourceId}`

## 2. Hasura (必需)

**用途**: GraphQL API 层，存储元数据和用户权限

**连接方式**:
- Endpoint: `HASURA_ENDPOINT=http://hasura:8080/v1/graphql`
- 认证: `HASURA_GRAPHQL_ADMIN_SECRET`

**代码位置**:
- `services/cubejs/src/utils/graphql.js:3-4` - GraphQL 客户端
- `services/cubejs/src/utils/dataSourceHelpers.js` - 所有数据查询

**功能**:
- 用户认证和权限验证
- 存储数据源配置 (datasources)
- 存储数据模型 schema (dataschemas)
- 存储分支和版本信息 (branches, versions)
- SQL 凭证管理 (sql_credentials)

**主要 GraphQL 查询**:
- `userQuery` - 获取用户及其数据源
- `sourcesQuery` - 获取所有数据源
- `sqlCredentialsQuery` - SQL API 认证
- `dataschemasQuery` - 获取数据模型定义
- `upsertVersionMutation` - 创建新版本

## 3. Redis (可选)

**用途**: 缓存层

**连接方式**:
- Address: `REDIS_ADDR=redis://redis:6379`

**代码位置**: `services/cubejs/src/utils/redis.js:1-16`

**功能**:
- 提供可选的缓存支持
- 如果 `REDIS_ADDR` 未设置，服务仍可运行（`redisClient` 为 null）
- 使用 ioredis 客户端连接

## 4. PostgreSQL (间接依赖)

**用途**: 主数据库，通过 Hasura 间接访问

**连接方式**:
- `POSTGRES_ADDR=postgres://user:pg_pass@postgres/db`
- Port: 5435 (外部) → 5432 (容器内)

**功能**:
- 存储 Hasura 的所有元数据
- 不直接被 cubejs 访问，而是通过 Hasura GraphQL API
- 存储用户、数据源、schema、权限等信息

## 5. 用户配置的数据源 (动态)

**用途**: 实际的分析数据库

**代码位置**:
- `services/cubejs/src/utils/driverFactory.js` - 动态创建数据库驱动
- `services/cubejs/src/utils/prepareDbParams.js` - 转换数据源配置

**支持的数据库** (package.json:11-37):
- **关系型**: PostgreSQL, MySQL, MSSQL
- **云数据仓库**: BigQuery, Snowflake, Redshift, Athena, Databricks
- **OLAP**: ClickHouse, Druid, DuckDB
- **其他**: Elasticsearch, MongoDB, Vertica, Trino, Presto
- 等 20+ 种数据库驱动

**功能**:
- 根据用户在 Hasura 中配置的 datasource 动态连接
- 每个数据源有独立的 Cube.js orchestrator 实例
- 支持多租户数据源隔离

## 服务交互流程

```
1. 客户端请求 → Cubejs (带 JWT token + x-hasura-datasource-id header)
   ↓
2. Cubejs → JWT 验证 (checkAuth.js)
   ↓
3. Cubejs → Hasura GraphQL (查询用户权限和数据源配置)
   - fetchGraphQL(userQuery, { userId })
   - 获取 datasources, members, access_list
   ↓
4. Cubejs → 动态加载 Schema (从 Hasura 获取 dataschemas)
   - repositoryFactory.dataSchemaFiles()
   - findDataSchemasByIds({ ids })
   ↓
5. Cubejs → 创建数据库驱动 (连接用户配置的数据源)
   - driverFactory({ securityContext, dataSource })
   - 根据 dbType 和 dbParams 创建相应驱动
   ↓
6. Cubejs → Cubestore (缓存和预聚合)
   - externalDriverFactory() 创建 cubestore 驱动
   - 预聚合存储在隔离的 schema 中
   ↓
7. Cubejs ↔ Redis (可选缓存)
   - 缓存查询结果和元数据
   ↓
8. Cubejs → 返回查询结果
```

## 关键架构特点

### 多租户隔离
```javascript
// services/cubejs/index.js:37-47
const contextToOrchestratorId = ({ securityContext }) =>
  `CUBEJS_APP_${securityContext?.userScope?.dataSource?.dataSourceVersion}_${securityContext?.userScope?.dataSource?.schemaVersion}`;

const contextToAppId = ({ securityContext }) =>
  `CUBEJS_APP_${securityContext?.userScope?.dataSource?.dataSourceVersion}_${securityContext?.userScope?.dataSource?.schemaVersion}`;

const preAggregationsSchema = ({ securityContext }) =>
  `pre_aggregations_${securityContext?.userScope?.dataSource?.preAggregationSchema}`;
```
- 每个数据源版本有独立的 orchestrator ID
- 每个数据源有独立的预聚合 schema
- Schema 版本控制支持

### 动态 Schema 加载
```javascript
// services/cubejs/src/utils/repositoryFactory.js
const repositoryFactory = ({ securityContext }) => {
  return {
    dataSchemaFiles: async () => {
      const ids = securityContext?.userScope?.dataSource?.files;
      const dataSchemas = await findDataSchemasByIds({ ids });
      return dataSchemas.map(mapSchemaToFile);
    },
  };
};
```
- Schema 存储在数据库中，不是文件系统
- 通过 Hasura GraphQL API 动态获取
- 支持版本控制和分支管理

### 安全上下文流程
```javascript
// services/cubejs/src/utils/checkAuth.js:19-85
1. 提取 JWT token 和 headers (datasource-id, branch-id)
2. 验证 JWT signature (jwt.verify)
3. 从 Hasura 查询用户信息和权限
4. 构建 securityContext:
   - authToken
   - userId
   - userScope (dataSource, branch, files, permissions)
```
- 通过 JWT + Hasura 实现细粒度权限控制
- 每个请求都携带安全上下文
- 支持团队级别的访问控制

### SQL API
- **MySQL 协议**: Port 13306 (`CUBEJS_SQL_PORT`)
- **PostgreSQL 协议**: Port 15432 (`CUBEJS_PG_SQL_PORT`)
- SQL 认证通过 `checkSqlAuth.js` 处理
- 支持 SQL 客户端直接连接查询

## 暴露的 API 端点

### 1. 自定义路由 (在 app.use(routes()) 中注册)

**代码位置**: `services/cubejs/src/routes/index.js`

```javascript
// services/cubejs/index.js:95
app.use(routes({ basePath, cubejs }));

// 注册的路由:
POST   /api/v1/run-sql                  - 执行 SQL 查询 (自定义)
GET    /api/v1/test                     - 测试数据源连接 (自定义)
GET    /api/v1/get-schema               - 获取 Cube.js schema (自定义)
POST   /api/v1/generate-models          - 生成数据模型 (自定义)
POST   /api/v1/pre-aggregation-preview  - 预览预聚合 (自定义)
GET    /api/v1/pre-aggregations         - 获取预聚合列表 (自定义)
```

### 2. Cube.js 标准 API (通过 cubejs.initApp(app) 自动注册)

**代码位置**: `services/cubejs/index.js:97`

```javascript
cubejs.initApp(app);
```

这一行代码通过 `@cubejs-backend/server-core` 的 `initApp()` 方法自动注册所有 Cube.js 标准 API 路由。

**内部实现**:
1. `ServerCore.initApp(app)` 调用 `this.apiGateway().initApp(app)`
2. API Gateway 注册所有标准端点

**用户端点** (需要用户认证):
```javascript
POST   /api/v1/load                     - 加载数据查询 (Cube.js 标准)
GET    /api/v1/load                     - 加载数据查询 (Cube.js 标准)
GET    /api/v1/subscribe                - 订阅数据查询 (Cube.js 标准)
POST   /api/v1/sql                      - SQL 查询接口 (Cube.js 标准)
GET    /api/v1/sql                      - SQL 查询接口 (Cube.js 标准)
GET    /api/v1/meta                     - 获取元数据 (Cube.js 标准)
POST   /api/v1/dry-run                  - 干运行查询分析 (Cube.js 标准)
GET    /api/v1/dry-run                  - 干运行查询分析 (Cube.js 标准)
POST   /api/v1/cubesql                  - 执行 CubeSQL 查询 (Cube.js 标准)
POST   /api/v1/pre-aggregations/can-use - 检查预聚合可用性 (Cube.js 标准)
POST   /api/graphql                     - GraphQL 端点 (Cube.js 标准)
POST   /api/v1/graphql-to-json          - GraphQL 转 JSON (Cube.js 标准)
```

**系统端点** (内部管理端点):
```javascript
GET/POST /api/cubejs-system/v1/context
GET/POST /api/cubejs-system/v1/pre-aggregations
GET/POST /api/cubejs-system/v1/pre-aggregations/security-contexts
GET/POST /api/cubejs-system/v1/pre-aggregations/timezones
GET/POST /api/cubejs-system/v1/pre-aggregations/partitions
GET/POST /api/cubejs-system/v1/pre-aggregations/preview
POST     /api/cubejs-system/v1/pre-aggregations/build
GET      /api/cubejs-system/v1/pre-aggregations/queue
POST     /api/cubejs-system/v1/pre-aggregations/cancel
```

**健康检查端点**:
```javascript
GET    /readyz                          - 就绪检查
GET    /livez                           - 存活检查
```

### 3. Swagger 文档

**代码位置**: `services/cubejs/index.js:93`

```javascript
app.use("/docs", swaggerUi.serve, swaggerUi.setup(swaggerDocument));
```

```
GET    /docs                            - Swagger UI 文档界面
```

### 路由注册顺序

```javascript
// services/cubejs/index.js

// 1. Swagger 文档 (先注册)
app.use("/docs", swaggerUi.serve, swaggerUi.setup(swaggerDocument));

// 2. 自定义路由 (在 basePath 下)
app.use(routes({ basePath, cubejs }));

// 3. Cube.js 标准 API (在 basePath 下)
cubejs.initApp(app);

// 4. 错误处理中间件
app.use((err, req, res, next) => { ... });
```

## 环境变量配置

### Cubestore 配置
```bash
CUBEJS_CUBESTORE_HOST=cubestore
CUBEJS_CUBESTORE_PORT=3030
CUBESTORE_VERSION=v1.2.3-arm64v8
```

### Hasura 配置
```bash
HASURA_ENDPOINT=http://hasura:8080/v1/graphql
HASURA_GRAPHQL_ADMIN_SECRET=<secret>
```

### Redis 配置
```bash
REDIS_ADDR=redis://redis:6379
```

### JWT 配置
```bash
JWT_KEY=LGB6j3RkoVuOuqKzjgnCeq7vwfqBYJDw
JWT_ALGORITHM=HS256
JWT_EXPIRES_IN=10800
```

### Cube.js 配置
```bash
CUBEJS_SECRET=cubejsKey
CUBEJS_URL=http://cubejs:4000
CUBEJS_SQL_PORT=13306
CUBEJS_PG_SQL_PORT=15432
CUBEJS_SCHEDULED_REFRESH=true
CUBEJS_REFRESH_TIMER=60
CUBEJS_TELEMETRY=false
```

## Docker Compose 依赖关系

```yaml
# docker-compose.dev.yml
cubejs:
  depends_on:
    - hasura      # 必需: 元数据和认证
    - cubestore   # 必需: 缓存和预聚合
    - redis       # 可选: 额外缓存层
  networks:
    - synmetrix_default
```

## 总结

Cubejs 服务是 Synmetrix 架构的核心分析引擎，它：
1. **必须依赖** Hasura 和 Cubestore 才能运行
2. **可选依赖** Redis 提供日志存储
3. **间接依赖** PostgreSQL (通过 Hasura)
4. **动态连接**用户配置的各种数据源
5. 实现了完整的多租户隔离和权限控制
6. 提供 REST API 和 SQL API 两种访问方式
