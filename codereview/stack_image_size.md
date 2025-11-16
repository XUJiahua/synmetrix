# 为什么 Stack 镜像这么大？

## 镜像大小

```
synmetrix/stack:latest
总大小: 1.9GB
```

这是一个非常大的 Docker 镜像。让我们深入分析原因。

## 层级分析（按大小排序）

### 1. 系统依赖层 - 960MB（最大）

```dockerfile
RUN apt-get update -y \
  && apt-get install -y \
   wget gnupg curl git unixodbc-dev nginx gettext-base \
   apt-transport-https ca-certificates gnupg unzip lsb-release \
   python3 gcc g++ make cmake libc-bin libc6 \
   && apt-get install -y \
   postgresql-client-15 \
   chromium fonts-ipafont-gothic fonts-wqy-zenhei fonts-thai-tlwg \
   fonts-kacst fonts-freefont-ttf libxss1 \
   java-1.8.0-amazon-corretto-jdk \
   --no-install-recommends
```

**大小**: 960MB

**包含内容**:
- **Chromium 浏览器** (~300MB) - 用于生成 PDF 报表和截图
- **Java 8 JDK** (~200MB) - 用于 JDBC 驱动（Databricks 等）
- **PostgreSQL 客户端** (~50MB)
- **编译工具链** (gcc, g++, make, cmake) (~150MB)
- **各种字体** (~100MB) - 支持多语言 PDF 生成
- **Nginx** (~50MB)
- **其他工具** (~110MB)

### 2. Python 和编译工具 - 418MB

```dockerfile
RUN apt-get update \
    && apt-get install -y python3 gcc g++ make cmake libc-bin libc6
```

**大小**: 418MB

**原因**: 这是重复安装，可能是为了构建原生 Node.js 模块

### 3. Node.js 22 - 141MB

```dockerfile
RUN curl -fsSLO "https://nodejs.org/dist/v$NODE_VERSION/node-v$NODE_VERSION-linux-$ARCH.tar.xz" \
    && tar -xJf "node-v$NODE_VERSION-linux-$ARCH.tar.xz" -C /usr/local
```

**大小**: 141MB

**版本**: Node.js 22.13.1

### 4. Hasura GraphQL Engine - 139MB

```dockerfile
RUN curl -o graphql-engine https://graphql-engine-cdn.hasura.io/server/latest/linux-amd64 \
    && mv graphql-engine /usr/local/bin/ \
    && chmod +x /usr/local/bin/graphql-engine
```

**大小**: 139MB (133MB binary)

**用途**: GraphQL API 引擎

### 5. PM2 和依赖 - 84.2MB

```dockerfile
RUN yarn add pm2@5.3.0 wait-on@7.2.0 axios@1.6.7 chalk@4.1.2 serve@14.2.1
```

**大小**: 84.2MB

**包含**:
- PM2 进程管理器
- 辅助工具（wait-on, axios, chalk, serve）
- node_modules 依赖

### 6. Debian 基础镜像 - 74.8MB

```dockerfile
FROM node:22.13.1-bookworm
```

**大小**: 74.8MB

**基础**: Debian Bookworm

### 7. Client 前端静态文件 - 32MB

```dockerfile
RUN curl https://github.com/mlcraft-io/client-v2/releases/download/$CLIENT_VERSION/dist.tar.gz -L \
    | tar -xz -C $SERVICES_DIR \
    && mv $SERVICES_DIR/dist $SERVICES_DIR/client-v2
```

**大小**: 32MB

**内容**: 编译后的 React 应用

### 8. Hasura CLI - 24.6MB

```dockerfile
RUN export VERSION=${HASURA_VERSION} \
    && curl -L https://github.com/hasura/graphql-engine/raw/stable/cli/get.sh | bash
```

**大小**: 24.6MB (24MB binary)

**用途**: Hasura 迁移管理

### 9. Databricks JDBC 驱动 - 19.3MB

```dockerfile
RUN wget -q -O /tmp/DatabricksJDBC.zip "${DATABRICKS_JDBC_URL}" \
    && unzip /tmp/DatabricksJDBC.zip -d $SERVICES_DIR/cubejs/
```

**大小**: 19.3MB

**用途**: 连接 Databricks 数据仓库

### 10. Yarn 包管理器 - 7.18MB

```dockerfile
RUN curl -fsSLO "https://yarnpkg.com/downloads/$YARN_VERSION/yarn-v$YARN_VERSION.tar.gz" \
    && tar -xzf yarn-v$YARN_VERSION.tar.gz -C /opt/
```

**大小**: 7.18MB

### 11-13. 小型组件

- MLCraft 源码 (2.42MB)
- Hasura Backend Plus (2.04MB)
- 应用代码 (121MB 在 /app 目录)

## 大小分解饼图

```
总计: 1.9GB
├─ 960MB (50.5%) - 系统依赖（Chromium, Java, 字体等）
├─ 418MB (22.0%) - Python 和编译工具
├─ 141MB (7.4%)  - Node.js 运行时
├─ 139MB (7.3%)  - Hasura GraphQL Engine
├─ 121MB (6.4%)  - 应用代码和 node_modules
├─ 84MB  (4.4%)  - PM2 和工具
├─ 75MB  (3.9%)  - Debian 基础
└─ 其他 (108MB, 5.7%)
```

## 为什么需要这些组件？

### Chromium (300MB) - 必须

**用途**:
```javascript
// services/actions/src/rpc/sendExplorationScreenshot.js
// 使用 Puppeteer 生成 PDF 报表和图表截图
import puppeteer from 'puppeteer';

const browser = await puppeteer.launch({
  executablePath: '/usr/bin/chromium',
  args: ['--no-sandbox', '--disable-setuid-sandbox']
});
```

**功能**:
- 生成 PDF 报表
- 截取数据可视化图表
- 邮件报表附件

**可选性**: ⚠️ 如果不需要报表功能可以移除

### Java JDK (200MB) - 部分必须

**用途**:
```javascript
// services/cubejs/src/utils/driverFactory.js
// 支持 JDBC 驱动
if (dbType === "databricks-jdbc") {
  return new driverModule.DatabricksDriver(dbParams);
}
```

**支持的数据库**:
- Databricks
- Hive
- 其他 JDBC 数据库

**可选性**: ⚠️ 如果不连接 JDBC 数据源可以移除

### 编译工具链 (150MB) - 部分必须

**用途**:
- 编译原生 Node.js 模块
- 一些数据库驱动需要编译

**示例**:
```json
// package.json 中的原生模块
{
  "dependencies": {
    "@cubejs-backend/postgres-driver": "^1.2.3",  // 需要 node-gyp
    "ioredis": "^5.3.2",                          // 可能需要编译
    "pg": "^8.7.1"                                // 原生绑定
  }
}
```

**可选性**: ⚠️ 生产环境可以使用多阶段构建移除

### 各种字体 (100MB) - 部分必须

**用途**:
- PDF 报表中的多语言支持
- 中文、日文、泰文等字体

**可选性**: ⚠️ 如果只支持英文可以移除

### PostgreSQL 客户端 (50MB) - 可选

**用途**:
- 数据库迁移脚本
- 调试工具

**可选性**: ✅ 生产环境可以移除

### Nginx (50MB) - 必须

**用途**:
- 反向代理
- 静态文件服务（前端）
- 路由分发

**可选性**: ❌ 必须（Stack 容器的核心）

## 应用代码分析

### /app 目录结构 (121MB)

```bash
/app
├── node_modules/     # 63MB - PM2 和工具依赖
├── stack/            # 58MB - 主应用代码
│   ├── services/
│   │   ├── cubejs/      # Node.js 代码 + node_modules
│   │   ├── actions/     # Node.js 代码 + node_modules
│   │   ├── client-v2/   # 前端静态文件 (32MB)
│   │   └── hasura-backend-plus/
│   └── ...
└── ...
```

### node_modules 详细 (63MB)

主要依赖:
```
pm2/                   # 进程管理器
@pm2/agent/           # PM2 监控
wait-on/              # 启动依赖检查
axios/                # HTTP 客户端
serve/                # 静态文件服务
chalk/                # 终端颜色
```

## 优化建议

### 1. 多阶段构建（推荐）

```dockerfile
# ===== 构建阶段 =====
FROM node:22-bookworm AS builder

# 安装编译工具
RUN apt-get update && apt-get install -y \
    python3 gcc g++ make cmake

WORKDIR /build

# 复制应用代码
COPY services/ ./services/

# 构建各个服务
RUN cd services/cubejs && yarn install --production
RUN cd services/actions && yarn install --production

# ===== 运行阶段 =====
FROM node:22-bookworm-slim AS runtime

# 只安装运行时依赖（不包含编译工具）
RUN apt-get update && apt-get install -y \
    chromium \
    nginx \
    postgresql-client-15 \
    java-1.8.0-amazon-corretto-jre \  # 只装 JRE，不装 JDK
    --no-install-recommends \
    && rm -rf /var/lib/apt/lists/*

# 安装 Hasura Engine 和 CLI
RUN curl -o /usr/local/bin/graphql-engine \
    https://graphql-engine-cdn.hasura.io/server/latest/linux-amd64 \
    && chmod +x /usr/local/bin/graphql-engine

RUN export VERSION=v2.36.0 \
    && curl -L https://github.com/hasura/graphql-engine/raw/stable/cli/get.sh | bash

# 复制构建产物（不包含 node_modules 中的开发依赖）
COPY --from=builder /build/services/ /app/services/

WORKDIR /app
CMD ["pm2-runtime", "ecosystem.config.js"]
```

**预期优化**: 1.9GB → 1.3GB (节省 600MB)

### 2. 条件性包含组件

```dockerfile
# 使用构建参数控制可选组件
ARG ENABLE_REPORTING=true
ARG ENABLE_JDBC=true

# 只在需要时安装 Chromium
RUN if [ "$ENABLE_REPORTING" = "true" ]; then \
      apt-get install -y chromium fonts-ipafont-gothic; \
    fi

# 只在需要时安装 Java
RUN if [ "$ENABLE_JDBC" = "true" ]; then \
      apt-get install -y java-1.8.0-amazon-corretto-jre; \
    fi
```

**用法**:
```bash
# 最小镜像（无报表、无 JDBC）
docker build --build-arg ENABLE_REPORTING=false \
             --build-arg ENABLE_JDBC=false \
             -t synmetrix/stack:minimal .

# 标准镜像（全功能）
docker build -t synmetrix/stack:latest .
```

**预期优化**: 1.9GB → 1.2GB (节省 700MB)

### 3. 使用 Alpine 基础镜像（激进）

```dockerfile
FROM node:22-alpine AS runtime

# Alpine 使用 apk 包管理器
RUN apk add --no-cache \
    chromium \
    nginx \
    postgresql-client \
    openjdk8-jre-base

# ... 其他配置
```

**优势**: Alpine 基础镜像只有 ~5MB
**劣势**:
- 兼容性问题（使用 musl libc 而非 glibc）
- 某些原生模块可能无法工作
- 需要大量测试

**预期优化**: 1.9GB → 800MB (节省 1.1GB)

### 4. 分离服务镜像（微服务架构）

不使用 All-in-One 镜像，而是为每个服务构建单独镜像：

```yaml
# docker-compose.yml
services:
  frontend:
    image: synmetrix/frontend:latest  # ~100MB (Nginx + 静态文件)

  hasura:
    image: hasura/graphql-engine:v2.46.0  # ~140MB (官方镜像)

  cubejs:
    image: synmetrix/cubejs:latest    # ~500MB (Node + 数据库驱动)

  actions:
    image: synmetrix/actions:latest   # ~400MB (Node + Chromium)

  hasura-plus:
    image: nhost/hasura-backend-plus:latest  # ~200MB (官方镜像)
```

**总大小**: ~1.3GB (分散在多个镜像)

**优势**:
- ✓ 单独更新和扩展
- ✓ 故障隔离
- ✓ 更小的增量更新

**劣势**:
- ✗ 配置复杂
- ✗ 需要管理多个镜像

### 5. 移除不必要的组件

#### 5.1 移除 PostgreSQL 客户端（节省 50MB）

```dockerfile
# 如果不需要在容器内运行迁移脚本
# RUN apt-get install -y postgresql-client-15
```

#### 5.2 移除多余字体（节省 70MB）

```dockerfile
# 只保留必要的字体
RUN apt-get install -y \
    fonts-liberation \  # 基础英文字体
    fonts-noto-cjk      # 中日韩字体（如需要）
    # 移除其他字体包
```

#### 5.3 使用 JRE 而非 JDK（节省 100MB）

```dockerfile
# 运行时只需要 JRE
RUN apt-get install -y java-1.8.0-amazon-corretto-jre
# 而非 java-1.8.0-amazon-corretto-jdk
```

#### 5.4 清理 APT 缓存（在每个 RUN 层）

```dockerfile
RUN apt-get update \
    && apt-get install -y ... \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*
```

## 对比其他语义层产品

| 产品 | 镜像大小 | 说明 |
|------|---------|------|
| **Synmetrix Stack** | 1.9GB | All-in-One, 包含 Chromium + Java |
| **Cube.js 官方** | 600MB | 仅 Cube.js 核心 |
| **Apache Superset** | 1.2GB | BI 工具，包含 Python 生态 |
| **Metabase** | 400MB | Java 应用 |
| **Hasura 官方** | 140MB | 仅 GraphQL Engine |

**结论**: Synmetrix Stack 的大小在 All-in-One 产品中属于正常范围。

## 实际优化示例

### 优化后的 Dockerfile（推荐）

```dockerfile
# ==================== 构建阶段 ====================
FROM node:22-bookworm AS builder

# 安装构建依赖
RUN apt-get update && apt-get install -y \
    python3 gcc g++ make cmake git \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# 复制 package.json 并安装依赖
COPY services/cubejs/package*.json ./services/cubejs/
COPY services/actions/package*.json ./services/actions/
RUN cd services/cubejs && yarn install --production --frozen-lockfile
RUN cd services/actions && yarn install --production --frozen-lockfile

# 复制应用代码
COPY services/ ./services/

# ==================== 运行阶段 ====================
FROM node:22-bookworm-slim AS runtime

# 构建参数
ARG ENABLE_REPORTING=true
ARG ENABLE_JDBC=true

# 安装基础运行时依赖
RUN apt-get update && apt-get install -y \
    nginx \
    curl \
    wget \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# 条件安装 Chromium（仅报表功能需要）
RUN if [ "$ENABLE_REPORTING" = "true" ]; then \
      apt-get update && apt-get install -y \
        chromium \
        fonts-liberation \
        fonts-noto-cjk \
      && rm -rf /var/lib/apt/lists/*; \
    fi

# 条件安装 Java JRE（仅 JDBC 数据源需要）
RUN if [ "$ENABLE_JDBC" = "true" ]; then \
      wget -qO - https://apt.corretto.aws/corretto.key | apt-key add - && \
      echo "deb https://apt.corretto.aws stable main" | tee /etc/apt/sources.list.d/corretto.list && \
      apt-get update && apt-get install -y java-1.8.0-amazon-corretto-jre && \
      rm -rf /var/lib/apt/lists/*; \
    fi

# 安装 Hasura GraphQL Engine
RUN curl -o /usr/local/bin/graphql-engine \
    https://graphql-engine-cdn.hasura.io/server/latest/linux-amd64 \
    && chmod +x /usr/local/bin/graphql-engine

# 安装 Hasura CLI
RUN export VERSION=v2.36.0 && \
    curl -L https://github.com/hasura/graphql-engine/raw/stable/cli/get.sh | bash

# 复制构建产物
COPY --from=builder /build/services/ /app/services/

# 下载前端静态文件
ARG CLIENT_VERSION=v1.11.6
RUN mkdir -p /app/services/client-v2 && \
    curl -L https://github.com/mlcraft-io/client-v2/releases/download/${CLIENT_VERSION}/dist.tar.gz \
    | tar -xz -C /app/services && \
    mv /app/services/dist /app/services/client-v2

# 安装 PM2
WORKDIR /app
RUN yarn global add pm2@5.3.0

COPY ecosystem.config.js /app/
COPY docker-entrypoint.sh /usr/local/bin/

EXPOSE 8888 4000 15432 13306

ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["pm2-runtime", "ecosystem.config.js"]
```

**构建不同变体**:

```bash
# 完整版（1.3GB）
docker build -t synmetrix/stack:full .

# 无报表版（1.0GB）
docker build --build-arg ENABLE_REPORTING=false \
             -t synmetrix/stack:no-reporting .

# 最小版（800MB）
docker build --build-arg ENABLE_REPORTING=false \
             --build-arg ENABLE_JDBC=false \
             -t synmetrix/stack:minimal .
```

## 总结

### 当前镜像大小分解

```
1.9GB 总计
├─ 50% (960MB)  - Chromium, Java, 字体等系统依赖
├─ 22% (418MB)  - 编译工具链（可在多阶段构建中移除）
├─ 7%  (141MB)  - Node.js 运行时（必须）
├─ 7%  (139MB)  - Hasura GraphQL Engine（必须）
├─ 6%  (121MB)  - 应用代码和依赖（必须）
├─ 4%  (84MB)   - PM2 和工具（必须）
└─ 4%  (75MB)   - Debian 基础（必须）
```

### 优化潜力

| 优化方案 | 节省空间 | 复杂度 | 兼容性 | 推荐度 |
|---------|---------|--------|--------|--------|
| 多阶段构建 | 600MB | 低 | 高 | ⭐⭐⭐⭐⭐ |
| JRE 替代 JDK | 100MB | 低 | 高 | ⭐⭐⭐⭐⭐ |
| 移除不必要字体 | 70MB | 低 | 中 | ⭐⭐⭐⭐ |
| 条件性组件 | 500MB | 中 | 高 | ⭐⭐⭐⭐ |
| 微服务架构 | 600MB | 高 | 高 | ⭐⭐⭐ |
| Alpine 基础 | 1.1GB | 高 | 低 | ⭐⭐ |

### 最佳实践建议

1. **立即可行**: 使用多阶段构建 → 1.3GB
2. **按需定制**: 根据数据源类型选择性包含组件 → 0.8-1.3GB
3. **长期目标**: 考虑微服务架构，独立扩展 → 更灵活

### 为什么官方保持 1.9GB？

**原因**:
1. **开箱即用** - 支持所有功能，无需额外配置
2. **兼容性** - 支持所有数据源类型（包括 JDBC）
3. **完整功能** - 包含报表、PDF 导出等高级功能
4. **简化部署** - 单一镜像，降低部署复杂度

对于大多数用户，存储成本（1.9GB）远低于配置和维护成本。
