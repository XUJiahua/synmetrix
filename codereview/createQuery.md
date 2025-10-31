# createQuery 工厂方法分析

## 概述

`QueryBuilder.ts` 提供了创建数据库查询适配器的工厂方法，位于 `packages/cubejs-schema-compiler/src/adapter/QueryBuilder.ts`。它实现了**工厂模式**，根据数据库类型自动选择并实例化对应的 Query 类。

## 核心代码结构

### 1. ADAPTERS 映射表（行 19-41）

```typescript
const ADAPTERS = {
  postgres: PostgresQuery,
  redshift: RedshiftQuery,
  mysql: MysqlQuery,
  mysqlauroraserverless: MysqlQuery,
  mongobi: MongoBiQuery,
  mssql: MssqlQuery,
  bigquery: BigqueryQuery,
  prestodb: PrestodbQuery,
  qubole_prestodb: PrestodbQuery,
  athena: PrestodbQuery,
  vertica: VerticaQuery,
  snowflake: SnowflakeQuery,
  clickhouse: ClickHouseQuery,
  crate: CrateQuery,
  hive: HiveQuery,
  oracle: OracleQuery,
  sqlite: SqliteQuery,
  awselasticsearch: AWSElasticSearchQuery,
  elasticsearch: ElasticSearchQuery,
  materialize: PostgresQuery,      // 注意：Materialize 使用 PostgresQuery
  cubestore: CubeStoreQuery,
};
```

**关键点**：
- `materialize` 数据库使用 `PostgresQuery` 适配器（PostgreSQL 兼容）
- `athena` 和 `qubole_prestodb` 都使用 `PrestodbQuery`
- `mysqlauroraserverless` 使用 `MysqlQuery`

---

### 2. queryClass 函数（行 43）

```typescript
export const queryClass = (dbType: string, dialectClass) =>
  dialectClass || ADAPTERS[dbType];
```

**功能**：获取查询类

**参数**：
- `dbType`: 数据库类型字符串（如 'postgres', 'mysql'）
- `dialectClass`: 自定义方言类（可选，用于覆盖默认行为）

**返回值**：Query 类构造函数

**逻辑**：
1. 如果提供了 `dialectClass`，直接返回它
2. 否则从 `ADAPTERS` 映射表中查找

**使用场景**：
```typescript
// 获取默认的 PostgreSQL Query 类
const QueryClass = queryClass('postgres', null);

// 使用自定义方言类
const CustomQueryClass = queryClass('postgres', CustomPostgresQuery);
```

---

### 3. createQuery 函数（行 45-64）

```typescript
export const createQuery = (compilers, dbType: string, queryOptions: any) => {
  // 1. 验证数据库类型
  if (!queryOptions.dialectClass && !ADAPTERS[dbType]) {
    return null;
  }

  // 2. 处理外部查询类（用于联邦查询）
  let externalQueryClass = queryOptions.externalDialectClass;

  if (!externalQueryClass && queryOptions.externalDbType) {
    if (!ADAPTERS[queryOptions.externalDbType]) {
      throw new Error(`Dialect for '${queryOptions.externalDbType}' is not found`);
    }
    externalQueryClass = ADAPTERS[queryOptions.externalDbType];
  }

  // 3. 实例化并返回 Query 对象
  return new (queryClass(dbType, queryOptions.dialectClass))(compilers, {
    ...queryOptions,
    externalQueryClass,
  });
};
```

**功能**：创建查询实例的工厂方法

**参数**：
- `compilers`: 编译器上下文对象
  - `compiler`: DataSchemaCompiler
  - `joinGraph`: JoinGraph
  - `cubeEvaluator`: CubeEvaluator
- `dbType`: 数据库类型字符串
- `queryOptions`: 查询选项对象

**返回值**：Query 实例（如 PostgresQuery、MysqlQuery 等）

**逻辑流程**：
1. **验证数据库类型**（行 46-48）
   - 检查是否提供了自定义 `dialectClass` 或 `dbType` 在 ADAPTERS 中存在
   - 如果都不满足，返回 `null`

2. **处理外部查询类**（行 50-58）
   - 用于联邦查询（跨数据库查询）
   - 优先使用 `externalDialectClass`
   - 否则根据 `externalDbType` 从 ADAPTERS 中查找
   - 如果 `externalDbType` 不存在，抛出错误

3. **实例化查询对象**（行 60-63）
   - 调用 `queryClass()` 获取构造函数
   - 使用 `new` 创建实例
   - 传入 `compilers` 和合并后的 `queryOptions`

---

## 使用方式

### 方式一：使用 createQuery（推荐）

```typescript
import { createQuery } from '@cubejs-backend/schema-compiler';

// 1. 准备编译器上下文
const compilers = {
  compiler,       // DataSchemaCompiler 实例
  joinGraph,      // JoinGraph 实例
  cubeEvaluator   // CubeEvaluator 实例
};

// 2. 准备查询选项
const queryOptions = {
  measures: ['visitors.count'],
  dimensions: ['visitors.name'],
  timeDimensions: [{
    dimension: 'visitors.createdAt',
    granularity: 'month',
    dateRange: ['2020-01-01', '2020-12-31']
  }],
  filters: [{
    member: 'visitors.name',
    operator: 'equals',
    values: ['John']
  }],
  timezone: 'America/Los_Angeles'
};

// 3. 创建查询实例
const query = createQuery(compilers, 'postgres', queryOptions);

// 4. 生成 SQL
const [sql, params] = query.buildSqlAndParams();

// 5. 执行查询
const result = await pgClient.query(sql, params);
```

---

### 方式二：使用 queryClass 手动实例化

```typescript
import { queryClass } from '@cubejs-backend/schema-compiler';

// 1. 获取 Query 类
const PostgresQueryClass = queryClass('postgres', null);

// 2. 手动实例化
const query = new PostgresQueryClass(compilers, queryOptions);

// 3. 生成 SQL
const [sql, params] = query.buildSqlAndParams();
```

---

## queryOptions 参数详解

### 核心查询定义

```typescript
{
  // 度量字段（必选）
  measures?: string[],
  // 示例: ['visitors.count', 'orders.totalAmount', 'products.avgPrice']

  // 维度字段（可选）
  dimensions?: string[],
  // 示例: ['visitors.city', 'visitors.country', 'orders.status']

  // 时间维度（可选）
  timeDimensions?: Array<{
    dimension: string,              // 时间维度名称
    granularity: string,            // 'second', 'minute', 'hour', 'day', 'week', 'month', 'quarter', 'year'
    dateRange?: [string, string],   // 时间范围
    compareDateRange?: [string, string][]  // 对比时间范围
  }>,
  // 示例:
  // [{
  //   dimension: 'visitors.createdAt',
  //   granularity: 'day',
  //   dateRange: ['2024-01-01', '2024-01-31']
  // }]

  // 过滤条件（可选）
  filters?: Array<{
    member?: string,                // 字段名
    operator: string,               // 'equals', 'notEquals', 'contains', 'notContains', 'gt', 'gte', 'lt', 'lte', 'set', 'notSet', 'inDateRange', 'notInDateRange', 'beforeDate', 'afterDate'
    values?: any[],                 // 过滤值
    or?: Array<Filter>,             // OR 条件组
    and?: Array<Filter>             // AND 条件组
  }>,
  // 示例:
  // [{
  //   member: 'visitors.country',
  //   operator: 'equals',
  //   values: ['US', 'UK']
  // }]

  // 排序（可选）
  order?: Array<{
    id: string,                     // 字段标识符
    desc: boolean                   // 降序/升序
  }> | Record<string, 'asc' | 'desc'>,
  // 示例:
  // [{ id: 'visitors.count', desc: true }]
  // 或
  // { 'visitors.count': 'desc', 'visitors.createdAt': 'asc' }

  // 分页（可选）
  limit?: number,
  offset?: number,

  // 时区（必填）
  timezone: string,
  // 示例: 'America/Los_Angeles', 'UTC', 'Asia/Shanghai'

  // 总计查询（可选）
  total?: boolean,

  // 续订令牌（用于分页）
  renewQuery?: boolean,

  // 未分组查询（返回原始行）
  ungrouped?: boolean
}
```

---

### 高级配置选项

```typescript
{
  // 自定义方言类（覆盖默认的 ADAPTERS 映射）
  dialectClass?: any,
  // 示例:
  // class CustomPostgresQuery extends PostgresQuery {
  //   convertTz(field) { return `custom_tz(${field})`; }
  // }
  // queryOptions.dialectClass = CustomPostgresQuery

  // 外部数据库类型（用于联邦查询）
  externalDbType?: string,
  // 示例: 'bigquery', 'snowflake', 'redshift'
  // 用途: 主查询用一个数据库，外部查询用另一个数据库

  // 外部方言类（显式指定外部查询类）
  externalDialectClass?: any,

  // 参数分配器（通常由系统自动管理，不需要手动指定）
  paramAllocator?: ParamAllocator,

  // 预聚合查询标志
  preAggregationQuery?: boolean,
  // true: 该查询用于生成预聚合
  // false: 普通查询

  // Query 工厂（用于创建子查询）
  queryFactory?: QueryFactory,
  // 在处理 rollup joins 时使用，确保不同 cube 使用正确的参数分配器

  // 上下文符号
  contextSymbols?: any,
  // 包含 securityContext、filters 等上下文信息

  // 预聚合 schema
  preAggregationsSchema?: string,
  // 默认: 'stb_pre_aggregations'

  // 在预聚合中使用原生 SQL 预聚合
  useOriginalSqlPreAggregationsInPreAggregation?: boolean,

  // Cube Lattice 缓存
  cubeLatticeCache?: any,

  // 历史查询记录
  historyQueries?: any[],

  // 使用原生 SQL 规划器（Rust 实现）
  useNativeSqlPlanner?: boolean
}
```

---

## 实际使用示例

### 示例 1：基本查询

```typescript
import { createQuery } from '@cubejs-backend/schema-compiler';

const query = createQuery(
  { compiler, joinGraph, cubeEvaluator },
  'postgres',
  {
    measures: ['visitors.count'],
    dimensions: ['visitors.city'],
    timezone: 'UTC'
  }
);

const [sql, params] = query.buildSqlAndParams();
```

**生成的 SQL**：
```sql
SELECT
  "visitors".city AS "visitors__city",
  count(*) AS "visitors__count"
FROM visitors AS "visitors"
GROUP BY 1
ORDER BY 1 ASC
```

---

### 示例 2：带时间维度的查询

```typescript
const query = createQuery(
  { compiler, joinGraph, cubeEvaluator },
  'postgres',
  {
    measures: ['orders.totalAmount'],
    timeDimensions: [{
      dimension: 'orders.createdAt',
      granularity: 'day',
      dateRange: ['2024-01-01', '2024-01-31']
    }],
    timezone: 'America/New_York'
  }
);
```

**生成的 SQL**：
```sql
SELECT
  date_trunc('day', (orders.created_at::timestamptz AT TIME ZONE 'America/New_York')) AS "orders__created_at_day",
  sum(orders.amount) AS "orders__total_amount"
FROM orders AS "orders"
WHERE (orders.created_at::timestamptz AT TIME ZONE 'America/New_York') >= $1::timestamptz
  AND (orders.created_at::timestamptz AT TIME ZONE 'America/New_York') < $2::timestamptz
GROUP BY 1
ORDER BY 1 ASC
```

---

### 示例 3：带过滤和排序

```typescript
const query = createQuery(
  { compiler, joinGraph, cubeEvaluator },
  'postgres',
  {
    measures: ['visitors.count'],
    dimensions: ['visitors.source'],
    filters: [{
      member: 'visitors.country',
      operator: 'equals',
      values: ['US']
    }],
    order: [{
      id: 'visitors.count',
      desc: true
    }],
    limit: 10,
    timezone: 'UTC'
  }
);
```

**生成的 SQL**：
```sql
SELECT
  "visitors".source AS "visitors__source",
  count(*) AS "visitors__count"
FROM visitors AS "visitors"
WHERE ("visitors".country = $1)
GROUP BY 1
ORDER BY 2 DESC
LIMIT 10
```

---

### 示例 4：使用自定义方言类

```typescript
import { PostgresQuery } from './adapter/PostgresQuery';

// 创建自定义方言类
class CustomPostgresQuery extends PostgresQuery {
  // 覆写时区转换逻辑
  convertTz(field: string): string {
    return `custom_tz_convert(${field}, '${this.timezone}')`;
  }

  // 添加自定义函数
  customFunction(sql: string): string {
    return `my_custom_func(${sql})`;
  }
}

const query = createQuery(
  { compiler, joinGraph, cubeEvaluator },
  'postgres',
  {
    measures: ['visitors.count'],
    timezone: 'UTC',
    dialectClass: CustomPostgresQuery  // 使用自定义类
  }
);
```

**应用场景**：
- 扩展现有数据库适配器
- 添加企业特定的 SQL 函数
- 修改默认的 SQL 生成逻辑

---

### 示例 5：联邦查询（跨数据库）

```typescript
// 场景：主数据在 PostgreSQL，需要 join BigQuery 的外部表
const query = createQuery(
  { compiler, joinGraph, cubeEvaluator },
  'postgres',  // 主数据库
  {
    measures: ['visitors.count'],
    dimensions: ['external_data.category'],  // 来自 BigQuery
    timezone: 'UTC',
    externalDbType: 'bigquery',  // 外部数据源使用 BigQuery 语法
  }
);
```

**使用场景**：
- 数据湖查询（PostgreSQL + S3/Athena）
- 混合云架构（本地数据库 + 云数据仓库）
- 实时数据 + 历史数据（OLTP + OLAP）

---

### 示例 6：多数据库支持

```typescript
// 根据配置动态选择数据库类型
function createDynamicQuery(dbConfig, queryDef) {
  const dbTypeMapping = {
    'pg': 'postgres',
    'mysql': 'mysql',
    'mssql': 'mssql',
    'bq': 'bigquery'
  };

  const dbType = dbTypeMapping[dbConfig.type];

  return createQuery(
    { compiler, joinGraph, cubeEvaluator },
    dbType,
    {
      ...queryDef,
      timezone: dbConfig.timezone
    }
  );
}

// 使用
const query = createDynamicQuery(
  { type: 'pg', timezone: 'UTC' },
  { measures: ['visitors.count'] }
);
```

---

## 在 Cube 架构中的位置

### 调用链路

```
1. 用户请求
   ↓
2. API Gateway (cubejs-api-gateway)
   - REST API: /cubejs-api/v1/load
   - GraphQL API: /cubejs-api/graphql
   ↓
3. Query Orchestrator (cubejs-query-orchestrator)
   - 缓存检查
   - 查询队列管理
   ↓
4. Schema Compiler (cubejs-schema-compiler)
   - 编译数据模型
   - 解析查询定义
   ↓
5. createQuery() ← 你在这里
   - 选择数据库适配器
   - 创建 Query 实例
   ↓
6. Query.buildSqlAndParams()
   - 生成 SQL 字符串
   - 生成参数数组
   ↓
7. Database Driver (cubejs-xxx-driver)
   - 执行 SQL
   - 返回结果集
   ↓
8. 结果处理和返回
```

---

### 在 BaseQuery 中的使用（行 3963）

`createQuery` 主要通过 `QueryFactory` 在 `BaseQuery.newSubQueryForCube()` 中被调用：

```typescript
// BaseQuery.js:3956-3967
newSubQueryForCube(cube, options) {
  options = { ...options };
  if (this.options.queryFactory) {
    // 当处理 rollup joins 时，对于使用中的特定 cube，使用正确的参数分配器至关重要
    // 默认情况下我们使用 BaseQuery，但需要注意不同数据库（Oracle, PostgreSQL, MySQL, Druid 等）
    // 有独特的参数分配器符号。使用错误的分配器会破坏查询，特别是当 rollup joins 涉及
    // 需要不同分配器的不同 cubes 时
    return this.options.queryFactory.createQuery(
      cube,
      this.compilers,
      { ...this.subQueryOptions(options), paramAllocator: null }
    );
  }

  return this.newSubQuery(options);
}
```

**关键点**：
- **用途**：处理预聚合连接（rollup joins）
- **问题**：不同数据库的参数占位符不同
  - PostgreSQL: `$1, $2, $3...`
  - MySQL: `?, ?, ?...`
  - Oracle: `:1, :2, :3...`
- **解决方案**：通过 QueryFactory 为每个 cube 创建正确的 Query 实例
- **参数重置**：`paramAllocator: null` 确保子查询使用新的参数分配器

---

## 错误处理

### 1. 不支持的数据库类型（行 46-48）

```typescript
if (!queryOptions.dialectClass && !ADAPTERS[dbType]) {
  return null;  // 返回 null，不抛出异常
}
```

**行为**：
- 返回 `null` 而不是抛出异常
- 调用方需要检查返回值

**使用示例**：
```typescript
const query = createQuery(compilers, 'unknown_db', queryOptions);
if (!query) {
  console.error('Unsupported database type: unknown_db');
  // 处理错误
}
```

---

### 2. 无效的外部数据库类型（行 52-55）

```typescript
if (!externalQueryClass && queryOptions.externalDbType) {
  if (!ADAPTERS[queryOptions.externalDbType]) {
    throw new Error(`Dialect for '${queryOptions.externalDbType}' is not found`);
  }
  externalQueryClass = ADAPTERS[queryOptions.externalDbType];
}
```

**行为**：
- 抛出异常，明确指出哪个方言找不到
- 防止静默失败

**使用示例**：
```typescript
try {
  const query = createQuery(compilers, 'postgres', {
    measures: ['visitors.count'],
    timezone: 'UTC',
    externalDbType: 'invalid_db'  // 会抛出异常
  });
} catch (error) {
  console.error(error.message);  // "Dialect for 'invalid_db' is not found"
}
```

---

## 支持的数据库完整列表

| 数据库类型 | dbType 字符串 | Query 类 | 备注 |
|-----------|--------------|----------|------|
| PostgreSQL | `postgres` | PostgresQuery | 原生支持 |
| Amazon Redshift | `redshift` | RedshiftQuery | 继承自 PostgresQuery |
| MySQL | `mysql` | MysqlQuery | 原生支持 |
| MySQL Aurora Serverless | `mysqlauroraserverless` | MysqlQuery | 使用 MySQL 适配器 |
| MongoDB BI Connector | `mongobi` | MongoBiQuery | - |
| Microsoft SQL Server | `mssql` | MssqlQuery | 原生支持 |
| Google BigQuery | `bigquery` | BigqueryQuery | - |
| PrestoDB | `prestodb` | PrestodbQuery | 原生支持 |
| Qubole Presto | `qubole_prestodb` | PrestodbQuery | 使用 Presto 适配器 |
| AWS Athena | `athena` | PrestodbQuery | 使用 Presto 适配器 |
| Vertica | `vertica` | VerticaQuery | - |
| Snowflake | `snowflake` | SnowflakeQuery | - |
| ClickHouse | `clickhouse` | ClickHouseQuery | - |
| CrateDB | `crate` | CrateQuery | 继承自 PostgresQuery |
| Apache Hive | `hive` | HiveQuery | - |
| Oracle | `oracle` | OracleQuery | - |
| SQLite | `sqlite` | SqliteQuery | - |
| AWS Elasticsearch | `awselasticsearch` | AWSElasticSearchQuery | - |
| Elasticsearch | `elasticsearch` | ElasticSearchQuery | - |
| Materialize | `materialize` | PostgresQuery | PostgreSQL 兼容 |
| CubeStore | `cubestore` | CubeStoreQuery | Cube 专有存储引擎 |

---

## 设计模式分析

### 工厂模式（Factory Pattern）

**意图**：定义一个创建对象的接口，让子类决定实例化哪一个类

**实现**：
```typescript
// 简单工厂
const ADAPTERS = { /* 映射表 */ };

// 工厂方法
export const createQuery = (compilers, dbType, queryOptions) => {
  const QueryClass = queryClass(dbType, queryOptions.dialectClass);
  return new QueryClass(compilers, queryOptions);
};
```

**优点**：
1. **解耦**：客户端代码不需要知道具体的 Query 类
2. **扩展性**：添加新数据库只需在 ADAPTERS 中注册
3. **统一接口**：所有 Query 类实现相同的接口
4. **灵活性**：支持自定义方言类覆盖默认行为

**缺点**：
1. **类膨胀**：每个数据库都需要一个 Query 类
2. **映射维护**：ADAPTERS 映射表需要手动维护

---

### 策略模式（Strategy Pattern）

**意图**：定义一系列算法，把它们一个个封装起来，并且使它们可以相互替换

**实现**：
```typescript
// 不同的 SQL 生成策略
class PostgresQuery { /* PostgreSQL SQL 生成策略 */ }
class MysqlQuery { /* MySQL SQL 生成策略 */ }
class BigqueryQuery { /* BigQuery SQL 生成策略 */ }

// 运行时选择策略
const query = createQuery(compilers, dbType, queryOptions);
```

**优点**：
1. **算法独立**：每个数据库的 SQL 生成逻辑独立
2. **运行时切换**：可以根据配置动态选择数据库
3. **避免条件语句**：不需要大量 if-else 判断数据库类型

---

## 扩展指南

### 添加新数据库支持

```typescript
// 1. 创建新的 Query 类
// src/adapter/MyDatabaseQuery.ts
import { BaseQuery } from './BaseQuery';

export class MyDatabaseQuery extends BaseQuery {
  public convertTz(field: string): string {
    return `CONVERT_TIMEZONE('${this.timezone}', ${field})`;
  }

  public timeGroupedColumn(granularity: string, dimension: string): string {
    return `DATE_TRUNC('${granularity}', ${dimension})`;
  }

  public sqlTemplates() {
    const templates = super.sqlTemplates();
    templates.params.param = '?';  // 参数占位符
    // 自定义其他模板...
    return templates;
  }
}

// 2. 注册到 ADAPTERS
// src/adapter/QueryBuilder.ts
import { MyDatabaseQuery } from './MyDatabaseQuery';

const ADAPTERS = {
  // ...
  mydatabase: MyDatabaseQuery,
};

// 3. 使用
const query = createQuery(compilers, 'mydatabase', queryOptions);
```

---

### 扩展现有适配器

```typescript
// 创建扩展类
class EnhancedPostgresQuery extends PostgresQuery {
  // 添加企业特定的函数
  public encryptColumn(column: string): string {
    return `pgp_sym_encrypt(${column}, '${this.encryptionKey}')`;
  }

  // 覆写现有方法
  public timeGroupedColumn(granularity: string, dimension: string): string {
    // 添加自定义逻辑
    if (granularity === 'fiscal_year') {
      return `fiscal_year(${dimension})`;
    }
    return super.timeGroupedColumn(granularity, dimension);
  }
}

// 使用自定义类
const query = createQuery(compilers, 'postgres', {
  measures: ['visitors.count'],
  timezone: 'UTC',
  dialectClass: EnhancedPostgresQuery
});
```

---

## 性能优化建议

### 1. 缓存 Query 类

```typescript
// 避免重复查找
const queryClassCache = new Map();

function getCachedQueryClass(dbType: string, dialectClass: any) {
  const key = `${dbType}:${dialectClass?.name || 'default'}`;
  if (!queryClassCache.has(key)) {
    queryClassCache.set(key, queryClass(dbType, dialectClass));
  }
  return queryClassCache.get(key);
}
```

---

### 2. 重用 Query 实例（谨慎使用）

```typescript
// 注意：只有在 queryOptions 完全相同时才能重用
const queryCache = new Map();

function getCachedQuery(compilers, dbType, queryOptions) {
  const key = JSON.stringify({ dbType, queryOptions });
  if (!queryCache.has(key)) {
    queryCache.set(key, createQuery(compilers, dbType, queryOptions));
  }
  return queryCache.get(key);
}
```

**警告**：Query 实例包含状态，重用需要确保线程安全

---

### 3. 延迟加载 Query 类

```typescript
// 按需加载，减少初始启动时间
const ADAPTERS_LAZY = {
  postgres: () => require('./PostgresQuery').PostgresQuery,
  mysql: () => require('./MysqlQuery').MysqlQuery,
  // ...
};

export const createQueryLazy = (compilers, dbType, queryOptions) => {
  const QueryClass = ADAPTERS_LAZY[dbType]();
  return new QueryClass(compilers, queryOptions);
};
```

---

## 测试建议

### 单元测试示例

```typescript
import { createQuery, queryClass } from '../src/adapter/QueryBuilder';
import { PostgresQuery } from '../src/adapter/PostgresQuery';

describe('QueryBuilder', () => {
  describe('queryClass', () => {
    it('should return correct query class for postgres', () => {
      expect(queryClass('postgres', null)).toBe(PostgresQuery);
    });

    it('should return custom dialect class when provided', () => {
      class CustomQuery {}
      expect(queryClass('postgres', CustomQuery)).toBe(CustomQuery);
    });
  });

  describe('createQuery', () => {
    it('should create PostgresQuery instance', () => {
      const query = createQuery(compilers, 'postgres', { timezone: 'UTC' });
      expect(query).toBeInstanceOf(PostgresQuery);
    });

    it('should return null for unsupported database', () => {
      const query = createQuery(compilers, 'unknown', { timezone: 'UTC' });
      expect(query).toBeNull();
    });

    it('should throw error for invalid externalDbType', () => {
      expect(() => {
        createQuery(compilers, 'postgres', {
          timezone: 'UTC',
          externalDbType: 'invalid'
        });
      }).toThrow("Dialect for 'invalid' is not found");
    });
  });
});
```

---

## 常见问题（FAQ）

### Q1: createQuery 返回 null 是什么原因？

**A**: 数据库类型不支持，且没有提供自定义 `dialectClass`。检查：
1. `dbType` 拼写是否正确
2. 数据库类型是否在 ADAPTERS 中
3. 是否需要提供 `queryOptions.dialectClass`

---

### Q2: 如何支持自定义数据库？

**A**: 两种方式：
1. 继承现有 Query 类，通过 `dialectClass` 传入
2. 修改 ADAPTERS 映射表，添加新的数据库类型

---

### Q3: externalDbType 有什么用？

**A**: 用于联邦查询（跨数据库查询）：
- 主查询使用一个数据库（如 PostgreSQL）
- 外部表使用另一个数据库（如 BigQuery）
- 系统会为外部表生成对应数据库的 SQL 语法

---

### Q4: 参数分配器（paramAllocator）是什么？

**A**: 负责生成参数占位符：
- PostgreSQL: `$1, $2, $3...`
- MySQL: `?, ?, ?...`
- Oracle: `:1, :2, :3...`

通常不需要手动管理，系统会自动选择。

---

### Q5: 如何调试生成的 SQL？

**A**:
```typescript
const query = createQuery(compilers, 'postgres', queryOptions);
const [sql, params] = query.buildSqlAndParams();
console.log('SQL:', sql);
console.log('Params:', params);
```

---

## 相关文件

### 核心文件
- **QueryBuilder.ts** - 工厂方法实现
- **QueryFactory.ts** - Query 工厂类（用于子查询）
- **BaseQuery.js** - 所有 Query 类的基类
- **ParamAllocator.ts** - 参数分配器基类

### Query 实现
- **PostgresQuery.ts** - PostgreSQL 适配器
- **MysqlQuery.ts** - MySQL 适配器
- **MssqlQuery.ts** - SQL Server 适配器
- **BigqueryQuery.ts** - BigQuery 适配器
- **SnowflakeQuery.ts** - Snowflake 适配器
- **RedshiftQuery.ts** - Redshift 适配器
- **ClickHouseQuery.ts** - ClickHouse 适配器
- **CubeStoreQuery.ts** - CubeStore 适配器

### 测试文件
- **test/unit/postgres-query.test.ts** - PostgresQuery 单元测试
- **test/integration/postgres/pre-aggregations.test.ts** - 预聚合集成测试

---

## 总结

### 关键优势

1. **工厂模式**：统一的创建接口，隐藏实例化细节
2. **类型安全**：通过 `dbType` 自动选择正确的 Query 类
3. **可扩展性**：支持自定义 `dialectClass` 覆盖默认行为
4. **联邦查询**：通过 `externalDbType` 实现跨数据库查询
5. **灵活配置**：`queryOptions` 提供丰富的配置选项
6. **错误处理**：清晰的错误信息，易于调试

### 使用建议

| 场景 | 推荐方式 | 原因 |
|------|---------|------|
| **生产代码** | `createQuery()` | 自动类型选择、支持联邦查询 |
| **单元测试** | `new PostgresQuery()` | 明确、简单、易于 mock |
| **自定义适配器** | `dialectClass` 参数 | 无需修改 ADAPTERS 映射 |
| **多数据库支持** | 动态 `dbType` | 灵活切换数据库 |
| **性能优化** | 缓存 Query 类 | 避免重复查找 |

### 设计原则

- **单一职责**：每个 Query 类只负责一种数据库的 SQL 生成
- **开闭原则**：对扩展开放（可添加新数据库），对修改封闭
- **依赖倒置**：依赖于 BaseQuery 抽象，而非具体实现
- **里氏替换**：所有 Query 类可互换使用

---

## 参考资料

- **Cube 文档**: https://cube.dev/docs
- **源代码**: packages/cubejs-schema-compiler/src/adapter/
- **设计模式**: 《Design Patterns: Elements of Reusable Object-Oriented Software》
