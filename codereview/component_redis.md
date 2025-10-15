# Redis 组件分析（可选组件）

## 概述

Redis 在 Synmetrix 架构中作为可选的辅助组件,主要用于异步日志传输和分布式锁机制。设计上采用优雅降级策略,即使 Redis 不可用,系统仍可正常运行。

## 主要用途

### 1. 日志流 (Log Streaming) - 核心功能

**实现位置**: `services/cubejs/src/utils/logging.js:16-59`

Redis Stream 用作 CubeJS 和 Actions 服务之间的日志传输管道。

#### 工作流程

```
┌─────────────┐         ┌─────────┐         ┌─────────────┐         ┌────────────┐
│   CubeJS    │  XADD   │  Redis  │  XREAD  │   Actions   │ INSERT  │ PostgreSQL │
│   Request   │ ──────> │ Streams │ ──────> │  saveLogs   │ ──────> │   Logs     │
└─────────────┘         └─────────┘         └─────────────┘         └────────────┘
```

#### CubeJS 写入日志

**文件**: `services/cubejs/src/utils/logging.js`

```javascript
export const logging = async (message, event) => {
  const requestId = event?.requestId;

  // 1. 输出到控制台
  const log = devLogger("info")(message, event);
  if (log) {
    console.log(log);
  }

  // 2. 检查 Redis 状态
  if (redisClient?.status !== "ready") {
    console.warn(
      "Redis is disabled. To view logs in UI, set the REDIS_ADDR or check the connection."
    );
    return;
  }

  // 3. 过滤调度器请求
  if (!requestId || requestId?.includes("scheduler")) {
    return;
  }

  // 4. 构造日志数据
  const data = event;
  data.event = message;
  data.timestamp = new Date().toISOString();

  if (data?.securityContext) {
    data.userId = data.securityContext?.userId;
    data.dataSourceId = data.securityContext?.userScope?.dataSource?.dataSourceId;
    delete data.securityContext;
  }

  // 5. 写入 Redis Stream
  await redisClient.xadd(
    "streams:cubejs-logs-stream",
    "*",
    "data",
    JSON.stringify(data)
  );
};
```

#### 日志数据结构

```javascript
{
  requestId: "uuid",
  event: "Load Request",           // 事件类型
  timestamp: "2025-10-15T10:30:00.000Z",
  path: "/cubejs-api/v1/load",     // 请求路径
  userId: "user-id",               // 用户 ID
  dataSourceId: "datasource-id",   // 数据源 ID
  duration: 1234,                  // 执行时长 (ms)
  query: {...},                    // 查询对象
  queryKey: {...},                 // 查询缓存键
  sqlQuery: {...},                 // 生成的 SQL
  error: "error message"           // 错误信息 (可选)
}
```

#### Actions 读取和持久化

**文件**: `services/actions/src/rpc/saveLogs.js`

```javascript
export default async () => {
  // 1. 从 Redis Stream 读取所有日志
  let streamData = await redisClient.xread("STREAMS", streamName, 0);

  let data = streamData?.[0]?.[1];
  if (!data?.length) {
    return "No logs, skipped.";
  }

  // 2. 清理已读取的日志
  let lastId = data.pop()?.[0];
  lastId = parseInt(lastId) + 1;
  await redisClient.xtrim(streamName, "MINID", lastId);

  // 3. 解析日志数据
  data = data
    .map(([_, val]) => JSON.parse(val?.[1]))
    .filter((d) => d?.requestId);

  // 4. 聚合请求和事件
  const input = data.reduce((acc, event) => {
    let { requests, events } = acc;
    const curRequestId = event.requestId;

    // 聚合请求级别信息
    if (!requests?.[curRequestId]) {
      requests[curRequestId] = {
        request_id: curRequestId,
        start_time: timestamp,
        end_time: timestamp,
        user_id: event.userId,
        datasource_id: event.dataSourceId,
        path: event.path
      };
    }

    // 收集事件详情
    events.push({
      request_id: curRequestId,
      event: event?.event,
      duration: event?.duration,
      query: JSON.stringify(event.query),
      query_key: JSON.stringify(event?.queryKey),
      query_key_md5: createMd5Hex(queryKey),
      query_sql: event?.sqlQuery?.sql?.[0],
      timestamp,
      error: event?.error
    });

    return { requests, events };
  }, { requests: {}, events: [] });

  // 5. 插入 PostgreSQL
  await fetchGraphQL(createEventLogsMutation, input);
};
```

#### 数据库表结构

**request_logs**: 请求级别日志
```sql
- request_id (uuid, primary key)
- user_id (uuid)
- datasource_id (uuid)
- path (text)
- start_time (timestamp)
- end_time (timestamp)
```

**request_event_logs**: 事件级别日志
```sql
- request_id (uuid, foreign key)
- event (text)
- duration (integer)
- query (jsonb)
- query_key (jsonb)
- query_key_md5 (text)
- query_sql (text)
- timestamp (timestamp)
- error (text)
```

### 2. Alert 锁机制 (Distributed Locking)

**实现位置**: `services/actions/src/utils/alertLocks.js`

用于防止 Alert 定时任务并发执行,确保同一个 Alert 在同一时间只能有一个实例运行。

#### 锁的生命周期

```javascript
// 锁键格式
const getLockKey = (id) => `alert:${id}:lock`;

// 1. 获取锁数据
export const getLockData = async (alert) => {
  const { id, locks_config: locksConfig } = alert;
  const lockKey = getLockKey(id);

  // 优先从 Redis 读取
  const lockValue = await redisClient?.get(lockKey);

  if (lockValue) {
    const ttl = await redisClient.ttl(lockKey);
    return {
      key: lockKey,
      value: lockValue,
      ttl: ttl
    };
  }

  // 降级: 从数据库读取 (如果 Redis 不可用)
  const { value, expiresAt } = locksConfig;
  const isExpired = moment().tz(TIMEZONE).isAfter(expiresAt);

  if (isExpired) {
    return { key: null, value: null, ttl: null };
  }

  const ttl = moment().tz(TIMEZONE).diff(expiresAt, "seconds");
  return {
    key: lockKey,
    value: value,
    ttl: Math.abs(ttl)
  };
};

// 2. 设置锁 (带过期时间)
export const setLockData = async (alert, { value, ttl }) => {
  const { id } = alert;
  const lockKey = getLockKey(id);

  if (redisClient) {
    // Redis 实现: 使用原生 TTL
    await redisClient.set(lockKey, value, "EX", ttl);
    return;
  }

  // 降级: 写入数据库
  const expiresAt = moment().tz(TIMEZONE).add(ttl, "seconds").format();
  await fetchGraphQL(alertSetLockMutation, {
    id,
    locks_config: { value, expiresAt }
  });
};

// 3. 删除锁
export const delLockData = async (alert) => {
  const { id } = alert;
  const lockKey = getLockKey(id);

  if (redisClient) {
    await redisClient.del(lockKey);
    return;
  }

  // 降级: 清空数据库记录
  await fetchGraphQL(alertSetLockMutation, {
    id,
    locks_config: {}
  });
};
```

#### 锁的使用场景

**文件**: `services/actions/src/rpc/createCronTaskByAlert.js:89-92`

```javascript
// 当 Alert 配置更新时,清除旧锁
if (operationName === "UPDATE") {
  if (isNeedToClearLocks) {
    const lockKey = `alert:${id}:lock`;
    await redisClient.del(lockKey);
  }
  await deleteCronTaskByReport(session, deletionParams);
}
```

#### 优雅降级设计

| 场景 | Redis 可用 | Redis 不可用 |
|------|-----------|-------------|
| 存储方式 | Redis Key-Value + TTL | PostgreSQL jsonb 字段 |
| 过期机制 | Redis 自动过期 | 应用层检查 expiresAt |
| 性能 | 高 (内存读写) | 低 (数据库 I/O) |
| 可靠性 | 低 (重启丢失) | 高 (持久化) |

### 3. Cube.js 缓存层 (当前未使用)

**配置位置**: `services/cubejs/index.js:77-78`

```javascript
const options = {
  // ...
  externalDbType: "cubestore",
  externalDriverFactory,
  cacheAndQueueDriver: "cubestore",  // 使用 Cubestore 而非 Redis
  // ...
};
```

#### 说明

- Cube.js 支持 Redis 作为查询缓存和队列驱动
- Synmetrix 选择使用 **Cubestore** (分布式列存储) 替代 Redis
- Cubestore 提供更好的分析性能和数据持久化

#### 可选配置 (如果使用 Redis)

```javascript
// 如果要使用 Redis 作为缓存
const options = {
  cacheAndQueueDriver: "redis",
  redis: {
    createClient: () => new Redis(REDIS_ADDR)
  }
};
```

## 客户端实现

### 连接管理

**CubeJS**: `services/cubejs/src/utils/redis.js`
```javascript
import Redis from 'ioredis';

const { REDIS_ADDR } = process.env;

let redisClient = null;

if (REDIS_ADDR) {
  redisClient = new Redis(REDIS_ADDR);

  redisClient.on('error', (error) => {
    // 忽略连接拒绝错误,避免日志污染
    if (error?.code === 'ECONNREFUSED' || error?.code === 'EAI_AGAIN') return;
    console.error(error);
  })
}

export default redisClient;
```

**Actions**: `services/actions/src/utils/redis.js`
```javascript
import Redis from "ioredis";

const { REDIS_ADDR } = process.env;

let redisClient = null;

if (REDIS_ADDR) {
  redisClient = new Redis(REDIS_ADDR);
}

export default redisClient;
```

### 依赖包

**CubeJS** (`services/cubejs/package.json`):
```json
{
  "dependencies": {
    "ioredis": "^5.3.2",
    "redis": "^4.6.4"
  }
}
```

**Actions** (通过 ioredis):
```json
{
  "dependencies": {
    "ioredis": "^5.3.2"
  }
}
```

## 配置

### 环境变量

**`.env`**:
```bash
REDIS_ADDR=redis://redis:6379
```

### Docker Compose

**`docker-compose.dev.yml`**:
```yaml
services:
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data
    command: redis-server --appendonly yes
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5

  cubejs:
    environment:
      - REDIS_ADDR=redis://redis:6379
    depends_on:
      - redis

  actions:
    environment:
      - REDIS_ADDR=redis://redis:6379
    depends_on:
      - redis

volumes:
  redis-data:
```

## Redis Stream 特性

### 为什么使用 Redis Stream?

1. **天然的发布-订阅模型**: 适合日志传输场景
2. **持久化**: 支持 AOF,重启不丢失未消费数据
3. **消费组**: 支持多消费者 (虽然当前未使用)
4. **自动 ID 生成**: 时间戳 + 序列号,保证有序性
5. **范围查询**: 支持按 ID 范围读取和删除

### 关键命令

```bash
# 写入日志
XADD streams:cubejs-logs-stream * data '{"requestId":"..."}'

# 读取所有日志
XREAD STREAMS streams:cubejs-logs-stream 0

# 清理已消费日志
XTRIM streams:cubejs-logs-stream MINID 1697000000000

# 查看 Stream 长度
XLEN streams:cubejs-logs-stream

# 查看 Stream 信息
XINFO STREAM streams:cubejs-logs-stream
```

## 架构优势

### 1. 松耦合设计

```javascript
// 所有 Redis 操作都检查可用性
if (redisClient?.status !== "ready") {
  console.warn("Redis is disabled...");
  return;
}
```

### 2. 优雅降级

| 功能 | Redis 可用 | Redis 不可用 |
|------|-----------|-------------|
| 日志流 | 实时传输到 Actions | 仅控制台输出,无 UI 展示 |
| Alert 锁 | Redis TTL | PostgreSQL jsonb + 应用层过期 |
| 系统可用性 | 100% | 100% (功能降级) |

### 3. 异步处理

```
Request → CubeJS → Redis Stream (非阻塞)
                       ↓
                  Actions (定时消费)
                       ↓
                  PostgreSQL (持久化)
```

- CubeJS 不需要等待日志持久化
- 日志写入失败不影响查询请求
- Actions 可以批量处理日志 (提高吞吐)

## 监控和调试

### 查看日志流状态

```bash
# 连接到 Redis
docker exec -it synmetrix-redis-1 redis-cli

# 查看 Stream 长度
XLEN streams:cubejs-logs-stream

# 查看最近 10 条日志
XREVRANGE streams:cubejs-logs-stream + - COUNT 10

# 查看 Stream 详细信息
XINFO STREAM streams:cubejs-logs-stream
```

### 查看 Alert 锁

```bash
# 查看所有锁
KEYS alert:*:lock

# 查看特定锁
GET alert:550e8400-e29b-41d4-a716-446655440000:lock
TTL alert:550e8400-e29b-41d4-a716-446655440000:lock
```

### 性能监控

```bash
# 查看 Redis 统计信息
INFO stats

# 监控实时命令
MONITOR

# 查看慢日志
SLOWLOG GET 10
```

## 潜在改进

### 1. 日志消费组

当前实现使用简单的 XREAD + XTRIM,可以升级为消费组模式:

```javascript
// 创建消费组
await redisClient.xgroup("CREATE", streamName, "actions-group", "0", "MKSTREAM");

// 使用消费组读取
const logs = await redisClient.xreadgroup(
  "GROUP", "actions-group", "consumer-1",
  "COUNT", 100,
  "STREAMS", streamName, ">"
);

// 确认消费
await redisClient.xack(streamName, "actions-group", messageId);
```

优势:
- 支持多个 Actions 实例并发消费
- 自动跟踪消费进度
- 支持消息重试

### 2. 日志压缩

```javascript
// 写入时压缩
const compressed = zlib.gzipSync(JSON.stringify(data));
await redisClient.xadd(streamName, "*", "data", compressed.toString("base64"));

// 读取时解压
const decompressed = zlib.gunzipSync(Buffer.from(val, "base64"));
```

### 3. 分布式锁优化

使用 Redlock 算法实现更可靠的分布式锁:

```javascript
import Redlock from "redlock";

const redlock = new Redlock([redisClient], {
  driftFactor: 0.01,
  retryCount: 3,
  retryDelay: 200
});

const lock = await redlock.lock(lockKey, ttl * 1000);
try {
  // 执行 Alert 任务
} finally {
  await lock.unlock();
}
```

## 总结

### 设计理念

1. **可选依赖**: Redis 是增强功能,不是必需组件
2. **优雅降级**: Redis 不可用时系统仍可运行
3. **合理分工**:
   - Redis: 临时数据、实时传输、分布式协调
   - PostgreSQL: 持久化存储、关系查询
   - Cubestore: 分析缓存、预聚合

### 适用场景

| 场景 | 是否需要 Redis |
|------|---------------|
| 开发环境 | 可选 (方便调试) |
| 单机部署 | 可选 (性能提升有限) |
| 集群部署 | **推荐** (Alert 锁、日志聚合) |
| 高并发场景 | **必需** (分布式协调) |

### 关键文件清单

```
services/
├── cubejs/
│   ├── index.js                    # 配置: cacheAndQueueDriver
│   └── src/utils/
│       ├── redis.js                # Redis 客户端
│       └── logging.js              # 日志写入 Redis Stream
└── actions/
    ├── src/rpc/
    │   ├── saveLogs.js             # 从 Redis 读取日志并持久化
    │   └── createCronTaskByAlert.js # 清除 Alert 锁
    └── src/utils/
        ├── redis.js                # Redis 客户端
        └── alertLocks.js           # Alert 分布式锁实现
```
