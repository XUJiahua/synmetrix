# Synmetrix 相比原生 Cube.js 的优势分析

## 概述

Synmetrix (原 MLCraft) 是一个基于 Cube.js 构建的开源数据工程平台和语义层,用于集中化指标管理。本文档详细分析了 Synmetrix 相比原生 Cube.js 的核心优势和创新功能。

## 🎯 核心优势

### 1. **完整的企业级平台架构**

Synmetrix 不仅仅是 Cube.js,而是构建在 Cube.js 之上的完整数据工程平台:

- **多服务架构**:
  - `cubejs` - 核心分析引擎 (端口 4000)
  - `actions` - 业务逻辑服务 (端口 3000)
  - `hasura` - GraphQL API 层 (端口 8080)
  - `client` - 前端 Web 界面 (端口 80)
  - `hasura_plus` - 扩展功能 (端口 8081)

- **统一的语义层**: 整合多数据源的指标管理
- **开箱即用**: 包含前端 UI、GraphQL API、认证系统等完整组件

### 2. **动态 Schema 管理** ⭐

这是 Synmetrix 最核心的创新功能:

#### 原生 Cube.js 的局限:
- Schema 文件存储在文件系统中
- 修改 Schema 需要重新部署应用
- 版本控制完全依赖 Git
- 多租户场景下 Schema 管理困难

#### Synmetrix 的创新:
- Schema **存储在 PostgreSQL 数据库**中
- 通过 `repositoryFactory` 动态加载 Schema (`services/cubejs/src/utils/repositoryFactory.js:16-20`)
- 通过 GraphQL API 实时更新 Schema
- **无需重启服务**即可应用 Schema 变更
- 内置版本控制和分支管理系统

```javascript
// repositoryFactory.js
const repositoryFactory = ({ securityContext }) => {
  return {
    dataSchemaFiles: async () => {
      const ids = securityContext?.userScope?.dataSource?.files;
      const dataSchemas = await findDataSchemasByIds({ ids });
      return dataSchemas.map(mapSchemaToFile);
    },
  };
};
```

**优势**:
- 支持在线编辑和即时生效
- 无需 Git 仓库和 CI/CD 流程
- 适合非技术用户进行数据建模
- 支持多环境 Schema 隔离

### 3. **自动化 Schema 生成** 🤖

Synmetrix 提供了强大的自动化 Schema 生成能力:

#### 功能特性:
- **一键生成**: 通过 `/cubejs/generateDataSchema` API 自动从数据库生成 Cube.js Schema
- **智能推断**: 自动识别表结构、字段类型、维度和度量
- **支持覆盖/合并**: 可选择覆盖已有 Schema 或增量添加新表
- **多种格式**: 支持 YAML 和 JavaScript 格式

#### 实现位置:
- RPC 入口: `services/actions/src/rpc/genSchemas.js`
- 核心逻辑: `services/cubejs/src/routes/generateDataSchema.js`

```javascript
// generateDataSchema.js 关键代码
const scaffoldingTemplate = new ScaffoldingTemplate(
  normalizedSchema,
  driver,
  { format }
);

const newFiles = scaffoldingTemplate.generateFilesByTableNames(normalizedTables);

if (overwrite) {
  files = filterFiles(newFiles, existedFiles);
} else {
  files = filterFiles(existedFiles, newFiles);
}
```

**优势**:
- 大幅降低数据建模门槛
- 减少手动编写 Schema 的时间
- 自动适配不同数据库的 Schema 结构
- 支持增量更新,不会覆盖已有的自定义逻辑

### 4. **高级多租户隔离** 🏢

原生 Cube.js 的多租户支持较为基础,Synmetrix 实现了**完全隔离的多租户架构**:

#### 隔离机制:

```javascript
// buildSecurityContext.js
const buildSecurityContext = (dataSource, branch, version) => {
  const dataSourceVersion = JSum.digest(data, "SHA256", "hex");
  const schemaVersion = createMd5Hex(files);
  const preAggregationSchema = createMd5Hex(data.dataSourceId);

  return {
    dataSourceVersion,      // 数据源版本隔离
    preAggregationSchema,   // 预聚合隔离
    schemaVersion,          // Schema 版本隔离
    files,                  // 文件隔离
  };
};
```

#### 隔离层级:
1. **Orchestrator 隔离**: `contextToOrchestratorId` - 每个数据源独立的查询编排器
2. **App 上下文隔离**: `contextToAppId` - 隔离的 Schema 编译上下文
3. **预聚合隔离**: 独立的预聚合 Schema 命名空间 `pre_aggregations_{dataSourceId}`
4. **缓存隔离**: 基于数据源的独立缓存空间

**优势**:
- 不同租户/数据源的查询完全隔离,互不干扰
- 缓存隔离,避免数据泄露风险
- 支持 SaaS 级别的多租户场景
- 每个租户可以有独立的 Schema 版本和配置

### 5. **企业级权限控制** 🔐

Synmetrix 实现了完整的 RBAC (基于角色的访问控制) 系统:

#### 认证流程:

```
Frontend → Hasura (JWT) → Cube.js/Actions (验证) → 数据源访问
```

#### 核心实现:

```javascript
// checkAuth.js
const checkAuth = async (req) => {
  const authHeader = req.headers.authorization;
  const dataSourceId = req.headers["x-hasura-datasource-id"];
  const branchId = req.headers["x-hasura-branch-id"];

  // 验证 JWT Token
  jwtDecoded = jwt.verify(authToken, JWT_KEY, {
    algorithms: [JWT_ALGORITHM],
  });

  const { "x-hasura-user-id": userId } = jwtDecoded?.hasura || {};

  // 构建用户权限范围
  const userScope = defineUserScope(
    user.dataSources,
    user.members,
    dataSourceId,
    branchId,
    branchVersionId
  );

  req.securityContext = {
    authToken,
    userId,
    userScope,
  };
};
```

#### 权限层级:
- **用户级**: JWT 认证和用户身份验证
- **团队级**: 基于 Team 成员关系的权限
- **数据源级**: 细粒度的数据源访问控制
- **分支级**: 支持对不同分支的访问控制
- **SQL API 级**: 独立的 SQL API 用户凭证系统

**优势**:
- 完整的企业级认证授权体系
- 细粒度的权限控制
- 支持团队协作场景
- 符合企业安全合规要求

### 6. **调度报告和告警系统** 📊

这是 Cube.js 完全没有的高级功能:

#### 告警系统

实现位置: `services/actions/src/rpc/checkAlert.js`

**核心功能**:
- 基于指标阈值的自动监控
- 支持单个或多个度量值的边界检查
- 锁机制防止重复触发 (`alertLocks.js`)
- 可配置的请求超时和触发冷却时间
- 自动生成和发送告警通知

```javascript
// checkAlert.js 核心逻辑
const checkMultipleMeasuresBounds = ({
  dataset,
  triggerConfig,
  defaultMeasure,
}) => {
  if (Object.keys(triggerConfig?.measures || {}).length > 0) {
    const measures = Object.entries(triggerConfig.measures).map(([k, v]) => ({
      key: k.replace(":", "."),
      config: v,
    }));

    return measures.some(({ key, config }) =>
      checkBoundsMatch({
        dataset,
        measure: key,
        lowerBound: config.lowerBound,
        upperBound: config.upperBound,
      })
    );
  }
  // ... 单度量检查逻辑
};
```

**告警触发流程**:
1. 定时任务触发告警检查
2. 执行查询获取最新数据
3. 检查指标是否超出阈值
4. 生成 Exploration 截图
5. 通过配置的渠道发送通知
6. 设置冷却时间防止频繁触发

#### 报告系统

实现位置: `services/actions/src/rpc/checkReport.js`

**核心功能**:
- 定时生成和发送数据报告
- 支持多种交付方式 (邮件、Slack 等)
- 集成 Exploration 可视化截图
- 基于 Cron 的调度系统

**优势**:
- 支持主动监控和异常检测
- 减少人工检查工作量
- 及时发现数据异常
- 支持定期报告自动化

### 7. **版本控制和审计** 📝

Synmetrix 内置了类似 Git 的版本控制系统:

#### 版本控制功能:
- **分支管理**: 支持创建多个 Schema 分支
- **版本追踪**: 每次 Schema 变更创建新版本
- **Checksum 验证**: MD5 校验保证数据一致性
- **变更历史**: 完整记录所有 Schema 变更
- **版本切换**: 可回滚到任意历史版本

```javascript
// generateDataSchema.js
let commitChecksum = files.reduce((acc, cur) => acc + cur.code, "");
commitChecksum = createMd5Hex(commitChecksum);

const commitObject = {
  authToken,
  user_id: userId,
  branch_id: branchId,
  checksum: commitChecksum,
  dataschemas: {
    data: [...preparedSchemas],
  },
};

await createDataSchema(commitObject);
```

#### 审计功能:
- 记录操作用户和时间戳
- 追踪 Schema 变更历史
- 支持审计日志查询
- 满足合规性要求

**优势**:
- 支持实验性 Schema 开发
- 可安全回滚错误变更
- 提供完整的变更历史
- 支持多环境部署策略

### 8. **数据探索和协作** 🔍

Synmetrix 提供了强大的数据探索和团队协作功能:

#### 数据探索功能:
- **可视化查询构建器**: Web UI 界面无需写 SQL
- **Playground**: 交互式查询构建和测试
- **结果可视化**: 内置图表和可视化组件
- **查询保存**: 保存和分享 Exploration 配置

#### 协作功能:
- **团队管理**: 支持创建和管理团队
- **成员邀请**: 邀请团队成员协作
- **共享探索**: 分享查询配置和结果
- **权限管理**: 基于角色的访问控制

#### BI 工具集成:
通过 SQL API 无缝对接主流 BI 工具:
- Apache Superset
- DBeaver
- TablePlus
- 任何支持 PostgreSQL/MySQL 协议的工具

**优势**:
- 降低数据分析门槛
- 促进团队协作和知识共享
- 与现有 BI 工具生态兼容
- 统一的数据访问入口

### 9. **增强的 SQL API** 💾

Synmetrix 扩展了 Cube.js 的 SQL API 功能:

#### 协议支持:
- **MySQL 协议**: 端口 13306 (`CUBEJS_SQL_PORT`)
- **PostgreSQL 协议**: 端口 15432 (`CUBEJS_PG_SQL_PORT`)

#### 认证机制:
- 每个数据源独立的 SQL 用户凭证
- 基于用户名/密码的认证
- 与 Cube.js REST API 认证分离
- 支持传统 BI 工具的连接方式

#### Demo 凭证示例:
```
| Host      | Port  | Database | User                 | Password              |
|-----------|-------|----------|----------------------|-----------------------|
| localhost | 15432 | db       | demo_pg_user         | demo_pg_pass          |
| localhost | 15432 | db       | demo_clickhouse_user | demo_clickhouse_pass  |
```

**优势**:
- 兼容现有 BI 工具生态
- 无需修改客户端代码
- 支持标准 SQL 查询
- 降低迁移成本

### 10. **开发者友好的 RPC 框架** 🛠️

Actions 服务提供了简洁的 RPC 框架:

#### 约定大于配置:
```javascript
// 文件路径即 RPC 路由
services/actions/src/rpc/genSchemas.js → POST /rpc/gen-schemas
services/actions/src/rpc/checkAlert.js → POST /rpc/check-alert
```

#### RPC 方法格式:
```javascript
// services/actions/src/rpc/{methodName}.js
export default async (session, input, headers) => {
  // session: Hasura session 信息
  // input: 请求参数
  // headers: HTTP headers

  return { data, error };
};
```

#### 自动路由:
- 文件名自动映射为路由 (kebab-case)
- 无需手动注册路由
- 支持热重载开发

**优势**:
- 简化 API 开发流程
- 统一的错误处理
- 自动集成认证
- 易于扩展新功能

### 11. **完整的基础设施** 🏗️

Synmetrix 提供开箱即用的完整技术栈:

#### 数据存储:
- **PostgreSQL**: 元数据存储 (端口 5435)
- **Cubestore**: 分布式缓存和查询引擎 (端口 3030)
- **MinIO**: S3 兼容对象存储 (端口 9000, 9001)

#### 缓存和消息:
- **Redis**: (端口 6379)

#### 开发工具:
- **Mailhog**: 邮件测试 (SMTP: 1025, UI: 8025)

#### 部署方案:
- Docker Compose 配置文件
- 多环境支持 (dev, stage, test, production)
- CLI 工具 (smcli) 简化管理
- AWS 基础设施代码 (`infra/aws/`)

**优势**:
- 快速启动和部署
- 生产就绪的架构
- 统一的技术栈
- 完整的监控和日志

## 📊 功能对比总结

| 功能维度 | Cube.js | Synmetrix |
|---------|---------|-----------|
| **Schema 管理** | 文件系统存储 | 数据库动态管理 |
| **Schema 生成** | 手动编写 | 自动生成 + UI 编辑 |
| **版本控制** | 依赖 Git | 内置分支和版本系统 |
| **多租户** | 基础支持 | 完全隔离的多租户架构 |
| **权限控制** | 需自行实现 | 完整 RBAC 系统 |
| **告警监控** | ❌ 不支持 | ✅ 完整告警系统 |
| **定时报告** | ❌ 不支持 | ✅ Cron 调度报告 |
| **Web UI** | ❌ 仅开发环境 | ✅ 生产级 Web 界面 |
| **团队协作** | ❌ 不支持 | ✅ 团队和成员管理 |
| **SQL API 认证** | 基础 JWT | 独立用户凭证系统 |
| **数据探索** | API only | 可视化查询构建器 |
| **部署方式** | 自行部署 | Docker Stack 一键部署 |
| **基础设施** | 需自行搭建 | 完整技术栈 |
| **文档和示例** | 框架文档 | 平台文档 + 视频教程 |

## 🎯 适用场景分析

### 选择 Synmetrix 的场景:

✅ **企业级数据平台**
- 需要多团队协作的数据建模
- 需要细粒度的权限控制
- 需要审计和合规性要求

✅ **SaaS 多租户场景**
- 每个客户独立的数据源和 Schema
- 需要完全隔离的查询和缓存
- 需要自助式数据建模

✅ **快速原型和迭代**
- 非技术用户参与数据建模
- 频繁的 Schema 变更
- 无需 CI/CD 流程

✅ **监控和报告需求**
- 需要自动化告警
- 定期数据报告
- 异常检测和通知

✅ **BI 工具集成**
- 需要 SQL API 接入现有 BI 工具
- 统一的语义层管理
- 跨数据源的指标整合

### 选择原生 Cube.js 的场景:

✅ **轻量级嵌入式分析**
- 单体应用内嵌入式分析
- 不需要完整的用户管理系统
- 简单的单租户场景

✅ **完全自定义架构**
- 需要深度定制 Cube.js 行为
- 已有完整的认证授权系统
- 需要与特定技术栈深度集成

✅ **简单场景**
- Schema 相对稳定,变更少
- 开发团队主导,不需要业务用户参与
- 不需要 Web UI

## 🔧 技术架构亮点

### 1. Security Context 设计

Synmetrix 的安全上下文设计是其多租户架构的核心:

```javascript
securityContext = {
  authToken: "JWT Token",
  userId: "user-uuid",
  userScope: {
    dataSource: {
      dataSourceId: "ds-uuid",
      dataSourceVersion: "SHA256-hash",
      dbType: "postgres",
      dbParams: { /* 连接参数 */ },
      files: ["schema-id-1", "schema-id-2"],
      schemaVersion: "MD5-hash",
      preAggregationSchema: "pre_aggregations_ds-uuid"
    }
  }
}
```

这个设计实现了:
- 完整的用户身份和权限信息
- 数据源级别的隔离
- Schema 版本追踪
- 预聚合隔离

### 2. 动态 Driver Factory

支持多种数据库类型的动态驱动加载:

```javascript
// driverFactory.js
const driverFactory = async ({ securityContext, dataSource }) => {
  const { userScope, user } = securityContext || {};

  let dbParams;
  let dbType;

  if (!dataSource || dataSource === "default") {
    dbParams = userScope.dataSource.dbParams;
    dbType = userScope.dataSource.dbType;
  } else {
    // 支持切换不同数据源
    const nextUserScope = defineUserScope(
      user.dataSources,
      user.members,
      dataSource
    );
    dbParams = nextUserScope.dataSource.dbParams;
    dbType = nextUserScope.dataSource.dbType;
  }

  // 动态加载对应的数据库驱动
  const dbDriver = DriverDependencies[dbType];
  driverModule = await import(dbDriver);

  return new driverModule.default(dbParams);
};
```

支持的数据库:
- PostgreSQL, MySQL, ClickHouse
- BigQuery, Athena, Redshift, Snowflake
- Databricks, Druid, DuckDB
- Elasticsearch, MongoDB, Vertica 等

### 3. Repository Factory 模式

实现了 Schema 的动态加载:

```javascript
const repositoryFactory = ({ securityContext }) => {
  return {
    dataSchemaFiles: async () => {
      // 从数据库加载 Schema
      const ids = securityContext?.userScope?.dataSource?.files;
      const dataSchemas = await findDataSchemasByIds({ ids });

      // 映射为 Cube.js 文件格式
      return dataSchemas.map(mapSchemaToFile);
    },
  };
};
```

优势:
- 无需文件系统访问
- 支持动态更新
- 基于权限的 Schema 加载
- 版本化的 Schema 管理

## 📈 性能优化

### 1. 多层缓存架构
- Cubestore: 预聚合数据存储
- Compiler Cache: Schema 编译缓存

### 2. 查询优化
- 基于 Cube.js 的智能查询重写
- 预聚合支持
- 分布式查询执行

### 3. 隔离性能影响
- 每个数据源独立的 Orchestrator
- 避免不同租户间的性能干扰
- 独立的缓存空间

## 🚀 部署和运维

### 开发环境启动

```bash
# 克隆仓库
git clone https://github.com/mlcraft-io/mlcraft.git

# 启动开发环境
./cli.sh compose up -e dev

# 查看日志
./cli.sh compose logs cubejs
```

### 生产环境部署

```bash
# 下载 docker-compose 文件
wget https://raw.githubusercontent.com/mlcraft-io/mlcraft/main/install-manifests/docker-compose/docker-compose.yml

# 启动服务
docker-compose pull stack && docker-compose up -d

# 等待服务就绪 (5-7 分钟)
docker-compose logs -f
```

### 环境要求

| 组件 | 要求 |
|------|------|
| CPU | 3.2 GHz 多核处理器 |
| 内存 | 8 GB 以上 |
| 磁盘 | 30 GB 可用空间 |
| Docker | 最新版本 |
| Docker Compose | 最新版本 |

## 📚 学习资源

### 官方文档
- [Synmetrix 文档](https://docs.synmetrix.org/)
- [Cube.js 文档](https://cube.dev/docs)

### 示例和教程
- [与 Apache Superset 集成](https://github.com/mlcraft-io/examples/tree/main/superset) ([视频](https://www.youtube.com/watch?v=TzLy88IAYZo))
- [与 Observable 集成](https://github.com/mlcraft-io/examples/tree/main/observable) ([视频](https://www.youtube.com/watch?v=VcAP4vrL8cY))
- [SQL API 与 DBeaver](https://github.com/mlcraft-io/examples/tree/main/dbeaver) ([视频](https://www.youtube.com/watch?v=8l_Ud3IM0OQ))
- [LLM 语义层集成](https://github.com/mlcraft-io/examples/tree/main/langchain) ([视频](https://www.youtube.com/watch?v=TtH-pFGDK84))
- [性能基准测试](https://github.com/mlcraft-io/examples/tree/main/benchmarks)

### 社区支持
- [Slack 社区](https://join.slack.com/t/mlcraft/shared_invite/zt-1x2gxwn37-J3tTvCR5xSFVfxwUU_YKtg)
- [GitHub](https://github.com/mlcraft-io/mlcraft)
- [Twitter](https://twitter.com/trySynmetrix)
- [YouTube](https://www.youtube.com/channel/UCEPlxaWYrdOaf9IXjD2IRTg)

## 🎓 总结

Synmetrix 将 Cube.js 从一个**"语义层框架"**提升为**"企业级数据工程平台"**,特别适合以下场景:

1. **需要快速构建多租户分析平台**
2. **业务用户参与数据建模**
3. **需要企业级权限和审计**
4. **需要告警和自动化报告**
5. **希望开箱即用的完整解决方案**

通过动态 Schema 管理、完整的多租户隔离、企业级权限控制、告警报告系统等创新功能,Synmetrix 为构建现代化的数据分析平台提供了强大的基础。

同时,Synmetrix 保持了对 Cube.js 核心能力的完全兼容,用户可以充分利用 Cube.js 生态系统的资源和最佳实践。

---

**最后更新**: 2025-10-11
**Cube.js 版本**: v1.2.3
**Synmetrix 架构**: 微服务架构
**许可证**: Apache License 2.0 (核心) + MIT License (其他内容)
