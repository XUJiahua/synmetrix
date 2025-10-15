# Cube.js 查询去重机制详解

## 概述

Cube.js 对于耗时长的请求会返回 "Continue wait" 响应。下次相同的请求应该是查询正在进行的长耗时请求的结果，而不是重新发起数据库查询。本文档详细分析了这一去重功能的实现机制。

## 核心原理

Cube.js 通过 **QueryQueue** 和底层的 **QueueDriver** 实现查询去重，确保相同的查询请求不会重复执行数据库查询。

## 查询去重的关键流程

### 1. 查询键(QueryKey)与哈希

每个查询都会生成一个唯一的 `QueryKey`，然后转换为 `QueryKeyHash` 作为去重标识：

```typescript
const queryKeyHash = this.redisHash(queryKey);
```

**位置**: `packages/cubejs-query-orchestrator/src/orchestrator/QueryQueue.ts:241`

### 2. 队列添加时的去重检查

在 `executeInQueue` 方法中 (`QueryQueue.ts:184-370`)，当请求进来时：

#### 步骤 1: 先检查缓存结果

```typescript
let result = !query.forceBuild && await queueConnection.getResult(queryKey);
if (result && !result.streamResult) {
    return this.parseResult(result);
}
```

**位置**: `QueryQueue.ts:236-239`

如果已有缓存结果，直接返回，无需进入队列。

#### 步骤 2: 尝试将查询添加到队列

```typescript
const [added, queueId, queueSize, addedToQueueTime] = await queueConnection.addToQueue(
    keyScore, queryKey, orphanedTime, queryHandler, query, priority, options
);
```

**位置**: `QueryQueue.ts:256-258`

### 3. addToQueue 的去重逻辑

在 `LocalQueueDriverConnection.ts:164-206` 中：

```typescript
if (!this.state.queryDef[key]) {
    this.state.queryDef[key] = queryQueueObj;
}

let added = 0;

if (!this.state.toProcess[key] && !this.state.active[key]) {
    this.state.toProcess[key] = {
        order: keyScore,
        queueId: options.queueId,
        key
    };
    added = 1;
}
```

**关键点**：
- 如果 `queryKeyHash` 已经在 `toProcess`(待处理队列) 或 `active`(正在执行) 中存在，`added` 返回 0
- 如果不存在，则添加到 `toProcess` 队列，`added` 返回 1

### 4. 等待已存在查询的结果

当 `added = 0` 时（表示查询已经在队列中），新请求不会重新发起查询，而是：

```typescript
if (!added) {
    const queryDef = await queueConnection.getQueryDef(queryKeyHash, queueId);
    if (queryDef) {
        waitingContext = {
            queueId,
            spanId: options.spanId,
            queryKey: queryDef.queryKey,
            queuePrefix: this.redisQueuePrefix,
            requestId: options.requestId,
            waitingForRequestId: queryDef.requestId
        };
    }
}
```

**位置**: `QueryQueue.ts:290-302`

然后等待结果：

```typescript
result = !query.isJob && await queueConnection.getResultBlocking(queryKeyHash, queueId);
```

**位置**: `QueryQueue.ts:353`

### 5. getResultBlocking 的实现

在 `LocalQueueDriverConnection.ts:119-135` 中：

```typescript
public async getResultBlocking(queryKeyHash: QueryKeyHash, _queueId?: QueueId): Promise<any> {
    const resultListKey = this.resultListKey(queryKeyHash);
    if (!this.state.queryDef[queryKeyHash] && !this.state.resultPromises[resultListKey]) {
        return null;
    }
    const timeoutPromise = (timeout: number) => new Promise((resolve) =>
        setTimeout(() => resolve(null), timeout));

    const res = await Promise.race([
        this.getResultPromise(resultListKey),
        timeoutPromise(this.continueWaitTimeout * 1000),
    ]);

    if (res) {
        delete this.state.resultPromises[resultListKey];
    }
    return res;
}
```

**关键机制**：
- 使用 **Promise 共享机制**: 所有相同 queryKeyHash 的请求共享同一个 `resultPromise`
- 使用 `Promise.race` 在两者之间竞争：
  - 等待实际查询结果
  - 等待 `continueWaitTimeout` 超时（默认5秒）

### 6. Continue Wait 机制

如果超时仍未完成，抛出 `ContinueWaitError`：

```typescript
if (!query.isJob && !result) {
    throw new ContinueWaitError();
}
```

**位置**: `QueryQueue.ts:357-359`

这会返回 "Continue wait" 响应给客户端，客户端会用**相同的 QueryKey** 再次请求。

### 7. 结果设置与广播

当查询完成后，在 `setResultAndRemoveQuery` 中：

```typescript
public async setResultAndRemoveQuery(queryKeyHash: QueryKeyHash, executionResult: any,
    processingId: ProcessingId, _queueId?: QueueId | null): Promise<boolean> {

    const promise = this.getResultPromise(this.resultListKey(queryKeyHash));

    // 清理队列状态
    delete this.state.active[queryKeyHash];
    delete this.state.toProcess[queryKeyHash];
    delete this.state.queryDef[queryKeyHash];

    // 标记结果已准备好，并通知所有等待的请求
    promise.resolved = true;
    if (promise.resolve) {
        promise.resolve(executionResult);
    }

    return true;
}
```

**位置**: `LocalQueueDriverConnection.ts:234-254`

所有等待该 queryKeyHash 结果的请求都会通过 `promise.resolve(executionResult)` 同时收到结果。

## 队列驱动模式对比

Cube.js 支持三种队列驱动模式（在 `QueryQueue.ts:52-68` 中定义）：

1. **memory** (LocalQueueDriver) - 内存模式
2. **cubestore** (CubeStoreQueueDriver) - CubeStore 模式

### Memory 模式的去重机制

#### 共享状态实现

在 `LocalQueueDriver.ts:8-16` 中：

```typescript
const sharedState: Record<string, LocalQueueDriverConnectionState> = {};

function getState(prefix: string): LocalQueueDriverConnectionState {
  if (!sharedState[prefix]) {
    sharedState[prefix] = new LocalQueueDriverConnectionState();
  }
  return sharedState[prefix];
}
```

**关键点**：
- 使用**全局内存变量** `sharedState` 存储队列状态
- 同一个 Node.js 进程内的所有请求共享这个状态
- 包含 `queryDef`、`toProcess`、`active`、`resultPromises` 等队列信息

#### 去重逻辑与 CubeStore 相同

在 `LocalQueueDriverConnection.ts:164-206` 中的 `addToQueue` 方法实现了相同的去重逻辑。

### CubeStore 模式的去重机制

CubeStore 通过 SQL 命令实现分布式队列（在 `CubeStoreQueueDriver.ts:55-96`）：

```typescript
const rows = await this.driver.query(
  `QUEUE ADD PRIORITY ?${options.orphanedTimeout ? ' ORPHANED ?' : ''} ? ?`,
  values
);
```

**CubeStore 特殊命令**：
- `QUEUE ADD` - 添加到队列（原子性检查是否已存在）
- `QUEUE RETRIEVE` - 获取待处理查询（带并发控制）
- `QUEUE RESULT_BLOCKING` - 阻塞等待结果（`CubeStoreQueueDriver.ts:297-308`）
- `QUEUE ACK` - 确认完成并设置结果

这些命令在 CubeStore 内部实现了**分布式锁和去重逻辑**，确保跨多个 Cube 实例的查询去重。

### 两种模式对比

| 特性 | Memory 模式 | CubeStore 模式 |
|------|------------|----------------|
| **去重范围** | 单个 Node.js 进程 | 跨所有 Cube 实例 |
| **持久化** | 无，进程重启丢失 | 持久化到 CubeStore |
| **集群支持** | ❌ 不支持跨进程 | ✅ 支持多节点集群 |
| **实现方式** | 内存 Promise | CubeStore SQL 命令 |
| **生产环境** | ❌ 不推荐 | ✅ 推荐 |

## Memory 模式的局限性

### 1. 单进程限制

如果运行多个 Cube.js 实例（例如通过 PM2 或 Kubernetes），每个进程有自己独立的内存状态：

```
进程1: sharedState[prefix] = { queryDef: {}, toProcess: {}, ... }
进程2: sharedState[prefix] = { queryDef: {}, toProcess: {}, ... }  // 独立的状态！
```

**结果**：
- 相同查询可能在不同进程中重复执行
- 去重只在单个进程内有效

### 2. 负载均衡问题

在负载均衡场景：

```
请求1 -> 负载均衡 -> Cube实例A (执行查询)
请求2 (相同查询) -> 负载均衡 -> Cube实例B (重复执行！)
```

## 部署建议

### 开发环境

```javascript
// 可以使用 memory 模式
{
  cacheAndQueueDriver: 'memory'
}
```

- 去重功能正常工作
- 单进程环境足够
- 配置简单，无需额外服务

### 生产环境

```javascript
// 必须使用 cubestore
{
  cacheAndQueueDriver: 'cubestore'
}
```

- 支持水平扩展
- 跨实例去重
- 持久化队列状态
- 分布式锁保证一致性

## 总结

Cube.js 的查询去重机制通过以下方式实现：

1. **QueryKey 哈希**: 相同查询生成相同的 hash 作为唯一标识
2. **队列状态检查**: 检查 `toProcess` 和 `active` 队列，避免重复添加
3. **Promise 共享**: 多个相同请求共享同一个 result promise
4. **超时与重试**: 超时返回 "Continue wait"，客户端重新请求时继续等待同一个查询
5. **结果广播**: 查询完成后，通过 promise.resolve() 同时通知所有等待的请求

### 去重逻辑在不使用 CubeStore 时的工作情况

✅ **可以工作的场景**：
- 单个 Node.js 进程
- 开发环境
- 小规模部署（单实例）

❌ **无法工作的场景**：
- 多进程集群（PM2、Kubernetes 多副本）
- 负载均衡多实例部署
- 需要队列持久化的场景

**核心逻辑（QueryKey 哈希、队列检查、Promise 共享）在两种模式下是相同的，只是存储层不同：Memory 用进程内存，CubeStore 用分布式存储。**

这样确保了同一时刻，相同的查询只会在数据库层面执行一次，大大提高了效率并减少了数据库负载。

## 相关代码位置

- **QueryQueue**: `packages/cubejs-query-orchestrator/src/orchestrator/QueryQueue.ts`
- **LocalQueueDriver**: `packages/cubejs-query-orchestrator/src/orchestrator/LocalQueueDriver.ts`
- **LocalQueueDriverConnection**: `packages/cubejs-query-orchestrator/src/orchestrator/LocalQueueDriverConnection.ts`
- **CubeStoreQueueDriver**: `packages/cubejs-cubestore-driver/src/CubeStoreQueueDriver.ts`
- **ContinueWaitError**: `packages/cubejs-query-orchestrator/src/orchestrator/ContinueWaitError.ts`
- **QueueDriverInterface**: `packages/cubejs-base-driver/src/queue-driver.interface.ts`
