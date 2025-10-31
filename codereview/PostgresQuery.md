# PostgresQuery 代码分析

## 概述

`PostgresQuery` 是 Cube 的查询适配器类，位于 `packages/cubejs-schema-compiler/src/adapter/PostgresQuery.ts`，用于生成 **PostgreSQL 特定的 SQL 查询语法**。它继承自 `BaseQuery`，为 PostgreSQL 数据库提供了特定的查询转换逻辑。

## 类继承关系

```
BaseQuery (抽象基类)
  └─ PostgresQuery (PostgreSQL 实现)
       ├─ RedshiftQuery (Amazon Redshift)
       └─ CrateQuery (CrateDB)
```

## 核心组件

### 1. PostgresParamAllocator (行 15-19)

**功能**：生成 PostgreSQL 风格的参数占位符

```typescript
class PostgresParamAllocator extends ParamAllocator {
  public paramPlaceHolder(paramIndex) {
    return `$${paramIndex + 1}`;  // PostgreSQL 使用 $1, $2, $3...
  }
}
```

**作用**：
- 将参数索引转换为 PostgreSQL 的占位符格式
- 索引从 0 开始，但 PostgreSQL 占位符从 $1 开始
- 示例：`WHERE name = $1 AND age > $2`

**安全性**：防止 SQL 注入攻击

---

### 2. newParamAllocator (行 22-24)

```typescript
public newParamAllocator(expressionParams) {
  return new PostgresParamAllocator(expressionParams);
}
```

工厂方法，用于创建参数分配器实例。

---

### 3. convertTz - 时区转换 (行 26-28)

```typescript
public convertTz(field: string): string {
  return `(${field}::timestamptz AT TIME ZONE '${this.timezone}')`;
}
```

**功能**：将时间戳字段转换到指定时区

**生成的 SQL 示例**：
```sql
(created_at::timestamptz AT TIME ZONE 'America/Los_Angeles')
```

**应用场景**：
- 处理跨时区的时间查询
- 确保时间维度在正确的时区下聚合

---

### 4. timeGroupedColumn - 时间分组 (行 30-32)

```typescript
public timeGroupedColumn(granularity: string, dimension: string): string {
  return `date_trunc('${GRANULARITY_TO_INTERVAL[granularity]}', ${dimension})`;
}
```

**功能**：按指定粒度对时间维度进行分组

**支持的粒度** (行 4-13)：
```typescript
const GRANULARITY_TO_INTERVAL = {
  day: 'day',
  week: 'week',
  hour: 'hour',
  minute: 'minute',
  second: 'second',
  month: 'month',
  quarter: 'quarter',
  year: 'year'
};
```

**生成的 SQL 示例**：
```sql
date_trunc('month', created_at)  -- 按月分组
date_trunc('day', created_at)    -- 按天分组
```

---

### 5. dateBin - 日期装箱 (行 40-46)

```typescript
public dateBin(interval: string, source: string, origin: string): string {
  return `('${origin}'::timestamp + INTERVAL '${interval}' *
    FLOOR(
      EXTRACT(EPOCH FROM (${source} - '${origin}'::timestamp)) /
      EXTRACT(EPOCH FROM INTERVAL '${interval}')
    ))`;
}
```

**功能**：将时间戳对齐到固定间隔的时间点

**数学原理**：
1. 计算源时间与原点的时间差（秒）
2. 除以间隔长度（秒）得到倍数
3. 向下取整后乘以间隔
4. 加上原点时间

**应用场景**：
- 自定义时间粒度（如财年、财季）
- 非标准时间间隔的分组

**生成的 SQL 示例**：
```sql
('2020-01-01'::timestamp + INTERVAL '3 months' * FLOOR(...))
```

**优势**：
- 支持任意时间间隔
- 支持偏移量（通过 origin 参数）
- PostgreSQL 和 RedShift 均支持

---

### 6. HyperLogLog 近似计数 (行 48-58)

#### 6.1 hllInit - 初始化 HLL

```typescript
public hllInit(sql) {
  return `hll_add_agg(hll_hash_any(${sql}))`;
}
```

**功能**：创建 HyperLogLog 数据结构用于近似去重计数

#### 6.2 hllMerge - 合并 HLL

```typescript
public hllMerge(sql) {
  return `round(hll_cardinality(hll_union_agg(${sql})))`;
}
```

**功能**：合并多个 HLL 结构并计算基数

#### 6.3 countDistinctApprox - 近似去重计数

```typescript
public countDistinctApprox(sql) {
  return `round(hll_cardinality(hll_add_agg(hll_hash_any(${sql}))))`;
}
```

**功能**：一步完成近似去重计数

**应用场景**：
- 大规模数据的唯一值计数
- 预聚合中的去重计数
- 性能优于精确计数（COUNT DISTINCT）

**精度**：通常误差在 2% 以内

**依赖**：需要 PostgreSQL 安装 `postgresql-hll` 扩展

---

### 7. supportGeneratedSeriesForCustomTd (行 60-62)

```typescript
public supportGeneratedSeriesForCustomTd() {
  return true;
}
```

**功能**：声明支持为自定义时间维度生成时间序列

**作用**：启用 `generate_series` 生成连续时间点

---

### 8. sqlTemplates - SQL 模板定义 (行 64-96)

这是 PostgresQuery 最复杂的方法，定义了 PostgreSQL 特定的 SQL 语法模板。

#### 8.1 参数占位符

```typescript
templates.params.param = '${{ param_index + 1 }}';
```

使用 PostgreSQL 的 `$1, $2...` 格式

#### 8.2 日期时间函数

```typescript
// 日期截断
templates.functions.DATETRUNC = 'DATE_TRUNC({{ args_concat }})';

// 日期部分提取
templates.functions.DATEPART = 'DATE_PART({{ args_concat }})';

// 当前日期
templates.functions.CURRENTDATE = 'CURRENT_DATE';

// 当前时间
templates.functions.NOW = 'NOW({{ args_concat }})';
```

#### 8.3 DATEDIFF 函数（行 78）

**复杂的日期差异计算**：

```typescript
templates.functions.DATEDIFF =
  'CASE WHEN LOWER(\'{{ date_part }}\') IN (\'year\', \'quarter\', \'month\')
   THEN (EXTRACT(YEAR FROM AGE(...)) * 12 + EXTRACT(MONTH FROM AGE(...))) /
        CASE LOWER(\'{{ date_part }}\')
          WHEN \'year\' THEN 12
          WHEN \'quarter\' THEN 3
          WHEN \'month\' THEN 1
        END
   ELSE EXTRACT(EPOCH FROM ...) / EXTRACT(EPOCH FROM \'1 {{ date_part }}\'::interval)
   END::bigint';
```

**处理逻辑**：
- **年/季/月**：使用 `AGE` 函数计算月份差异，再转换
- **周/天/时/分/秒**：使用 `EPOCH`（秒数）计算差异

#### 8.4 字符串拼接

```typescript
templates.functions.CONCAT =
  'CONCAT({% for arg in args %}CAST({{arg}} AS TEXT){% if not loop.last %},{% endif %}{% endfor %})';
```

**特点**：自动将所有参数转换为 TEXT 类型

#### 8.5 比较函数

```typescript
templates.functions.LEAST = 'LEAST({{ args_concat }})';
templates.functions.GREATEST = 'GREATEST({{ args_concat }})';
```

#### 8.6 表达式模板

```typescript
// 间隔表达式
templates.expressions.interval = 'INTERVAL \'{{ interval }}\'';

// 提取表达式
templates.expressions.extract = 'EXTRACT({{ date_part }} FROM {{ expr }})';

// 时间戳字面量
templates.expressions.timestamp_literal = 'timestamptz \'{{ value }}\'';
```

#### 8.7 数据类型映射

```typescript
templates.types.string = 'TEXT';
templates.types.tinyint = 'SMALLINT';
templates.types.float = 'REAL';
templates.types.double = 'DOUBLE PRECISION';
templates.types.binary = 'BYTEA';
```

**作用**：将通用类型名映射到 PostgreSQL 特定类型

#### 8.8 窗口函数支持

```typescript
templates.window_frame_types.groups = 'GROUPS';
```

支持 `GROUPS` 窗口框架类型

#### 8.9 运算符

```typescript
templates.operators.is_not_distinct_from = 'IS NOT DISTINCT FROM';
```

支持 NULL 安全比较

#### 8.10 时间序列生成（行 89-94）

**简单时间序列**：
```typescript
templates.statements.generated_time_series_select =
  'SELECT {{ date_from }} AS "date_from",\n' +
  '{{ date_to }} AS "date_to" \n' +
  'FROM generate_series({{ start }}::timestamp, {{ end }}::timestamp, {{ granularity }}::interval) d ';
```

**生成的 SQL 示例**：
```sql
SELECT d AS "date_from",
       d AS "date_to"
FROM generate_series('2020-01-01'::timestamp, '2020-12-31'::timestamp, '1 day'::interval) d
```

**带 CTE 的时间序列**：
```typescript
templates.statements.generated_time_series_with_cte_range_source =
  'SELECT d AS "date_from",\n' +
  'd + interval {{ granularity }} - interval \'1 millisecond\' AS "date_to" \n' +
  'FROM {{ range_source }}, LATERAL generate_series({{ range_source }}.{{ min_name }}, {{ range_source }}.{{ max_name }}, {{ granularity }}::interval) d ';
```

**特点**：
- `date_to` = `date_from` + 粒度 - 1 毫秒
- 使用 `LATERAL` 连接实现动态范围

---

### 9. shouldReuseParams (行 98-100)

```typescript
public get shouldReuseParams() {
  return true;
}
```

**功能**：允许重用相同的参数值

**作用**：
- 减少参数数量
- 优化查询性能
- PostgreSQL 支持参数重用

---

## 使用方式

### 基本用法

```typescript
import { PostgresQuery } from './adapter/PostgresQuery';

// 1. 实例化查询对象
const query = new PostgresQuery(
  {
    joinGraph,      // 表连接关系图
    cubeEvaluator,  // Cube 评估器
    compiler        // Schema 编译器
  },
  {
    // 查询定义
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
  }
);

// 2. 生成 SQL 和参数
const [sql, params] = query.buildSqlAndParams();

// 3. 执行查询
const result = await pgClient.query(sql, params);
```

### 生成的 SQL 示例

**输入查询**：
```javascript
{
  measures: ['visitors.count'],
  timeDimensions: [{
    dimension: 'visitors.createdAt',
    granularity: 'month'
  }],
  filters: [{
    member: 'visitors.name',
    operator: 'equals',
    values: ['John']
  }],
  timezone: 'America/Los_Angeles'
}
```

**生成的 SQL**：
```sql
SELECT
  date_trunc('month', (visitors.created_at::timestamptz AT TIME ZONE 'America/Los_Angeles')) AS "visitors__created_at_month",
  count(*) AS "visitors__count"
FROM visitors AS "visitors"
WHERE ("visitors".name = $1)
GROUP BY 1
ORDER BY 1 ASC
```

**参数**：`['John']`

---

## 在 Cube 架构中的位置

```
用户请求
   ↓
REST/GraphQL API (cubejs-api-gateway)
   ↓
Query Orchestrator (cubejs-query-orchestrator)
   ↓
Schema Compiler (cubejs-schema-compiler)
   ↓
PostgresQuery ← 你在这里
   ↓
Database Driver (cubejs-postgres-driver)
   ↓
PostgreSQL Database
```

---

## 测试用例位置

- **单元测试**：`packages/cubejs-schema-compiler/test/unit/postgres-query.test.ts`
- **集成测试**：`packages/cubejs-schema-compiler/test/integration/postgres/`

### 测试覆盖的功能

1. **参数化查询**：测试不同类型的参数（null, boolean, string）
2. **自定义粒度**：测试财年、财季等自定义时间粒度
3. **时区转换**：测试跨时区的时间维度处理
4. **排序逻辑**：测试多粒度时间维度的排序
5. **滚动窗口**：测试 unbounded 滚动窗口

---

## 继承自 BaseQuery 的关键方法

虽然 PostgresQuery 本身代码较少，但它继承了 BaseQuery 的大量功能：

1. **buildSqlAndParams()** - 构建 SQL 和参数的主入口
2. **joinGraph** - 处理表连接关系
3. **filtersCompiler** - 编译过滤条件
4. **dimensionsCompiler** - 编译维度
5. **measuresCompiler** - 编译度量
6. **preAggregations** - 处理预聚合

---

## 关键特性总结

| 特性 | 实现 | 优势 |
|------|------|------|
| **参数化查询** | `$1, $2...` | 防止 SQL 注入 |
| **时区处理** | `AT TIME ZONE` | 跨时区查询准确性 |
| **时间分组** | `date_trunc()` | 支持多种时间粒度 |
| **自定义粒度** | `dateBin()` | 支持财年等非标准粒度 |
| **近似去重** | HyperLogLog | 大规模数据性能优化 |
| **时间序列生成** | `generate_series()` | 自动填充缺失时间点 |
| **参数重用** | `shouldReuseParams=true` | 减少参数数量 |
| **类型安全** | 强类型映射 | 避免类型转换错误 |

---

## 适用的数据库

虽然名为 `PostgresQuery`，但实际上也适用于：

1. **PostgreSQL** - 原生支持
2. **Amazon Redshift** - RedshiftQuery 继承自 PostgresQuery
3. **CrateDB** - CrateQuery 继承自 PostgresQuery
4. **其他兼容 PostgreSQL 协议的数据库**

---

## 扩展建议

如果需要支持新的 PostgreSQL 功能：

1. **添加新函数**：在 `sqlTemplates()` 中添加 `templates.functions.XXX`
2. **添加新类型**：在 `templates.types` 中添加类型映射
3. **添加新运算符**：在 `templates.operators` 中添加
4. **覆写方法**：直接覆写父类的方法以改变行为

---

## 性能优化考虑

1. **参数重用**：`shouldReuseParams=true` 减少参数解析开销
2. **HyperLogLog**：对大数据集的去重计数有显著性能提升
3. **预编译语句**：参数化查询支持数据库的语句缓存
4. **索引友好**：生成的 `date_trunc()` 可以使用表达式索引

---

## 相关文件

- **基类**：`src/adapter/BaseQuery.js`
- **参数分配器**：`src/adapter/ParamAllocator.js`
- **子类**：
  - `src/adapter/RedshiftQuery.ts`
  - `src/adapter/CrateQuery.ts`
- **测试**：`test/unit/postgres-query.test.ts`

---

## 总结

`PostgresQuery` 是 Cube 语义层的核心组件之一，负责将高层次的数据模型抽象转换为高效的 PostgreSQL SQL 查询。它通过以下方式实现：

1. **安全性**：参数化查询防止注入
2. **准确性**：时区感知的时间处理
3. **灵活性**：支持自定义时间粒度
4. **性能**：HyperLogLog 近似算法
5. **可扩展性**：清晰的模板系统

这个设计使得 Cube 可以为不同数据库提供统一的查询接口，同时充分利用各数据库的特性。
