# Cube.js Server API 分析

## 概述

`@cubejs-backend/server` 是 Cube.js 的独立 Express 服务器包，通过 `@cubejs-backend/api-gateway` 提供完整的 API 层。它是 Cube.js 的核心服务组件，负责处理所有的数据查询、元数据访问、预聚合管理等功能。

## 架构设计

### 核心组件

1. **CubejsServer** (`packages/cubejs-server/src/server.ts`)
   - Express 服务器的封装
   - 管理 HTTP/WebSocket/SQL Server 的生命周期
   - 集成 CubejsServerCore 和 ApiGateway

2. **CubejsServerCore** (`packages/cubejs-server-core/src/core/server.ts`)
   - 核心业务逻辑
   - 管理 CompilerApi（模式编译）
   - 管理 OrchestratorApi（查询编排）
   - 处理安全上下文和多租户

3. **ApiGateway** (`packages/cubejs-api-gateway/src/gateway.ts`)
   - REST/GraphQL/SQL API 的统一入口
   - 认证授权中间件
   - 查询重写和安全策略

### 服务器启动流程

```typescript
// 1. 创建 CubejsServer 实例
const server = new CubejsServer(options);

// 2. 启动监听
const { app, port, server: httpServer } = await server.listen();

// 3. 初始化流程
// - 创建 Express app
// - 应用 CORS 和 body-parser 中间件
// - 调用 core.initApp(app) 初始化 API Gateway
// - 可选：启动 WebSocket Server
// - 可选：启动 SQL Server (Postgres 协议)
```

## API 详细说明

### 1. 数据查询 API (Data Scope)

#### 1.1 REST 查询接口

**`GET/POST /cubejs-api/v1/load`**

主要的数据查询接口，支持复杂的多维分析查询。

**请求参数：**
```json
{
  "measures": ["Orders.count", "Orders.totalAmount"],
  "dimensions": ["Orders.status", "Users.city"],
  "segments": ["Orders.completedOrders"],
  "timeDimensions": [{
    "dimension": "Orders.createdAt",
    "granularity": "day",
    "dateRange": ["2024-01-01", "2024-12-31"]
  }],
  "filters": [{
    "member": "Orders.status",
    "operator": "equals",
    "values": ["completed"]
  }],
  "limit": 100,
  "offset": 0,
  "order": {
    "Orders.createdAt": "desc"
  },
  "timezone": "UTC",
  "responseFormat": "default"
}
```

**功能特性：**
- ✅ 支持 measures（指标）、dimensions（维度）、segments（段）
- ✅ 支持复杂的 filters（过滤器）
- ✅ 支持 timeDimensions（时间维度）和粒度（granularity）
- ✅ 支持排序、分页
- ✅ 支持数据混合查询（blending query）- 数组形式的多查询
- ✅ 支持日期范围对比（compareDateRange）
- ✅ 支持 total 总计查询
- ✅ 查询重写（queryRewrite）和行级安全（RLS）
- ✅ 成员表达式（Member Expressions）支持

**响应格式：**
```json
{
  "query": { /* 标准化后的查询 */ },
  "data": [ /* 查询结果数据 */ ],
  "annotation": {
    "measures": { /* 指标元数据 */ },
    "dimensions": { /* 维度元数据 */ },
    "timeDimensions": { /* 时间维度元数据 */ }
  },
  "dataSource": "default",
  "dbType": "postgres",
  "lastRefreshTime": "2024-01-01T00:00:00.000Z",
  "total": 1000,
  "slowQuery": false,
  // 开发模式额外字段
  "refreshKeyValues": [...],
  "usedPreAggregations": {...},
  "transformedQuery": {...},
  "requestId": "uuid"
}
```

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:308-325`

#### 1.2 订阅接口

**`GET /cubejs-api/v1/subscribe`**

支持实时数据订阅，定期轮询数据更新。

**功能特性：**
- ✅ 基于 WebSocket 或长轮询
- ✅ 自动检测数据变化
- ✅ 与 `/load` 接口参数兼容

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:327-334`

#### 1.3 SQL 查询接口

**`GET/POST /cubejs-api/v1/sql`**

将 Cube.js 查询转换为 SQL 或执行 SQL 查询。

**请求参数：**
```json
{
  "query": { /* Cube.js 查询对象 */ },
  "format": "sql",  // 可选，用于 SQL-to-SQL 转换
  "disable_post_processing": false
}
```

**功能特性：**
- ✅ 查询转 SQL：返回生成的 SQL 语句
- ✅ SQL-to-SQL：将用户 SQL 转换为优化的 SQL
- ✅ 支持注解 SQL（annotated SQL）
- ✅ 支持禁用外部预聚合
- ✅ 包含查询执行计划信息

**响应格式：**
```json
{
  "sql": {
    "sql": ["SELECT ...", ["param1", "param2"]],
    "aliasNameToMember": { /* 别名映射 */ },
    "order": { "member": "desc" },
    "cacheKeyQueries": { /* 缓存键查询 */ },
    "preAggregations": [ /* 预聚合信息 */ ],
    "dataSource": "default",
    "external": false
  }
}
```

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:336-374`

#### 1.4 预览/验证接口

**`GET/POST /cubejs-api/v1/dry-run`**

查询预览和验证，不执行实际的数据查询。

**功能特性：**
- ✅ 验证查询语法
- ✅ 返回标准化查询
- ✅ 返回查询顺序
- ✅ 返回 pivot 查询配置
- ✅ 用于 UI 调试和开发

**响应格式：**
```json
{
  "queryType": "regularQuery",
  "normalizedQueries": [ /* 标准化查询 */ ],
  "queryOrder": [ /* 排序配置 */ ],
  "transformedQueries": [ /* 转换后的查询 */ ],
  "pivotQuery": { /* pivot 配置 */ }
}
```

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:376-390`

#### 1.5 CubeSQL 执行接口

**`POST /cubejs-api/v1/cubesql`**

通过 Postgres 兼容的 SQL 接口执行查询。

**请求参数：**
```json
{
  "query": "SELECT * FROM Orders WHERE status = 'completed'"
}
```

**功能特性：**
- ✅ 支持标准 SQL 语法
- ✅ 流式响应（chunked transfer encoding）
- ✅ 与 CubeSQL Rust 引擎集成

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:414-441`

### 2. 元数据 API (Meta Scope)

#### 2.1 基础元数据接口

**`GET /cubejs-api/v1/meta`**

获取数据模型的元数据定义。

**查询参数：**
- `extended` - 获取扩展元数据
- `includeCompilerId` - 包含编译器 ID
- `onlyCompilerId` - 仅返回编译器 ID

**响应格式（基础模式）：**
```json
{
  "cubes": [
    {
      "name": "Orders",
      "title": "Orders",
      "measures": [
        {
          "name": "Orders.count",
          "title": "Count",
          "type": "number",
          "aggType": "count",
          "cumulative": false,
          "isVisible": true
        }
      ],
      "dimensions": [
        {
          "name": "Orders.status",
          "title": "Status",
          "type": "string",
          "isVisible": true
        }
      ],
      "segments": [
        {
          "name": "Orders.completedOrders",
          "title": "Completed Orders",
          "isVisible": true
        }
      ]
    }
  ]
}
```

**响应格式（扩展模式）：**
```json
{
  "cubes": [
    {
      "name": "Orders",
      /* ... 基础字段 ... */
      "fileName": "Orders.js",
      "sql": "SELECT * FROM orders",
      "joins": [
        {
          "name": "Users",
          "relationship": "belongsTo",
          "sql": "{CUBE}.user_id = {Users}.id"
        }
      ],
      "preAggregations": [
        {
          "name": "Orders.main",
          "type": "rollup",
          "dimensions": ["status"],
          "measures": ["count"],
          "timeDimension": "createdAt",
          "granularity": "day",
          "refreshKey": { /* ... */ }
        }
      ],
      "measures": [
        {
          /* ... 基础字段 ... */
          "sql": "COUNT(*)",
          "filters": [],
          "drillMembers": []
        }
      ]
    }
  ]
}
```

**功能特性：**
- ✅ 过滤不可见的成员（基于 `isVisible`）
- ✅ 开发模式显示所有成员
- ✅ 支持多租户/安全上下文
- ✅ 扩展模式包含 SQL 定义、joins、preAggregations

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:396-412, 596-673`

#### 2.2 预聚合兼容性检查

**`POST /cubejs-api/v1/pre-aggregations/can-use`**

检查查询是否可以使用指定的预聚合（Rollup Designer 使用）。

**请求参数：**
```json
{
  "transformedQuery": { /* 转换后的查询 */ },
  "references": { /* 引用信息 */ }
}
```

**响应格式：**
```json
{
  "canUsePreAggregationForTransformedQuery": true
}
```

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:443-462`

### 3. GraphQL API (GraphQL Scope)

#### 3.1 GraphQL 查询接口

**`POST/GET /cubejs-api/graphql`**

基于数据模型自动生成的 GraphQL API。

**功能特性：**
- ✅ 自动从 Cube.js 模型生成 GraphQL Schema
- ✅ 支持 GraphiQL 交互式界面（开发环境）
- ✅ 支持所有 Cube.js 查询功能
- ✅ 类型安全的查询

**GraphQL Schema 示例：**
```graphql
type Query {
  cube(
    measures: [String]
    dimensions: [String]
    segments: [String]
    timeDimensions: [TimeDimension]
    filters: [Filter]
    limit: Int
    offset: Int
  ): CubeResult
}

type CubeResult {
  data: [JSON]
  annotation: JSON
}
```

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:272-302`

#### 3.2 GraphQL 到 JSON 转换

**`POST /cubejs-api/v1/graphql-to-json`**

将 GraphQL 查询转换为 Cube.js JSON 查询格式。

**请求参数：**
```json
{
  "query": "query { cube(measures: [\"Orders.count\"]) { data } }",
  "variables": {}
}
```

**响应格式：**
```json
{
  "jsonQuery": {
    "measures": ["Orders.count"]
  }
}
```

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:246-270`

### 4. 预聚合管理 API (Jobs Scope)

#### 4.1 预聚合任务接口

**`POST /cubejs-api/v1/pre-aggregations/jobs`**

管理预聚合构建任务的提交和查询。

**提交任务（POST）：**
```json
{
  "action": "post",
  "selector": {
    "contexts": [
      { "securityContext": { "tenant": "t1" } },
      { "securityContext": { "tenant": "t2" } }
    ],
    "timezones": ["UTC", "America/Los_Angeles"],
    "dataSources": ["default"],
    "cubes": ["Orders", "Users"],
    "preAggregations": ["Orders.main", "Users.summary"],
    "dateRange": ["2024-01-01", "2024-12-31"]
  }
}
```

**响应（POST）：**
```json
[
  "ec1232ea3356f04f8be313fecf3deb4d",
  "48b75d5c466fa579c936dc451f498f69"
]
```

**查询任务状态（GET）：**
```json
{
  "action": "get",
  "tokens": [
    "ec1232ea3356f04f8be313fecf3deb4d",
    "48b75d5c466fa579c936dc451f498f69"
  ],
  "resType": "array"  // 或 "object"
}
```

**响应（GET - array）：**
```json
[
  {
    "token": "ec1232ea3356f04f8be313fecf3deb4d",
    "status": "done",
    "table": "dev_pre_aggregations.orders_main_20240101",
    "selector": { /* 选择器信息 */ }
  },
  {
    "token": "48b75d5c466fa579c936dc451f498f69",
    "status": "processing",
    "table": "dev_pre_aggregations.users_summary_20240101",
    "selector": { /* 选择器信息 */ }
  }
]
```

**任务状态：**
- `scheduled` - 已调度，等待执行
- `processing` - 正在处理
- `done` - 完成
- `failure` - 失败
- `not_found` - 任务不存在

**功能特性：**
- ✅ 批量提交预聚合构建任务
- ✅ 支持多租户/多上下文
- ✅ 支持时区和日期范围过滤
- ✅ 支持按 cube 和 preAggregation 过滤
- ✅ 任务状态跟踪和查询
- ✅ 队列管理

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:468-472, 862-1159`

### 5. 系统管理 API (Private/System API)

所有系统管理 API 都需要 `playgroundAuthSecret` 认证。

#### 5.1 系统上下文

**`GET /cubejs-system/v1/context`**

获取系统上下文信息（basePath、版本等）。

**响应格式：**
```json
{
  "basePath": "/cubejs-api",
  "dockerVersion": "1.0.0",
  "serverCoreVersion": "0.35.0"
}
```

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:486, 2642-2652`

#### 5.2 预聚合列表

**`GET /cubejs-system/v1/pre-aggregations`**

获取所有预聚合的列表和状态。

**查询参数：**
- `cacheOnly` - 仅返回缓存的预聚合
- `metaOnly` - 仅返回元数据，不查询实际状态

**响应格式：**
```json
{
  "preAggregations": [
    {
      "id": "Orders.main",
      "type": "rollup",
      "partitions": [
        {
          "tableName": "dev_pre_aggregations.orders_main_20240101",
          "status": "ready",
          "lastRefreshTime": "2024-01-01T00:00:00.000Z"
        }
      ]
    }
  ]
}
```

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:488-495, 675-703`

#### 5.3 安全上下文列表

**`GET /cubejs-system/v1/pre-aggregations/security-contexts`**

获取预聚合刷新的安全上下文列表。

**响应格式：**
```json
{
  "securityContexts": [
    { "tenant": "t1" },
    { "tenant": "t2" }
  ]
}
```

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:497-504`

#### 5.4 时区列表

**`GET /cubejs-system/v1/pre-aggregations/timezones`**

获取预聚合刷新的时区配置。

**响应格式：**
```json
{
  "timezones": ["UTC", "America/Los_Angeles", "Europe/London"]
}
```

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:506-510`

#### 5.5 预聚合分区信息

**`POST /cubejs-system/v1/pre-aggregations/partitions`**

获取预聚合的详细分区信息。

**请求参数：**
```json
{
  "query": {
    "timezones": ["UTC"],
    "preAggregations": [
      { "id": "Orders.main" }
    ],
    "expand": ["partitions.details", "partitions.versions"]
  }
}
```

**响应格式：**
```json
{
  "preAggregationPartitions": [
    {
      "preAggregation": { "id": "Orders.main" },
      "partitions": [
        {
          "dataSource": "default",
          "preAggregationId": "Orders.main",
          "tableName": "dev_pre_aggregations.orders_main_20240101",
          "type": "rollup",
          "versionEntries": [ /* 版本信息 */ ],
          "structureVersion": "v1"
        }
      ],
      "timezones": ["UTC"],
      "invalidateKeyQueries": [ /* 失效键查询 */ ]
    }
  ]
}
```

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:512-518, 705-766`

#### 5.6 预聚合数据预览

**`POST /cubejs-system/v1/pre-aggregations/preview`**

预览预聚合表的数据。

**请求参数：**
```json
{
  "query": {
    "preAggregationId": "Orders.main",
    "versionEntry": {
      "table_name": "dev_pre_aggregations.orders_main_20240101"
    },
    "timezone": "UTC"
  }
}
```

**响应格式：**
```json
{
  "preview": [
    { "status": "completed", "count": 100, "created_at_day": "2024-01-01" },
    { "status": "pending", "count": 50, "created_at_day": "2024-01-01" }
  ]
}
```

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:520-526, 768-800`

#### 5.7 构建预聚合

**`POST /cubejs-system/v1/pre-aggregations/build`**

手动触发预聚合构建。

**请求参数：**
```json
{
  "query": {
    "timezones": ["UTC"],
    "preAggregations": [
      { "id": "Orders.main" }
    ]
  }
}
```

**响应格式：**
```json
{
  "result": [
    {
      "status": "scheduled",
      "partition": "2024-01-01"
    }
  ]
}
```

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:528-534, 802-820`

#### 5.8 查看构建队列

**`POST /cubejs-system/v1/pre-aggregations/queue`**

查看当前预聚合构建队列状态。

**响应格式：**
```json
{
  "result": [
    {
      "queryHandler": "query",
      "status": ["active", "2024-01-01T00:00:00.000Z"],
      "query": { /* 查询信息 */ },
      "priority": 10
    }
  ]
}
```

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:536-541, 1161-1175`

#### 5.9 取消构建任务

**`POST /cubejs-system/v1/pre-aggregations/cancel`**

取消队列中的预聚合构建任务。

**请求参数：**
```json
{
  "query": {
    "queryKeys": ["key1", "key2"],
    "dataSource": "default"
  }
}
```

**响应格式：**
```json
{
  "result": [
    { "queryKey": "key1", "cancelled": true },
    { "queryKey": "key2", "cancelled": false }
  ]
}
```

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:543-549, 1177-1192`

### 6. 健康检查 API (No Scope Required)

#### 6.1 就绪检查

**`GET /readyz`**

检查服务是否准备好接收请求。

**检查项：**
- ✅ 数据源连接测试
- ✅ 编排器连接测试
- ✅ 查询引擎就绪状态

**响应格式：**
```json
{
  "health": "HEALTH"  // 或 "DOWN"
}
```

**HTTP 状态码：**
- `200` - 健康
- `500` - 不健康

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:239, 2663-2681`

#### 6.2 存活检查

**`GET /livez`**

检查服务是否存活（轻量级检查）。

**检查项：**
- ✅ 数据存储连接测试
- ✅ 编排器连接测试

**响应格式：**
```json
{
  "health": "HEALTH"  // 或 "DOWN"
}
```

**HTTP 状态码：**
- `200` - 存活
- `500` - 不存活

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:240, 2683-2696`

### 7. WebSocket API

Cube.js 支持通过 WebSocket 进行实时查询订阅。

**连接端点：** `ws://localhost:4000/` (或配置的端口)

**消息格式：**
```json
{
  "messageId": "unique-id",
  "authorization": "JWT_TOKEN",
  "query": {
    "measures": ["Orders.count"]
  }
}
```

**功能特性：**
- ✅ 实时数据更新推送
- ✅ 自动重连机制
- ✅ 上下文验证（wsContextAcceptor）
- ✅ 订阅状态管理

**实现位置：**
- Server: `packages/cubejs-server/src/websocket-server.ts`
- Gateway: `packages/cubejs-api-gateway/src/SubscriptionServer.ts`
- Storage: `packages/cubejs-api-gateway/src/LocalSubscriptionStore.ts`

### 8. SQL Server API (Postgres 协议)

Cube.js 提供 Postgres 兼容的 SQL 接口，允许使用标准 SQL 工具连接。

**配置：**
```bash
CUBEJS_SQL_PORT=5432
CUBEJS_PG_SQL_PORT=5432
```

**连接示例：**
```bash
psql -h localhost -p 5432 -U user -d cubedb
```

**功能特性：**
- ✅ Postgres 线协议兼容
- ✅ 支持标准 SQL 语法
- ✅ 支持 JDBC/ODBC 驱动
- ✅ 与 BI 工具集成（Tableau, Metabase, Superset 等）
- ✅ 查询转换和优化

**实现位置：** `packages/cubejs-api-gateway/src/sql-server/`

### 9. Native API Gateway (v2)

实验性的原生 API 网关，通过 Rust 实现更高性能。

**配置：**
```bash
CUBEJS_NATIVE_API_GATEWAY=true
CUBEJS_NATIVE_API_GATEWAY_PORT=8080
```

**端点：** `/cubejs-api/v2/*`

**功能特性：**
- ✅ 高性能 Rust 实现
- ✅ 通过 HTTP 代理转发到原生网关
- ✅ 向后兼容 v1 API

**实现位置：** `packages/cubejs-api-gateway/src/gateway.ts:559-569`

## 认证与授权

### 认证方式

#### 1. JWT Token 认证（默认）

**请求头：**
```http
Authorization: Bearer <JWT_TOKEN>
# 或
x-cube-authorization: <JWT_TOKEN>
```

**JWT Payload 示例：**
```json
{
  "sub": "user_id",
  "iat": 1234567890,
  "exp": 1234567999,
  "tenant": "t1",
  "role": "admin"
}
```

#### 2. JWT 配置选项

```typescript
{
  jwt: {
    key: 'SECRET_KEY',              // JWT 密钥
    algorithms: ['HS256', 'RS256'], // 支持的算法
    issuer: 'cube.dev',             // 签发者
    audience: 'cube-api',           // 受众
    subject: 'user',                // 主题
    claimsNamespace: 'https://cube.dev', // 声明命名空间
    jwkUrl: 'https://auth.example.com/.well-known/jwks.json' // JWK URL
  }
}
```

#### 3. 自定义认证

```typescript
{
  checkAuth: async (req, authorization) => {
    // 自定义认证逻辑
    const user = await validateToken(authorization);
    req.securityContext = {
      userId: user.id,
      tenant: user.tenant,
      roles: user.roles
    };
  }
}
```

### API Scopes（权限范围）

Cube.js 支持细粒度的 API 权限控制：

**可用 Scopes：**
- `graphql` - GraphQL API 访问权限
- `meta` - 元数据 API 访问权限
- `data` - 数据查询 API 访问权限
- `sql` - SQL API 访问权限
- `jobs` - 预聚合任务 API 访问权限

**配置示例：**
```typescript
{
  contextToApiScopes: async (securityContext, defaultScopes) => {
    if (securityContext.role === 'admin') {
      return ['graphql', 'meta', 'data', 'sql', 'jobs'];
    } else if (securityContext.role === 'analyst') {
      return ['graphql', 'meta', 'data'];
    } else {
      return ['data'];
    }
  }
}
```

**环境变量配置：**
```bash
CUBEJS_DEFAULT_API_SCOPES=graphql,meta,data,sql
```

### 安全上下文（Security Context）

安全上下文是贯穿整个请求生命周期的认证信息对象。

**设置方式：**
1. JWT Token Payload
2. checkAuth 函数设置 `req.securityContext`
3. extendContext 函数扩展上下文

**使用场景：**
- 多租户数据隔离
- 行级安全（Row Level Security）
- 查询重写（Query Rewrite）
- 数据源路由

**示例：**
```typescript
// 在 Cube.js 模型中使用
cube('Orders', {
  sql: `SELECT * FROM orders WHERE tenant_id = '${SECURITY_CONTEXT.tenant}'`,

  measures: {
    count: {
      type: 'count'
    }
  }
});
```

### Playground 认证

开发环境专用的认证机制。

**配置：**
```bash
CUBEJS_PLAYGROUND_AUTH_SECRET=secret123
```

**功能：**
- ✅ 访问系统管理 API (`/cubejs-system/*`)
- ✅ 查看所有元数据（包括隐藏成员）
- ✅ 查看调试信息（refreshKeyValues, usedPreAggregations 等）

**实现：** 自动尝试主认证和 Playground 认证，任一成功即通过。

## 查询处理流程

### 1. 查询生命周期

```
1. 请求接收
   ↓
2. 认证授权（checkAuth）
   ↓
3. 上下文创建（contextByReq）
   ↓
4. 上下文验证（contextRejectionMiddleware）
   ↓
5. 查询解析（parseQueryParam）
   ↓
6. 成员表达式解析（parseMemberExpressionsInQuery）
   ↓
7. 行级安全应用（applyRowLevelSecurity）
   ↓
8. 查询重写（queryRewrite）
   ↓
9. 查询标准化（normalizeQuery）
   ↓
10. SQL 生成（CompilerApi.getSql）
   ↓
11. 预聚合匹配（findPreAggregationToUse）
   ↓
12. 查询执行（OrchestratorApi.executeQuery）
   ↓
13. 结果转换（ResultWrapper.transform）
   ↓
14. 响应返回
```

### 2. 核心组件交互

```
ApiGateway
  ├─ checkAuth()
  ├─ requestContextMiddleware()
  ├─ load() / sql() / meta()
  │   ├─ getCompilerApi(context)
  │   │   └─ CompilerApi
  │   │       ├─ metaConfig() - 获取元数据
  │   │       ├─ getSql() - 生成 SQL
  │   │       └─ applyRowLevelSecurity() - 应用安全策略
  │   │
  │   └─ getAdapterApi(context)
  │       └─ OrchestratorApi
  │           ├─ executeQuery() - 执行查询
  │           ├─ getPreAggregationVersionEntries() - 预聚合版本
  │           └─ testConnection() - 测试连接
  │
  └─ RefreshScheduler
      ├─ preAggregationPartitions() - 获取分区
      ├─ buildPreAggregations() - 构建预聚合
      └─ postBuildJobs() - 提交构建任务
```

### 3. 查询重写（Query Rewrite）

查询重写允许在查询执行前修改查询参数。

**配置示例：**
```typescript
{
  queryRewrite: async (query, context) => {
    // 添加租户过滤
    if (context.securityContext.tenant) {
      query.filters = query.filters || [];
      query.filters.push({
        member: 'Orders.tenantId',
        operator: 'equals',
        values: [context.securityContext.tenant]
      });
    }

    // 限制时间范围
    if (!query.timeDimensions || query.timeDimensions.length === 0) {
      query.timeDimensions = [{
        dimension: 'Orders.createdAt',
        dateRange: 'last 30 days'
      }];
    }

    return query;
  }
}
```

**执行时机：** 在行级安全（RLS）之后，SQL 生成之前。

### 4. 行级安全（Row Level Security）

在 Cube.js 模型中定义的安全策略。

**定义示例：**
```javascript
cube('Orders', {
  sql: `SELECT * FROM orders`,

  // 访问策略
  accessPolicy: {
    // 拒绝访问条件
    denyIf: (securityContext) => {
      return !securityContext.userId;
    },

    // 自动添加过滤器
    addFilters: (securityContext) => {
      if (securityContext.role !== 'admin') {
        return [{
          member: 'Orders.userId',
          operator: 'equals',
          values: [securityContext.userId]
        }];
      }
    }
  }
});
```

**执行时机：** 在查询重写之前，作用于标准化后的查询。

### 5. 成员表达式（Member Expressions）

允许在查询中动态定义计算成员。

**语法示例：**
```json
{
  "measures": [
    "Orders.count",
    "{\"expr\":{\"type\":\"SqlFunction\",\"sql\":\"SUM(${Orders.amount} * 1.1)\",\"cubeParams\":[\"Orders\"]},\"alias\":\"totalWithTax\",\"cubeName\":\"Orders\"}"
  ]
}
```

**功能特性：**
- ✅ SqlFunction - 自定义 SQL 表达式
- ✅ PatchMeasure - 修改现有指标（更改聚合类型、添加过滤器）
- ✅ 支持 Cube 参数传递
- ✅ 分组集（Grouping Sets）支持

**处理流程：**
1. 解析 JSON 字符串
2. 转换为 ParsedMemberExpression
3. 构建 Function 对象
4. 在 SQL 生成时执行

### 6. 数据流式传输（Streaming）

支持大数据集的流式查询。

**API 调用：**
```typescript
const streamResult = await apiGateway.stream(context, query);
if (streamResult) {
  streamResult.stream
    .on('data', (chunk) => {
      console.log('Data chunk:', chunk);
    })
    .on('end', () => {
      console.log('Stream ended');
    })
    .on('error', (error) => {
      console.error('Stream error:', error);
    });
}
```

**特性：**
- ✅ 不缓存结果
- ✅ 持久查询（persistent: true）
- ✅ 逐块返回数据
- ✅ 减少内存使用

## 配置选项

### Server Options (CubejsServer)

```typescript
interface CreateOptions {
  // 核心配置
  dbType?: DatabaseType | DbTypeAsyncFn;
  driverFactory?: (context: DriverContext) => Promise<BaseDriver>;
  schemaPath?: string;
  apiSecret?: string;

  // HTTP 配置
  http?: {
    cors?: CorsOptions;
  };

  // WebSocket 配置
  webSockets?: boolean;

  // SQL Server 配置
  sqlPort?: number;
  pgSqlPort?: number;
  gatewayPort?: number;

  // 服务器配置
  serverKeepAliveTimeout?: number;
  serverHeadersTimeout?: number;
  gracefulShutdown?: number;

  // 预聚合配置
  scheduledRefreshContexts?: () => Promise<UserBackgroundContext[]>;
  scheduledRefreshTimeZones?: string[] | ScheduledRefreshTimeZonesFn;
  scheduledRefreshConcurrency?: number;
  scheduledRefreshBatchSize?: number;

  // 编译器配置
  schemaVersion?: (context: RequestContext) => string;
  compilerCacheSize?: number;
  maxCompilerCacheKeepAlive?: number;
  updateCompilerCacheKeepAlive?: boolean;

  // 安全配置
  checkAuth?: CheckAuthFn;
  jwt?: JWTOptions;
  contextToApiScopes?: ContextToApiScopesFn;
  queryRewrite?: QueryRewriteFn;
  extendContext?: ExtendContextFn;

  // 多租户配置
  contextToAppId?: ContextToAppIdFn;
  contextToOrchestratorId?: ContextToOrchestratorIdFn;
  contextToCubeStoreRouterId?: ContextToCubeStoreRouterIdFn;

  // 开发配置
  devServer?: boolean;
  telemetry?: boolean;

  // 其他
  logger?: LoggerFn;
  basePath?: string;
  playgroundAuthSecret?: string;
}
```

### 环境变量

**核心配置：**
```bash
# 数据库配置
CUBEJS_DB_TYPE=postgres
CUBEJS_DB_HOST=localhost
CUBEJS_DB_PORT=5432
CUBEJS_DB_NAME=database
CUBEJS_DB_USER=user
CUBEJS_DB_PASS=password

# API 配置
CUBEJS_API_SECRET=secret
CUBEJS_PORT=4000
CUBEJS_BASE_PATH=/cubejs-api

# WebSocket
CUBEJS_WEB_SOCKETS=true

# SQL Server
CUBEJS_SQL_PORT=5432
CUBEJS_PG_SQL_PORT=5432

# 安全配置
CUBEJS_DEFAULT_API_SCOPES=graphql,meta,data,sql
NODE_ENV=production  # 启用安全检查

# 开发配置
CUBEJS_DEV_MODE=true
CUBEJS_PLAYGROUND_AUTH_SECRET=secret123

# 预聚合配置
CUBEJS_SCHEDULED_REFRESH_CONCURRENCY=4
CUBEJS_SCHEDULED_REFRESH_TIMER=30

# 外部数据库（预聚合存储）
CUBEJS_EXT_DB_TYPE=cubestore
CUBEJS_EXT_DB_HOST=localhost
CUBEJS_EXT_DB_PORT=3030

# CubeStore
CUBESTORE_REMOTE_DIR=s3://bucket/path

# 日志配置
CUBEJS_LOG_LEVEL=info

# 性能配置
CUBEJS_DB_MAX_POOL=8
CUBEJS_CONCURRENCY=2
CUBEJS_SERVER_KEEP_ALIVE_TIMEOUT=120000
CUBEJS_SERVER_HEADERS_TIMEOUT=130000

# Telemetry
CUBEJS_TELEMETRY=true

# Native API Gateway
CUBEJS_NATIVE_API_GATEWAY=true
CUBEJS_NATIVE_API_GATEWAY_PORT=8080
```

## 错误处理

### 错误类型

#### 1. UserError
用户输入错误（400）

```typescript
throw new UserError('Invalid query format');
```

**场景：**
- 查询参数格式错误
- 必填字段缺失
- 无效的成员引用

#### 2. CubejsHandlerError
API 处理错误（自定义状态码）

```typescript
throw new CubejsHandlerError(403, 'Forbidden', 'Access denied');
```

**场景：**
- 认证失败（403）
- 授权失败（403）
- API Scope 不足（403）

#### 3. 系统错误
内部服务器错误（500）

**场景：**
- 数据库连接失败
- 编译错误
- 预聚合构建失败

### 错误响应格式

```json
{
  "error": "Error message",
  "type": "UserError",
  "stack": "Error stack (dev mode only)",
  "requestId": "uuid (dev mode only)",
  "plainError": ["详细错误消息数组"]
}
```

### 错误日志

所有错误都会通过 logger 记录：

```typescript
this.logger('Error Type', {
  error: e.message,
  stack: e.stack,
  query: query,
  context: context,
  requestId: requestId
});
```

## 性能优化

### 1. 查询缓存

**缓存键生成：**
- 查询参数
- 安全上下文
- 数据源
- 刷新键（Refresh Key）

**缓存存储：**
- Redis（生产环境）
- 内存（开发环境）

**配置：**
```typescript
{
  cacheAndQueueDriver: 'redis',
  redis: {
    url: 'redis://localhost:6379'
  }
}
```

### 2. 预聚合（Pre-aggregations）

**自动匹配：**
查询执行时自动查找可用的预聚合表。

**手动刷新：**
通过预聚合管理 API 手动触发构建。

**调度刷新：**
通过 `scheduledRefreshContexts` 配置定时刷新。

### 3. 查询队列

**并发控制：**
```bash
CUBEJS_CONCURRENCY=2  # 同时执行的查询数
```

**队列优先级：**
- 实时查询优先级高
- 预聚合构建优先级低

### 4. 编译器缓存

**LRU 缓存：**
```typescript
{
  compilerCacheSize: 250,
  maxCompilerCacheKeepAlive: 60000,  // 1分钟
  updateCompilerCacheKeepAlive: true
}
```

**多租户缓存：**
根据 `contextToAppId` 为每个租户缓存编译结果。

### 5. 连接池

**数据库连接池：**
```bash
CUBEJS_DB_MAX_POOL=8
```

**动态计算：**
基于 `CUBEJS_CONCURRENCY` 自动计算最优池大小。

## 监控与日志

### 日志事件类型

**查询日志：**
- `Load Request` - 查询开始
- `Load Request Success` - 查询成功
- `Load Request SQL` - SQL 生成
- `Query Rewrite` - 查询重写
- `Slow Query Warning` - 慢查询警告

**错误日志：**
- `User Error` - 用户错误
- `Orchestrator error` - 编排错误
- `Internal Server Error` - 系统错误
- `Cube SQL Error` - SQL 错误

**系统日志：**
- `Server Start` - 服务启动
- `Refresh Scheduler Interval` - 刷新调度
- `Graceful Shutdown` - 优雅关闭

**网络日志：**
- `Incoming network usage` - 入站流量
- `Outgoing network usage` - 出站流量
- `REST API Request` - API 请求

### 指标收集

**Telemetry 事件：**
- `first_server_start` - 首次启动
- `Server Start` - 服务启动
- `Load Request Success Aggregated` - 聚合成功计数
- `pre_aggregations_jobs_post` - 预聚合任务提交
- `pre_aggregations_jobs_get` - 预聚合任务查询

**配置：**
```bash
CUBEJS_TELEMETRY=true
```

### 健康检查集成

**Kubernetes 配置：**
```yaml
livenessProbe:
  httpGet:
    path: /livez
    port: 4000
  initialDelaySeconds: 30
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /readyz
    port: 4000
  initialDelaySeconds: 5
  periodSeconds: 5
```

## 部署最佳实践

### 1. 生产环境配置

```bash
# 强制安全检查
NODE_ENV=production

# API Secret 必须设置
CUBEJS_API_SECRET=<strong-random-secret>

# 禁用开发模式
CUBEJS_DEV_MODE=false

# 关闭 Telemetry（可选）
CUBEJS_TELEMETRY=false

# 使用外部预聚合存储
CUBEJS_EXT_DB_TYPE=cubestore
CUBEJS_EXT_DB_HOST=cubestore
CUBEJS_EXT_DB_PORT=3030

# 使用 Redis 缓存
CUBEJS_CACHE_AND_QUEUE_DRIVER=redis
CUBEJS_REDIS_URL=redis://redis:6379

# 性能调优
CUBEJS_CONCURRENCY=4
CUBEJS_DB_MAX_POOL=16
CUBEJS_SERVER_KEEP_ALIVE_TIMEOUT=120000
CUBEJS_SERVER_HEADERS_TIMEOUT=130000

# 预聚合刷新
CUBEJS_SCHEDULED_REFRESH_TIMER=60
CUBEJS_SCHEDULED_REFRESH_CONCURRENCY=4
```

### 2. 高可用部署

**水平扩展：**
- 多个 Cube.js 实例
- 负载均衡器分发请求
- 共享 Redis 缓存
- 共享 CubeStore

**架构示例：**
```
Load Balancer
    ↓
Cube.js Instance 1 ─┐
Cube.js Instance 2 ─┼─→ Redis ─→ CubeStore
Cube.js Instance 3 ─┘
```

### 3. 安全加固

**最小权限原则：**
- 数据库用户只读权限
- 预聚合存储隔离
- API Token 定期轮换

**网络隔离：**
- 仅暴露必要端口
- 使用 VPC/私有网络
- TLS/SSL 加密

**审计日志：**
- 记录所有 API 访问
- 敏感操作告警
- 定期审查日志

### 4. 监控告警

**关键指标：**
- 请求成功率
- 请求延迟（P50, P95, P99）
- 慢查询数量
- 预聚合构建状态
- 数据库连接数
- 内存使用率

**告警阈值：**
- 错误率 > 1%
- P95 延迟 > 5s
- 慢查询 > 10s
- 预聚合构建失败

### 5. 备份与恢复

**需备份内容：**
- Cube.js 模型文件（schema）
- 预聚合配置
- 环境变量配置
- Redis 数据（可选）

**灾难恢复：**
- 自动化部署脚本
- 配置版本控制
- 预聚合重建计划

## 与其他组件的集成

### 1. 前端客户端

**官方客户端库：**
- `@cubejs-client/core` - 核心 JavaScript 客户端
- `@cubejs-client/react` - React 集成
- `@cubejs-client/vue` - Vue 集成
- `@cubejs-client/vue3` - Vue 3 集成
- `@cubejs-client/ngx` - Angular 集成

**使用示例：**
```typescript
import cube from '@cubejs-client/core';

const cubejsApi = cube(
  'JWT_TOKEN',
  { apiUrl: 'http://localhost:4000/cubejs-api/v1' }
);

const resultSet = await cubejsApi.load({
  measures: ['Orders.count'],
  timeDimensions: [{
    dimension: 'Orders.createdAt',
    granularity: 'day',
    dateRange: 'last 7 days'
  }]
});
```

### 2. BI 工具集成

**通过 SQL API：**
- Tableau
- Metabase
- Apache Superset
- Redash
- Looker
- Power BI (ODBC/JDBC)

**连接配置：**
```
Host: localhost
Port: 5432 (CUBEJS_SQL_PORT)
Database: cubedb
User: <any>
Password: JWT_TOKEN
```

### 3. ETL 工具集成

**通过 REST API：**
- Apache Airflow
- dbt
- Luigi
- Prefect

**触发预聚合构建：**
```python
import requests

response = requests.post(
    'http://localhost:4000/cubejs-system/v1/pre-aggregations/build',
    headers={
        'Authorization': f'Bearer {PLAYGROUND_SECRET}'
    },
    json={
        'query': {
            'timezones': ['UTC'],
            'preAggregations': [{'id': 'Orders.main'}]
        }
    }
)
```

### 4. 数据源集成

**支持的数据库：**
- PostgreSQL, MySQL, MS SQL Server
- BigQuery, Redshift, Snowflake
- Athena, Presto, ClickHouse
- MongoDB, Elasticsearch
- Druid, Databricks
- 等 50+ 数据源

**多数据源配置：**
```typescript
{
  driverFactory: async (context) => {
    if (context.dataSource === 'analytics') {
      return new BigQueryDriver({...});
    } else if (context.dataSource === 'warehouse') {
      return new SnowflakeDriver({...});
    }
  }
}
```

## 总结

Cube.js Server 提供了一套完整、强大且灵活的 API 体系：

**核心优势：**
1. ✅ **统一的 API 层** - REST/GraphQL/SQL 多协议支持
2. ✅ **灵活的安全模型** - JWT、API Scopes、RLS、查询重写
3. ✅ **智能的查询优化** - 自动预聚合匹配、缓存、查询队列
4. ✅ **多租户支持** - 上下文隔离、数据源路由
5. ✅ **完整的管理能力** - 预聚合管理、健康检查、监控日志
6. ✅ **丰富的集成选项** - BI 工具、ETL、前端框架
7. ✅ **生产就绪** - 高可用、横向扩展、优雅关闭

**适用场景：**
- 构建数据分析 API
- 嵌入式分析（Embedded Analytics）
- 内部 BI 平台
- 数据产品 API
- 实时数据仪表板
- 多租户 SaaS 分析

**技术栈：**
- TypeScript + Node.js（主要服务）
- Rust（CubeSQL 引擎、CubeStore）
- Express（HTTP 服务器）
- Redis（缓存队列）
- WebSocket（实时订阅）

这是一个设计良好、功能完备的语义层解决方案，值得学习和参考。
