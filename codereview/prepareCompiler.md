# PrepareCompiler.ts 详细功能解析

## 📋 核心功能概述

`PrepareCompiler.ts` 是 **编译器工厂模块**，负责组装和初始化整个 Cube.js Schema 编译系统。它将多个独立的编译组件组织成一个完整的编译流水线。

**文件位置**: `packages/cubejs-schema-compiler/src/compiler/PrepareCompiler.ts`

---

## 🏗️ 架构设计：工厂模式 + 依赖注入

### 1. **主要导出函数**

```typescript
// 48-119行：准备编译器（不执行编译）
export const prepareCompiler = (
  repo: SchemaFileRepository,    // 文件仓库
  options: PrepareCompilerOptions // 配置选项
) => { /* ... */ }

// 121-126行：准备并立即执行编译
export const compile = (
  repo: SchemaFileRepository,
  options?: PrepareCompilerOptions
) => {
  const compilers = prepareCompiler(repo, options);
  return compilers.compiler.compile().then(() => compilers);
};
```

---

## 🔧 编译器组件初始化流程

### **第一阶段：基础组件创建**（48-61行）

```typescript
// 1. Native 实例 - 提供 Rust 原生能力（Python/Jinja 编译等）
const nativeInstance = options.nativeInstance || new NativeInstance();

// 2. Cube 字典 - 存储所有 cube 名称映射
const cubeDictionary = new CubeDictionary();

// 3. Cube 符号表 - 管理 cube 的符号（用于 JS 编译）
const cubeSymbols = new CubeSymbols();

// 4. View 编译器 - 处理视图（isView=true）
const viewCompiler = new CubeSymbols(true);

// 5. View 编译门控 - 控制视图编译时机
const viewCompilationGate = new ViewCompilationGate();

// 6. Cube 验证器 - 验证 schema 合法性（基于 Joi）
const cubeValidator = new CubeValidator(cubeSymbols);

// 7. Cube 评估器 - 评估和解析 cube 定义
const cubeEvaluator = new CubeEvaluator(cubeValidator);

// 8. 上下文评估器 - 评估编译上下文
const contextEvaluator = new ContextEvaluator(cubeEvaluator);

// 9. 连接图 - 管理 cube 之间的 join 关系
const joinGraph = new JoinGraph(cubeValidator, cubeEvaluator);

// 10. 元数据转换器 - 将 cube 定义转换为元数据
const metaTransformer = new CubeToMetaTransformer(
  cubeValidator, cubeEvaluator, contextEvaluator, joinGraph
);

// 11. 编译缓存 - 查询结果缓存
const compilerCache = new CompilerCache({
  maxQueryCacheSize,
  maxQueryCacheAge
});

// 12. YAML 编译器 - 处理 YAML 格式的 schema
const yamlCompiler = new YamlCompiler(
  cubeSymbols, cubeDictionary, nativeInstance, viewCompiler
);
```

### **第二阶段：缓存系统初始化**（63-65行）

```typescript
// 编译脚本缓存（JavaScript VM 脚本）
const compiledScriptCache = options.compiledScriptCache ||
  new LRUCache<string, vm.Script>({ max: 250 });

// YAML 缓存（编译后的 YAML）
const compiledYamlCache = options.compiledYamlCache ||
  new LRUCache<string, string>({ max: 250 });

// Jinja 模板缓存
const compiledJinjaCache = options.compiledJinjaCache ||
  new LRUCache<string, string>({ max: 250 });
```

**缓存作用**：
- `compiledScriptCache`：缓存已编译的 JavaScript VM 脚本，避免重复编译
- `compiledYamlCache`：缓存 YAML 转换结果
- `compiledJinjaCache`：缓存 Jinja 模板渲染结果

### **第三阶段：Transpiler 管道配置**（67-76行）

Transpiler 按顺序执行，转换 JavaScript/YAML schema：

```typescript
const transpilers: TranspilerInterface[] = [
  // 1. 验证语法错误
  new ValidationTranspiler(),

  // 2. 处理 ES6 import/export
  new ImportExportTranspiler(),

  // 3. 转换 cube 属性上下文（最核心）
  //    将 ${dimension} 转换为实际引用
  new CubePropContextTranspiler(cubeSymbols, cubeDictionary, viewCompiler),

  // 4. 包装为 IIFE（立即执行函数）
  new IIFETranspiler(),
];

// 可选：检查重复属性
if (!options.allowJsDuplicatePropsInSchema) {
  transpilers.push(new CubeCheckDuplicatePropTranspiler());
}
```

**Transpiler 管道说明**：
- **ValidationTranspiler**: 使用 `syntax-error` 库检查 JavaScript 语法
- **ImportExportTranspiler**: 转换 ES6 模块语法为 CommonJS
- **CubePropContextTranspiler**: 最关键的转换器，处理模板字符串引用
- **IIFETranspiler**: 将代码包装为立即执行函数，隔离作用域
- **CubeCheckDuplicatePropTranspiler**: 检测并报告重复的属性定义

### **第四阶段：主编译器组装**（78-107行）

```typescript
const compilerId = uuidv4(); // 唯一编译器ID

const compiler = new DataSchemaCompiler(repo, {
  // 🔹 编译阶段划分
  cubeNameCompilers: [cubeDictionary],              // 阶段0：收集cube名称
  preTranspileCubeCompilers: [cubeSymbols, cubeValidator], // 阶段1：预转译
  transpilers,                                      // 阶段2：转译
  viewCompilers: [viewCompiler],                    // 阶段3：编译视图
  cubeCompilers: [cubeEvaluator, joinGraph, metaTransformer], // 阶段3：编译cube
  contextCompilers: [contextEvaluator],             // 阶段3：编译上下文

  // 🔹 编译基础设施
  viewCompilationGate,
  cubeFactory: cubeSymbols.createCube.bind(cubeSymbols),
  compilerCache,

  // 🔹 缓存系统
  compiledScriptCache,
  compiledYamlCache,
  compiledJinjaCache,

  // 🔹 符号表管理
  cubeDictionary,
  cubeOnlySymbols: cubeSymbols,
  cubeAndViewSymbols: viewCompiler,

  // 🔹 扩展功能
  extensions: {
    Funnels,        // 漏斗分析
    RefreshKeys,    // 刷新键
    Reflection      // 反射能力
  },

  // 🔹 其他配置
  compileContext: options.compileContext,
  standalone: options.standalone,
  nativeInstance,
  yamlCompiler,
  compilerId,
  ...options
});
```

---

## 📊 编译流程图

```
┌────────────────────────────────────────────────────────────────┐
│                      prepareCompiler()                          │
└────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌────────────────────────────────────────────────────────────────┐
│  阶段 0: cubeNameCompilers                                      │
│  ├─ CubeDictionary: 收集所有 cube/view 名称                    │
└────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌────────────────────────────────────────────────────────────────┐
│  阶段 1: preTranspileCubeCompilers                              │
│  ├─ CubeSymbols: 构建符号表                                     │
│  └─ CubeValidator: 初步验证                                     │
└────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌────────────────────────────────────────────────────────────────┐
│  阶段 2: transpilers（按顺序执行）                              │
│  ├─ ValidationTranspiler: 语法检查                              │
│  ├─ ImportExportTranspiler: 处理 import/export                 │
│  ├─ CubePropContextTranspiler: ${ref} -> 实际引用               │
│  ├─ IIFETranspiler: 包装为 (function(){...})()                 │
│  └─ CubeCheckDuplicatePropTranspiler: 检查重复 [可选]           │
└────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌────────────────────────────────────────────────────────────────┐
│  阶段 3: cubeCompilers / viewCompilers / contextCompilers      │
│  ├─ CubeEvaluator: 评估 cube 定义                              │
│  ├─ JoinGraph: 构建连接关系图                                  │
│  ├─ CubeToMetaTransformer: 生成元数据                          │
│  ├─ ViewCompiler: 编译视图                                     │
│  └─ ContextEvaluator: 评估上下文                               │
└────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌────────────────────────────────────────────────────────────────┐
│  返回完整的编译器对象                                           │
│  { compiler, metaTransformer, cubeEvaluator, ... }             │
└────────────────────────────────────────────────────────────────┘
```

---

## 🎯 核心组件详解

### **1. CubeSymbols** - 符号表管理
**文件**: `CubeSymbols.ts`

- **职责**：管理 cube 中的符号（dimensions、measures 等）
- **功能**：
  - 提供 `createCube()` 工厂函数
  - 解析 `${CUBE}`, `${dimension}` 引用
  - 支持 View 和 Cube 两种模式
  - 存储 cube 定义的结构信息

**关键方法**：
```typescript
class CubeSymbols {
  createCube(cubeDefinition: CubeDefinition): CubeDefinitionExtended;
  isCurrentCube(name: string): boolean;
  resolveSymbol(cubeName: string, name: string): any;
}
```

### **2. CubeValidator** - Schema 验证
**文件**: `CubeValidator.ts`

- **职责**：基于 Joi 验证 cube 定义的合法性
- **验证内容**：
  - 字段类型正确性（使用 Joi schema）
  - 必填字段存在性
  - 时间间隔格式（如 `1 day`, `2 hours`）
  - PreAggregation 配置完整性
  - 标识符命名规范（`/^[_a-zA-Z][_a-zA-Z0-9]*$/`）

**验证示例**：
```typescript
const validator = new CubeValidator(cubeSymbols);
validator.validate(cubeDefinition, errorReporter);
// 如果验证失败，会通过 errorReporter 报告错误
```

### **3. CubeEvaluator** - Cube 评估器
**文件**: `CubeEvaluator.ts`

- **职责**：评估和解析 cube 定义
- **功能**：
  - 获取 cube 的 measures/dimensions/segments
  - 计算 PreAggregations
  - 处理 Hierarchies
  - 解析 Access Policies

**常用方法**：
```typescript
class CubeEvaluator {
  cubeNames(): string[];
  measuresForCube(cubeName: string): Record<string, MeasureDefinition>;
  dimensionsForCube(cubeName: string): Record<string, DimensionDefinition>;
  preAggregationsForCube(cubeName: string): Record<string, PreAggregationDefinition>;
  cubeFromPath(cubeName: string): CubeDefinitionExtended;
}
```

### **4. JoinGraph** - 连接图
**文件**: `JoinGraph.ts`

- **职责**：管理 cube 之间的 join 关系
- **功能**：
  - 构建连接关系图（使用 Dijkstra 算法）
  - 查找两个 cube 之间的最短连接路径
  - 验证连接有效性
  - 支持多路径 join

**使用场景**：
```typescript
// 当查询涉及多个 cube 时，JoinGraph 确定如何连接它们
const joinPath = joinGraph.buildJoin(['Orders', 'Customers', 'Products']);
```

### **5. CubeToMetaTransformer** - 元数据转换
**文件**: `CubeToMetaTransformer.js`

- **职责**：将内部 cube 定义转换为 API 元数据
- **输出**：供客户端使用的 JSON 元数据
- **转换内容**：
  - Cube 信息（名称、描述、可见性）
  - Measures/Dimensions/Segments 列表
  - 数据类型映射
  - PreAggregations 元数据

**输出格式**：
```json
{
  "cubes": [
    {
      "name": "Orders",
      "title": "Orders",
      "measures": [...],
      "dimensions": [...],
      "segments": [...]
    }
  ]
}
```

### **6. Transpilers** - 代码转换器

#### **ValidationTranspiler**
- **功能**：使用 syntax-error 检查 JavaScript 语法
- **位置**: `transpilers/ValidationTranspiler.ts`

#### **ImportExportTranspiler**
- **功能**：转换 ESM 模块语法为 CommonJS
- **转换示例**：
  ```javascript
  // 转换前
  import { cube } from '@cubejs-backend/schema-compiler';
  export default cube(...);

  // 转换后
  const { cube } = require('@cubejs-backend/schema-compiler');
  module.exports = cube(...);
  ```

#### **CubePropContextTranspiler** (核心)
- **功能**：处理模板字符串中的引用
- **转换示例**：
  ```javascript
  // 转换前
  sql: `${CUBE}.created_at`
  measures: { total: { sql: `${count} * 2` } }

  // 转换后
  sql: () => `${CUBE}.created_at`
  measures: { total: { sql: () => `${count} * 2` } }
  ```

#### **IIFETranspiler**
- **功能**：包装为立即执行函数
- **转换示例**：
  ```javascript
  // 转换前
  cube('Orders', { ... });

  // 转换后
  (function() {
    cube('Orders', { ... });
  })();
  ```

#### **CubeCheckDuplicatePropTranspiler**
- **功能**：检测并报告重复的属性定义
- **检查内容**：measures、dimensions、segments 中的重复键

### **7. ContextEvaluator** - 上下文评估器
**文件**: `ContextEvaluator.js`

- **职责**：评估编译上下文（如 securityContext）
- **功能**：处理动态上下文值，如用户角色、租户ID等

### **8. CompilerCache** - 编译缓存
**文件**: `CompilerCache.ts`

- **职责**：缓存查询编译结果
- **配置**：
  - `maxQueryCacheSize`: 最大缓存数量
  - `maxQueryCacheAge`: 缓存过期时间

### **9. YamlCompiler** - YAML 编译器
**文件**: `YamlCompiler.ts`

- **职责**：将 YAML 格式的 schema 编译为内部格式
- **支持**：
  - 标准 YAML 语法
  - Jinja 模板语法
  - Python 表达式（通过 Native 实例）

### **10. ViewCompilationGate** - 视图编译门控
**文件**: `ViewCompilationGate.ts`

- **职责**：控制视图（Views）的编译时机
- **功能**：确保 views 在依赖的 cubes 之后编译

---

## ⚙️ 配置选项详解

```typescript
export type PrepareCompilerOptions = {
  // 🔸 Native 支持
  nativeInstance?: NativeInstance,  // Rust 原生实例（用于 Python/Jinja）

  // 🔸 安全选项
  allowNodeRequire?: boolean;       // 允许在 schema 中使用 require() 调用
                                    // 默认: true（生产环境建议 false）

  // 🔸 Schema 选项
  allowJsDuplicatePropsInSchema?: boolean; // 允许重复属性
                                           // 默认: false

  // 🔸 缓存配置
  maxQueryCacheSize?: number;       // 查询缓存最大数量
                                    // 默认: 无限制
  maxQueryCacheAge?: number;        // 缓存过期时间（毫秒）
                                    // 默认: 无限制
  compiledScriptCache?: LRUCache<string, vm.Script>;  // 脚本缓存
  compiledYamlCache?: LRUCache<string, string>;       // YAML 缓存
  compiledJinjaCache?: LRUCache<string, string>;      // Jinja 缓存

  // 🔸 编译上下文
  compileContext?: any;             // 传递给 schema 的全局上下文
                                    // 在 schema 中通过 COMPILE_CONTEXT 访问

  // 🔸 部署模式
  standalone?: boolean;             // 独立模式（无服务器部署）
                                    // 会影响某些功能的可用性

  // 🔸 版本控制
  headCommitId?: string;            // Git commit ID
                                    // 用于版本追踪和缓存失效

  // 🔸 数据库适配器
  adapter?: string;                 // 目标数据库类型
                                    // 'postgres', 'mysql', 'bigquery' 等
                                    // 影响 SQL 方言和函数支持
};
```

**配置建议**：

1. **生产环境**：
   ```typescript
   {
     allowNodeRequire: false,           // 安全考虑
     allowJsDuplicatePropsInSchema: false,
     maxQueryCacheSize: 10000,          // 适中的缓存
     maxQueryCacheAge: 1000 * 60 * 60,  // 1小时
   }
   ```

2. **开发环境**：
   ```typescript
   {
     allowNodeRequire: true,            // 方便调试
     allowJsDuplicatePropsInSchema: true, // 宽松验证
     maxQueryCacheSize: 100,            // 小缓存便于测试
   }
   ```

3. **高性能场景**：
   ```typescript
   {
     compiledScriptCache: new LRUCache({ max: 1000 }),
     compiledYamlCache: new LRUCache({ max: 500 }),
     maxQueryCacheSize: 50000,
     maxQueryCacheAge: 1000 * 60 * 60 * 24, // 24小时
   }
   ```

---

## 🔄 返回值结构

```typescript
return {
  compiler,         // DataSchemaCompiler - 主编译器
                    // 使用: await compiler.compile()

  metaTransformer,  // CubeToMetaTransformer - 元数据转换器
                    // 使用: metaTransformer.cubes() 获取元数据

  cubeEvaluator,    // CubeEvaluator - Cube 评估器
                    // 使用: cubeEvaluator.cubeNames() 获取所有 cube
                    // 最常用的组件之一

  contextEvaluator, // ContextEvaluator - 上下文评估器
                    // 使用: 处理 securityContext 等

  joinGraph,        // JoinGraph - 连接图
                    // 使用: 查询优化和连接路径查找

  compilerCache,    // CompilerCache - 查询缓存
                    // 使用: 缓存管理和失效

  headCommitId,     // string - Git commit ID
                    // 使用: 版本追踪

  compilerId,       // string - 唯一编译器ID (UUID)
                    // 使用: 区分不同的编译器实例
};
```

**典型使用模式**：
```typescript
const { compiler, cubeEvaluator, joinGraph } = prepareCompiler(repo, options);

// 1. 执行编译
await compiler.compile();

// 2. 获取 cube 信息
const cubes = cubeEvaluator.cubeNames();
const measures = cubeEvaluator.measuresForCube('Orders');

// 3. 查询连接路径
const path = joinGraph.buildJoin(['Orders', 'Customers']);
```

---

## 💡 使用示例

### **示例 1：基础编译**

```typescript
import { prepareCompiler } from '@cubejs-backend/schema-compiler';

const repo = {
  localPath: () => './schema',
  dataSchemaFiles: () => Promise.resolve([
    {
      fileName: 'Orders.js',
      content: `
        cube('Orders', {
          sql: 'SELECT * FROM orders',
          measures: {
            count: { type: 'count' }
          },
          dimensions: {
            id: { sql: 'id', type: 'number', primaryKey: true }
          }
        })
      `
    }
  ])
};

const { compiler, cubeEvaluator } = prepareCompiler(repo, {
  adapter: 'postgres',
  allowNodeRequire: true
});

await compiler.compile();

// 获取所有 cube 名称
const cubeNames = cubeEvaluator.cubeNames();
console.log(cubeNames); // ['Orders']

// 获取 cube 的 measures
const measures = cubeEvaluator.measuresForCube('Orders');
console.log(Object.keys(measures)); // ['count']
```

### **示例 2：带缓存的编译**

```typescript
import { LRUCache } from 'lru-cache';

// 创建共享缓存（可以在多个编译器实例间共享）
const scriptCache = new LRUCache({ max: 500 });
const yamlCache = new LRUCache({ max: 300 });

const { compiler } = prepareCompiler(repo, {
  adapter: 'postgres',
  compiledScriptCache: scriptCache,
  compiledYamlCache: yamlCache,
  maxQueryCacheSize: 1000,
  maxQueryCacheAge: 1000 * 60 * 60 // 1小时
});

await compiler.compile();

// 缓存统计
console.log('Script cache size:', scriptCache.size);
console.log('YAML cache size:', yamlCache.size);
```

### **示例 3：传递编译上下文**

```typescript
const { compiler, cubeEvaluator } = prepareCompiler(repo, {
  adapter: 'bigquery',
  compileContext: {
    projectId: 'my-project',
    dataset: 'analytics',
    environment: 'production',
    // 这些值在 schema 中可以通过 COMPILE_CONTEXT 访问
  }
});

// 在 schema 文件中可以这样使用：
// cube('Orders', {
//   sql: `SELECT * FROM \`${COMPILE_CONTEXT.projectId}.${COMPILE_CONTEXT.dataset}.orders\``,
//   ...
// })
```

### **示例 4：多数据源编译**

```typescript
const { compiler, cubeEvaluator } = prepareCompiler(repo, {
  adapter: 'postgres', // 默认适配器
  // 不同的 cube 可以指定不同的 dataSource
});

await compiler.compile();

// Schema 中指定数据源：
// cube('Orders', {
//   sql: 'SELECT * FROM orders',
//   dataSource: 'postgres_primary'
// })
//
// cube('Analytics', {
//   sql: 'SELECT * FROM events',
//   dataSource: 'clickhouse'
// })
```

### **示例 5：获取完整元数据**

```typescript
const { compiler, metaTransformer, cubeEvaluator } = prepareCompiler(repo, options);

await compiler.compile();

// 方法1：使用 cubeEvaluator（细粒度）
const cubes = cubeEvaluator.cubeNames();
for (const cubeName of cubes) {
  const measures = cubeEvaluator.measuresForCube(cubeName);
  const dimensions = cubeEvaluator.dimensionsForCube(cubeName);
  const preAggs = cubeEvaluator.preAggregationsForCube(cubeName);

  console.log(`Cube: ${cubeName}`);
  console.log('Measures:', Object.keys(measures));
  console.log('Dimensions:', Object.keys(dimensions));
  console.log('PreAggregations:', Object.keys(preAggs));
}

// 方法2：使用 metaTransformer（一次性获取所有）
const metadata = metaTransformer.cubes();
console.log(JSON.stringify(metadata, null, 2));
```

### **示例 6：错误处理**

```typescript
try {
  const { compiler } = prepareCompiler(repo, options);
  await compiler.compile();
} catch (error) {
  if (error.message.includes('UserError')) {
    // 用户 schema 错误
    console.error('Schema 定义错误:', error.message);
  } else {
    // 系统错误
    console.error('编译器错误:', error.stack);
  }
}
```

### **示例 7：在 cubejs-server-core 中的实际使用**

```typescript
// 来自 CompilerApi.js
class CompilerApi {
  async getCompilers() {
    if (!this.compilers || this.compilerVersion !== newVersion) {
      this.compilers = compile(this.repository, {
        allowNodeRequire: this.allowNodeRequire,
        compileContext: this.compileContext,
        allowJsDuplicatePropsInSchema: this.allowJsDuplicatePropsInSchema,
        standalone: this.standalone,
        nativeInstance: this.nativeInstance,
        compiledScriptCache: this.compiledScriptCache,
      });
      this.compilerVersion = newVersion;
    }
    return this.compilers;
  }
}
```

---

## 🎨 设计模式

### 1. **工厂模式** (Factory Pattern)
`prepareCompiler` 函数作为工厂，负责创建和组装复杂的编译器系统。

**优点**：
- 隐藏复杂的初始化逻辑
- 确保组件之间的依赖关系正确
- 便于测试（可以 mock 组件）

### 2. **依赖注入** (Dependency Injection)
所有组件通过构造函数接收依赖，而非自己创建。

**示例**：
```typescript
const cubeValidator = new CubeValidator(cubeSymbols);
const cubeEvaluator = new CubeEvaluator(cubeValidator);
```

**优点**：
- 松耦合
- 易于单元测试
- 便于替换实现

### 3. **管道模式** (Pipeline Pattern)
Transpilers 形成处理管道，数据依次通过每个转换器。

```typescript
const transpilers = [
  new ValidationTranspiler(),
  new ImportExportTranspiler(),
  new CubePropContextTranspiler(...),
  new IIFETranspiler(),
];
```

**执行流程**：
```
原始代码 → Validation → Import/Export → PropContext → IIFE → 转换后代码
```

### 4. **单一职责原则** (Single Responsibility)
每个组件只负责一个明确的功能：
- `CubeValidator`: 只做验证
- `CubeEvaluator`: 只做评估
- `JoinGraph`: 只管理连接

### 5. **策略模式** (Strategy Pattern)
不同的 Transpiler 是不同的转换策略，可以灵活组合。

```typescript
// 可以根据需要添加或移除 transpiler
if (!options.allowJsDuplicatePropsInSchema) {
  transpilers.push(new CubeCheckDuplicatePropTranspiler());
}
```

### 6. **门面模式** (Facade Pattern)
`prepareCompiler` 提供了一个简单的接口，隐藏了内部复杂的组件组装逻辑。

**外部使用**：
```typescript
// 简单的调用
const compilers = prepareCompiler(repo, options);
```

**内部实现**：
```typescript
// 实际创建了 12+ 个组件并正确连接它们
```

---

## 📌 关键要点

### 1. **编译是多阶段的**
```
阶段 0: 收集 cube 名称（CubeDictionary）
阶段 1: 预转译（CubeSymbols, CubeValidator）
阶段 2: 转译（Transpilers）
阶段 3: 编译（CubeEvaluator, JoinGraph, MetaTransformer）
```

### 2. **缓存至关重要**
三层缓存提升性能：
- **Script Cache**: 避免重复编译 JavaScript
- **YAML Cache**: 避免重复解析 YAML
- **Jinja Cache**: 避免重复渲染模板

**性能提升**：使用缓存后，重新编译速度可提升 10-100 倍。

### 3. **组件高度解耦**
每个组件可独立测试和替换：
```typescript
// 可以单独测试 CubeValidator
const validator = new CubeValidator(mockCubeSymbols);
validator.validate(testCube, mockErrorReporter);
```

### 4. **支持多种格式**
通过不同的编译器处理不同格式：
- **JavaScript**: 通过 Transpilers 处理
- **YAML**: 通过 YamlCompiler 处理
- **Jinja**: 通过 NativeInstance (Rust) 处理

### 5. **扩展性强**
通过 extensions 机制可以添加新功能：
```typescript
extensions: {
  Funnels,        // 漏斗分析扩展
  RefreshKeys,    // 刷新键扩展
  Reflection,     // 反射扩展
  // 可以添加自定义扩展
}
```

### 6. **错误处理分层**
- **语法错误**: ValidationTranspiler 捕获
- **Schema 错误**: CubeValidator 捕获（Joi validation）
- **运行时错误**: 各个 Evaluator 捕获
- **用户错误**: 通过 UserError 类型化

### 7. **性能优化策略**
- **缓存**: LRU 缓存避免重复计算
- **惰性编译**: 只在需要时编译（通过 viewCompilationGate）
- **并行处理**: 支持多线程转译（通过 workerpool）
- **增量编译**: 根据 compilerVersion 决定是否重新编译

---

## 🔍 深入理解：编译过程示例

### 输入 Schema (JavaScript)

```javascript
// Orders.js
cube('Orders', {
  sql: `SELECT * FROM orders`,

  measures: {
    count: {
      type: 'count'
    },
    totalAmount: {
      sql: `${amount}`,
      type: 'sum'
    }
  },

  dimensions: {
    id: {
      sql: 'id',
      type: 'number',
      primaryKey: true
    },
    amount: {
      sql: 'amount',
      type: 'number'
    },
    status: {
      sql: 'status',
      type: 'string'
    }
  }
});
```

### 编译过程

#### **阶段 0: CubeDictionary 收集名称**
```javascript
// 结果：
cubeDictionary = {
  'Orders': true
}
```

#### **阶段 1: CubeSymbols 构建符号表**
```javascript
// 结果：
cubeSymbols = {
  'Orders': {
    measures: ['count', 'totalAmount'],
    dimensions: ['id', 'amount', 'status']
  }
}
```

#### **阶段 2: Transpilers 转换代码**

**ValidationTranspiler**: ✓ 语法正确

**ImportExportTranspiler**: (无 import/export，跳过)

**CubePropContextTranspiler**: 转换模板字符串引用
```javascript
// 转换前
totalAmount: {
  sql: `${amount}`,
  type: 'sum'
}

// 转换后
totalAmount: {
  sql: () => `${CUBE.amount}`,
  type: 'sum'
}
```

**IIFETranspiler**: 包装为 IIFE
```javascript
(function() {
  cube('Orders', { ... });
})();
```

#### **阶段 3: CubeEvaluator 评估**
```javascript
// 执行转换后的代码
vm.runInContext(transpiledCode, context);

// 结果：
cubeEvaluator.cubeFromPath('Orders') = {
  name: 'Orders',
  sql: [Function],
  measures: {
    count: { type: 'count', ownedByCube: true },
    totalAmount: { sql: [Function], type: 'sum', ownedByCube: true }
  },
  dimensions: {
    id: { sql: [Function], type: 'number', primaryKey: true, ownedByCube: true },
    amount: { sql: [Function], type: 'number', ownedByCube: true },
    status: { sql: [Function], type: 'string', ownedByCube: true }
  }
}
```

#### **阶段 3: JoinGraph 构建连接图**
```javascript
// 如果有 joins 定义
joins: {
  Customers: {
    sql: `${CUBE}.customer_id = ${Customers}.id`,
    relationship: 'belongsTo'
  }
}

// 结果：
joinGraph = {
  'Orders': {
    'Customers': { /* join info */ }
  }
}
```

#### **阶段 3: MetaTransformer 生成元数据**
```json
{
  "cubes": [
    {
      "name": "Orders",
      "title": "Orders",
      "measures": [
        {
          "name": "Orders.count",
          "title": "Orders Count",
          "type": "count"
        },
        {
          "name": "Orders.totalAmount",
          "title": "Orders Total Amount",
          "type": "sum"
        }
      ],
      "dimensions": [
        {
          "name": "Orders.id",
          "title": "Orders Id",
          "type": "number"
        },
        {
          "name": "Orders.amount",
          "title": "Orders Amount",
          "type": "number"
        },
        {
          "name": "Orders.status",
          "title": "Orders Status",
          "type": "string"
        }
      ]
    }
  ]
}
```

---

## 🚀 性能优化建议

### 1. **合理配置缓存大小**
```typescript
// 根据 schema 复杂度调整
const options = {
  compiledScriptCache: new LRUCache({
    max: schemaFileCount * 10  // 经验值：文件数 * 10
  }),
  maxQueryCacheSize: expectedQueriesPerHour * 2
};
```

### 2. **使用 standalone 模式**
```typescript
// 在无服务器环境或边缘计算中
const options = {
  standalone: true,  // 减少不必要的依赖
};
```

### 3. **启用编译缓存共享**
```typescript
// 在多个编译器实例间共享缓存
const sharedScriptCache = new LRUCache({ max: 1000 });

const compiler1 = prepareCompiler(repo1, {
  compiledScriptCache: sharedScriptCache
});
const compiler2 = prepareCompiler(repo2, {
  compiledScriptCache: sharedScriptCache
});
```

### 4. **监控编译性能**
```typescript
const start = Date.now();
await compiler.compile();
const duration = Date.now() - start;

console.log(`Compilation took ${duration}ms`);
console.log(`Script cache size: ${compiledScriptCache.size}`);
console.log(`Cache hit rate: ${cacheHits / totalRequests}`);
```

---

## 🐛 常见问题和解决方案

### 问题 1: 编译速度慢
**原因**: 缓存未启用或缓存过小
**解决**:
```typescript
const options = {
  compiledScriptCache: new LRUCache({ max: 500 }),
  compiledYamlCache: new LRUCache({ max: 300 }),
};
```

### 问题 2: 内存占用过高
**原因**: 缓存过大或未设置过期时间
**解决**:
```typescript
const options = {
  maxQueryCacheSize: 1000,           // 限制缓存大小
  maxQueryCacheAge: 1000 * 60 * 60,  // 设置过期时间
};
```

### 问题 3: Schema 验证失败
**原因**: Cube 定义不符合 Joi schema
**解决**: 查看 CubeValidator.ts 中的 schema 定义，确保所有字段类型正确。

### 问题 4: 引用解析失败
**原因**: CubePropContextTranspiler 无法解析 ${ref}
**解决**: 确保引用的 dimension/measure 存在，或使用完整路径 `${CubeName.field}`。

---

## 📚 相关文件

- **PrepareCompiler.ts**: 本文件
- **DataSchemaCompiler.ts**: 主编译器实现
- **CubeSymbols.ts**: 符号表管理
- **CubeValidator.ts**: Schema 验证
- **CubeEvaluator.ts**: Cube 评估
- **JoinGraph.ts**: 连接图管理
- **CubeToMetaTransformer.js**: 元数据转换
- **transpilers/**: 所有 transpiler 实现
- **YamlCompiler.ts**: YAML 编译器
- **CompilerCache.ts**: 缓存管理

---

## 总结

`PrepareCompiler.ts` 是整个 schema-compiler 的 **入口点和组装器**：

1. **作为工厂**: 创建并连接所有编译组件
2. **作为配置中心**: 接收配置并分发给各个组件
3. **作为协调器**: 确保编译流程按正确顺序执行
4. **作为优化器**: 通过缓存机制提升性能

理解这个文件，就理解了整个 Cube.js Schema 编译系统的架构！

---

**文档版本**: 1.0
**最后更新**: 2025-10-31
**适用版本**: Cube.js v1.5.0
