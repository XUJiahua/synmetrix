# Keycloak JWK_URL 兼容性改造指南

## 执行摘要

系统已配置 Keycloak 作为 IdP，使用 `HASURA_GRAPHQL_JWT_SECRET={"jwk_url": "${JWK_URL}"}` 验证令牌。但 **Actions Service** 仍然使用本地 HS256 签名生成用户令牌，这与 Keycloak RS256 不兼容。

**影响范围**：后台任务（告警、报告、文档生成）无法执行，因为 Hasura/Cubejs 拒绝 HS256 令牌。

**解决方案**：从 Keycloak 获取实际的 RS256 令牌，或禁用需要令牌的后台功能。

---

## 现状分析

### 当前的令牌流

#### 1. 前端用户流（✅ 工作正常）
```
User登录 → Keycloak → 返回RS256 access_token
              ↓
           Token → Hasura/Cubejs
              ↓
        使用 JWK_URL 验证
              ↓
           ✅ 成功
```

#### 2. 后台任务流（❌ 失败）
```
Background Task (checkAlert, etc.)
              ↓
   generateUserAccessToken(userId)  [签署 HS256]
              ↓
   Token → Hasura
              ↓
   使用 JWK_URL 验证 HS256
              ↓
   ❌ JsonWebTokenError: invalid algorithm
```

### 技术根源

| 组件 | JWT 签署方式 | 验证方式 | 兼容性 |
|-----|-----------|--------|------|
| **Actions** | 本地 HS256 (JWT_KEY) | - | ❌ 不兼容 |
| **Hasura** | - | JWK_URL (RS256) | 只接受 RS256 |
| **Cubejs** | - | JWK_URL (RS256) | 只接受 RS256 |
| **Keycloak** | RS256 | - | ✅ 标准 |

---

## 影响的功能清单

### 🔴 完全受影响（无令牌则无法工作）

#### 1. **Alert（告警）执行** `services/actions/src/rpc/checkAlert.js`
- **触发方式**：Cron scheduler（按 `alerts.schedule` 时间表）
- **功能**：
  - 定时检查告警条件
  - 查询 Cubejs 获取数据
  - 如果匹配，发送截图通知（邮件/Slack/Webhook）
- **当前状态**：❌ 失败（无法获取用户令牌）
- **必需改造**：从 Keycloak 获取令牌或禁用

#### 2. **Report（报告）执行** `services/actions/src/rpc/checkReport.js`
- **触发方式**：Cron scheduler（按 `reports.schedule` 时间表）
- **功能**：
  - 定时生成报告截图
  - 发送给收件人
- **当前状态**：❌ 失败（同上）
- **必需改造**：同上

#### 3. **Schema 文档生成** `services/actions/src/rpc/genSchemasDocs.js`
- **触发方式**：Hasura Event Trigger（新 DataSchema Version 创建时）
- **功能**：
  - 自动生成 Markdown 文档
  - 列出 Cubes、Measures、Dimensions
- **当前状态**：⚠️ 部分失败（某些 GraphQL 调用）
- **必需改造**：获取令牌或禁用自动文档生成

#### 4. **Exploration 截图** `services/actions/src/rpc/sendExplorationScreenshot.js`
- **触发方式**：Alert 或 Report 触发时
- **功能**：
  - Puppeteer 浏览器自动化
  - 生成界面截图并上传 S3
- **当前状态**：❌ 失败（依赖 checkAlert/checkReport）
- **必需改造**：同上

### 🟡 间接受影响（依赖上述功能）

- **Alert 通知**（邮件/Slack/Webhook）
- **Report 导出**
- **自动化数据分析**（依赖定时任务）

---

## 解决方案对比

### 方案 A：从 Keycloak 获取令牌（✅ 推荐）

#### 优点
- ✅ 单一令牌来源（Keycloak）
- ✅ 完全兼容 JWK_URL
- ✅ 无额外的密钥管理
- ✅ 符合 OAuth 2.0 标准
- ✅ 所有功能都能保留

#### 缺点
- ⚠️ 需要 Keycloak 配置（Token Exchange 或 Impersonation）
- ⚠️ 需要新的环境变量
- ⚠️ 增加外部依赖调用
- ⚠️ Keycloak 故障会影响后台任务

#### 技术方案

##### 1. 配置 Keycloak 客户端

需要在 Keycloak 中创建或配置一个 **service account** 客户端，用于代表用户获取令牌。

**文件**：`keycloak-config/realms/hasura-app/clients/client_backend_service.json`

```json
{
  "realm": "hasura-app",
  "clients": [
    {
      "clientId": "backend-service",
      "name": "Backend Service Account",
      "description": "Service account for Actions to obtain user tokens",
      "enabled": true,
      "clientAuthenticatorType": "client-secret",
      "secretRequired": true,
      "publicClient": false,
      "protocol": "openid-connect",
      "standardFlowEnabled": false,
      "directAccessGrantsEnabled": false,
      "serviceAccountsEnabled": true,
      "implicitFlowEnabled": false,
      "consentRequired": false,
      "attributes": {
        "standard.token.exchange.enabled": "true",
        "token.response.type.bearer.lower-case": "false",
        "client.use.lightweight.access.token.enabled": "false"
      },
      "protocolMappers": [],
      "defaultClientScopes": [
        "profile",
        "roles",
        "email"
      ]
    }
  ]
}
```

**关键配置**：
- `serviceAccountsEnabled: true` - 启用 service account
- `standard.token.exchange.enabled: true` - 启用 Token Exchange（用于代表用户获取令牌）
- `secretRequired: true` - 需要客户端密钥

##### 2. 配置 Token Exchange 权限

需要为 service account 授予 `manage-users` 和 `impersonate` 权限。

**步骤**：
1. 进入 Keycloak Admin Console
2. 选择 `backend-service` 客户端
3. 进入 "Service Account Roles" 选项卡
4. 添加以下角色：
   - `realm-management -> manage-users`
   - `realm-management -> impersonate`（如果使用 Impersonation）

或通过 `keycloak-config/realms/hasura-app/service-accounts/backend-service-roles.json` 配置：

```json
{
  "realm": "hasura-app",
  "clientId": "backend-service",
  "roles": [
    {
      "clientRole": true,
      "id": "...",
      "name": "manage-users"
    },
    {
      "clientRole": true,
      "id": "...",
      "name": "impersonate"
    }
  ]
}
```

##### 3. 环境变量配置

添加到 `.my.env`：

```bash
# Keycloak Token Exchange
KEYCLOAK_URL=http://keycloak:8080
KEYCLOAK_REALM=hasura-app
KEYCLOAK_CLIENT_ID=backend-service
KEYCLOAK_CLIENT_SECRET=<client-secret-from-keycloak>

# Token Exchange Grant Type
KEYCLOAK_TOKEN_GRANT_TYPE=urn:ietf:params:oauth:grant-type:token-exchange
```

##### 4. 实现新的 Token 获取函数

**文件**：`services/actions/src/utils/keycloakToken.js`（新建）

```javascript
import fetch from "node-fetch";

const {
  KEYCLOAK_URL,
  KEYCLOAK_REALM,
  KEYCLOAK_CLIENT_ID,
  KEYCLOAK_CLIENT_SECRET,
  KEYCLOAK_TOKEN_GRANT_TYPE = "urn:ietf:params:oauth:grant-type:token-exchange",
} = process.env;

const TOKEN_CACHE = new Map(); // 简单的 in-memory 缓存
const TOKEN_CACHE_TTL = 55 * 60 * 1000; // 55 分钟（token 通常有效 60 分钟）

/**
 * 从 Keycloak 获取用户访问令牌
 *
 * 使用 Token Exchange Grant 代表指定用户获取令牌
 *
 * @param {string} userId - 要代表的用户 ID
 * @returns {Promise<string>} 有效的 RS256 access_token
 * @throws {Error} 如果令牌获取失败
 */
export const getKeycloakUserToken = async (userId) => {
  // 检查缓存
  const cacheKey = `token_${userId}`;
  const cachedToken = TOKEN_CACHE.get(cacheKey);

  if (cachedToken && cachedToken.expiresAt > Date.now()) {
    return cachedToken.token;
  }

  try {
    // 第 1 步：获取 service account 令牌
    const clientTokenResponse = await fetch(
      `${KEYCLOAK_URL}/realms/${KEYCLOAK_REALM}/protocol/openid-connect/token`,
      {
        method: "POST",
        headers: {
          "Content-Type": "application/x-www-form-urlencoded",
        },
        body: new URLSearchParams({
          grant_type: "client_credentials",
          client_id: KEYCLOAK_CLIENT_ID,
          client_secret: KEYCLOAK_CLIENT_SECRET,
        }).toString(),
      }
    );

    if (!clientTokenResponse.ok) {
      const error = await clientTokenResponse.json();
      throw new Error(
        `Failed to get client token: ${clientTokenResponse.status} - ${JSON.stringify(error)}`
      );
    }

    const clientToken = await clientTokenResponse.json();

    // 第 2 步：使用 Token Exchange 获取用户令牌
    const userTokenResponse = await fetch(
      `${KEYCLOAK_URL}/realms/${KEYCLOAK_REALM}/protocol/openid-connect/token`,
      {
        method: "POST",
        headers: {
          "Content-Type": "application/x-www-form-urlencoded",
          Authorization: `Bearer ${clientToken.access_token}`,
        },
        body: new URLSearchParams({
          grant_type: KEYCLOAK_TOKEN_GRANT_TYPE,
          client_id: KEYCLOAK_CLIENT_ID,
          subject_token: clientToken.access_token,
          subject_token_type: "urn:ietf:params:oauth:token-type:access_token",
          requested_subject: userId,
        }).toString(),
      }
    );

    if (!userTokenResponse.ok) {
      const error = await userTokenResponse.json();
      throw new Error(
        `Failed to exchange token for user ${userId}: ${userTokenResponse.status} - ${JSON.stringify(error)}`
      );
    }

    const userToken = await userTokenResponse.json();

    // 缓存令牌（有效期 - 5 分钟缓冲）
    TOKEN_CACHE.set(cacheKey, {
      token: userToken.access_token,
      expiresAt: Date.now() + (userToken.expires_in - 300) * 1000,
    });

    return userToken.access_token;
  } catch (err) {
    throw new Error(
      `[Keycloak Token Exchange] Failed to get token for user ${userId}: ${err.message}`
    );
  }
};

export default getKeycloakUserToken;
```

##### 5. 更新 RPC 方法

**修改** `services/actions/src/rpc/checkAlert.js`：

```javascript
import getKeycloakUserToken from "../utils/keycloakToken.js";

// ... 原有代码 ...

const checkAndTriggerAlert = async (alert) => {
  // ...

  const { user_id: userId } = alert.exploration;

  // 替换原来的 generateUserAccessToken
  let authToken;
  try {
    authToken = await getKeycloakUserToken(userId);
  } catch (err) {
    await delLockData(alert);
    throw new Error(`Failed to get token for alert execution: ${err.message}`);
  }

  // ... 其余代码保持不变 ...
};
```

**修改** `services/actions/src/rpc/checkReport.js`（类似）：

```javascript
import getKeycloakUserToken from "../utils/keycloakToken.js";

// ...

export default async (_, input) => {
  const { id } = input?.payload || {};

  const queryResult = await fetchGraphQL(reportQuery, { id });
  const report = queryResult?.data?.reports_by_pk || {};

  const { exploration, delivery_type: deliveryType, delivery_config: deliveryConfig } = report;

  if (!exploration) {
    return apiError("Exploration not found");
  }

  try {
    const { user_id: userId } = exploration;

    const authToken = await getKeycloakUserToken(userId);

    const { error } = await sendExplorationScreenshot({
      deliveryType,
      deliveryConfig,
      exploration,
      name: `Report ${report.name}`,
      authToken, // 传递令牌
    });

    if (error) {
      return apiError(error);
    }

    return { error: false };
  } catch (err) {
    return apiError(err);
  }
};
```

**修改** `services/actions/src/rpc/genSchemasDocs.js`：

```javascript
import getKeycloakUserToken from "../utils/keycloakToken.js";

// ...

const generateVersionDoc = async ({ version }) => {
  const { id: versionId, user, dataschemas, branch } = version;
  const { id: userId, display_name: versionAuthorName } = user;
  const { name: branchName, datasource_id: datasourceId, id: branchId } = branch;

  // 从 Keycloak 获取令牌
  const authToken = await getKeycloakUserToken(userId);

  const metaResp = await fetchGraphQL(
    datasourceMetaQuery,
    { datasourceId, branchId },
    authToken
  );

  // ... 其余代码 ...
};
```

**修改** `services/actions/src/rpc/sendExplorationScreenshot.js`：

```javascript
import getKeycloakUserToken from "../utils/keycloakToken.js";

// ...

const getDataAndScreenshot = async (exploration, authToken) => {
  const {
    datasource_id: datasourceId,
    user_id: userId,
    id: explorationId,
  } = exploration;

  const explorationTableURL = `${APP_FRONTEND_URL}/explore/${datasourceId}/${explorationId}/?screenshot=1`;

  // 使用传入的 authToken，不再本地生成
  if (!authToken) {
    console.error("Access token is required");
    return { error: "Access token is required" };
  }

  // ... 其余代码保持不变，在 localStorage 中使用 authToken ...
};
```

##### 6. 依赖项

在 `services/actions/package.json` 中检查 `node-fetch` 已存在（应该有）。

---

### 方案 B：禁用后台任务（⚠️ 折中方案）

#### 优点
- ✅ 无需改造代码
- ✅ 立即可用
- ✅ 无额外的 Keycloak 配置

#### 缺点
- ❌ 失去关键功能
- ❌ 用户体验下降
- ❌ 需要找替代方案

#### 实施步骤

1. **禁用 Alert Cron Triggers**
   - 在 Hasura 元数据中标记 `create_cron_task_by_alert` 为 `paused: true`
   - 或删除事件触发器

2. **禁用 Report Cron Triggers**
   - 同上

3. **禁用自动文档生成**
   - 移除 `generate_dataschemas_docs` 事件触发器
   - 改为手动生成文档

4. **通知用户**
   - 这些功能在 Keycloak 迁移期间不可用

---

### 方案 C：双令牌支持（🔧 过渡方案）

#### 思路
同时支持 HS256 和 RS256 令牌。

#### 实施
修改 Hasura JWT 配置为多种类型（仅企业版支持），或在 Cubejs `checkAuth` 中添加 fallback 逻辑。

#### 风险
- ⚠️ 增加复杂性
- ⚠️ 安全考量（多个验证路径）
- ⚠️ 非官方方案

---

## 推荐实施路径

### 第 1 阶段：准备（1-2 天）

1. **创建 Keycloak 客户端配置**
   - `keycloak-config/realms/hasura-app/clients/client_backend_service.json`
   - 包含 Token Exchange 配置

2. **配置 service account 角色**
   - 添加 `manage-users` 和 `impersonate` 权限

3. **获取客户端密钥**
   - 从 Keycloak Admin Console 复制 `KEYCLOAK_CLIENT_SECRET`
   - 添加到 `.my.env`

### 第 2 阶段：开发（2-3 天）

1. **实现 `keycloakToken.js`**
   - Token Exchange 逻辑
   - 缓存机制

2. **更新 RPC 方法**
   - `checkAlert.js`
   - `checkReport.js`
   - `genSchemasDocs.js`
   - `sendExplorationScreenshot.js`

3. **单元测试**
   - 测试 Token Exchange 流程
   - 测试缓存机制
   - 测试错误处理

### 第 3 阶段：集成测试（1 day）

1. **端到端测试**
   - 创建测试 alert → 验证执行
   - 创建测试 report → 验证执行
   - 创建 version → 验证文档生成

2. **验证令牌有效性**
   - 确认令牌包含正确的 claims
   - 确认 Hasura/Cubejs 接受令牌

3. **性能测试**
   - 检查 Token Exchange 延迟
   - 验证缓存效果

### 第 4 阶段：部署（1 day）

1. **环境变量更新**
   - 更新 `.my.env` 和 `docker-compose.my.yml`

2. **Keycloak 配置部署**
   - 执行 `keycloak-config-cli` 导入新客户端

3. **代码部署**
   - 推送 Actions 服务更新
   - 重启服务

4. **监控**
   - 检查日志中是否有令牌错误
   - 验证定时任务正常执行

---

## 兼容性矩阵

| 功能 | JWK_URL | 方案 A | 方案 B | 方案 C |
|-----|--------|--------|--------|--------|
| **前端用户登录** | ✅ | ✅ | ✅ | ✅ |
| **Alert 执行** | ❌ | ✅ | ❌ | ✅ |
| **Report 执行** | ❌ | ✅ | ❌ | ✅ |
| **文档生成** | ⚠️ | ✅ | ❌ | ✅ |
| **截图生成** | ❌ | ✅ | ❌ | ✅ |
| **所有数据查询** | ✅ | ✅ | ✅ | ✅ |

---

## 风险与注意事项

### 令牌缓存风险

- **问题**：缓存的令牌可能在用户被删除或权限变更后仍然有效
- **缓解**：
  - 设置保守的缓存 TTL（55 分钟，token 60 分钟有效期）
  - 定期清空缓存
  - 监控 Keycloak 日志中的异常访问

### Keycloak 故障

- **问题**：Keycloak 宕机 → 后台任务无法执行
- **缓解**：
  - 在 Keycloak 前放置负载均衡器
  - 配置 Keycloak 集群
  - 添加 alerting

### 时间同步

- **问题**：容器之间时间差异 → JWT 验证失败
- **缓解**：
  - 配置 NTP 同步
  - 定期检查 `date` 命令输出

### 环境变量泄露

- **问题**：`KEYCLOAK_CLIENT_SECRET` 泄露 → 任何人都能代表任意用户获取令牌
- **缓解**：
  - 使用 secret management 工具（Vault, AWS Secrets Manager）
  - 限制环境变量访问权限
  - 定期轮换客户端密钥

---

## 调试指南

### 检查 Token Exchange 是否启用

```bash
# 进入 Keycloak 容器
docker exec -it synmetrix_keycloak /bin/bash

# 查询 backend-service 客户端
curl -X GET \
  http://localhost:8080/admin/realms/hasura-app/clients \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" | jq '.[] | select(.clientId=="backend-service") | .attributes["standard.token.exchange.enabled"]'
```

应该返回 `true`。

### 手动测试 Token Exchange

```bash
# 1. 获取 service account 令牌
SERVICE_TOKEN=$(curl -s -X POST \
  http://keycloak:8080/realms/hasura-app/protocol/openid-connect/token \
  -d "grant_type=client_credentials" \
  -d "client_id=backend-service" \
  -d "client_secret=$KEYCLOAK_CLIENT_SECRET" \
  -H "Content-Type: application/x-www-form-urlencoded" | jq -r '.access_token')

echo "Service Token: $SERVICE_TOKEN"

# 2. 使用 Token Exchange 获取用户令牌
USER_ID="<target-user-id>"

USER_TOKEN=$(curl -s -X POST \
  http://keycloak:8080/realms/hasura-app/protocol/openid-connect/token \
  -d "grant_type=urn:ietf:params:oauth:grant-type:token-exchange" \
  -d "client_id=backend-service" \
  -d "subject_token=$SERVICE_TOKEN" \
  -d "subject_token_type=urn:ietf:params:oauth:token-type:access_token" \
  -d "requested_subject=$USER_ID" \
  -H "Authorization: Bearer $SERVICE_TOKEN" \
  -H "Content-Type: application/x-www-form-urlencoded" | jq -r '.access_token')

echo "User Token: $USER_TOKEN"

# 3. 验证令牌（解码）
echo $USER_TOKEN | jq -R 'split(".") | .[1] | @base64d | fromjson'
```

应该看到 JWT payload 中包含 `x-hasura-user-id` 等 claims。

### 检查 Hasura 令牌验证

```bash
# 使用新令牌查询 Hasura
curl -X POST http://hasura:8080/v1/graphql \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"query":"{ users(limit: 1) { id } }"}'
```

如果令牌有效，应该返回用户数据。如果无效，会返回 JWT 错误。

---

## 预期日志

### 成功场景

```
[Keycloak Token Exchange] Getting token for user: <user-id>
[Keycloak Token Exchange] Token obtained successfully, expires in 3600s
[checkAlert] Alert check started for id: <alert-id>
[checkAlert] Data fetched successfully, 5 rows returned
[checkAlert] Alert condition matched, sending notification
```

### 失败场景

```
[Keycloak Token Exchange] Failed to get token for user <user-id>: Client authentication failed
[checkAlert] Failed to get token for alert execution: Client authentication failed
[checkAlert] Alert check failed, setting lock for retry
```

---

## 完整时间表

| 阶段 | 任务 | 时间 |
|------|------|------|
| 准备 | Keycloak 配置 | 1 天 |
| 开发 | 代码实现 + 单元测试 | 2-3 天 |
| 测试 | 集成测试 + 验证 | 1 天 |
| 部署 | 上线 + 监控 | 1 天 |
| **总计** | | **5-6 天** |

---

## 核查清单

在部署到生产环境前，确保完成以下检查：

### Keycloak 配置
- [ ] `backend-service` 客户端已创建
- [ ] Token Exchange 已启用（`standard.token.exchange.enabled: true`）
- [ ] Service account 有 `manage-users` 权限
- [ ] Service account 有 `impersonate` 权限（如果使用 Impersonation）
- [ ] 客户端密钥已生成并保存

### 代码
- [ ] `keycloakToken.js` 实现完成
- [ ] 错误处理包含日志
- [ ] 缓存机制工作正常
- [ ] 所有 RPC 方法已更新
- [ ] 单元测试通过

### 环境
- [ ] `.my.env` 包含所有 Keycloak 变量
- [ ] `docker-compose.my.yml` 未修改（使用 env vars）
- [ ] Keycloak 健康检查通过
- [ ] Hasura 连接到 Keycloak JWKS 成功

### 集成测试
- [ ] 手动测试 Token Exchange
- [ ] Alert 定时执行成功
- [ ] Report 定时执行成功
- [ ] 文档生成成功
- [ ] Hasura GraphQL 查询成功
- [ ] Cubejs 数据查询成功

### 监控
- [ ] 应用日志配置正确
- [ ] Keycloak 日志可访问
- [ ] Alerting 规则配置（Keycloak 故障、令牌错误等）

---

## FAQ

### Q: 为什么不使用密码授权流（Resource Owner Password）？

A: Resource Owner Password Flow 需要用户密码，这有安全风险且不适合后台服务。Token Exchange 和 Impersonation 都是更安全的替代方案。

### Q: 令牌缓存会导致权限变更延迟吗？

A: 是的。如果用户在后台任务执行中权限被撤销，缓存的令牌仍然有效。可以通过：
- 缩短缓存 TTL
- 增加权限变更后清空缓存的逻辑

### Q: Keycloak 故障时怎么办？

A: 定时任务会失败。需要：
- 配置 Keycloak 高可用
- 添加 alerting
- 提供手动重试机制

### Q: 能否同时支持 HS256 和 RS256？

A: 可以在 Cubejs 的 `checkAuth` 中添加 fallback 逻辑，但不推荐：
- 增加复杂性
- 安全考量
- 过渡期后应该移除

### Q: 如何测试没有实际 alert 的令牌流程？

A: 可以使用 `curl` 手动测试（见调试指南）或创建集成测试调用 `keycloakToken.js`。

---

## 参考资源

- [Keycloak Token Exchange](https://www.keycloak.org/docs/latest/securing_apps/#_token-exchange)
- [Keycloak Impersonation](https://www.keycloak.org/docs/latest/server_admin/#user-impersonation)
- [OASIS Token Exchange Spec](https://tools.ietf.org/html/rfc8693)
- [Hasura JWT Auth](https://hasura.io/docs/2.0/auth/authentication/jwt/)
- [Cubejs JWT](https://cube.dev/docs/security/api-reference#security-context)

---

## 更新历史

| 日期 | 版本 | 变更 |
|------|------|------|
| 2025-01-19 | 1.0 | 初稿 - 方案 A（推荐）完整实施指南 |

