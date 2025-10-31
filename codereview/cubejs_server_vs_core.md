# CubejsServer vs CubejsServerCore 关系详解

## 快速回答

**是的！** `@cubejs-backend/server` 包中的 `CubejsServer` 类**确实使用了** `CubejsServerCore`。

`CubejsServer` 是对 `CubejsServerCore` 的高层封装，提供了完整的 HTTP 服务器功能。

## 包结构关系

```
@cubejs-backend/server                 (用户直接使用的包)
    ↓ 依赖
@cubejs-backend/server-core            (核心逻辑包)
    ↓ 包含
CubejsServerCore                       (核心类)
```

## 代码关系

### CubejsServer 使用 CubejsServerCore

**文件**: `packages/cubejs-server/src/server.ts:3-9, 80, 84-86`

```typescript
import CubeCore, {
  CreateOptions as CoreCreateOptions,
  DatabaseType,
  DriverContext,
  DriverOptions,
  SystemOptions
} from '@cubejs-backend/server-core';

// ...

export class CubejsServer {
  protected readonly core: CubeCore;  // 持有 CubejsServerCore 实例

  public constructor(config: CreateOptions = {}, systemOptions?: SystemOptions) {
    // ... 配置处理
    this.core = this.createCoreInstance(this.config, systemOptions);
  }

  protected createCoreInstance(config: CreateOptions, systemOptions?: SystemOptions): CubeCore {
    return new CubeCore(config, systemOptions);  // 创建 CubejsServerCore 实例
  }
}
```

**关键点**:
- `CubeCore` 就是 `CubejsServerCore` 的别名（通过 `import` 重命名）
- `CubejsServer` 在构造函数中创建 `CubejsServerCore` 实例
- 通过 `this.core` 持有和使用 `CubejsServerCore`

## 两者的区别和职责

### CubejsServerCore (`@cubejs-backend/server-core`)

**职责**: 核心业务逻辑

```typescript
class CubejsServerCore {
  // ✅ 数据模型编译
  public async getCompilerApi(context: RequestContext)

  // ✅ 查询编排
  public async getOrchestratorApi(context: RequestContext)

  // ✅ 数据库驱动管理
  public async getDriver(context: DriverContext)

  // ✅ 定时刷新调度
  public handleScheduledRefreshInterval(options)

  // ✅ 初始化 API Gateway
  public async initApp(app: ExpressApplication)
}
```

**特点**:
- 💡 **纯逻辑层**：不涉及 HTTP 服务器
- 🔧 **可嵌入**：可以集成到任何 Node.js 应用
- 🎯 **核心功能**：编译、查询、缓存、刷新
- 📦 **轻量级**：只依赖核心组件

### CubejsServer (`@cubejs-backend/server`)

**职责**: 完整的 HTTP 服务器封装

```typescript
class CubejsServer {
  protected readonly core: CubeCore;  // 使用 CubejsServerCore

  // 🌐 HTTP 服务器
  public async listen(options): Promise<{app, port, server, version}>

  // 🔌 WebSocket 支持
  protected socketServer: WebSocketServer | null

  // 🗄️ SQL 接口
  protected sqlServer: SQLServer | null

  // 🛑 优雅关闭
  public async shutdown(signal: string, graceful: boolean)

  // 🔒 健康检查
  protected readonly status: ServerStatusHandler
}
```

**特点**:
- 🚀 **即用型服务器**：开箱即用的 HTTP 服务
- 🔌 **多协议支持**：HTTP, WebSocket, SQL
- 🛡️ **生产就绪**：优雅关闭、健康检查、超时控制
- ⚙️ **配置管理**：CORS, Body Parser, dotenv 加载

## 详细对比

| 维度 | CubejsServerCore | CubejsServer |
|------|------------------|--------------|
| **包名** | `@cubejs-backend/server-core` | `@cubejs-backend/server` |
| **定位** | 核心引擎 | 完整服务器 |
| **HTTP 服务器** | ❌ 需要自己创建 | ✅ 内置 HTTP 服务器 |
| **Express 集成** | ✅ 提供 `initApp(app)` | ✅ 自动创建和配置 |
| **WebSocket** | ❌ | ✅ 可选启用 |
| **SQL 接口** | ✅ 提供 `initSQLServer()` | ✅ 自动启动和管理 |
| **CORS** | ❌ | ✅ 自动配置 |
| **优雅关闭** | ✅ 基础支持 | ✅ 完整的优雅关闭流程 |
| **环境变量** | ⚠️ 部分支持 | ✅ 自动加载 `.env` |
| **健康检查** | ❌ | ✅ 内置状态管理 |
| **使用场景** | 嵌入现有应用 | 独立运行服务 |

## 使用示例对比

### 使用 CubejsServerCore（低层 API）

```typescript
import CubejsServerCore from '@cubejs-backend/server-core';
import express from 'express';
import http from 'http';

// 1. 创建 Express 应用
const app = express();

// 2. 创建 Cube 核心实例
const core = new CubejsServerCore({
  dbType: 'postgres',
  // ... 配置
});

// 3. 手动集成到 Express
await core.initApp(app);

// 4. 手动创建 HTTP 服务器
const server = http.createServer(app);

// 5. 手动启动监听
await server.listen(4000);

// 6. 手动处理关闭
process.on('SIGTERM', async () => {
  await core.releaseConnections();
  server.close();
});
```

**特点**:
- ✅ **灵活性高**：完全控制服务器配置
- ✅ **可嵌入**：可以集成到现有 Express 应用
- ⚠️ **需要手动管理**：CORS, body-parser, 优雅关闭等

### 使用 CubejsServer（高层 API）

```typescript
import CubejsServer from '@cubejs-backend/server';

// 1. 创建服务器（自动创建 core、express、http server）
const server = new CubejsServer({
  dbType: 'postgres',
  webSockets: true,
  sqlPort: 5432,
  // ... 配置
});

// 2. 一键启动（自动配置 CORS、body-parser、WebSocket 等）
const { app, port, server: httpServer } = await server.listen();
console.log(`🚀 Cube server listening on ${port}`);

// 3. 自动处理优雅关闭
process.on('SIGTERM', () => server.shutdown('SIGTERM'));
```

**特点**:
- ✅ **开箱即用**：自动配置所有必需组件
- ✅ **生产就绪**：内置最佳实践
- ⚠️ **灵活性较低**：部分配置被固化

## CubejsServer 如何使用 Core

### 1. 构造函数中创建 Core

**代码**: `packages/cubejs-server/src/server.ts:62-86`

```typescript
export class CubejsServer {
  protected readonly core: CubeCore;

  public constructor(config: CreateOptions = {}, systemOptions?: SystemOptions) {
    // 1. 合并配置
    this.config = {
      ...config,
      webSockets: config.webSockets || getEnv('webSockets'),
      sqlPort: config.sqlPort || getEnv('sqlPort'),
      // ... 其他配置
    };

    // 2. 创建 CubejsServerCore 实例
    this.core = this.createCoreInstance(this.config, systemOptions);
  }

  protected createCoreInstance(config: CreateOptions, systemOptions?: SystemOptions): CubeCore {
    // 实际调用 new CubejsServerCore()
    return new CubeCore(config, systemOptions);
  }
}
```

### 2. listen() 方法中集成 Core

**代码**: `packages/cubejs-server/src/server.ts:88-147`

```typescript
public async listen(options: http.ServerOptions = {}): Promise<{...}> {
  // 1. 创建 Express 应用
  const app = express();
  app.use(cors(this.config.http.cors));
  app.use(bodyParser.json({ limit: '50mb' }));

  // 2. 集成 Core 到 Express
  await this.core.initApp(app);  // ← 使用 Core

  // 3. 创建 HTTP 服务器
  this.server = gracefulHttp(http.createServer(options, app));

  // 4. 初始化 WebSocket（如果启用）
  if (this.config.webSockets) {
    this.socketServer = new WebSocketServer(this.core, this.config);  // ← 传递 Core
    this.socketServer.initServer(this.server);
  }

  // 5. 初始化 SQL 接口（如果启用）
  if (this.config.sqlPort || this.config.pgSqlPort) {
    this.sqlServer = this.core.initSQLServer();  // ← 使用 Core
    await this.sqlServer.init(this.config);
  }

  // 6. 启动监听
  const PORT = getEnv('port');
  await this.server.listen(PORT);

  return { app, port: PORT, server: this.server, version };
}
```

### 3. 代理方法到 Core

**代码**: `packages/cubejs-server/src/server.ts:149-166`

```typescript
// 测试数据库连接
public testConnections() {
  return this.core.testConnections();  // 代理到 Core
}

// 处理定时刷新
public handleScheduledRefreshInterval(options: any) {
  return this.core.handleScheduledRefreshInterval(options);  // 代理到 Core
}

// 运行定时刷新
public runScheduledRefresh(context: any, queryingOptions: any) {
  return this.core.runScheduledRefresh(context, queryingOptions);  // 代理到 Core
}

// 获取驱动
public async getDriver(ctx: DriverContext): Promise<BaseDriver> {
  return this.core.getDriver(ctx);  // 代理到 Core
}
```

### 4. 优雅关闭流程

**代码**: `packages/cubejs-server/src/server.ts:218-279`

```typescript
public async shutdown(signal: string, graceful: boolean = true) {
  // 1. 设置超时杀手
  const timeoutKiller = withTimeout(
    () => process.exit(1),
    ((this.config.gracefulShutdown || 2) + 1) * 1000,
  );

  // 2. 标记服务器状态为关闭中
  this.status.shutdown();

  // 3. 收集所有需要关闭的组件
  const locks: Promise<any>[] = [
    this.core.beforeShutdown()  // ← 调用 Core 的 beforeShutdown
  ];

  if (this.socketServer) {
    locks.push(this.socketServer.close());
  }

  if (this.sqlServer) {
    locks.push(this.sqlServer.shutdown(graceful && (signal === 'SIGTERM') ? 'semifast' : 'fast'));
  }

  if (this.server) {
    locks.push(this.server.stop((this.config.gracefulShutdown || 2) * 1000));
  }

  // 4. 执行关闭流程
  const shutdownAll = async () => {
    try {
      if (graceful) {
        await Promise.all(locks);  // 等待所有组件停止
      }
      await this.core.shutdown();  // ← 最后调用 Core 的 shutdown
    } finally {
      timeoutKiller.cancel();
    }
  };

  await Promise.any([shutdownAll(), timeoutKiller]);
  return 0;
}
```

## 调用链示例

### HTTP 请求处理流程

```
用户请求
    ↓
HTTP Server (CubejsServer.server)
    ↓
Express App (express)
    ↓
API Gateway (CubejsServerCore.apiGateway)
    ↓
Compiler API (CubejsServerCore.getCompilerApi)
    ↓
Orchestrator API (CubejsServerCore.getOrchestratorApi)
    ↓
Query Execution
    ↓
Database Driver (CubejsServerCore.getDriver)
    ↓
返回结果
```

### WebSocket 请求流程

```
WebSocket 连接
    ↓
WebSocketServer (CubejsServer.socketServer)
    ↓
Core 引用 (传递给 WebSocketServer)
    ↓
API Gateway Subscription (CubejsServerCore.initSubscriptionServer)
    ↓
实时查询处理
```

### SQL 查询流程

```
SQL 客户端连接
    ↓
SQL Server (CubejsServer.sqlServer)
    ↓
Core SQL Server (CubejsServerCore.initSQLServer)
    ↓
SQL 编译和执行
    ↓
返回结果集
```

## 配置选项继承

### CubejsServer 扩展的配置

```typescript
export interface CreateOptions extends CoreCreateOptions, WebSocketServerOptions, SQLServerOptions {
  // CubejsServer 特有的配置
  webSockets?: boolean;              // WebSocket 支持
  http?: HttpOptions;                // HTTP 配置（CORS 等）
  gracefulShutdown?: number;         // 优雅关闭超时（秒）
  serverKeepAliveTimeout?: number;   // Keep-Alive 超时
  serverHeadersTimeout?: number;     // Headers 超时
}
```

### 配置传递示例

```typescript
const server = new CubejsServer({
  // ===== CubejsServerCore 的配置 =====
  dbType: 'postgres',
  externalDbType: 'cubestore',
  cacheAndQueueDriver: 'cubestore',
  apiSecret: 'secret',
  // ... 所有 CoreCreateOptions

  // ===== CubejsServer 额外的配置 =====
  webSockets: true,              // 启用 WebSocket
  sqlPort: 5432,                 // SQL 接口端口
  gracefulShutdown: 5,           // 5 秒优雅关闭
  http: {
    cors: {
      origin: '*',
      allowedHeaders: 'custom-header,authorization'
    }
  }
});
```

## 静态方法委托

**代码**: `packages/cubejs-server/src/server.ts:202-216`

```typescript
export class CubejsServer {
  // 委托到 CubejsServerCore 的静态方法
  public static createDriver(dbType: DatabaseType, opt: DriverOptions) {
    return CubeCore.createDriver(dbType, opt);
  }

  public static driverDependencies(dbType: DatabaseType) {
    return CubeCore.driverDependencies(dbType);
  }

  // CubejsServer 自己的静态方法
  public static apiSecret() {
    return process.env.CUBEJS_API_SECRET;
  }

  public static version() {
    return version;  // 来自 @cubejs-backend/server 的 package.json
  }
}
```

## 环境变量加载

### CubejsServer 自动加载 .env

**代码**: `packages/cubejs-server/src/server.ts:1, 27-29`

```typescript
import dotenv from '@cubejs-backend/dotenv';

// 在模块加载时自动执行
dotenv.config({
  multiline: 'line-breaks',
});
```

**效果**:
- ✅ 自动读取 `.env` 文件
- ✅ 支持多行值（使用 `\n` 作为换行符）
- ✅ 在 `CubejsServerCore` 初始化之前完成

### CubejsServerCore 不自动加载

- ⚠️ 需要手动加载 `.env` 或通过代码传递配置
- ✅ 更适合嵌入式场景（避免副作用）

## 依赖关系

### package.json 依赖

**`@cubejs-backend/server` 的 package.json**:

```json
{
  "dependencies": {
    "@cubejs-backend/server-core": "1.5.0",  // ← 依赖 Core
    "@cubejs-backend/cubestore-driver": "1.5.0",
    "@cubejs-backend/dotenv": "^9.0.2",
    "@cubejs-backend/shared": "1.5.0",
    "express": "^4.21.1",
    "cors": "^2.8.4",
    "body-parser": "^1.19.0",
    "ws": "^7.1.2",
    // ... 其他依赖
  }
}
```

**`@cubejs-backend/server-core` 的 package.json**:

```json
{
  "dependencies": {
    "@cubejs-backend/api-gateway": "1.5.0",
    "@cubejs-backend/query-orchestrator": "1.5.0",
    "@cubejs-backend/schema-compiler": "1.5.0",
    "@cubejs-backend/shared": "1.5.0",
    // 不依赖 express, http, websocket 等
  }
}
```

## 使用场景选择

### 选择 CubejsServerCore

✅ **适用场景**:
- 需要嵌入到现有 Express/Koa/Fastify 应用
- 需要完全控制 HTTP 服务器配置
- 微服务架构中的一个模块
- 自定义的服务器框架

❌ **不适用**:
- 独立运行的 Cube 服务
- 需要快速开发原型
- 标准的 Cube 部署

### 选择 CubejsServer

✅ **适用场景**:
- 独立运行的 Cube 服务（最常见）
- 快速开发和原型验证
- 标准的生产部署
- 使用 Docker/Kubernetes 部署

❌ **不适用**:
- 需要深度定制服务器行为
- 已有复杂的 Express 应用需要集成

## 实际使用统计

### 官方推荐

**官方文档和示例都使用 `CubejsServer`**:

```typescript
// 官方推荐方式
const CubejsServer = require('@cubejs-backend/server');

const server = new CubejsServer();

server.listen().then(({ version, port }) => {
  console.log(`🚀 Cube.js server (${version}) is listening on ${port}`);
});
```

### CLI 工具使用

**`cubejs-server` CLI**: `packages/cubejs-server/bin/server`

```bash
#!/usr/bin/env node

# 实际执行的是 CubejsServer，而不是 CubejsServerCore
```

### Docker 镜像使用

**官方 Docker 镜像**: `cubejs/cube`

```dockerfile
# 内部使用 @cubejs-backend/server
CMD ["node", "index.js"]
# index.js 使用 CubejsServer.listen()
```

## 总结

### 关系总结

```
CubejsServer (高层封装)
    ├── 创建和持有 CubejsServerCore 实例
    ├── 提供 HTTP 服务器功能
    ├── 管理 WebSocket 和 SQL 接口
    ├── 处理优雅关闭和健康检查
    └── 自动加载环境变量

CubejsServerCore (核心引擎)
    ├── 数据模型编译
    ├── 查询编排和缓存
    ├── 数据库驱动管理
    ├── 定时刷新调度
    └── API Gateway 集成
```

### 设计模式

1. **门面模式** (Facade): `CubejsServer` 为 `CubejsServerCore` 提供简化的接口
2. **委托模式** (Delegation): `CubejsServer` 将核心逻辑委托给 `CubejsServerCore`
3. **适配器模式** (Adapter): `CubejsServer` 适配 HTTP/WebSocket/SQL 协议到核心 API

### 架构分层

```
┌─────────────────────────────────────┐
│  Application Layer                  │  ← 用户代码
├─────────────────────────────────────┤
│  CubejsServer                       │  ← 服务器层（@cubejs-backend/server）
│  - HTTP Server                      │
│  - WebSocket                        │
│  - SQL Interface                    │
│  - Graceful Shutdown                │
├─────────────────────────────────────┤
│  CubejsServerCore                   │  ← 核心逻辑层（@cubejs-backend/server-core）
│  - Compiler API                     │
│  - Orchestrator API                 │
│  - Driver Management                │
│  - Refresh Scheduler                │
├─────────────────────────────────────┤
│  Infrastructure Layer               │  ← 基础设施层
│  - Query Orchestrator               │  (@cubejs-backend/query-orchestrator)
│  - Schema Compiler                  │  (@cubejs-backend/schema-compiler)
│  - API Gateway                      │  (@cubejs-backend/api-gateway)
│  - Database Drivers                 │  (@cubejs-backend/*-driver)
└─────────────────────────────────────┘
```

### 关键要点

1. ✅ **`CubejsServer` 确实使用了 `CubejsServerCore`**
2. 🏗️ **组合关系**: `CubejsServer` 通过 `this.core` 持有 `CubejsServerCore` 实例
3. 🎯 **职责分离**: Server 负责网络层，Core 负责业务逻辑
4. 📦 **包设计**: 两个独立的 npm 包，清晰的依赖关系
5. 🚀 **用户选择**: 大多数用户使用 `CubejsServer`，高级用户可以直接使用 `CubejsServerCore`

---

*文档生成时间: 2025-10-31*
*基于 Cube.js 版本: v1.5.0*
*分析文件:
  - packages/cubejs-server/src/server.ts
  - packages/cubejs-server-core/src/core/server.ts*
