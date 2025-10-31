# Cube Store 依赖分析：CUBEJS_CACHE_AND_QUEUE_DRIVER 环境变量详解

## 概述

本文档深入分析 `CUBEJS_CACHE_AND_QUEUE_DRIVER` 环境变量的作用，以及如何在部署 Cube.js 时减少或消除对 Cube Store 的依赖。

## 核心问题

**Q: 设置 `CUBEJS_CACHE_AND_QUEUE_DRIVER=memory` 是否可以完全不依赖 Cube Store？**

**A: 不完全是。** `CUBEJS_CACHE_AND_QUEUE_DRIVER=memory` 只解决了部分依赖问题。

## Cube Store 的双重角色

Cube Store 在 Cube 架构中扮演**两个独立的角色**：

### 1. 缓存和队列驱动 (Cache & Queue Driver)

**控制变量**: `CUBEJS_CACHE_AND_QUEUE_DRIVER`

**职责**:
- 查询结果缓存 (Query Result Cache)
- 查询队列管理 (Query Queue Management)
- 预聚合元数据缓存 (Pre-aggregation Metadata Cache)

**可选值**:
- `memory` - 内存存储（开发环境默认）
- `cubestore` - Cube Store 存储（生产环境默认）
- ~~`redis`~~ - Redis 存储（已弃用，v0.36+ 不再支持）

**代码位置**: `packages/cubejs-query-orchestrator/src/orchestrator/QueryOrchestrator.ts`

```typescript
function detectQueueAndCacheDriver(options: QueryOrchestratorOptions): CacheAndQueryDriverType {
  if (options.cacheAndQueueDriver) {
    return options.cacheAndQueueDriver;
  }

  const cacheAndQueueDriver = getEnv('cacheAndQueueDriver');
  if (cacheAndQueueDriver) {
    return cacheAndQueueDriver;
  }

  // 检查是否配置了 Redis（已弃用）
  if (getEnv('redisUrl') || getEnv('redisUseIORedis')) {
    return 'redis';
  }

  // 生产环境默认使用 cubestore
  if (getEnv('nodeEnv') === 'production') {
    return 'cubestore';
  }

  // 开发环境默认使用 memory
  return 'memory';
}
```

### 2. 预聚合存储引擎 (Pre-aggregation Storage / External Database)

**控制变量**: `CUBEJS_EXT_DB_TYPE` 或配置选项 `externalDbType`

**职责**:
- 存储预聚合表的实际数据
- 提供高性能 OLAP 查询能力
- 支持跨数据库联接 (Data Federation)

**可选值**:
- `cubestore` - Cube Store（推荐）
- `postgres` - PostgreSQL（不推荐，低优先级支持）
- `mysql` - MySQL（不推荐，低优先级支持）
- 未设置 - 不使用外部存储，预聚合在源数据库中构建

**代码位置**: `packages/cubejs-server-core/src/core/OptsHandler.ts:363-367`

```typescript
const externalDbType =
  opts.externalDbType ||
  <DatabaseType | undefined>process.env.CUBEJS_EXT_DB_TYPE ||
  // 开发模式或有相关环境变量时默认使用 cubestore
  (getEnv('devMode') || definedExtDBVariables.length > 0) && 'cubestore' ||
  undefined;
```

## 默认行为分析

### 开发环境 (CUBEJS_DEV_MODE=true)

```bash
# 默认行为
CUBEJS_CACHE_AND_QUEUE_DRIVER=memory        # 缓存和队列使用内存
CUBEJS_EXT_DB_TYPE=cubestore                # 预聚合使用 Cube Store

# Cube 会自动做什么？
# 1. 自动启动内置的 Cube Store 进程
# 2. 数据存储在 .cubestore/ 目录
# 3. 监听在 localhost:3030 端口
```

**代码证据**: `packages/cubejs-server-core/src/core/OptsHandler.ts:404-439`

```typescript
if (externalDbType === 'cubestore' && this.isDevMode() && !opts.serverless) {
  if (!definedExtDBVariables.length) {
    const cubeStorePackage = require('@cubejs-backend/cubestore-driver');
    if (cubeStorePackage.isCubeStoreSupported()) {
      const cubeStoreHandler = new cubeStorePackage.CubeStoreHandler({
        stdout: (data) => console.log(data.toString().trim()),
        stderr: (data) => console.log(data.toString().trim()),
        onRestart: (code) => this.core.logger('Cube Store Restarting', {
          warning: `Instance exit with ${code}, restarting`,
        }),
      });

      console.log(`🔥 Cube Store (${version}) is assigned to 3030 port.`);

      // 在官方 Docker 镜像中自动启动 Cube Store
      if (isDockerImage()) {
        cubeStoreHandler.acquire().catch(
          (e) => this.core.logger('Cube Store Start Error', {
            error: e.message,
          })
        );
      }

      // Lazy loading for Cube Store
      externalDriverFactory = () => new cubeStorePackage.CubeStoreDevDriver(cubeStoreHandler);
    }
  }
}
```

### 生产环境 (CUBEJS_DEV_MODE=false 或 NODE_ENV=production)

```bash
# 默认行为
CUBEJS_CACHE_AND_QUEUE_DRIVER=cubestore     # 缓存和队列使用 Cube Store
# 需要手动配置外部 Cube Store
CUBEJS_CUBESTORE_HOST=cubestore-host
CUBEJS_CUBESTORE_PORT=3030
```

如果未配置，会显示警告：

```typescript
if (!this.isDevMode() && getEnv('externalDefault') && !externalDbType) {
  displayCLIWarning(
    'Cube Store is not found. Please follow this documentation ' +
    'to configure Cube Store ' +
    'https://cube.dev/docs/caching/running-in-production'
  );
}
```

## 完全不依赖 Cube Store 的方案

### 方案 1: 最小化部署（不使用预聚合）

**适用场景**:
- 小规模数据
- 源数据库性能足够
- 不需要预聚合加速

**配置**:
```bash
# 环境变量
CUBEJS_CACHE_AND_QUEUE_DRIVER=memory
CUBEJS_DEV_MODE=false
# 不设置 CUBEJS_EXT_DB_TYPE

# 数据模型中不定义预聚合
# 所有查询直接从源数据库执行
```

**优点**:
- 架构简单
- 无需额外组件
- 部署成本低

**缺点**:
- 无法使用预聚合功能
- 查询性能完全依赖源数据库
- 无法处理大规模数据分析

### 方案 2: 使用其他数据库存储预聚合

**适用场景**:
- 已有 PostgreSQL/MySQL 基础设施
- 不想引入新组件
- 数据规模适中

**配置**:
```bash
# 环境变量
CUBEJS_CACHE_AND_QUEUE_DRIVER=memory

# 使用 PostgreSQL 存储预聚合（不推荐）
CUBEJS_EXT_DB_TYPE=postgres
CUBEJS_EXT_DB_HOST=your-postgres-host
CUBEJS_EXT_DB_PORT=5432
CUBEJS_EXT_DB_NAME=cube_pre_aggregations
CUBEJS_EXT_DB_USER=cube
CUBEJS_EXT_DB_PASS=password
```

**警告**（来自代码）:
```typescript
if (this.isDevMode() && externalDbType !== 'cubestore') {
  displayCLIWarning(
    `Using ${externalDbType} as an external database is deprecated. ` +
    'Please use Cube Store instead: ' +
    'https://cube.dev/docs/caching/running-in-production'
  );
}
```

**优点**:
- 可以使用预聚合
- 利用现有基础设施

**缺点**:
- 官方不推荐，低优先级支持
- 性能不如 Cube Store
- 缺少 Cube Store 的优化特性

### 方案 3: 源数据库内预聚合

**适用场景**:
- 源数据库性能强大
- 希望统一管理
- 简化架构

**配置**:
```yaml
# 在 cube 定义中设置 external: false
cubes:
  - name: orders
    sql: SELECT * FROM orders

    pre_aggregations:
      - name: main
        type: rollup
        external: false  # 在源数据库中构建预聚合
        measures:
          - count
        dimensions:
          - status
        time_dimension: created_at
        granularity: day
```

**环境变量**:
```bash
CUBEJS_CACHE_AND_QUEUE_DRIVER=memory
# 不设置 CUBEJS_EXT_DB_TYPE
```

**优点**:
- 完全不依赖 Cube Store
- 架构简单
- 预聚合数据在源数据库中，便于管理

**缺点**:
- 占用源数据库存储和计算资源
- 影响源数据库性能
- 无法使用 Cube Store 的高级特性

## 生产环境推荐方案

### 推荐配置（使用 Cube Store）

```bash
# 缓存和队列都使用 Cube Store
CUBEJS_CACHE_AND_QUEUE_DRIVER=cubestore

# 配置 Cube Store 连接
CUBEJS_CUBESTORE_HOST=cubestore.internal
CUBEJS_CUBESTORE_PORT=3030
CUBEJS_CUBESTORE_USER=cube
CUBEJS_CUBESTORE_PASS=secret

# Cube Store 同时作为：
# 1. 缓存和队列驱动
# 2. 预聚合存储引擎
```

**为什么推荐？**

1. **高性能**: 专为 OLAP 查询优化
2. **高并发**: 支持大量并发查询
3. **低延迟**: 亚秒级响应时间
4. **数据联邦**: 支持跨数据库联接
5. **官方支持**: 优先级最高的支持

### Docker Compose 示例

```yaml
version: '3.8'

services:
  cube:
    image: cubejs/cube:latest
    environment:
      - CUBEJS_DEV_MODE=false
      - CUBEJS_CACHE_AND_QUEUE_DRIVER=cubestore
      - CUBEJS_CUBESTORE_HOST=cubestore
      - CUBEJS_CUBESTORE_PORT=3030
      # ... 其他配置
    depends_on:
      - cubestore

  cubestore:
    image: cubejs/cubestore:latest
    ports:
      - "3030:3030"
    volumes:
      - cubestore-data:/cube/data
    environment:
      - CUBESTORE_WORKERS=1

volumes:
  cubestore-data:
```

## 配置方式对比

| 配置方式 | 缓存/队列 | 预聚合存储 | 复杂度 | 性能 | 推荐度 |
|---------|----------|-----------|-------|------|-------|
| 纯 Memory + 无预聚合 | Memory | N/A | ⭐ | ⭐ | 仅限开发/测试 |
| Memory + PostgreSQL | Memory | PostgreSQL | ⭐⭐ | ⭐⭐ | 不推荐 |
| Memory + 源库预聚合 | Memory | 源数据库 | ⭐⭐ | ⭐⭐ | 小规模可用 |
| Cube Store (推荐) | Cube Store | Cube Store | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ |

## 环境变量完整参考

### 缓存和队列相关

```bash
# 主配置
CUBEJS_CACHE_AND_QUEUE_DRIVER=memory|cubestore

# Cube Store 连接配置（当使用 cubestore 时）
CUBEJS_CUBESTORE_HOST=localhost
CUBEJS_CUBESTORE_PORT=3030
CUBEJS_CUBESTORE_USER=cube
CUBEJS_CUBESTORE_PASS=secret
CUBEJS_CUBESTORE_MAX_CONNECT_RETRIES=20
CUBEJS_CUBESTORE_NO_HEART_BEAT_TIMEOUT=30
```

### 预聚合存储相关

```bash
# 外部数据库类型
CUBEJS_EXT_DB_TYPE=cubestore|postgres|mysql

# 外部数据库连接（通用）
CUBEJS_EXT_DB_HOST=localhost
CUBEJS_EXT_DB_PORT=5432
CUBEJS_EXT_DB_NAME=cube_external
CUBEJS_EXT_DB_USER=cube
CUBEJS_EXT_DB_PASS=password
CUBEJS_EXT_DB_URL=postgresql://user:pass@host:5432/db

# 如果使用 cubestore，通常复用 CUBEJS_CUBESTORE_* 变量
```

### 其他相关配置

```bash
# 开发模式（影响默认值）
CUBEJS_DEV_MODE=true|false

# 预聚合相关
CUBEJS_PRE_AGGREGATIONS_SCHEMA=prod_pre_aggregations
CUBEJS_SCHEDULED_REFRESH_TIMEZONES=America/Los_Angeles,America/New_York
CUBEJS_REFRESH_WORKER=true|false

# 仅使用预聚合（不查询源数据库）
CUBEJS_ROLLUP_ONLY=true|false
```

## 代码位置索引

| 功能 | 文件路径 | 行号 |
|------|---------|------|
| 缓存/队列驱动检测 | `packages/cubejs-query-orchestrator/src/orchestrator/QueryOrchestrator.ts` | 41-60 |
| 外部数据库配置 | `packages/cubejs-server-core/src/core/OptsHandler.ts` | 363-367 |
| 开发模式 Cube Store 启动 | `packages/cubejs-server-core/src/core/OptsHandler.ts` | 404-449 |
| 环境变量定义 | `packages/cubejs-backend-shared/src/env.ts` | 2031-2032 |
| 环境变量文档 | `docs/pages/product/configuration/reference/environment-variables.mdx` | 89-98 |
| 配置选项文档 | `docs/pages/product/configuration/reference/config.mdx` | 285-308 |

## 常见问题

### Q1: 为什么不推荐使用 PostgreSQL/MySQL 作为预聚合存储？

**A**:
1. 性能：Cube Store 专为 OLAP 查询优化，使用列式存储
2. 并发：Cube Store 支持更高的并发查询
3. 功能：缺少数据联邦等高级特性
4. 支持：官方低优先级支持，可能存在兼容性问题

### Q2: Memory 模式下数据会丢失吗？

**A**: 是的。`CUBEJS_CACHE_AND_QUEUE_DRIVER=memory` 时：
- 所有缓存数据存储在内存中
- 进程重启后数据丢失
- 不适合生产环境

### Q3: 如何在 Kubernetes 中部署 Cube Store？

**A**: 参考官方文档：
- [Running in Production](https://cube.dev/docs/deployment/production-checklist)
- [Cube Store Deployment](https://cube.dev/docs/caching/running-in-production)

### Q4: 可以混合使用不同的配置吗？

**A**: 可以，例如：
```bash
# 缓存/队列用 memory，预聚合用 Cube Store
CUBEJS_CACHE_AND_QUEUE_DRIVER=memory
CUBEJS_EXT_DB_TYPE=cubestore
CUBEJS_CUBESTORE_HOST=cubestore-host
```

但不推荐，建议统一使用 Cube Store。

## 总结

1. **`CUBEJS_CACHE_AND_QUEUE_DRIVER=memory` 只解决了缓存和队列的依赖**
2. **预聚合存储是一个独立的配置项**
3. **完全不用 Cube Store 意味着放弃预聚合或使用其他数据库**
4. **生产环境强烈推荐使用 Cube Store 作为统一的缓存/队列/预聚合存储**
5. **开发环境中 Cube 会自动启动内置的 Cube Store 进程**

## 相关文档

- [Environment Variables Reference](https://cube.dev/docs/product/configuration/reference/environment-variables#cubejs_cache_and_queue_driver)
- [Configuration Options Reference](https://cube.dev/docs/product/configuration/reference/config#cache_and_queue_driver)
- [Using Pre-aggregations](https://cube.dev/docs/product/caching/using-pre-aggregations)
- [Running in Production](https://cube.dev/docs/deployment/production-checklist)
- [Cube Store Introduction](https://cube.dev/blog/introducing-cubestore)

---

*文档生成时间: 2025-10-31*
*基于 Cube.js 版本: v1.5.0*
*分析代码库: cubejs/cubejs*
