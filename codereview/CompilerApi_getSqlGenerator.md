# CompilerApi.getSqlGenerator() 详细解析

## 概述

`getSqlGenerator` 是 Cube 架构中的核心方法，位于 `packages/cubejs-server-core/src/core/CompilerApi.js:174`。它负责为用户查询创建合适的 SQL 生成器实例，并处理数据源的动态切换。

## 方法签名

```javascript
// CompilerApi.js:174-207
async getSqlGenerator(query, dataSource) {
  // ...
  return { sqlGenerator, compilers };
}
```

**参数**：
- `query`: 用户查询对象
  - `measures`: 度量字段数组
  - `dimensions`: 维度字段数组
  - `timeDimensions`: 时间维度数组
  - `filters`: 过滤条件数组
  - `requestId`: 请求 ID（用于日志追踪）
  - 其他查询选项...

- `dataSource`: 数据源名称（可选）
  - 默认值: `'default'`
  - 可以是自定义数据源名称

**返回值**：`{ sqlGenerator, compilers }`
- `sqlGenerator`: Query 实例（如 PostgresQuery、MysqlQuery 等）
- `compilers`: 编译器对象
  - `compiler`: DataSchemaCompiler 实例
  - `joinGraph`: JoinGraph 实例
  - `cubeEvaluator`: CubeEvaluator 实例
  - `compilerCache`: CompilerCache 实例

---

## 完整源码分析

### 第一步：获取数据库类型（行 175）

```javascript
const dbType = await this.getDbType(dataSource);
```

**`getDbType` 方法**（行 166-168）：
```javascript
async getDbType(dataSource = 'default') {
  return this.dbType({ dataSource });
}
```

**作用**：
- 根据数据源名称获取对应的数据库类型
- 调用用户配置的 `dbType` 函数

**配置示例**：
```javascript
// cube.js
module.exports = {
  dbType: ({ dataSource }) => {
    if (dataSource === 'analytics') return 'bigquery';
    if (dataSource === 'orders') return 'postgres';
    return 'mysql';  // default
  }
};
```

**输出示例**：
```javascript
await getDbType('default')    // → 'postgres'
await getDbType('analytics')  // → 'bigquery'
await getDbType('orders')     // → 'postgres'
```

---

### 第二步：获取编译器（行 176）

```javascript
const compilers = await this.getCompilers({ requestId: query.requestId });
```

**`getCompilers` 方法**（行 73-97）：
```javascript
async getCompilers({ requestId } = {}) {
  // 1. 计算 schema 版本
  let compilerVersion = (
    this.schemaVersion && await this.schemaVersion() ||
    'default_schema_version'
  );

  // 2. 开发模式下，添加文件 hash 到版本
  if (this.options.devServer || this.options.fastReload) {
    const files = await this.repository.dataSchemaFiles();
    compilerVersion += `_${crypto.createHash('md5').update(JSON.stringify(files)).digest('hex')}`;
  }

  // 3. 如果版本变化，重新编译
  if (!this.compilers || this.compilerVersion !== compilerVersion) {
    this.compilers = this.compileSchema(compilerVersion, requestId).catch(e => {
      this.compilers = undefined;
      throw e;
    });
    this.compilerVersion = compilerVersion;
  }

  return this.compilers;
}
```

**作用**：
- 获取或创建编译器实例
- 使用版本号进行缓存
- 开发模式下检测文件变化

**编译流程**（`compileSchema` 方法，行 114-148）：
```javascript
async compileSchema(compilerVersion, requestId) {
  const startCompilingTime = new Date().getTime();

  try {
    this.logger('Compiling schema', {
      version: compilerVersion,
      requestId
    });

    // 调用 schema-compiler 包的 compile 函数
    const compilers = await compile(this.repository, {
      allowNodeRequire: this.allowNodeRequire,
      compileContext: this.compileContext,
      allowJsDuplicatePropsInSchema: this.allowJsDuplicatePropsInSchema,
      standalone: this.standalone,
      nativeInstance: this.nativeInstance,
      compiledScriptCache: this.compiledScriptCache,
    });

    // 创建 QueryFactory
    this.queryFactory = await this.createQueryFactory(compilers);

    this.logger('Compiling schema completed', {
      version: compilerVersion,
      requestId,
      duration: ((new Date()).getTime() - startCompilingTime),
    });

    return compilers;
  } catch (e) {
    this.logger('Compiling schema error', {
      version: compilerVersion,
      requestId,
      duration: ((new Date()).getTime() - startCompilingTime),
      error: (e.stack || e).toString()
    });
    throw e;
  }
}
```

**`createQueryFactory` 方法**（行 150-164）：
```javascript
async createQueryFactory(compilers) {
  const { cubeEvaluator } = compilers;

  // 为每个 cube 映射对应的 Query 类
  const cubeToQueryClass = Object.fromEntries(
    await Promise.all(
      cubeEvaluator.cubeNames().map(async (cube) => {
        const dataSource = cubeEvaluator.cubeFromPath(cube).dataSource ?? 'default';
        const dbType = await this.getDbType(dataSource);
        const dialectClass = this.getDialectClass(dataSource, dbType);
        return [cube, queryClass(dbType, dialectClass)];
      })
    )
  );

  return new QueryFactory(cubeToQueryClass);
}
```

**QueryFactory 的作用**：
- 为不同的 cube 提供不同的 Query 类
- 支持多数据源架构（同一个查询可能涉及多个数据库）

**示例**：
```javascript
// 数据模型定义
cube('Orders', {
  sql: 'SELECT * FROM orders',
  dataSource: 'postgres'  // ← 使用 PostgreSQL
});

cube('Analytics', {
  sql: 'SELECT * FROM analytics',
  dataSource: 'bigquery'  // ← 使用 BigQuery
});

// QueryFactory 映射
{
  'Orders': PostgresQuery,
  'Analytics': BigqueryQuery
}
```

---

### 第三步：创建初始 SQL 生成器（行 177）

```javascript
let sqlGenerator = await this.createQueryByDataSource(compilers, query, dataSource, dbType);
```

**`createQueryByDataSource` 方法**（行 520-526）：
```javascript
async createQueryByDataSource(compilers, query, dataSource, dbType) {
  if (!dbType) {
    dbType = await this.getDbType(dataSource);
  }

  return this.createQuery(compilers, dbType, this.getDialectClass(dataSource, dbType), query);
}
```

**`createQuery` 方法**（行 528-543）：
```javascript
createQuery(compilers, dbType, dialectClass, query) {
  return createQuery(  // ← 调用 schema-compiler 包的 createQuery 工厂函数
    compilers,
    dbType,
    {
      ...query,  // 用户查询参数
      dialectClass,  // 方言类（自定义 Query 类）
      externalDialectClass: this.options.externalDialectClass,  // 外部数据源方言
      externalDbType: this.options.externalDbType,  // 外部数据库类型
      preAggregationsSchema: this.preAggregationsSchema,  // 预聚合 schema
      allowUngroupedWithoutPrimaryKey: this.allowUngroupedWithoutPrimaryKey,
      convertTzForRawTimeDimension: this.convertTzForRawTimeDimension,
      queryFactory: this.queryFactory,  // ← QueryFactory 实例
    }
  );
}
```

**这里调用的是 `schema-compiler` 包的 `createQuery` 工厂函数**（参考前面的 createQuery.md）：
```javascript
// @cubejs-backend/schema-compiler/src/adapter/QueryBuilder.ts
export const createQuery = (compilers, dbType: string, queryOptions: any) => {
  const QueryClass = queryClass(dbType, queryOptions.dialectClass);
  return new QueryClass(compilers, queryOptions);
};
```

---

### 第四步：验证 SQL 生成器（行 179-181）

```javascript
if (!sqlGenerator) {
  throw new Error(`Unknown dbType: ${dbType}`);
}
```

**错误场景**：
- 数据库类型不在 ADAPTERS 映射表中
- 没有提供自定义 `dialectClass`

---

### 第五步：动态数据源切换（行 186-204）✨核心逻辑

这是 `getSqlGenerator` 最重要的功能之一：**根据查询中实际使用的成员动态切换数据源**。

```javascript
// 1. 获取查询实际使用的数据源
dataSource = compilers.compiler.withQuery(sqlGenerator, () => sqlGenerator.dataSource);

// 2. 如果数据源与初始不同，重新创建 SQL 生成器
if (dataSource !== undefined) {
  const _dbType = await this.getDbType(dataSource);

  if (dataSource !== 'default' && dbType !== _dbType) {
    // 重新创建针对新数据源的 SQL 生成器
    sqlGenerator = await this.createQueryByDataSource(
      compilers,
      query,
      dataSource,
      _dbType
    );

    if (!sqlGenerator) {
      throw new Error(
        `Can't find dialect for '${dataSource}' data source: ${_dbType}`
      );
    }
  }
}
```

#### 为什么需要动态切换？

**场景 1：用户未指定数据源**
```javascript
// 用户查询（没有指定 dataSource）
const query = {
  measures: ['Orders.count']  // Orders cube 定义了 dataSource: 'postgres'
};

// 调用 getSqlGenerator
await compilerApi.getSqlGenerator(query);

// 执行流程:
// 1. 初始 dataSource = undefined → 使用默认数据源创建 sqlGenerator
// 2. sqlGenerator.dataSource → 'postgres' (从 Orders cube 获取)
// 3. 检测到不一致 → 重新创建 PostgresQuery 实例
```

**场景 2：多数据源查询**
```javascript
// Orders cube 使用 PostgreSQL
cube('Orders', {
  sql: 'SELECT * FROM orders',
  dataSource: 'postgres'
});

// Analytics cube 使用 BigQuery
cube('Analytics', {
  sql: 'SELECT * FROM analytics',
  dataSource: 'bigquery'
});

// 用户查询同时包含两个 cube
const query = {
  measures: ['Orders.count', 'Analytics.totalViews']
};

// 这种情况下会报错，因为不能在一个查询中混用多个数据源
// Cube 会强制查询只能使用一个数据源
```

**场景 3：动态数据源路由**
```javascript
// API Gateway 调用
await compilerApi.getSqlGenerator(query, 'analytics');
// ↓
// 初始使用 'analytics' 数据源
// 如果查询中的 cube 定义了不同的数据源，会自动切换
```

#### `sqlGenerator.dataSource` 属性

这是一个 **getter 属性**，定义在 `BaseQuery.js` 中：

```javascript
// BaseQuery.js
get dataSource() {
  // 获取查询中所有成员（measures + dimensions + segments）使用的数据源
  const dataSources = R.uniq(
    this.collectFrom(
      [...this.measures, ...this.dimensions, ...this.segments],
      this.collectDataSourcesFor.bind(this),
      'collectDataSourcesFor'
    )
  );

  // 如果有多个数据源，抛出错误
  if (dataSources.length > 1) {
    throw new UserError(`Your query uses multiple data sources: ${dataSources.join(', ')}. Please split your query into multiple queries, each using a single data source.`);
  }

  return dataSources[0];
}
```

**工作原理**：
1. 遍历查询中的所有成员
2. 从对应的 cube 定义中提取 `dataSource` 属性
3. 确保所有成员来自同一个数据源
4. 返回该数据源名称

---

### 第六步：返回结果（行 206）

```javascript
return { sqlGenerator, compilers };
```

**返回的对象结构**：
```javascript
{
  sqlGenerator: PostgresQuery {
    measures: [...],
    dimensions: [...],
    timeDimensions: [...],
    filters: [...],
    // ... 其他查询配置
    buildSqlAndParams: [Function],
    preAggregations: PreAggregations {},
    // ... 其他方法
  },
  compilers: {
    compiler: DataSchemaCompiler {},
    joinGraph: JoinGraph {},
    cubeEvaluator: CubeEvaluator {},
    compilerCache: CompilerCache {}
  }
}
```

---

## 完整调用链路图

```
┌─────────────────────────────────────────────────────────────────┐
│                      API Gateway                                 │
│  gateway.ts:1512                                                 │
│  const { sqlGenerator } = await compilerApi.getSqlGenerator(...) │
└──────────────────────────┬──────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────────┐
│              CompilerApi.getSqlGenerator(query, dataSource)      │
│                     CompilerApi.js:174                           │
└──────────────────────────┬──────────────────────────────────────┘
                           ↓
         ┌─────────────────┴─────────────────┐
         ↓                                   ↓
┌────────────────────┐            ┌────────────────────────┐
│  1. getDbType()    │            │  2. getCompilers()     │
│     行 175         │            │     行 176             │
├────────────────────┤            ├────────────────────────┤
│ 获取数据库类型      │            │ 获取/创建编译器         │
│ 'postgres'         │            │                        │
│ 'mysql'            │            │ ├─ compileSchema()    │
│ 'bigquery'         │            │ │  - compile()        │
│ ...                │            │ │  - 编译数据模型      │
│                    │            │ │                      │
│ 配置来源:          │            │ └─ createQueryFactory()│
│ cube.js dbType()   │            │    - 为每个 cube 创建  │
│                    │            │      Query 类映射      │
└────────────────────┘            └────────────────────────┘
         │                                   │
         └─────────────────┬─────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────────┐
│         3. createQueryByDataSource(compilers, query, ...)        │
│                         行 177                                   │
│                            ↓                                     │
│              createQuery(compilers, dbType, dialectClass, query) │
│                         行 528                                   │
│                            ↓                                     │
│         调用 @cubejs-backend/schema-compiler 的 createQuery     │
│                            ↓                                     │
│              new PostgresQuery(compilers, queryOptions)          │
└──────────────────────────┬──────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────────┐
│                   4. 验证 sqlGenerator                           │
│                         行 179-181                               │
│  if (!sqlGenerator) throw new Error(...)                        │
└──────────────────────────┬──────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────────┐
│              5. 动态数据源切换 ✨核心逻辑✨                       │
│                         行 186-204                               │
│                                                                   │
│  dataSource = sqlGenerator.dataSource  ← getter 属性             │
│           ↓                                                       │
│  获取查询中实际使用的成员的数据源                                  │
│           ↓                                                       │
│  检查是否与初始数据源不同                                         │
│           ↓                                                       │
│  if (dataSource !== 'default' && dbType !== _dbType) {          │
│    // 重新创建 SQL 生成器                                         │
│    sqlGenerator = await this.createQueryByDataSource(...);       │
│  }                                                                │
└──────────────────────────┬──────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────────┐
│                6. 返回 { sqlGenerator, compilers }               │
│                         行 206                                   │
└─────────────────────────────────────────────────────────────────┘
```

---

## 使用场景

### 场景 1：API Gateway 获取 SQL 生成器（最常见）

**位置**：`gateway.ts:1512`

```javascript
// gateway.ts:1510-1513
const dataSources = new Set(Object.values(memberToDataSource));
const dataSourceToSqlGenerator = (await Promise.all(
  [...dataSources].map(async dataSource =>
    ({ [dataSource]: (await compilerApi.getSqlGenerator(query, dataSource)).sqlGenerator })
  )
)).reduce((a, b) => ({ ...a, ...b }), {});
```

**用途**：
- 为每个数据源创建对应的 SQL 生成器
- 用于多数据源的元数据查询

**示例**：
```javascript
// 用户有 3 个数据源
const dataSources = ['default', 'analytics', 'logs'];

// 并行创建 SQL 生成器
const dataSourceToSqlGenerator = {
  'default': PostgresQuery {},
  'analytics': BigqueryQuery {},
  'logs': ClickHouseQuery {}
};
```

---

### 场景 2：CompilerApi.getSql 调用（SQL 生成）

**位置**：`CompilerApi.js:211`

```javascript
// CompilerApi.js:209-238
async getSql(query, options = {}) {
  const { includeDebugInfo, exportAnnotatedSql } = options;
  const { sqlGenerator, compilers } = await this.getSqlGenerator(query);

  const getSqlFn = () => compilers.compiler.withQuery(sqlGenerator, () => ({
    external: sqlGenerator.externalPreAggregationQuery(),
    sql: sqlGenerator.buildSqlAndParams(exportAnnotatedSql),  // ← 生成 SQL
    lambdaQueries: sqlGenerator.buildLambdaQuery(),
    timeDimensionAlias: sqlGenerator.timeDimensions[0]?.unescapedAliasName(),
    order: sqlGenerator.order,
    cacheKeyQueries: sqlGenerator.cacheKeyQueries(),
    preAggregations: sqlGenerator.preAggregations.preAggregationsDescription(),
    dataSource: sqlGenerator.dataSource,
    aliasNameToMember: sqlGenerator.aliasNameToMember,
    rollupMatchResults: includeDebugInfo ?
      sqlGenerator.preAggregations.rollupMatchResultDescriptions() : undefined,
    canUseTransformedQuery: sqlGenerator.preAggregations.canUseTransformedQuery(),
    memberNames: sqlGenerator.collectAllMemberNames(),
  }));

  // SQL 缓存
  if (this.sqlCache) {
    const { requestId, ...keyOptions } = query;
    const key = { query: keyOptions, options };
    return compilers.compilerCache.getQueryCache(key).cache(['sql'], getSqlFn);
  } else {
    return getSqlFn();
  }
}
```

**用途**：
- 为用户查询生成可执行的 SQL
- 获取预聚合信息
- 获取缓存键

**调用示例**：
```javascript
const compilerApi = new CompilerApi(...);

const query = {
  measures: ['Orders.count'],
  dimensions: ['Orders.status'],
  timeDimensions: [{
    dimension: 'Orders.createdAt',
    granularity: 'day',
    dateRange: ['2024-01-01', '2024-01-31']
  }]
};

const sqlResult = await compilerApi.getSql(query);

console.log(sqlResult.sql);
// [
//   "SELECT status, date_trunc('day', created_at) as created_at_day, COUNT(*) as count FROM orders WHERE created_at >= $1 AND created_at < $2 GROUP BY 1, 2",
//   ['2024-01-01T00:00:00Z', '2024-02-01T00:00:00Z']
// ]
```

---

### 场景 3：多数据源查询路由

```javascript
// 数据模型定义
cube('PostgresOrders', {
  sql: 'SELECT * FROM orders',
  dataSource: 'postgres'
});

cube('BigQueryAnalytics', {
  sql: 'SELECT * FROM analytics',
  dataSource: 'bigquery'
});

// 查询 1：只查询 PostgreSQL
const query1 = {
  measures: ['PostgresOrders.count']
};

const { sqlGenerator: pg } = await compilerApi.getSqlGenerator(query1);
// → PostgresQuery 实例

// 查询 2：只查询 BigQuery
const query2 = {
  measures: ['BigQueryAnalytics.totalViews']
};

const { sqlGenerator: bq } = await compilerApi.getSqlGenerator(query2);
// → BigqueryQuery 实例

// 查询 3：混合查询（会报错）
const query3 = {
  measures: ['PostgresOrders.count', 'BigQueryAnalytics.totalViews']
};

await compilerApi.getSqlGenerator(query3);
// ❌ Error: Your query uses multiple data sources: postgres, bigquery.
//    Please split your query into multiple queries, each using a single data source.
```

---

## 关键特性

### 1. 智能数据源检测

**自动检测查询中使用的数据源**：
```javascript
// 用户查询没有指定数据源
const query = { measures: ['Orders.count'] };

// getSqlGenerator 自动检测
await compilerApi.getSqlGenerator(query);
// ↓
// 1. 初始创建默认 Query 实例
// 2. 调用 sqlGenerator.dataSource 检测实际数据源
// 3. 如果不同，重新创建正确的 Query 实例
```

---

### 2. Schema 版本管理

**自动检测 schema 变化并重新编译**：

```javascript
// getCompilers 方法中的逻辑
async getCompilers({ requestId } = {}) {
  // 计算版本号
  let compilerVersion = await this.schemaVersion() || 'default_schema_version';

  // 开发模式：添加文件 hash
  if (this.options.devServer || this.options.fastReload) {
    const files = await this.repository.dataSchemaFiles();
    compilerVersion += `_${crypto.createHash('md5').update(JSON.stringify(files)).digest('hex')}`;
  }

  // 版本变化时重新编译
  if (!this.compilers || this.compilerVersion !== compilerVersion) {
    this.compilers = this.compileSchema(compilerVersion, requestId);
    this.compilerVersion = compilerVersion;
  }

  return this.compilers;
}
```

**版本号生成规则**：
1. **生产模式**：使用用户提供的 `schemaVersion()`
2. **开发模式**：`schemaVersion + '_' + files_hash`

**示例**：
```javascript
// cube.js
module.exports = {
  // 生产模式：手动指定版本
  schemaVersion: async () => {
    return await fetchVersionFromDatabase();
  },

  // 开发模式：自动检测文件变化
  devServer: true
};

// 版本号示例:
// 生产: "v1.2.3"
// 开发: "v1.2.3_a1b2c3d4e5f6g7h8i9j0"
//                   ^^^^^^^^^^^^^^^^^ 文件内容的 MD5 hash
```

---

### 3. QueryFactory 多数据源支持

**为不同 cube 使用不同的 Query 类**：

```javascript
// createQueryFactory 方法（行 150-164）
async createQueryFactory(compilers) {
  const { cubeEvaluator } = compilers;

  const cubeToQueryClass = Object.fromEntries(
    await Promise.all(
      cubeEvaluator.cubeNames().map(async (cube) => {
        const dataSource = cubeEvaluator.cubeFromPath(cube).dataSource ?? 'default';
        const dbType = await this.getDbType(dataSource);
        const dialectClass = this.getDialectClass(dataSource, dbType);
        return [cube, queryClass(dbType, dialectClass)];
      })
    )
  );

  return new QueryFactory(cubeToQueryClass);
}
```

**QueryFactory 的使用**（在 BaseQuery 中）：

```javascript
// BaseQuery.js:3963
newSubQueryForCube(cube, options) {
  if (this.options.queryFactory) {
    // 为特定 cube 创建正确的 Query 实例
    return this.options.queryFactory.createQuery(
      cube,
      this.compilers,
      { ...this.subQueryOptions(options), paramAllocator: null }
    );
  }

  return this.newSubQuery(options);
}
```

**应用场景**：
```javascript
// 复杂查询涉及多个 cube 的 JOIN
const query = {
  measures: ['Orders.count'],
  dimensions: ['Products.name']  // JOIN to Products cube
};

// Orders 使用 PostgreSQL
cube('Orders', {
  dataSource: 'postgres',
  joins: {
    Products: {
      relationship: 'belongsTo',
      sql: `${CUBE}.product_id = ${Products}.id`
    }
  }
});

// Products 也使用 PostgreSQL（必须同一数据源）
cube('Products', {
  dataSource: 'postgres'
});

// QueryFactory 确保为 Orders 和 Products 都使用 PostgresQuery
```

---

### 4. 编译缓存优化

**LRU 缓存机制**：

```javascript
// CompilerApi 构造函数（行 40-52）
this.compiledScriptCache = new LRUCache({
  max: options.compilerCacheSize || 250,  // 最多缓存 250 个编译结果
  ttl: options.maxCompilerCacheKeepAlive,  // 存活时间
  updateAgeOnGet: options.updateCompilerCacheKeepAlive  // 访问时更新年龄
});

// 定期清理过期缓存
if (this.options.maxCompilerCacheKeepAlive) {
  this.compiledScriptCacheInterval = setInterval(
    () => this.compiledScriptCache.purgeStale(),
    this.options.maxCompilerCacheKeepAlive
  );
}
```

**缓存的内容**：
- 编译后的 JavaScript 代码
- Cube 定义的求值结果
- Join Graph
- 元数据

**配置示例**：
```javascript
// cube.js
module.exports = {
  compilerCacheSize: 500,  // 增加缓存大小
  maxCompilerCacheKeepAlive: 60 * 60 * 1000,  // 1 小时
  updateCompilerCacheKeepAlive: true  // 访问时刷新
};
```

---

## 错误处理

### 错误 1：未知数据库类型

```javascript
// CompilerApi.js:179-181
if (!sqlGenerator) {
  throw new Error(`Unknown dbType: ${dbType}`);
}
```

**触发条件**：
- `dbType` 不在 ADAPTERS 映射表中
- 没有提供自定义 `dialectClass`

**解决方案**：
```javascript
// cube.js
module.exports = {
  dbType: 'postgres',  // 使用支持的数据库类型

  // 或者提供自定义方言类
  dialectClass: (options) => {
    if (options.dataSource === 'custom') {
      return CustomQuery;
    }
  }
};
```

---

### 错误 2：找不到数据源的方言

```javascript
// CompilerApi.js:199-201
if (!sqlGenerator) {
  throw new Error(
    `Can't find dialect for '${dataSource}' data source: ${_dbType}`
  );
}
```

**触发条件**：
- 数据源切换后找不到对应的方言类

**解决方案**：
```javascript
// 确保为每个数据源配置正确的 dbType
module.exports = {
  dbType: ({ dataSource }) => {
    if (dataSource === 'analytics') return 'bigquery';
    if (dataSource === 'warehouse') return 'snowflake';
    return 'postgres';  // default
  }
};
```

---

### 错误 3：多数据源查询

```javascript
// BaseQuery.js (sqlGenerator.dataSource getter)
if (dataSources.length > 1) {
  throw new UserError(
    `Your query uses multiple data sources: ${dataSources.join(', ')}. ` +
    `Please split your query into multiple queries, each using a single data source.`
  );
}
```

**触发条件**：
- 查询中的成员来自不同的数据源

**示例**：
```javascript
// ❌ 错误：混合数据源
const query = {
  measures: [
    'PostgresOrders.count',  // dataSource: 'postgres'
    'BigQueryAnalytics.views'  // dataSource: 'bigquery'
  ]
};

// ✅ 正确：拆分为多个查询
const query1 = { measures: ['PostgresOrders.count'] };
const query2 = { measures: ['BigQueryAnalytics.views'] };

const [result1, result2] = await Promise.all([
  cubejsApi.load(query1),
  cubejsApi.load(query2)
]);
```

---

## 性能优化

### 1. Schema 编译缓存

**避免重复编译**：
```javascript
// getCompilers 方法中的缓存逻辑
if (!this.compilers || this.compilerVersion !== compilerVersion) {
  this.compilers = this.compileSchema(compilerVersion, requestId);
  this.compilerVersion = compilerVersion;
}
return this.compilers;  // 返回缓存的编译结果
```

**性能提升**：
- 首次编译：200-1000ms
- 缓存命中：< 1ms
- **提升倍数**：200-1000x

---

### 2. SQL 缓存

```javascript
// getSql 方法中的缓存逻辑（行 230-237）
if (this.sqlCache) {
  const { requestId, ...keyOptions } = query;
  const key = { query: keyOptions, options };
  return compilers.compilerCache.getQueryCache(key).cache(['sql'], getSqlFn);
}
```

**配置**：
```javascript
// cube.js
module.exports = {
  sqlCache: true  // 启用 SQL 缓存
};
```

**性能提升**：
- 首次生成：10-50ms
- 缓存命中：< 1ms
- **提升倍数**：10-50x

---

### 3. 并行数据源处理

```javascript
// gateway.ts:1511-1513
const dataSourceToSqlGenerator = (await Promise.all(
  [...dataSources].map(async dataSource =>
    ({ [dataSource]: (await compilerApi.getSqlGenerator(query, dataSource)).sqlGenerator })
  )
)).reduce((a, b) => ({ ...a, ...b }), {});
```

**优化**：
- 串行：3 * 100ms = 300ms
- 并行：max(100ms) = 100ms
- **提升倍数**：3x

---

## 实际业务流程示例

### 完整的查询执行流程

```javascript
// 1. 用户发起请求
const query = {
  measures: ['Orders.totalAmount', 'Orders.count'],
  dimensions: ['Orders.status', 'Users.country'],
  timeDimensions: [{
    dimension: 'Orders.createdAt',
    granularity: 'day',
    dateRange: ['2024-01-01', '2024-01-31']
  }],
  filters: [{
    member: 'Users.country',
    operator: 'equals',
    values: ['US']
  }]
};

// 2. API Gateway 调用 CompilerApi
const compilerApi = await getCompilerApi(context);
const { sqlGenerator, compilers } = await compilerApi.getSqlGenerator(query);

// 执行步骤详解：

// 步骤 2.1: getDbType('default')
// → 'postgres'

// 步骤 2.2: getCompilers({ requestId: query.requestId })
// → 检查 schema 版本
// → 版本: "v1.0.0_a1b2c3d4" (开发模式)
// → 缓存命中，返回已编译的 compilers

// 步骤 2.3: createQueryByDataSource(compilers, query, undefined, 'postgres')
// → createQuery(compilers, 'postgres', null, query)
// → new PostgresQuery(compilers, queryOptions)
// → 初始 sqlGenerator 创建完成

// 步骤 2.4: 检测实际数据源
// dataSource = sqlGenerator.dataSource
// → 分析 query 中的成员:
//    - Orders.totalAmount → dataSource: 'postgres'
//    - Orders.count → dataSource: 'postgres'
//    - Orders.status → dataSource: 'postgres'
//    - Users.country → dataSource: 'postgres'
// → 统一数据源: 'postgres'
// → 与初始数据源一致，无需重新创建

// 步骤 2.5: 返回结果
return { sqlGenerator: PostgresQuery {...}, compilers: {...} };

// 3. 生成 SQL
const sqlResult = compilers.compiler.withQuery(sqlGenerator, () => ({
  sql: sqlGenerator.buildSqlAndParams(),
  preAggregations: sqlGenerator.preAggregations.preAggregationsDescription(),
  // ...
}));

// 4. 执行查询
const [sql, params] = sqlResult.sql;
const result = await database.query(sql, params);

// 5. 返回结果给用户
return result.rows;
```

---

## 总结

### 核心职责

`getSqlGenerator` 方法是 Cube 查询处理的**入口点**，负责：

1. ✅ **数据库类型解析**：根据数据源获取对应的数据库类型
2. ✅ **Schema 编译**：获取或创建编译器实例
3. ✅ **Query 实例创建**：创建合适的 SQL 生成器（PostgresQuery、MysqlQuery 等）
4. ✅ **数据源检测**：智能检测查询实际使用的数据源
5. ✅ **动态切换**：根据实际数据源重新创建 Query 实例
6. ✅ **错误处理**：验证数据源和数据库类型的有效性

### 关键优化

| 优化项 | 技术 | 性能提升 |
|-------|------|---------|
| **Schema 编译缓存** | 版本号 + LRU Cache | 200-1000x |
| **SQL 缓存** | QueryCache | 10-50x |
| **并行处理** | Promise.all | 3x |
| **智能切换** | 动态数据源检测 | 避免错误创建 |

### 最佳实践

1. **合理配置缓存**：
   ```javascript
   compilerCacheSize: 500,
   maxCompilerCacheKeepAlive: 60 * 60 * 1000
   ```

2. **使用 schema 版本控制**：
   ```javascript
   schemaVersion: async () => await fetchVersion()
   ```

3. **避免混合数据源查询**：
   - 拆分为多个查询
   - 在应用层合并结果

4. **启用 SQL 缓存**：
   ```javascript
   sqlCache: true
   ```

5. **监控编译性能**：
   - 记录编译时间
   - 监控缓存命中率

---

## 相关文件

- **核心实现**：`packages/cubejs-server-core/src/core/CompilerApi.js`
- **Query 工厂**：`packages/cubejs-schema-compiler/src/adapter/QueryBuilder.ts`
- **Query 基类**：`packages/cubejs-schema-compiler/src/adapter/BaseQuery.js`
- **Query 工厂类**：`packages/cubejs-schema-compiler/src/adapter/QueryFactory.ts`
- **Schema 编译器**：`packages/cubejs-schema-compiler/src/compiler/DataSchemaCompiler.js`

## 参考资料

- [Cube 文档 - Multi-Data Source](https://cube.dev/docs/product/configuration/advanced/multiple-data-sources)
- [Cube 文档 - Schema Compilation](https://cube.dev/docs/product/data-modeling/overview)
