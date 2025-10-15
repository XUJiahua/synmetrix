# Cube.js 服务深度分析

## 概述

`services/cubejs/` 是 Synmetrix 的**核心分析引擎服务**，基于 Cube.js v1.2.3 构建。它是整个平台的数据查询和分析引擎核心，负责将来自不同数据源的数据统一建模并提供查询能力。

## 核心功能

### 1. 语义层和数据建模引擎

- **统一语义层**: 基于 Cube.js 提供统一的语义层，将多个数据源的指标统一管理
- **动态模型加载**: 数据模型(schema)从数据库动态加载，而非传统的文件系统方式
- **版本管理**: 支持数据模型的版本管理，通过 `schemaVersion` 跟踪变更

**关键实现** (`index.js:35-47`):
```javascript
const schemaVersion = ({ securityContext }) =>
  securityContext?.userScope?.dataSource?.schemaVersion;

const contextToAppId = ({ securityContext }) =>
  `CUBEJS_APP_${securityContext?.userScope?.dataSource?.dataSourceVersion}_${securityContext?.userScope?.dataSource?.schemaVersion}`;
```

### 2. 多数据源支持

支持 20+ 种数据库驱动，通过 `driverFactory` 动态创建对应的数据库连接:

#### 支持的数据源类型

**云数据仓库**:
- Snowflake
- BigQuery
- Redshift
- Databricks
- Athena

**传统数据库**:
- PostgreSQL
- MySQL
- MS SQL Server
- Oracle (via JDBC)

**分析型数据库**:
- ClickHouse
- DuckDB
- Druid
- Vertica
- QuestDB

**其他**:
- MongoDB (via MongoBI)
- Elasticsearch
- Firebolt
- Materialize
- Trino/Presto

#### 驱动工厂实现

**文件**: `src/utils/driverFactory.js`

```javascript
const driverFactory = async ({ securityContext, dataSource }) => {
  const { userScope, user } = securityContext || {};

  // 从安全上下文获取数据库参数和类型
  let dbParams = userScope.dataSource.dbParams;
  let dbType = userScope.dataSource.dbType;

  // 特殊处理 Vertica
  if (dbType === "vertica") {
    return new VerticaDriver(dbParams);
  }

  // 动态导入驱动模块
  const dbDriver = DriverDependencies[dbType];
  const driverModule = await import(dbDriver);

  // 特殊处理 Databricks JDBC
  if (dbType === "databricks-jdbc") {
    return new driverModule.DatabricksDriver(dbParams);
  }

  return new driverModule.default(dbParams);
};
```

**特点**:
- 动态导入驱动，避免加载所有驱动
- 错误处理机制，驱动失败时返回错误对象
- 支持多数据源切换

### 3. REST API 服务

**基础路径**: `/api` (端口 4000)

#### API 端点

**文件**: `src/routes/index.js`

| 端点 | 方法 | 功能 | 实现文件 |
|------|------|------|----------|
| `/api/v1/run-sql` | POST | 执行 SQL 查询 | `runSql.js` |
| `/api/v1/test` | GET | 测试数据源连接 | `testConnection.js` |
| `/api/v1/get-schema` | GET | 获取数据模型 | `getSchema.js` |
| `/api/v1/generate-models` | POST | 自动生成数据模型 | `generateDataSchema.js` |
| `/api/v1/pre-aggregations` | GET | 预聚合管理 | `preAggregations.js` |
| `/api/v1/pre-aggregation-preview` | POST | 预聚合预览 | `preAggregationPreview.js` |

所有端点都受 `checkAuthMiddleware` 保护，需要有效的 JWT token。

#### Swagger 文档

服务提供 Swagger UI 文档:
- 路径: `/docs`
- 定义文件: `src/swagger.yaml`

### 4. SQL API 服务

允许使用标准 SQL 客户端连接并查询 Cube.js:

**配置** (`index.js:82-85`):
```javascript
// MySQL 协议
sqlPort: parseInt(CUBEJS_SQL_PORT, 10),  // 默认 13306

// PostgreSQL 协议
pgSqlPort: parseInt(CUBEJS_PG_SQL_PORT, 10),  // 默认 15432

// 认证
checkSqlAuth,
canSwitchSqlUser: () => false,
```

**认证实现**: `src/utils/checkSqlAuth.js`

**使用场景**:
- BI 工具连接 (Tableau, Power BI, Metabase 等)
- SQL 客户端查询 (DBeaver, DataGrip 等)
- ETL 工具集成

### 5. 多租户隔离架构

通过安全上下文实现完整的租户隔离:

#### Orchestrator 隔离

**实现** (`index.js:37-38`):
```javascript
const contextToOrchestratorId = ({ securityContext }) =>
  `CUBEJS_APP_${securityContext?.userScope?.dataSource?.dataSourceVersion}_${securityContext?.userScope?.dataSource?.schemaVersion}`;
```

**作用**:
- 每个数据源版本创建独立的 Cube.js orchestrator 实例
- 隔离查询队列、缓存、预聚合
- 避免不同租户之间的资源竞争

#### Schema 编译隔离

**实现** (`index.js:40-41`):
```javascript
const contextToAppId = ({ securityContext }) =>
  `CUBEJS_APP_${securityContext?.userScope?.dataSource?.dataSourceVersion}_${securityContext?.userScope?.dataSource?.schemaVersion}`;
```

**作用**:
- 为每个数据源+schema 版本创建独立的编译上下文
- Schema 变更不影响其他租户
- 支持并行编译和加载

#### 预聚合隔离

**实现** (`index.js:46-47`):
```javascript
const preAggregationsSchema = ({ securityContext }) =>
  `pre_aggregations_${securityContext?.userScope?.dataSource?.preAggregationSchema}`;
```

**作用**:
- 每个数据源的预聚合存储在独立的 schema
- 避免表名冲突
- 便于管理和清理

## 关键技术实现

### 动态 Schema 加载

**文件**: `src/utils/repositoryFactory.js`

传统 Cube.js 从文件系统加载 schema，Synmetrix 实现了从数据库动态加载:

```javascript
const repositoryFactory = ({ securityContext }) => {
  return {
    dataSchemaFiles: async () => {
      // 从安全上下文获取 schema 文件 ID 列表
      const ids = securityContext?.userScope?.dataSource?.files;

      // 从数据库查询 schema 定义
      const dataSchemas = await findDataSchemasByIds({ ids });

      // 映射为 Cube.js 文件格式
      return dataSchemas.map(mapSchemaToFile);
    },
  };
};
```

**流程**:
1. 安全上下文包含当前数据源的 schema 文件 ID 列表
2. 通过 GraphQL 查询从数据库获取 schema 内容
3. `mapSchemaToFile` 将数据库记录转换为 Cube.js 文件格式
4. Cube.js 编译并加载 schema

**优势**:
- Schema 存储在数据库，便于版本管理
- 支持 Web UI 编辑 schema
- 多租户共享代码，schema 独立

### 安全认证流程

**文件**: `src/utils/checkAuth.js`

完整的 JWT 认证和安全上下文构建:

#### 认证流程

1. **Token 验证**
   - 从 Authorization header 提取 JWT token
   - 使用 `CUBEJS_SECRET` 验证签名
   - 解码获取 Hasura claims

2. **用户数据查询**
   - 提取 `x-hasura-user-id`
   - 通过 GraphQL 查询用户信息
   - 获取用户关联的数据源列表

3. **数据源选择**
   - 从 token 提取 `x-hasura-datasource-id`
   - 或从请求参数获取 `dataSourceId`
   - 验证用户对该数据源的访问权限

4. **构建安全上下文**
   - 使用 `defineUserScope` 构建用户作用域
   - 包含: 用户信息、数据源配置、schema 文件列表
   - 返回完整的 `securityContext` 对象

**安全上下文结构**:
```javascript
{
  user: { id, email, dataSources: [...], members: [...] },
  userScope: {
    dataSource: {
      id,
      dbType,
      dbParams,
      files: [...],  // schema 文件 IDs
      dataSourceVersion,
      schemaVersion,
      preAggregationSchema
    }
  }
}
```

### 查询改写

**文件**: `src/utils/queryRewrite.js`

在查询执行前对查询进行改写和增强:

**可能的功能**:
- 注入行级安全过滤器
- 添加租户隔离条件
- 查询优化和重写
- 审计日志记录

### 数据库参数准备

**文件**: `src/utils/prepareDbParams.js`

将数据源配置转换为驱动所需的连接参数:

**功能**:
- 处理加密的凭证
- 转换参数格式
- 添加默认配置
- 特殊数据库的参数处理

### Schema 映射

**文件**: `src/utils/mapSchemaToFile.js`

将数据库中的 schema 记录转换为 Cube.js 文件格式:

**转换**:
```javascript
// 数据库记录
{
  id: "123",
  name: "orders",
  code: "cube('orders', { ... })",
  checksum: "abc123"
}

// Cube.js 文件格式
{
  fileName: "orders.js",
  content: "cube('orders', { ... })"
}
```

### 预聚合刷新上下文

**文件**: `src/utils/scheduledRefreshContexts.js`

定义定时刷新预聚合的上下文列表:

**配置** (`index.js:71-75`):
```javascript
scheduledRefreshTimer:
  String(CUBEJS_SCHEDULED_REFRESH) !== "false"
    ? parseInt(CUBEJS_REFRESH_TIMER, 10)  // 默认 60 秒
    : undefined,
scheduledRefreshContexts,
```

**作用**:
- 定期刷新预聚合数据
- 保持数据新鲜度
- 提升查询性能

## 服务配置

### 环境变量

**核心配置** (`index.js:17-27`):

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PORT` | 4000 | HTTP API 端口 |
| `CUBEJS_SECRET` | - | API 密钥和 JWT 签名密钥 |
| `CUBEJS_SQL_PORT` | 13306 | MySQL 协议端口 |
| `CUBEJS_PG_SQL_PORT` | 15432 | PostgreSQL 协议端口 |
| `CUBEJS_CUBESTORE_HOST` | - | Cubestore 主机 |
| `CUBEJS_CUBESTORE_PORT` | 3030 | Cubestore 端口 |
| `CUBEJS_TELEMETRY` | false | 遥测数据 |
| `CUBEJS_SCHEDULED_REFRESH` | true | 定时刷新 |
| `CUBEJS_REFRESH_TIMER` | 60 | 刷新间隔(秒) |
| `CUBEJS_SQL_API` | true | 启用 SQL API |

### ServerCore 配置

**完整配置** (`index.js:57-86`):

```javascript
const options = {
  // 查询和上下文
  queryRewrite,
  contextToAppId,
  contextToOrchestratorId,
  dbType,

  // 认证和安全
  checkAuth,
  checkSqlAuth,
  apiSecret: CUBEJS_SECRET,
  canSwitchSqlUser: () => false,

  // Schema 和驱动
  schemaVersion,
  driverFactory,
  repositoryFactory,

  // 预聚合
  preAggregationsSchema,
  scheduledRefreshTimer,
  scheduledRefreshContexts,

  // 外部存储 (Cubestore)
  externalDbType: "cubestore",
  externalDriverFactory,
  cacheAndQueueDriver: "cubestore",

  // SQL API
  pgSqlPort: parseInt(CUBEJS_PG_SQL_PORT, 10),
  sqlPort: parseInt(CUBEJS_SQL_PORT, 10),

  // 其他
  devServer: false,
  basePath: "/api",
  telemetry: CUBEJS_TELEMETRY,
  logger: logging,
};
```

### Cubestore 集成

**外部驱动工厂** (`index.js:49-53`):
```javascript
const externalDriverFactory = async () =>
  ServerCore.createDriver("cubestore", {
    host: CUBEJS_CUBESTORE_HOST,
    port: CUBEJS_CUBESTORE_PORT,
  });
```

**用途**:
- 预聚合存储
- 查询结果缓存
- 查询队列管理
- 分布式查询引擎

## 数据流

### 查询处理流程

```
1. 客户端请求
   ↓
2. checkAuth 中间件
   - 验证 JWT token
   - 查询用户数据
   - 构建安全上下文
   ↓
3. Cube.js API Gateway
   - 解析查询
   - 选择/创建 orchestrator
   ↓
4. Schema 编译
   - repositoryFactory 加载 schema
   - 编译 Cube 定义
   ↓
5. 查询改写
   - queryRewrite 应用安全规则
   - 注入过滤条件
   ↓
6. 驱动执行
   - driverFactory 创建连接
   - 生成 SQL
   - 执行查询
   ↓
7. 缓存处理
   - 检查 Cubestore 缓存
   - 存储查询结果
   ↓
8. 返回结果
```

### Schema 生成流程

```
1. POST /api/v1/generate-models
   ↓
2. generateDataSchema.js
   - 连接数据源
   - 获取表结构
   ↓
3. Cube.js Schema 生成
   - 分析列类型
   - 推断关系
   - 生成 Cube 定义
   ↓
4. 保存到数据库
   - 通过 GraphQL mutation
   - 创建 schema 记录
   ↓
5. 返回生成的 schema
```

## 性能优化

### 预聚合 (Pre-Aggregations)

**配置**:
- 独立 schema 隔离
- 存储在 Cubestore
- 定时刷新机制

**管理 API**:
- `GET /api/v1/pre-aggregations` - 列出预聚合
- `POST /api/v1/pre-aggregation-preview` - 预览预聚合数据

### 缓存策略

**多层缓存**:
1. **查询结果缓存** - Cubestore
2. **Schema 编译缓存** - 内存

**缓存配置**:
```javascript
cacheAndQueueDriver: "cubestore"
```

### 查询优化

- **SQL 生成优化**: Cube.js 智能生成 SQL
- **查询下推**: 尽可能在数据库层面处理
- **并行查询**: 多个 cube 并行加载
- **增量刷新**: 预聚合增量更新

## 错误处理

### 全局错误处理器

**实现** (`index.js:104-108`):
```javascript
app.use((err, req, res, next) => {
  console.error(err.stack);
  res.status(500).send(err.message);
});
```

### 驱动错误处理

**实现** (`driverFactory.js:6-17`):
```javascript
const driverError = (err) => {
  console.error("Driver error:");

  const throwError = () => {
    throw new Error(err?.message || err);
  };

  // 返回带错误方法的对象
  return {
    tablesSchema: throwError,
    testConnection: throwError,
  };
};
```

**策略**:
- 驱动加载失败时返回错误对象
- 方法调用时才抛出异常
- 便于错误定位和调试

## 日志和监控

### 日志系统

**文件**: `src/utils/logging.js`

**配置** (`index.js:79`):
```javascript
logger: logging
```

**功能**:
- 结构化日志
- 不同级别 (debug, info, warn, error)
- 查询日志记录
- 性能指标

### 开发模式

**启动命令** (`package.json:6`):
```json
"start.dev": "nodemon --exitcrash --inspect=0.0.0.0 --max-old-space-size=8096 --max-http-header-size=32768 --watch src --watch index.js"
```

**特性**:
- 文件变更自动重启
- 调试端口开放 (0.0.0.0)
- 增加内存限制 (8GB)
- 增加 HTTP header 限制

**调试端口**: 9231 (Docker 映射)

## 依赖管理

### 核心依赖

**Cube.js 生态**:
```json
"@cubejs-backend/server-core": "^1.2.3",
"@cubejs-backend/api-gateway": "^1.2.3",
"@cubejs-backend/query-orchestrator": "^1.2.3",
"@cubejs-backend/cubestore-driver": "^1.2.3"
```

**数据库驱动** (部分):
```json
"@cubejs-backend/postgres-driver": "^1.2.3",
"@cubejs-backend/mysql-driver": "^1.2.3",
"@cubejs-backend/clickhouse-driver": "^1.2.3",
"@cubejs-backend/bigquery-driver": "^1.2.3",
"@cubejs-backend/snowflake-driver": "^1.2.3"
```

**Web 框架**:
```json
"express": "^4.18.2",
"body-parser": "^1.19.0",
"swagger-ui-express": "^5.0.0"
```

**认证和安全**:
```json
"jsonwebtoken": "^9.0.0",
"jsum": "^2.0.0-alpha.3"
```

**数据库客户端**:
```json
"pg": "^8.7.1",
"pg-connection-string": "^2.2.0"
```

**工具库**:
```json
"ramda": "^0.29.0",
"uuid": "^9.0.0",
"yaml": "^2.3.4",
"unchanged": "^2.2.1"
```

### 版本一致性

**重要**: 所有 `@cubejs-backend/*` 包必须使用相同版本 (1.2.3)，避免兼容性问题。

## 扩展性

### 添加新数据源驱动

1. 在 `package.json` 添加驱动依赖
2. 在 `driverFactory.js` 添加特殊处理逻辑 (如需要)
3. 在 `prepareDbParams.js` 添加参数转换逻辑

### 添加新 API 端点

1. 在 `src/routes/` 创建路由处理器
2. 在 `src/routes/index.js` 注册路由
3. 添加认证中间件 (如需要)
4. 更新 `src/swagger.yaml` 文档

### 自定义查询改写

在 `src/utils/queryRewrite.js` 实现自定义逻辑:
- 行级安全
- 数据脱敏
- 查询限制
- 审计日志

## 最佳实践

### 安全

1. **JWT 密钥管理**
   - 使用强随机密钥
   - 定期轮换
   - 安全存储在环境变量

2. **数据库凭证**
   - 加密存储
   - 最小权限原则
   - 定期审计

3. **API 访问控制**
   - 所有端点需认证
   - 验证数据源访问权限
   - 记录审计日志

### 性能

1. **预聚合策略**
   - 为常用查询创建预聚合
   - 合理设置刷新频率
   - 监控存储使用

2. **缓存配置**
   - 使用 Cubestore 缓存
   - 设置合理的 TTL
   - 预热关键数据

3. **资源管理**
   - 限制并发查询数
   - 设置查询超时
   - 监控内存使用

### 可维护性

1. **Schema 版本管理**
   - 使用版本号跟踪变更
   - 测试 schema 变更影响
   - 保留回滚能力

2. **日志和监控**
   - 记录关键操作
   - 监控性能指标
   - 设置告警规则

3. **文档维护**
   - 更新 Swagger 文档
   - 记录配置变更
   - 维护代码注释

## 故障排查

### 常见问题

1. **Schema 加载失败**
   - 检查数据库连接
   - 验证 schema 文件 IDs
   - 查看编译错误日志

2. **驱动连接失败**
   - 验证数据库参数
   - 检查网络连接
   - 确认驱动版本兼容性

3. **认证失败**
   - 验证 JWT token
   - 检查 Hasura claims
   - 确认用户权限

4. **查询性能慢**
   - 检查预聚合状态
   - 查看查询计划
   - 优化 SQL 生成

### 调试技巧

1. **启用详细日志**
   - 设置日志级别为 debug
   - 查看 SQL 生成日志
   - 监控查询执行时间

2. **使用调试器**
   - 连接到 9231 端口
   - 设置断点调试
   - 检查安全上下文

3. **测试连接**
   - 使用 `/api/v1/test` 端点
   - 检查驱动初始化
   - 验证数据库连接

## 总结

`services/cubejs/` 是 Synmetrix 的核心服务，实现了:

✅ **强大的数据抽象层** - 统一访问 20+ 种数据源
✅ **灵活的多租户架构** - 完全隔离的租户环境
✅ **动态 Schema 管理** - 数据库驱动的模型管理
✅ **高性能查询引擎** - 预聚合和智能缓存
✅ **标准 SQL 接口** - 兼容主流 BI 工具
✅ **企业级安全** - JWT 认证和细粒度权限控制

通过 Cube.js 的强大能力和 Synmetrix 的创新架构，这个服务为整个平台提供了可扩展、高性能的数据分析能力。
