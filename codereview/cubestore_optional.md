# Cubestore 是否可以不使用？

## 简短答案

**可以！** Cubestore 在 Synmetrix 中是**可选的**，可以完全移除或替换为其他缓存方案（如 Redis）。如果你愿意接受数据库查询压力，完全可以让查询直接下推到源数据库。

## 深入分析

### Cubestore 在 Synmetrix 中的角色

从代码分析来看，Cubestore 在 Synmetrix 中承担三个主要职责：

```javascript
// services/cubejs/index.js:76-78
const options = {
  // ...
  externalDbType: "cubestore",          // 1. 预聚合存储
  externalDriverFactory,                // 2. 预聚合查询引擎
  cacheAndQueueDriver: "cubestore",     // 3. 查询缓存和队列管理
  // ...
};
```

#### 1. **预聚合存储** (`externalDbType`)
- 存储预先计算的聚合表
- 例如：按天汇总的订单数据

#### 2. **预聚合查询** (`externalDriverFactory`)
- 从预聚合表读取数据
- 避免重复计算

#### 3. **缓存和队列** (`cacheAndQueueDriver`)
- 查询结果缓存
- 查询队列管理
- 预聚合构建协调

### 不使用 Cubestore 的配置方案

#### 方案 1: 完全禁用缓存和预聚合（最简单）

```javascript
// services/cubejs/index.js 修改
const options = {
  queryRewrite,
  contextToAppId,
  contextToOrchestratorId,
  dbType,
  devServer: false,
  checkAuth,
  apiSecret: CUBEJS_SECRET,
  basePath,
  schemaVersion,
  driverFactory,
  repositoryFactory,
  preAggregationsSchema,
  telemetry: CUBEJS_TELEMETRY,

  // 禁用定时刷新
  scheduledRefreshTimer: undefined,

  // 移除 Cubestore 相关配置
  // externalDbType: "cubestore",     // ← 删除或注释
  // externalDriverFactory,           // ← 删除或注释
  // cacheAndQueueDriver: "cubestore", // ← 删除或注释

  logger: logging,

  // SQL API 配置
  pgSqlPort: parseInt(CUBEJS_PG_SQL_PORT, 10),
  sqlPort: parseInt(CUBEJS_SQL_PORT, 10),
  canSwitchSqlUser: () => false,
  checkSqlAuth,
};

const cubejs = new ServerCore(options);
```

**效果**：
- ✓ 所有查询直接访问源数据库
- ✓ 无需 Cubestore 容器
- ✓ 架构更简单
- ✗ 每次查询都要重新计算
- ✗ 无查询结果缓存
- ✗ 大数据量场景下性能差

**Docker Compose 修改**：
```yaml
# docker-compose.dev.yml

# 直接删除 cubestore 服务定义
# cubestore:
#   image: cubejs/cubestore:${CUBESTORE_VERSION}
#   ...

# cubejs 服务中移除环境变量
cubejs:
  build:
    context: ./services/cubejs
  # ...
  # 不再需要这些环境变量：
  # - CUBEJS_CUBESTORE_HOST
  # - CUBEJS_CUBESTORE_PORT
```

#### 方案 2: 使用 Redis 替代（推荐）

Cube.js 原生支持使用 Redis 作为缓存和队列驱动。

```javascript
// services/cubejs/index.js 修改
import Redis from 'ioredis';

const { REDIS_ADDR = 'redis://redis:6379' } = process.env;

const options = {
  queryRewrite,
  contextToAppId,
  contextToOrchestratorId,
  dbType,
  devServer: false,
  checkAuth,
  apiSecret: CUBEJS_SECRET,
  basePath,
  schemaVersion,
  driverFactory,
  repositoryFactory,
  telemetry: CUBEJS_TELEMETRY,

  // 使用 Redis 替代 Cubestore
  cacheAndQueueDriver: 'redis',
  redisPoolOptions: {
    createClient: () => new Redis(REDIS_ADDR),
  },

  // 禁用外部预聚合存储
  // 预聚合将存储在源数据库中
  // externalDbType: undefined,
  // externalDriverFactory: undefined,

  logger: logging,

  // SQL API 配置
  pgSqlPort: parseInt(CUBEJS_PG_SQL_PORT, 10),
  sqlPort: parseInt(CUBEJS_SQL_PORT, 10),
  canSwitchSqlUser: () => false,
  checkSqlAuth,
};

const cubejs = new ServerCore(options);
```

**效果**：
- ✓ 查询结果缓存（存储在 Redis）
- ✓ 查询队列管理
- ✓ 多实例协调
- ✓ Redis 已经在架构中，无需额外组件
- ✗ 预聚合仍需要计算（但会缓存结果）
- ✗ Redis 内存有限，不适合大规模预聚合

**Docker Compose 修改**：
```yaml
# docker-compose.dev.yml

# 保留 redis 服务
redis:
  image: redis:7.0.0
  restart: always
  ports:
    - 6379:6379
  networks:
    - synmetrix_default

# 删除 cubestore 服务
# cubestore:
#   image: cubejs/cubestore:${CUBESTORE_VERSION}
#   ...

cubejs:
  build:
    context: ./services/cubejs
  # ...
  environment:
    - REDIS_ADDR=redis://redis:6379
  depends_on:
    - redis  # 依赖 Redis
```

#### 方案 3: 使用源数据库存储预聚合（部分场景可行）

如果源数据库性能足够好，可以将预聚合表存储在源数据库中。

```javascript
// services/cubejs/index.js 修改
const options = {
  queryRewrite,
  contextToAppId,
  contextToOrchestratorId,
  dbType,
  devServer: false,
  checkAuth,
  apiSecret: CUBEJS_SECRET,
  basePath,
  schemaVersion,
  driverFactory,
  repositoryFactory,
  preAggregationsSchema,  // 预聚合 schema 名称
  telemetry: CUBEJS_TELEMETRY,

  // 使用 Redis 做缓存和队列
  cacheAndQueueDriver: 'redis',
  redisPoolOptions: {
    createClient: () => new Redis(REDIS_ADDR),
  },

  // 预聚合存储在源数据库中
  externalDbType: undefined,  // 不使用外部存储
  externalDriverFactory: undefined,

  // 定时刷新预聚合（可选）
  scheduledRefreshTimer: parseInt(CUBEJS_REFRESH_TIMER, 10),
  scheduledRefreshContexts,

  logger: logging,
  pgSqlPort: parseInt(CUBEJS_PG_SQL_PORT, 10),
  sqlPort: parseInt(CUBEJS_SQL_PORT, 10),
  canSwitchSqlUser: () => false,
  checkSqlAuth,
};
```

**工作原理**：
```
查询请求
    ↓
Cube.js 检查预聚合
    ↓
    ├─→ 预聚合表存在
    │   └─→ 从源数据库的 pre_aggregations_* schema 查询
    │
    └─→ 预聚合表不存在
        └─→ 查询源数据库原始表
```

**效果**：
- ✓ 支持预聚合（存储在源数据库）
- ✓ 查询结果缓存（Redis）
- ✓ 队列管理（Redis）
- ✗ 预聚合表占用源数据库空间
- ✗ 源数据库负载增加
- ⚠️  仅适用于支持的数据库（PostgreSQL、MySQL 等）

**注意事项**：
- 不是所有数据库都适合存储预聚合（如 BigQuery 成本高）
- 需要在源数据库创建 `pre_aggregations_*` schema 的权限

### 不同方案对比

| 方案 | 查询缓存 | 预聚合 | 队列管理 | 额外组件 | 性能 | 复杂度 |
|------|---------|--------|---------|---------|------|--------|
| **原始（Cubestore）** | ✓ | ✓ | ✓ | Cubestore | 最高 | 中 |
| **方案 1：完全禁用** | ✗ | ✗ | ✗ | 无 | 最低 | 最低 |
| **方案 2：仅 Redis** | ✓ | ✗ | ✓ | Redis | 中 | 低 |
| **方案 3：源库预聚合** | ✓ | ✓ | ✓ | Redis | 高 | 中 |

### 性能影响分析

#### 场景 1: 简单查询（几百万行数据）

**使用 Cubestore**:
```
首次查询: 5 秒（查源库 + 缓存）
后续查询: 0.1 秒（从 Cubestore 缓存读取）
```

**不使用 Cubestore（方案 1）**:
```
每次查询: 5 秒（每次都查源库）
```

**使用 Redis（方案 2）**:
```
首次查询: 5 秒（查源库 + 缓存到 Redis）
后续查询: 0.2 秒（从 Redis 读取）
```

**性能差距**: 中等场景下差异不大

#### 场景 2: 复杂聚合（数亿行数据）

**使用 Cubestore + 预聚合**:
```
预聚合构建: 一次性 300 秒（后台构建）
查询性能: 0.1 秒（查询预聚合表）
```

**不使用 Cubestore（方案 1）**:
```
每次查询: 120-300 秒（全表扫描 + 聚合）
无法使用: 查询超时
```

**使用源库预聚合（方案 3）**:
```
预聚合构建: 一次性 300 秒（在源库构建）
查询性能: 0.5-1 秒（查询源库的预聚合表）
源库压力: 增加（存储 + 查询）
```

**性能差距**: 大数据场景下差异巨大

#### 场景 3: 高并发查询（100+ QPS）

**使用 Cubestore**:
```
缓存命中率: 80%
平均响应时间: 0.5 秒
源库压力: 20 QPS
```

**不使用 Cubestore（方案 1）**:
```
缓存命中率: 0%
平均响应时间: 5-10 秒
源库压力: 100 QPS（可能压垮源库）
```

**使用 Redis（方案 2）**:
```
缓存命中率: 70%
平均响应时间: 1 秒
源库压力: 30 QPS
```

**性能差距**: 高并发场景下有明显差异

### 推荐决策树

```
你的场景是什么？
    │
    ├─→ 数据量小（< 1000 万行）
    │   且并发低（< 10 QPS）
    │   └─→ 方案 1：完全禁用
    │       简单，无需额外组件
    │
    ├─→ 数据量中等（1000 万 - 1 亿行）
    │   且并发中等（10-50 QPS）
    │   └─→ 方案 2：仅使用 Redis
    │       平衡性能和复杂度
    │
    ├─→ 数据量大（> 1 亿行）
    │   或并发高（> 50 QPS）
    │   ├─→ 源数据库性能好（如 ClickHouse）
    │   │   └─→ 方案 3：源库预聚合
    │   │
    │   └─→ 源数据库性能一般（如 PostgreSQL）
    │       └─→ 保留 Cubestore（原始方案）
    │           最佳性能
    │
    └─→ 实时性要求高（秒级更新）
        └─→ 方案 1 或 方案 2
            预聚合不适合实时场景
```

### 实际修改步骤（方案 2: 使用 Redis）

#### 步骤 1: 修改 Cube.js 配置

```javascript
// services/cubejs/index.js

import ServerCore from "@cubejs-backend/server-core";
import express from "express";
import Redis from "ioredis";  // 已在 package.json 中

// ... 其他导入

const {
  CUBEJS_SECRET,
  CUBEJS_SQL_PORT,
  CUBEJS_PG_SQL_PORT,
  REDIS_ADDR = 'redis://redis:6379',  // Redis 地址
  CUBEJS_TELEMETRY = false,
  CUBEJS_SCHEDULED_REFRESH = false,  // 禁用预聚合刷新
  CUBEJS_SQL_API = true,
} = process.env;

const port = parseInt(process.env.PORT, 10) || 4000;
const app = express();

app.use(express.json({ limit: "50mb", extended: true }));
app.use(express.urlencoded({ limit: "50mb", extended: true }));

const dbType = ({ securityContext }) =>
  securityContext?.userScope?.dataSource?.dbType || "none";

const contextToOrchestratorId = ({ securityContext }) =>
  `CUBEJS_APP_${securityContext?.userScope?.dataSource?.dataSourceVersion}_${securityContext?.userScope?.dataSource?.schemaVersion}`;

const contextToAppId = ({ securityContext }) =>
  `CUBEJS_APP_${securityContext?.userScope?.dataSource?.dataSourceVersion}_${securityContext?.userScope?.dataSource?.schemaVersion}`;

const schemaVersion = ({ securityContext }) =>
  securityContext?.userScope?.dataSource?.schemaVersion;

// 预聚合 schema（可选，如果不用预聚合可删除）
// const preAggregationsSchema = ({ securityContext }) =>
//   `pre_aggregations_${securityContext?.userScope?.dataSource?.preAggregationSchema}`;

const basePath = `/api`;

const options = {
  queryRewrite,
  contextToAppId,
  contextToOrchestratorId,
  dbType,
  devServer: false,
  checkAuth,
  apiSecret: CUBEJS_SECRET,
  basePath,
  schemaVersion,
  driverFactory,
  repositoryFactory,
  // preAggregationsSchema,  // 禁用预聚合
  telemetry: CUBEJS_TELEMETRY,

  // 禁用定时刷新
  scheduledRefreshTimer: undefined,

  // 使用 Redis 作为缓存和队列驱动
  cacheAndQueueDriver: 'redis',
  redisPoolOptions: {
    createClient: () => new Redis(REDIS_ADDR),
  },

  // 移除 Cubestore 配置
  // externalDbType: "cubestore",
  // externalDriverFactory,

  logger: logging,

  // sql server
  pgSqlPort: parseInt(CUBEJS_PG_SQL_PORT, 10),
  sqlPort: parseInt(CUBEJS_SQL_PORT, 10),
  canSwitchSqlUser: () => false,
  checkSqlAuth,
};

const cubejs = new ServerCore(options);

const file = fs.readFileSync("./src/swagger.yaml", "utf8");
const swaggerDocument = YAML.parse(file);

app.use("/docs", swaggerUi.serve, swaggerUi.setup(swaggerDocument));

app.use(routes({ basePath, cubejs }));

cubejs.initApp(app);

if (String(CUBEJS_SQL_API) === "true") {
  const sqlServer = cubejs.initSQLServer();
  sqlServer.init(options);
}

app.use((err, req, res, next) => {
  console.error(err.stack);
  res.status(500).send(err.message);
});

app.listen(port);
```

#### 步骤 2: 修改 Docker Compose

```yaml
# docker-compose.dev.yml

version: '3.8'

networks:
  synmetrix_default:
    external: true

services:
  redis:
    image: redis:7.0.0
    restart: always
    ports:
      - 6379:6379
    networks:
      - synmetrix_default

  postgres:
    image: postgres:${POSTGRES_VERSION}
    restart: always
    ports:
      - 5435:5432
    volumes:
      - pgstorage-data:/var/lib/postgresql/data
    env_file:
      - .env
      - .dev.env
    networks:
      - synmetrix_default

  cubejs:
    build:
      context: ./services/cubejs
    restart: always
    command: yarn start.dev
    volumes:
      - ./services/cubejs/src:/app/src
      - ./services/cubejs/index.js:/app/index.js
    ports:
      - 4000:4000
      - 9231:9229
      - 13306:13306
      - 15432:15432
    env_file:
      - .env
      - .dev.env
    environment:
      CUBEJS_SCHEDULED_REFRESH: false
      REDIS_ADDR: redis://redis:6379
    depends_on:
      - redis
    networks:
      - synmetrix_default

  # 删除 cubestore 服务
  # cubestore:
  #   image: cubejs/cubestore:${CUBESTORE_VERSION}
  #   ...

  # 其他服务保持不变
  # ...

volumes:
  pgstorage-data:
```

#### 步骤 3: 修改环境变量

```bash
# .env

# 删除或注释 Cubestore 相关配置
# CUBESTORE_VERSION=v1.2.3
# CUBEJS_CUBESTORE_HOST=cubestore
# CUBEJS_CUBESTORE_PORT=3030

# 确保 Redis 配置存在
REDIS_ADDR=redis://redis:6379

# 禁用预聚合刷新（可选）
CUBEJS_SCHEDULED_REFRESH=false
```

#### 步骤 4: 测试

```bash
# 停止并删除旧容器
docker-compose down

# 启动新配置
docker-compose up -d

# 查看日志
docker-compose logs -f cubejs

# 测试查询
curl -H "Authorization: Bearer <token>" \
     -H "x-hasura-datasource-id: <uuid>" \
     http://localhost:4000/api/v1/load \
     -d '{"query": {"measures": ["Orders.count"]}}'
```

### 注意事项和权衡

#### 优势（不使用 Cubestore）

1. **架构更简单**
   - 少一个组件维护
   - 少一个故障点
   - 部署更容易

2. **资源消耗更低**
   - 不需要 Cubestore 的 CPU/内存
   - 不需要额外的存储空间

3. **成本更低**
   - 云环境下节省计算资源费用
   - 减少存储成本

4. **适合小规模场景**
   - 数据量不大
   - 并发不高
   - 实时性要求高

#### 劣势（不使用 Cubestore）

1. **性能显著下降**
   - 每次查询都访问源数据库
   - 大数据量场景下查询慢
   - 高并发场景下源库压力大

2. **无法使用高级功能**
   - 预聚合（Pre-aggregations）
   - 分布式查询
   - 高效的列式存储

3. **扩展性受限**
   - 单点查询性能瓶颈
   - 难以应对突发流量
   - 多实例缓存不共享（如果不用 Redis）

4. **用户体验差**
   - 查询响应慢
   - 可能超时
   - 高并发时更明显

### 混合方案（灵活配置）

你也可以让 Cubestore 成为**可选**配置，根据环境变量决定是否使用：

```javascript
// services/cubejs/index.js

const USE_CUBESTORE = process.env.USE_CUBESTORE === 'true';
const REDIS_ADDR = process.env.REDIS_ADDR || 'redis://redis:6379';

let cacheAndQueueConfig = {};
let externalStorageConfig = {};

if (USE_CUBESTORE) {
  // 使用 Cubestore
  const externalDriverFactory = async () =>
    ServerCore.createDriver("cubestore", {
      host: process.env.CUBEJS_CUBESTORE_HOST,
      port: process.env.CUBEJS_CUBESTORE_PORT,
    });

  externalStorageConfig = {
    externalDbType: "cubestore",
    externalDriverFactory,
    preAggregationsSchema,
  };

  cacheAndQueueConfig = {
    cacheAndQueueDriver: "cubestore",
  };
} else {
  // 使用 Redis
  cacheAndQueueConfig = {
    cacheAndQueueDriver: 'redis',
    redisPoolOptions: {
      createClient: () => new Redis(REDIS_ADDR),
    },
  };
}

const options = {
  queryRewrite,
  contextToAppId,
  contextToOrchestratorId,
  dbType,
  checkAuth,
  apiSecret: CUBEJS_SECRET,
  basePath,
  schemaVersion,
  driverFactory,
  repositoryFactory,
  telemetry: CUBEJS_TELEMETRY,
  logger: logging,

  // 动态配置
  ...cacheAndQueueConfig,
  ...externalStorageConfig,

  // SQL API
  pgSqlPort: parseInt(CUBEJS_PG_SQL_PORT, 10),
  sqlPort: parseInt(CUBEJS_SQL_PORT, 10),
  canSwitchSqlUser: () => false,
  checkSqlAuth,
};
```

**环境变量控制**：
```bash
# 使用 Cubestore
USE_CUBESTORE=true docker-compose up

# 不使用 Cubestore
USE_CUBESTORE=false docker-compose up
```

### 结论

**可以不使用 Cubestore**，具体取决于你的场景：

- **小规模、低并发、实时性高** → 方案 1（完全禁用）或方案 2（仅 Redis）
- **中等规模、中等并发** → 方案 2（仅 Redis）
- **大规模、高并发、分析型** → 保留 Cubestore（原始方案）

**推荐做法**：
1. 先尝试方案 2（使用 Redis 替代）
2. 在实际负载下测试性能
3. 如果性能满足需求，就不需要 Cubestore
4. 如果性能不够，再引入 Cubestore

这样既保持了架构的灵活性，又能根据实际需求优化成本和性能。
