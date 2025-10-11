# Synmetrix 中 Cube.js 的服务调用架构分析

## 一、核心调用流程

### 1. Cube.js 服务初始化 (`services/cubejs/index.js`)

```
启动流程:
├─ 创建 ServerCore 实例
├─ 配置关键回调函数
│  ├─ checkAuth: JWT 认证
│  ├─ driverFactory: 动态数据库驱动创建
│  ├─ repositoryFactory: 动态 schema 加载
│  ├─ queryRewrite: 查询权限控制
│  ├─ scheduledRefreshContexts: 定时刷新上下文
│  └─ checkSqlAuth: SQL API 认证
├─ 初始化 Express 应用 (端口 4000)
├─ 初始化 SQL Server (MySQL: 13306, PostgreSQL: 15432)
└─ 注册自定义路由
```

**关键配置代码**:
```javascript
// services/cubejs/index.js:57-86
const options = {
  queryRewrite,              // 查询权限控制
  contextToAppId,            // 应用隔离 ID
  contextToOrchestratorId,   // 编排器隔离 ID
  dbType,                    // 数据库类型
  devServer: false,
  checkAuth,                 // REST API 认证
  apiSecret: CUBEJS_SECRET,
  basePath: '/api',
  schemaVersion,             // Schema 版本控制
  driverFactory,             // 动态驱动工厂
  repositoryFactory,         // 动态 Schema 仓库
  preAggregationsSchema,     // 预聚合 Schema 隔离
  telemetry: false,
  scheduledRefreshTimer: 60, // 定时刷新间隔
  scheduledRefreshContexts,  // 刷新上下文列表
  externalDbType: 'cubestore',
  externalDriverFactory,     // Cubestore 驱动
  cacheAndQueueDriver: 'cubestore',
  logger: logging,
  // SQL API 配置
  pgSqlPort: 15432,
  sqlPort: 13306,
  canSwitchSqlUser: () => false,
  checkSqlAuth,              // SQL API 认证
};
```

## 二、关键服务调用路径

### 路径 1: Actions → Cube.js (RPC 调用)

```
前端/GraphQL
    ↓
Actions Service (port 3000)
    ↓ (HTTP Request)
    POST /rpc/fetch-meta → fetchMeta.js
    POST /rpc/run-query → runQuery.js
    POST /rpc/gen-schemas → genSchemas.js
    ↓
使用 cubejsApi 客户端 (actions/src/utils/cubejsApi.js)
    ↓ (HTTP + JWT)
Cube.js Service (port 4000)
    ↓
返回数据
```

**调用示例**:

#### 1. fetchMeta - 获取数据模型元数据

```javascript
// actions/src/rpc/fetchMeta.js
export default async (session, input, headers) => {
  const { datasource_id: dataSourceId, branch_id: branchId } = input || {};
  const userId = session?.["x-hasura-user-id"];

  const result = await cubejsApi({
    dataSourceId,
    branchId,
    userId,
    authToken: headers?.authorization
  }).meta();

  return { cubes: result };
};
```

调用 Cube.js 的 `/api/v1/meta` 端点，返回所有 cube 的元数据（measures、dimensions、segments）。

#### 2. runQuery - 执行数据查询

```javascript
// actions/src/rpc/runQuery.js (简化)
export default async (session, input, headers) => {
  const { datasource_id, branch_id, query, limit } = input;
  const userId = session?.["x-hasura-user-id"];

  const result = await cubejsApi({
    dataSourceId: datasource_id,
    branchId: branch_id,
    userId,
    authToken: headers?.authorization
  }).query(query, 'json', { renewQuery });

  return { result };
};
```

内部调用流程:
```javascript
// actions/src/utils/cubejsApi.js:210-257
query: async (playgroundState, fileType = 'json', args = {}) => {
  // 1. 标准化查询格式
  const { query } = normalizeQuery(playgroundState);

  // 2. 根据文件类型选择不同的执行方式
  if (fileType === 'sql') {
    // 返回生成的 SQL，不执行
    const resultSet = await init.sql(query, options);
    return { sql: resultSet.sqlQuery.sql };
  } else {
    // 执行查询并返回数据
    const resultSet = await init.loadMethod(
      () => init.request('load', { query, headers: reqHeaders }),
      (body) => ({ loadResponse: body }),
      options
    );
    return resultSet?.loadResponse;
  }
}
```

#### 3. genSchemas - 生成数据模型

```javascript
// actions/src/rpc/genSchemas.js
export default async (session, input, headers) => {
  const { datasource_id, branch_id, tables, overwrite, format = 'yaml' } = input;
  const userId = session?.["x-hasura-user-id"];

  const result = await cubejsApi({
    dataSourceId: datasource_id,
    userId,
    authToken: headers?.authorization
  }).generateSchemaFiles({ branchId: branch_id, tables, overwrite, format });

  return result;
};
```

调用 `/api/v1/generate-models`，基于数据库表结构自动生成 Cube.js schema。

### 路径 2: 直接 Cube.js API 调用

```
客户端
    ↓ (JWT + Headers)
    Authorization: Bearer <token>
    x-hasura-datasource-id: <uuid>
    x-hasura-branch-id: <uuid>
    ↓
Cube.js Express 中间件
    ↓
checkAuth 认证 (checkAuth.js)
    ├─ 验证 JWT token
    ├─ 从 Hasura 查询用户数据源
    └─ 构建 securityContext
    ↓
自定义路由处理 (routes/index.js)
    POST /api/v1/run-sql
    GET  /api/v1/test
    GET  /api/v1/get-schema
    POST /api/v1/generate-models
    GET  /api/v1/pre-aggregations
    POST /api/v1/pre-aggregation-preview
    ↓
业务逻辑执行
    ↓
返回结果
```

**路由定义**:
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

**runSql 示例**:
```javascript
// services/cubejs/src/routes/runSql.js
export default async (req, res, cubejs) => {
  const { securityContext } = req;

  // 使用 driverFactory 创建数据库驱动
  const driver = await cubejs.options.driverFactory({ securityContext });

  if (!req.body.query) {
    res.status(400).json({
      code: 'query_missing',
      message: 'The query parameter is missing.'
    });
    return;
  }

  try {
    // 直接执行原始 SQL
    const rows = await driver.query(req.body.query);
    res.json(rows);
  } catch (err) {
    res.status(500).json({
      code: 'run_sql_failed',
      message: err.message || err
    });
  }
};
```

### 路径 3: SQL API 调用

```
BI 工具 (DBeaver/Superset/Tableau/等)
    ↓ (MySQL/PostgreSQL 协议)
    连接: localhost:13306 (MySQL) 或 localhost:15432 (PostgreSQL)
    用户名: sql_credentials.username
    密码: sql_credentials.password
    ↓
Cube.js SQL Server
    ↓
checkSqlAuth (checkSqlAuth.js)
    ├─ 从 Hasura 查询 SQL 凭据
    ├─ 验证密码
    └─ 构建 securityContext
    ↓
Cube.js 查询引擎
    ├─ 将 SQL 映射到 Cube schema
    ├─ 生成优化的查询计划
    ├─ 执行查询（可能使用预聚合）
    └─ 返回 SQL 结果集
    ↓
BI 工具显示结果
```

**checkSqlAuth 实现**:
```javascript
// services/cubejs/src/utils/checkSqlAuth.js
const checkSqlAuth = async (_, user) => {
  // 1. 根据用户名查询 SQL 凭据
  const sqlCredentials = await findSqlCredentials(user);

  if (!sqlCredentials) {
    throw new Error("Incorrect user name or password");
  }

  // 2. 构建 SQL 用户的安全上下文
  const dataSourceId = sqlCredentials?.datasource?.id;
  const teamId = sqlCredentials?.datasource?.team_id;
  const allMembers = sqlCredentials?.user?.members;

  const dataSourceAccessList = getDataSourceAccessList(
    allMembers,
    dataSourceId,
    teamId
  );

  const dataSourceContext = buildSecurityContext(sqlCredentials?.datasource);

  // 3. 返回密码和安全上下文
  return {
    password: sqlCredentials?.password,
    securityContext: {
      userId: sqlCredentials?.user_id,
      userScope: {
        dataSource: dataSourceContext,
        ...dataSourceAccessList
      }
    }
  };
};
```

## 三、核心技术机制

### 1. 多租户隔离机制

通过 `contextToOrchestratorId` 实现完全隔离：

```javascript
// services/cubejs/index.js:37-41
const contextToOrchestratorId = ({ securityContext }) =>
  `CUBEJS_APP_${securityContext?.userScope?.dataSource?.dataSourceVersion}_${securityContext?.userScope?.dataSource?.schemaVersion}`;

const contextToAppId = ({ securityContext }) =>
  `CUBEJS_APP_${securityContext?.userScope?.dataSource?.dataSourceVersion}_${securityContext?.userScope?.dataSource?.schemaVersion}`;
```

**实现效果**:
- 数据源 A: `CUBEJS_APP_abc123_xyz789`
- 数据源 B: `CUBEJS_APP_def456_uvw012`
- 完全隔离的查询队列、缓存、schema 编译环境

**隔离的内容**:
- Query queue (查询队列)
- Schema compilation cache (Schema 编译缓存)
- Pre-aggregations (预聚合表)
- Query results cache (查询结果缓存)

### 2. 动态 Schema 加载 (`repositoryFactory`)

**传统 Cube.js**: Schema 存储在文件系统中的 `.js` 或 `.yaml` 文件

**Synmetrix**: Schema 存储在数据库 (PostgreSQL) 的 `dataschemas` 表中

```javascript
// services/cubejs/src/utils/repositoryFactory.js
const repositoryFactory = ({ securityContext }) => {
  return {
    dataSchemaFiles: async () => {
      // 1. 从 securityContext 获取文件 ID 列表
      const ids = securityContext?.userScope?.dataSource?.files;

      // 2. 从 Hasura GraphQL 查询 dataschemas 表
      const dataSchemas = await findDataSchemasByIds({ ids });

      // 3. 转换为 Cube.js 文件格式
      return dataSchemas.map(mapSchemaToFile);
      // 返回格式: [{ fileName: 'Orders.yml', content: '...' }]
    }
  };
};
```

**mapSchemaToFile 转换**:
```javascript
// services/cubejs/src/utils/mapSchemaToFile.js (推断)
const mapSchemaToFile = (dataSchema) => ({
  fileName: `${dataSchema.name}.yml`,
  content: dataSchema.code  // YAML 或 JavaScript 格式的 schema
});
```

**调用时机**:
- 每次查询时，Cube.js 根据 `securityContext` 加载对应的 schema
- Schema 版本变更时，通过 `schemaVersion` hash 触发重新编译
- 支持分支/版本管理 (通过 `x-hasura-branch-id` header)

**查询 Schema 的 GraphQL**:
```graphql
# services/cubejs/src/utils/dataSourceHelpers.js:117-125
query GetSchemas($_in: [uuid!]) {
  dataschemas(where: {id: {_in: $_in}}) {
    code
    name
    id
  }
}
```

### 3. 动态数据库驱动创建 (`driverFactory`)

```javascript
// services/cubejs/src/utils/driverFactory.js:26-71
const driverFactory = async ({ securityContext, dataSource }) => {
  const { userScope, user } = securityContext || {};

  let dbParams;
  let dbType;

  // 1. 确定数据源配置
  if (!dataSource || dataSource === "default") {
    dbParams = userScope.dataSource.dbParams;
    dbType = userScope.dataSource.dbType;
  } else {
    // 支持跨数据源查询
    const nextUserScope = defineUserScope(
      user.dataSources,
      user.members,
      dataSource
    );
    dbParams = nextUserScope.dataSource.dbParams;
    dbType = nextUserScope.dataSource.dbType;
  }

  // 2. 特殊处理 Vertica
  if (dbType === "vertica") {
    return new VerticaDriver(dbParams);
  }

  // 3. 动态导入对应的驱动
  const dbDriver = DriverDependencies[dbType];
  const driverModule = await import(dbDriver);

  // 4. 特殊处理某些驱动
  if (dbType === "druid") {
    driverModule = driverModule.default;
  }

  if (dbType === "databricks-jdbc") {
    return new driverModule.DatabricksDriver(dbParams);
  }

  // 5. 创建驱动实例
  return new driverModule.default(dbParams);
};
```

**支持的数据库** (services/cubejs/package.json):
- PostgreSQL, MySQL, MSSQL
- ClickHouse, DuckDB
- BigQuery, Snowflake, Redshift, Athena
- Databricks, Trino, Presto
- MongoDB (BI Connector), Elasticsearch
- Druid, Firebolt, QuestDB
- 等 20+ 种数据库

**prepareDbParams 预处理**:
```javascript
// services/cubejs/src/utils/prepareDbParams.js (部分)
const prepareDbParams = (dbParams, dbType) => {
  // 统一参数格式
  let dbConfig = Object.keys(dbParams || {})
    .filter((key) => !!dbParams[key])
    .reduce((res, key) => ((res[key] = dbParams[key]), res), {});

  switch (dbType) {
    case 'bigquery':
      // BigQuery 需要解析 keyFile JSON
      dbConfig = {
        ...dbConfig,
        credentials: JSON.parse(dbConfig.keyFile)
      };
      break;

    case 'clickhouse':
      // ClickHouse 特殊参数映射
      dbConfig = {
        host: dbConfig.host,
        port: dbConfig.port,
        username: dbConfig.user,
        password: dbConfig.password,
        protocol: dbConfig.ssl ? 'https:' : 'http:',
        database: dbConfig.database || 'default'
      };
      break;

    case 'athena':
      // AWS Athena 参数映射
      dbConfig = {
        ...dbConfig,
        accessKeyId: dbConfig.awsKey,
        secretAccessKey: dbConfig.awsSecret,
        S3OutputLocation: dbConfig.awsS3OutputLocation,
        region: dbConfig.awsRegion
      };
      break;

    case 'snowflake':
      // Snowflake 需要组合 account
      const account = [dbConfig.orgId, dbConfig.accountId].join('-');
      dbConfig = { ...dbConfig, account };
      break;

    // ... 其他数据库类型
  }

  return dbConfig;
};
```

### 4. 查询权限控制 (`queryRewrite`)

在查询执行前进行权限验证：

```javascript
// services/cubejs/src/utils/queryRewrite.js
const queryRewrite = async (query, { securityContext }) => {
  const { userScope } = securityContext;
  const { dataSourceAccessList, role } = userScope;

  // 1. Owner/Admin 跳过检查
  if (["owner", "admin"].includes(role)) {
    return query;
  }

  // 2. 检查是否有数据源访问权限
  if (!dataSourceAccessList) {
    throw new Error("403: You have no access to the datasource");
  }

  // 3. 提取查询中的所有字段
  const queryNames = [
    ...(query.dimensions || []),
    ...(query.measures || []),
    ...(query.segments || [])
  ];

  // 4. 提取用户有权限访问的字段
  const accessNames = Object.values(dataSourceAccessList).reduce(
    (acc, cube) => [
      ...acc,
      ...(cube.dimensions || []),
      ...(cube.measures || []),
      ...(cube.segments || [])
    ],
    []
  );

  // 5. 逐个检查权限
  queryNames.forEach((fieldName) => {
    if (!accessNames.includes(fieldName)) {
      throw new Error(`403: You have no access to "${fieldName}" cube property`);
    }
  });

  return query;
};
```

**权限数据结构**:
```javascript
// dataSourceAccessList 示例
{
  "Orders": {
    "measures": ["Orders.count", "Orders.totalAmount"],
    "dimensions": ["Orders.status", "Orders.createdAt"],
    "segments": ["Orders.completed"]
  },
  "Users": {
    "measures": ["Users.count"],
    "dimensions": ["Users.email", "Users.name"],
    "segments": []
  }
}
```

权限存储在数据库中:
```
users
  └─ members (团队成员关系)
      └─ member_roles (成员角色)
          └─ access_list (访问列表)
              └─ config.datasources[datasource_id].cubes
```

### 5. Security Context 构建流程

**完整流程图**:

```
HTTP Request Headers:
├─ Authorization: Bearer <JWT>
├─ x-hasura-datasource-id: <uuid>
├─ x-hasura-branch-id: <uuid> (可选)
└─ x-hasura-branch-version-id: <uuid> (可选)
    ↓
checkAuth 解析 (services/cubejs/src/utils/checkAuth.js):
    ↓
├─ 1. 验证 JWT, 提取 userId
│   jwt.verify(authToken, JWT_KEY, { algorithms: [JWT_ALGORITHM] })
│   提取: jwtDecoded.hasura['x-hasura-user-id']
    ↓
├─ 2. 从 Hasura GraphQL 查询用户数据
│   findUser({ userId })
│   ├─ 用户的所有数据源 (dataSources)
│   └─ 用户的团队成员关系 (members)
    ↓
├─ 3. 调用 defineUserScope()
│   ├─ 匹配指定的 dataSourceId
│   ├─ 选择 branch (默认 active branch)
│   ├─ 选择 version (默认最新版本)
│   ├─ 构建 dataSourceContext (buildSecurityContext)
│   │   ├─ dbType: 'postgres'
│   │   ├─ dbParams: { host, port, user, password, database }
│   │   ├─ dataSourceVersion: SHA256(dbType + dbParams)
│   │   ├─ schemaVersion: MD5(file IDs array)
│   │   ├─ preAggregationSchema: MD5(dataSourceId)
│   │   └─ files: [schema IDs]
│   └─ 获取访问权限列表 (getDataSourceAccessList)
│       ├─ 从 member_roles 获取用户角色
│       └─ 提取 access_list.config.datasources[id].cubes
    ↓
└─ 4. 构建最终 securityContext
    req.securityContext = {
      authToken: "jwt_token_string",
      userId: "user-uuid",
      userScope: {
        dataSource: {
          dataSourceId: "ds-uuid",
          dbType: "postgres",
          dbParams: { host, port, ... },
          dataSourceVersion: "abc123...",
          schemaVersion: "xyz789...",
          preAggregationSchema: "pre_aggregations_def456",
          files: ["schema-id-1", "schema-id-2"]
        },
        role: "member" | "admin" | "owner",
        dataSourceAccessList: {
          "Orders": {
            measures: ["Orders.count"],
            dimensions: ["Orders.status"]
          }
        }
      }
    }
```

**buildSecurityContext 详细实现**:
```javascript
// services/cubejs/src/utils/buildSecurityContext.js
const buildSecurityContext = (dataSource, branch, version) => {
  if (!dataSource?.db_params) {
    throw new Error("No dbParams provided");
  }

  // 1. 准备基础数据
  const data = {
    dataSourceId: dataSource.id,
    dbType: dataSource.db_type?.toLowerCase(),
    dbParams: dataSource.db_params
  };

  // 2. 预处理数据库参数
  data.dbParams = prepareDbParams(data.dbParams, data.dbType);

  // 3. 计算数据源版本 (用于隔离)
  const dataSourceVersion = JSum.digest(data, 'SHA256', 'hex');

  // 4. 确定使用哪个版本的 schema
  const dataModels =
    version?.dataschemas ||
    branch?.versions?.[0]?.dataschemas ||
    dataSource.branches?.[0]?.versions?.[0]?.dataschemas ||
    [];

  // 5. 提取 schema 文件 ID 列表
  const files = dataModels.map((schema) => schema.id);

  // 6. 计算 schema 版本 hash
  const schemaVersion = createMd5Hex(files);

  // 7. 计算预聚合 schema 名称
  const preAggregationSchema = createMd5Hex(data.dataSourceId);

  return {
    ...data,
    dataSourceVersion,  // 用于 orchestrator 隔离
    preAggregationSchema,  // 用于预聚合表隔离
    schemaVersion,      // 用于 schema 编译缓存
    files               // schema 文件 ID 列表
  };
};
```

**defineUserScope 详细实现**:
```javascript
// services/cubejs/src/utils/defineUserScope.js
const defineUserScope = (
  allDataSources,
  allMembers,
  selectedDataSourceId,
  selectedBranchId,
  selectedVersionId
) => {
  // 1. 查找指定的数据源
  const dataSource = allDataSources.find(
    (source) => source.id === selectedDataSourceId
  );

  if (!dataSource) {
    throw new Error(`404: source "${selectedDataSourceId}" not found`);
  }

  // 2. 选择分支
  let selectedBranch;
  if (selectedBranchId) {
    selectedBranch = dataSource.branches.find(
      (branch) => branch.id === selectedBranchId
    );
  } else {
    // 默认使用 active 分支
    selectedBranch = dataSource.branches.find(
      (branch) => branch.status === "active"
    );
  }

  // 3. 选择版本
  let selectedVersion;
  if (selectedVersionId) {
    selectedVersion = selectedBranch.versions.find(
      (version) => version.id === selectedVersionId
    );
  }
  // 否则使用最新版本 (默认行为)

  // 4. 获取访问权限列表
  const dataSourceAccessList = getDataSourceAccessList(
    allMembers,
    selectedDataSourceId,
    dataSource.team_id
  );

  // 5. 构建数据源上下文
  const dataSourceContext = buildSecurityContext(
    dataSource,
    selectedBranch,
    selectedVersion
  );

  return {
    dataSource: dataSourceContext,
    ...dataSourceAccessList  // { role, dataSourceAccessList }
  };
};
```

## 四、预聚合 (Pre-Aggregations) 架构

### Schema 隔离

```javascript
// services/cubejs/index.js:46-47
const preAggregationsSchema = ({ securityContext }) =>
  `pre_aggregations_${securityContext?.userScope?.dataSource?.preAggregationSchema}`;
```

**实现效果**:
- 数据源 A: `pre_aggregations_abc123`
- 数据源 B: `pre_aggregations_def456`
- 每个数据源的预聚合存储在独立的 schema 中
- 存储在 Cubestore (分布式缓存数据库)

### 定时刷新机制

```javascript
// services/cubejs/src/utils/scheduledRefreshContexts.js
const scheduledRefreshContexts = async () => {
  // 1. 获取所有活跃数据源
  const dataSources = await getDataSources();

  // 2. 为每个数据源构建刷新上下文
  return (dataSources || []).map((dataSource) => {
    const userScopeDataSource = buildSecurityContext(dataSource);

    return {
      securityContext: {
        userScope: {
          dataSource: userScopeDataSource
        }
      }
    };
  });
};
```

**工作原理**:
1. Cube.js 定时器 (默认 60 秒) 触发
2. 调用 `scheduledRefreshContexts()` 获取所有数据源的上下文
3. 遍历每个上下文，检查预聚合是否需要刷新
4. 如果需要，在对应的 `pre_aggregations_{hash}` schema 中更新预聚合表

**查询所有数据源的 GraphQL**:
```graphql
# services/cubejs/src/utils/dataSourceHelpers.js:74-81
{
  datasources {
    id
    name
    db_type
    db_params
    team_id
    branches(where: {status: {_eq: active}}) {
      id
      name
      status
      versions (limit: 1, order_by: {created_at: desc}) {
        dataschemas {
          id
          name
          code
        }
      }
    }
  }
}
```

## 五、关键调用示例

### 示例 1: 前端执行数据查询 (完整链路)

```
步骤 1: 用户在前端构建查询
─────────────────────────────
{
  measures: ["Orders.count", "Orders.totalAmount"],
  dimensions: ["Orders.status"],
  timeDimensions: [{
    dimension: "Orders.createdAt",
    granularity: "day",
    dateRange: "Last 7 days"
  }],
  filters: [{
    member: "Orders.status",
    operator: "equals",
    values: ["completed"]
  }]
}

步骤 2: 前端调用 Hasura GraphQL Action
─────────────────────────────────────
POST https://localhost/v1/graphql
Headers:
  Authorization: Bearer <user_jwt_token>

Body:
mutation {
  runQuery(input: {
    datasource_id: "550e8400-e29b-41d4-a716-446655440000",
    branch_id: "660e8400-e29b-41d4-a716-446655440000",
    query: {
      measures: ["Orders.count", "Orders.totalAmount"],
      dimensions: ["Orders.status"],
      timeDimensions: [...],
      filters: [...]
    },
    limit: 1000
  }) {
    result
  }
}

步骤 3: Hasura 转发到 Actions Service
─────────────────────────────────────
POST http://actions:3000/rpc/run-query
Headers:
  Authorization: Bearer <user_jwt_token>
  x-request-id: req-123

Body:
{
  session_variables: {
    "x-hasura-user-id": "user-uuid-123",
    "x-hasura-role": "user"
  },
  input: {
    datasource_id: "550e8400-...",
    branch_id: "660e8400-...",
    query: {...},
    limit: 1000
  }
}

步骤 4: Actions 调用 cubejsApi
────────────────────────────────
// actions/src/rpc/runQuery.js
const userId = session["x-hasura-user-id"];
const result = await cubejsApi({
  dataSourceId: input.datasource_id,
  branchId: input.branch_id,
  userId,
  authToken: headers.authorization
}).query(input.query, 'json', { renewQuery: false });

步骤 5: cubejsApi 发送请求到 Cube.js
──────────────────────────────────────
POST http://cubejs:4000/api/v1/load
Headers:
  Authorization: Bearer <user_jwt_token>
  x-hasura-datasource-id: 550e8400-e29b-41d4-a716-446655440000
  x-hasura-branch-id: 660e8400-e29b-41d4-a716-446655440000
  Content-Type: application/json

Body:
{
  query: {
    measures: ["Orders.count", "Orders.totalAmount"],
    dimensions: ["Orders.status"],
    timeDimensions: [{
      dimension: "Orders.createdAt",
      granularity: "day",
      dateRange: ["2024-01-01", "2024-01-07"]
    }],
    filters: [{
      member: "Orders.status",
      operator: "equals",
      values: ["completed"]
    }],
    timezone: "UTC",
    limit: 1000
  }
}

步骤 6: Cube.js 处理流程
───────────────────────

6.1 checkAuth 中间件
  ├─ 验证 JWT token
  ├─ 提取 userId: "user-uuid-123"
  ├─ GraphQL 查询用户数据源
  │   query {
  │     users(where: {id: {_eq: "user-uuid-123"}}) {
  │       datasources { ... }
  │       members { ... }
  │     }
  │   }
  ├─ defineUserScope(dataSources, members, dataSourceId, branchId)
  └─ 构建 req.securityContext

6.2 Cube.js ServerCore 处理
  ├─ contextToOrchestratorId
  │   → "CUBEJS_APP_sha256_md5"
  │   → 选择/创建对应的 orchestrator
  │
  ├─ repositoryFactory.dataSchemaFiles()
  │   → 从数据库加载 schema 文件
  │   → 返回: [{ fileName: 'Orders.yml', content: '...' }]
  │
  ├─ schemaVersion check
  │   → 如果 schema 版本变化，重新编译
  │
  ├─ queryRewrite(query, { securityContext })
  │   → 验证用户对 Orders.count, Orders.totalAmount, Orders.status 的权限
  │   → 通过检查，返回原查询
  │
  ├─ driverFactory({ securityContext })
  │   → 创建 PostgreSQL driver
  │   → 配置: { host: 'db.example.com', port: 5432, ... }
  │
  ├─ 编译查询
  │   → 根据 Orders cube 定义生成 SQL
  │   → 检查是否有可用的预聚合
  │   → 优化查询计划
  │
  ├─ 执行查询
  │   → 如果有预聚合: 从 Cubestore 读取
  │   → 否则: 执行实际 SQL 查询
  │   → 缓存结果
  │
  └─ 返回结果

生成的 SQL 示例:
SELECT
  orders.status AS "Orders.status",
  DATE_TRUNC('day', orders.created_at) AS "Orders.createdAt.day",
  COUNT(*) AS "Orders.count",
  SUM(orders.total_amount) AS "Orders.totalAmount"
FROM public.orders AS orders
WHERE orders.status = 'completed'
  AND orders.created_at >= '2024-01-01'
  AND orders.created_at < '2024-01-08'
GROUP BY 1, 2
ORDER BY 2 ASC
LIMIT 1000

步骤 7: 返回路径
──────────────
Cube.js → Actions → Hasura → 前端

返回数据格式:
{
  "data": [
    {
      "Orders.status": "completed",
      "Orders.createdAt.day": "2024-01-01T00:00:00.000Z",
      "Orders.count": 150,
      "Orders.totalAmount": 15230.50
    },
    {
      "Orders.status": "completed",
      "Orders.createdAt.day": "2024-01-02T00:00:00.000Z",
      "Orders.count": 180,
      "Orders.totalAmount": 18950.75
    }
    // ... 更多数据
  ],
  "annotation": {
    "measures": {
      "Orders.count": { ... },
      "Orders.totalAmount": { ... }
    },
    "dimensions": {
      "Orders.status": { ... }
    },
    "timeDimensions": {
      "Orders.createdAt.day": { ... }
    }
  },
  "hitLimit": false
}
```

### 示例 2: BI 工具通过 SQL API 查询

```
步骤 1: DBeaver 连接配置
───────────────────────
Connection Type: PostgreSQL
Host: localhost
Port: 15432
Database: db
Username: demo_pg_user
Password: demo_pg_pass
SSL: disabled

步骤 2: 用户在 DBeaver 中执行 SQL
────────────────────────────────
SELECT
  status,
  DATE_TRUNC('day', created_at) as day,
  COUNT(*) as order_count,
  SUM(total_amount) as total_revenue
FROM Orders
WHERE status = 'completed'
  AND created_at >= '2024-01-01'
GROUP BY status, day
ORDER BY day
LIMIT 100

步骤 3: Cube.js SQL Server 接收连接
─────────────────────────────────
// Cube.js 内部流程

3.1 PostgreSQL 协议握手
  ├─ 接收连接请求 (port 15432)
  ├─ 解析认证信息
  │   Username: demo_pg_user
  │   Password: demo_pg_pass
  └─ 调用 checkSqlAuth

3.2 checkSqlAuth 处理
  ├─ GraphQL 查询 sql_credentials 表
  │   query {
  │     sql_credentials(where: {username: {_eq: "demo_pg_user"}}) {
  │       id
  │       user_id
  │       password
  │       user {
  │         members {
  │           member_roles {
  │             team_role
  │             access_list { config }
  │           }
  │         }
  │       }
  │       datasource {
  │         id
  │         db_type
  │         db_params
  │         branches(where: {status: {_eq: active}}) {
  │           versions { dataschemas { id, name, code } }
  │         }
  │       }
  │     }
  │   }
  │
  ├─ 验证密码: demo_pg_pass === sqlCredentials.password
  ├─ buildSecurityContext(sqlCredentials.datasource)
  └─ 返回 securityContext

3.3 SQL 解析和转换
  ├─ 解析 SQL 语句
  │   → FROM Orders → 对应 Orders cube
  │   → status → 对应 Orders.status dimension
  │   → COUNT(*) → 对应 Orders.count measure
  │   → SUM(total_amount) → 对应 Orders.totalAmount measure
  │   → DATE_TRUNC('day', created_at) → Orders.createdAt.day
  │
  ├─ 转换为 Cube.js 查询
  │   {
  │     measures: ["Orders.count", "Orders.totalAmount"],
  │     dimensions: ["Orders.status"],
  │     timeDimensions: [{
  │       dimension: "Orders.createdAt",
  │       granularity: "day",
  │       dateRange: ["2024-01-01", "2024-12-31"]
  │     }],
  │     filters: [{
  │       member: "Orders.status",
  │       operator: "equals",
  │       values: ["completed"]
  │     }],
  │     order: { "Orders.createdAt": "asc" },
  │     limit: 100
  │   }
  │
  └─ queryRewrite 权限检查

3.4 查询执行
  ├─ repositoryFactory 加载 Orders schema
  ├─ driverFactory 创建数据库驱动
  ├─ 生成优化的 SQL
  ├─ 检查预聚合
  └─ 执行查询

3.5 结果转换
  ├─ Cube.js 结果格式 → PostgreSQL wire protocol
  ├─ 构建结果集元数据
  │   Columns: [status, day, order_count, total_revenue]
  │   Types: [varchar, timestamp, bigint, numeric]
  └─ 发送数据行

步骤 4: DBeaver 显示结果
───────────────────────
┌───────────┬────────────┬─────────────┬───────────────┐
│ status    │ day        │ order_count │ total_revenue │
├───────────┼────────────┼─────────────┼───────────────┤
│ completed │ 2024-01-01 │ 150         │ 15230.50      │
│ completed │ 2024-01-02 │ 180         │ 18950.75      │
│ completed │ 2024-01-03 │ 165         │ 16780.20      │
│ ...       │ ...        │ ...         │ ...           │
└───────────┴────────────┴─────────────┴───────────────┘
```

### 示例 3: 生成 Cube.js Schema

```
步骤 1: 前端操作
───────────────
用户点击 "从数据库表生成 Schema" 按钮
选择表: orders, users, products
格式: YAML

步骤 2: 调用 genSchemas RPC
──────────────────────────
POST http://actions:3000/rpc/gen-schemas
Headers:
  Authorization: Bearer <jwt>

Body:
{
  session_variables: { "x-hasura-user-id": "user-123" },
  input: {
    datasource_id: "ds-uuid",
    branch_id: "branch-uuid",
    tables: ["orders", "users", "products"],
    overwrite: false,
    format: "yaml"
  }
}

步骤 3: Actions 转发到 Cube.js
────────────────────────────
POST http://cubejs:4000/api/v1/generate-models
Headers:
  Authorization: Bearer <jwt>
  x-hasura-datasource-id: ds-uuid
  x-hasura-branch-id: branch-uuid

Body:
{
  branchId: "branch-uuid",
  tables: ["orders", "users", "products"],
  overwrite: false,
  format: "yaml"
}

步骤 4: Cube.js 生成 Schema
─────────────────────────

4.1 获取表结构
  ├─ driverFactory 创建驱动
  ├─ driver.tablesSchema() 获取表元数据
  └─ 返回: { orders: [...columns], users: [...], products: [...] }

4.2 生成 Cube schema
  ├─ 遍历每个表
  ├─ 分析列类型
  │   ├─ 数字类型 → measures
  │   ├─ 字符串/枚举 → dimensions
  │   ├─ 时间戳 → dimensions (time: true)
  │   └─ 外键 → joins
  │
  └─ 生成 YAML 格式

生成的 Schema 示例 (orders.yml):
cubes:
  - name: Orders
    sql: SELECT * FROM orders

    measures:
      - name: count
        type: count

      - name: totalAmount
        sql: total_amount
        type: sum

      - name: avgAmount
        sql: total_amount
        type: avg

    dimensions:
      - name: id
        sql: id
        type: number
        primaryKey: true

      - name: status
        sql: status
        type: string

      - name: createdAt
        sql: created_at
        type: time

    joins:
      - name: Users
        sql: "{CUBE}.user_id = {Users}.id"
        relationship: manyToOne

4.3 保存到数据库
  ├─ 创建新的 version 记录
  ├─ 为每个表创建 dataschema 记录
  │   ├─ name: "Orders"
  │   ├─ code: <生成的 YAML 内容>
  │   └─ version_id: <新版本 ID>
  └─ 返回创建的记录

步骤 5: 返回结果
──────────────
Cube.js → Actions → Hasura → 前端

{
  "schemas": [
    {
      "name": "Orders",
      "fileName": "Orders.yml",
      "content": "cubes:\n  - name: Orders\n    ..."
    },
    {
      "name": "Users",
      "fileName": "Users.yml",
      "content": "..."
    },
    {
      "name": "Products",
      "fileName": "Products.yml",
      "content": "..."
    }
  ],
  "version_id": "new-version-uuid"
}
```

## 六、架构亮点总结

### 1. 完全动态化
- **Schema**: 存储在数据库，支持版本控制和分支管理
- **驱动**: 运行时根据数据源配置动态创建
- **权限**: 从数据库读取，实时生效
- **优势**: 无需重启服务即可修改配置

### 2. 多租户隔离
- **Orchestrator 隔离**: 每个数据源独立的查询队列
- **Schema 隔离**: 每个数据源独立编译
- **缓存隔离**: 查询结果和预聚合完全隔离
- **预聚合隔离**: 独立的 schema 命名空间

### 3. 统一认证
- **JWT 贯穿**: 从前端到 Cube.js 的完整链路
- **Hasura 集成**: 统一的用户管理和权限系统
- **SQL API 支持**: 通过 sql_credentials 表实现

### 4. 权限细粒度
- **Cube 级别**: 控制对整个 cube 的访问
- **字段级别**: 精确到 measure/dimension/segment
- **查询拦截**: 在查询执行前验证权限

### 5. 多协议支持
- **REST API**: `/api/v1/load` 标准 Cube.js API
- **GraphQL**: 通过 Actions Service 集成
- **MySQL 协议**: port 13306
- **PostgreSQL 协议**: port 15432
- **优势**: 兼容几乎所有 BI 工具

### 6. 版本控制
- **Branch**: 支持开发/测试/生产分支
- **Version**: 每次 schema 修改创建新版本
- **灰度发布**: 通过指定 branch-version-id 实现
- **回滚**: 切换到历史版本

### 7. 性能优化
- **Cubestore**: 分布式缓存和预聚合引擎
- **查询缓存**: 基于查询签名的智能缓存
- **预聚合**: 支持定时刷新和增量更新
- **连接池**: 每个数据源独立的连接池

## 七、与传统 Cube.js 的对比

| 特性 | 传统 Cube.js | Synmetrix |
|------|-------------|-----------|
| Schema 存储 | 文件系统 (.js/.yml 文件) | PostgreSQL 数据库 |
| Schema 加载 | 文件系统读取 | GraphQL 动态查询 |
| 多租户支持 | 需要自行实现 | 内置多租户隔离 |
| 版本控制 | Git | 数据库版本表 |
| 权限控制 | 自定义实现 | 集成 Hasura RBAC |
| 配置更新 | 需要重启 | 实时生效 |
| 用户管理 | 外部系统 | Hasura 统一管理 |
| API 网关 | 直接暴露 | 通过 Actions Service |

这种架构使 Synmetrix 能够作为一个企业级的多租户语义层平台，为不同团队、不同数据源提供隔离、安全、高性能的数据访问能力。
