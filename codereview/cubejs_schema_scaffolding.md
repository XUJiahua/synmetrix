# Cube.js Schema Scaffolding 逻辑分析

## 概述

本文档详细分析 Cube.js 从数据库表结构自动生成数据模型（Schema）的逻辑实现。该功能位于 `packages/cubejs-schema-compiler/src/scaffolding/` 目录。

## 核心架构

### 主要组件

```
scaffolding/
├── ScaffoldingSchema.ts          # 核心逻辑：从表结构生成 cube 描述符
├── ScaffoldingTemplate.ts        # 模板引擎：协调生成流程
├── formatters/
│   ├── BaseSchemaFormatter.ts    # 抽象格式化器
│   ├── JavaScriptSchemaFormatter.ts  # JS 格式输出
│   └── YamlSchemaFormatter.ts    # YAML 格式输出
├── descriptors/
│   ├── MemberReference.ts        # 成员引用描述符
│   └── ValueWithComments.ts      # 带注释的值描述符
└── utils.ts                      # 工具函数
```

## 核心类：ScaffoldingSchema

### 输入数据结构

```typescript
type DatabaseSchema = {
  [schemaName: string]: {
    [tableName: string]: ColumnData[]
  }
};

type ColumnData = {
  name: string;              // 列名
  type: string;              // 数据类型（如 'int', 'varchar', 'timestamp'）
  attributes: string[];      // 属性标记（如 'primaryKey'）
  foreign_keys?: ForeignKey[]; // 外键信息
};

type ForeignKey = {
  target_table: string;      // 目标表
  target_column: string;     // 目标列
};
```

### 数据类型映射逻辑

`columnType()` 方法 (ScaffoldingSchema.ts:385) 将数据库类型映射为 Cube.js 类型：

```typescript
enum ColumnType {
  Time = 'time',
  Number = 'number',
  String = 'string',
  Boolean = 'boolean',
}
```

**映射规则：**

| 数据库类型关键字 | Cube.js 类型 | 说明 |
|----------------|------------|------|
| `time`, `date` | `time` | 时间/日期类型 |
| `int`, `dec`, `double`, `numb`, `float`, `real`, `serial`, `money` | `number` | 数字类型 |
| `bool` | `boolean` | 布尔类型 |
| 其他 | `string` | 默认字符串类型 |

## 生成流程

### 完整调用链

```
ScaffoldingTemplate.generateFilesByTableNames()
  ↓
BaseSchemaFormatter.generateFilesByTableNames()
  ↓
ScaffoldingSchema.generateForTables()
  ↓
  对每个表执行 ScaffoldingSchema.tableSchema()
    ├─ dimensions()       → 生成维度成员
    ├─ numberMeasures()   → 生成度量成员
    └─ joins()            → 生成表关联
  ↓
BaseSchemaFormatter.schemaDescriptorForTable()  // 构建完整描述对象
  ↓
JavaScriptSchemaFormatter.renderFile()  // 渲染为代码
```

### 第一步：生成 Dimensions（维度）

**方法：** `dimensionColumns()` (ScaffoldingSchema.ts:286-303)

**会生成为维度的字段：**

1. ✅ **String 类型字段**
2. ✅ **Boolean 类型字段**
3. ✅ **Time 类型字段**
4. ✅ **标记为主键的字段**（`attributes` 包含 `'primaryKey'`，即使是数字类型）
5. ✅ **列名为 `'id'` 的字段**（即使是数字类型）

**不会生成为维度的字段：**

- ❌ **普通数字类型字段** → 会被识别为 measures
- ❌ **以 `_` 开头的字段**（内部字段）

**关键代码：**

```typescript
// ScaffoldingSchema.ts:287-291
const dimensionColumns = tableDefinition.filter(
  column => !column.name.startsWith('_') && ['string', 'boolean'].includes(this.columnType(column)) ||
    column.attributes?.includes('primaryKey') ||
    this.fixCase(column.name) === 'id'
);

// 时间字段会被单独处理并排序
const timeColumns = tableDefinition
  .filter(column => !column.name.startsWith('_') && this.columnType(column) === 'time')
  .sort(column => this.timeColumnIndex(column));  // create < update < 其他
```

**时间字段优先级：**
- `created_at`, `created_date` 等 → 优先级 0
- `updated_at`, `modified_date` 等 → 优先级 1
- 其他时间字段 → 优先级 2

**示例：**

```sql
CREATE TABLE orders (
  id INT PRIMARY KEY,           -- ✅ 维度（主键）
  user_id INT,                  -- ❌ 不会生成维度
  status VARCHAR(50),           -- ✅ 维度（string）
  amount DECIMAL(10,2),         -- ❌ 不会生成维度
  quantity INT,                 -- ❌ 不会生成维度
  is_paid BOOLEAN,              -- ✅ 维度（boolean）
  created_at TIMESTAMP          -- ✅ 维度（time）
);
```

生成的维度：`id`, `status`, `is_paid`, `created_at`

### 第二步：生成 Measures（度量）

**方法：** `numberMeasures()` (ScaffoldingSchema.ts:269-280)

**筛选条件：**

1. 必须是 **Number 类型**
2. 不以 `_` 开头
3. 不是 `id` 字段
4. 符合度量字典（或配置允许所有数字字段）

**度量字典：**

```typescript
// ScaffoldingSchema.ts:79-91
const MEASURE_DICTIONARY = [
  'amount',
  'price',
  'count',
  'balance',
  'total',
  'number',
  'cost',
  'qty',
  'quantity',
  'duration',
  'value',
];
```

**匹配逻辑：**
- 列名以字典中的词结尾（不区分大小写）
- 例如：`total_amount`, `unit_price`, `item_quantity` 都会匹配

**生成的聚合类型：**

每个 measure 会生成 4 种聚合函数：
```typescript
types: ['sum', 'avg', 'min', 'max']
```

**配置选项：**

```typescript
{
  includeNonDictionaryMeasures: boolean  // 是否包含不在字典中的数字字段
}
```

- `false`（默认）：只生成匹配字典的字段
- `true`：生成所有数字字段，但标记 `included: false` 给非字典字段

**示例输出：**

```javascript
measures: {
  count: { type: 'count' },  // 默认添加
  totalAmount: {
    sql: `${CUBE}.total_amount`,
    type: 'sum',  // 默认取第一个聚合类型
    title: 'Total Amount'
  }
}
```

### 第三步：生成 Joins（表关联）

**方法：** `joins()` (ScaffoldingSchema.ts:313-372)

**两种识别策略：**

#### 策略 1：基于外键约束（优先）

```typescript
if (column.foreign_keys?.length) {
  column.foreign_keys.forEach(fk => {
    // 直接使用外键信息
    cubeToJoin: fk.target_table,
    columnToJoin: fk.target_column
  });
}
```

#### 策略 2：基于命名约定（后备）

**匹配规则：**

1. 列名匹配正则：`/_id$|id$/i`
2. 列名不是 `'id'`
3. 去掉 `_id` 或 `id` 后缀，查找对应的表

**查找逻辑：**

```typescript
// 例如：user_id → user
const withoutId = column.name.replace(/_id$|id$/i, '');

// 尝试多种表名变体
const tablesToJoin =
  this.tableNamesToTables[withoutId] ||                      // user
  this.tableNamesToTables[inflection.tableize(withoutId)] || // users
  this.tableNamesToTables[this.fixCase(withoutId)] ||        // USER
  this.tableNamesToTables[inflection.tableize(this.fixCase(withoutId))];
```

**关联类型：**

默认生成 `belongsTo` 关系：

```javascript
{
  relationship: 'belongsTo'  // 或 snake_case: 'many_to_one'
}
```

**示例：**

假设表结构：
```sql
-- orders 表
CREATE TABLE orders (
  id INT PRIMARY KEY,
  user_id INT,  -- 会查找 users 表
  ...
);

-- users 表
CREATE TABLE users (
  id INT PRIMARY KEY,
  ...
);
```

生成的 join：
```javascript
joins: {
  Users: {
    sql: `${CUBE.userId} = ${Users.id}`,  // 优先使用维度引用
    relationship: 'belongsTo'
  }
}
```

**智能引用策略：**

如果关联的列在各自的 cube 中作为维度存在，则使用维度引用（更清晰）：

```javascript
// 列作为维度存在
sql: `${CUBE.userId} = ${Users.id}`

// 列不是维度
sql: `${CUBE}.user_id = ${Users}.id`
```

## 格式化层

### BaseSchemaFormatter

**核心职责：**

1. **命名转换**

```typescript
protected memberName(member: { title: string }) {
  const title = member.title.replace(/[^A-Za-z0-9]+/g, '_').toLowerCase();

  if (this.options.snakeCase) {
    return toSnakeCase(title);  // user_id
  }

  return inflection.camelize(title, true);  // userId
}
```

2. **SQL 引用生成**

```typescript
protected sqlForMember(m) {
  // 如果列名包含特殊字符，使用 CUBE 引用
  return `${
    this.escapeName(m.name) !== m.name || !this.eligibleIdentifier(m.name)
      ? `${this.cubeReference('CUBE')}.`
      : ''
  }${this.escapeName(m.name)}`;
}
```

3. **完整 Schema 描述符构建**

```typescript
protected schemaDescriptorForTable(tableSchema: TableSchema, schemaContext: SchemaContext) {
  return {
    cube: tableSchema.cube,
    sql_table: `schema.table`,  // 或 sql: SELECT * FROM ...
    dataSource: schemaContext.dataSource,  // 可选

    joins: { ... },
    dimensions: { ... },
    measures: {
      count: { type: 'count' },  // 默认添加
      ...
    },

    preAggregations: new ValueWithComments(null, [
      'Pre-aggregation definitions go here.',
      'Learn more in the documentation: ...'
    ])
  };
}
```

### JavaScriptSchemaFormatter

**输出格式：**

```javascript
cube(`OrdersFact`, {
  sql_table: `public.orders`,

  joins: {
    Users: {
      sql: `${CUBE.userId} = ${Users.id}`,
      relationship: `belongsTo`,
    },
  },

  dimensions: {
    id: {
      sql: `${CUBE}.id`,
      type: `number`,
      primaryKey: true,
    },

    status: {
      sql: `${CUBE}.status`,
      type: `string`,
    },

    createdAt: {
      sql: `${CUBE}.created_at`,
      type: `time`,
    },
  },

  measures: {
    count: {
      type: `count`,
    },

    totalAmount: {
      sql: `${CUBE}.total_amount`,
      type: `sum`,
    },
  },

  preAggregations: {
    // Pre-aggregation definitions go here.
    // Learn more in the documentation: https://cube.dev/docs/caching/pre-aggregations/getting-started
  },
});
```

### YamlSchemaFormatter

**输出格式：**

```yaml
cubes:
  - name: orders_fact
    sql_table: public.orders

    joins:
      - name: users
        sql: "{CUBE.user_id} = {users.id}"
        relationship: many_to_one

    dimensions:
      - name: id
        sql: id
        type: number
        primary_key: true

      - name: status
        sql: status
        type: string

    measures:
      - name: count
        type: count

      - name: total_amount
        sql: total_amount
        type: sum
```

## 配置选项

### ScaffoldingSchema 选项

```typescript
type ScaffoldingSchemaOptions = {
  includeNonDictionaryMeasures?: boolean;  // 是否包含非字典的数字字段为 measure
  snakeCase?: boolean;                     // 使用 snake_case 命名
};
```

### ScaffoldingTemplate 选项

```typescript
type ScaffoldingTemplateOptions = {
  format?: SchemaFormat;  // 'js' | 'yaml'
  snakeCase?: boolean;    // 命名风格
  catalog?: string | null; // 数据库 catalog 前缀
};
```

## 表名解析

**支持的格式：**

```typescript
// 1. 单表名（自动查找 schema）
'orders' → 查找 public.orders 或其他 schema

// 2. 完整表名
'public.orders' → 直接使用

// 3. 数组格式
['public', 'orders'] → 解析为 public.orders

// 4. 带引号的标识符
'"My Schema"."My Table"' → 保留引号
```

**解析逻辑：**

```typescript
// ScaffoldingSchema.ts:132-162
public resolveTableName(tableName: TableName) {
  // 如果只有表名，自动查找 schema
  if (tableParts.length === 1) {
    const schema = Object.keys(this.dbSchema).find(
      (tableSchema) =>
        this.dbSchema[tableSchema][tableName] ||
        this.dbSchema[tableSchema][inflection.tableize(tableName)]
    );
  }

  // 支持 tableize 变体（users vs user）
  if (this.dbSchema[schema][inflection.tableize(tableName)]) {
    return `${schema}.${inflection.tableize(tableName)}`;
  }
}
```

## 智能表名匹配

为了支持灵活的 join 推断，系统会为每个表生成多个名称变体：

```typescript
// ScaffoldingSchema.ts:204-209
const tableizeName = inflection.tableize(this.fixCase(table));
const parts = tableizeName.split('_');

// 例如：user_order_items → ['user_order_items', 'order_items', 'items']
const tableNamesFromParts = R.range(0, parts.length - 1)
  .map(toDrop => inflection.tableize(R.drop(toDrop, parts).join('_')));

const names = R.uniq([table, tableizeName].concat(tableNamesFromParts));
```

**示例：**

表 `user_order_items` 会注册为：
- `user_order_items`
- `order_items`
- `items`

这样 `order_item_id` 可以匹配到该表。

## 设计原则

### 1. 约定优于配置

- 使用命名约定自动推断关系（`*_id` → join）
- 使用类型约定自动分类成员（number → measure, string → dimension）
- 使用字典匹配识别有意义的度量

### 2. 智能默认值

- 自动添加 `count` measure
- 主键自动标记 `primaryKey: true`
- 时间字段自动排序（created > updated > 其他）

### 3. 灵活性

- 支持外键元数据或命名约定
- 支持多种命名风格（camelCase / snake_case）
- 支持多种输出格式（JavaScript / YAML）
- 可配置是否包含所有数字字段

### 4. OLAP 最佳实践

- **维度**：用于分组和筛选（类别、标识、时间）
- **度量**：用于聚合计算（sum, avg, min, max）
- 数字字段默认作为度量，符合分析场景

## 常见问题

### Q1: 为什么数字字段不自动生成维度？

**A:** 这符合 OLAP 数据建模的最佳实践：

- 数字字段通常用于聚合运算（求和、平均等）
- 如果需要用数字字段做分组，可以：
  1. 手动在生成后添加为维度
  2. 确保该字段标记为主键
  3. 将列名设为 `id`

### Q2: 如何让非字典的数字字段也生成 measure？

**A:** 使用配置选项：

```typescript
new ScaffoldingSchema(dbSchema, {
  includeNonDictionaryMeasures: true
});
```

这会生成所有数字字段为 measure，但非字典字段会标记 `included: false`。

### Q3: 如何自定义外键关系？

**A:** 两种方式：

1. **提供外键元数据**（优先）：
```typescript
{
  name: 'user_id',
  type: 'int',
  foreign_keys: [
    { target_table: 'users', target_column: 'id' }
  ]
}
```

2. **使用命名约定**：
   - 列名格式：`<table_name>_id` 或 `<table_name>id`
   - 系统会自动查找对应的表

### Q4: 生成的 schema 可以直接使用吗？

**A:** 可以作为起点，但建议：

1. 检查自动生成的 joins 是否正确
2. 调整 measure 的聚合类型（默认是第一个类型）
3. 添加业务逻辑相关的计算字段
4. 配置 pre-aggregations 以优化性能
5. 添加必要的安全限制和访问控制

## 相关文件

| 文件路径 | 行号 | 说明 |
|---------|-----|------|
| ScaffoldingSchema.ts:124 | - | ScaffoldingSchema 类定义 |
| ScaffoldingSchema.ts:253 | - | dimensions() 方法 |
| ScaffoldingSchema.ts:269 | - | numberMeasures() 方法 |
| ScaffoldingSchema.ts:313 | - | joins() 方法 |
| ScaffoldingSchema.ts:385 | - | columnType() 类型映射 |
| BaseSchemaFormatter.ts:128 | - | schemaDescriptorForTable() 方法 |
| JavaScriptSchemaFormatter.ts:15 | - | renderFile() JS 渲染 |

## 总结

Cube.js 的 Schema Scaffolding 功能通过以下策略实现智能化的数据模型生成：

1. **类型驱动**：根据数据库列类型自动分类为 dimension 或 measure
2. **约定识别**：通过命名约定和字典匹配推断语义
3. **元数据优先**：优先使用外键等元数据，回退到命名约定
4. **灵活输出**：支持多种格式和命名风格
5. **可定制性**：提供配置选项满足不同需求

该设计既降低了手动编写 schema 的工作量，又保持了足够的灵活性供后续调整。
