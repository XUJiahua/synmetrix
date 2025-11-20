# Client 镜像构建与服务交互分析

## 概述

Client 服务是 Synmetrix 平台的统一入口,基于 Nginx 实现**反向代理 + 静态前端**架构,将前端应用和后端服务整合在一起。

---

## 镜像构建

### 基础信息

- **基础镜像**: `nginx:1.25.3`
- **前端源码**: 从 GitHub `mlcraft-io/client-v2` 仓库下载预编译版本
- **配置位置**: `services/client/`

### 构建参数

在 `docker-compose.dev.yml` 中定义:

```yaml
build:
  context: ./services/client/
  args:
    CLIENT_VERSION: v1.5.0
    ENABLE_MINIO_PROXY: 'true'
    HASURA_GRAPHQL_ENDPOINT: /v1/graphql
    HASURA_WS_ENDPOINT: /v1/graphql
    GRAPHQL_PLUS_SERVER_URL:
    CUBEJS_MYSQL_API_URL: localhost:13306
    CUBEJS_PG_API_URL: localhost:15432
    CUBEJS_REST_API_URL: /api/v1/load
    CUBEJS_API_DOCS_URL: http://localhost:4000/docs
```

### 构建流程 (Dockerfile)

1. **下载前端代码** (line 28):
   ```dockerfile
   RUN curl https://github.com/mlcraft-io/client-v2/releases/download/$CLIENT_VERSION/dist.tar.gz -L \
       | tar -xz -C $APP_DIR
   ```

2. **部署静态文件** (line 29-30):
   ```dockerfile
   rm -rf /usr/share/nginx/html && \
   mv $APP_DIR/dist /usr/share/nginx/html
   ```

3. **配置 Nginx** (line 32-34):
   ```dockerfile
   COPY nginx/default.conf.template /etc/nginx/templates/default.conf.template
   COPY nginx/minio-proxy.conf.template /etc/nginx/templates/minio-proxy.conf.template
   RUN if [ -z "$ENABLE_MINIO_PROXY" ] ; then rm /etc/nginx/templates/minio-proxy.conf.template ; fi
   ```

4. **环境变量转换** (line 18-25):
   将 build args 转为环境变量,供 Nginx 模板使用

---

## 服务交互架构

### 端口映射

```yaml
ports:
  - 8888:8888  # 主服务端口 (静态文件 + API 代理)
  - 9055:9055  # MinIO 存储代理
```

### 依赖服务

```yaml
depends_on:
  - hasura        # GraphQL 核心引擎
  - hasura_plus   # 认证/存储扩展
  - cubejs        # 分析引擎
  - actions       # 业务逻辑服务
```

### Nginx 路由规则

| 路径 | 代理目标 | 服务 | 功能 | 配置位置 |
|------|----------|------|------|----------|
| `/auth/*` | `hasura_plus:3000` | hasura_plus | 用户认证 | default.conf.template:4 |
| `/v1/*` | `hasura:8080` | hasura | GraphQL API + WebSocket | default.conf.template:26 |
| `/v2/*` | `hasura:8080` | hasura | GraphQL v2 API | default.conf.template:34 |
| `/console` | `hasura:8080` | hasura | Hasura 管理控制台 | default.conf.template:38 |
| `/api/v1/*` | `cubejs:4000` | cubejs | Cube.js REST API | default.conf.template:42 |
| `/` | 静态文件 | nginx | React 前端应用 | default.conf.template:8 |

**MinIO 代理** (端口 9055):
```nginx
# minio-proxy.conf.template
location / {
    proxy_pass http://minio:9000;
    proxy_set_header Host 'minio:9000';
}
```

---

## 环境变量注入机制

### 动态配置注入

Nginx 使用 `sub_filter` 在 HTML `<head>` 中注入 JavaScript 配置:

```nginx
# default.conf.template:13-23
sub_filter <head>
    '<head><script language="javascript">
    window.HASURA_WS_ENDPOINT = "$HASURA_WS_ENDPOINT";
    window.HASURA_GRAPHQL_ENDPOINT = "$HASURA_GRAPHQL_ENDPOINT";
    window.GRAPHQL_PLUS_SERVER_URL = "$GRAPHQL_PLUS_SERVER_URL";
    window.CUBEJS_MYSQL_API_URL = "$CUBEJS_MYSQL_API_URL";
    window.CUBEJS_PG_API_URL = "$CUBEJS_PG_API_URL";
    window.CUBEJS_REST_API_URL = "$CUBEJS_REST_API_URL";
    window.CUBEJS_API_DOCS_URL = "$CUBEJS_API_DOCS_URL";
    </script>';
sub_filter_once on;
```

### 工作原理

1. Dockerfile 将 build args 转为环境变量
2. Nginx 启动时,模板引擎替换 `$VARIABLE` (Nginx 1.19+ 特性)
3. 前端 JavaScript 通过 `window` 对象访问配置
4. 无需重新构建镜像即可修改配置

---

## 路由方式详解

### 方式 1: Nginx 代理路由 (容器内部)

**特点**:
- 使用 Docker 网络服务名 (`hasura`, `cubejs`)
- 用户无感知后端服务位置
- 配置硬编码在 `default.conf.template`

**示例**:
```
用户访问: http://localhost:8888/v1/graphql
↓
Nginx 代理: http://hasura:8080/v1/graphql
↓
Hasura 服务响应
```

**修改方式**: 编辑 `services/client/nginx/default.conf.template`

---

### 方式 2: 前端直连路由 (浏览器直接访问)

**特点**:
- 绝对路径配置 (如 `http://server:port`)
- 绕过 Nginx 代理
- 可通过 build args 灵活修改

**示例**:
```javascript
// 前端代码
const mysqlUrl = window.CUBEJS_MYSQL_API_URL;  // "localhost:13306"

// 用户使用 MySQL Workbench 连接
mysql -h localhost -P 13306 -u user
```

**修改方式**: 修改 `docker-compose.dev.yml` build args

---

## 配置参数说明

### 相对路径 vs 绝对路径

| 参数 | 默认值 | 类型 | 说明 |
|------|--------|------|------|
| `HASURA_GRAPHQL_ENDPOINT` | `/v1/graphql` | 相对路径 | 走 Nginx 代理 |
| `HASURA_WS_ENDPOINT` | `/v1/graphql` | 相对路径 | WebSocket 端点 |
| `CUBEJS_REST_API_URL` | `/api/v1/load` | 相对路径 | 走 Nginx 代理 |
| `CUBEJS_MYSQL_API_URL` | `localhost:13306` | 绝对路径 | 用户本地 SQL 客户端直连 |
| `CUBEJS_PG_API_URL` | `localhost:15432` | 绝对路径 | 用户本地 SQL 客户端直连 |
| `CUBEJS_API_DOCS_URL` | `http://localhost:4000/docs` | 绝对路径 | API 文档链接 |

### 环境差异配置

**开发环境** (docker-compose.dev.yml):
```yaml
HASURA_GRAPHQL_ENDPOINT: /v1/graphql        # 相对路径,走代理
CUBEJS_MYSQL_API_URL: localhost:13306       # 本地端口映射
```

**生产环境** (可能配置):
```yaml
HASURA_GRAPHQL_ENDPOINT: https://api.example.com/v1/graphql  # 外部服务
CUBEJS_MYSQL_API_URL: cubejs.example.com:3306               # 远程数据库
```

---

## 完整交互流程

### 前端应用访问流程

```
┌─────────────┐
│ 用户浏览器   │
│ localhost:8888
└──────┬──────┘
       │
┌──────▼─────────────────────────┐
│  Client (Nginx)                │
│  - 静态文件: React App         │
│  - 环境变量注入                │
└────────┬───────────────────────┘
         │ (反向代理)
         ├─→ /auth/*     → hasura_plus:3000  (认证/存储)
         ├─→ /v1/*       → hasura:8080       (GraphQL + WS)
         ├─→ /v2/*       → hasura:8080       (GraphQL v2)
         ├─→ /console    → hasura:8080       (控制台)
         └─→ /api/v1/*   → cubejs:4000       (分析引擎)
```

### SQL 客户端访问流程

```
┌─────────────────┐
│ MySQL Workbench │
│ localhost:13306 │
└────────┬────────┘
         │ (Docker 端口映射)
┌────────▼────────┐
│ cubejs:13306    │  ← Cube.js MySQL 协议端口
│ (SQL API)       │
└─────────────────┘
```

### MinIO 文件访问流程

```
┌─────────────┐
│ 前端应用    │
│ localhost:9055
└──────┬──────┘
       │
┌──────▼──────────┐
│ Nginx 代理      │
│ (9055 → 9000)   │
└──────┬──────────┘
       │
┌──────▼──────────┐
│ minio:9000      │  ← S3 兼容对象存储
└─────────────────┘
```

---

## 网络配置

所有服务通过 `synmetrix_default` 外部网络通信:

```yaml
networks:
  synmetrix_default:
    external: true
```

**优势**:
- 容器间可通过服务名互相访问
- 隔离外部网络流量
- 支持跨 compose 文件共享网络

---

## WebSocket 支持

GraphQL 订阅需要 WebSocket 支持,Nginx 配置:

```nginx
location ~ ^/v1 {
    proxy_pass http://hasura:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;        # ← 关键配置
    proxy_set_header Connection "Upgrade";         # ← 关键配置
    proxy_set_header Host $host;
}
```

**作用**:
- `Upgrade` 头支持协议升级 (HTTP → WebSocket)
- `Connection: Upgrade` 保持长连接
- 支持 Hasura GraphQL 订阅功能

---

## 设计亮点

1. **单入口架构**
   - 用户只需访问 8888 端口
   - 无需知道后端服务细节
   - 简化 CORS 配置

2. **动态配置**
   - 环境变量运行时注入
   - 无需重新构建镜像
   - 支持多环境部署

3. **开发友好**
   - MinIO 代理避免 CORS 问题
   - 热重载前端代码 (volume mount)
   - 统一的日志输出

4. **灵活路由**
   - 支持相对路径 (代理) 和绝对路径 (直连)
   - 可根据需求选择路由方式
   - 生产/开发环境配置分离

---

## 修改指南

### 场景 1: 修改后端服务地址 (代理路由)

**文件**: `services/client/nginx/default.conf.template`

```nginx
# 示例: 将 Hasura 代理到其他服务
location ~ ^/v1 {
    proxy_pass http://my-hasura-server:8080;  # ← 修改此处
}
```

### 场景 2: 修改前端直连地址 (绝对路径)

**文件**: `docker-compose.dev.yml`

```yaml
args:
  CUBEJS_MYSQL_API_URL: my-server.com:3306  # ← 修改此处
```

### 场景 3: 禁用 MinIO 代理

**文件**: `docker-compose.dev.yml`

```yaml
args:
  ENABLE_MINIO_PROXY: ''  # ← 设为空字符串
```

### 场景 4: 升级前端版本

**文件**: `docker-compose.dev.yml`

```yaml
args:
  CLIENT_VERSION: v1.11.6  # ← 修改版本号
```

---

## 常见问题

### Q1: 如何判断某个 API 是走代理还是直连?

**A**: 查看配置值:
- 相对路径 (如 `/v1/graphql`) → 走 Nginx 代理
- 绝对路径 (如 `http://host:port`) → 浏览器直连

### Q2: 修改 build args 后需要重新构建吗?

**A**: 是的,需要运行:
```bash
./cli.sh compose up -e dev --build
```

### Q3: MinIO 代理的作用是什么?

**A**:
- 开发环境避免 CORS 跨域问题
- 前端可以通过 `localhost:9055` 访问 MinIO
- 生产环境通常不启用 (直接配置 S3 域名)

### Q4: 为什么 SQL API 用 `localhost` 而不是服务名?

**A**:
- SQL 客户端运行在**用户本地机器**,不在 Docker 网络内
- 通过 docker-compose `ports` 映射访问:
  ```yaml
  ports:
    - 13306:13306  # 本地 13306 → 容器 13306
  ```

---

## 相关文件

- `docker-compose.dev.yml:186-209` - Client 服务定义
- `services/client/Dockerfile` - 镜像构建文件
- `services/client/nginx/default.conf.template` - 主 Nginx 配置
- `services/client/nginx/minio-proxy.conf.template` - MinIO 代理配置
- `.env` - 基础环境变量
- `.dev.env` - 开发环境变量

---

## 总结

Client 服务通过 Nginx 实现了**统一网关**模式:
- 对外提供单一入口 (8888)
- 对内路由到多个微服务
- 灵活支持代理和直连两种模式
- 环境变量动态注入,适应不同部署场景

这种架构简化了前端配置,提升了开发体验,是微服务架构中常见的 API Gateway 模式实践。
