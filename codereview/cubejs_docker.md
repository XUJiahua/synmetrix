# Cube.js Docker 架构深度解析

> 本文档详细分析 `@cubejs-backend/docker` 包的设计、各 Dockerfile 的差异及使用场景。
>
> 最后更新: 2025-11-03

---

## 目录

1. [包概述](#1-包概述)
2. [三种 Dockerfile 的本质区别](#2-三种-dockerfile-的本质区别)
3. [local.Dockerfile 详解](#3-localdockerfile-详解)
4. [dev.Dockerfile 详解](#4-devdockerfile-详解)
5. [dev.Dockerfile 的使用场景](#5-devdockerfile-的使用场景)
6. [最佳实践建议](#6-最佳实践建议)

---

## 1. 包概述

### 1.1 `@cubejs-backend/docker` 包的作用

这是一个**虚拟包**（virtual package），专门用于构建和分发 Cube 的 Docker 镜像。

**位置**: `packages/cubejs-docker/`

**主要功能**:

1. **提供预构建的 Docker 镜像**
   - 让用户可以快速启动 Cube 实例，无需本地安装和配置
   - 支持多个版本标签：`latest`、`<version>`、`dev` 等

2. **打包所有数据库驱动**
   - 包含了 **20+ 种数据库驱动**，包括:
     - 云数据仓库：BigQuery, Snowflake, Redshift, Databricks
     - 传统数据库：PostgreSQL, MySQL, Oracle, MSSQL
     - 现代分析引擎：ClickHouse, DuckDB, Trino, Druid
     - 其他：Elasticsearch, MongoDB BI, Hive 等

3. **提供开箱即用的运行环境**
   - 包含 Cube Server、CLI 工具
   - 配置好 Node.js 环境和所有依赖
   - 预设环境变量和端口（4000 开发界面，3000 API）

### 1.2 包结构

```
cubejs-docker/
├── latest.Dockerfile              # 生产镜像（从 npm 安装）
├── latest-debian-jdk.Dockerfile   # 包含 JDK（用于 JDBC 驱动）
├── dev.Dockerfile                 # 开发镜像（从源码编译）
├── local.Dockerfile               # 本地开发测试（yarn link）
├── testing-drivers.Dockerfile     # 驱动测试镜像
├── package.json                   # 依赖声明
└── bin/cubejs-dev                 # 开发环境 CLI
```

### 1.3 使用示例

```bash
# 快速启动 Cube 实例
docker run -d -p 3000:3000 -p 4000:4000 \
  -e CUBEJS_DB_TYPE=postgres \
  -e CUBEJS_DB_HOST=localhost \
  -v $(pwd):/cube/conf \
  cubejs/cube:latest
```

---

## 2. 三种 Dockerfile 的本质区别

### 2.1 核心差异对比表

| 特性 | latest.Dockerfile | dev.Dockerfile | local.Dockerfile |
|------|-------------------|----------------|------------------|
| **环境变量** | `NODE_ENV=production` | `NODE_ENV=development` | `NODE_ENV=production` |
| **代码来源** | npm registry | 源码编译 | 本地 yarn link |
| **构建工具** | ❌ 无 | ✅ Rust/JDK/gcc | ❌ 无（依赖 dev） |
| **构建时间** | 快（仅安装） | 慢（完整编译） | 中等（复制+链接） |
| **镜像大小** | ~500MB | ~2GB | ~800MB |
| **适用场景** | 生产部署 | CI/CD 构建 | 本地开发测试 |
| **工作目录** | `/cube` | `/cubejs` | `/cube` |
| **构建阶段** | 2 | 5 | 2 |

### 2.2 latest.Dockerfile - 生产镜像 📦

**特点**:
- **用途**: 生产环境使用
- **依赖来源**: 从 **npm registry** 安装已发布的稳定包
- **构建复杂度**: 简单，只安装依赖
- **标签**: `cubejs/cube:latest`

**构建流程**:
```dockerfile
FROM node:22.20.0-bookworm-slim AS builder
WORKDIR /cube
COPY . .
RUN yarn install --prod  # 从 npm 安装已发布的包

FROM node:22.20.0-bookworm-slim
ENV NODE_ENV=production
COPY --from=builder /cube .
```

### 2.3 dev.Dockerfile - 完整开发镜像 🔧

**特点**:
- **用途**: 从源码构建整个项目
- **依赖来源**: 复制 **monorepo 所有源码** 并编译
- **包含工具**: Rust、JDK、gcc/g++、cmake、python
- **构建过程**: 完整编译所有 packages（包括 Rust 组件）
- **标签**: `cubejs/cube:dev`
- **工作目录**: `/cubejs`（不同于其他两个的 `/cube`）

### 2.4 local.Dockerfile - 本地开发测试镜像 🔗

**特点**:
- **用途**: **本地开发时测试本地修改**的代码
- **依赖来源**: 从 **dev 镜像的构建产物** + **yarn link** 链接本地包
- **关键机制**: 使用 `yarn link:dev` 将本地开发的包链接到镜像中
- **使用场景**: 开发者修改代码后，想在 Docker 环境中测试，但不想发布到 npm

### 2.5 使用场景对比

**latest**: "我要用稳定的发布版本" → 从 npm 拉取
**dev**: "我要从零构建整个项目" → 完整源码编译
**local**: "我要测试本地修改的代码" → 链接本地构建产物

---

## 3. local.Dockerfile 详解

### 3.1 完整代码解析

#### 第 1-3 行：基础镜像选择
```dockerfile
ARG DEV_BUILD_IMAGE=cubejs/cube:build
FROM $DEV_BUILD_IMAGE AS build
FROM node:22.20.0-bookworm-slim
```

**关键设计**: 这个 Dockerfile **依赖于预先构建好的 dev 镜像**

#### 第 21-26 行：核心机制 - 复制构建产物与 yarn link

```dockerfile
# 关键注释
# Unlike latest.Dockerfile, this one doesn't install the latest cubejs from
# npm, but rather copies all the artifacts from the dev image and links them to
# the /cube directory
COPY --from=build /cubejs /cube-build
RUN cd /cube-build && yarn run link:dev
COPY package.json.local package.json
```

**这是整个文件的核心！**

**Step 1**: 复制 dev 镜像的构建产物
```dockerfile
COPY --from=build /cubejs /cube-build
```
- 从 `dev.Dockerfile` 构建的镜像中复制 `/cubejs` 目录
- 这包含了**所有已编译的 packages**（TypeScript → JavaScript）

**Step 2**: 执行 yarn link
```dockerfile
RUN cd /cube-build && yarn run link:dev
```

查看 `package.json.local`，这个脚本会执行：
```bash
yarn link @cubejs-backend/shared \
          @cubejs-backend/server \
          @cubejs-backend/server-core \
          @cubejs-backend/api-gateway \
          # ... 等 14 个核心包
```

**yarn link 做了什么？**
- 在全局 yarn 链接注册表中注册这些包
- 创建符号链接: `~/.yarn/link/@cubejs-backend/server` → `/cube-build/packages/cubejs-server`

**Step 3**: 替换 package.json
```dockerfile
COPY package.json.local package.json
```

`package.json.local` 使用 **file: 协议**：
```json
{
  "dependencies": {
    "@cubejs-backend/athena-driver": "file:/cube-build/packages/cubejs-athena-driver",
    "@cubejs-backend/bigquery-driver": "file:/cube-build/packages/cubejs-bigquery-driver",
    // ... 所有驱动都指向 /cube-build 内的本地文件
  }
}
```

**file: 协议的作用**:
- 告诉 yarn 不要从 npm 下载这些包
- 直接使用 `/cube-build/packages/` 中的本地编译版本

#### 第 39 行：安装依赖并再次链接
```dockerfile
RUN yarn install --prod && yarn cache clean && yarn link:dev
```

**详细流程**:

1. **`yarn install --prod`**:
   - 根据 `package.json.local` 安装依赖
   - 由于使用 `file:` 协议，会**创建符号链接**到 `/cube-build/packages/*`

2. **`yarn cache clean`**:
   - 清理 yarn 缓存以减小镜像体积

3. **`yarn link:dev`**（第二次执行）:
   - 再次执行 `yarn link` 确保符号链接正确
   - 这样做是因为 `yarn install` 可能覆盖之前的链接

### 3.2 完整工作流程图

```
┌─────────────────────────────────────────────────────────┐
│  Step 1: 构建 dev.Dockerfile                             │
│  docker build -f dev.Dockerfile -t cubejs/cube:build .  │
│  结果: /cubejs/packages/*/dist (编译后的 JS 代码)         │
└─────────────────────────┬───────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  Step 2: 构建 local.Dockerfile                           │
│  COPY --from=build /cubejs /cube-build                  │
│  复制编译产物到 /cube-build                               │
└─────────────────────────┬───────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  Step 3: yarn link:dev (第一次)                          │
│  在全局注册表注册核心包                                    │
│  ~/.yarn/link/@cubejs-backend/server →                   │
│    /cube-build/packages/cubejs-server                   │
└─────────────────────────┬───────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  Step 4: 替换 package.json                               │
│  使用 package.json.local（包含 file: 依赖）               │
└─────────────────────────┬───────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  Step 5: yarn install --prod                            │
│  根据 file: 协议创建符号链接:                              │
│  node_modules/@cubejs-backend/server →                  │
│    /cube-build/packages/cubejs-server                   │
└─────────────────────────┬───────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  Step 6: yarn link:dev (第二次)                          │
│  确保符号链接正确无误                                      │
└─────────────────────────┬───────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  最终结果:                                                │
│  • /cube/node_modules 中所有包都指向 /cube-build         │
│  • 运行时加载的是本地编译的代码，而不是 npm 版本            │
│  • 支持开发者修改源码后快速重新构建测试                     │
└─────────────────────────────────────────────────────────┘
```

### 3.3 使用场景示例

假设你正在开发 Cube，修改了 `@cubejs-backend/postgres-driver`:

#### 传统方式（latest.Dockerfile）:
```bash
# 需要发布到 npm 才能测试
yarn publish
docker build -f latest.Dockerfile .  # 从 npm 下载新版本
```

#### local.Dockerfile 方式:
```bash
# 1. 在本地构建 dev 镜像
docker build -f dev.Dockerfile -t cubejs/cube:build .

# 2. 构建 local 镜像（会自动链接到 dev 镜像的代码）
docker build -f local.Dockerfile -t cubejs/cube:local .

# 3. 测试你的修改
docker run -p 4000:4000 \
  -v $(pwd)/my-cube-app:/cube/conf \
  cubejs/cube:local

# 如果修改了代码，只需重新构建 dev 镜像，local 镜像会自动使用新代码！
```

---

## 4. dev.Dockerfile 详解

### 4.1 多阶段构建架构

dev.Dockerfile 使用了 **5 个构建阶段**：

```
base → prod_base_dependencies → prod_dependencies
  ↓                                      ↓
build ─────────────────────────────→ final
```

### 4.2 阶段详解

#### 阶段 1: `base` - 构建基础环境

```dockerfile
FROM node:22.20.0-bookworm-slim AS base
ARG IMAGE_VERSION=dev
ENV CUBEJS_DOCKER_IMAGE_VERSION=$IMAGE_VERSION
ENV CUBEJS_DOCKER_IMAGE_TAG=dev
ENV CI=0
```

**安装系统依赖**:
```dockerfile
RUN apt-get install -y --no-install-recommends \
   libssl3 curl cmake python3 python3.11 libpython3.11-dev \
   gcc g++ make cmake openjdk-17-jdk-headless
```

**关键工具解析**:

| 工具 | 用途 |
|------|------|
| `gcc g++ make cmake` | 编译 C/C++ 原生模块（如 node-oracledb, better-sqlite3） |
| `python3.11` | node-gyp 编译原生模块的必需依赖 |
| `openjdk-17-jdk-headless` | **编译 JDBC 驱动**（Databricks, Hive 等） |
| `curl` | 下载 Rust 工具链 |
| `libssl3` | 加密库，数据库连接需要 |

**安装 Rust 工具链** 🦀:

```dockerfile
ENV RUSTUP_HOME=/usr/local/rustup
ENV CARGO_HOME=/usr/local/cargo
ENV PATH=/usr/local/cargo/bin:$PATH

RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | \
    sh -s -- --profile minimal --default-toolchain nightly-2022-03-08 -y
```

**为什么需要 Rust？**

Cube 包含两个核心的 **Rust 组件**：

1. **CubeStore** (`rust/cubestore`): 分布式 OLAP 存储引擎
2. **CubeSQL** (`rust/cubesql`): Postgres 兼容的 SQL 接口

**复制 package.json（依赖声明文件）**:

这是 **Docker 分层缓存优化**的经典技巧：

```
📁 只有 package.json 变化 → 重新安装依赖
📝 只有源码变化 → 复用依赖层，跳过 yarn install
```

#### 阶段 2 & 3: `prod_base_dependencies` & `prod_dependencies`

处理 Databricks JDBC 驱动的特殊逻辑：

```dockerfile
FROM base AS prod_base_dependencies
COPY packages/cubejs-databricks-jdbc-driver/package.json ...
RUN mkdir packages/cubejs-databricks-jdbc-driver/bin
RUN echo '#!/usr/bin/env node' > packages/cubejs-databricks-jdbc-driver/bin/post-install
RUN yarn install --prod

FROM prod_dependencies AS prod_dependencies
COPY packages/cubejs-databricks-jdbc-driver/bin ...
RUN yarn install --prod --ignore-scripts
```

**为什么这么复杂？**

Databricks JDBC 驱动有一个 **post-install 脚本**需要下载 JDBC JAR 文件。这里的策略是：

1. **prod_base_dependencies**: 创建一个**假的 post-install** 脚本，先安装依赖
2. **prod_dependencies**: 复制真实的 bin 目录，重新安装（`--ignore-scripts` 避免重复执行）

#### 阶段 4: `build` - 核心构建阶段

**安装所有依赖（包括 devDependencies）**:
```dockerfile
FROM base AS build
RUN yarn install
```

**复制所有源码**:
```dockerfile
# Backend
COPY rust/cubestore/ rust/cubestore/
COPY rust/cubesql/ rust/cubesql/
COPY packages/cubejs-backend-shared/ packages/cubejs-backend-shared/
# ... 复制所有 40+ 个包的完整源码

# Frontend
COPY packages/cubejs-playground/ packages/cubejs-playground/
```

**核心构建步骤** 🎯:

```dockerfile
RUN yarn build
RUN yarn lerna run build

RUN find . -name 'node_modules' -type d -prune -exec rm -rf '{}' +
```

**详细说明**:

1. **`yarn build`**: 执行根目录的构建脚本
   - 编译 TypeScript 前端包（client-core, client-react 等）
   - 打包 Playground 界面
   - 使用 Rollup 打包客户端库

2. **`yarn lerna run build`**: 使用 Lerna 在所有 packages 中运行 build 脚本
   - 编译 TypeScript → JavaScript (`.ts` → `.js`)
   - **编译 Rust 项目**（cubestore, cubesql）
   - 生成类型声明文件 (`.d.ts`)
   - 编译数据库驱动

3. **删除所有 node_modules**: 删除构建依赖（TypeScript、Webpack等），只保留编译后的代码

#### 阶段 5: `final` - 最终运行镜像

```dockerfile
FROM base AS final

RUN apt-get update \
    && apt-get install -y ca-certificates python3.11 libpython3.11-dev \
    && apt-get clean

COPY --from=build /cubejs .
COPY --from=prod_dependencies /cubejs .
```

**多阶段复制**:
- 从 `build` 阶段复制**编译后的源码**（包括 Rust 二进制文件）
- 从 `prod_dependencies` 阶段复制**生产依赖的 node_modules**

**配置命令行工具**:

```dockerfile
COPY packages/cubejs-docker/bin/cubejs-dev /usr/local/bin/cubejs

ENV NODE_PATH /cube/conf/node_modules:/cube/node_modules
ENV PYTHONUNBUFFERED=1
RUN ln -s /cubejs/packages/cubejs-docker /cube
RUN ln -s /cubejs/rust/cubestore/bin/cubestore-dev /usr/local/bin/cubestore-dev
```

**特别注意**:
- 使用 `cubejs-dev` 而不是 `cubejs`
- 它指向源码中的 CLI：`require('/cubejs/packages/cubejs-cli/dist/src/index.js');`

### 4.3 完整构建流程图

```
┌────────────────────────────────────────────────────────┐
│ 阶段 1: base                                            │
│ ✓ 安装系统工具 (gcc, python, JDK)                       │
│ ✓ 安装 Rust nightly-2022-03-08                         │
│ ✓ 复制所有 package.json                                 │
└─────────────┬──────────────────────────────────────────┘
              │
    ┌─────────┴─────────┐
    │                   │
    ▼                   ▼
┌─────────────────┐  ┌──────────────────────────────┐
│ prod_base_deps  │  │ 阶段 4: build                 │
│ yarn install    │  │ ✓ yarn install (全部依赖)     │
│ --prod (1)      │  │ ✓ 复制所有源码                 │
└────┬────────────┘  │ ✓ yarn build (编译前端)        │
     │               │ ✓ yarn lerna run build        │
     ▼               │   - 编译 TypeScript           │
┌─────────────────┐  │   - 编译 Rust (cubestore)    │
│ prod_deps       │  │   - 编译所有 drivers          │
│ 复制真实 bin/   │  │ ✓ 删除 node_modules           │
│ yarn install    │  └──────────┬───────────────────┘
│ --prod (2)      │             │
└────┬────────────┘             │
     │                          │
     │        ┌─────────────────┘
     │        │
     ▼        ▼
┌────────────────────────────────────────┐
│ 阶段 5: final                           │
│ ✓ COPY --from=build (编译后代码)        │
│ ✓ COPY --from=prod_deps (运行时依赖)    │
│ ✓ 配置 cubejs-dev 命令                  │
│ ✓ 链接 cubestore-dev                   │
└────────────────────────────────────────┘
```

### 4.4 构建命令详解

```bash
# 从 cubejs-docker 目录运行
cd packages/cubejs-docker
docker build -t cubejs/cube:dev -f dev.Dockerfile ../../
```

**为什么是 `../../`？**

```
/cube (monorepo root)
  ├── packages/
  │   └── cubejs-docker/
  │       └── dev.Dockerfile  ← 从这里运行
  ├── rust/                    ↑
  │   ├── cubestore/          需要访问这些
  │   └── cubesql/            ↑
  └── package.json            ↑
```

Docker build context 是 **monorepo 根目录**，这样 Dockerfile 才能访问所有 packages 和 rust 目录！

---

## 5. dev.Dockerfile 的使用场景

### 5.1 场景 1: local.Dockerfile - 本地开发镜像

**位置**: `packages/cubejs-docker/local.Dockerfile:1-3`

```dockerfile
ARG DEV_BUILD_IMAGE=cubejs/cube:build
FROM $DEV_BUILD_IMAGE AS build
```

**关系**:
- local.Dockerfile **依赖 dev.Dockerfile 构建的镜像** 作为基础
- 从 dev 镜像复制编译产物（`/cubejs`），然后通过 `yarn link` 链接本地包
- 这是 **最直接的依赖关系**

**使用流程**:
```bash
# 先构建 dev 镜像
docker build -f dev.Dockerfile -t cubejs/cube:build ../../

# 再构建 local 镜像（会自动使用 cubejs/cube:build）
docker build -f local.Dockerfile -t cubejs/cube:local .
```

### 5.2 场景 2: GitHub Actions - Master 分支 CI/CD

**位置**: `.github/workflows/master.yml:94-134`

#### 触发条件:
- 代码推送到 `master` 分支
- 并且 **不是标签发布**（`if: needs['latest-tag-sha'].outputs.sha != github.sha`）

#### 构建流程:

```yaml
docker-image-dev:
  name: Release :dev image
  needs: [latest-tag-sha, build_native_linux]
  runs-on: ubuntu-24.04
  steps:
    - name: Push to Docker Hub
      uses: docker/build-push-action@v6
      with:
        context: ./
        file: ./packages/cubejs-docker/dev.Dockerfile
        platforms: linux/amd64
        push: true
        tags: cubejs/cube:dev
```

**关键点**:
1. **构建原生模块**: 先单独构建 `cubejs-backend-native`（Rust + Python）
2. **下载构建产物**: 将原生模块下载到 `packages/cubejs-backend-native/index.node`
3. **构建 dev 镜像**: 使用 dev.Dockerfile 构建完整镜像
4. **推送到 Docker Hub**: 发布为 `cubejs/cube:dev` 标签

**触发后续动作**:
```yaml
trigger-test-suites:
  needs: [docker-image-dev]
  steps:
    - name: Dispatch event
      inputs:
        'cube-image': 'cubejs/cube:dev'  # 使用刚构建的 dev 镜像
```

触发外部测试套件 (`cubedevinc/sql-api-test-suite`) 使用新构建的 dev 镜像进行测试。

### 5.3 场景 3: GitHub Actions - Pull Request 构建

**位置**: `.github/workflows/push.yml:52-164`

#### 触发条件:
- 提交 Pull Request
- 修改了 packages、rust 或配置文件

#### 构建流程:

```yaml
docker-dev:
  strategy:
    matrix:
      dockerfile: [dev.Dockerfile]
      include:
        - dockerfile: dev.Dockerfile
          name: Debian
          tag: tmp-dev
```

**关键区别**:
- 标签是 **`tmp-dev`**（临时开发镜像）
- **不推送到 Docker Hub**（仅用于 PR 验证）
- 用于验证 Dockerfile 语法和构建是否成功

### 5.4 场景 4: E2E 测试 - cubejs-testing

**位置**:
- `packages/cubejs-testing/src/birdbox.ts:286-292`
- `packages/cubejs-testing/DEVELOPMENT.md:48-49`

#### Birdbox 测试框架:

Birdbox 是 Cube 的 E2E 测试框架，可以在本地或 Docker 环境中运行测试。

```typescript
// packages/cubejs-testing/src/birdbox.ts
if (
  execInDir(
    '../..',
    `docker build . -f packages/cubejs-docker/dev.Dockerfile -t ${tag}`
  ) !== 0
) {
  throw new Error('[Birdbox] Docker build failed.');
}
```

#### 手动测试流程:

```bash
# 1. 构建自定义 dev 镜像
docker build . \
  -f packages/cubejs-docker/dev.Dockerfile \
  -t localhost:5000/cubejs/cube:testx

# 2. 配置测试环境变量
export BIRDBOX_CUBEJS_VERSION=testx
export BIRDBOX_CUBEJS_REGISTRY_PATH=localhost:5000/
export BIRDBOX_CYPRESS_BROWSER=chrome
export BIRDBOX_CYPRESS_TARGET=postgresql

# 3. 运行 Cypress 测试
cd packages/cubejs-testing
yarn dataset:minimal
yarn cypress:birdbox
```

**使用场景**:
- 测试新功能对 Cube Server 的影响
- 验证数据库集成（PostgreSQL, MySQL 等）
- UI/API 端到端测试

### 5.5 场景 5: 数据库驱动测试 - testing-drivers.Dockerfile

**位置**: `packages/cubejs-docker/testing-drivers.Dockerfile`

#### 关系:

这是一个 **dev.Dockerfile 的精简版本**：

| 特性 | dev.Dockerfile | testing-drivers.Dockerfile |
|------|----------------|----------------------------|
| **Rust 工具链** | ✅ 完整（nightly-2022-03-08） | ❌ 无 |
| **前端包** | ✅ 包含 Playground, Client 库 | ❌ 跳过 |
| **构建命令** | `yarn build` + `yarn lerna run build` | 仅 `yarn lerna run build` |
| **用途** | 完整开发环境 | **仅测试数据库驱动** |

#### 为什么需要单独的 testing-drivers.Dockerfile？

1. **更快的构建速度**: 跳过前端编译，节省 10-15 分钟
2. **更小的镜像体积**: 不包含 React/Vue/Angular 客户端库
3. **专注后端**: 只测试数据库连接和查询执行

### 5.6 完整依赖关系图

```
dev.Dockerfile (源头)
    │
    ├─→ 1️⃣ local.Dockerfile
    │       └─> 本地开发测试（yarn link 机制）
    │
    ├─→ 2️⃣ GitHub Actions: master.yml
    │       ├─> 构建 cubejs/cube:dev 镜像
    │       └─> 触发 SQL API 测试套件
    │
    ├─→ 3️⃣ GitHub Actions: push.yml
    │       └─> PR 验证（临时 tmp-dev 镜像）
    │
    ├─→ 4️⃣ cubejs-testing (Birdbox)
    │       ├─> E2E Cypress 测试
    │       ├─> 数据库集成测试
    │       └─> 冒烟测试 (smoke tests)
    │
    └─→ 5️⃣ testing-drivers.Dockerfile (变体)
            └─> 数据库驱动专项测试
```

### 5.7 dev 镜像的标签变体

| 标签 | 来源 | 用途 |
|------|------|------|
| `cubejs/cube:dev` | master.yml | 最新开发版（推送到 Docker Hub） |
| `cubejs/cube:tmp-dev` | push.yml | PR 临时验证（不推送） |
| `cubejs/cube:build` | 本地构建 | local.Dockerfile 的基础镜像 |
| `localhost:5000/cubejs/cube:testx` | Birdbox | E2E 测试自定义版本 |

---

## 6. 最佳实践建议

### 6.1 场景 1: 修改了核心功能，想在 Docker 中测试

```bash
# 使用 dev.Dockerfile
cd packages/cubejs-docker
docker build -f dev.Dockerfile -t cubejs/cube:mytest ../../
docker run -p 4000:4000 cubejs/cube:mytest
```

### 6.2 场景 2: 只修改了数据库驱动，想快速测试

```bash
# 使用 testing-drivers.Dockerfile（更快）
docker build -f testing-drivers.Dockerfile -t cubejs/cube:driver-test ../../
```

### 6.3 场景 3: 本地开发时想使用 yarn link

```bash
# 先构建 dev，再构建 local
docker build -f dev.Dockerfile -t cubejs/cube:build ../../
docker build -f local.Dockerfile -t cubejs/cube:local .
```

### 6.4 性能优化建议

#### Docker 分层缓存优化

1. **分离依赖安装和代码构建**
   ```dockerfile
   # 先复制 package.json
   COPY package.json .
   RUN yarn install

   # 再复制源码
   COPY . .
   RUN yarn build
   ```

2. **使用 BuildKit**
   ```bash
   DOCKER_BUILDKIT=1 docker build -f dev.Dockerfile -t cubejs/cube:dev ../../
   ```

3. **多阶段构建减小镜像体积**
   - dev.Dockerfile 已经使用了 5 阶段构建
   - 最终镜像只包含运行时依赖

#### 构建时间优化

| 优化项 | 说明 | 节省时间 |
|-------|------|---------|
| 使用 testing-drivers.Dockerfile | 跳过前端编译 | ~15 分钟 |
| 本地 Rust 缓存 | 避免重复编译 Rust | ~20 分钟 |
| Docker layer cache | 复用未变化的层 | ~30 分钟 |

### 6.5 注意事项

#### dev.Dockerfile

1. **构建时间长**: 完整编译 Rust + TypeScript + 40+ packages 需要 30-60 分钟
2. **镜像体积大**: 包含编译后的 Rust 二进制、所有驱动、前端资源
3. **Rust 版本锁定**: 使用特定的 nightly 版本，升级需谨慎
4. **需要 Docker BuildKit**: 某些特性（如多平台构建）需要 BuildKit
5. **内存需求**: Rust 编译需要至少 **4GB RAM**

#### local.Dockerfile

1. **必须先构建 dev 镜像**: local.Dockerfile 依赖 `cubejs/cube:build` 镜像
2. **file: 协议限制**: 只能指向容器内路径（`/cube-build`），不能指向宿主机
3. **yarn link 的双重调用**: 确保在 `yarn install` 前后都执行，防止链接丢失
4. **体积较大**: 因为包含了完整的编译产物（/cube-build 目录）

---

## 总结

### 核心要点

1. **@cubejs-backend/docker** 是一个虚拟包，用于构建和分发 Cube 的 Docker 镜像

2. **三种 Dockerfile 的定位**:
   - `latest.Dockerfile`: 生产环境，从 npm 安装
   - `dev.Dockerfile`: 开发环境，从源码完整编译
   - `local.Dockerfile`: 本地测试，使用 yarn link

3. **dev.Dockerfile 是整个生态的基石**:
   - 作为 local.Dockerfile 的基础镜像
   - CI/CD 的核心构建目标
   - E2E 和驱动测试的默认镜像
   - testing-drivers.Dockerfile 的设计蓝本

4. **设计理念**: 从源码完整构建一次，然后在多个场景中复用

### 架构优势

1. **模块化设计**: 不同场景使用不同 Dockerfile
2. **缓存优化**: 充分利用 Docker 分层缓存
3. **多阶段构建**: 减小最终镜像体积
4. **灵活性**: 支持本地开发、CI/CD、测试等多种场景

### 适用场景选择

- **生产部署** → latest.Dockerfile
- **CI/CD 构建** → dev.Dockerfile
- **本地开发测试** → local.Dockerfile
- **驱动测试** → testing-drivers.Dockerfile

---

**参考文档**:
- [CONTRIBUTING.md](../../CONTRIBUTING.md)
- [packages/cubejs-docker/DEVELOPMENT.md](../../packages/cubejs-docker/DEVELOPMENT.md)
- [packages/cubejs-docker/README.md](../../packages/cubejs-docker/README.md)
