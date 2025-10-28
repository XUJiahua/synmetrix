# BaseQuery.js 深度分析

## 文件概述

**路径**: `packages/cubejs-schema-compiler/src/adapter/BaseQuery.js`
**大小**: 5193 行代码
**角色**: Schema Compiler 的核心 SQL 查询生成器，负责将 Cube 查询请求转换为数据库特定的 SQL

---

## 目录

1. [整体架构](#整体架构)
2. [核心数据结构](#核心数据结构)
3. [构造函数与初始化](#构造函数与初始化)
4. [SQL 生成主流程](#sql-生成主流程)
5. [预聚合处理](#预聚合处理)
6. [多阶段查询 (Multi-Stage)](#多阶段查询-multi-stage)
7. [Join 处理](#join-处理)
8. [时间维度处理](#时间维度处理)
9. [过滤器处理](#过滤器处理)
10. [查询优化技术](#查询优化技术)
11. [子查询与 CTE](#子查询与-cte)
12. [度量聚合](#度量聚合)
13. [原生 SQL 规划器](#原生-sql-规划器)
14. [可扩展性](#可扩展性)
15. [设计模式](#设计模式)

---

## 整体架构

### 1. 类的定位

```
┌────────────────────────────────────────────────┐
│ API Gateway                                    │
│ - 接收用户查询请求                             │
└────────────┬───────────────────────────────────┘
             ↓
┌────────────────────────────────────────────────┐
│ BaseQuery (本文件)                             │
│ - 查询请求 → SQL 转换                          │
│ - 预聚合选择                                   │
│ - Join 构建                                    │
│ - 多阶段查询处理                               │
└────────────┬───────────────────────────────────┘
             ↓
┌────────────────────────────────────────────────┐
│ Database-specific Query Classes                │
│ - PostgresQuery, MySQLQuery, BigQueryQuery...  │
└────────────────────────────────────────────────┘
```

### 2. 职责范围

BaseQuery 是一个**抽象基类**，负责：

1. **查询结构解析**: 解析 measures、dimensions、timeDimensions、filters、segments
2. **Join 构建**: 使用 JoinGraph 构建多 Cube 连接
3. **预聚合选择**: 通过 PreAggregations 类选择最优预聚合表
4. **SQL 生成**: 生成完整的 SELECT 语句
5. **参数化**: 防止 SQL 注入的参数化查询
6. **多阶段处理**: 处理需要多次查询的复杂度量
7. **时间序列**: 填充时间序列中的空缺数据点

---

## 核心数据结构

### 1. 类属性

```javascript
export class BaseQuery {
  // 核心组件
  compilers;           // 编译器集合 (cubeEvaluator, joinGraph, etc.)
  cubeEvaluator;       // Cube 定义评估器
  joinGraph;           // Join 关系图
  paramAllocator;      // 参数分配器

  // 查询元素
  measures;            // BaseMeasure[] - 度量数组
  dimensions;          // BaseDimension[] - 维度数组
  timeDimensions;      // BaseTimeDimension[] - 时间维度数组
  segments;            // BaseSegment[] - 段数组
  filters;             // (BaseFilter|BaseGroupFilter)[] - WHERE 过滤器
  measureFilters;      // (BaseFilter|BaseGroupFilter)[] - HAVING 过滤器

  // 多阶段查询
  multiStageDimensions;      // 多阶段维度
  multiStageTimeDimensions;  // 多阶段时间维度

  // Join 相关
  join;                // FinishedJoinTree - 构建好的 Join 树
  customSubQueryJoins; // 自定义子查询 Join

  // 预聚合
  preAggregations;     // PreAggregations 实例

  // 配置
  options;             // 查询选项
  timezone;            // 时区
  ungrouped;           // 是否非分组查询
  order;               // 排序规则
}
```

### 2. 查询选项 (options)

```javascript
{
  measures: ['Orders.count'],
  dimensions: ['Users.country'],
  timeDimensions: [{
    dimension: 'Orders.createdAt',
    granularity: 'day',
    dateRange: ['2024-01-01', '2024-12-31']
  }],
  filters: [{
    member: 'Orders.status',
    operator: 'equals',
    values: ['completed']
  }],
  segments: ['Users.active'],
  order: [{ id: 'Orders.count', desc: true }],
  limit: 100,
  offset: 0,
  timezone: 'UTC',

  // 高级选项
  preAggregationQuery: false,
  ungrouped: false,
  useNativeSqlPlanner: true,
  externalQueryClass: ExternalQuery,
  disableExternalPreAggregations: false,
  multiStageQuery: false,
  subqueryJoins: [],
  joinHints: []
}
```

---

## 构造函数与初始化

### 1. constructor()

```javascript
constructor(compilers, options) {
  this.compilers = compilers;
  this.cubeEvaluator = compilers.cubeEvaluator;
  this.joinGraph = compilers.joinGraph;
  this.options = options || {};

  this.paramAllocator = this.options.paramAllocator ||
    this.newParamAllocator(this.options.expressionParams);

  this.initFromOptions();
}
```

### 2. initFromOptions()

这是初始化的核心方法，做了大量工作：

```javascript
initFromOptions() {
  // 1. 初始化上下文和缓存
  this.contextSymbols = { securityContext: {}, ...this.options.contextSymbols };
  this.compilerCache = this.compilers.compiler.compilerCache;
  this.queryCache = this.compilerCache.getQueryCache(/* ... */);

  // 2. 创建查询元素实例
  this.measures = (this.options.measures || []).map(this.newMeasure.bind(this));
  this.dimensions = (this.options.dimensions || []).map(this.newDimension.bind(this));
  this.timeDimensions = (this.options.timeDimensions || []).map(this.newTimeDimension.bind(this));
  this.segments = (this.options.segments || []).map(this.newSegment.bind(this));

  // 3. 处理过滤器 (分离 dimension filters 和 measure filters)
  const filters = this.extractFiltersAsTree(this.options.filters || []);
  this.filters = filters.filter(f => f.dimensionGroup || f.dimension || ...).map(...);
  this.measureFilters = filters.filter(f => f.measureGroup || f.measure).map(...);

  // 4. 处理时间维度 (如果没有指定，使用默认的)
  this.timeDimensions = (this.options.timeDimensions || []).map(td => {
    if (!td.dimension) {
      // 自动选择第一个时间维度
      td.dimension = this.cubeEvaluator.timeDimensionPathsForCube(join.root)[0];
    }
    return td;
  }).filter(R.identity).map(this.newTimeDimension.bind(this));

  // 5. 构建 Join
  this.prebuildJoin();

  // 6. 设置排序
  this.order = this.options.order ?? this.defaultOrder();

  // 7. 初始化 ungrouped 模式
  this.initUngrouped();
}
```

**关键点**:
- **Filter 分离**: WHERE 条件(dimension filters) vs HAVING 条件(measure filters)
- **Join 预构建**: 提前构建好 Join 树，避免重复计算
- **默认排序**: 如果没有指定 order，自动设置合理的默认值
- **Ungrouped 验证**: 检查 ungrouped 查询的主键要求

---

## SQL 生成主流程

### 1. buildSqlAndParams()

这是生成 SQL 的入口方法：

```javascript
buildSqlAndParams(exportAnnotatedSql) {
  // 1. 使用原生 SQL 规划器 (Rust)
  if (this.useNativeSqlPlanner) {
    if (!this.canUseNativeSqlPlannerPreAggregation && isRelatedToPreAggregation) {
      // 回退到 JavaScript 实现
      return this.newQueryWithoutNative().buildSqlAndParams(exportAnnotatedSql);
    }
    return this.buildSqlAndParamsRust(exportAnnotatedSql);
  }

  // 2. 使用外部查询类 (CubeStore)
  if (this.externalPreAggregationQuery()) {
    return this.externalQuery().buildSqlAndParams(exportAnnotatedSql);
  }

  // 3. 标准流程
  return this.compilers.compiler.withQuery(this, () =>
    this.cacheValue(['buildSqlAndParams', exportAnnotatedSql], () =>
      this.paramAllocator.buildSqlAndParams(
        this.buildParamAnnotatedSql(),  // 生成带参数占位符的 SQL
        exportAnnotatedSql,
        this.shouldReuseParams
      )
    )
  );
}
```

### 2. buildParamAnnotatedSql()

生成带参数注解的 SQL：

```javascript
buildParamAnnotatedSql() {
  // 1. 如果指定了 from，直接返回简单查询
  if (this.from) {
    return this.simpleQuery();
  }

  // 2. 尝试使用预聚合
  if (!this.options.preAggregationQuery && !this.customSubQueryJoins.length) {
    preAggForQuery = this.preAggregations.findPreAggregationForQuery();
    if (this.options.disableExternalPreAggregations && preAggForQuery?.preAggregation.external) {
      preAggForQuery = undefined;
    }
  }

  // 3. 使用预聚合查询
  if (preAggForQuery) {
    const { multipliedMeasures, regularMeasures, cumulativeMeasures } =
      this.fullKeyQueryAggregateMeasures();

    if (cumulativeMeasures.length === 0) {
      sql = this.preAggregations.rollupPreAggregation(preAggForQuery, this.measures, true);
    } else {
      sql = this.regularAndTimeSeriesRollupQuery(
        regularMeasures, multipliedMeasures, cumulativeMeasures, preAggForQuery
      );
    }
  }
  // 4. 标准查询 (无预聚合)
  else {
    sql = this.fullKeyQueryAggregate();
  }

  // 5. 如果是总数查询，包装成 COUNT(*)
  return this.options.totalQuery ? this.countAllQuery(sql) : sql;
}
```

### 3. fullKeyQueryAggregate()

生成完整的聚合查询：

```javascript
fullKeyQueryAggregate() {
  if (this.from) {
    return this.simpleQuery();
  }

  const {
    multipliedMeasures,    // 需要行去重的度量 (来自多对多 Join)
    regularMeasures,       // 普通度量
    cumulativeMeasures,    // 累积度量 (rolling window)
    withQueries,           // CTE (WITH 子句)
    multiStageMembers,     // 多阶段成员
  } = this.fullKeyQueryAggregateMeasures();

  // 简单情况: 没有复杂度量
  if (!multipliedMeasures.length && !cumulativeMeasures.length && !multiStageMembers.length) {
    return this.simpleQuery();
  }

  // 复杂情况: 需要子查询
  const renderedWithQueries = withQueries.map(q => this.renderWithQuery(q));

  // 构建需要 JOIN 的子查询数组
  const toJoin = [
    // 主查询 (regular measures)
    regularMeasures.length ? [
      this.withCubeAliasPrefix('main', () =>
        this.regularMeasuresSubQuery(regularMeasures)
      )
    ] : [],

    // 按 Cube 分组的 multiplied measures (每个 Cube 一个子查询)
    R.pipe(
      R.groupBy(m => m.cube().name),
      R.toPairs,
      R.map(([keyCubeName, measures]) =>
        this.withCubeAliasPrefix(`${this.aliasName(keyCubeName)}_key`, () =>
          this.aggregateSubQuery(keyCubeName, measures)
        )
      )
    )(multipliedMeasures),

    // Cumulative measures (时间序列查询)
    R.map(([multiplied, measure]) =>
      this.withCubeAliasPrefix(`${this.aliasName(measure.measure)}_cumulative`, () =>
        this.overTimeSeriesQuery(/* ... */, measure, false)
      )
    )(cumulativeMeasures),

    // Multi-stage members
    multiStageMembers.map(m => `SELECT * FROM ${m.alias}`)
  ].flat();

  // JOIN 所有子查询
  return this.joinFullKeyQueryAggregate(
    multipliedMeasures, regularMeasures, cumulativeMeasures, toJoin
  );
}
```

**关键概念**:

1. **Regular Measures**: 普通度量,不需要特殊处理
2. **Multiplied Measures**: 来自多对多 Join,需要去重
3. **Cumulative Measures**: 滚动窗口,需要时间序列查询
4. **Multi-Stage Members**: 需要多次查询的复杂度量

---

## 预聚合处理

### 1. 预聚合查找

```javascript
// 在 buildParamAnnotatedSql() 中
preAggForQuery = this.preAggregations.findPreAggregationForQuery();
```

`PreAggregations` 类负责：
- 匹配查询与预聚合定义
- 选择最优预聚合表
- 判断是否可以使用预聚合

### 2. 使用预聚合

```javascript
if (preAggForQuery) {
  sql = this.preAggregations.rollupPreAggregation(
    preAggForQuery,
    this.measures,
    true  // includeOriginalGranularity
  );
}
```

### 3. 预聚合类型

根据 `buildParamAnnotatedSql()` 的逻辑:

```javascript
if (cumulativeMeasures.length === 0) {
  // 简单预聚合查询
  sql = this.preAggregations.rollupPreAggregation(preAggForQuery, this.measures, true);
} else {
  // 复杂预聚合查询 (包含累积度量)
  sql = this.regularAndTimeSeriesRollupQuery(
    regularMeasures, multipliedMeasures, cumulativeMeasures, preAggForQuery
  );
}
```

### 4. Original SQL 预聚合

```javascript
cubeSql(cube) {
  // 检查是否有 originalSql 类型的预聚合
  const foundPreAggregation = this.preAggregations.findPreAggregationToUseForCube(cube);

  if (foundPreAggregation &&
      (!this.options.preAggregationQuery || this.options.useOriginalSqlPreAggregationsInPreAggregation)) {
    return this.preAggregations.originalSqlPreAggregationTable(foundPreAggregation);
  }

  // 否则使用 Cube 的 sql 或 sqlTable
  const fromPath = this.cubeEvaluator.cubeFromPath(cube);
  if (fromPath.sqlTable) {
    return this.evaluateSql(cube, fromPath.sqlTable);
  }

  const evaluatedSql = this.evaluateSql(cube, fromPath.sql);
  // 优化: 如果是 SELECT * FROM table, 直接返回 table
  const selectAsterisk = evaluatedSql.match(/^\s*select\s+\*\s+from\s+([...]+)\s*$/i);
  if (selectAsterisk) {
    return selectAsterisk[1];
  }

  return `(${evaluatedSql})`;
}
```

---

## 多阶段查询 (Multi-Stage)

多阶段查询用于处理需要多次聚合的复杂度量。

### 1. 什么是多阶段度量?

```yaml
# 示例: 计算每个用户的平均订单金额
measures:
  - name: avgOrderAmountPerUser
    type: number
    sql: avg({orderAmount})  # 第一阶段: 每个用户的订单总额
    multi_stage: true
    group_by:
      - userId
```

这需要两次聚合:
1. **第一阶段**: 按 userId 分组,计算每个用户的订单总额
2. **第二阶段**: 在第一阶段结果上计算平均值

### 2. 多阶段查询构建

```javascript
fullKeyQueryAggregateMeasures({ hasMultipliedForPreAggregation } = {}) {
  // 收集所有成员的子成员
  const allMemberChildren = this.collectAllMemberChildren({
    dimensions: this.dimensions.map(d => d.expressionPath()),
    // ...
  });

  // 判断哪些成员是多阶段的
  const allMultiStageMembers = this.collectAllMultiStageMembers(allMemberChildren);

  // 构建 WITH 查询 (CTE)
  const withQueries = [];
  const multiStageMembers = this.measures
    .filter(m => allMultiStageMembers[m.expressionPath()])
    .map(m => this.multiStageWithQueries(
      m.expressionPath(),
      queryContext,
      allMemberChildren,
      withQueries
    ));

  return {
    multipliedMeasures,
    regularMeasures,
    cumulativeMeasures,
    multiStageMembers,
    withQueries
  };
}
```

### 3. 多阶段 WITH 查询

```javascript
multiStageWithQueries(member, queryContext, memberChildren, withQueries) {
  // 递归处理子成员
  let memberFrom = memberChildren[member]?.map(child =>
    this.multiStageWithQueries(child, this.childrenMultiStageContext(member, queryContext), memberChildren, withQueries)
  );

  // 构建子查询
  const subQuery = {
    ...selfContext,
    measures: this.cubeEvaluator.isMeasure(member) ? [member] : [],
    dimensions: /* ... */,
    memberFrom,
  };

  // 去重: 如果已有相同查询,复用
  const foundWith = withQueries.find(({ alias, ...q }) => R.equals(subQuery, q));
  if (foundWith) {
    return foundWith;
  }

  // 添加新的 CTE
  subQuery.alias = `cte_${withQueries.length}`;
  withQueries.push(subQuery);

  return subQuery;
}
```

### 4. 多阶段上下文

**Children Context** (子查询上下文):
```javascript
childrenMultiStageContext(memberPath, queryContext) {
  const member = this.newMeasure(memberPath);
  const memberDef = member.definition();

  // 添加 addGroupBy 维度
  if (memberDef.addGroupByReferences) {
    queryContext = {
      ...queryContext,
      dimensions: R.uniq(queryContext.dimensions.concat(memberDef.addGroupByReferences)),
    };
  }

  // 处理 timeShift
  if (memberDef.timeShiftReferences?.length) {
    // ...
  }

  // 移除当前成员的过滤器
  queryContext = {
    ...queryContext,
    filters: this.keepFilters(queryContext.filters, filterMember => filterMember !== memberPath),
  };

  return queryContext;
}
```

**Self Context** (自身查询上下文):
```javascript
selfMultiStageContext(memberPath, queryContext, wouldNodeApplyFilters) {
  const memberDef = member.definition();

  // 处理 reduceBy
  if (memberDef.reduceByReferences) {
    queryContext = {
      ...queryContext,
      multiStageDimensions: R.difference(queryContext.multiStageDimensions, memberDef.reduceByReferences),
    };
  }

  // 处理 groupBy
  if (memberDef.groupByReferences) {
    queryContext = {
      ...queryContext,
      multiStageDimensions: R.intersection(queryContext.multiStageDimensions, memberDef.groupByReferences),
    };
  }

  // 过滤器处理
  if (!wouldNodeApplyFilters) {
    queryContext = {
      ...queryContext,
      timeDimensions: queryContext.timeDimensions.map(td => ({ ...td, dateRange: undefined })),
      segments: [],
      filters: this.keepFilters(queryContext.filters, filterMember => filterMember === memberPath),
    };
  }

  return queryContext;
}
```

### 5. 多阶段 SQL 生成

```javascript
renderSqlMeasure(name, evaluateSql, symbol, cubeName, parentMeasure, orderBySql) {
  // ...

  if (symbol.multiStage) {
    const partitionBy = (this.multiStageDimensions.length || this.multiStageTimeDimensions.length) ?
      `PARTITION BY ${this.multiStageDimensions.concat(this.multiStageTimeDimensions).map(d => d.dimensionSql()).join(', ')} ` : '';

    // Rank 类型
    if (symbol.type === 'rank') {
      return `rank() OVER (${partitionBy}ORDER BY ${orderBySql.map(o => `${o.sql} ${o.dir}`).join(', ')})`;
    }

    // 其他聚合类型 (需要窗口函数)
    if (!(R.equals(this.multiStageDimensions, this.dimensions))) {
      let funDef;
      if (symbol.type === 'countDistinctApprox') {
        funDef = this.countDistinctApprox(evaluateSql);
      } else if (symbol.type === 'countDistinct') {
        funDef = `count(distinct ${evaluateSql})`;
      } else {
        // 双重聚合: sum(sum(x))
        funDef = `${symbol.type}(${symbol.type}(${evaluateSql}))`;
      }
      return `${funDef} OVER(${partitionBy})`;
    }
  }

  // ...
}
```

---

## Join 处理

### 1. Join 构建流程

```javascript
prebuildJoin() {
  try {
    this.join = this.joinGraph.buildJoin(this.allJoinHints);

    // 构建 Join 图路径 (用于后续查询)
    const queryJoinGraph = {};
    for (const { originalFrom, originalTo } of (this.join?.joins || [])) {
      if (!queryJoinGraph[originalFrom]) {
        queryJoinGraph[originalFrom] = [];
      }
      queryJoinGraph[originalFrom].push(originalTo);
    }
    this.joinGraphPaths = queryJoinGraph || {};
  } catch (e) {
    if (this.useNativeSqlPlanner) {
      // Tesseract (原生规划器) 不需要预构建 Join
      // 忽略错误 (多事实表查询可能无法构建单一 Join)
    } else {
      throw e;
    }
  }
}
```

### 2. Join Hints 收集

```javascript
collectJoinHints(excludeTimeDimensions = false) {
  const allMembersJoinHints = this.collectJoinHintsFromMembers(
    this.allMembersConcat(excludeTimeDimensions)
  );
  const explicitJoinHintMembers = new Set(
    allMembersJoinHints.filter(j => Array.isArray(j)).flat()
  );

  // 收集查询级别的 Join Map (来自 View)
  const queryJoinMaps = this.queryJoinMap();

  // 收集自定义子查询的 Join Hints
  const customSubQueryJoinHints = this.collectJoinHintsFromMembers(
    this.joinMembersFromCustomSubQuery()
  );

  const newCollectedHints = [];

  // 迭代收集 Join Hints (因为 Join 条件可能引用其他 Cube)
  let prevJoin = null;
  let newJoin = null;
  let cnt = 0;
  let newJoinHintsCollectedCnt;

  do {
    const allJoinHints = this.enrichHintsWithJoinMap([
      ...this.queryLevelJoinHints,
      ...newCollectedHints,
      ...allMembersJoinHints,
      ...customSubQueryJoinHints,
    ], queryJoinMaps);

    prevJoin = newJoin;
    newJoin = this.joinGraph.buildJoin(allJoinHints);

    const allJoinHintsFlatten = new Set(allJoinHints.flat());
    const joinMembersJoinHints = this.collectJoinHintsFromMembers(
      this.joinMembersFromJoin(newJoin)
    );

    const iterationCollectedHints = joinMembersJoinHints.filter(
      j => !allJoinHintsFlatten.has(j)
    );
    newJoinHintsCollectedCnt = iterationCollectedHints.length;
    cnt++;

    if (newJoin) {
      newCollectedHints.push(...joinMembersJoinHints.filter(
        j => !explicitJoinHintMembers.has(j)
      ));
    }
  } while (
    newJoin?.joins.length > 0 &&
    !this.isJoinTreesEqual(prevJoin, newJoin) &&
    cnt < 10000 &&
    newJoinHintsCollectedCnt > 0
  );

  if (cnt >= 10000) {
    throw new UserError('Can not construct joins for the query, potential loop detected');
  }

  return allJoinHints;
}
```

**为什么需要迭代?**

因为 Join 条件 (`sql`) 可能引用其他 Cube 的成员:

```javascript
joins: {
  Users: {
    sql: `${Orders}.user_id = ${Users}.id AND ${Users}.country = ${Countries}.code`,
    relationship: 'belongsTo'
  }
}
```

在这个例子中，`Users` 的 Join 条件引用了 `Countries`，所以需要先加入 `Countries` 的 Join Hint。

### 3. Join SQL 生成

```javascript
joinQuery(join, subQueryDimensions) {
  const joins = join.joins.map(j => {
    const fromAlias = this.cubeAlias(j.originalFrom);
    const toAlias = this.cubeAlias(j.originalTo);

    return {
      ...j,
      fromAlias,
      toAlias
    };
  });

  // 生成 JOIN 子句
  const joinStrings = joins.map(j =>
    `${j.join} JOIN ${this.cubeSql(j.originalTo)} ${this.asSyntaxJoin} ${j.toAlias} ON ${j.on}`
  );

  return `${this.cubeSql(join.root)} ${this.asSyntaxTable} ${this.cubeAlias(join.root)}${
    joinStrings.length ? ' ' + joinStrings.join(' ') : ''
  }`;
}
```

### 4. 多对多 Join 处理

当 Join 导致行倍增时，需要特殊处理：

```javascript
multipliedJoinRowResult(cubeName) {
  if (!this.join) {
    return false;
  }

  // 检查 cubeName 到 root 的路径上是否有 many_to_many 或 many_to_one 关系
  const path = this.pathFromArray([cubeName]);
  if (!path || !path.joins) {
    return false;
  }

  return path.joins.some(j =>
    j.relationship === 'many_to_many' ||
    (j.relationship === 'many_to_one' && j.originalFrom !== join.root)
  );
}
```

**处理策略**:
- 将这些度量标记为 `multipliedMeasures`
- 为每个 Cube 生成单独的子查询
- 在子查询中先去重,再聚合
- 最后 JOIN 所有子查询

```javascript
aggregateSubQuery(keyCubeName, measures, filters) {
  const primaryKeyDimensions = this.primaryKeyNames(keyCubeName, false)
    .map(pk => this.newDimension(pk));

  // 生成 keys 查询 (DISTINCT 主键)
  const keysQuery = this.keysQuery(primaryKeyDimensions, filters);

  // 生成 measures 查询 (聚合度量)
  const measuresQuery = this.dimensionsJoinCondition(
    primaryKeyDimensions,
    this.collectFrom(measures, this.collectSubQueryDimensionsFor.bind(this))
  );

  // JOIN keys 和 measures
  return this.aggregateSubQueryMeasureJoin(
    keyCubeName, measures, measuresJoin, primaryKeyDimensions, measureSubQueryDimensions
  );
}
```

---

## 时间维度处理

### 1. 时间粒度 (Granularity)

标准时间粒度及其层次关系：

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

### 2. 时间序列生成

```javascript
overTimeSeriesQuery(baseQueryFn, measure, ungroupedForWrappingGroupBy) {
  const timeDimension = this.timeDimensions.find(d =>
    this.granularityFor(d.dimension) === this.minGranularity()
  );

  if (!timeDimension) {
    throw new Error('Time dimension is required for cumulative measures');
  }

  // 生成时间序列
  const timeSeries = this.timeSeries(timeDimension);

  // 生成基础查询 (聚合)
  const baseQuery = baseQueryFn([measure], filters);

  // LEFT JOIN 时间序列和基础查询
  return `
    SELECT
      ${timeDimension.aliasName()} as ${timeDimension.aliasName()},
      ${measure.cumulativeSelectColumns()}
    FROM ${timeSeries.sql} ${this.asSyntaxTable} ${timeSeries.alias}
    LEFT JOIN (${baseQuery}) ${this.asSyntaxTable} ${baseQueryAlias}
      ON ${timeSeries.alias}.${timeDimension.unescapedAliasName()} = ${baseQueryAlias}.${timeDimension.unescapedAliasName()}
    ${this.baseWhere(this.dateFromStartToEndConditionSql(timeDimension))}
  `;
}
```

### 3. 时间序列 SQL

```javascript
timeSeries(timeDimension) {
  // 生成时间序列的 SQL
  // 例如 PostgreSQL: generate_series('2024-01-01', '2024-12-31', '1 day')
  return {
    sql: this.timeSeriesSql(timeDimension),
    alias: 'time_series'
  };
}

timeSeriesSql(timeDimension) {
  const dateRange = timeDimension.dateRange;
  const granularity = timeDimension.granularity;

  // 使用数据库特定的时间序列生成函数
  return this.timeSeriesGenerator(dateRange[0], dateRange[1], granularity);
}

// 在子类中实现 (例如 PostgresQuery)
timeSeriesGenerator(from, to, granularity) {
  const interval = this.granularityToInterval(granularity);
  return `generate_series(${from}, ${to}, ${interval})`;
}
```

### 4. Rolling Window (滚动窗口)

```javascript
rollingWindowMeasure(measure) {
  if (!measure.definition().rollingWindow) {
    return false;
  }

  const { trailing, leading, offset } = measure.definition().rollingWindow;

  // 生成窗口函数
  return {
    trailing,  // 例如 '7 day' (过去7天)
    leading,   // 例如 '0 day' (不包括未来)
    offset     // 例如 '1 day' (偏移)
  };
}
```

---

## 过滤器处理

### 1. 过滤器树提取

```javascript
extractFiltersAsTree(filters = []) {
  return filters.map(f => {
    // 逻辑运算符 (and/or)
    if (f.and || f.or) {
      let operator = f.and ? 'and' : 'or';
      const data = this.extractDimensionsAndMeasures(f[operator]);
      const dimension = data.filter(e => !!e.dimension).map(e => e.dimension);
      const measure = data.filter(e => !!e.measure).map(e => e.measure);

      // 验证: 不能在同一个条件中混合 dimension 和 measure
      if (dimension.length && measure.length) {
        throw new UserError(`You cannot use dimension and measure in same condition: ${JSON.stringify(f)}`);
      }

      // 标记为 dimensionGroup 或 measureGroup
      return {
        values: this.extractFiltersAsTree(f[operator]),
        operator,
        dimensionGroup: dimension.length > 0,
        measureGroup: measure.length > 0,
      };
    }

    // 叶子节点
    if (!f.member && !f.dimension) {
      throw new UserError(`member attribute is required for filter ${JSON.stringify(f)}`);
    }

    // 判断是 dimension 还是 measure
    if (this.cubeEvaluator.isMeasure(f.member || f.dimension)) {
      return { ...f, measure: f.member || f.dimension, dimension: null };
    }

    return { ...f, dimension: f.member || f.dimension, measure: null };
  });
}
```

### 2. 过滤器分离

```javascript
// WHERE 条件 (dimension filters)
this.filters = filters.filter(f =>
  f.dimensionGroup || f.dimension ||
  f.operator === 'measure_filter' || f.operator === 'measureFilter'
).map(this.initFilter.bind(this));

// HAVING 条件 (measure filters)
this.measureFilters = filters.filter(f =>
  (f.measureGroup || f.measure) &&
  f.operator !== 'measure_filter' && f.operator !== 'measureFilter'
).map(this.initFilter.bind(this));
```

### 3. 过滤器 SQL 生成

```javascript
// BaseFilter.js
filterToWhere() {
  const { operator, values } = this.filter;

  switch (operator) {
    case 'equals':
      return `${this.dimensionSql()} = ${this.allocateParam(values[0])}`;
    case 'notEquals':
      return `${this.dimensionSql()} <> ${this.allocateParam(values[0])}`;
    case 'in':
      return `${this.dimensionSql()} IN (${values.map(v => this.allocateParam(v)).join(', ')})`;
    case 'notIn':
      return `${this.dimensionSql()} NOT IN (${values.map(v => this.allocateParam(v)).join(', ')})`;
    case 'gt':
      return `${this.dimensionSql()} > ${this.allocateParam(values[0])}`;
    case 'gte':
      return `${this.dimensionSql()} >= ${this.allocateParam(values[0])}`;
    case 'lt':
      return `${this.dimensionSql()} < ${this.allocateParam(values[0])}`;
    case 'lte':
      return `${this.dimensionSql()} <= ${this.allocateParam(values[0])}`;
    case 'contains':
      return `${this.dimensionSql()} LIKE ${this.allocateParam(`%${values[0]}%`)}`;
    case 'notContains':
      return `${this.dimensionSql()} NOT LIKE ${this.allocateParam(`%${values[0]}%`)}`;
    case 'startsWith':
      return `${this.dimensionSql()} LIKE ${this.allocateParam(`${values[0]}%`)}`;
    case 'endsWith':
      return `${this.dimensionSql()} LIKE ${this.allocateParam(`%${values[0]}`)}`;
    case 'set':
      return `${this.dimensionSql()} IS NOT NULL`;
    case 'notSet':
      return `${this.dimensionSql()} IS NULL`;
    default:
      throw new Error(`Unsupported operator: ${operator}`);
  }
}
```

### 4. 分组过滤器

```javascript
// BaseGroupFilter.js
filterToWhere() {
  const { operator, values } = this.filter;
  const conditions = values.map(v => v.filterToWhere());

  if (operator === 'and') {
    return `(${conditions.join(' AND ')})`;
  } else if (operator === 'or') {
    return `(${conditions.join(' OR ')})`;
  }
}
```

---

## 查询优化技术

### 1. 查询缓存

```javascript
cacheValue(key, fn, { contextPropNames, inputProps, cache } = {}) {
  const currentContext = this.safeEvaluateSymbolContext();

  // 添加上下文到缓存键
  if (contextPropNames) {
    const contextKey = {};
    for (const element of contextPropNames) {
      contextKey[element] = currentContext[element];
    }
    key = key.concat([JSON.stringify(contextKey)]);
  }

  // 从缓存获取或计算
  const { value, resultProps } = (cache || this.compilerCache).cache(key, () => {
    if (inputProps) {
      return {
        value: this.evaluateSymbolSqlWithContext(fn, inputProps),
        resultProps: inputProps
      };
    }
    return { value: fn() };
  });

  // 合并 resultProps 到当前上下文
  if (resultProps) {
    Object.keys(resultProps).forEach(k => {
      if (Array.isArray(currentContext[k])) {
        currentContext[k].push.apply(currentContext[k], resultProps[k]);
      } else if (currentContext[k]) {
        Object.keys(currentContext[k]).forEach(innerKey => {
          currentContext[k][innerKey] = resultProps[k][innerKey];
        });
      }
    });
  }

  return value;
}
```

**缓存层次**:
1. **CompilerCache**: 编译结果缓存 (跨查询)
2. **QueryCache**: 查询级别缓存 (单次查询内)
3. **Symbol Context Cache**: Symbol SQL 评估缓存

### 2. 参数化查询

```javascript
// ParamAllocator.js
allocateParam(value) {
  if (this.params.has(value)) {
    return this.params.get(value);  // 复用参数
  }

  const paramIndex = this.params.size + 1;
  const paramName = this.parameterPlaceholder(paramIndex);
  this.params.set(value, paramName);
  this.values.push(value);

  return paramName;
}

buildSqlAndParams(annotatedSql, exportAnnotatedSql, shouldReuseParams) {
  // 替换参数占位符
  const sql = annotatedSql.replace(/\$\{PARAM_(\d+)\}/g, (match, index) => {
    if (exportAnnotatedSql) {
      return match;  // 导出时保留占位符
    }
    return this.parameterPlaceholder(parseInt(index));
  });

  return [sql, this.values];
}
```

### 3. Inline WHERE 优化

```javascript
rewriteInlineWhere(fn, inlineWhereConditions) {
  return this.evaluateSymbolSqlWithContext(fn, {
    inlineWhereConditions
  });
}

// 在 dimensionSql() 中
dimensionSql() {
  let sql = this.evaluateSql(/* ... */);

  // 将简单的 WHERE 条件内联到 dimension SQL 中
  const inlineWhere = this.safeEvaluateSymbolContext().inlineWhereConditions;
  if (inlineWhere) {
    // CASE WHEN filter THEN dimension ELSE NULL END
    sql = this.caseWhenStatement([{ sql: inlineWhere, label: sql }]);
  }

  return sql;
}
```

这个优化将过滤条件推入到维度表达式中，减少 JOIN 后的数据量。

### 4. 子查询去重

```javascript
multiStageWithQueries(member, queryContext, memberChildren, withQueries) {
  // 构建子查询
  const subQuery = {
    ...selfContext,
    measures: [member],
    dimensions: /* ... */,
    memberFrom,
  };

  // 去重: 如果已有相同查询,复用
  const foundWith = withQueries.find(({ alias, ...q }) => R.equals(subQuery, q));
  if (foundWith) {
    return foundWith;  // 复用现有 CTE
  }

  // 添加新的 CTE
  subQuery.alias = `cte_${withQueries.length}`;
  withQueries.push(subQuery);

  return subQuery;
}
```

### 5. 原生 SQL 规划器

```javascript
buildSqlAndParamsRust(exportAnnotatedSql) {
  const queryParams = {
    measures: this.options.measures,
    dimensions: this.options.dimensions,
    timeDimensions: this.options.timeDimensions,
    filters: this.options.filters,
    segments: this.options.segments,
    order: this.order,
    limit: this.options.limit,
    offset: this.options.offset,
    timezone: this.timezone,
  };

  // 调用 Rust 原生模块生成 SQL
  return nativeBuildSqlAndParams(
    this.compilers.cubeEvaluator,
    queryParams,
    this.constructor.name
  );
}
```

**优势**:
- Rust 性能更高 (10-100x)
- 更好的内存管理
- 支持多事实表查询

---

## 子查询与 CTE

### 1. WITH 子句 (CTE)

```javascript
renderWithQuery(withQuery) {
  const measures = (withQuery.measures || []).map(this.newMeasure.bind(this));
  const dimensions = (withQuery.dimensions || []).map(this.newDimension.bind(this));
  const timeDimensions = (withQuery.timeDimensions || []).map(this.newTimeDimension.bind(this));

  // 构建子查询实例
  const QueryClass = this.constructor;
  const subQuery = new QueryClass(this.compilers, {
    ...this.options,
    measures: withQuery.measures,
    dimensions: withQuery.dimensions,
    timeDimensions: withQuery.timeDimensions,
    filters: withQuery.filters,
    segments: withQuery.segments,
    from: withQuery.memberFrom?.map(m => m.alias),  // FROM 其他 CTE
    multiStageQuery: true,
  });

  // 生成 SQL
  const [sql] = subQuery.buildSqlAndParams();

  return `${withQuery.alias} AS (${sql})`;
}
```

### 2. 完整的 WITH 查询

```javascript
buildWithQueriesAndSql(withQueries, mainSql) {
  if (!withQueries.length) {
    return mainSql;
  }

  const renderedWithQueries = withQueries.map(q => this.renderWithQuery(q));

  return `WITH ${renderedWithQueries.join(',\n')} ${mainSql}`;
}
```

### 3. 子查询 JOIN

```javascript
joinFullKeyQueryAggregate(multipliedMeasures, regularMeasures, cumulativeMeasures, toJoin) {
  const primaryKeyDimensions = this.primaryKeyDimensions();

  // 构建 JOIN 链
  const joins = toJoin.slice(1).map((subQuery, i) => {
    const onConditions = primaryKeyDimensions.map(d =>
      `${toJoin[0].alias}.${d.unescapedAliasName()} = ${subQuery.alias}.${d.unescapedAliasName()}`
    ).join(' AND ');

    return `LEFT JOIN (${subQuery.sql}) ${this.asSyntaxTable} ${subQuery.alias} ON ${onConditions}`;
  });

  // 主 SELECT
  return `
    SELECT ${this.selectAllDimensionsAndMeasures(multipliedMeasures, regularMeasures, cumulativeMeasures)}
    FROM (${toJoin[0].sql}) ${this.asSyntaxTable} ${toJoin[0].alias}
    ${joins.join('\n')}
    ${this.groupByClause()}
    ${this.orderByClause()}
    ${this.limitClause()}
  `;
}
```

---

## 度量聚合

### 1. 度量分类

```javascript
fullKeyQueryAggregateMeasures() {
  const multipliedMeasures = [];    // 需要去重的度量
  const regularMeasures = [];       // 普通度量
  const cumulativeMeasures = [];    // 累积度量

  this.measures.forEach(measure => {
    const multiplied = this.multipliedJoinRowResult(measure.cube().name);
    const cumulative = this.isCumulativeMeasure(measure);

    if (cumulative) {
      cumulativeMeasures.push([multiplied, measure]);
    } else if (multiplied) {
      multipliedMeasures.push(measure);
    } else {
      regularMeasures.push(measure);
    }
  });

  return { multipliedMeasures, regularMeasures, cumulativeMeasures };
}
```

### 2. 度量 SQL 渲染

```javascript
renderSqlMeasure(name, evaluateSql, symbol, cubeName, parentMeasure, orderBySql) {
  const multiplied = this.multipliedJoinRowResult(cubeName) || false;
  const measurePath = `${cubeName}.${name}`;

  // 1. Ungrouped 模式
  if (this.ungrouped) {
    if (symbol.type === 'count' || symbol.type === 'countDistinct') {
      const sql = this.caseWhenStatement([{ sql: `(${evaluateSql}) IS NOT NULL`, label: '1' }]);
      return evaluateSql === '*' ? '1' : sql;
    }
    return evaluateSql;
  }

  // 2. Multi-stage 模式
  if (symbol.multiStage) {
    const partitionBy = this.multiStageDimensions.length ?
      `PARTITION BY ${this.multiStageDimensions.map(d => d.dimensionSql()).join(', ')} ` : '';

    if (symbol.type === 'rank') {
      return `rank() OVER (${partitionBy}ORDER BY ${orderBySql.map(o => `${o.sql} ${o.dir}`).join(', ')})`;
    }

    // 窗口函数
    let funDef;
    if (symbol.type === 'countDistinctApprox') {
      funDef = this.countDistinctApprox(evaluateSql);
    } else if (symbol.type === 'countDistinct') {
      funDef = `count(distinct ${evaluateSql})`;
    } else {
      funDef = `${symbol.type}(${symbol.type}(${evaluateSql}))`;  // sum(sum(x))
    }
    return `${funDef} OVER(${partitionBy})`;
  }

  // 3. 标准聚合
  if (symbol.type === 'countDistinctApprox') {
    return this.safeEvaluateSymbolContext().overTimeSeriesAggregate ?
      this.hllInit(evaluateSql) :
      this.countDistinctApprox(evaluateSql);
  } else if (symbol.type === 'countDistinct' || (symbol.type === 'count' && !symbol.sql && multiplied)) {
    return `count(distinct ${evaluateSql})`;
  } else if (symbol.type === 'runningTotal') {
    return `sum(${evaluateSql})`;
  }

  // 4. Multiplied 情况的特殊处理
  if (multiplied && symbol.type === 'number' && evaluateSql === 'count(*)') {
    return this.primaryKeyCount(cubeName, true);
  }

  // 5. Calculated measure (直接返回表达式)
  if (CubeSymbols.isCalculatedMeasureType(symbol.type)) {
    return evaluateSql;
  }

  // 6. 默认聚合函数
  return `${symbol.type}(${evaluateSql})`;
}
```

### 3. Cumulative 度量处理

```javascript
aggregateOnGroupedColumn(symbol, evaluateSql, topLevelMerge, measurePath) {
  // 应用 cumulative 过滤器
  const cumulativeMeasureFilters = (this.safeEvaluateSymbolContext().cumulativeMeasureFilters || {})[measurePath];
  if (cumulativeMeasureFilters) {
    const sql = cumulativeMeasureFilters.filterToWhere();
    if (sql) {
      evaluateSql = this.caseWhenStatement([{ sql, label: evaluateSql }]);
    }
  }

  // 根据类型选择聚合函数
  if (symbol.type === 'count' || symbol.type === 'sum') {
    return `sum(${evaluateSql})`;
  } else if (symbol.type === 'countDistinctApprox') {
    return topLevelMerge ? this.hllCardinalityMerge(evaluateSql) : this.hllMergeOnly(evaluateSql);
  } else if (symbol.type === 'min' || symbol.type === 'max') {
    return `${symbol.type}(${evaluateSql})`;
  }

  return undefined;
}
```

### 4. HyperLogLog (近似去重)

```javascript
// 基类中抛出错误 (子类实现)
hllInit(sql) {
  throw new UserError('Distributed approximate distinct count is not supported by this DB');
}

hllMerge(sql) {
  throw new UserError('Distributed approximate distinct count is not supported by this DB');
}

hllCardinality(sql) {
  throw new UserError('Distributed approximate distinct count is not supported by this DB');
}

// PostgresQuery 中的实现示例
hllInit(sql) {
  return `hll_add_agg(hll_hash_any(${sql}))`;
}

hllMerge(sql) {
  return `hll_union_agg(${sql})`;
}

hllCardinality(sql) {
  return `hll_cardinality(${sql})`;
}
```

---

## 原生 SQL 规划器

### 1. 使用条件

```javascript
constructor(compilers, options) {
  // ...
  this.useNativeSqlPlanner = this.options.useNativeSqlPlanner ?? getEnv('nativeSqlPlanner');
  this.canUseNativeSqlPlannerPreAggregation = getEnv('nativeSqlPlannerPreAggregations');

  if (this.useNativeSqlPlanner && !this.canUseNativeSqlPlannerPreAggregation) {
    const fullAggregateMeasures = this.fullKeyQueryAggregateMeasures({ hasMultipliedForPreAggregation: true });

    // 如果有多阶段成员,允许使用原生规划器
    this.canUseNativeSqlPlannerPreAggregation = fullAggregateMeasures.multiStageMembers.length > 0;
  }
}
```

### 2. buildSqlAndParamsRust()

```javascript
buildSqlAndParamsRust(exportAnnotatedSql) {
  const order = this.options.order && R.pipe(
    R.map((hash) => ((!hash || !hash.id) ? null : hash)),
    R.reject(R.isNil),
  )(this.options.order);

  const queryParams = {
    measures: this.options.measures,
    dimensions: this.options.dimensions,
    timeDimensions: this.options.timeDimensions,
    filters: this.options.filters,
    segments: this.options.segments,
    order,
    limit: this.options.limit,
    offset: this.options.offset,
    timezone: this.timezone,
    ungrouped: this.ungrouped,
    rowLimit: this.rowLimit,
  };

  try {
    // 调用 Rust 原生模块
    const result = nativeBuildSqlAndParams(
      JSON.stringify({
        cubeEvaluator: this.compilers.cubeEvaluator.toJSON(),
        query: queryParams,
        dialectClass: this.constructor.name,
      })
    );

    return JSON.parse(result);
  } catch (e) {
    // 回退到 JavaScript 实现
    console.error('Native SQL planner failed, falling back to JS:', e);
    return this.newQueryWithoutNative().buildSqlAndParams(exportAnnotatedSql);
  }
}
```

### 3. 回退机制

```javascript
buildSqlAndParams(exportAnnotatedSql) {
  if (this.useNativeSqlPlanner) {
    let isRelatedToPreAggregation = false;

    // 检查是否涉及预聚合
    if (!this.canUseNativeSqlPlannerPreAggregation) {
      if (this.options.preAggregationQuery) {
        isRelatedToPreAggregation = true;
      } else if (this.externalPreAggregationQuery()) {
        isRelatedToPreAggregation = true;
      } else {
        let preAggForQuery = this.preAggregations.findPreAggregationForQuery();
        if (preAggForQuery) {
          isRelatedToPreAggregation = true;
        }
      }

      // 如果涉及预聚合且原生规划器不支持,回退到 JS
      if (isRelatedToPreAggregation) {
        return this.newQueryWithoutNative().buildSqlAndParams(exportAnnotatedSql);
      }
    }

    return this.buildSqlAndParamsRust(exportAnnotatedSql);
  }

  // 标准 JavaScript 实现
  return this.compilers.compiler.withQuery(this, () =>
    this.cacheValue(['buildSqlAndParams', exportAnnotatedSql], () =>
      this.paramAllocator.buildSqlAndParams(
        this.buildParamAnnotatedSql(),
        exportAnnotatedSql,
        this.shouldReuseParams
      )
    )
  );
}
```

---

## 可扩展性

### 1. 方法扩展点

BaseQuery 提供了大量可被子类覆盖的方法：

**时间函数**:
```javascript
timeStampCast(value) { return `CAST(${value} as TIMESTAMP)`; }
dateTimeCast(value) { return `CAST(${value} as DATETIME)`; }
timeGroupedColumn(granularity, dimension) { /* 子类实现 */ }
```

**数据类型转换**:
```javascript
castToString(sql) { return `CAST(${sql} as TEXT)`; }
convertTz(field) { /* 时区转换 */ }
```

**SQL 语法**:
```javascript
get asSyntaxTable() { return 'AS'; }
get asSyntaxJoin() { return 'AS'; }
escapeColumnName(name) { return `"${name}"`; }
```

**聚合函数**:
```javascript
countDistinctApprox(sql) { return `COUNT(DISTINCT ${sql})`; }
hllInit(sql) { throw new UserError('Not supported'); }
hllMerge(sql) { throw new UserError('Not supported'); }
```

### 2. 子类实现示例

**PostgresQuery.js**:
```javascript
export class PostgresQuery extends BaseQuery {
  timeStampCast(value) {
    return `${value}::timestamptz`;
  }

  timeGroupedColumn(granularity, dimension) {
    return `DATE_TRUNC('${granularity}', ${dimension})`;
  }

  hllInit(sql) {
    return `hll_add_agg(hll_hash_any(${sql}))`;
  }

  hllMerge(sql) {
    return `hll_union_agg(${sql})`;
  }

  hllCardinality(sql) {
    return `hll_cardinality(${sql})`;
  }

  // ... 其他 PostgreSQL 特定实现
}
```

**MySQLQuery.js**:
```javascript
export class MysqlQuery extends BaseQuery {
  timeStampCast(value) {
    return `CAST(${value} as DATETIME)`;
  }

  timeGroupedColumn(granularity, dimension) {
    if (granularity === 'week') {
      return `DATE_FORMAT(${dimension}, '%Y-%m-%d')`;
    }
    return `DATE_FORMAT(${dimension}, '${this.granularityFormat(granularity)}')`;
  }

  get asSyntaxTable() {
    return '';  // MySQL 不需要 AS
  }

  // ... 其他 MySQL 特定实现
}
```

### 3. 插件架构

通过 `options` 传递自定义行为：

```javascript
const query = new BaseQuery(compilers, {
  // 自定义参数分配器
  paramAllocator: new CustomParamAllocator(),

  // 自定义外部查询类
  externalQueryClass: CustomExternalQuery,

  // 自定义 Join Hints
  joinHints: [['Orders', 'Users'], 'Products'],

  // 自定义子查询 Joins
  subqueryJoins: [{
    sql: 'SELECT ...',
    on: { expression: () => '...' },
    joinType: 'LEFT',
    alias: 'subquery_alias'
  }],
});
```

---

## 设计模式

### 1. 模板方法模式

BaseQuery 定义了 SQL 生成的算法骨架,子类实现具体步骤：

```javascript
// BaseQuery - 模板方法
buildSqlAndParams() {
  return [
    this.buildSelect(),
    this.buildFrom(),
    this.buildWhere(),
    this.buildGroupBy(),
    this.buildOrderBy(),
    this.buildLimit()
  ].join(' ');
}

// 具体步骤由子类实现
timeGroupedColumn(granularity, dimension) {
  throw new Error('Must be implemented by subclass');
}
```

### 2. 策略模式

不同的聚合策略：

```javascript
// 策略接口
class MeasureStrategy {
  renderSql(measure) { throw new Error('Not implemented'); }
}

// 具体策略
class RegularMeasureStrategy extends MeasureStrategy {
  renderSql(measure) {
    return `${measure.type}(${measure.sql})`;
  }
}

class MultipliedMeasureStrategy extends MeasureStrategy {
  renderSql(measure) {
    return `count(distinct ${measure.sql})`;
  }
}

class CumulativeMeasureStrategy extends MeasureStrategy {
  renderSql(measure) {
    return `sum(${measure.sql}) OVER (ORDER BY time)`;
  }
}
```

### 3. 组合模式

过滤器树结构：

```javascript
// 组件接口
class FilterComponent {
  filterToWhere() { throw new Error('Not implemented'); }
}

// 叶子节点
class BaseFilter extends FilterComponent {
  filterToWhere() {
    return `${this.dimension} ${this.operator} ${this.value}`;
  }
}

// 组合节点
class BaseGroupFilter extends FilterComponent {
  constructor(filter) {
    this.operator = filter.operator;  // 'and' or 'or'
    this.values = filter.values;      // FilterComponent[]
  }

  filterToWhere() {
    const conditions = this.values.map(v => v.filterToWhere());
    return `(${conditions.join(` ${this.operator.toUpperCase()} `)})`;
  }
}
```

### 4. 建造者模式

查询构建：

```javascript
class QueryBuilder {
  constructor(compilers, options) {
    this.query = new BaseQuery(compilers, options);
  }

  withMeasures(measures) {
    this.query.measures = measures.map(m => new BaseMeasure(this.query, m));
    return this;
  }

  withDimensions(dimensions) {
    this.query.dimensions = dimensions.map(d => new BaseDimension(this.query, d));
    return this;
  }

  withFilters(filters) {
    this.query.filters = this.extractFilters(filters);
    return this;
  }

  build() {
    return this.query.buildSqlAndParams();
  }
}

// 使用
const [sql, params] = new QueryBuilder(compilers, options)
  .withMeasures(['Orders.count'])
  .withDimensions(['Users.country'])
  .withFilters([{ member: 'Orders.status', operator: 'equals', values: ['completed'] }])
  .build();
```

### 5. 访问者模式

Symbol 遍历：

```javascript
evaluateSymbolSql(cube, name, symbol) {
  // 访问 symbol 并生成 SQL
  if (symbol.sql) {
    return this.evaluateSql(cube, symbol.sql);
  }

  // 访问子节点
  if (symbol.measures) {
    return this.evaluateMeasures(cube, symbol.measures);
  }

  // ...
}
```

### 6. 工厂方法模式

创建查询元素：

```javascript
// 工厂方法
newMeasure(measurePath) {
  return new BaseMeasure(this, measurePath);
}

newDimension(dimensionPath) {
  // 特殊处理: dimension.granularities.xxx -> TimeDimension
  if (dimensionPath.includes('.granularities.')) {
    return this.newTimeDimension(/* ... */);
  }
  return new BaseDimension(this, dimensionPath);
}

newFilter(filter) {
  return new BaseFilter(this, filter);
}

newGroupFilter(filter) {
  return new BaseGroupFilter(filter);
}

newTimeDimension(timeDimension) {
  return new BaseTimeDimension(this, timeDimension);
}
```

### 7. 享元模式

缓存和复用：

```javascript
cacheValue(key, fn, { cache } = {}) {
  // 复用已计算的结果
  const cached = (cache || this.compilerCache).get(key);
  if (cached) {
    return cached.value;
  }

  // 计算并缓存
  const value = fn();
  (cache || this.compilerCache).set(key, { value });

  return value;
}
```

---

## 总结

### 核心特点

1. **查询转换引擎**: 将高层 Cube 查询转换为数据库特定的 SQL
2. **预聚合优化**: 自动选择和使用预聚合表提升性能
3. **多阶段查询**: 支持需要多次聚合的复杂度量
4. **Join 管理**: 自动构建和优化多 Cube 连接
5. **时间序列**: 填充时间序列空缺,支持滚动窗口
6. **过滤器处理**: 智能分离 WHERE 和 HAVING 条件
7. **参数化查询**: 防止 SQL 注入
8. **缓存优化**: 多层缓存提升性能
9. **可扩展性**: 通过继承支持 20+ 种数据库

### 关键数据流

```
用户查询请求
    ↓
BaseQuery 构造函数
    ↓
initFromOptions() - 初始化查询元素
    ↓
prebuildJoin() - 构建 Join 树
    ↓
buildSqlAndParams() - 入口方法
    ↓
buildParamAnnotatedSql() - 生成带参数注解的 SQL
    ↓
fullKeyQueryAggregate() - 完整聚合查询
    ├─ simpleQuery() - 简单查询
    ├─ regularMeasuresSubQuery() - 普通度量子查询
    ├─ aggregateSubQuery() - 带去重的聚合子查询
    ├─ overTimeSeriesQuery() - 时间序列查询
    └─ multiStageWithQueries() - 多阶段 CTE 查询
    ↓
joinFullKeyQueryAggregate() - JOIN 所有子查询
    ↓
paramAllocator.buildSqlAndParams() - 参数化
    ↓
[SQL, params] - 最终结果
```

### 性能优化要点

1. **预聚合选择**: 减少扫描数据量
2. **子查询去重**: 避免冗余计算
3. **Inline WHERE**: 推送过滤条件
4. **多层缓存**: 避免重复编译
5. **原生规划器**: Rust 加速 (10-100x)
6. **参数复用**: 减少参数数量

### 扩展建议

如果需要支持新的数据库:

1. 继承 `BaseQuery`
2. 实现数据库特定的方法 (timeGroupedColumn, hllInit 等)
3. 覆盖 SQL 语法方法 (asSyntaxTable, escapeColumnName 等)
4. 注册到 DriverFactory

---

**文档版本**: 1.0
**Cube 版本**: 1.3.83
**文件行数**: 5193
**分析日期**: 2025-10-28
