# Hasura "http exception when calling webhook" 错误分析

## 错误来源

**错误信息**: `"http exception when calling webhook"`

**报出位置**: **Hasura GraphQL Engine** (不是应用代码)

这是 Hasura 内部生成的错误消息,当 Hasura 尝试调用 webhook (Actions 或 Event Triggers) 失败时产生。

## 错误发生场景

### 1. Hasura Actions 调用失败
当 Hasura 调用 Actions 服务的 webhook 时:
```
Client → Hasura → Actions Service (webhook)
                    ↓
                  失败 (连接/超时/错误)
                    ↓
         "http exception when calling webhook"
```

### 2. Event Triggers 调用失败
当数据库事件触发 webhook 调用时:
```
Database Change → Hasura Event Trigger → Webhook
                                          ↓
                                        失败
                                          ↓
                  "http exception when calling webhook"
```

## 实际案例分析 (来自日志)

### 案例: Response Timeout

从 Hasura 日志中提取的实际错误:

```json
{
  "level": "error",
  "type": "http-log",
  "detail": {
    "operation": {
      "error": {
        "code": "unexpected",
        "error": "http exception when calling webhook",
        "internal": {
          "error": {
            "message": "Response timeout",
            "type": "http_exception",
            "request": {
              "host": "actions",
              "method": "POST",
              "path": "/rpc/fetch_dataset",
              "port": 3000,
              "responseTimeout": "ResponseTimeoutMicro 30000000"
            }
          }
        }
      }
    },
    "query_execution_time": 30.075747596
  }
}
```

**关键信息**:
- **触发 Action**: `fetch_dataset`
- **目标服务**: `actions:3000/rpc/fetch_dataset`
- **失败原因**: `Response timeout` (30秒超时)
- **执行时间**: 30.08 秒 (超过默认30秒超时)

## 常见失败原因

### 1. Response Timeout (最常见)

**场景**: Webhook 处理时间超过配置的超时时间

**Synmetrix 中的超时配置**:
```yaml
# actions.yaml
- name: fetch_dataset
  definition:
    handler: '{{ACTIONS_URL}}/rpc/fetch_dataset'
    # 默认超时: 30秒

- name: fetch_tables
  definition:
    handler: '{{ACTIONS_URL}}/rpc/fetch_tables'
    timeout: 180  # 明确设置为180秒
```

**原因**:
- Cube.js 查询执行时间过长
- 数据源响应慢
- 复杂的数据转换

**解决方案**:
```yaml
# 增加超时时间
- name: fetch_dataset
  definition:
    handler: '{{ACTIONS_URL}}/rpc/fetch_dataset'
    timeout: 60  # 或更长
```

### 2. Connection Refused

**场景**: Hasura 无法连接到 webhook 端点

**原因**:
- Actions 服务未启动
- Docker 网络配置错误
- 主机名解析失败
- 端口未开放

**日志特征**:
```json
{
  "error": {
    "message": "Connection refused",
    "request": {
      "host": "actions",
      "port": 3000
    }
  }
}
```

**Synmetrix 的网络配置**:
```yaml
# docker-compose.dev.yml
services:
  hasura:
    networks:
      - synmetrix_default

  actions:
    networks:
      - synmetrix_default
```

**验证连接**:
```bash
# 从 Hasura 容器内测试连接
docker exec synmetrix-hasura-1 curl -v http://actions:3000/healthz

# 检查服务状态
docker ps | grep actions
```

### 3. DNS/Hostname Resolution Failure

**场景**: Docker 容器无法解析主机名

**日志特征**:
```json
{
  "error": {
    "message": "does not exist (Name or service not known)"
  }
}
```

**常见错误**:
- 使用 `localhost` 而不是 Docker 服务名
- 使用 `127.0.0.1` 而不是服务名

**正确配置**:
```yaml
# ✅ 正确: 使用 Docker 服务名
handler: '{{ACTIONS_URL}}/rpc/...'
# ACTIONS_URL=http://actions:3000

# ❌ 错误: 使用 localhost
# ACTIONS_URL=http://localhost:3000
```

### 4. HTTP 500 错误

**场景**: Webhook 返回 HTTP 500 错误

**原因**:
- Actions 服务代码错误
- 未处理的异常
- 数据库连接失败

**日志特征**:
```json
{
  "error": {
    "message": "Internal Server Error",
    "status": 500
  }
}
```

### 5. Webhook 返回格式错误

**场景**: Webhook 返回的数据不符合 GraphQL schema

**原因**:
- 返回 null 而 schema 要求非空
- 缺少必需字段
- 类型不匹配

## Hasura 如何生成这个错误

### Hasura 源码逻辑 (推测)

```haskell
-- Hasura 内部伪代码
callWebhook :: WebhookConfig -> IO Response
callWebhook config = do
  result <- try (httpRequest config)
  case result of
    Left (HttpException e) ->
      -- 生成错误消息
      throwError $ ActionError
        { code = "unexpected"
        , error = "http exception when calling webhook"
        , internal = e
        }
    Right response ->
      parseResponse response
```

### 错误消息结构

```json
{
  "errors": [{
    "message": "http exception when calling webhook",
    "extensions": {
      "code": "unexpected",
      "internal": {
        "error": {
          "message": "Response timeout",  // 具体原因
          "type": "http_exception",
          "request": { ... }              // 请求详情
        }
      }
    }
  }]
}
```

## 调试步骤

### 1. 查看 Hasura 日志

```bash
# 查看实时日志
docker logs -f synmetrix-hasura-1

# 过滤 webhook 相关日志
docker logs synmetrix-hasura-1 2>&1 | grep -i "webhook"

# 查看错误日志
docker logs synmetrix-hasura-1 2>&1 | grep '"level":"error"'
```

**关键字段**:
- `operation.error.internal.error.message` - 具体错误原因
- `operation.error.internal.error.request` - 请求详情
- `query_execution_time` - 执行时间

### 2. 检查 Actions 服务状态

```bash
# 检查服务是否运行
docker ps | grep actions

# 查看 Actions 日志
docker logs -f synmetrix-actions-1

# 测试健康检查端点
curl http://localhost:3000/healthz
```

### 3. 验证网络连接

```bash
# 从 Hasura 容器内测试连接
docker exec synmetrix-hasura-1 curl -v http://actions:3000/healthz

# 检查 Docker 网络
docker network inspect synmetrix_default

# 检查服务的网络别名
docker inspect synmetrix-actions-1 | grep -A 10 Networks
```

### 4. 测试 Webhook 调用

```bash
# 模拟 Hasura 调用 Action
curl -X POST http://localhost:3000/rpc/fetch_dataset \
  -H "Content-Type: application/json" \
  -d '{
    "session_variables": {
      "x-hasura-user-id": "test-user-id",
      "x-hasura-role": "user"
    },
    "input": {
      "exploration_id": "test-id",
      "limit": 10,
      "offset": 0
    }
  }'
```

### 5. 启用 Hasura Debug 日志

```yaml
# docker-compose.dev.yml
hasura:
  environment:
    HASURA_GRAPHQL_LOG_LEVEL: debug
    HASURA_GRAPHQL_DEV_MODE: "true"
    HASURA_GRAPHQL_ENABLED_LOG_TYPES: startup, http-log, webhook-log, websocket-log
```

## Synmetrix 中的具体配置

### Actions 超时配置

```yaml
# services/hasura/metadata/actions.yaml
actions:
  - name: fetch_dataset
    definition:
      kind: ""  # 默认 synchronous
      handler: '{{ACTIONS_URL}}/rpc/fetch_dataset'
      forward_client_headers: true
      # timeout: 30  # 默认30秒,未显式设置

  - name: fetch_tables
    definition:
      handler: '{{ACTIONS_URL}}/rpc/fetch_tables'
      timeout: 180  # 显式设置180秒
```

### Event Triggers 超时配置

```yaml
# services/hasura/metadata/tables.yaml
event_triggers:
  - name: create_cron_task_by_alert
    retry_conf:
      interval_sec: 10
      num_retries: 3
      timeout_sec: 60  # Event Trigger 超时
    webhook: '{{ACTIONS_URL}}/rpc/create_cron_task_by_alert'
```

### 环境变量配置

```bash
# .env
ACTIONS_URL=http://actions:3000

# .dev.env
# Actions 服务运行在 Docker 内部网络
# 使用服务名 "actions" 而不是 "localhost"
```

## 解决方案总结

### 1. 针对 Response Timeout

**问题**: `fetch_dataset` 执行时间超过30秒

**解决方案 A**: 增加 Action 超时时间
```yaml
- name: fetch_dataset
  definition:
    handler: '{{ACTIONS_URL}}/rpc/fetch_dataset'
    timeout: 60  # 增加到60秒
```

**解决方案 B**: 优化查询性能
```javascript
// services/actions/src/rpc/fetchDataset.js
// 1. 添加查询缓存
// 2. 限制返回数据量
// 3. 使用 Cube.js 预聚合
// 4. 添加超时信号
```

**解决方案 C**: 改为异步 Action
```yaml
- name: fetch_dataset
  definition:
    kind: asynchronous  # 异步执行
    handler: '{{ACTIONS_URL}}/rpc/fetch_dataset'
    timeout: 300  # 更长的超时
```

### 2. 针对 Connection Refused

**检查清单**:
- ✅ Actions 服务是否启动: `docker ps | grep actions`
- ✅ 端口是否正确: `3000`
- ✅ 网络是否正确: `synmetrix_default`
- ✅ 环境变量是否正确: `ACTIONS_URL=http://actions:3000`
- ✅ 服务健康检查: `curl http://actions:3000/healthz`

### 3. 针对 DNS Resolution

**正确配置**:
```yaml
# docker-compose.dev.yml
services:
  hasura:
    environment:
      ACTIONS_URL: http://actions:3000  # ✅ 使用服务名
      # NOT: http://localhost:3000      # ❌ 错误
```

**Docker Desktop 特殊情况**:
```yaml
# 如果需要访问宿主机服务
ACTIONS_URL: http://host.docker.internal:3000
```

### 4. 监控和预防

**添加监控**:
```javascript
// services/actions/index.js
import responseTime from "response-time";

app.use(responseTime((req, res, time) => {
  if (time > 25000) {  // 25秒预警
    logger.warn(`Slow webhook: ${req.path} took ${time}ms`);
  }
}));
```

**添加超时处理**:
```javascript
// services/actions/src/rpc/fetchDataset.js
import timeoutSignal from "timeout-signal";

export default async (session, input, headers) => {
  const timeoutMs = 25000; // 留5秒缓冲
  const signal = timeoutSignal(timeoutMs);

  try {
    const result = await cubejsApi.load(query, { signal });
    return result;
  } catch (err) {
    if (err.name === 'AbortError') {
      return {
        error: true,
        code: 'query_timeout',
        message: 'Query execution timeout'
      };
    }
    throw err;
  }
};
```

## 最佳实践

### 1. 合理设置超时

```yaml
# 快速操作 (CRUD)
- name: create_team
  timeout: 10

# 中等操作 (数据查询)
- name: fetch_dataset
  timeout: 60

# 长时间操作 (批量处理)
- name: fetch_tables
  timeout: 180
```

### 2. 使用异步 Action

对于可能超时的操作:
```yaml
- name: generate_report
  definition:
    kind: asynchronous
    handler: '{{ACTIONS_URL}}/rpc/generate_report'
    timeout: 300
```

### 3. 添加健康检查

```javascript
// services/actions/index.js
app.get("/healthz", (req, res) => {
  // 检查依赖服务
  const checks = await Promise.all([
    checkDatabase(),
    checkRedis(),
    checkCubejs()
  ]);

  if (checks.every(c => c.ok)) {
    return res.json({ status: "ok" });
  }
  return res.status(503).json({ status: "error" });
});
```

### 4. 实现熔断机制

```javascript
// 当服务不可用时快速失败
let circuitOpen = false;

app.post("/rpc/:method", async (req, res) => {
  if (circuitOpen) {
    return res.status(503).json({
      code: "service_unavailable",
      message: "Service temporarily unavailable"
    });
  }

  try {
    // 处理请求...
  } catch (err) {
    if (isRepeatedError(err)) {
      circuitOpen = true;
      setTimeout(() => { circuitOpen = false; }, 60000);
    }
    throw err;
  }
});
```

### 5. 日志和监控

```javascript
// 记录所有 webhook 调用
app.use((req, res, next) => {
  const start = Date.now();
  res.on('finish', () => {
    const duration = Date.now() - start;
    logger.info({
      method: req.method,
      path: req.path,
      status: res.statusCode,
      duration,
      userId: req.body?.session_variables?.['x-hasura-user-id']
    });
  });
  next();
});
```

## 参考资料

- [Hasura Actions 文档](https://hasura.io/docs/latest/actions/overview/)
- [Hasura Event Triggers 文档](https://hasura.io/docs/latest/event-triggers/overview/)
- [GitHub Issue #6537](https://github.com/hasura/graphql-engine/issues/6537)
- [GitHub Issue #9012](https://github.com/hasura/graphql-engine/issues/9012)

## 总结

**"http exception when calling webhook"** 错误:

1. **来源**: Hasura GraphQL Engine 内部生成
2. **触发**: Actions 或 Event Triggers 调用 webhook 失败
3. **常见原因**:
   - Response Timeout (最常见)
   - Connection Refused
   - DNS Resolution Failure
   - HTTP 500 错误
4. **调试**: 查看 Hasura 日志中的 `internal.error.message` 字段
5. **解决**:
   - 增加超时时间
   - 优化查询性能
   - 使用异步 Action
   - 检查网络配置

**Synmetrix 中的具体问题**: `fetch_dataset` action 在查询大数据集时超过30秒默认超时,需要增加 `timeout` 配置或优化查询。
