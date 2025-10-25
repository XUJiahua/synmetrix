# Docker Compose 配置文件对比分析

本文档分析了 Synmetrix 项目中四个不同的 docker-compose 配置文件，说明它们的用途、特点和差异。

## 概述

项目包含以下四个 docker-compose 文件：

1. `docker-compose.dev.yml` - 本地开发环境
2. `docker-compose.stage.yml` - 预发布/暂存环境
3. `docker-compose.test.yml` - 测试环境
4. `docker-compose.stack.yml` - 一体化生产栈

---

## 1. docker-compose.dev.yml - 本地开发环境

### 特点

- **完整的微服务架构**（12 个独立服务）
- **热重载支持**：挂载源代码目录，使用 `yarn start.dev` 命令
- **调试端口暴露**：
  - 9231（Node.js debugger for cubejs）
  - 9695、9693（Hasura CLI）
  - 9055（Client dev server）
- **开发工具**：
  - mailhog（邮件测试服务，SMTP: 1025, Web UI: 8025）
  - hasura_cli（数据库迁移和控制台）
- **本地构建**：所有自定义服务使用 `build:` 配置，从本地 Dockerfile 构建
- **卷挂载**：源代码实时同步，支持热重载

### 服务列表

```yaml
services:
  - redis (6379)
  - postgres (5435)
  - actions (本地构建，源码挂载)
  - cubejs (本地构建，源码挂载，端口：4000, 9231, 13306, 15432)
  - cubejs_refresh_worker (共享镜像，源码挂载)
  - hasura_cli (9695, 9693)
  - hasura (8080)
  - hasura_plus (8081)
  - minio (9000, 9001)
  - mailhog (1025, 8025)
  - client (80, 9055)
  - cubestore (3030)
```

### 优化改进

已优化 cubejs 和 cubejs_refresh_worker 的重复构建问题：

```yaml
cubejs:
  build:
    context: ./services/cubejs
  image: synmetrix/cubejs:dev  # 构建后打标签

cubejs_refresh_worker:
  image: synmetrix/cubejs:dev  # 复用镜像，不重复构建
```

### 环境配置

- 环境文件：`.env` + `.dev.env`
- 网络：外部网络 `synmetrix_default`

---

## 2. docker-compose.stage.yml - 预发布/暂存环境

### 特点

- **生产级配置**：使用镜像仓库 `${REGISTRY_HOST}/synmetrix/*`
- **Traefik 反向代理**：自动 HTTPS、负载均衡、SSL 证书管理
- **域名路由**：基于域名的服务路由
- **Docker Swarm 支持**：deploy labels、placement constraints
- **生产命令**：使用 `yarn start`（非 dev 模式）

### 域名路由配置

| 域名 | 服务 | 说明 |
|------|------|------|
| `app.${DOMAIN}` | client | 前端应用 |
| `api.${DOMAIN}` | hasura | GraphQL API |
| `cube.${DOMAIN}` | cubejs | Cube.js REST API |
| `s3.${DOMAIN}` | minio | 对象存储 |
| `lb.${DOMAIN}` | traefik | 负载均衡器管理界面 |

### Traefik 配置特点

- **自动 HTTPS**：Let's Encrypt 自动证书申请和续期
- **HTTP → HTTPS 重定向**：强制使用 HTTPS
- **TCP 路由**：支持数据库协议路由
  - PostgreSQL API: 15432
  - MySQL API: 13306
- **Swarm 模式**：适配 Docker Swarm 集群部署
- **压缩中间件**：client 服务启用 gzip 压缩

### 服务列表

```yaml
services:
  - redis
  - postgres (带持久化卷)
  - traefik (负载均衡器，80, 443, 15432, 13306)
  - cubejs (镜像仓库)
  - client (镜像仓库)
  - hasura
  - hasura_plus (镜像仓库)
  - actions (镜像仓库)
  - cubestore (带持久化卷)
  - minio (带持久化卷)
```

### 环境配置

- 环境文件：`.env` + `.stage.env`
- 网络：外部网络 `synmetrix_default`
- 关键环境变量：
  - `DOMAIN` - 部署域名
  - `LETSENCRYPT_EMAIL` - SSL 证书邮箱
  - `REGISTRY_HOST` - Docker 镜像仓库地址

---

## 3. docker-compose.test.yml - 测试环境

### 特点

- **测试专用**：使用预构建镜像 `synmetrix/*`
- **额外测试数据库**：`postgres_test` 服务，预加载测试数据
- **Cube.js 副本**：3 个副本进行负载和并发测试
- **独立卷**：test 专用 volumes，数据隔离
- **简化配置**：移除开发工具和调试端口

### 测试数据库配置

```yaml
postgres_test:
  image: postgres:12
  volumes:
    - ./tests/data/orders.sql:/docker-entrypoint-initdb.d/orders.sql
  environment:
    POSTGRES_USER: test_pg
    POSTGRES_PASSWORD: test_pg
    POSTGRES_DB: synmetrix_test
```

### Cube.js 负载测试

```yaml
cubejs:
  image: synmetrix/cubejs:latest
  deploy:
    replicas: 3  # 3 个副本进行负载测试
  ports:
    - 4000:4000
    - 15432:15432
    - 13306:13306
```

### 服务列表

```yaml
services:
  - redis
  - postgres_test (测试数据源)
  - postgres (元数据库)
  - cubejs (3 副本)
  - cubejs_refresh_worker
  - client
  - hasura
  - hasura_plus
  - actions
  - cubestore
  - minio
```

### 环境配置

- 环境文件：`.env` + `.test.env`
- 网络：外部网络 `synmetrix_default`
- 独立卷：`pgstorage-test-data`、`cubestore-test`、`minio-test-data`

### 已知问题

⚠️ **配置错误**：hasura 服务使用了 `.dev.env` 而不是 `.test.env`

```yaml
# docker-compose.test.yml 第 77 行
hasura:
  env_file:
    - .env
    - .dev.env  # ❌ 应该改为 .test.env
```

**建议修复**：
```yaml
hasura:
  env_file:
    - .env
    - .test.env  # ✅ 修正
```

---

## 4. docker-compose.stack.yml - 一体化生产栈

### 特点

- **极简架构**：只有 4 个容器
- **单一服务镜像**：`stack` 容器包含所有应用层服务
- **硬编码配置**：数据库凭证直接写入（适合快速部署）
- **桥接网络**：独立网络 `synmetrix_stack`
- **易部署**：适合快速部署和演示场景

### 服务架构

```
┌─────────────────────────────────────────┐
│            stack 容器                    │
│  ┌────────────────────────────────────┐ │
│  │ Nginx (8888) → Client 前端         │ │
│  │ Cube.js API (4000)                 │ │
│  │ PostgreSQL API (15432)             │ │
│  │ MySQL API (13306)                  │ │
│  │ Hasura + Actions + Client          │ │
│  └────────────────────────────────────┘ │
└─────────────────────────────────────────┘
         ↓              ↓            ↓
    ┌────────┐    ┌──────────┐  ┌──────────┐
    │ Redis  │    │ Postgres │  │Cubestore │
    └────────┘    └──────────┘  └──────────┘
```

### 服务列表

```yaml
services:
  - redis (6379)
  - postgres (5432) - 硬编码凭证
  - cubestore
  - stack (80, 4000, 15432, 13306) - 多合一容器
```

### 环境配置

- **网络**：独立桥接网络 `synmetrix_stack`
- **数据库配置**（硬编码）：
  ```yaml
  POSTGRES_USER: synmetrix_stack
  POSTGRES_PASSWORD: pg_pass
  POSTGRES_DB: synmetrix_stack
  HASURA_GRAPHQL_ADMIN_SECRET: adminsecret
  ```
- **外部密钥**（从环境变量读取）：
  - SMTP 配置
  - S3 配置

### 适用场景

- 快速演示
- 单机部署
- 开发环境快速搭建
- Docker 初学者友好

---

## 关键差异对比表

| 维度 | dev.yml | stage.yml | test.yml | stack.yml |
|------|---------|-----------|----------|-----------|
| **用途** | 本地开发 | 预发布环境 | 集成测试 | 一键部署 |
| **服务数量** | 12 | 10 | 11 | 4 |
| **镜像来源** | 本地构建 | 镜像仓库 | 镜像仓库 | 镜像仓库 |
| **热重载** | ✅ | ❌ | ❌ | ❌ |
| **反向代理** | ❌ | Traefik | ❌ | Nginx (内置) |
| **SSL/HTTPS** | ❌ | ✅ 自动 | ❌ | ❌ |
| **Docker Swarm** | ❌ | ✅ | 部分支持 | ❌ |
| **环境文件** | .dev.env | .stage.env | .test.env | 环境变量 |
| **网络** | 外部网络 | 外部网络 | 外部网络 | 桥接网络 |
| **调试端口** | 全部暴露 | 不暴露 | 部分暴露 | 不暴露 |
| **源码挂载** | ✅ | ❌ | ❌ | ❌ |
| **Cube.js 副本** | 2 (主+worker) | 1 (主+worker) | 3 + worker | 1 (内置) |
| **邮件服务** | mailhog | 外部 SMTP | 外部 SMTP | 外部 SMTP |
| **数据库迁移** | hasura_cli | 手动 | 手动 | 内置 |

---

## 网络架构对比

### 开发环境 (dev.yml)

```
外部网络: synmetrix_default
    │
    ├── redis (6379)
    ├── postgres (5435)
    ├── hasura (8080)
    ├── hasura_plus (8081)
    ├── cubejs (4000, 13306, 15432, 9231)
    ├── cubejs_refresh_worker
    ├── actions
    ├── client (80, 9055)
    ├── cubestore (3030)
    ├── minio (9000, 9001)
    ├── mailhog (1025, 8025)
    └── hasura_cli (9695, 9693)
```

### 预发布环境 (stage.yml)

```
外部网络: synmetrix_default
    │
    ├── Traefik (80→443, 15432, 13306)
    │     │
    │     ├── app.${DOMAIN} → client:8888
    │     ├── api.${DOMAIN} → hasura:8080
    │     ├── cube.${DOMAIN} → cubejs:4000
    │     ├── s3.${DOMAIN} → minio:9000
    │     └── lb.${DOMAIN} → traefik:888
    │
    ├── redis
    ├── postgres
    ├── cubejs
    ├── hasura
    ├── hasura_plus
    ├── actions
    ├── cubestore
    └── minio
```

### 测试环境 (test.yml)

```
外部网络: synmetrix_default
    │
    ├── postgres_test (测试数据源)
    ├── postgres (元数据)
    ├── redis
    ├── cubejs (副本×3)
    ├── cubejs_refresh_worker
    ├── client (80)
    ├── hasura
    ├── hasura_plus
    ├── actions
    ├── cubestore
    └── minio
```

### 一体化栈 (stack.yml)

```
桥接网络: synmetrix_stack
    │
    ├── stack (多合一)
    │     ├── Nginx:8888
    │     ├── Cubejs:4000
    │     ├── PostgreSQL API:15432
    │     └── MySQL API:13306
    │
    ├── redis (6379)
    ├── postgres (5432)
    └── cubestore
```

---

## 存储卷配置

### dev.yml
```yaml
volumes:
  pgstorage-data:     # PostgreSQL 数据
  minio-data:         # MinIO 对象存储
  .cubestore:         # Cubestore 本地目录（非 volume）
```

### stage.yml
```yaml
volumes:
  pgstorage-data:     # PostgreSQL 数据
  cubestore:          # Cubestore 数据
  minio-data:         # MinIO 对象存储
```

### test.yml
```yaml
volumes:
  pgstorage-test-data:  # PostgreSQL 元数据（测试隔离）
  cubestore-test:       # Cubestore 数据（测试隔离）
  minio-test-data:      # MinIO 数据（测试隔离）
```

### stack.yml
```yaml
volumes:
  pg_db_data:         # PostgreSQL 数据
  cube_store_data:    # Cubestore 数据
```

---

## 使用场景推荐

### 本地开发 → `docker-compose.dev.yml`

**适用于：**
- 日常开发调试
- 代码热重载
- 断点调试
- 数据库迁移测试

**启动命令：**
```bash
./cli.sh compose up -e dev
./cli.sh compose up -e dev --build  # 重新构建
```

---

### CI/CD 测试 → `docker-compose.test.yml`

**适用于：**
- 自动化集成测试
- 负载测试（3 个 cubejs 副本）
- API 端到端测试
- 性能基准测试

**启动命令：**
```bash
./cli.sh compose up -e test
./cli.sh tests stepci  # 运行集成测试
```

---

### 预发布/演示 → `docker-compose.stage.yml`

**适用于：**
- 客户演示
- UAT 测试
- 准生产环境
- Docker Swarm 集群部署

**启动命令：**
```bash
# 需要配置环境变量
export DOMAIN=example.com
export LETSENCRYPT_EMAIL=admin@example.com
export REGISTRY_HOST=registry.example.com

./cli.sh compose up -e stage
```

---

### 快速部署/Demo → `docker-compose.stack.yml`

**适用于：**
- 快速演示
- 单机部署
- 学习和试用
- 资源受限环境

**启动命令：**
```bash
docker-compose -f docker-compose.stack.yml up -d
```

---

## 已知问题和改进建议

### 问题 1：dev.yml 重复构建 ✅ 已修复

**问题描述：**
cubejs 和 cubejs_refresh_worker 使用相同 Dockerfile，会构建两次。

**解决方案：**
```yaml
cubejs:
  build:
    context: ./services/cubejs
  image: synmetrix/cubejs:dev  # 添加镜像标签

cubejs_refresh_worker:
  image: synmetrix/cubejs:dev   # 复用镜像
```

---

### 问题 2：test.yml 环境配置错误 ⚠️ 待修复

**问题描述：**
hasura 服务使用了 `.dev.env` 而不是 `.test.env`（第 77 行）

**建议修复：**
```yaml
hasura:
  env_file:
    - .env
    - .test.env  # 改为 .test.env
```

---

### 问题 3：镜像标签管理建议

**当前状态：**
- stage.yml: 使用 `latest` 标签
- test.yml: 使用 `latest` 标签
- stack.yml: 使用 `${STACK_VERSION:-latest}`

**建议改进：**
- 使用语义化版本标签（如 `v1.2.3`）
- 使用 Git commit SHA 作为标签
- 在 CI/CD 中自动打标签

---

### 问题 4：安全配置建议

**stack.yml 硬编码密钥：**
```yaml
# ❌ 不安全：硬编码密码
POSTGRES_PASSWORD: pg_pass
HASURA_GRAPHQL_ADMIN_SECRET: adminsecret
```

**建议改进：**
```yaml
# ✅ 使用环境变量
POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
HASURA_GRAPHQL_ADMIN_SECRET: ${HASURA_GRAPHQL_ADMIN_SECRET}
```

---

### 问题 5：资源限制建议

**当前状态：**
所有配置文件都没有设置资源限制。

**建议添加：**
```yaml
services:
  cubejs:
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 4G
        reservations:
          cpus: '1'
          memory: 2G
```

---

## 环境变量配置指南

### 公共环境变量（所有环境）

`.env` 文件应包含：
```bash
# 版本控制
POSTGRES_VERSION=12
CUBESTORE_VERSION=v1.3.39
HASURA_VERSION=v2.40.2
CLIENT_VERSION=latest

# 密钥（应从密钥管理器读取）
JWT_KEY=your-jwt-secret
CUBEJS_SECRET=your-cubejs-secret
HASURA_GRAPHQL_ADMIN_SECRET=your-admin-secret
```

### 开发环境变量

`.dev.env` 应包含：
```bash
NODE_ENV=development
CUBEJS_DEV_MODE=true
HASURA_GRAPHQL_DEV_MODE=true
```

### 预发布环境变量

`.stage.env` 应包含：
```bash
NODE_ENV=staging
DOMAIN=stage.example.com
LETSENCRYPT_EMAIL=devops@example.com
REGISTRY_HOST=registry.example.com
PROTOCOL=https
```

### 测试环境变量

`.test.env` 应包含：
```bash
NODE_ENV=test
CUBEJS_DB_TYPE=postgres
CUBEJS_DB_HOST=postgres_test
CUBEJS_DB_NAME=synmetrix_test
```

---

## 迁移路径建议

### 从开发到测试
```bash
# 1. 构建镜像
docker-compose -f docker-compose.dev.yml build

# 2. 打标签
docker tag synmetrix/cubejs:dev synmetrix/cubejs:latest
docker tag synmetrix/hasura-actions:dev synmetrix/hasura-actions:latest
docker tag synmetrix/app-client:dev synmetrix/app-client:latest

# 3. 启动测试环境
docker-compose -f docker-compose.test.yml up -d

# 4. 运行测试
./cli.sh tests stepci
```

### 从测试到预发布
```bash
# 1. 推送镜像到仓库
docker push registry.example.com/synmetrix/cubejs:v1.2.3
docker push registry.example.com/synmetrix/hasura-actions:v1.2.3
docker push registry.example.com/synmetrix/app-client:v1.2.3

# 2. 更新环境变量
export REGISTRY_HOST=registry.example.com
export DOMAIN=stage.example.com

# 3. 部署到预发布环境
docker stack deploy -c docker-compose.stage.yml synmetrix
```

---

## 总结

四个 docker-compose 文件各有其特定用途，形成了从开发到生产的完整部署链路：

1. **dev.yml** - 开发友好，支持热重载和调试
2. **test.yml** - 自动化测试，多副本负载测试
3. **stage.yml** - 生产级配置，自动 HTTPS 和域名路由
4. **stack.yml** - 极简部署，适合快速演示

建议：
- 开发使用 dev.yml
- CI/CD 使用 test.yml
- 预发布使用 stage.yml（配合 Docker Swarm）
- 演示/试用使用 stack.yml

**注意事项：**
- 保持环境变量文件的一致性
- 定期更新依赖版本
- 在 stage 和生产环境使用固定版本标签
- 敏感信息使用密钥管理器而非环境文件
