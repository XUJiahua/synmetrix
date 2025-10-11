# 为什么 Synmetrix 使用 cubejs/cubestore 而不是 cubejs/cube？

## 核心答案

Synmetrix 实际上**同时使用了两个独立的组件**，它们各司其职：

1. **Cube.js 服务** - 自己构建的容器（基于 `@cubejs-backend/server-core`）
2. **Cubestore** - 使用官方 `cubejs/cubestore` 镜像

这两个组件在 Cube.js 生态系统中扮演不同的角色。

## 架构对比

### Synmetrix 的架构（实际使用）

```
┌─────────────────────────────────────────────────────┐
│  Cube.js 服务 (自建容器)                              │
│  ────────────────────────────────────────           │
│  - 接收查询请求                                       │
│  - JWT 认证和权限控制                                │
│  - 从数据库动态加载 schema                           │
│  - 生成 SQL 查询                                     │
│  - 连接多种源数据库                                  │
│  - 管理预聚合生命周期                                │
│  - 提供 REST API + SQL API                          │
└────────────┬───────────────────────┬─────────────────┘
             │                       │
             │ 查询源数据             │ 存储预聚合/缓存
             ↓                       ↓
    ┌────────────────┐      ┌──────────────────┐
    │  用户数据源     │      │   Cubestore      │
    │  ─────────     │      │  (官方镜像)       │
    │  PostgreSQL    │      │  ────────────    │
    │  MySQL         │      │  - 预聚合表       │
    │  ClickHouse    │      │  - 查询结果缓存   │
    │  BigQuery      │      │  - 列式存储       │
    │  Snowflake     │      │  - 分布式查询     │
    │  等 20+ 种     │      │                  │
    └────────────────┘      └──────────────────┘
```

### 传统 Cube.js 的架构（不适用于 Synmetrix）

```
┌─────────────────────────────────────┐
│  cubejs/cube 官方镜像                │
│  ─────────────────────────          │
│  - 固定认证方式                      │
│  - 从文件系统加载 schema             │
│  - 连接单一数据源                    │
│  - 基础权限控制                      │
└────────────┬───────────────────────┘
             │
             ↓
    ┌────────────────┐
    │  单一数据源     │
    │  PostgreSQL    │
    └────────────────┘
```

## Docker Compose 配置详解

### 1. Cube.js 服务配置（自己构建）

```yaml
# docker-compose.dev.yml:44-63
cubejs:
  build:
    context: ./services/cubejs  # 从本地 Dockerfile 构建，不用官方镜像
  restart: always
  command: yarn start.dev
  volumes:
    - ./services/cubejs/src:/app/src            # 挂载自定义代码
    - ./services/cubejs/index.js:/app/index.js
  ports:
    - 4000:4000      # REST API 端口
    - 9231:9229      # Node.js 调试端口
    - 13306:13306    # MySQL 协议 SQL API
    - 15432:15432    # PostgreSQL 协议 SQL API
  env_file:
    - .env
    - .dev.env
  environment:
    CUBEJS_SCHEDULED_REFRESH: false  # 主服务不做定时刷新
  networks:
    - synmetrix_default
```

**为什么不用 `image: cubejs/cube`？**

因为需要运行**自定义代码**，官方镜像无法满足需求。

### 2. Cubestore 配置（使用官方镜像）

```yaml
# docker-compose.dev.yml:201-211
cubestore:
  image: cubejs/cubestore:${CUBESTORE_VERSION}  # 使用官方镜像 v1.2.3
  restart: always
  ports:
    - 3030:3030  # Cubestore 默认端口
  environment:
    - CUBESTORE_REMOTE_DIR=/cube/data
  volumes:
    - .cubestore:/cube/data  # 持久化存储
  networks:
    - synmetrix_default
```

**为什么用官方镜像？**

Cubestore 是一个**独立的专用数据库**，功能稳定，无需定制。

### 3. Cube.js 刷新 Worker（可选）

```yaml
# docker-compose.dev.yml:65-81
cubejs_refresh_worker:
  build:
    context: ./services/cubejs  # 同样的代码
  restart: always
  command: yarn start.dev
  volumes:
    - ./services/cubejs/src:/app/src
    - ./services/cubejs/index.js:/app/index.js
  env_file:
    - .env
    - .dev.env
  environment:
    CUBEJS_REFRESH_TIMER: 60              # 每 60 秒检查一次
    CUBEJS_SCHEDULED_REFRESH: true        # 负责定时刷新
    CUBEJS_SQL_API: false                 # 不提供 SQL API
  networks:
    - synmetrix_default
```

**作用**：专门负责预聚合的定时刷新，避免影响主服务的查询性能。

## Cube.js 自定义构建详解

### Dockerfile 分析

```dockerfile
# services/cubejs/Dockerfile
FROM node:18.20.4-bullseye

# 安装 Databricks JDBC 驱动下载地址
ARG DATABRICKS_JDBC_URL=https://databricks-bi-artifacts.s3.us-east-2.amazonaws.com/simbaspark-drivers/jdbc/2.6.32/DatabricksJDBC42-2.6.32.1054.zip

# 安装系统依赖
RUN DEBIAN_FRONTEND=noninteractive \
    && apt-get update \
    && apt-get install -y --no-install-recommends rxvt-unicode libssl1.1 \
    && rm -rf /var/lib/apt/lists/*

ENV TERM rxvt-unicode
ENV NODE_ENV production

# 安装构建工具 (支持原生模块)
RUN apt-get update \
    && apt-get install -y python3 gcc g++ make cmake libc-bin libc6 \
    && rm -rf /var/lib/apt/lists/*

# 安装 Java 8 (支持 JDBC 驱动，如 Databricks)
RUN apt-get update && \
    apt-get install -y wget apt-transport-https ca-certificates gnupg && \
    wget -qO - https://apt.corretto.aws/corretto.key | apt-key add - && \
    echo "deb https://apt.corretto.aws stable main" | tee /etc/apt/sources.list.d/corretto.list

RUN apt-get update && apt-get install -y java-1.8.0-amazon-corretto-jdk

WORKDIR /app
ENV PATH /app/node_modules/.bin:$PATH

# 安装 Node.js 依赖
COPY yarn.lock /app/yarn.lock
COPY package.json /app/package.json
RUN yarn --network-timeout 100000

# 复制自定义代码
COPY index.js /app/
COPY src/ /app/src/

# 下载并安装 Databricks JDBC 驱动
RUN apt-get update && apt-get install -y wget unzip && \
    wget -qO /tmp/DatabricksJDBC.zip "${DATABRICKS_JDBC_URL}" && \
    unzip /tmp/DatabricksJDBC.zip -d /app && \
    rm /tmp/DatabricksJDBC.zip

CMD ["yarn", "start.dev"]
```

**关键点**：
- 安装 Java 支持 JDBC 连接（Databricks、Hive 等）
- 安装编译工具支持原生 Node.js 模块
- 复制自定义代码而非使用预构建镜像

### package.json 依赖分析

```json
{
  "dependencies": {
    // Cube.js 核心库（作为依赖，非镜像）
    "@cubejs-backend/server-core": "^1.2.3",
    "@cubejs-backend/api-gateway": "^1.2.3",
    "@cubejs-backend/query-orchestrator": "^1.2.3",

    // Cubestore 驱动（连接到 cubestore 容器）
    "@cubejs-backend/cubestore-driver": "^1.2.3",

    // 20+ 种数据库驱动
    "@cubejs-backend/postgres-driver": "^1.2.3",
    "@cubejs-backend/mysql-driver": "^1.2.3",
    "@cubejs-backend/clickhouse-driver": "^1.2.3",
    "@cubejs-backend/bigquery-driver": "^1.2.3",
    "@cubejs-backend/snowflake-driver": "^1.2.3",
    "@cubejs-backend/athena-driver": "^1.2.3",
    "@cubejs-backend/databricks-jdbc-driver": "^1.2.3",
    "@cubejs-backend/redshift-driver": "^1.2.3",
    "@cubejs-backend/elasticsearch-driver": "^1.2.3",
    "@cubejs-backend/druid-driver": "^1.2.3",
    "@cubejs-backend/duckdb-driver": "^1.2.3",
    // ... 更多驱动

    // Web 框架和工具
    "express": "^4.18.2",
    "body-parser": "^1.19.0",
    "swagger-ui-express": "^5.0.0",

    // 认证相关
    "jsonwebtoken": "^9.0.0",
    "jose": "^4.11.2",

    // 数据库客户端
    "pg": "^8.7.1",
    "ioredis": "^5.3.2",
    "redis": "^4.6.4",

    // 工具库
    "jsum": "^2.0.0-alpha.3",
    "ramda": "^0.29.0",
    "uuid": "^9.0.0",
    "yaml": "^2.3.4"
  }
}
```

**关键理解**：
- `@cubejs-backend/server-core` 是一个**库**，不是完整的应用
- Synmetrix 将它作为依赖引入，然后编写自己的 `index.js` 和 `src/` 代码
- 这与使用 `cubejs/cube` 官方镜像完全不同

### 自定义代码入口

```javascript
// services/cubejs/index.js (关键部分)
import ServerCore from "@cubejs-backend/server-core";
import express from "express";

// 导入自定义模块
import routes from "./src/routes/index.js";
import { checkAuth } from "./src/utils/checkAuth.js";
import checkSqlAuth from "./src/utils/checkSqlAuth.js";
import driverFactory from "./src/utils/driverFactory.js";
import queryRewrite from "./src/utils/queryRewrite.js";
import repositoryFactory from "./src/utils/repositoryFactory.js";
import scheduledRefreshContexts from "./src/utils/scheduledRefreshContexts.js";

const app = express();

// 多租户隔离函数
const contextToOrchestratorId = ({ securityContext }) =>
  `CUBEJS_APP_${securityContext?.userScope?.dataSource?.dataSourceVersion}_${securityContext?.userScope?.dataSource?.schemaVersion}`;

const contextToAppId = ({ securityContext }) =>
  `CUBEJS_APP_${securityContext?.userScope?.dataSource?.dataSourceVersion}_${securityContext?.userScope?.dataSource?.schemaVersion}`;

// 预聚合 schema 隔离
const preAggregationsSchema = ({ securityContext }) =>
  `pre_aggregations_${securityContext?.userScope?.dataSource?.preAggregationSchema}`;

// Cubestore 连接配置
const externalDriverFactory = async () =>
  ServerCore.createDriver("cubestore", {
    host: CUBEJS_CUBESTORE_HOST,  // "cubestore" (容器名)
    port: CUBEJS_CUBESTORE_PORT,  // 3030
  });

// Cube.js 配置选项（高度定制）
const options = {
  queryRewrite,              // 自定义权限控制
  contextToAppId,            // 自定义应用隔离
  contextToOrchestratorId,   // 自定义编排器隔离
  dbType,                    // 动态数据库类型
  checkAuth,                 // 自定义 REST API 认证
  driverFactory,             // 自定义驱动工厂
  repositoryFactory,         // 自定义 schema 仓库（从数据库加载）
  preAggregationsSchema,     // 自定义预聚合隔离
  scheduledRefreshContexts,  // 自定义刷新上下文列表
  externalDbType: "cubestore",
  externalDriverFactory,     // 连接 Cubestore
  cacheAndQueueDriver: "cubestore",
  // SQL API 配置
  pgSqlPort: 15432,
  sqlPort: 13306,
  checkSqlAuth,              // 自定义 SQL API 认证
  // ... 更多配置
};

// 创建 Cube.js 实例
const cubejs = new ServerCore(options);

// 挂载自定义路由
app.use(routes({ basePath: '/api', cubejs }));

// 初始化 Cube.js 标准路由
cubejs.initApp(app);

// 初始化 SQL Server
if (String(CUBEJS_SQL_API) === "true") {
  const sqlServer = cubejs.initSQLServer();
  sqlServer.init(options);
}

app.listen(4000);
```

**与官方 `cubejs/cube` 的区别**：

| 特性 | 官方 cubejs/cube 镜像 | Synmetrix 自定义构建 |
|------|---------------------|---------------------|
| Schema 来源 | 文件系统 (`/cube/conf/schema/*.js`) | PostgreSQL 数据库 |
| 认证方式 | 环境变量配置的简单 JWT | 集成 Hasura，自定义 `checkAuth` |
| 多租户 | 不支持 | 完全隔离（orchestrator、cache、schema） |
| 数据源 | 单一数据源 | 动态多数据源 |
| 权限控制 | 基础 | 字段级别细粒度控制 |
| 自定义路由 | 不支持 | 支持 (`/api/v1/generate-models` 等) |
| 代码可见性 | 黑盒 | 完全可控 |

## Cubestore 的作用

### 什么是 Cubestore？

根据 README.md:180-191：

> **Cube Store** is a purpose-built database for operational analytics, optimized for fast aggregations and time series data. It provides:
>
> - **Distributed querying** for scalability
> - **Advanced caching** for fast queries
> - **Columnar storage** for analytics performance
> - **Integration with Cube** for modeling

### Cubestore 在架构中的位置

```
用户查询
    ↓
Cube.js 服务
    ↓
    ├─→ 检查 Cubestore 缓存
    │   └─→ 缓存命中 → 直接返回 ✓
    │
    ├─→ 检查预聚合表（存储在 Cubestore）
    │   └─→ 预聚合存在 → 从 Cubestore 查询 ✓
    │
    └─→ 缓存未命中
        └─→ 查询源数据库 (PostgreSQL/MySQL/等)
            └─→ 将结果缓存到 Cubestore
            └─→ 如果配置了预聚合，创建预聚合表
```

### Cubestore 的具体用途

#### 1. 预聚合存储

```javascript
// services/cubejs/index.js:46-47
const preAggregationsSchema = ({ securityContext }) =>
  `pre_aggregations_${securityContext?.userScope?.dataSource?.preAggregationSchema}`;
```

**存储位置**：
```
Cubestore
  └─ pre_aggregations_abc123  (数据源 A)
      ├─ orders_by_day
      ├─ revenue_by_month
      └─ user_metrics
  └─ pre_aggregations_def456  (数据源 B)
      ├─ sales_summary
      └─ product_stats
```

**预聚合定义示例** (在 Cube schema 中):
```yaml
# Orders.yml
cubes:
  - name: Orders
    # ...

    preAggregations:
      - name: ordersByDay
        measures:
          - count
          - totalAmount
        dimensions:
          - status
        timeDimension: createdAt
        granularity: day
        refreshKey:
          every: 1 hour
```

**效果**：
- 原始查询：扫描 1000 万行订单数据，耗时 30 秒
- 使用预聚合：查询 365 行汇总数据，耗时 0.1 秒

#### 2. 查询结果缓存

```javascript
// 配置
cacheAndQueueDriver: "cubestore"
```

**工作流程**：
1. 用户查询：`SELECT status, COUNT(*) FROM Orders GROUP BY status`
2. Cube.js 生成查询签名：`md5(query + securityContext)`
3. 检查 Cubestore 是否有该签名的缓存
4. 如果有且未过期，直接返回
5. 如果没有，执行查询并缓存结果

#### 3. 分布式队列

```javascript
externalDbType: "cubestore",
externalDriverFactory: async () =>
  ServerCore.createDriver("cubestore", {
    host: CUBEJS_CUBESTORE_HOST,
    port: CUBEJS_CUBESTORE_PORT,
  })
```

**多实例场景**：
```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Cube.js 1  │     │  Cube.js 2  │     │  Cube.js 3  │
└──────┬──────┘     └──────┬──────┘     └──────┬──────┘
       │                   │                   │
       └───────────────────┼───────────────────┘
                           ↓
                   ┌───────────────┐
                   │   Cubestore   │
                   │  ────────────  │
                   │  - 队列管理    │
                   │  - 锁机制      │
                   │  - 缓存共享    │
                   └───────────────┘
```

**好处**：
- 避免重复查询（多个实例共享缓存）
- 预聚合构建协调（不会重复构建）
- 查询队列管理（避免数据库过载）

### Cube.js 连接 Cubestore 的代码

```javascript
// services/cubejs/index.js
const {
  CUBEJS_CUBESTORE_PORT = 3030,
  CUBEJS_CUBESTORE_HOST = "cubestore",
} = process.env;

const externalDriverFactory = async () =>
  ServerCore.createDriver("cubestore", {
    host: CUBEJS_CUBESTORE_HOST,  // Docker 网络中的容器名
    port: CUBEJS_CUBESTORE_PORT,  // 3030
  });

const options = {
  // ...
  externalDbType: "cubestore",
  externalDriverFactory,
  cacheAndQueueDriver: "cubestore",
  // ...
};
```

**环境变量配置** (.env):
```bash
CUBESTORE_VERSION=v1.2.3
CUBEJS_CUBESTORE_HOST=cubestore
CUBEJS_CUBESTORE_PORT=3030
```

## 为什么不能用 cubejs/cube 官方镜像？

### 官方镜像的典型用法

```yaml
# 标准 Cube.js 部署
version: '3.8'
services:
  cube:
    image: cubejs/cube:latest
    ports:
      - 4000:4000
    environment:
      - CUBEJS_DB_TYPE=postgres
      - CUBEJS_DB_HOST=database
      - CUBEJS_DB_NAME=mydb
      - CUBEJS_DB_USER=user
      - CUBEJS_DB_PASS=password
      - CUBEJS_API_SECRET=secret
    volumes:
      - ./schema:/cube/conf/schema  # Schema 文件目录
```

**Schema 文件示例** (`schema/Orders.js`):
```javascript
// /cube/conf/schema/Orders.js
cube(`Orders`, {
  sql: `SELECT * FROM orders`,

  measures: {
    count: {
      type: `count`
    }
  },

  dimensions: {
    status: {
      sql: `status`,
      type: `string`
    }
  }
});
```

### 官方镜像的限制

| 限制 | 说明 | Synmetrix 需求 |
|------|------|---------------|
| **Schema 来源** | 必须是文件系统中的 `.js` 文件 | 需要从数据库加载，支持版本控制 |
| **认证方式** | 简单的 API Secret 或基础 JWT | 需要集成 Hasura，支持复杂的用户权限 |
| **数据源配置** | 环境变量配置单一数据源 | 需要运行时动态切换多个数据源 |
| **权限控制** | 基于 `queryRewrite`，但需自己扩展 | 需要字段级别的细粒度控制 |
| **多租户** | 不支持 | 需要完全隔离的多租户架构 |
| **自定义路由** | 无法添加 | 需要 `/api/v1/generate-models` 等自定义端点 |
| **定制能力** | 配置受限 | 需要完全控制初始化流程 |

### Synmetrix 的定制需求列表

#### 1. 动态 Schema 加载

**官方方式**：
```javascript
// 从文件系统读取
/cube/conf/schema/Orders.js
/cube/conf/schema/Users.js
```

**Synmetrix 方式**：
```javascript
// services/cubejs/src/utils/repositoryFactory.js
const repositoryFactory = ({ securityContext }) => {
  return {
    dataSchemaFiles: async () => {
      const ids = securityContext?.userScope?.dataSource?.files;
      // 从 PostgreSQL 查询
      const dataSchemas = await findDataSchemasByIds({ ids });
      return dataSchemas.map(schema => ({
        fileName: `${schema.name}.yml`,
        content: schema.code
      }));
    }
  };
};
```

#### 2. 自定义认证

**官方方式**：
```javascript
// 环境变量
CUBEJS_API_SECRET=mysecret
```

**Synmetrix 方式**：
```javascript
// services/cubejs/src/utils/checkAuth.js
const checkAuth = async (req) => {
  const authHeader = req.headers.authorization;
  const dataSourceId = req.headers["x-hasura-datasource-id"];
  const branchId = req.headers["x-hasura-branch-id"];

  // 验证 JWT
  const jwtDecoded = jwt.verify(authToken, JWT_KEY, {
    algorithms: [JWT_ALGORITHM]
  });

  const userId = jwtDecoded.hasura["x-hasura-user-id"];

  // 从 Hasura GraphQL 查询用户数据
  const user = await findUser({ userId });

  // 构建安全上下文
  const userScope = defineUserScope(
    user.dataSources,
    user.members,
    dataSourceId,
    branchId
  );

  req.securityContext = {
    authToken,
    userId,
    userScope
  };
};
```

#### 3. 动态驱动工厂

**官方方式**：
```javascript
// 固定数据源
CUBEJS_DB_TYPE=postgres
CUBEJS_DB_HOST=localhost
```

**Synmetrix 方式**：
```javascript
// services/cubejs/src/utils/driverFactory.js
const driverFactory = async ({ securityContext, dataSource }) => {
  // 从安全上下文获取数据库配置
  const dbParams = userScope.dataSource.dbParams;
  const dbType = userScope.dataSource.dbType;

  // 预处理参数（支持 20+ 种数据库）
  dbParams = prepareDbParams(dbParams, dbType);

  // 动态导入驱动
  const driverModule = await import(DriverDependencies[dbType]);

  return new driverModule.default(dbParams);
};
```

#### 4. 权限控制

**官方方式**：
```javascript
// 基础 queryRewrite
queryRewrite: (query, { securityContext }) => {
  if (!securityContext.userId) {
    throw new Error("Unauthorized");
  }
  return query;
}
```

**Synmetrix 方式**：
```javascript
// services/cubejs/src/utils/queryRewrite.js
const queryRewrite = async (query, { securityContext }) => {
  const { role, dataSourceAccessList } = securityContext.userScope;

  // Owner/Admin 跳过
  if (["owner", "admin"].includes(role)) {
    return query;
  }

  // 检查数据源访问权限
  if (!dataSourceAccessList) {
    throw new Error("403: No access to datasource");
  }

  // 提取查询字段
  const queryFields = [
    ...query.measures,
    ...query.dimensions,
    ...query.segments
  ];

  // 提取允许的字段
  const allowedFields = Object.values(dataSourceAccessList).flatMap(
    cube => [...cube.measures, ...cube.dimensions, ...cube.segments]
  );

  // 逐个检查
  queryFields.forEach(field => {
    if (!allowedFields.includes(field)) {
      throw new Error(`403: No access to "${field}"`);
    }
  });

  return query;
};
```

#### 5. 多租户隔离

**官方方式**：
不支持

**Synmetrix 方式**：
```javascript
const contextToOrchestratorId = ({ securityContext }) =>
  `CUBEJS_APP_${securityContext.userScope.dataSource.dataSourceVersion}_${securityContext.userScope.dataSource.schemaVersion}`;

const contextToAppId = ({ securityContext }) =>
  `CUBEJS_APP_${securityContext.userScope.dataSource.dataSourceVersion}_${securityContext.userScope.dataSource.schemaVersion}`;

const preAggregationsSchema = ({ securityContext }) =>
  `pre_aggregations_${securityContext.userScope.dataSource.preAggregationSchema}`;
```

**效果**：
- 数据源 A 和数据源 B 完全隔离
- 独立的查询队列、缓存、schema 编译
- 独立的预聚合表

#### 6. 自定义路由

**官方方式**：
无法添加

**Synmetrix 方式**：
```javascript
// services/cubejs/src/routes/index.js
export default ({ basePath, cubejs }) => {
  router.post(`${basePath}/v1/run-sql`, checkAuthMiddleware, (req, res) =>
    runSql(req, res, cubejs)
  );

  router.get(`${basePath}/v1/test`, checkAuthMiddleware, (req, res) =>
    testConnection(req, res, cubejs)
  );

  router.get(`${basePath}/v1/get-schema`, checkAuthMiddleware,
    async (req, res) => getSchema(req, res, cubejs)
  );

  router.post(`${basePath}/v1/generate-models`, checkAuthMiddleware,
    async (req, res) => generateDataSchema(req, res, cubejs)
  );

  router.post(`${basePath}/v1/pre-aggregation-preview`, checkAuthMiddleware,
    async (req, res) => preAggregationPreview(req, res, cubejs)
  );

  router.get(`${basePath}/v1/pre-aggregations`, checkAuthMiddleware,
    async (req, res) => preAggregations(req, res, cubejs)
  );

  return router;
};
```

## 总结对比

### 组件职责对比

| 组件 | 镜像来源 | 主要职责 | 定制程度 | 语言 |
|------|---------|---------|---------|------|
| **Cube.js 服务** | 自己构建 | 查询引擎、API 服务、认证、权限、Schema 管理 | 高度定制 | Node.js |
| **Cubestore** | 官方镜像 | 缓存数据库、预聚合存储、分布式查询 | 无需定制 | Rust |
| **PostgreSQL** | 官方镜像 | 元数据存储（用户、数据源、schema） | 无需定制 | C |
| **Redis** | 官方镜像 | 会话缓存、临时数据 | 无需定制 | C |

### 架构优势

**使用自定义 Cube.js + 官方 Cubestore 的好处**：

1. **完全控制 Cube.js 行为**
   - 自定义认证流程
   - 动态加载 schema
   - 多租户隔离
   - 细粒度权限

2. **保留 Cubestore 的性能优势**
   - 高性能列式存储
   - 分布式查询能力
   - 预聚合优化
   - 官方持续优化

3. **清晰的职责分离**
   - Cube.js：业务逻辑、认证、权限
   - Cubestore：数据存储、缓存、性能

4. **灵活性和稳定性兼得**
   - 业务逻辑完全可控
   - 存储层稳定可靠

### 最终答案总结

**问题**：为什么 Synmetrix 使用 `cubejs/cubestore` 而不是 `cubejs/cube`？

**答案**：

1. **Synmetrix 同时使用两个组件**：
   - **Cube.js 服务**（自建）：基于 `@cubejs-backend/server-core` 库自己构建
   - **Cubestore**（官方）：使用 `cubejs/cubestore` 官方镜像

2. **为什么不用 `cubejs/cube` 官方镜像**：
   - 需要实现企业级多租户语义层的复杂功能
   - 需要从数据库动态加载 schema
   - 需要集成 Hasura 认证系统
   - 需要细粒度的权限控制
   - 需要支持运行时动态多数据源
   - 需要自定义 API 路由

3. **为什么用 `cubejs/cubestore` 官方镜像**：
   - Cubestore 是独立的专用数据库
   - 功能稳定，无需定制
   - 提供高性能的预聚合和缓存能力
   - 官方持续优化和维护

4. **架构模式**：
   ```
   自定义 Cube.js 服务 (基于 @cubejs-backend/server-core)
            +
   官方 Cubestore (cubejs/cubestore)
            +
   官方 PostgreSQL (postgres)
            +
   官方 Redis (redis)
   ```

这种架构使 Synmetrix 能够在保持 Cube.js 强大分析能力的同时，实现企业级的多租户、权限控制、动态配置等高级功能。
