# Cube.js ScaffoldingSchema 代码分析

本文档记录了对 Cube.js scaffolding 功能的深入代码分析，涵盖了自动生成数据模型的核心逻辑。

---

## 目录

- [1. 架构概览](#1-架构概览)
- [2. generateFilesByTableNames 处理流程](#2-generatefilebytablenames-处理流程)
- [3. YamlSchemaFormatter 深入分析](#3-yamlschemaformatter-深入分析)
- [4. {CUBE} 关键字详解](#4-cube-关键字详解)
- [5. 下划线前缀字段过滤规则](#5-下划线前缀字段过滤规则)
- [6. 数据库类型到 Cube 类型的映射](#6-数据库类型到-cube-类型的映射)
- [7. Primary Key 要求分析](#7-primary-key-要求分析)

---

## 1. 架构概览

### 1.1 核心类关系

```
ScaffoldingTemplate (策略上下文)
    ├── BaseSchemaFormatter (抽象策略)
    │   ├── YamlSchemaFormatter (具体策略 - YAML)
    │   └── JavaScriptSchemaFormatter (具体策略 - JS)
    └── ScaffoldingSchema (Schema 生成器)
```

### 1.2 关键文件位置

| 文件 | 路径 | 作用 |
|------|------|------|
| ScaffoldingTemplate | `packages/cubejs-schema-compiler/src/scaffolding/ScaffoldingTemplate.ts` | 策略模式入口 |
| BaseSchemaFormatter | `packages/cubejs-schema-compiler/src/scaffolding/formatters/BaseSchemaFormatter.ts` | 格式化器基类 |
| YamlSchemaFormatter | `packages/cubejs-schema-compiler/src/scaffolding/formatters/YamlSchemaFormatter.ts` | YAML 格式化器 |
| ScaffoldingSchema | `packages/cubejs-schema-compiler/src/scaffolding/ScaffoldingSchema.ts` | Schema 生成逻辑 |

---

## 2. generateFilesByTableNames 处理流程

### 2.1 调用链路

```typescript
ScaffoldingTemplate.generateFilesByTableNames()
    ↓ (委托给策略)
BaseSchemaFormatter.generateFilesByTableNames()
    ↓
ScaffoldingSchema.generateForTables()
    ↓
BaseSchemaFormatter.generateFilesByTableSchemas()
    ↓
BaseSchemaFormatter.schemaDescriptorForTable()
    ↓
YamlSchemaFormatter.renderFile()
    ↓
YamlSchemaFormatter.render()
```

**位置**：
- `ScaffoldingTemplate.ts:64-72`
- `BaseSchemaFormatter.ts:51-60`

### 2.2 详细步骤

#### 步骤 1: 解析表名 (BaseSchemaFormatter.ts:51-60)

```typescript
public generateFilesByTableNames(
  tableNames: TableName[],
  schemaContext: SchemaContext = {}
): SchemaFile[] {
  const tableSchemas = this.scaffoldingSchema.generateForTables(
    tableNames.map((n) => this.scaffoldingSchema.resolveTableName(n))
  );

  return this.generateFilesByTableSchemas(tableSchemas, schemaContext);
}
```

**功能**：
- 将输入的表名（如 `'users'` 或 `['public', 'users']`）标准化
- 调用 `ScaffoldingSchema.generateForTables()` 生成表结构

#### 步骤 2: 生成 TableSchema (ScaffoldingSchema.ts:185-188)

```typescript
public generateForTables(tableNames: TableName[]) {
  this.prepareTableNamesToTables(tableNames);
  return tableNames.map(tableName => this.tableSchema(tableName, true));
}
```

**生成的 TableSchema 结构**：
```typescript
{
  cube: 'Users',
  tableName: 'public.users',
  schema: 'public',
  table: 'users',
  measures: [...],
  dimensions: [...],
  joins: [...]
}
```

#### 步骤 3: 构建维度映射 (BaseSchemaFormatter.ts:70-85)

```typescript
const cubeToDimensionNamesMap = new Map(
  tableSchemas.map(tableSchema =>
    [tableSchema.cube, tableSchema.dimensions.map(d => d.name)]
  )
);

tableSchemas = tableSchemas.map((tableSchema) => {
  const updatedJoins = tableSchema.joins.map((join) => ({
    ...join,
    thisTableColumnIncludedAsDimension:
      !!cubeToDimensionNamesMap.get(tableSchema.cube)?.includes(join.thisTableColumn),
    columnToJoinIncludedAsDimension:
      !!cubeToDimensionNamesMap.get(join.cubeToJoin)?.includes(join.columnToJoin)
  }));

  return { ...tableSchema, joins: updatedJoins };
});
```

**目的**：标记 join 的列是否已定义为维度，用于生成更优化的引用方式。

#### 步骤 4: 生成文件 (BaseSchemaFormatter.ts:87-90)

```typescript
return tableSchemas.map((tableSchema) => ({
  fileName: `${tableSchema.cube}.${this.fileExtension()}`,
  content: this.renderFile(this.schemaDescriptorForTable(tableSchema, schemaContext))
}));
```

---

## 3. YamlSchemaFormatter 深入分析

### 3.1 核心方法

#### 方法 1: fileExtension() (YamlSchemaFormatter.ts:16-18)

```typescript
public fileExtension(): string {
  return 'yml';
}
```

#### 方法 2: cubeReference() (YamlSchemaFormatter.ts:20-22)

```typescript
protected cubeReference(cube: string): string {
  return `{${cube}}`;  // 将 'CUBE' 转换为 '{CUBE}'
}
```

#### 方法 3: renderFile() (YamlSchemaFormatter.ts:24-34)

```typescript
protected renderFile(fileDescriptor: Record<string, unknown>): string {
  const { cube, sql, preAggregations: _, ...descriptor } = fileDescriptor;

  return `cubes:\n  - name: ${cube}${this.render(
    {
      ...(sql ? { sql } : null),
      ...descriptor,
    },
    2
  )}\n`;
}
```

**生成的顶层结构**：
```yaml
cubes:
  - name: users
    # ... 其他属性从缩进级别 2 开始
```

#### 方法 4: render() - 递归渲染核心 (YamlSchemaFormatter.ts:36-106)

这是最复杂的方法，处理不同类型的值：

##### (1) MemberReference 处理

```typescript
if (value instanceof MemberReference) {
  return value.member;  // 直接返回成员引用字符串
}
```

**示例**：`{CUBE.user_id}` → `{CUBE.user_id}`

##### (2) ValueWithComments 处理

```typescript
if (value instanceof ValueWithComments) {
  const comments = `\n${value.comments
    .map((comment) => `${indent}# ${comment}`)
    .join('\n')}\n`;

  return value.value ? `${this.render(value.value)}${comments}` : comments;
}
```

**生成效果**：
```yaml
# Pre-aggregation definitions go here.
# Learn more in the documentation: https://cube.dev/docs/...
```

##### (3) 数组处理

```typescript
if (Array.isArray(value)) {
  // 简单数组（基本类型或 MemberReference）
  if (value.every((v) => typeof v !== 'object' || v instanceof MemberReference)) {
    return ` [${value.map(this.render).join(', ')}]\n`;
  }

  // 复杂数组（对象）
  return `\n${value
    .map((v) => `${indent}- ${this.render(v, level + 1, value)}`)
    .join('\n')}`;
}
```

**示例**：
```yaml
# 简单数组
types: [sum, avg, min, max]

# 复杂数组
dimensions:
  - name: id
    type: number
  - name: email
    type: string
```

##### (4) 对象处理

**无父级**（顶层对象）：
```typescript
const newLineKeys = Object.keys(value).includes('data_source')
  ? ['data_source']
  : ['sql_table'];
const content = Object.keys(value).map((key) => {
  if (!isPlainObject(value[key])) {
    const newLine = newLineKeys.includes(key) ? '\n' : '';
    return `${indent}${key}:${this.render(value[key], level + 1, value)}${newLine}`;
  }

  // 特殊处理：将 { dimensionName: {...props} }
  // 转换为 { name: dimensionName, ...props }
  return `${indent}${key}:${this.render(
    Object.entries(value[key] || {}).map(([ok, ov]) => ({
      name: ok,
      ...Object.entries(ov)
        .filter(([, v]) => v != null)
        .reduce((memo, [k, v]) => ({ ...memo, [k]: v }), {})
    })),
    level + 1,
    value
  )}`;
}).join('\n');
```

##### (5) 基本类型处理

```typescript
return `${Array.isArray(parent) ? '' : ' '}${this.escapedValue(value)}`;
```

#### 方法 5: escapedValue() (YamlSchemaFormatter.ts:108-114)

```typescript
private escapedValue(value: string | number | boolean): string | number | boolean {
  if (typeof value !== 'string') {
    return value;
  }

  // 包含 {}或" 的字符串需要引号包裹并转义
  return value.match(/[{}"]/) ? `"${value.replace(/"/g, '\\"')}"` : value;
}
```

**转义规则**：
- `{CUBE}.name` → `"{CUBE}.name"`
- `simple_value` → `simple_value`
- `"quoted"` → `"\"quoted\""`

### 3.2 完整生成示例

**输入**：`['public.users']`

**输出 YAML**：
```yaml
cubes:
  - name: users
    sql_table: public.users

    dimensions:
      - name: id
        sql: "{CUBE}.id"
        type: number
        primary_key: true
      - name: email
        sql: "{CUBE}.email"
        type: string
      - name: created_at
        sql: "{CUBE}.created_at"
        type: time

    measures:
      - name: count
        type: count

    joins:
      - orders:
          sql: "{CUBE.id} = {orders.user_id}"
          relationship: one_to_many

    # pre_aggregations:
    # Pre-aggregation definitions go here.
    # Learn more in the documentation: https://cube.dev/docs/caching/pre-aggregations/getting-started
```

---

## 4. {CUBE} 关键字详解

### 4.1 基本概念

`{CUBE}` 是 Cube.js 数据模型中的**动态引用占位符**，在运行时会被替换为**当前 cube 的实际名称**。

### 4.2 为什么需要 {CUBE}

1. **可重用性**：复制模型时不需要修改所有引用
2. **可维护性**：重命名 cube 时不需要修改内部引用
3. **动态性**：Cube.js 在编译时自动替换

### 4.3 生成逻辑

#### sqlForMember() (BaseSchemaFormatter.ts:93-99)

```typescript
protected sqlForMember(m) {
  return `${
    this.escapeName(m.name) !== m.name || !this.eligibleIdentifier(m.name)
      ? `${this.cubeReference('CUBE')}.`  // 添加 {CUBE}. 前缀
      : ''
  }${this.escapeName(m.name)}`;
}
```

**逻辑**：
- 如果列名需要转义（包含特殊字符），使用 `{CUBE}.column_name`
- 如果列名是简单标识符，直接使用 `column_name`

**示例**：
```typescript
sqlForMember({ name: 'email' })      // → "email"
sqlForMember({ name: 'user-email' }) // → "{CUBE}.`user-email`"
```

#### eligibleIdentifier() (BaseSchemaFormatter.ts:124-126)

```typescript
protected eligibleIdentifier(name: string) {
  return !!name.match(/^[a-z0-9_]+$/);  // 只包含小写字母、数字、下划线
}
```

### 4.4 两种引用方式

| 引用方式 | 示例 | 含义 | 使用场景 |
|---------|------|------|---------|
| `{CUBE.dimension}` | `{CUBE.user_id}` | 引用当前 cube 的维度成员 | 列已定义为维度，引用维度定义 |
| `{CUBE}.column` | `{CUBE}.user_id` | 引用当前 cube 的原始列 | 列未定义为维度，直接引用数据库列 |

#### Join 中的智能引用 (BaseSchemaFormatter.ts:159-166)

```typescript
const thisTableColumnRef = j.thisTableColumnIncludedAsDimension
  ? this.cubeReference(`CUBE.${this.memberName({ title: j.thisTableColumn })}`)
  // → {CUBE.user_id} (如果 user_id 是维度)
  : `${this.cubeReference('CUBE')}.${this.escapeName(j.thisTableColumn)}`;
  // → {CUBE}.user_id (如果 user_id 不是维度)
```

**优势**：
- `{CUBE.dimension}` 会应用维度的所有转换和逻辑
- `{CUBE}.column` 仅引用原始数据库列

### 4.5 实际应用示例

#### 示例 1：基本维度定义

```yaml
cubes:
  - name: users
    sql_table: public.users

    dimensions:
      - name: email
        sql: "{CUBE}.email"  # 运行时变成: users.email
        type: string
```

**编译后的 SQL**：
```sql
SELECT users.email FROM public.users
```

#### 示例 2：Join 关系

```yaml
cubes:
  - name: orders
    sql_table: public.orders

    joins:
      - users:
          sql: "{CUBE}.user_id = {users.id}"
          # 运行时变成: orders.user_id = users.id
          relationship: many_to_one
```

#### 示例 3：复杂表达式

```yaml
measures:
  - name: total_amount
    sql: "SUM({CUBE}.amount)"  # 运行时: SUM(orders.amount)
    type: number
```

### 4.6 在 escapedValue 中的处理

```typescript
// YamlSchemaFormatter.ts:108-114
return value.match(/[{}"]/) ? `"${value.replace(/"/g, '\\"')}"` : value;
```

**为什么要转义 `{}`？**

因为 `{CUBE}` 这类引用包含花括号，在 YAML 中可能被误解析。通过加引号确保正确解析：

```yaml
# 不转义（可能有问题）
sql: {CUBE}.email

# 转义后（安全）
sql: "{CUBE}.email"
```

---

## 5. 下划线前缀字段过滤规则

### 5.1 问题描述

数据库字段如果有下划线前缀（如 `__date`、`_internal_id`），默认**不会自动生成为 Dimension 或 Measure**。

### 5.2 根本原因

在 `ScaffoldingSchema.ts` 中有**硬编码的过滤规则**：

#### dimensionColumns() (ScaffoldingSchema.ts:286-303)

```typescript
protected dimensionColumns(tableDefinition: ColumnData[]): Array<ColumnData & { columnType?: string }> {
  // 普通维度列（string, boolean）
  const dimensionColumns = tableDefinition.filter(
    column => !column.name.startsWith('_') && ['string', 'boolean'].includes(this.columnType(column)) ||
      column.attributes?.includes('primaryKey') ||
      this.fixCase(column.name) === 'id'
  );

  // 时间维度列
  const timeColumns = R.pipe(
    R.filter(column => !column.name.startsWith('_') && this.columnType(column) === 'time'),
    R.sortBy(column => this.timeColumnIndex(column)),
    R.map(column => ({ ...column, columnType: 'time' }))
  )(tableDefinition);

  return dimensionColumns.concat(timeColumns);
}
```

#### numberMeasures() (ScaffoldingSchema.ts:269-280)

```typescript
protected numberMeasures(tableDefinition: ColumnData[]) {
  return tableDefinition.filter(
    column => (!column.name.startsWith('_') &&  // 同样排除下划线开头的字段
      (this.columnType(column) === 'number') &&
      (this.options.includeNonDictionaryMeasures
        ? this.fixCase(column.name) !== 'id'
        : this.fromMeasureDictionary(column)))
  ).map(column => ({
    name: column.name,
    types: ['sum', 'avg', 'min', 'max'],
    title: inflection.titleize(column.name),
    ...(this.options.includeNonDictionaryMeasures
      ? { included: this.fromMeasureDictionary(column) }
      : null)
  }));
}
```

### 5.3 过滤逻辑详解

#### Dimension 的生成条件（必须满足以下任一条件）

```typescript
// 条件 1: 普通字符串/布尔类型（但不能以 _ 开头）
(!column.name.startsWith('_') && ['string', 'boolean'].includes(this.columnType(column)))

// 或者 条件 2: 是主键
|| column.attributes?.includes('primaryKey')

// 或者 条件 3: 列名是 'id'
|| this.fixCase(column.name) === 'id'

// 时间类型（也不能以 _ 开头）
!column.name.startsWith('_') && this.columnType(column) === 'time'
```

#### 例外情况（能通过的下划线字段）

只有以下情况下，下划线开头的字段**才会**被包含：

1. **是主键**：`column.attributes?.includes('primaryKey')`
2. **列名恰好是 'id'**：`this.fixCase(column.name) === 'id'`

### 5.4 实际测试案例

假设数据库表结构：

```sql
CREATE TABLE users (
  id INT PRIMARY KEY,           -- ✅ 生成 dimension (是 'id')
  _internal_id INT PRIMARY KEY, -- ✅ 生成 dimension (是主键)
  __date TIMESTAMP,             -- ❌ 不生成 (以 _ 开头，非主键，非 'id')
  _metadata TEXT,               -- ❌ 不生成 (以 _ 开头，非主键，非 'id')
  email VARCHAR,                -- ✅ 生成 dimension (string 类型)
  created_at TIMESTAMP          -- ✅ 生成 dimension (time 类型)
);
```

**生成结果**：
```yaml
dimensions:
  - name: id
    type: number
    primary_key: true
  - name: _internal_id  # ✅ 因为是主键
    type: number
    primary_key: true
  # __date 不会出现
  # _metadata 不会出现
  - name: email
    type: string
  - name: created_at
    type: time
```

### 5.5 设计意图

#### 为什么要排除下划线开头的字段？

这是一种**约定俗成的命名规范**：

1. **私有/内部字段**：下划线前缀通常表示内部使用的字段
2. **系统字段**：如 `_created_by`, `_updated_at` 等框架自动添加的字段
3. **临时字段**：如 `_tmp_value` 等临时计算字段
4. **元数据字段**：如 `__version`, `__metadata` 等元信息

#### 历史背景

很多编程语言和框架约定：
- Python: `_private`, `__dunder__`
- MongoDB: `_id`, `__v`
- PostgreSQL 内部表: `pg_*`, `_pg_*`

### 5.6 解决方案

如果需要包含 `__date` 这样的字段，有以下几种方法：

#### 方法 1：修改数据库列名（推荐）

```sql
ALTER TABLE your_table RENAME COLUMN __date TO date;
```

#### 方法 2：手动添加到生成的模型中

生成模型后，手动添加：

```yaml
cubes:
  - name: your_cube
    dimensions:
      - name: internal_date
        sql: "{CUBE}.__date"
        type: time
```

#### 方法 3：数据库视图（推荐用于生产环境）

```sql
CREATE VIEW users_view AS
SELECT
  id,
  email,
  __date AS date  -- 重命名下划线字段
FROM users;
```

然后对 `users_view` 生成模型。

#### 方法 4：修改源码（不推荐，除非维护自己的 fork）

修改 `ScaffoldingSchema.ts:288` 和 `295` 行：

```typescript
// 修改前
column => !column.name.startsWith('_') && ['string', 'boolean'].includes(this.columnType(column))

// 修改后（移除下划线检查）
column => ['string', 'boolean'].includes(this.columnType(column))
```

### 5.7 相关代码位置

| 文件位置 | 行号 | 功能 | 过滤规则 |
|---------|------|------|---------|
| ScaffoldingSchema.ts | 271 | numberMeasures | `!column.name.startsWith('_')` |
| ScaffoldingSchema.ts | 288 | dimensionColumns (普通) | `!column.name.startsWith('_')` |
| ScaffoldingSchema.ts | 295 | dimensionColumns (时间) | `!column.name.startsWith('_')` |

---

## 6. 数据库类型到 Cube 类型的映射

### 6.1 核心映射方法

#### columnType() (ScaffoldingSchema.ts:385-398)

```typescript
protected columnType(column): ColumnType {
  const type = this.fixCase(column.type);  // 转换为小写

  if (['time', 'date'].find(t => type.includes(t))) {
    return ColumnType.Time;
  } else if (['int', 'dec', 'double', 'numb'].find(t => type.includes(t))) {
    return ColumnType.Number;
  } else if (['bool'].find(t => type.includes(t))) {
    return ColumnType.Boolean;
  }

  return ColumnType.String;  // 默认返回 String
}
```

**关键特征**：
- 基于**字符串包含匹配**的模糊映射
- **大小写不敏感**（通过 `fixCase()` 转小写）
- **默认兜底为 String**

#### ColumnType 枚举 (ScaffoldingSchema.ts:7-12)

```typescript
enum ColumnType {
  Time = 'time',
  Number = 'number',
  String = 'string',
  Boolean = 'boolean',
}
```

### 6.2 类型映射规则

#### 1. Time 类型（优先级 1）

```typescript
['time', 'date'].find(t => type.includes(t))
```

**匹配规则**：数据库类型名称包含 `time` 或 `date`

| 数据库类型 | 匹配关键词 | Cube 类型 |
|-----------|----------|----------|
| `TIMESTAMP` | `time` ✅ | `time` |
| `DATETIME` | `time` ✅ | `time` |
| `DATE` | `date` ✅ | `time` |
| `TIMESTAMPTZ` | `time` ✅ | `time` |
| `TIME WITHOUT TIME ZONE` | `time` ✅ | `time` |

#### 2. Number 类型（优先级 2）

```typescript
['int', 'dec', 'double', 'numb'].find(t => type.includes(t))
```

**匹配规则**：数据库类型名称包含 `int`、`dec`、`double` 或 `numb`

| 数据库类型 | 匹配关键词 | Cube 类型 | 是否正确 |
|-----------|----------|----------|---------|
| `INTEGER` | `int` ✅ | `number` | ✅ |
| `INT` | `int` ✅ | `number` | ✅ |
| `BIGINT` | `int` ✅ | `number` | ✅ |
| `SMALLINT` | `int` ✅ | `number` | ✅ |
| `DECIMAL` | `dec` ✅ | `number` | ✅ |
| `NUMERIC` | `numb` ✅ | `number` | ✅ |
| `DOUBLE PRECISION` | `double` ✅ | `number` | ✅ |
| `FLOAT` | ❌ | `string` | ⚠️ **错误** |
| `REAL` | ❌ | `string` | ⚠️ **错误** |
| `SERIAL` | ❌ | `string` | ⚠️ **错误** |
| `BIGSERIAL` | ❌ | `string` | ⚠️ **错误** |
| `MONEY` | ❌ | `string` | ⚠️ **错误** |

#### 3. Boolean 类型（优先级 3）

```typescript
['bool'].find(t => type.includes(t))
```

**匹配规则**：数据库类型名称包含 `bool`

| 数据库类型 | 匹配关键词 | Cube 类型 |
|-----------|----------|----------|
| `BOOLEAN` | `bool` ✅ | `boolean` |
| `BOOL` | `bool` ✅ | `boolean` |

#### 4. String 类型（默认，优先级 4）

```typescript
return ColumnType.String;  // 兜底规则
```

**所有未匹配上述规则的类型都会被映射为 String**：

| 数据库类型 | Cube 类型 | 说明 |
|-----------|----------|------|
| `VARCHAR` | `string` | 标准字符串 |
| `CHARACTER VARYING` | `string` | PostgreSQL 字符串 |
| `TEXT` | `string` | 文本类型 |
| `CHAR` | `string` | 定长字符 |
| `JSON` | `string` | JSON 类型 |
| `JSONB` | `string` | PostgreSQL JSONB |
| `UUID` | `string` | UUID 类型 |
| `ENUM` | `string` | 枚举类型 |
| `FLOAT` | `string` | ⚠️ **未匹配到 number** |
| `REAL` | `string` | ⚠️ **未匹配到 number** |
| `MONEY` | `string` | ⚠️ **未匹配到 number** |
| `SERIAL` | `string` | ⚠️ **未匹配到 number** |

### 6.3 为什么数值类型字段会被误判为 String？

#### 原因分析

`columnType()` 方法只检查以下关键词：
- `int`
- `dec`
- `double`
- `numb`

**容易被误判为 String 的数值类型**：

```typescript
// 这些类型名不包含上述关键词，会被映射为 string
'float'        // ❌ 不包含 int/dec/double/numb
'real'         // ❌ 不包含 int/dec/double/numb
'money'        // ❌ 不包含 int/dec/double/numb
'serial'       // ❌ 不包含 int/dec/double/numb (包含 'ial' 不是 'int')
'bigserial'    // ❌ 不包含 int/dec/double/numb
'smallmoney'   // ❌ 不包含 int/dec/double/numb
```

#### 测试用例验证

从 `scaffolding-schema.test.ts` 可以看到：

```javascript
// 测试用例中的类型映射
{
  name: 'id',
  type: 'integer',      // ✅ 映射为 number
  attributes: []
}
→ dimension: { types: ['number'] }

{
  name: 'name',
  type: 'character varying',  // ✅ 映射为 string
  attributes: []
}
→ dimension: { types: ['string'] }
```

### 6.4 完整类型映射表

| 数据库类型 | fixCase 后 | 匹配关键词 | Cube 类型 | 是否正确 |
|-----------|-----------|-----------|----------|---------|
| **数值类型** |
| INTEGER | `integer` | `int` | `number` | ✅ |
| BIGINT | `bigint` | `int` | `number` | ✅ |
| SMALLINT | `smallint` | `int` | `number` | ✅ |
| DECIMAL | `decimal` | `dec` | `number` | ✅ |
| NUMERIC | `numeric` | `numb` | `number` | ✅ |
| DOUBLE PRECISION | `double precision` | `double` | `number` | ✅ |
| FLOAT | `float` | ❌ | `string` | ⚠️ **错误** |
| REAL | `real` | ❌ | `string` | ⚠️ **错误** |
| SERIAL | `serial` | ❌ | `string` | ⚠️ **错误** |
| BIGSERIAL | `bigserial` | ❌ | `string` | ⚠️ **错误** |
| MONEY | `money` | ❌ | `string` | ⚠️ **错误** |
| **字符串类型** |
| VARCHAR | `varchar` | ❌ | `string` | ✅ |
| CHARACTER VARYING | `character varying` | ❌ | `string` | ✅ |
| TEXT | `text` | ❌ | `string` | ✅ |
| CHAR | `char` | ❌ | `string` | ✅ |
| **时间类型** |
| TIMESTAMP | `timestamp` | `time` | `time` | ✅ |
| TIMESTAMPTZ | `timestamptz` | `time` | `time` | ✅ |
| DATE | `date` | `date` | `time` | ✅ |
| TIME | `time` | `time` | `time` | ✅ |
| DATETIME | `datetime` | `time` | `time` | ✅ |
| **布尔类型** |
| BOOLEAN | `boolean` | `bool` | `boolean` | ✅ |
| BOOL | `bool` | `bool` | `boolean` | ✅ |
| **其他类型** |
| JSON | `json` | ❌ | `string` | ✅ |
| JSONB | `jsonb` | ❌ | `string` | ✅ |
| UUID | `uuid` | ❌ | `string` | ✅ |
| ENUM | `enum` | ❌ | `string` | ✅ |

### 6.5 fixCase 方法的影响

```typescript
// ScaffoldingSchema.ts:305-311
private fixCase(value: string) {
  if (this.options.snakeCase) {
    return toSnakeCase(value);
  }
  return value.toLocaleLowerCase();  // 默认转小写
}
```

**重要**：所有类型名称在匹配前都会被转换为**小写**（或 snake_case），所以匹配是**大小写不敏感**的。

### 6.6 Dimension vs Measure 的分配逻辑

#### Dimension 条件

```typescript
// dimensionColumns:286-302

// 1. 字符串或布尔类型（且不以 _ 开头）
!column.name.startsWith('_') && ['string', 'boolean'].includes(this.columnType(column))

// 2. 或者是主键
|| column.attributes?.includes('primaryKey')

// 3. 或者列名是 'id'
|| this.fixCase(column.name) === 'id'

// 4. 时间类型（且不以 _ 开头）
!column.name.startsWith('_') && this.columnType(column) === 'time'
```

#### Measure 条件

```typescript
// numberMeasures:269-280

// 必须同时满足：
!column.name.startsWith('_')  // 1. 不以 _ 开头
&& this.columnType(column) === 'number'  // 2. 是数值类型
&& (
  // 3. 列名不是 'id' 且在 MEASURE_DICTIONARY 中
  this.options.includeNonDictionaryMeasures
    ? this.fixCase(column.name) !== 'id'
    : this.fromMeasureDictionary(column)
)
```

**MEASURE_DICTIONARY** (ScaffoldingSchema.ts:79-91):
```typescript
const MEASURE_DICTIONARY = [
  'amount', 'price', 'count', 'balance', 'total',
  'number', 'cost', 'qty', 'quantity', 'duration', 'value',
];
```

### 6.7 数值类型字段的处理规则

| 数据库字段 | columnType | 列名 | 是否 Dimension | 是否 Measure | 说明 |
|-----------|-----------|------|---------------|-------------|------|
| `id INT` | `number` | `id` | ✅ | ❌ | id 总是 dimension |
| `user_id INT` | `number` | `user_id` | ❌ | ❌ | 外键不是 measure（不在字典中） |
| `amount DECIMAL` | `number` | `amount` | ❌ | ✅ | 在字典中 |
| `price FLOAT` | `string` ⚠️ | `price` | ✅ | ❌ | **被错误映射为 string dimension** |
| `count INT` | `number` | `count` | ❌ | ✅ | 在字典中 |
| `age INT` | `number` | `age` | ❌ | ❌ | 不在字典中（默认不包含） |

### 6.8 Bug 案例分析

#### 案例 1：FLOAT 类型被映射为 String

```sql
CREATE TABLE products (
  id INT,
  price FLOAT,  -- 应该是数值类型
  name VARCHAR
);
```

**实际结果**：
```yaml
dimensions:
  - name: id
    type: number
  - name: price
    type: string  # ⚠️ 错误！应该是 number
  - name: name
    type: string
```

**原因**：`'float'` 不包含 `'int'`, `'dec'`, `'double'`, `'numb'`

#### 案例 2：SERIAL 类型被映射为 String

```sql
CREATE TABLE orders (
  order_id SERIAL PRIMARY KEY,  -- PostgreSQL 自增序列
  amount DECIMAL
);
```

**实际结果**：
```yaml
dimensions:
  - name: order_id
    type: string  # ⚠️ 错误！应该是 number
    primary_key: true
```

**原因**：`'serial'` 不包含完整的 `'int'`（只包含 `'ial'`）

### 6.9 解决方案

#### 方案 1：数据库层面使用兼容类型（推荐）

```sql
-- 不推荐
CREATE TABLE products (
  price FLOAT,
  order_id SERIAL
);

-- 推荐
CREATE TABLE products (
  price DECIMAL(10,2),  -- 包含 'dec'，会被识别为 number
  order_id INTEGER      -- 包含 'int'，会被识别为 number
);
```

#### 方案 2：手动修正生成的模型

生成后手动修改：
```yaml
dimensions:
  - name: price
    type: number  # 手动改为 number
    sql: "{CUBE}.price"
```

#### 方案 3：提交 PR 修复 Bug（贡献开源）

修改 `ScaffoldingSchema.ts:390`：

```typescript
// 修改前
else if (['int', 'dec', 'double', 'numb'].find(t => type.includes(t))) {

// 修改后（添加更多数值类型关键词）
else if (['int', 'dec', 'double', 'numb', 'float', 'real', 'serial', 'money'].find(t => type.includes(t))) {
```

### 6.10 总结

#### 核心问题

1. **模糊匹配机制**：使用 `includes()` 而不是精确匹配
2. **关键词不完整**：只检查 4 个关键词（`int`, `dec`, `double`, `numb`）
3. **默认策略保守**：未匹配的类型全部归为 `string`

#### 容易出错的场景

- ✅ `INTEGER`, `BIGINT`, `DECIMAL`, `NUMERIC`, `DOUBLE PRECISION` → 正确
- ⚠️ `FLOAT`, `REAL`, `SERIAL`, `MONEY` → **错误映射为 string**

#### 最佳实践

1. **优先使用标准类型**：`INTEGER`, `DECIMAL`, `TIMESTAMP`
2. **生成后检查**：验证数值类型字段是否正确映射
3. **必要时手动修正**：修改生成的 YAML/JS 文件

---

## 7. Primary Key 要求分析

### 7.1 简短回答

**Cube 不是必须有 Primary Key，但强烈推荐。**Primary Key 是否必需取决于使用场景。

### 7.2 Primary Key 是强制要求的场景

#### 1. 当 Cube 定义了 Joins 时 ⚠️ **最重要**

**验证逻辑** (JoinGraph.ts:135-143)：

```typescript
const fromMultipliedMeasures = getMultipliedMeasures(cube.name);
if (!this.cubeEvaluator.primaryKeys[cube.name].length && fromMultipliedMeasures.length > 0) {
  errorReporter.error(joinRequired(cube.name));
  return false;
}

// 错误消息 (JoinGraph.ts:125)
`primary key for '${v}' is required when join is defined in order to make aggregates work properly`
```

**为什么？**
- Join 操作可能导致行数乘法问题（row multiplication）
- Cube.js 使用 Primary Key 来处理 **Chasm Trap** 和 **Fan Trap**
- 在 `one_to_many` 关系中，Cube.js 会先 `SELECT DISTINCT` 主键以避免重复计数

**示例**：
```yaml
cubes:
  - name: orders
    sql_table: orders

    # ❌ 错误：定义了 join 但没有 primary_key
    joins:
      - customers:
          sql: "{CUBE.customer_id} = {customers.id}"
          relationship: many_to_one

    dimensions:
      - name: id
        sql: id
        type: number
        # 缺少 primary_key: true ← 会报错！
```

**正确做法**：
```yaml
dimensions:
  - name: id
    sql: id
    type: number
    primary_key: true  # ✅ 必须添加
```

#### 2. Ungrouped 查询时（默认情况）

```typescript
// 测试用例：sql-generation.test.ts:3117
ungrouped: true,
allowUngroupedWithoutPrimaryKey: true,  // 需要明确允许
```

- 默认情况下，ungrouped 查询需要 Primary Key
- 可以通过配置 `allowUngroupedWithoutPrimaryKey: true` 覆盖

#### 3. Pre-aggregations 匹配时

- Pre-aggregations 应该包含所有相关 cube 的 Primary Key

### 7.3 Primary Key 是可选的场景

#### 1. 独立的简单 Cube（无 Join）

```yaml
cubes:
  - name: simple_logs
    sql_table: logs

    # ✅ 没有 join，不需要 primary_key
    dimensions:
      - name: message
        sql: message
        type: string

    measures:
      - name: count
        type: count
```

#### 2. 只用于简单查询的 Cube

如果您的 Cube：
- 不会与其他 Cube join
- 只执行简单的聚合查询
- 不需要 ungrouped 查询

那么 Primary Key 可以省略。

### 7.4 ScaffoldingSchema 如何识别 Primary Key

**识别逻辑** (ScaffoldingSchema.ts:261-264)：

```typescript
if (column.columnType !== 'time') {
  res.isPrimaryKey = column.attributes?.includes('primaryKey') ||
    this.fixCase(column.name) === 'id';
}
```

**自动识别规则**：

| 条件 | 是否识别为 PK |
|------|-------------|
| 数据库定义了 `PRIMARY KEY` 约束 | ✅ 是 |
| 列名是 `'id'`（不区分大小写） | ✅ 是 |
| 列类型是 `time` | ❌ 否（时间维度不能作为 PK）|
| 其他情况 | ❌ 否 |

**示例**：

```sql
-- 情况 1：数据库约束
CREATE TABLE users (
  user_id INT PRIMARY KEY,  -- ✅ 自动识别
  name VARCHAR
);

-- 情况 2：列名为 'id'
CREATE TABLE orders (
  id INT,           -- ✅ 自动识别（列名匹配）
  amount DECIMAL
);

-- 情况 3：既不是 PK 也不叫 'id'
CREATE TABLE events (
  event_key INT,    -- ❌ 不会自动识别
  timestamp TIMESTAMP
);
-- 需要手动添加 primary_key: true
```

### 7.5 测试用例验证

#### 有 Primary Key 的情况

```javascript
// scaffolding-schema.test.ts
{
  name: 'id',
  type: 'integer',
  attributes: []
}
→ dimension: {
  name: 'id',
  types: ['number'],
  isPrimaryKey: true  // ✅ 列名是 'id'
}
```

```javascript
{
  name: 'test',
  type: 'integer',
  attributes: ['primaryKey']  // 数据库约束
}
→ dimension: {
  name: 'test',
  types: ['number'],
  isPrimaryKey: true  // ✅ 有 PK 约束
}
```

#### 没有 Primary Key 的情况

```javascript
{
  name: 'name',
  type: 'character varying',
  attributes: []
}
→ dimension: {
  name: 'name',
  types: ['string'],
  isPrimaryKey: false  // ❌ 不是 PK
}
```

### 7.6 Primary Key 的特殊行为

#### 1. 自动排序 (BaseSchemaFormatter.ts:185)

```typescript
dimensions: tableSchema.dimensions.sort((a) => (a.isPrimaryKey ? -1 : 0))
```

Primary Key 维度会排在最前面。

#### 2. 默认隐藏

根据文档，设置 `primary_key: true` 会将维度的默认 `public` 属性改为 `false`：

```yaml
dimensions:
  - name: id
    sql: id
    type: number
    primary_key: true
    # public: false  ← 默认隐藏，不会在查询结果中显示
```

如果需要显示：
```yaml
dimensions:
  - name: id
    primary_key: true
    public: true  # 明确设置为 true
```

### 7.7 没有自然 Primary Key 怎么办？

如果表没有自然主键，可以创建**复合主键**：

```yaml
dimensions:
  - name: composite_key
    sql: "CONCAT({CUBE}.column_a, '-', {CUBE}.column_b, '-', {CUBE}.column_c)"
    type: string
    primary_key: true
```

或者使用 SQL 表达式：
```yaml
dimensions:
  - name: pk
    sql: "MD5(CONCAT({CUBE}.col1, {CUBE}.col2))"
    type: string
    primary_key: true
```

### 7.8 实际场景总结

| 场景 | 需要 PK | 说明 |
|------|---------|------|
| **独立 Cube（无 join）** | ❌ 可选 | 但建议添加，方便未来扩展 |
| **有 joins 且有聚合 measures** | ✅ **必须** | 否则会报错 |
| **有 joins 但无聚合 measures** | ⚠️ 可能需要 | 取决于具体查询 |
| **Ungrouped 查询** | ✅ 默认必须 | 可配置 `allowUngroupedWithoutPrimaryKey` |
| **Pre-aggregations** | ⚠️ 推荐 | 有助于正确的预聚合匹配 |
| **只读、简单查询** | ❌ 可选 | 不影响功能 |

### 7.9 最佳实践建议

1. **总是定义 Primary Key**，即使当前不需要
   - 未来可能添加 joins
   - 有助于查询优化
   - 符合数据建模最佳实践

2. **确保表有主键列**
   - 列名为 `id`（自动识别）
   - 或者有数据库 PRIMARY KEY 约束
   - 或者手动在 Cube 定义中指定

3. **对于事实表（Fact Table）**
   - 如果没有自然主键，考虑添加代理键（Surrogate Key）
   - 或使用复合键

4. **对于维度表（Dimension Table）**
   - 通常都有自然主键
   - 确保在 Cube 定义中标记为 `primary_key: true`

### 7.10 总结

**Cube 不是必须有 Primary Key，但在以下情况下强制要求**：
- ✅ **有 joins 且有 aggregated measures**（最常见）
- ✅ **Ungrouped 查询**（默认）

**建议**：即使不是强制要求，也应该为每个 Cube 定义 Primary Key，这是良好的数据建模实践，能够避免未来的问题。

---

## 附录

### A. 关键代码位置索引

| 功能 | 文件 | 行号 |
|------|------|------|
| generateFilesByTableNames | ScaffoldingTemplate.ts | 64-72 |
| generateFilesByTableNames | BaseSchemaFormatter.ts | 51-60 |
| generateFilesByTableSchemas | BaseSchemaFormatter.ts | 69-91 |
| schemaDescriptorForTable | BaseSchemaFormatter.ts | 128-225 |
| renderFile (YAML) | YamlSchemaFormatter.ts | 24-34 |
| render (递归渲染) | YamlSchemaFormatter.ts | 36-106 |
| escapedValue | YamlSchemaFormatter.ts | 108-114 |
| cubeReference | YamlSchemaFormatter.ts | 20-22 |
| sqlForMember | BaseSchemaFormatter.ts | 93-99 |
| columnType | ScaffoldingSchema.ts | 385-398 |
| dimensionColumns | ScaffoldingSchema.ts | 286-303 |
| numberMeasures | ScaffoldingSchema.ts | 269-280 |
| dimensions | ScaffoldingSchema.ts | 253-267 |
| isPrimaryKey 识别 | ScaffoldingSchema.ts | 261-264 |
| Primary Key 验证 | JoinGraph.ts | 135-143 |

### B. 测试文件

- `packages/cubejs-schema-compiler/test/unit/scaffolding-schema.test.ts`
- `packages/cubejs-schema-compiler/test/unit/scaffolding-template.test.ts`

### C. 相关文档

- Cube.js 官方文档：https://cube.dev/docs
- Primary Key 参考：`/docs/pages/product/data-modeling/reference/dimensions.mdx`
- Joins 参考：`/docs/pages/product/data-modeling/reference/joins.mdx`

---

**文档生成时间**：2025-10-22
**分析的 Cube.js 版本**：基于 commit `69b2cb302`
**分析者**：Claude Code (Anthropic)
