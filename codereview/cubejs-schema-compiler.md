# @cubejs-backend/schema-compiler 包功能分析

## 概述

`@cubejs-backend/schema-compiler` 是 Cube 的**数据模型编译器和 SQL 查询生成引擎**，充当 OLAP 分析的 ORM 层。它将 Cube 的数据模型定义（JavaScript/YAML/Python）编译成可执行的 SQL 查询。

**Package 信息:**
- **名称**: `@cubejs-backend/schema-compiler`
- **描述**: Cube schema compiler
- **License**: Apache 2.0
- **位置**: `packages/cubejs-schema-compiler/`

---

## 核心职责

Schema Compiler 作为 ORM for analytics，负责：
1. 将高层数据模型（Cubes、维度、度量）编译成低层 SQL 查询
2. 支持 20+ 种数据库方言的 SQL 生成
3. 管理预聚合（rollup）表的定义和选择
4. 提供数据模型验证和错误报告
5. 自动生成 Cube schema 脚手架

它是连接**业务逻辑层**（数据模型定义）和**数据层**（SQL 数据库）的桥梁。

---

## 目录结构

```
src/
├── adapter/          # SQL 方言适配器和查询生成
├── compiler/         # Schema 编译核心逻辑
├── parser/           # SQL 和 Python 语法解析器
├── scaffolding/      # Schema 脚手架生成器
├── extensions/       # 扩展系统
└── index.ts          # 导出入口
```

---

## 主要功能模块

### 1. Schema 编译器 (`/src/compiler`)

#### **DataSchemaCompiler** (`DataSchemaCompiler.ts`)
核心编译引擎，负责整个编译流程：

- **多语言解析**: 解析和编译 Cube 数据模型文件（.js、.yaml、.py）
- **多线程转译**: 使用 workerpool 进行并行转译
  - 根据 CPU 核心数自动分配工作线程（最多 5 个）
  - 将文件分块并发处理
- **AST 转换**: 使用 Babel 进行 JavaScript AST 解析和转换
- **模板支持**: 支持 Jinja 模板语法（Python 风格）
- **沙箱执行**: 使用 Node.js VM 模块安全执行用户定义的代码
- **原生加速**: 通过 `@cubejs-backend/native` 进行 Python/YAML 转译

**关键特性:**
```typescript
export type DataSchemaCompilerOptions = {
  compilerCache: CompilerCache;
  extensions?: Record<string, any>;
  nativeInstance: NativeInstance;
  cubeFactory: Function;
  cubeDictionary: CubeDictionary;
  cubeSymbols: CubeSymbols;
  transpilers?: TranspilerInterface[];
  yamlCompiler: YamlCompiler;
  // ...
};
```

#### **CubeEvaluator** (`CubeEvaluator.ts`)
评估和处理 Cube 定义：

- 处理 **measures**（度量）：count、sum、avg、min、max 等
- 处理 **dimensions**（维度）：普通维度、时间维度
- 处理 **segments**（段）：数据过滤条件
- 管理 **hierarchies**（层次结构）：用于 drill-down 分析
- 处理 **joins**（连接）：Cube 之间的关系
- 管理 **time dimensions** 和 **granularities**（时间粒度）

**核心类型定义:**
```typescript
export type MeasureDefinition = {
  type: string;
  sql(): string;
  rollingWindow?: any;
  filters?: any;
  drillFilters?: any;
  multiStage?: boolean;
  groupBy?: (...args: Array<unknown>) => Array<ToString>;
  timeShift?: TimeShiftDefinition[];
};

export type DimensionDefinition = {
  type: string;
  sql(): string;
  primaryKey?: true;
  fieldType?: string;
  multiStage?: boolean;
  shiftInterval?: string;
};
```

#### **CubeSymbols** (`CubeSymbols.ts`)
定义数据模型的类型系统：

- **Cube 定义结构**: measure、dimension、segment、pre-aggregation 等
- **Refresh key 配置**: 数据刷新策略
  - SQL-based refresh
  - Cron-based refresh (every)
  - Immutable data
- **Granularity 定义**: 自定义时间粒度
- **Time shift 定义**: 时间偏移和对比分析
- **Pre-aggregation 定义**: Rollup 表配置

**Refresh Key 类型:**
```typescript
export type CubeRefreshKey =
  | CubeRefreshKeySqlVariant      // { sql: () => string; every?: string }
  | CubeRefreshKeyEveryVariant    // { every: string; timezone?: string }
  | CubeRefreshKeyImmutableVariant; // { immutable: true }
```

#### **JoinGraph** (`JoinGraph.ts`)
构建和管理 Cube 之间的连接关系图：

- 自动解析表连接路径
- 生成最优连接树
- 处理多级连接（A → B → C）
- 验证连接循环和死路径

#### **PreAggregations 管理**
预聚合（Rollup）定义和管理：

- **Rollup 类型**:
  - `originalSql`: 基于原始 SQL 的预聚合
  - `rollup`: 标准 rollup
  - `autoRollup`: 自动 rollup
  - `rollupJoin`: 跨 Cube 的 rollup join
  - `rollupLambda`: 多阶段 Lambda rollup

- **分区策略**: 支持时间分区
- **刷新策略**: 增量刷新、定时刷新、不可变数据
- **索引配置**: 为预聚合表定义索引

#### **其他编译器组件**

- **CubeValidator** (`CubeValidator.ts`): Schema 验证
- **CompilerCache** (`CompilerCache.ts`): 编译结果缓存
- **ContextEvaluator** (`ContextEvaluator.js`): 上下文变量评估
- **CubeDictionary** (`CubeDictionary.js`): Cube 字典管理
- **YamlCompiler** (`YamlCompiler.ts`): YAML 格式编译
- **ErrorReporter** (`ErrorReporter.ts`): 错误收集和报告
- **UserError** (`UserError.ts`): 用户友好的错误类型

---

### 2. SQL 适配器 (`/src/adapter`)

#### **BaseQuery** (`BaseQuery.js`)
抽象查询生成器基类，是所有数据库方言的基础：

**核心职责:**
- 将 Cube 查询请求转换为 SQL
- 管理查询元素：measures、dimensions、filters、time dimensions
- 处理 SQL 子句生成：SELECT、FROM、WHERE、GROUP BY、ORDER BY、JOIN
- 生成时间序列数据
- 参数化查询（防 SQL 注入）

**查询构建流程:**
```javascript
constructor(compilers, options)
  ↓
处理 measures, dimensions, timeDimensions, filters, segments
  ↓
buildJoin() - 构建 JOIN 子句
  ↓
buildSelect() - 构建 SELECT 子句
  ↓
buildWhere() - 构建 WHERE 子句
  ↓
buildGroupBy() - 构建 GROUP BY 子句
  ↓
buildOrderBy() - 构建 ORDER BY 子句
  ↓
buildSqlAndParams() - 生成最终 SQL 和参数
```

**标准时间粒度支持:**
```javascript
const standardGranularitiesParents = {
  year: ['year', 'quarter', 'month', 'day', 'hour', 'minute', 'second'],
  quarter: ['quarter', 'month', 'day', 'hour', 'minute', 'second'],
  month: ['month', 'day', 'hour', 'minute', 'second'],
  week: ['week', 'day', 'hour', 'minute', 'second'],
  day: ['day', 'hour', 'minute', 'second'],
  hour: ['hour', 'minute', 'second'],
  minute: ['minute', 'second'],
  second: ['second']
};
```

#### **数据库方言实现**
每个数据库都有专门的 Query 类继承自 BaseQuery：

| 方言类 | 数据库 | 特殊处理 |
|--------|--------|----------|
| `PostgresQuery` | PostgreSQL | 标准 SQL，EXTRACT 函数 |
| `MysqlQuery` | MySQL | LIMIT 语法，日期函数 |
| `BigqueryQuery` | BigQuery | 数组和结构体，分区表 |
| `SnowflakeQuery` | Snowflake | VARIANT 类型，时间旅行 |
| `ClickHouseQuery` | ClickHouse | Array Join，物化视图 |
| `RedshiftQuery` | Redshift | 分布键，排序键 |
| `MssqlQuery` | SQL Server | TOP 子句，T-SQL 函数 |
| `OracleQuery` | Oracle | ROWNUM，分页 |
| `PrestodbQuery` | Presto | 分布式查询，数组函数 |
| `HiveQuery` | Hive | 分区表，UDF |
| `ElasticSearchQuery` | Elasticsearch | DSL 转换 |
| `CubeStoreQuery` | CubeStore | 内部 OLAP 引擎 |
| 等... | | |

**方言特定功能:**
- 日期/时间函数转换（DATE_TRUNC, EXTRACT, DATE_FORMAT 等）
- 数据类型映射和转换
- 聚合函数语法差异
- 窗口函数支持
- 分页语法（LIMIT/OFFSET vs FETCH FIRST）
- 字符串拼接和函数

#### **其他适配器组件**

- **PreAggregations** (`PreAggregations.ts`): 预聚合表选择和管理
  - 自动选择最优预聚合表
  - 匹配查询和预聚合定义
  - 处理分区裁剪
  - Rollup join 处理

- **QueryBuilder** (`QueryBuilder.ts`): 查询构建器
  - 组合各种 SQL 片段
  - 子查询和 CTE（公用表表达式）生成
  - 嵌套查询处理

- **BaseMeasure/BaseDimension/BaseTimeDimension**: 查询元素类
  - 封装 measure、dimension、time dimension 的 SQL 生成逻辑
  - 处理别名、表达式、过滤器

- **BaseFilter/BaseGroupFilter**: 过滤器类
  - WHERE 条件生成
  - 运算符处理（=, !=, <, >, in, notIn, contains 等）
  - AND/OR 逻辑组合

- **ParamAllocator** (`ParamAllocator.ts`): 参数分配器
  - 生成参数化查询
  - 防止 SQL 注入

- **Granularity** (`Granularity.ts`): 时间粒度处理
  - 标准粒度（year, quarter, month, week, day, hour, minute, second）
  - 自定义粒度

---

### 3. 解析器 (`/src/parser`)

#### **SqlParser** (`SqlParser.ts`)
SQL 解析和验证：

- 使用 **ANTLR4** 生成的解析器（基于 `GenericSql.g4` 语法文件）
- 解析用户定义的 SQL 表达式
- 提取 SQL 中引用的表和列
- 验证 SQL 语法正确性
- 支持复杂的 SQL 表达式（子查询、CASE、窗口函数等）

**ANTLR4 生成的文件:**
- `GenericSqlLexer.ts` - 词法分析器
- `GenericSqlParser.ts` - 语法分析器
- `GenericSqlVisitor.ts` - 访问者模式接口
- `GenericSqlListener.ts` - 监听器模式接口

#### **PythonParser** (`PythonParser.ts`)
Python 语法解析：

- 支持 Python 风格的数据模型定义
- 基于 ANTLR4 的 Python3 语法（`Python3Lexer.g4`, `Python3Parser.g4`）
- 解析 Python 表达式和 Jinja 模板
- 转换 Python 代码为 JavaScript 可执行代码

**用途:**
- 允许用户使用 Python 语法定义 Cube
- Jinja 模板变量替换
- Python 到 JavaScript 的转译

---

### 4. 脚手架生成 (`/src/scaffolding`)

自动生成 Cube 定义的功能：

#### **ScaffoldingSchema** (`ScaffoldingSchema.ts`)
根据数据库 schema 自动生成 Cube 定义：

- 连接数据库并读取 schema 信息
- 推断 measures 和 dimensions
- 生成 joins（基于外键关系）
- 生成 YAML 或 JavaScript 格式的 Cube 文件

#### **ScaffoldingTemplate** (`ScaffoldingTemplate.ts`)
模板生成器：

- 提供可定制的模板
- 支持不同的输出格式（YAML、JavaScript、TypeScript）
- 生成符合最佳实践的 Cube 定义

#### **Descriptors 和 Formatters**
- `descriptors/`: 数据库 schema 描述符
- `formatters/`: 格式化输出（YAML、JS 等）

**应用场景:**
- 快速开始新项目
- 从现有数据库生成 Cube
- 数据模型原型设计

---

### 5. 扩展系统 (`/src/extensions`)

支持自定义扩展和插件：

- 允许注入自定义函数和上下文
- 扩展编译器功能
- 添加自定义验证规则
- 插件化架构

---

## 编译流程

```
┌─────────────────────────────────────────────────────────────┐
│ 1. 数据模型文件 (.js/.yaml/.py)                              │
└────────────────────┬────────────────────────────────────────┘
                     ↓
┌─────────────────────────────────────────────────────────────┐
│ 2. DataSchemaCompiler                                       │
│    - 语法检查 (syntax-error, Babel parser)                  │
│    - 多线程转译 (workerpool)                                │
│    - YAML → JS, Python → JS                                 │
│    - VM 沙箱执行用户代码                                    │
└────────────────────┬────────────────────────────────────────┘
                     ↓
┌─────────────────────────────────────────────────────────────┐
│ 3. CubeEvaluator                                            │
│    - 评估 Cube 定义                                         │
│    - 解析 measures, dimensions, segments                   │
│    - 处理 hierarchies, pre-aggregations                    │
│    - 验证数据模型 (CubeValidator)                           │
└────────────────────┬────────────────────────────────────────┘
                     ↓
┌─────────────────────────────────────────────────────────────┐
│ 4. JoinGraph                                                │
│    - 构建 Cube 连接关系图                                   │
│    - 计算最优连接路径                                       │
│    - 验证连接有效性                                         │
└────────────────────┬────────────────────────────────────────┘
                     ↓
┌─────────────────────────────────────────────────────────────┐
│ 5. BaseQuery / 数据库方言适配器                             │
│    - 接收查询请求（measures, dimensions, filters 等）       │
│    - 选择最优预聚合表（如果可用）                           │
│    - 生成特定数据库的 SQL                                   │
│    - 参数化查询（防 SQL 注入）                              │
└────────────────────┬────────────────────────────────────────┘
                     ↓
┌─────────────────────────────────────────────────────────────┐
│ 6. 最终 SQL 查询 + 参数                                      │
│    { sql: "SELECT ...", params: [...] }                     │
└─────────────────────────────────────────────────────────────┘
```

---

## 关键特性

### 1. 多语言支持

**JavaScript/TypeScript** (主要):
```javascript
cube('Orders', {
  sql: `SELECT * FROM orders`,

  measures: {
    count: {
      type: 'count'
    },
    totalAmount: {
      sql: `amount`,
      type: 'sum'
    }
  },

  dimensions: {
    status: {
      sql: `status`,
      type: 'string'
    },
    createdAt: {
      sql: `created_at`,
      type: 'time'
    }
  }
});
```

**YAML** (声明式):
```yaml
cubes:
  - name: Orders
    sql: SELECT * FROM orders

    measures:
      - name: count
        type: count
      - name: totalAmount
        sql: amount
        type: sum

    dimensions:
      - name: status
        sql: status
        type: string
      - name: createdAt
        sql: created_at
        type: time
```

**Python** (通过 Jinja):
```python
cube('Orders', {
  'sql': 'SELECT * FROM orders',
  'measures': {
    'count': {'type': 'count'}
  }
})
```

### 2. 性能优化

**编译缓存:**
- `CompilerCache`: 编译结果缓存
- `LRUCache`: 脚本和 YAML 缓存
- 避免重复编译相同的文件

**多线程转译:**
```typescript
const getThreadsCount = () => {
  const cpuCount = os.cpus()?.length;
  // 最多 5 个线程，即使有更多 CPU 核心
  return Math.min(Math.max(1, cpuCount - 1), 5);
};
```

**增量编译:**
- 只重新编译变更的文件
- 文件 hash 检测变化
- 依赖追踪

### 3. 预聚合系统

**自动预聚合选择:**
```typescript
// PreAggregations.ts
export type PreAggregationForQuery = {
  preAggregationName: string;
  cube: string;
  canUsePreAggregation: boolean;
  preAggregation: PreAggregationDefinitionExtended;
  references: PreAggregationReferences;
};
```

**预聚合类型:**
- **originalSql**: 基于原始 SQL 的物化表
- **rollup**: 聚合表（减少行数）
- **rollupJoin**: 跨 Cube 的预连接
- **rollupLambda**: 多阶段聚合（Lambda 架构）

**分区支持:**
- 时间分区（按天、月、年）
- 分区裁剪优化
- 滚动窗口更新

**刷新策略:**
```javascript
refreshKey: {
  every: '1 hour'  // Cron 表达式
}

refreshKey: {
  sql: `SELECT MAX(updated_at) FROM orders`  // SQL-based
}

refreshKey: {
  immutable: true  // 不可变数据
}
```

### 4. 错误处理和验证

**UserError 类型:**
```typescript
export class UserError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'UserError';
  }
}
```

**验证功能:**
- 语法检查（syntax-error 库）
- Schema 验证（Joi 库）
- 循环引用检测
- 类型检查
- SQL 语法验证

**错误报告:**
- 精确的错误位置（文件、行、列）
- 上下文代码片段
- 用户友好的错误消息
- 错误堆栈追踪

---

## 测试结构

### 单元测试 (`/test/unit`)

- 编译器逻辑测试
- SQL 生成测试
- 快照测试（snapshot testing）
- 验证生成的 SQL 是否符合预期

**运行单元测试:**
```bash
cd packages/cubejs-schema-compiler
yarn unit
```

### 集成测试 (`/test/integration`)

针对真实数据库的测试：

**支持的数据库:**
- PostgreSQL (`yarn integration:postgres`)
- MySQL (`yarn integration:mysql`)
- MSSQL (`yarn integration:mssql`)
- ClickHouse (`yarn integration:clickhouse`)

**测试环境:**
- 使用 **Testcontainers** 启动数据库容器
- 真实的数据库连接和查询执行
- 验证 SQL 执行结果
- 测试特定数据库的方言功能

**运行集成测试:**
```bash
# 所有集成测试
yarn integration

# 特定数据库
yarn integration:postgres
```

### 测试覆盖率

配置在 `jest.config.js`:
```javascript
module.exports = {
  coverageDirectory: 'coverage/',
  collectCoverageFrom: [
    'src/**/*.{ts,js}',
  ],
  testMatch: [
    '**/test/**/*.test.{ts,js}'
  ]
};
```

---

## 依赖关系

### 核心依赖

**编译和解析:**
- `@babel/core`, `@babel/parser`, `@babel/traverse` - JavaScript AST 处理
- `antlr4` - SQL 和 Python 语法解析器生成
- `js-yaml`, `yaml` - YAML 解析
- `syntax-error` - 语法错误检测

**数据处理:**
- `ramda` - 函数式编程工具库
- `inflection` - 单复数转换、驼峰命名
- `moment-timezone` - 时间处理
- `cron-parser` - Cron 表达式解析

**验证:**
- `joi` - Schema 验证

**性能:**
- `workerpool` - 多线程工作池
- `lru-cache` - LRU 缓存

**原生加速:**
- `@cubejs-backend/native` - Rust 原生模块（Python/YAML 转译加速）

### 开发依赖

**测试:**
- `jest` - 测试框架
- `testcontainers` - Docker 容器测试
- 数据库驱动: `pg-promise`, `mysql`, `mssql`, `@clickhouse/client`

**代码质量:**
- `@cubejs-backend/linter` - ESLint 配置
- `typescript` ~5.2.2

---

## 配置选项

### DataSchemaCompilerOptions

```typescript
export type DataSchemaCompilerOptions = {
  // 编译缓存
  compilerCache: CompilerCache;

  // 错误处理
  omitErrors?: boolean;
  errorReport?: ErrorReporterOptions;

  // 扩展
  extensions?: Record<string, any>;

  // 待编译文件
  filesToCompile?: string[];

  // 原生实例（Rust 加速）
  nativeInstance: NativeInstance;

  // Cube 工厂函数
  cubeFactory: Function;
  cubeDictionary: CubeDictionary;

  // Symbol 定义
  cubeOnlySymbols: CubeSymbols;
  cubeAndViewSymbols: CubeSymbols;

  // 编译器链
  cubeCompilers?: CompilerInterface[];
  contextCompilers?: CompilerInterface[];
  transpilers?: TranspilerInterface[];
  viewCompilers?: CompilerInterface[];

  // YAML 编译器
  yamlCompiler: YamlCompiler;

  // 缓存
  compiledScriptCache: LRUCache<string, vm.Script>;
  compiledYamlCache: LRUCache<string, string>;
  compiledJinjaCache: LRUCache<string, string>;

  // 其他选项
  compilerId?: string;
  standalone?: boolean;
  compileContext?: any;
  allowNodeRequire?: boolean;
};
```

---

## 使用场景

### 1. 基本查询编译

```javascript
// 用户请求
{
  measures: ['Orders.count', 'Orders.totalAmount'],
  dimensions: ['Orders.status'],
  timeDimensions: [{
    dimension: 'Orders.createdAt',
    granularity: 'day',
    dateRange: ['2024-01-01', '2024-01-31']
  }]
}

// 编译后的 SQL (PostgreSQL)
SELECT
  orders.status AS "orders__status",
  DATE_TRUNC('day', orders.created_at) AS "orders__created_at_day",
  COUNT(*) AS "orders__count",
  SUM(orders.amount) AS "orders__total_amount"
FROM orders
WHERE orders.created_at >= $1 AND orders.created_at <= $2
GROUP BY 1, 2
ORDER BY 2 ASC
```

### 2. 多 Cube Join

```javascript
// Orders joins Users
{
  measures: ['Orders.count'],
  dimensions: ['Users.country', 'Orders.status']
}

// 生成的 SQL
SELECT
  users.country AS "users__country",
  orders.status AS "orders__status",
  COUNT(*) AS "orders__count"
FROM orders
LEFT JOIN users ON orders.user_id = users.id
GROUP BY 1, 2
```

### 3. 预聚合使用

```javascript
// Cube 定义中的预聚合
preAggregations: {
  ordersRollup: {
    type: 'rollup',
    measures: [Orders.count, Orders.totalAmount],
    dimensions: [Orders.status],
    timeDimension: Orders.createdAt,
    granularity: 'day'
  }
}

// 查询自动使用预聚合表而非原始表
SELECT ... FROM cube_pre_aggregations.orders_rollup
```

### 4. 时间序列分析

```javascript
// 按小时统计订单数量（过去 7 天）
{
  measures: ['Orders.count'],
  timeDimensions: [{
    dimension: 'Orders.createdAt',
    granularity: 'hour',
    dateRange: 'last 7 days'
  }]
}

// 生成时间序列数据（填充空缺时间点）
// 2024-01-20 00:00:00 | 10
// 2024-01-20 01:00:00 | 0   <- 填充
// 2024-01-20 02:00:00 | 15
// ...
```

---

## 架构设计亮点

### 1. 编译器链模式

通过 CompilerInterface 实现编译器链：
```typescript
export interface CompilerInterface {
  compile(cube: any): any;
}
```

允许多个编译器按顺序处理 Cube 定义：
- PrepareCompiler - 预处理
- CubeValidator - 验证
- 自定义编译器 - 扩展功能

### 2. 访问者模式

使用 ANTLR Visitor 模式遍历 AST：
```typescript
export class GenericSqlVisitor<Result> extends AbstractParseTreeVisitor<Result> {
  visitSelectStatement(ctx: SelectStatementContext): Result;
  visitWhereClause(ctx: WhereClauseContext): Result;
  // ...
}
```

### 3. 策略模式

不同数据库方言通过策略模式实现：
```javascript
class PostgresQuery extends BaseQuery {
  // PostgreSQL 特定实现
}

class MysqlQuery extends BaseQuery {
  // MySQL 特定实现
}
```

### 4. 工厂模式

通过 DriverFactory 创建正确的数据库适配器：
```javascript
const queryClass = driverFactory.getDialectClass(databaseType);
const query = new queryClass(compilers, options);
```

### 5. 依赖注入

通过 options 对象注入依赖：
```typescript
constructor(compilers, options) {
  this.cubeEvaluator = compilers.cubeEvaluator;
  this.joinGraph = compilers.joinGraph;
  this.options = options;
}
```

---

## 性能考虑

### 1. 编译缓存层次

```
┌─────────────────────────────────────────┐
│ Level 1: compiledScriptCache (LRU)     │
│   - 编译后的 VM Script                  │
│   - 避免重复 VM 编译                    │
└─────────────────────────────────────────┘
           ↓
┌─────────────────────────────────────────┐
│ Level 2: compiledYamlCache (LRU)       │
│   - YAML 转 JS 的结果                   │
└─────────────────────────────────────────┘
           ↓
┌─────────────────────────────────────────┐
│ Level 3: CompilerCache                 │
│   - 完整的编译结果                      │
│   - Cube 定义、metadata 等              │
└─────────────────────────────────────────┘
```

### 2. 多线程转译

对于大型项目（多个 schema 文件）：
- 文件分块（chunk）
- 并行转译（workerpool）
- 自动根据 CPU 核心数调整线程数

### 3. 惰性编译

- 只编译实际使用的 Cube
- View 按需编译（ViewCompilationGate）
- 增量编译支持

### 4. 原生加速

使用 Rust 编写的原生模块加速：
- Python 转译（`@cubejs-backend/native`）
- YAML 解析
- SQL 参数化（`nativeBuildSqlAndParams`）

---

## 扩展性

### 1. 自定义数据库方言

```javascript
class CustomDbQuery extends BaseQuery {
  // 重写方法实现自定义逻辑
  timeStampCast(value) {
    return `CUSTOM_TIMESTAMP(${value})`;
  }

  dateTimeCast(value) {
    return `CUSTOM_DATETIME(${value})`;
  }
}
```

### 2. 自定义编译器

```typescript
class CustomCompiler implements CompilerInterface {
  compile(cube: any) {
    // 自定义编译逻辑
    return modifiedCube;
  }
}
```

### 3. 自定义 Transpiler

```typescript
class CustomTranspiler implements TranspilerInterface {
  transpile(file: FileContent): string {
    // 自定义转译逻辑
    return transpiledCode;
  }
}
```

---

## 总结

### 核心价值

`@cubejs-backend/schema-compiler` 提供了：

1. **抽象层**: 将业务逻辑（Cube 定义）与数据库实现解耦
2. **多数据库支持**: 一份代码，支持 20+ 种数据库
3. **性能优化**: 预聚合、缓存、多线程编译
4. **开发体验**: 多语言支持、自动脚手架、清晰的错误消息
5. **可扩展性**: 插件化架构，支持自定义扩展

### 在 Cube 架构中的位置

```
┌──────────────────────────────────────────┐
│ API Gateway (REST/GraphQL/SQL)           │
└──────────────┬───────────────────────────┘
               ↓
┌──────────────────────────────────────────┐
│ Schema Compiler (本包)                   │ ← 你在这里
│ - 编译数据模型                           │
│ - 生成 SQL 查询                          │
│ - 管理预聚合                             │
└──────────────┬───────────────────────────┘
               ↓
┌──────────────────────────────────────────┐
│ Query Orchestrator                       │
│ - 查询执行                               │
│ - 缓存管理                               │
└──────────────┬───────────────────────────┘
               ↓
┌──────────────────────────────────────────┐
│ Database Drivers                         │
│ - 执行 SQL                               │
└──────────────────────────────────────────┘
```

### 关键技术

- **编译技术**: Babel AST、ANTLR4、VM 沙箱
- **查询优化**: 预聚合选择、查询重写、缓存
- **多语言支持**: JavaScript/TypeScript、YAML、Python
- **性能**: 多线程、LRU 缓存、原生模块
- **架构模式**: 策略、工厂、访问者、编译器链

---

## 相关文件

- **README**: `packages/cubejs-schema-compiler/README.md`
- **Package**: `packages/cubejs-schema-compiler/package.json`
- **Tests**: `packages/cubejs-schema-compiler/test/`
- **License**: Apache 2.0

---

**文档生成时间**: 2025-10-28
**Cube 版本**: 1.3.83
