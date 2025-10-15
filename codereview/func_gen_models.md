# "Generate Models" 功能实现分析

## 概述

Synmetrix 的 "Generate Models" 功能自动从数据库表结构生成 Cube.js 数据模型文件（YAML/JS 格式），并将其存储到数据库中进行版本管理。

## 1. API 入口层

### RPC 端点 (Actions Service)

**文件**: `services/actions/src/rpc/genSchemas.js:4-28`

**端点**: `POST /rpc/gen-schemas`

**输入参数**:
```javascript
{
  datasource_id: uuid,  // 数据源 ID
  branch_id: uuid,      // 分支 ID
  tables: Array,        // 需要生成模型的表列表
  overwrite: boolean,   // 是否覆盖已有模型（默认 false）
  format: string        // 输出格式（默认 "yaml"）
}
```

**实现逻辑**:
```javascript
export default async (session, input, headers) => {
  const {
    datasource_id: dataSourceId,
    branch_id: branchId,
    tables,
    overwrite,
    format = "yaml",
  } = input || {};

  const userId = session?.["x-hasura-user-id"];

  try {
    const result = await cubejsApi({
      dataSourceId,
      userId,
      authToken: headers?.authorization,
    }).generateSchemaFiles({ branchId, tables, overwrite, format });

    return result;
  } catch (err) {
    return apiError(err);
  }
};
```

该 RPC 方法提取用户 ID 和认证令牌，然后转发到 Cube.js 服务的 API。

### Cube.js 路由

**文件**: `services/cubejs/src/routes/index.js:27-31`

**端点**: `POST /api/v1/generate-models`

**中间件**: `checkAuthMiddleware` 进行身份验证和安全上下文构建

```javascript
router.post(
  `${basePath}/v1/generate-models`,
  checkAuthMiddleware,
  async (req, res) => generateDataSchema(req, res, cubejs)
);
```

## 2. 核心生成逻辑

**文件**: `services/cubejs/src/routes/generateDataSchema.js`

### 关键步骤详解

#### 步骤 1: 获取数据库表结构 (Line 40-43)

```javascript
const driver = await cubejs.options.driverFactory({ securityContext });
let schema = await driver.tablesSchema();
```

使用数据库驱动获取完整的表结构信息（包括列名、数据类型等）。

#### 步骤 2: 表名规范化 (Line 44-52)

```javascript
const {
  tables = [],
  overwrite = false,
  branchId,
  format = "yaml",
} = req.body || {};

const { tables: normalizedTables, schema: normalizedSchema } =
  normalizeTables(schema, tables);
```

**`normalizeTables` 函数** (Line 16-33):
- 将 schema 名称 `NO_SCHEMA_KEY` 转为空字符串
- 处理表名中的 `/` 为 `.`
- 返回规范化的表名数组格式：`[[schema, tableName], ...]`

```javascript
const normalizeTables = (schema, tables) => {
  const normalizedSchema = { ...schema };

  if (normalizedSchema?.[NO_SCHEMA_KEY]) {
    normalizedSchema[""] = normalizedSchema[NO_SCHEMA_KEY];
    delete normalizedSchema[NO_SCHEMA_KEY];
  }

  let normalizedTables = tables.map((table) => [
    table?.schema !== NO_SCHEMA_KEY ? table?.schema.replace("/", ".") : "",
    table?.name,
  ]);

  return {
    tables: normalizedTables,
    schema: normalizedSchema,
  };
};
```

#### 步骤 3: 使用 Cube.js 脚手架生成模型 (Line 54-69)

```javascript
const scaffoldingTemplate = new ScaffoldingTemplate(
  normalizedSchema,
  driver,
  {
    format,
  }
);

const newFiles =
  scaffoldingTemplate.generateFilesByTableNames(normalizedTables);

if (!newFiles.length) {
  return res.status(400).json({
    code: "generate_schema_no_new_files",
    message: "No new files created",
  });
}
```

**核心**: 使用 Cube.js 官方的 `ScaffoldingTemplate` 类（来自 `@cubejs-backend/schema-compiler`）自动生成数据模型文件。

#### 步骤 4: 获取已存在的模型文件 (Line 72-81)

```javascript
const dataSchemas = await findDataSchemas({
  dataSourceId,
  branchId,
  authToken,
});

const existedFiles = dataSchemas.map((row) => ({
  fileName: row.name,
  content: row.code,
}));
```

从数据库查询当前分支最新版本的已有模型文件。

#### 步骤 5: 文件合并策略 (Line 83-88)

```javascript
let files;
if (overwrite) {
  files = filterFiles(newFiles, existedFiles);  // 新文件优先
} else {
  files = filterFiles(existedFiles, newFiles);  // 保留已有文件
}
```

**`filterFiles` 函数** (Line 8-14):
```javascript
const filterFiles = (mainFiles, addFiles) => {
  const fileNames = mainFiles.map((f) => f.fileName);
  return [
    ...mainFiles,
    ...addFiles.filter((f) => !fileNames.includes(f.fileName)),
  ];
};
```

去重逻辑：保留 `mainFiles` 中的所有文件，只添加 `addFiles` 中文件名不重复的文件。

#### 步骤 6: 创建版本 checksum (Line 90-91)

```javascript
let commitChecksum = files.reduce((acc, cur) => acc + cur.code, "");
commitChecksum = createMd5Hex(commitChecksum);
```

计算所有文件内容拼接后的 MD5 哈希值作为版本标识。

#### 步骤 7: 准备数据库插入对象 (Line 93-108)

```javascript
const preparedSchemas = files.map((file) => ({
  name: file.fileName,
  code: file.content,
  user_id: userId,
  datasource_id: dataSourceId,
}));

const commitObject = {
  authToken,
  user_id: userId,
  branch_id: branchId,
  checksum: commitChecksum,
  dataschemas: {
    data: [...preparedSchemas],
  },
};
```

构建嵌套插入对象：一个 `version` 记录关联多个 `dataschemas` 记录。

#### 步骤 8: 保存到数据库 (Line 110)

```javascript
await createDataSchema(commitObject);
```

通过 GraphQL mutation 插入数据。

#### 步骤 9: 清除编译缓存 (Line 112-114)

```javascript
if (cubejs.compilerCache) {
  cubejs.compilerCache.prune();
}
```

清除 Cube.js 编译缓存，确保新模型立即生效。

#### 步骤 10: 返回响应 (Line 116-128)

```javascript
res.json({ code: "ok", message: "Generation finished" });
```

成功返回或错误处理。

## 3. 数据持久化层

**文件**: `services/cubejs/src/utils/dataSourceHelpers.js`

### 查询已有模型 (`findDataSchemas`, Line 183-189)

```javascript
const branchSchemasQuery = `
  query ($branchId: uuid!) {
    branches_by_pk(id: $branchId) {
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
`;

export const findDataSchemas = async ({ branchId, authToken }) => {
  const res = await fetchGraphQL(branchSchemasQuery, { branchId }, authToken);

  const dataSchemas =
    res?.data?.branches_by_pk?.versions?.[0]?.dataschemas || [];

  return dataSchemas;
};
```

获取指定分支最新版本（按 `created_at` 降序取第一条）的所有数据模型。

### 创建新版本 (`createDataSchema`, Line 170-180)

```javascript
const upsertVersionMutation = `
  mutation ($object: versions_insert_input!) {
    insert_versions_one(
      object: $object
    ) {
      id
    }
  }
`;

export const createDataSchema = async (object) => {
  const { authToken, ...version } = object;

  let res = await fetchGraphQL(
    upsertVersionMutation,
    { object: version },
    authToken
  );
  res = res?.data?.insert_versions_one;

  return res;
};
```

插入新版本记录，Hasura 会根据嵌套关系自动插入关联的 `dataschemas` 记录。

### GraphQL 客户端 (`services/cubejs/src/utils/graphql.js`)

```javascript
export const fetchGraphQL = async (query, variables, authToken) => {
  const headers = {
    "x-hasura-admin-secret": HASURA_GRAPHQL_ADMIN_SECRET,
  };

  if (authToken) {
    headers.authorization = `Bearer ${authToken}`;
    delete headers["x-hasura-admin-secret"];
  }

  const result = await fetch(HASURA_ENDPOINT, {
    method: "POST",
    body: JSON.stringify({
      query,
      variables,
    }),
    headers,
  });

  const res = await result.json();

  if (res.errors) {
    throw new Error(JSON.stringify(res.errors));
  }

  return res;
};
```

如果提供 `authToken`，使用用户权限；否则使用管理员权限。

## 4. 数据库模式

### 表结构关系

```
datasources (数据源表)
  ↓ (一对多)
branches (分支表)
  ↓ (一对多)
versions (版本表 - 存储 checksum)
  ↓ (一对多)
dataschemas (数据模型文件表 - 存储 name 和 code)
```

### 核心表定义

#### `versions` 表 (`services/hasura/migrations/1679329948023_squashed/up.sql:9`)

```sql
CREATE TABLE "public"."versions" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "branch_id" uuid NOT NULL,
  "checksum" text NOT NULL,
  "user_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  FOREIGN KEY ("branch_id") REFERENCES "public"."branches"("id") ON UPDATE cascade ON DELETE cascade,
  FOREIGN KEY ("user_id") REFERENCES "public"."users"("id") ON UPDATE cascade ON DELETE cascade
);
```

#### `dataschemas` 表 (`services/hasura/migrations/1628432207371_create_table_public_dataschemas/up.sql:1`)

```sql
CREATE TABLE "public"."dataschemas" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "user_id" uuid NOT NULL,
  "datasource_id" uuid NOT NULL,
  "name" text NOT NULL,
  "code" text NOT NULL,
  "version_id" uuid,
  PRIMARY KEY ("id"),
  FOREIGN KEY ("user_id") REFERENCES "public"."users"("id") ON UPDATE cascade ON DELETE cascade,
  FOREIGN KEY ("datasource_id") REFERENCES "public"."datasources"("id") ON UPDATE cascade ON DELETE cascade,
  FOREIGN KEY ("version_id") REFERENCES "public"."versions"("id") ON UPDATE cascade ON DELETE cascade
);
```

### 数据模型

- **branches**: 存储数据源的不同分支（如 main, dev），支持类似 Git 的分支管理
- **versions**: 每次生成模型创建一个新版本记录，包含 checksum 用于版本对比
- **dataschemas**: 存储实际的 Cube.js 模型文件内容（YAML 或 JS 代码）

## 5. API 客户端

**文件**: `services/actions/src/utils/cubejsApi.js`

### 生成模型 API (Line 268-274)

```javascript
generateSchemaFiles: (params) => {
  return fetchCubeJS({
    route: "/generate-models",
    method: "post",
    params,
  });
}
```

### 超时配置 (Line 154-158)

```javascript
let signal = timeoutSignal(10 * 1000);

if (route === "/get-schema" || route === "/generate-dataschema") {
  signal = timeoutSignal(180 * 1000);
}
```

模型生成和 schema 获取操作设置 180 秒超时，其他操作 10 秒超时。

### 请求头配置 (Line 135-143)

```javascript
const reqHeaders = {
  Authorization: `Bearer ${cubejsAuthToken}`,
  "x-hasura-datasource-id": dataSourceId,
};

if (branchId) {
  reqHeaders["x-hasura-branch-id"] = branchId;
}
```

通过自定义请求头传递数据源和分支上下文。

## 6. 完整流程图

```
前端
  ↓
POST /rpc/gen-schemas (Actions Service)
  - 提取 userId, authToken
  - 转发请求
  ↓
POST /api/v1/generate-models (Cube.js Service)
  - checkAuthMiddleware 验证身份
  - 构建 securityContext
  ↓
generateDataSchema 函数
  1. driverFactory 创建数据库驱动
  2. driver.tablesSchema() 获取表结构
  3. normalizeTables 规范化表名
  4. new ScaffoldingTemplate() 初始化生成器
  5. generateFilesByTableNames() 生成模型文件
  6. findDataSchemas() 查询已有模型
  7. filterFiles() 合并策略（overwrite 控制）
  8. createMd5Hex() 计算 checksum
  9. createDataSchema() 保存到数据库
     ↓
     GraphQL Mutation (Hasura)
       - insert_versions_one
       - 嵌套插入 dataschemas
  10. cubejs.compilerCache.prune() 清除缓存
  ↓
返回 { code: "ok", message: "Generation finished" }
```

## 7. 核心依赖

### Cube.js ScaffoldingTemplate

**导入**: `import { ScaffoldingTemplate } from "@cubejs-backend/schema-compiler"`

这是 Cube.js 官方提供的脚手架工具，负责：
- 根据数据库表结构自动推断维度（dimensions）和度量（measures）
- 生成符合 Cube.js 规范的 YAML 或 JavaScript 模型代码
- 处理不同数据库类型的差异

### 方法调用

```javascript
const scaffoldingTemplate = new ScaffoldingTemplate(
  normalizedSchema,  // 数据库表结构
  driver,            // 数据库驱动
  { format }         // 输出格式配置
);

// 生成指定表的模型文件
const newFiles = scaffoldingTemplate.generateFilesByTableNames(normalizedTables);
```

**返回格式**:
```javascript
[
  {
    fileName: "Users.yml",
    content: "cubes:\n  - name: Users\n    sql: SELECT * FROM users\n    ..."
  },
  ...
]
```

## 8. 关键特性

### 版本管理

- 每次生成创建新的 `version` 记录
- 使用 checksum 标识版本内容
- 支持按时间查询历史版本（`order_by: {created_at: desc}`）

### 分支管理

- 类似 Git 的分支概念
- 不同分支可以有独立的模型版本
- 支持 main、dev 等多个分支并行开发

### 文件合并策略

- **overwrite = true**: 新生成的文件覆盖同名已有文件
- **overwrite = false**: 保留已有文件，只添加新表的模型

### 安全性

- JWT 认证流程
- 基于用户权限的数据源访问控制
- GraphQL 层面的细粒度权限管理

## 9. 错误处理

### 无新文件生成 (Line 65-69)

```javascript
if (!newFiles.length) {
  return res.status(400).json({
    code: "generate_schema_no_new_files",
    message: "No new files created",
  });
}
```

### 异常捕获 (Line 117-128)

```javascript
catch (err) {
  console.error(err);

  if (driver.release) {
    await driver.release();
  }

  res.status(500).json({
    code: "generate_schema_error",
    message: err.message || err,
  });
}
```

确保数据库连接正确释放。

## 总结

**核心流程**:
1. 前端调用 RPC `/rpc/gen-schemas`
2. Actions 服务转发到 Cube.js `/api/v1/generate-models`
3. Cube.js 获取数据库 schema 并使用官方 `ScaffoldingTemplate` 生成模型文件
4. 根据 `overwrite` 参数合并新旧文件
5. 计算版本 checksum
6. 通过 GraphQL 将模型文件保存到 `versions` 和 `dataschemas` 表
7. 清除 Cube.js 编译缓存
8. 返回成功响应

**核心技术**:
- Cube.js 的 `ScaffoldingTemplate` 负责实际的模型代码生成
- Hasura GraphQL 提供数据库操作接口
- PostgreSQL 存储模型文件和版本信息
- JWT + 自定义请求头实现多租户隔离
