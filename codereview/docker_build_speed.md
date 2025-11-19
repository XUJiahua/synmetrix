# Docker Build Speed 优化方案

## 问题

在 `docker-compose.my.yml` 中，`cubejs` 和 `cubejs_refresh_worker` 两个服务使用相同的镜像构建配置（同一个 context 和 Dockerfile），但 Docker Compose 会分别构建两次，造成时间和资源浪费。

## 解决方案

### 方案选择：使用共享镜像 + 指定镜像名称

在 `docker-compose.my.yml` 中进行如下改造：

#### 1. cubejs 服务

添加 `image` 字段指定镜像名称，保留 `build` 配置：

```yaml
cubejs:
  build:
    context: ./services/cubejs
    dockerfile: Dockerfile.oracle
  image: synmetrix/cubejs:dev
  restart: always
  command: yarn start.dev
  # ... 其他配置
```

#### 2. cubejs_refresh_worker 服务

移除 `build` 配置，只使用镜像：

```yaml
cubejs_refresh_worker:
  # reuse cubejs image
  image: synmetrix/cubejs:dev
  restart: always
  command: yarn start.dev
  # ... 其他配置，保留 depends_on: - cubejs
```

### 工作原理

1. `cubejs` 服务首先构建生成镜像 `synmetrix/cubejs:dev`
2. `cubejs_refresh_worker` 直接使用该已构建的镜像，不再重复构建
3. 两个容器共用同一个镜像，但运行独立的容器实例

### 效果

- **构建次数**：从 2 次减少到 1 次
- **构建时间**：显著降低（取决于 Dockerfile 复杂度）
- **磁盘空间**：不增加额外占用（相同镜像只存储一份）

## 附加：本地镜像标签管理

如果本地已有 `synmetrix-cubejs` 镜像，可以为其创建新标签而无需重新构建：

```bash
# 创建新标签
docker tag synmetrix-cubejs synmetrix/cubejs:dev

# 验证
docker images | grep synmetrix/cubejs
```

这样做的好处：
- 无需重新构建，即时生效
- 新标签与原镜像共享相同 image ID
- 不占用额外磁盘空间

## 附加：关于 yarn install 的警告

在执行 `yarn install` 时，可能出现以下警告和错误，**这些不影响最终使用**：

```
warning @cubejs-backend/snowflake-driver > snowflake-sdk > ... has unmet peer dependency
warning Error running install script for optional dependency: "java"
```

### 原因分析

1. **snowflake-driver peer dependency 警告**
   - Snowflake 是可选的数据库驱动
   - 除非使用 Snowflake 数据源，否则不会被加载
   - 即使加载也只会功能降级，不会导致应用崩溃

2. **java 模块编译失败**
   - `java` 是可选依赖（optional dependency）
   - Cube.js 使用它支持某些驱动程序（如 Druid、Presto）
   - 环境中没有 Java Home 导致编译失败，但 yarn 仍能成功完成（显示 `Done in xxx.xxs`）

### 结论

✅ **安全忽略** - 这些都是可选依赖，不影响核心功能。如果真有问题，yarn 会以失败状态退出。
