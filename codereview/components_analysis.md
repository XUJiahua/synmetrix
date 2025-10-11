# Synmetrix 组件必要性分析

## 生产部署配置分析

基于 `install-manifests/docker-compose/docker-compose.yml` 文件，这是 Synmetrix 官方推荐的生产部署配置，采用了**单一容器（All-in-One）架构**。

### 当前配置概览

```yaml
version: '3.8'

services:
  redis:          # 缓存服务
  postgres:       # 主数据库
  cubestore:      # 分析缓存
  stack:          # All-in-One 容器（包含所有服务）

volumes:
  pg_db_data:         # PostgreSQL 数据持久化
  cube_store_data:    # Cubestore 数据持久化
```

## 组件详细分析

### 1. Redis （必须）

```yaml
redis:
  image: redis:7.0.0
  ports:
    - 6379:6379
```

**角色**：
- 会话存储
- 临时缓存
- Pub/Sub 消息队列

**是否必须**：✅ **必须**

**原因**：
- Hasura GraphQL Engine 需要 Redis 存储会话
- Actions 服务使用 Redis 缓存临时数据
- 如果使用 Redis 替代 Cubestore 做查询缓存，更加必须

**不使用的后果**：
- Hasura 无法正常工作
- 用户会话管理失效
- 多实例部署时会有问题

**资源占用**：
- 内存：~50-200MB
- CPU：极低

---

### 2. PostgreSQL （必须）

```yaml
postgres:
  image: postgres:${POSTGRES_VERSION:-12}
  volumes:
    - pg_db_data:/var/lib/postgresql/data
  environment:
    POSTGRES_USER: synmetrix_stack
    POSTGRES_PASSWORD: pg_pass
    POSTGRES_DB: synmetrix_stack
```

**角色**：
- Hasura 元数据存储
- 用户数据（users, teams, members）
- 数据源配置（datasources, branches, versions）
- Data Schema 存储（dataschemas）
- 权限配置（access_list, roles）

**是否必须**：✅ **必须**

**原因**：
- 存储 Synmetrix 的所有配置和元数据
- Hasura 的后端数据库
- 动态 schema 加载的来源

**不使用的后果**：
- 整个系统无法运行
- 无数据持久化

**资源占用**：
- 存储：~1-10GB（取决于数据量）
- 内存：~256MB-2GB
- CPU：中等

**可替代性**：❌ 不可替代，但可以使用外部 PostgreSQL 实例

---

### 3. Cubestore （可选，推荐保留）

```yaml
cubestore:
  image: cubejs/cubestore:${CUBESTORE_VERSION:-v0.35.33}
  environment:
    - CUBESTORE_REMOTE_DIR=/cube/data
  volumes:
    - cube_store_data:/cube/data
```

**角色**：
- 预聚合表存储
- 查询结果缓存
- 分布式查询引擎

**是否必须**：⚠️ **可选（但强烈推荐）**

**原因**：
- 性能优化的核心组件
- 大数据场景下必不可少
- 可以用 Redis 替代部分功能

**不使用的后果**：
- 查询性能显著下降
- 无法使用预聚合
- 高并发场景下源数据库压力大

**资源占用**：
- 存储：~10GB-100GB+（取决于预聚合规模）
- 内存：~1GB-8GB
- CPU：中等到高

**替代方案**：
- 方案 1：完全禁用（小规模场景）
- 方案 2：使用 Redis 做缓存（中等规模）
- 方案 3：预聚合存储在源数据库（特定场景）

**详细分析**：参见 `cubestore_optional.md`

---

### 4. Stack 容器 （必须 - All-in-One）

```yaml
stack:
  image: synmetrix/stack:${STACK_VERSION:-latest}
  restart: always
  ports:
    - 80:8888     # Nginx + Frontend
    - 4000:4000   # Cube.js REST API
    - 15432:15432 # PostgreSQL SQL API
    - 13306:13306 # MySQL SQL API
  environment:
    HASURA_GRAPHQL_DATABASE_URL: postgres://synmetrix_stack:pg_pass@postgres:5432/synmetrix_stack
    HASURA_GRAPHQL_ADMIN_SECRET: adminsecret
    SMTP_HOST: ${SECRETS_SMTP_HOST}
    SMTP_PORT: ${SECRETS_SMTP_PORT}
    # ... 更多配置
```

**角色**：包含所有应用服务（All-in-One 架构）

**Stack 容器内部包含的服务**：

#### 4.1 Frontend (Client) - 必须
- **作用**：Web UI 界面
- **端口**：8888 (通过 Nginx 代理到 80)
- **技术栈**：React 应用
- **是否必须**：✅ 必须（如果需要 Web 界面）
- **可选性**：如果只用 API，可以不需要

#### 4.2 Hasura GraphQL Engine - 必须
- **作用**：GraphQL API 层、认证、权限管理
- **端口**：内部 8080
- **是否必须**：✅ 必须
- **不可替代**：整个架构依赖 Hasura

#### 4.3 Hasura Backend Plus - 可选
- **作用**：文件存储、扩展认证功能
- **端口**：内部 3000
- **是否必须**：⚠️ 可选
- **用途**：
  - 文件上传/下载
  - Magic link 认证
  - S3 集成

#### 4.4 Cube.js Service - 必须
- **作用**：查询引擎、SQL API、数据建模
- **端口**：4000, 13306, 15432
- **是否必须**：✅ 必须
- **核心功能**：整个语义层的核心

#### 4.5 Actions Service - 必须
- **作用**：业务逻辑处理、RPC 服务
- **端口**：内部 3000
- **是否必须**：✅ 必须
- **功能**：
  - Schema 生成
  - 查询执行
  - 报表和告警

#### 4.6 Nginx - 必须
- **作用**：反向代理、静态文件服务
- **端口**：8888 (映射到宿主机 80)
- **是否必须**：✅ 必须（在 stack 容器中）
- **功能**：
  - 路由分发
  - 负载均衡
  - SSL 终止

## Stack 容器 vs 开发环境对比

### 生产环境（Stack 容器）

```
┌─────────────────────────────────────────────┐
│       synmetrix/stack:latest                │
│  ─────────────────────────────────────      │
│  ┌─────────────┐  ┌──────────────────┐     │
│  │   Nginx     │  │  Frontend (React)│     │
│  │   :8888     │  │                  │     │
│  └─────┬───────┘  └──────────────────┘     │
│        │                                     │
│  ┌─────┴───────┬─────────┬──────────┐      │
│  │   Hasura    │ Actions │  Cube.js │      │
│  │   :8080     │ :3000   │  :4000   │      │
│  └─────────────┴─────────┴──────────┘      │
│  │ Hasura+     │                            │
│  │ :3000       │                            │
│  └─────────────┘                            │
└─────────────────────────────────────────────┘
       ↓             ↓              ↓
   PostgreSQL    Redis        Cubestore
```

**优势**：
- ✓ 部署简单（单一容器）
- ✓ 资源占用少（共享进程）
- ✓ 网络延迟低（本地通信）
- ✓ 适合中小规模部署

**劣势**：
- ✗ 无法单独扩展某个服务
- ✗ 故障影响所有服务
- ✗ 升级需要重启整个容器

### 开发环境（独立容器）

```
┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐
│  Client  │  │  Hasura  │  │ Actions  │  │  Cube.js │
│  :80     │  │  :8080   │  │ :3000    │  │  :4000   │
└────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘
     │             │              │             │
     └─────────────┴──────────────┴─────────────┘
                         ↓
                    Docker Network
                         ↓
     ┌──────────────────┴──────────────────┐
     │                                      │
┌────┴─────┐  ┌──────────┐  ┌────────────┐
│PostgreSQL│  │  Redis   │  │ Cubestore  │
│  :5432   │  │  :6379   │  │  :3030     │
└──────────┘  └──────────┘  └────────────┘
```

**优势**：
- ✓ 单独扩展每个服务
- ✓ 独立升级和重启
- ✓ 故障隔离
- ✓ 适合开发调试

**劣势**：
- ✗ 配置复杂
- ✗ 资源占用多
- ✗ 网络开销大

## 组件必要性总结表

| 组件 | 必要性 | 可替代性 | 资源占用 | 不使用的影响 |
|------|--------|---------|---------|-------------|
| **Redis** | ✅ 必须 | ❌ 不可替代 | 低（50-200MB） | 系统无法运行 |
| **PostgreSQL** | ✅ 必须 | ⚠️ 可用外部实例 | 中（256MB-2GB） | 系统无法运行 |
| **Cubestore** | ⚠️ 推荐 | ✅ 可用 Redis | 高（1-8GB） | 性能显著下降 |
| **Stack (All-in-One)** | ✅ 必须 | ❌ 不可替代 | 中高（2-4GB） | 系统无法运行 |
| ├─ Frontend | ⚠️ 可选 | ✅ 可单独部署 | 低 | 无 Web 界面 |
| ├─ Hasura | ✅ 必须 | ❌ 不可替代 | 中 | 系统无法运行 |
| ├─ Hasura+ | ⚠️ 可选 | ✅ 可禁用 | 低 | 文件功能不可用 |
| ├─ Cube.js | ✅ 必须 | ❌ 不可替代 | 中 | 无查询功能 |
| ├─ Actions | ✅ 必须 | ❌ 不可替代 | 低 | 业务逻辑失效 |
| └─ Nginx | ✅ 必须 | ⚠️ 可替代 | 极低 | 无法访问 |

## 最小化部署方案

### 方案 1: 最小核心（仅 API）

如果你只需要 API 功能，不需要 Web 界面：

```yaml
version: '3.8'

services:
  redis:
    image: redis:7.0.0

  postgres:
    image: postgres:12
    volumes:
      - pg_db_data:/var/lib/postgresql/data
    environment:
      POSTGRES_USER: synmetrix
      POSTGRES_PASSWORD: password
      POSTGRES_DB: synmetrix

  stack:
    image: synmetrix/stack:latest
    restart: always
    ports:
      - 4000:4000   # Cube.js API (必须)
      - 15432:15432 # PostgreSQL SQL API (可选)
      - 13306:13306 # MySQL SQL API (可选)
      # - 80:8888   # Web UI (禁用)
    environment:
      HASURA_GRAPHQL_DATABASE_URL: postgres://synmetrix:password@postgres:5432/synmetrix
      HASURA_GRAPHQL_ADMIN_SECRET: your-secret-here
      # SMTP 配置（如果不需要邮件功能可以不配置）
      # S3 配置（如果不需要文件存储可以不配置）
    depends_on:
      - postgres
      - redis

volumes:
  pg_db_data:
```

**最小资源需求**：
- CPU: 2 核
- 内存: 2GB
- 存储: 10GB

**功能**：
- ✓ GraphQL API
- ✓ REST API
- ✓ SQL API
- ✗ Web 界面

---

### 方案 2: 标准部署（推荐）

包含所有核心功能和性能优化：

```yaml
version: '3.8'

services:
  redis:
    image: redis:7.0.0

  postgres:
    image: postgres:12
    volumes:
      - pg_db_data:/var/lib/postgresql/data
    environment:
      POSTGRES_USER: synmetrix
      POSTGRES_PASSWORD: password
      POSTGRES_DB: synmetrix

  cubestore:
    image: cubejs/cubestore:v0.35.33
    volumes:
      - cube_store_data:/cube/data
    environment:
      - CUBESTORE_REMOTE_DIR=/cube/data

  stack:
    image: synmetrix/stack:latest
    restart: always
    ports:
      - 80:8888     # Web UI
      - 4000:4000   # Cube.js API
      - 15432:15432 # SQL API
      - 13306:13306 # SQL API
    environment:
      HASURA_GRAPHQL_DATABASE_URL: postgres://synmetrix:password@postgres:5432/synmetrix
      HASURA_GRAPHQL_ADMIN_SECRET: your-secret-here
    depends_on:
      - postgres
      - redis
      - cubestore

volumes:
  pg_db_data:
  cube_store_data:
```

**资源需求**：
- CPU: 4 核
- 内存: 8GB
- 存储: 50GB

**功能**：
- ✓ 完整功能
- ✓ Web 界面
- ✓ 高性能查询
- ✓ 预聚合支持

---

### 方案 3: 无 Cubestore（小规模）

如果数据量小、并发低，可以不使用 Cubestore：

```yaml
version: '3.8'

services:
  redis:
    image: redis:7.0.0

  postgres:
    image: postgres:12
    volumes:
      - pg_db_data:/var/lib/postgresql/data
    environment:
      POSTGRES_USER: synmetrix
      POSTGRES_PASSWORD: password
      POSTGRES_DB: synmetrix

  stack:
    image: synmetrix/stack:latest
    restart: always
    ports:
      - 80:8888
      - 4000:4000
      - 15432:15432
      - 13306:13306
    environment:
      HASURA_GRAPHQL_DATABASE_URL: postgres://synmetrix:password@postgres:5432/synmetrix
      HASURA_GRAPHQL_ADMIN_SECRET: your-secret-here
      # 配置使用 Redis 代替 Cubestore
      CUBEJS_CACHE_AND_QUEUE_DRIVER: redis
      REDIS_URL: redis://redis:6379
    depends_on:
      - postgres
      - redis

volumes:
  pg_db_data:
```

**注意**：需要修改 Stack 镜像内的配置，或者自己构建镜像。

**资源需求**：
- CPU: 2 核
- 内存: 4GB
- 存储: 20GB

**适用场景**：
- 数据量 < 1000 万行
- 并发 < 10 QPS
- 实时性要求高

---

## 可选功能配置

### SMTP（邮件功能）

```yaml
stack:
  environment:
    SMTP_HOST: ${SECRETS_SMTP_HOST}
    SMTP_PORT: ${SECRETS_SMTP_PORT}
    SMTP_SECURE: ${SECRETS_SMTP_SECURE}
    SMTP_USER: ${SECRETS_SMTP_USER}
    SMTP_PASS: ${SECRETS_SMTP_PASS}
    SMTP_SENDER: ${SECRETS_SMTP_SENDER}
```

**用途**：
- 用户注册验证
- 密码重置
- 报表和告警通知

**是否必须**：⚠️ **可选**

**不配置的影响**：
- 无法发送邮件通知
- 用户注册需要手动激活
- 报表功能受限

---

### S3 存储（文件上传）

```yaml
stack:
  environment:
    AWS_S3_ACCESS_KEY_ID: ${SECRETS_AWS_S3_ACCESS_KEY_ID}
    AWS_S3_SECRET_ACCESS_KEY: ${SECRETS_AWS_S3_SECRET_ACCESS_KEY}
    AWS_S3_BUCKET_NAME: ${SECRETS_AWS_S3_BUCKET_NAME}
```

**用途**：
- 用户头像上传
- 文件附件存储
- 报表导出文件

**是否必须**：⚠️ **可选**

**不配置的影响**：
- 无法上传文件
- 可以用本地存储替代（需要配置卷）

---

## 扩展和优化建议

### 单机部署建议

**小规模（< 100 用户，< 1000 万行数据）**：
```
- CPU: 4 核
- 内存: 8GB
- 存储: 50GB SSD
- 组件: Redis + PostgreSQL + Stack（无 Cubestore）
```

**中等规模（100-1000 用户，1000 万-1 亿行数据）**：
```
- CPU: 8 核
- 内存: 16GB
- 存储: 200GB SSD
- 组件: Redis + PostgreSQL + Cubestore + Stack
```

**大规模（> 1000 用户，> 1 亿行数据）**：
```
- CPU: 16+ 核
- 内存: 32GB+
- 存储: 500GB+ SSD
- 组件: Redis + PostgreSQL + Cubestore + Stack
- 建议: 拆分为微服务架构，独立扩展
```

---

### 分布式部署建议

对于大规模部署，建议拆分 Stack 容器，使用开发环境的微服务架构：

```yaml
# 生产级微服务架构
services:
  # 负载均衡
  nginx:
    image: nginx:latest
    ports:
      - 80:80
      - 443:443
    depends_on:
      - frontend
      - hasura
      - cubejs

  # 前端（可多实例）
  frontend:
    image: synmetrix/client:latest
    deploy:
      replicas: 2

  # Hasura（可多实例）
  hasura:
    image: hasura/graphql-engine:v2.40.2
    deploy:
      replicas: 2
    depends_on:
      - postgres
      - redis

  # Cube.js（可多实例）
  cubejs:
    build: ./services/cubejs
    deploy:
      replicas: 3
    depends_on:
      - postgres
      - redis
      - cubestore

  # Actions（可多实例）
  actions:
    build: ./services/actions
    deploy:
      replicas: 2
    depends_on:
      - postgres
      - redis

  # 数据库（推荐使用托管服务）
  postgres:
    image: postgres:12
    # 或使用 AWS RDS, Google Cloud SQL 等

  # Redis（推荐使用托管服务或集群）
  redis:
    image: redis:7.0.0
    # 或使用 AWS ElastiCache, Redis Cluster 等

  # Cubestore（可多实例集群）
  cubestore:
    image: cubejs/cubestore:v0.35.33
    deploy:
      replicas: 3
```

---

## 决策流程图

```
你的部署场景？
    │
    ├─→ 只需要 API，不需要 Web 界面
    │   └─→ 方案 1：最小核心
    │       组件: Redis + PostgreSQL + Stack
    │       资源: 2 核 + 2GB 内存
    │
    ├─→ 数据量小（< 1000 万行）
    │   并发低（< 10 QPS）
    │   └─→ 方案 3：无 Cubestore
    │       组件: Redis + PostgreSQL + Stack
    │       资源: 2 核 + 4GB 内存
    │
    ├─→ 中等规模（1000 万 - 1 亿行）
    │   或并发中等（10-50 QPS）
    │   └─→ 方案 2：标准部署
    │       组件: Redis + PostgreSQL + Cubestore + Stack
    │       资源: 4 核 + 8GB 内存
    │
    └─→ 大规模（> 1 亿行）
        或高并发（> 50 QPS）
        └─→ 微服务架构
            拆分 Stack，独立扩展各服务
            资源: 16+ 核 + 32GB+ 内存
```

---

## 总结

### 绝对必须的组件（3 个）

1. **Redis** - 会话存储和缓存
2. **PostgreSQL** - 元数据存储
3. **Stack 容器** - 应用服务集合

### 强烈推荐的组件（1 个）

4. **Cubestore** - 性能优化（除非是小规模部署）

### 可选配置

- **SMTP** - 邮件功能（可不配置）
- **S3** - 文件存储（可用本地存储替代）
- **Web UI** - 可以只用 API

### 最精简配置

仅保留 3 个核心组件：
```
Redis + PostgreSQL + Stack
```

**适用场景**：测试、开发、小规模生产（< 1000 万行数据）

**资源需求**：2 核 CPU + 2-4GB 内存 + 20GB 存储

### 推荐生产配置

包含所有 4 个组件：
```
Redis + PostgreSQL + Cubestore + Stack
```

**适用场景**：中到大规模生产环境

**资源需求**：4-8 核 CPU + 8-16GB 内存 + 50-200GB 存储
