# CreateOptions 默认行为分析：externalDbType 和 cacheAndQueueDriver

## 问题

在 `CreateOptions` 中，如果不传 `externalDbType` 和 `cacheAndQueueDriver` 这两个可选参数，会有哪些默认行为？

## 类型定义

```typescript
// packages/cubejs-server-core/src/core/types.ts:184-238
export interface CreateOptions {
  // ... 其他选项
  externalDbType?: DatabaseType | ExternalDbTypeFn;
  cacheAndQueueDriver?: CacheAndQueryDriverType;
  // ... 其他选项
}
```

## 1. cacheAndQueueDriver 默认行为

### 决策逻辑

**代码位置**: `packages/cubejs-query-orchestrator/src/orchestrator/QueryOrchestrator.ts:41-60`

```typescript
function detectQueueAndCacheDriver(options: QueryOrchestratorOptions): CacheAndQueryDriverType {
  // 1. 优先使用传入的 options.cacheAndQueueDriver
  if (options.cacheAndQueueDriver) {
    return options.cacheAndQueueDriver;
  }

  // 2. 检查环境变量 CUBEJS_CACHE_AND_QUEUE_DRIVER
  const cacheAndQueueDriver = getEnv('cacheAndQueueDriver');
  if (cacheAndQueueDriver) {
    return cacheAndQueueDriver;
  }

  // 3. 检查是否配置了 Redis（已弃用）
  if (getEnv('redisUrl') || getEnv('redisUseIORedis')) {
    return 'redis';
  }

  // 4. 生产环境默认使用 cubestore
  if (getEnv('nodeEnv') === 'production') {
    return 'cubestore';
  }

  // 5. 开发环境默认使用 memory
  return 'memory';
}
```

### 默认值决策树

```
cacheAndQueueDriver 未设置
    ↓
检查 CUBEJS_CACHE_AND_QUEUE_DRIVER 环境变量
    ↓ (未设置)
检查 CUBEJS_REDIS_URL 或 REDIS_URL
    ↓ (未设置)
检查 NODE_ENV
    ├─ production → 'cubestore'
    └─ 其他/未设置 → 'memory'
```

### 实际行为示例

#### 场景 1: 开发环境（默认）
```typescript
const server = CubejsServerCore.create({
  // 未设置 cacheAndQueueDriver
});
// 结果: cacheAndQueueDriver = 'memory'
```

**行为**:
- ✅ 查询结果缓存存储在内存中
- ✅ 查询队列在内存中管理
- ⚠️ 进程重启后缓存丢失
- ⚠️ 不支持多实例共享缓存

#### 场景 2: 生产环境
```bash
NODE_ENV=production
```

```typescript
const server = CubejsServerCore.create({
  // 未设置 cacheAndQueueDriver
});
// 结果: cacheAndQueueDriver = 'cubestore'
```

**行为**:
- ✅ 尝试连接到 Cube Store
- ❌ 如果 Cube Store 未配置，会报错
- 需要配置 `CUBEJS_CUBESTORE_HOST` 和 `CUBEJS_CUBESTORE_PORT`

#### 场景 3: 配置了 Redis（已弃用）
```bash
CUBEJS_REDIS_URL=redis://localhost:6379
```

```typescript
const server = CubejsServerCore.create({
  // 未设置 cacheAndQueueDriver
});
// 结果: cacheAndQueueDriver = 'redis'
```

**警告**: Redis 作为缓存和队列驱动在 v0.36+ 已不再支持，会导致错误。

## 2. externalDbType 默认行为

### 决策逻辑

**代码位置**: `packages/cubejs-server-core/src/core/OptsHandler.ts:363-367`

```typescript
// 定义需要检查的外部数据库环境变量
const skipOnEnv = [
  'CUBEJS_EXT_DB_URL',
  'CUBEJS_EXT_DB_HOST',
  'CUBEJS_EXT_DB_NAME',
  'CUBEJS_EXT_DB_PORT',
  'CUBEJS_EXT_DB_USER',
  'CUBEJS_EXT_DB_PASS',
  // Cube Store variables
  'CUBEJS_CUBESTORE_HOST',
  'CUBEJS_CUBESTORE_PORT',
  'CUBEJS_CUBESTORE_USER',
  'CUBEJS_CUBESTORE_PASS',
];

// 检查是否定义了任何外部数据库环境变量
const definedExtDBVariables =
  skipOnEnv.filter((field) => process.env[field] !== undefined);

const externalDbType =
  opts.externalDbType ||                                          // 1. 传入的选项
  <DatabaseType | undefined>process.env.CUBEJS_EXT_DB_TYPE ||   // 2. 环境变量
  (getEnv('devMode') || definedExtDBVariables.length > 0) && 'cubestore' ||  // 3. 开发模式或有配置
  undefined;                                                     // 4. 默认 undefined
```

### 默认值决策树

```
externalDbType 未设置
    ↓
检查 CUBEJS_EXT_DB_TYPE 环境变量
    ↓ (未设置)
检查以下条件之一：
    ├─ CUBEJS_DEV_MODE=true → 'cubestore'
    ├─ 定义了任何 CUBEJS_EXT_DB_* 环境变量 → 'cubestore'
    ├─ 定义了任何 CUBEJS_CUBESTORE_* 环境变量 → 'cubestore'
    └─ 以上都不满足 → undefined (不使用外部数据库)
```

### 实际行为示例

#### 场景 1: 开发环境（默认配置）
```bash
CUBEJS_DEV_MODE=true
# 未配置任何 CUBEJS_CUBESTORE_* 或 CUBEJS_EXT_DB_* 变量
```

```typescript
const server = CubejsServerCore.create({
  // 未设置 externalDbType
});
// 结果: externalDbType = 'cubestore'
```

**行为**: `packages/cubejs-server-core/src/core/OptsHandler.ts:404-439`

```typescript
if (externalDbType === 'cubestore' && this.isDevMode() && !opts.serverless) {
  if (!definedExtDBVariables.length) {
    // 尝试加载 @cubejs-backend/cubestore-driver
    const cubeStorePackage = require('@cubejs-backend/cubestore-driver');

    if (cubeStorePackage.isCubeStoreSupported()) {
      // 创建 Cube Store 处理器
      const cubeStoreHandler = new cubeStorePackage.CubeStoreHandler({
        stdout: (data) => console.log(data.toString().trim()),
        stderr: (data) => console.log(data.toString().trim()),
        onRestart: (code) => this.core.logger('Cube Store Restarting', {
          warning: `Instance exit with ${code}, restarting`,
        }),
      });

      console.log(`🔥 Cube Store (${version}) is assigned to 3030 port.`);

      // 在官方 Docker 镜像中自动启动
      if (isDockerImage()) {
        cubeStoreHandler.acquire().catch((e) =>
          this.core.logger('Cube Store Start Error', { error: e.message })
        );
      }

      // 创建延迟加载的驱动工厂
      externalDriverFactory = () => new cubeStorePackage.CubeStoreDevDriver(cubeStoreHandler);
    } else {
      // 系统不支持 Cube Store
      this.core.logger('Cube Store is not supported on your system', {
        warning: `You are using ${process.platform} platform with ${process.arch} architecture, which is not supported by Cube Store.`
      });
    }
  }
}
```

**实际效果**:
- ✅ **自动启动内置 Cube Store 进程**（如果系统支持）
- ✅ 监听在 `localhost:3030`
- ✅ 数据存储在 `.cubestore/` 目录
- ✅ 在 Docker 镜像中会自动启动
- ⚠️ 如果系统不支持（如某些架构），会显示警告

#### 场景 2: 生产环境（未配置外部数据库）
```bash
CUBEJS_DEV_MODE=false
NODE_ENV=production
# 未配置任何 CUBEJS_CUBESTORE_* 或 CUBEJS_EXT_DB_* 变量
```

```typescript
const server = CubejsServerCore.create({
  // 未设置 externalDbType
});
// 结果: externalDbType = undefined
```

**行为**: `packages/cubejs-server-core/src/core/OptsHandler.ts:388-394`

```typescript
if (!this.isDevMode() && getEnv('externalDefault') && !externalDbType) {
  displayCLIWarning(
    'Cube Store is not found. Please follow this documentation ' +
    'to configure Cube Store ' +
    'https://cube.dev/docs/caching/running-in-production'
  );
}
```

**实际效果**:
- ⚠️ **不使用外部数据库**
- ⚠️ 显示警告信息（如果 `CUBEJS_EXTERNAL_DEFAULT=true`）
- ⚠️ 预聚合将在源数据库中构建（如果定义了预聚合且未设置 `external: false`）
- ❌ 无法使用需要外部存储的预聚合特性

#### 场景 3: 配置了 Cube Store 环境变量
```bash
CUBEJS_CUBESTORE_HOST=cubestore.internal
CUBEJS_CUBESTORE_PORT=3030
```

```typescript
const server = CubejsServerCore.create({
  // 未设置 externalDbType
});
// 结果: externalDbType = 'cubestore'
```

**行为**:
- ✅ 连接到外部 Cube Store 实例
- ✅ 使用配置的主机和端口
- ✅ 不会启动内置 Cube Store 进程

#### 场景 4: 配置了其他外部数据库
```bash
CUBEJS_EXT_DB_TYPE=postgres
CUBEJS_EXT_DB_HOST=postgres.internal
CUBEJS_EXT_DB_PORT=5432
# ... 其他配置
```

```typescript
const server = CubejsServerCore.create({
  // 未设置 externalDbType
});
// 结果: externalDbType = 'postgres'
```

**警告**: `packages/cubejs-server-core/src/core/OptsHandler.ts:396-402`

```typescript
if (this.isDevMode() && externalDbType !== 'cubestore') {
  displayCLIWarning(
    `Using ${externalDbType} as an external database is deprecated. ` +
    'Please use Cube Store instead: ' +
    'https://cube.dev/docs/caching/running-in-production'
  );
}
```

## 完整默认行为矩阵

| 环境 | externalDbType 默认值 | cacheAndQueueDriver 默认值 | 行为描述 |
|------|---------------------|--------------------------|----------|
| **开发环境（默认）** | `'cubestore'` | `'memory'` | 自动启动内置 Cube Store，缓存在内存 |
| **开发环境 + Docker** | `'cubestore'` | `'memory'` | 自动启动内置 Cube Store（已启动），缓存在内存 |
| **生产环境（无配置）** | `undefined` | `'cubestore'` | ⚠️ 需要手动配置 Cube Store 或报错 |
| **生产环境 + 配置了 CUBEJS_CUBESTORE_*** | `'cubestore'` | `'cubestore'` | ✅ 连接外部 Cube Store，推荐配置 |
| **任何环境 + 配置了 CUBEJS_EXT_DB_TYPE** | 环境变量值 | 根据 NODE_ENV | 使用指定的外部数据库 |
| **任何环境 + 配置了 Redis** | 根据其他条件 | `'redis'` | ❌ v0.36+ 不支持，会报错 |

## 常见配置组合及其影响

### 组合 1: 最小化开发配置（默认）
```typescript
const server = CubejsServerCore.create({
  // 什么都不传
});
```

**实际行为**:
```
CUBEJS_DEV_MODE=true (默认)
externalDbType = 'cubestore'
cacheAndQueueDriver = 'memory'

→ 自动启动 Cube Store 进程 (localhost:3030)
→ 缓存在内存中
→ 预聚合存储在 .cubestore/ 目录
→ 进程重启后缓存丢失，但预聚合数据保留
```

### 组合 2: 生产环境 - 完整配置
```bash
NODE_ENV=production
CUBEJS_CUBESTORE_HOST=cubestore.internal
CUBEJS_CUBESTORE_PORT=3030
```

```typescript
const server = CubejsServerCore.create({
  // 什么都不传
});
```

**实际行为**:
```
externalDbType = 'cubestore'
cacheAndQueueDriver = 'cubestore'

→ 连接到外部 Cube Store (cubestore.internal:3030)
→ 缓存和队列都在 Cube Store 中
→ 预聚合存储在 Cube Store 中
→ 支持多实例、高可用、持久化
```

### 组合 3: 生产环境 - 最小化配置（不推荐）
```bash
NODE_ENV=production
# 不配置任何 Cube Store
```

```typescript
const server = CubejsServerCore.create({
  cacheAndQueueDriver: 'memory',  // 显式设置
});
```

**实际行为**:
```
externalDbType = undefined
cacheAndQueueDriver = 'memory'

⚠️ 警告: Cube Store is not found
→ 缓存在内存中（多实例不共享）
→ 预聚合在源数据库中构建（如果定义了）
→ 性能和扩展性受限
```

### 组合 4: 开发环境 - 禁用 Cube Store
```bash
CUBEJS_DEV_MODE=true
```

```typescript
const server = CubejsServerCore.create({
  externalDbType: undefined,  // 显式禁用
  cacheAndQueueDriver: 'memory',
});
```

**实际行为**:
```
externalDbType = undefined
cacheAndQueueDriver = 'memory'

→ 不启动 Cube Store
→ 缓存在内存中
→ 预聚合在源数据库中（如果定义了 external: false）
→ 架构最简单，但无法使用外部预聚合
```

## 关键环境变量影响

### 触发 Cube Store 自动配置的变量

以下任一环境变量被设置，都会导致 `externalDbType` 默认为 `'cubestore'`：

```bash
# Cube Store 专用变量
CUBEJS_CUBESTORE_HOST
CUBEJS_CUBESTORE_PORT
CUBEJS_CUBESTORE_USER
CUBEJS_CUBESTORE_PASS

# 通用外部数据库变量
CUBEJS_EXT_DB_URL
CUBEJS_EXT_DB_HOST
CUBEJS_EXT_DB_NAME
CUBEJS_EXT_DB_PORT
CUBEJS_EXT_DB_USER
CUBEJS_EXT_DB_PASS
```

### 特殊变量

```bash
# 直接指定外部数据库类型（优先级最高）
CUBEJS_EXT_DB_TYPE=cubestore|postgres|mysql

# 直接指定缓存和队列驱动
CUBEJS_CACHE_AND_QUEUE_DRIVER=memory|cubestore

# 控制是否显示警告
CUBEJS_EXTERNAL_DEFAULT=true  # 生产环境未配置时显示警告

# 开发模式（影响所有默认值）
CUBEJS_DEV_MODE=true|false
NODE_ENV=development|production
```

## 最佳实践建议

### 开发环境
```typescript
// 选项 1: 使用默认配置（推荐）
const server = CubejsServerCore.create({
  // 让 Cube 自动启动 Cube Store
});

// 选项 2: 完全不使用外部存储（简单场景）
const server = CubejsServerCore.create({
  cacheAndQueueDriver: 'memory',
  // 不定义预聚合，或使用 external: false
});
```

### 生产环境
```typescript
// 推荐配置
const server = CubejsServerCore.create({
  // 通过环境变量配置
  // CUBEJS_CUBESTORE_HOST=cubestore.internal
  // CUBEJS_CUBESTORE_PORT=3030
  //
  // externalDbType 和 cacheAndQueueDriver 会自动设置为 'cubestore'
});
```

```bash
# 环境变量
NODE_ENV=production
CUBEJS_CUBESTORE_HOST=cubestore.internal
CUBEJS_CUBESTORE_PORT=3030
CUBEJS_CUBESTORE_USER=cube
CUBEJS_CUBESTORE_PASS=secret
```

## 潜在问题和解决方案

### 问题 1: 生产环境启动失败

**症状**:
```
Error: Cube Store was specified as queue/cache driver.
Please set CUBEJS_CUBESTORE_HOST and CUBEJS_CUBESTORE_PORT variables.
```

**原因**: `NODE_ENV=production` 时默认使用 Cube Store，但未配置

**解决方案**:
```bash
# 方案 1: 配置 Cube Store（推荐）
CUBEJS_CUBESTORE_HOST=cubestore-host
CUBEJS_CUBESTORE_PORT=3030

# 方案 2: 显式使用 memory（不推荐生产环境）
CUBEJS_CACHE_AND_QUEUE_DRIVER=memory
```

### 问题 2: 开发环境 Cube Store 启动失败

**症状**:
```
Cube Store is not supported on your system
You are using darwin platform with arm64 architecture
```

**原因**: 系统架构不支持 Cube Store

**解决方案**:
```typescript
const server = CubejsServerCore.create({
  cacheAndQueueDriver: 'memory',
  // 不使用外部预聚合
});
```

### 问题 3: Redis 配置导致错误

**症状**:
```
Only 'cubestore' or 'memory' are supported for cacheAndQueueDriver option, passed: redis
```

**原因**: v0.36+ 不再支持 Redis

**解决方案**:
```bash
# 移除 Redis 配置
# CUBEJS_REDIS_URL=...  # 删除

# 显式设置为 cubestore 或 memory
CUBEJS_CACHE_AND_QUEUE_DRIVER=cubestore
```

## 总结

### cacheAndQueueDriver 不传时

| 条件 | 默认值 |
|------|--------|
| 配置了 `CUBEJS_CACHE_AND_QUEUE_DRIVER` | 环境变量值 |
| 配置了 Redis | `'redis'` (会报错) |
| `NODE_ENV=production` | `'cubestore'` |
| 其他情况 | `'memory'` |

### externalDbType 不传时

| 条件 | 默认值 |
|------|--------|
| 配置了 `CUBEJS_EXT_DB_TYPE` | 环境变量值 |
| `CUBEJS_DEV_MODE=true` | `'cubestore'` (自动启动) |
| 配置了任何 `CUBEJS_CUBESTORE_*` | `'cubestore'` |
| 配置了任何 `CUBEJS_EXT_DB_*` | `'cubestore'` |
| 其他情况 | `undefined` (不使用) |

### 关键要点

1. ✅ **开发环境默认会自动启动 Cube Store**（如果系统支持）
2. ⚠️ **生产环境必须手动配置 Cube Store**，否则可能报错
3. ❌ **Redis 不再支持**，会导致错误
4. 💡 **两个参数相互独立**，但推荐统一使用 Cube Store
5. 🔧 **环境变量优先级高于代码配置**

---

*文档生成时间: 2025-10-31*
*基于 Cube.js 版本: v1.5.0*
*分析文件: packages/cubejs-server-core/src/core/types.ts*
