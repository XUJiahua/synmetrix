# Keycloak 认证迁移方案

## 📋 概述

本文档详细说明如何将 Synmetrix 的用户认证系统从 **Hasura Backend Plus** 迁移到 **Keycloak**，并保持与现有 Hasura GraphQL 权限系统的兼容性。

---

## 目录

1. [当前认证架构分析](#1-当前认证架构分析)
2. [Keycloak 架构设计](#2-keycloak-架构设计)
3. [数据库 Schema 调整](#3-数据库-schema-调整)
4. [JWT Token 集成](#4-jwt-token-集成)
5. [用户同步策略](#5-用户同步策略)
6. [代码修改清单](#6-代码修改清单)
7. [迁移步骤](#7-迁移步骤)
8. [风险评估与缓解](#8-风险评估与缓解)
9. [回滚方案](#9-回滚方案)
10. [参考资料](#10-参考资料)

---

## 1. 当前认证架构分析

### 1.1 现有组件

**认证服务**: Hasura Backend Plus (nhost/hasura-backend-plus:2.7.1)
- 端口: 8081
- 职责: 用户注册、登录、JWT 签发、OAuth 集成、文件存储

**认证数据库表**:

```
auth schema:
├── accounts              # 认证账户（email, password_hash, MFA）
├── account_providers     # OAuth 提供商关联（GitHub, Google 等）
├── providers             # 支持的 OAuth 提供商列表
├── refresh_tokens        # JWT 刷新令牌
├── account_roles         # 用户角色关联
└── roles                 # 角色定义（user, anonymous, me）

public schema:
└── users                 # 用户基本信息（display_name, avatar_url）
    └── id (PK) ←──────  auth.accounts.user_id (FK, UNIQUE)
```

### 1.2 认证流程

```
┌──────────────┐
│  Frontend    │
└──────┬───────┘
       │ 1. POST /auth/login (email, password)
       ▼
┌──────────────────┐
│ hasura_plus:8081 │
└──────┬───────────┘
       │ 2. 验证 password_hash
       │ 3. 生成 JWT (包含 hasura claims)
       ▼
┌──────────────┐
│  Frontend    │ ← JWT Token
└──────┬───────┘
       │ 4. GraphQL Request + JWT
       ▼
┌──────────────┐
│ Hasura:8080  │
└──────┬───────┘
       │ 5. 验证 JWT signature
       │ 6. 提取 x-hasura-user-id
       │ 7. 应用 Row Level Security
       ▼
┌──────────────┐
│  PostgreSQL  │
└──────────────┘
```

### 1.3 JWT Token 结构

**当前 JWT Claims** (生成于 `services/actions/src/utils/jwt.js`):

```json
{
  "https://hasura.io/jwt/claims": {
    "x-hasura-user-id": "bd254cd6-ada3-4803-88ec-a47749459169",
    "x-hasura-allowed-roles": ["user"],
    "x-hasura-default-role": "user"
  },
  "iss": "services:actions",
  "aud": "services:hasura",
  "sub": "bd254cd6-ada3-4803-88ec-a47749459169",
  "iat": 1234567890,
  "exp": 1234571490
}
```

### 1.4 关键依赖点

| 组件 | 依赖 | 文件位置 |
|------|------|----------|
| Hasura 权限系统 | `x-hasura-user-id` claim | `services/hasura/metadata/tables.yaml` |
| Cube.js 认证 | JWT 验证 + userId | `services/cubejs/src/utils/checkAuth.js:48-53` |
| Actions RPC | JWT 生成 | `services/actions/src/utils/jwt.js:8-36` |
| SQL API 认证 | sql_credentials.user_id | `services/cubejs/src/utils/checkSqlAuth.js:38-50` |
| 用户查询 | users 表关联 | `services/cubejs/src/utils/dataSourceHelpers.js:127-154` |

---

## 2. Keycloak 架构设计

### 2.1 新架构图

```
┌──────────────┐
│  Frontend    │
└──────┬───────┘
       │ 1. Redirect to Keycloak
       ▼
┌─────────────────────┐
│  Keycloak:8443      │
│  (OIDC Provider)    │
└──────┬──────────────┘
       │ 2. 用户登录
       │ 3. 返回 ID Token + Access Token
       ▼
┌──────────────┐
│  Frontend    │ ← JWT Tokens
└──────┬───────┘
       │ 4. GraphQL Request + Access Token
       ▼
┌──────────────┐
│ Hasura:8080  │
│ (JWT Mode)   │
└──────┬───────┘
       │ 5. 从 Keycloak JWKS 验证 JWT
       │ 6. 提取 x-hasura-* claims
       │ 7. 应用 Row Level Security
       ▼
┌──────────────┐      ┌──────────────────┐
│  PostgreSQL  │ ←────│ User Sync Worker │ ← Keycloak Events
└──────────────┘      └──────────────────┘
       ▲
       │ 定期同步用户数据
       ▼
┌─────────────────────┐
│  Keycloak:8443      │
│  (Admin REST API)   │
└─────────────────────┘
```

### 2.2 Keycloak Realm 配置

**Realm 名称**: `synmetrix`

**Client 配置**:
- Client ID: `synmetrix-frontend`
- Client Protocol: `openid-connect`
- Access Type: `public` (SPA) 或 `confidential` (有后端)
- Valid Redirect URIs: `http://localhost:9055/*`, `https://yourdomain.com/*`
- Web Origins: `+` (允许 CORS)

**Client Scopes**:
```yaml
synmetrix-hasura:
  type: optional
  protocol: openid-connect
  mappers:
    - name: hasura-user-id
      protocol: openid-connect
      protocolMapper: oidc-usermodel-attribute-mapper
      config:
        user.attribute: id
        claim.name: https://hasura.io/jwt/claims.x-hasura-user-id
        jsonType.label: String
        id.token.claim: true
        access.token.claim: true

    - name: hasura-default-role
      protocol: openid-connect
      protocolMapper: oidc-hardcoded-claim-mapper
      config:
        claim.name: https://hasura.io/jwt/claims.x-hasura-default-role
        claim.value: user
        jsonType.label: String
        id.token.claim: true
        access.token.claim: true

    - name: hasura-allowed-roles
      protocol: openid-connect
      protocolMapper: oidc-hardcoded-claim-mapper
      config:
        claim.name: https://hasura.io/jwt/claims.x-hasura-allowed-roles
        claim.value: ["user"]
        jsonType.label: JSON
        id.token.claim: true
        access.token.claim: true
```

### 2.3 Docker Compose 配置

**新增 Keycloak 服务**:

```yaml
services:
  keycloak:
    image: quay.io/keycloak/keycloak:23.0
    container_name: synmetrix-keycloak
    environment:
      KEYCLOAK_ADMIN: admin
      KEYCLOAK_ADMIN_PASSWORD: ${KEYCLOAK_ADMIN_PASSWORD}
      KC_DB: postgres
      KC_DB_URL: jdbc:postgresql://postgres:5432/keycloak
      KC_DB_USERNAME: ${POSTGRES_USER}
      KC_DB_PASSWORD: ${POSTGRES_PASSWORD}
      KC_HOSTNAME: localhost
      KC_HTTP_PORT: 8080
      KC_HTTPS_PORT: 8443
      KC_PROXY: edge
    command:
      - start-dev
    ports:
      - "8443:8443"
      - "8082:8080"  # Keycloak admin console
    depends_on:
      - postgres
    networks:
      - synmetrix-network
```

**移除 hasura_plus 服务**:

```yaml
# docker-compose.dev.yml
# 注释或删除以下部分
# hasura_plus:
#   build:
#     context: ./scripts/containers/hasura-backend-plus
#   ...
```

---

## 3. 数据库 Schema 调整

### 3.1 保留的表

**public.users** - 完全保留，不做修改

```sql
-- 保持现有结构
CREATE TABLE public.users (
    id uuid DEFAULT public.gen_random_uuid() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    display_name text,
    avatar_url text,
    PRIMARY KEY (id)
);
```

### 3.2 新增字段（可选）

为了存储 Keycloak 用户 ID，可以添加字段：

```sql
-- 新增迁移文件
ALTER TABLE public.users
ADD COLUMN keycloak_id uuid UNIQUE;

ALTER TABLE public.users
ADD COLUMN keycloak_sub text UNIQUE;  -- Keycloak subject (username or UUID)

CREATE INDEX idx_users_keycloak_id ON public.users(keycloak_id);
```

**用途**:
- `keycloak_id`: 存储 Keycloak 内部 UUID
- `keycloak_sub`: 存储 Keycloak token 的 `sub` claim，用于快速查找

### 3.3 废弃的表（可以保留用于审计）

**auth schema** - 不再主动使用，但可以保留历史数据

```sql
-- 选项 1: 保留但标记为废弃
COMMENT ON SCHEMA auth IS 'DEPRECATED: Migrated to Keycloak, kept for audit purposes';

-- 选项 2: 归档后删除（生产环境慎用）
-- CREATE SCHEMA auth_archive;
-- ALTER TABLE auth.accounts SET SCHEMA auth_archive;
-- ... (迁移其他表)
-- DROP SCHEMA auth CASCADE;  -- 仅在确认数据已备份后执行
```

### 3.4 新增用户同步表

用于跟踪 Keycloak 用户同步状态：

```sql
CREATE TABLE public.keycloak_user_sync (
    id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    keycloak_id uuid NOT NULL,
    last_sync_at timestamp with time zone DEFAULT now(),
    sync_status text CHECK (sync_status IN ('synced', 'pending', 'failed')),
    error_message text,
    UNIQUE(keycloak_id)
);

CREATE INDEX idx_keycloak_user_sync_user_id ON public.keycloak_user_sync(user_id);
CREATE INDEX idx_keycloak_user_sync_status ON public.keycloak_user_sync(sync_status);
```

---

## 4. JWT Token 集成

### 4.1 Hasura JWT 配置

**环境变量修改** (`.env` 文件):

```bash
# 旧配置（删除或注释）
# HASURA_GRAPHQL_JWT_SECRET='{"type":"HS256","key":"your-secret-key"}'

# 新配置 - 使用 Keycloak JWKS
HASURA_GRAPHQL_JWT_SECRET='{
  "type": "RS256",
  "jwk_url": "http://keycloak:8080/realms/synmetrix/protocol/openid-connect/certs",
  "claims_namespace": "https://hasura.io/jwt/claims",
  "claims_format": "json",
  "audience": "synmetrix-frontend",
  "issuer": "http://localhost:8082/realms/synmetrix"
}'
```

**关键参数说明**:
- `type`: 改为 `RS256`（Keycloak 默认使用 RSA 签名）
- `jwk_url`: Keycloak 的公钥端点，Hasura 会自动获取公钥验证 JWT
- `claims_namespace`: 保持 `https://hasura.io/jwt/claims`，与现有配置一致
- `audience`: 必须匹配 Keycloak Client ID
- `issuer`: Keycloak realm 的 issuer URL

### 4.2 Cube.js JWT 验证调整

**当前代码** (`services/cubejs/src/utils/checkAuth.js:48-53`):

```javascript
// 旧代码 - 使用对称密钥 HS256
try {
  jwtDecoded = jwt.verify(authToken, JWT_KEY, {
    algorithms: [JWT_ALGORITHM],  // HS256
  });
} catch (err) {
  throw err;
}
```

**新代码** - 使用 JWKS 验证:

```javascript
import jwksClient from 'jwks-rsa';
import jwt from 'jsonwebtoken';

// 初始化 JWKS 客户端
const jwksClientInstance = jwksClient({
  jwksUri: process.env.KEYCLOAK_JWKS_URL ||
           'http://keycloak:8080/realms/synmetrix/protocol/openid-connect/certs',
  cache: true,
  cacheMaxAge: 600000,  // 10 分钟缓存
  rateLimit: true,
  jwksRequestsPerMinute: 10
});

// 获取签名密钥的函数
const getKey = (header, callback) => {
  jwksClientInstance.getSigningKey(header.kid, (err, key) => {
    if (err) {
      callback(err);
      return;
    }
    const signingKey = key.getPublicKey();
    callback(null, signingKey);
  });
};

// 验证 JWT
const checkAuth = async (req) => {
  const authHeader = req.headers.authorization;

  if (!authHeader) {
    throw new Error("Provide Authorization token");
  }

  let authToken;
  if (authHeader.startsWith("Bearer ")) {
    authToken = authHeader.split(" ")[1];
  } else {
    authToken = authHeader;
  }

  if (!authToken) {
    throw new Error("Provide Authorization token");
  }

  // 使用 JWKS 验证 JWT
  let jwtDecoded;
  try {
    jwtDecoded = await new Promise((resolve, reject) => {
      jwt.verify(authToken, getKey, {
        algorithms: ['RS256'],
        audience: process.env.KEYCLOAK_CLIENT_ID || 'synmetrix-frontend',
        issuer: process.env.KEYCLOAK_ISSUER ||
                'http://localhost:8082/realms/synmetrix'
      }, (err, decoded) => {
        if (err) reject(err);
        else resolve(decoded);
      });
    });
  } catch (err) {
    throw new Error(`JWT verification failed: ${err.message}`);
  }

  // 从 Keycloak token 提取 Hasura claims
  const hasuraClaims = jwtDecoded?.['https://hasura.io/jwt/claims'] || {};
  const userId = hasuraClaims['x-hasura-user-id'];

  if (!userId) {
    throw new Error('Missing x-hasura-user-id claim in JWT');
  }

  // ... 后续代码保持不变
  const dataSourceId = req.headers["x-hasura-datasource-id"];
  const branchId = req.headers["x-hasura-branch-id"];
  const branchVersionId = req.headers["x-hasura-branch-version-id"];

  const user = await findUser({ userId });

  if (!user.dataSources?.length || !user.members?.length) {
    throw new Error(`404: user "${userId}" not found`);
  }

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

**新增依赖** (`services/cubejs/package.json`):

```json
{
  "dependencies": {
    "jwks-rsa": "^3.1.0"
  }
}
```

### 4.3 前端 Token 获取

**使用 Keycloak JavaScript Adapter**:

```javascript
// 安装依赖
// npm install keycloak-js

import Keycloak from 'keycloak-js';

const keycloak = new Keycloak({
  url: 'http://localhost:8082/',
  realm: 'synmetrix',
  clientId: 'synmetrix-frontend'
});

// 初始化
keycloak.init({
  onLoad: 'login-required',
  checkLoginIframe: false
}).then(authenticated => {
  if (authenticated) {
    // 获取 Access Token
    const token = keycloak.token;

    // 在 GraphQL 请求中使用
    const client = new ApolloClient({
      uri: 'http://localhost:8080/v1/graphql',
      headers: {
        Authorization: `Bearer ${token}`
      }
    });
  }
});

// 自动刷新 Token
setInterval(() => {
  keycloak.updateToken(70).then(refreshed => {
    if (refreshed) {
      console.log('Token refreshed');
    }
  }).catch(() => {
    console.error('Failed to refresh token');
    keycloak.login();
  });
}, 60000);  // 每分钟检查一次
```

---

## 5. 用户同步策略

### 5.1 同步方向

**Keycloak → Synmetrix (public.users)**

```
┌─────────────────┐
│  Keycloak       │
│  (Source)       │
└────────┬────────┘
         │ User Events
         │ (CREATE, UPDATE, DELETE)
         ▼
┌─────────────────────────┐
│  Sync Worker Service    │
│  (新增 Node.js 服务)    │
└────────┬────────────────┘
         │ GraphQL Mutations
         ▼
┌─────────────────┐
│  Hasura:8080    │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  public.users   │
└─────────────────┘
```

### 5.2 同步触发方式

#### 方式 1: Keycloak Event Listener（推荐）

**实现步骤**:

1. **开发 Keycloak SPI Extension**

创建自定义 Event Listener:

```java
// keycloak-sync-extension/src/main/java/io/synmetrix/keycloak/SynmetrixEventListenerProvider.java
package io.synmetrix.keycloak;

import org.keycloak.events.Event;
import org.keycloak.events.EventListenerProvider;
import org.keycloak.events.EventType;
import org.keycloak.events.admin.AdminEvent;
import org.keycloak.models.KeycloakSession;
import org.keycloak.models.UserModel;

import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;

public class SynmetrixEventListenerProvider implements EventListenerProvider {

    private final KeycloakSession session;
    private static final String SYNC_ENDPOINT =
        System.getenv("SYNMETRIX_SYNC_URL");

    public SynmetrixEventListenerProvider(KeycloakSession session) {
        this.session = session;
    }

    @Override
    public void onEvent(Event event) {
        // 监听用户注册事件
        if (event.getType() == EventType.REGISTER) {
            syncUser(event.getUserId(), "CREATE");
        }
    }

    @Override
    public void onEvent(AdminEvent adminEvent, boolean includeRepresentation) {
        // 监听管理员操作（更新、删除用户）
        if (adminEvent.getResourceType().toString().equals("USER")) {
            String userId = extractUserId(adminEvent.getResourcePath());

            switch (adminEvent.getOperationType()) {
                case CREATE:
                    syncUser(userId, "CREATE");
                    break;
                case UPDATE:
                    syncUser(userId, "UPDATE");
                    break;
                case DELETE:
                    syncUser(userId, "DELETE");
                    break;
            }
        }
    }

    private void syncUser(String keycloakUserId, String action) {
        try {
            UserModel user = session.users().getUserById(
                session.getContext().getRealm(),
                keycloakUserId
            );

            // 构建同步请求
            URL url = new URL(SYNC_ENDPOINT + "/sync-user");
            HttpURLConnection conn = (HttpURLConnection) url.openConnection();
            conn.setRequestMethod("POST");
            conn.setRequestProperty("Content-Type", "application/json");
            conn.setDoOutput(true);

            String jsonPayload = String.format(
                "{\"keycloak_id\":\"%s\",\"email\":\"%s\",\"username\":\"%s\"," +
                "\"first_name\":\"%s\",\"last_name\":\"%s\",\"action\":\"%s\"}",
                keycloakUserId,
                user.getEmail(),
                user.getUsername(),
                user.getFirstName(),
                user.getLastName(),
                action
            );

            OutputStream os = conn.getOutputStream();
            os.write(jsonPayload.getBytes());
            os.flush();

            int responseCode = conn.getResponseCode();
            if (responseCode != 200) {
                // 记录错误日志
                System.err.println("Sync failed: " + responseCode);
            }

        } catch (Exception e) {
            e.printStackTrace();
        }
    }

    @Override
    public void close() {
        // Cleanup if needed
    }

    private String extractUserId(String resourcePath) {
        // Extract user ID from path like "users/abc-123-def"
        String[] parts = resourcePath.split("/");
        return parts[parts.length - 1];
    }
}
```

2. **部署 Extension 到 Keycloak**

```dockerfile
# Dockerfile for custom Keycloak
FROM quay.io/keycloak/keycloak:23.0

# 复制编译好的 JAR
COPY keycloak-sync-extension/target/synmetrix-event-listener.jar \
     /opt/keycloak/providers/

# 构建 Keycloak
RUN /opt/keycloak/bin/kc.sh build

ENTRYPOINT ["/opt/keycloak/bin/kc.sh", "start-dev"]
```

3. **在 Keycloak Admin Console 启用**

- 登录 Keycloak Admin Console
- Realm Settings → Events → Event Listeners
- 添加 `synmetrix-sync-listener`

#### 方式 2: Webhook 服务（简化版）

**新增 Sync Worker 服务**:

```yaml
# docker-compose.dev.yml
services:
  keycloak_sync:
    build: ./services/keycloak-sync
    container_name: synmetrix-keycloak-sync
    environment:
      HASURA_GRAPHQL_URL: http://hasura:8080/v1/graphql
      HASURA_ADMIN_SECRET: ${HASURA_GRAPHQL_ADMIN_SECRET}
      KEYCLOAK_URL: http://keycloak:8080
      KEYCLOAK_REALM: synmetrix
      KEYCLOAK_CLIENT_ID: ${KEYCLOAK_CLIENT_ID}
      KEYCLOAK_CLIENT_SECRET: ${KEYCLOAK_CLIENT_SECRET}
    ports:
      - "3001:3000"
    depends_on:
      - hasura
      - keycloak
    networks:
      - synmetrix-network
```

**Sync Worker 实现** (`services/keycloak-sync/index.js`):

```javascript
const express = require('express');
const { fetchGraphQL } = require('./utils/graphql');

const app = express();
app.use(express.json());

// 接收 Keycloak Event Listener 的 webhook
app.post('/sync-user', async (req, res) => {
  const { keycloak_id, email, username, first_name, last_name, action } = req.body;

  try {
    switch (action) {
      case 'CREATE':
        await createUser({
          keycloak_id,
          email,
          display_name: `${first_name || ''} ${last_name || ''}`.trim() || username
        });
        break;

      case 'UPDATE':
        await updateUser(keycloak_id, {
          display_name: `${first_name || ''} ${last_name || ''}`.trim() || username
        });
        break;

      case 'DELETE':
        await deleteUser(keycloak_id);
        break;
    }

    res.json({ success: true });
  } catch (error) {
    console.error('Sync error:', error);
    res.status(500).json({ success: false, error: error.message });
  }
});

// GraphQL Mutations
const createUser = async ({ keycloak_id, email, display_name }) => {
  const mutation = `
    mutation CreateUser($keycloak_id: uuid!, $display_name: String) {
      insert_users_one(
        object: {
          keycloak_id: $keycloak_id,
          display_name: $display_name
        }
      ) {
        id
      }
    }
  `;

  await fetchGraphQL(mutation, { keycloak_id, display_name });
};

const updateUser = async (keycloak_id, updates) => {
  const mutation = `
    mutation UpdateUser($keycloak_id: uuid!, $changes: users_set_input!) {
      update_users(
        where: { keycloak_id: { _eq: $keycloak_id } },
        _set: $changes
      ) {
        affected_rows
      }
    }
  `;

  await fetchGraphQL(mutation, { keycloak_id, changes: updates });
};

const deleteUser = async (keycloak_id) => {
  const mutation = `
    mutation DeleteUser($keycloak_id: uuid!) {
      delete_users(where: { keycloak_id: { _eq: $keycloak_id } }) {
        affected_rows
      }
    }
  `;

  await fetchGraphQL(mutation, { keycloak_id });
};

app.listen(3000, () => {
  console.log('Keycloak sync worker running on port 3000');
});
```

#### 方式 3: 定期轮询同步（备选）

适用于小规模或初期测试：

```javascript
const schedule = require('node-schedule');
const KeycloakAdminClient = require('@keycloak/keycloak-admin-client').default;

const kcAdminClient = new KeycloakAdminClient({
  baseUrl: 'http://keycloak:8080',
  realmName: 'synmetrix'
});

// 每5分钟同步一次
schedule.scheduleJob('*/5 * * * *', async () => {
  await kcAdminClient.auth({
    grantType: 'client_credentials',
    clientId: process.env.KEYCLOAK_CLIENT_ID,
    clientSecret: process.env.KEYCLOAK_CLIENT_SECRET
  });

  const users = await kcAdminClient.users.find();

  for (const user of users) {
    // 检查用户是否存在于 public.users
    const existingUser = await checkUserExists(user.id);

    if (!existingUser) {
      await createUser({
        keycloak_id: user.id,
        display_name: user.username || user.email
      });
    } else {
      // 更新 last_sync_at
      await updateSyncStatus(user.id);
    }
  }
});
```

### 5.3 用户 ID 映射策略

**选项 1: 保持现有 UUID** (推荐)

- `public.users.id`: 保持自动生成的 UUID（Synmetrix 内部 ID）
- `public.users.keycloak_id`: 存储 Keycloak 用户 UUID
- JWT 中的 `x-hasura-user-id`: 映射到 `public.users.id`

**Keycloak Mapper 配置**:

```yaml
# 在 Keycloak Client Scope 中添加自定义 Mapper
mappers:
  - name: synmetrix-internal-user-id
    protocol: openid-connect
    protocolMapper: oidc-user-attribute-mapper
    config:
      user.attribute: synmetrix_user_id  # 需要在用户属性中设置
      claim.name: https://hasura.io/jwt/claims.x-hasura-user-id
      jsonType.label: String
      id.token.claim: true
      access.token.claim: true
```

**同步时设置用户属性**:

```javascript
// 创建用户时
const createUser = async ({ keycloak_id, email, display_name }) => {
  // 1. 在 Synmetrix 创建用户
  const mutation = `
    mutation CreateUser($display_name: String) {
      insert_users_one(object: { display_name: $display_name }) {
        id
        keycloak_id
      }
    }
  `;

  const result = await fetchGraphQL(mutation, { display_name });
  const synmetrixUserId = result.data.insert_users_one.id;

  // 2. 更新 Keycloak 用户属性
  await kcAdminClient.users.update(
    { id: keycloak_id },
    { attributes: { synmetrix_user_id: synmetrixUserId } }
  );

  // 3. 更新 Synmetrix keycloak_id
  const updateMutation = `
    mutation UpdateKeycloakId($id: uuid!, $keycloak_id: uuid!) {
      update_users_by_pk(
        pk_columns: { id: $id },
        _set: { keycloak_id: $keycloak_id }
      ) {
        id
      }
    }
  `;

  await fetchGraphQL(updateMutation, { id: synmetrixUserId, keycloak_id });
};
```

**选项 2: 使用 Keycloak UUID**

- `public.users.id`: 直接使用 Keycloak 用户 UUID
- 需要修改数据库默认值生成逻辑
- 迁移现有用户数据更复杂

---

## 6. 代码修改清单

### 6.1 必须修改的文件

| 文件路径 | 修改内容 | 优先级 |
|---------|---------|--------|
| `services/cubejs/src/utils/checkAuth.js` | 替换 JWT 验证为 JWKS | 🔴 高 |
| `services/cubejs/package.json` | 添加 `jwks-rsa` 依赖 | 🔴 高 |
| `.env` | 修改 `HASURA_GRAPHQL_JWT_SECRET` | 🔴 高 |
| `docker-compose.dev.yml` | 添加 Keycloak 服务，移除 hasura_plus | 🔴 高 |
| `services/hasura/migrations/YYYYMMDD_add_keycloak_fields.sql` | 添加 keycloak_id 字段 | 🟡 中 |
| `services/hasura/metadata/tables.yaml` | 更新 users 表权限（如需） | 🟡 中 |

### 6.2 可选修改的文件

| 文件路径 | 修改内容 | 优先级 |
|---------|---------|--------|
| `services/actions/src/utils/jwt.js` | 删除或标记为废弃（不再生成 JWT） | 🟢 低 |
| `services/actions/src/rpc/*.js` | 移除依赖 hasura_plus 的 RPC 方法 | 🟢 低 |
| `services/client/src/auth/*` | 集成 Keycloak JS Adapter | 🔴 高 |
| `services/client/nginx/default.conf.template` | 移除 `/auth` 路由到 hasura_plus | 🟡 中 |

### 6.3 新增文件

| 文件路径 | 用途 |
|---------|------|
| `services/keycloak-sync/index.js` | 用户同步服务 |
| `services/keycloak-sync/package.json` | 依赖定义 |
| `services/keycloak-sync/utils/graphql.js` | GraphQL 客户端 |
| `keycloak-config/realm-export.json` | Keycloak Realm 配置导出 |
| `keycloak-sync-extension/src/main/java/...` | Keycloak Event Listener (Java) |

### 6.4 详细修改示例

#### 文件 1: `.env`

```bash
# 删除或注释
# JWT_KEY=your-secret-key
# JWT_ALGORITHM=HS256

# 新增 Keycloak 配置
KEYCLOAK_URL=http://keycloak:8080
KEYCLOAK_REALM=synmetrix
KEYCLOAK_CLIENT_ID=synmetrix-frontend
KEYCLOAK_CLIENT_SECRET=your-client-secret  # 如果是 confidential client
KEYCLOAK_ADMIN_PASSWORD=admin123
KEYCLOAK_JWKS_URL=http://keycloak:8080/realms/synmetrix/protocol/openid-connect/certs
KEYCLOAK_ISSUER=http://localhost:8082/realms/synmetrix

# 更新 Hasura JWT 配置
HASURA_GRAPHQL_JWT_SECRET='{"type":"RS256","jwk_url":"http://keycloak:8080/realms/synmetrix/protocol/openid-connect/certs","claims_namespace":"https://hasura.io/jwt/claims","claims_format":"json","audience":"synmetrix-frontend","issuer":"http://localhost:8082/realms/synmetrix"}'
```

#### 文件 2: `services/cubejs/src/utils/checkAuth.js`

完整代码见 [4.2 Cube.js JWT 验证调整](#42-cubejs-jwt-验证调整)

#### 文件 3: `services/client/nginx/default.conf.template`

```nginx
# 删除 hasura_plus 路由
# location /auth {
#     proxy_pass http://hasura_plus:3000;
# }

# 新增 Keycloak 路由（如果需要代理）
location /keycloak/ {
    proxy_pass http://keycloak:8080/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
}
```

---

## 7. 迁移步骤

### 7.1 准备阶段

#### 步骤 1: 备份数据

```bash
# 备份 PostgreSQL 数据库
docker exec synmetrix-postgres pg_dump -U postgres synmetrix > backup_$(date +%Y%m%d).sql

# 备份 auth schema
docker exec synmetrix-postgres pg_dump -U postgres -n auth synmetrix > backup_auth_$(date +%Y%m%d).sql
```

#### 步骤 2: 创建测试环境

```bash
# 复制 .env
cp .env .env.keycloak

# 创建独立的 docker-compose 文件
cp docker-compose.dev.yml docker-compose.keycloak.yml
```

### 7.2 开发阶段

#### 步骤 3: 部署 Keycloak

```bash
# 启动 Keycloak
docker-compose -f docker-compose.keycloak.yml up -d keycloak

# 等待 Keycloak 启动
docker logs -f synmetrix-keycloak
```

#### 步骤 4: 配置 Keycloak Realm

```bash
# 访问 Keycloak Admin Console
open http://localhost:8082

# 登录: admin / ${KEYCLOAK_ADMIN_PASSWORD}

# 执行以下操作:
# 1. 创建 Realm: synmetrix
# 2. 创建 Client: synmetrix-frontend
# 3. 配置 Client Scopes (见 2.2 节)
# 4. 测试用户创建
```

#### 步骤 5: 迁移现有用户（可选）

**选项 A: 手动迁移脚本**

```javascript
// scripts/migrate-users-to-keycloak.js
const { fetchGraphQL } = require('../services/actions/src/utils/graphql');
const KeycloakAdminClient = require('@keycloak/keycloak-admin-client').default;

const kcAdminClient = new KeycloakAdminClient({
  baseUrl: 'http://localhost:8082',
  realmName: 'synmetrix'
});

async function migrateUsers() {
  // 1. 认证
  await kcAdminClient.auth({
    username: 'admin',
    password: process.env.KEYCLOAK_ADMIN_PASSWORD,
    grantType: 'password',
    clientId: 'admin-cli'
  });

  // 2. 从 Synmetrix 获取所有用户
  const query = `
    query {
      users {
        id
        display_name
        account {
          email
        }
      }
    }
  `;

  const result = await fetchGraphQL(query);
  const users = result.data.users;

  console.log(`Found ${users.length} users to migrate`);

  // 3. 在 Keycloak 创建用户
  for (const user of users) {
    try {
      const kcUser = await kcAdminClient.users.create({
        username: user.account.email,
        email: user.account.email,
        emailVerified: true,
        enabled: true,
        attributes: {
          synmetrix_user_id: user.id  // 保持原有 ID
        }
      });

      console.log(`✓ Migrated: ${user.account.email}`);

      // 4. 更新 Synmetrix users 表
      const updateMutation = `
        mutation UpdateKeycloakId($id: uuid!, $keycloak_id: String!) {
          update_users_by_pk(
            pk_columns: { id: $id },
            _set: { keycloak_sub: $keycloak_id }
          ) {
            id
          }
        }
      `;

      await fetchGraphQL(updateMutation, {
        id: user.id,
        keycloak_id: kcUser.id
      });

    } catch (error) {
      console.error(`✗ Failed to migrate ${user.account.email}:`, error.message);
    }
  }

  console.log('Migration completed!');
}

migrateUsers().catch(console.error);
```

运行迁移:

```bash
node scripts/migrate-users-to-keycloak.js
```

**选项 B: 要求用户重新注册**

对于小型项目，可以选择不迁移现有用户，而是：
1. 发送邮件通知用户系统升级
2. 用户使用原邮箱在新系统重新注册
3. 提供数据关联工具（通过 email 匹配）

#### 步骤 6: 部署 Sync Worker

```bash
# 构建服务
cd services/keycloak-sync
npm install

# 启动服务
docker-compose -f docker-compose.keycloak.yml up -d keycloak_sync
```

#### 步骤 7: 更新代码

```bash
# 修改 Cube.js checkAuth
# (见 6.4 详细修改示例)

# 安装新依赖
cd services/cubejs
npm install jwks-rsa

# 重启服务
docker-compose -f docker-compose.keycloak.yml restart cubejs
```

### 7.3 测试阶段

#### 步骤 8: 集成测试

```bash
# 测试 Keycloak 登录流程
curl -X POST http://localhost:8082/realms/synmetrix/protocol/openid-connect/token \
  -d "client_id=synmetrix-frontend" \
  -d "username=test@example.com" \
  -d "password=test123" \
  -d "grant_type=password"

# 提取 access_token
export TOKEN="<access_token_from_above>"

# 测试 Hasura GraphQL API
curl -X POST http://localhost:8080/v1/graphql \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"query":"{ users { id display_name } }"}'

# 测试 Cube.js API
curl http://localhost:4000/cubejs-api/v1/meta \
  -H "Authorization: Bearer $TOKEN" \
  -H "x-hasura-datasource-id: <datasource-uuid>"
```

#### 步骤 9: 前端集成测试

```javascript
// 在 React 应用中测试
import Keycloak from 'keycloak-js';

const keycloak = new Keycloak({
  url: 'http://localhost:8082/',
  realm: 'synmetrix',
  clientId: 'synmetrix-frontend'
});

keycloak.init({ onLoad: 'login-required' }).then(authenticated => {
  console.log('Authenticated:', authenticated);
  console.log('Token:', keycloak.token);

  // 解码 Token 查看 claims
  const payload = JSON.parse(atob(keycloak.token.split('.')[1]));
  console.log('Hasura Claims:', payload['https://hasura.io/jwt/claims']);
});
```

### 7.4 生产部署

#### 步骤 10: 灰度发布（可选）

```yaml
# 同时运行两套认证系统
# docker-compose.prod.yml

services:
  # 旧系统
  hasura_plus:
    # ... 保持不变

  # 新系统
  keycloak:
    # ... Keycloak 配置

  # Hasura 配置支持两种 JWT
  hasura:
    environment:
      HASURA_GRAPHQL_JWT_SECRET: |
        [
          {
            "type": "HS256",
            "key": "${OLD_JWT_KEY}"
          },
          {
            "type": "RS256",
            "jwk_url": "http://keycloak:8080/realms/synmetrix/protocol/openid-connect/certs",
            "claims_namespace": "https://hasura.io/jwt/claims"
          }
        ]
```

**特性开关**:

```javascript
// 前端使用特性开关
const USE_KEYCLOAK = process.env.REACT_APP_USE_KEYCLOAK === 'true';

if (USE_KEYCLOAK) {
  // Keycloak 登录
  initKeycloak();
} else {
  // 旧版 hasura_plus 登录
  loginWithEmail(email, password);
}
```

#### 步骤 11: 完全切换

```bash
# 1. 停止 hasura_plus
docker-compose stop hasura_plus

# 2. 验证所有功能正常
# 运行完整测试套件

# 3. 备份并归档 auth schema
docker exec synmetrix-postgres pg_dump -U postgres -n auth synmetrix \
  > auth_schema_archive_$(date +%Y%m%d).sql

# 4. 删除 hasura_plus 配置
# 从 docker-compose.yml 移除 hasura_plus 服务

# 5. 更新文档
```

---

## 8. 风险评估与缓解

### 8.1 主要风险

| 风险 | 影响 | 概率 | 缓解措施 |
|------|------|------|----------|
| **JWT 验证失败** | 🔴 高 - 用户无法登录 | 中 | 1. 灰度发布支持双 JWT<br>2. 充分测试<br>3. 快速回滚机制 |
| **用户数据丢失** | 🔴 高 - 数据不一致 | 低 | 1. 完整备份<br>2. 事务性迁移<br>3. 数据校验脚本 |
| **性能下降** | 🟡 中 - JWKS 获取延迟 | 中 | 1. 启用 JWKS 缓存<br>2. 监控响应时间<br>3. CDN 加速 |
| **同步延迟** | 🟡 中 - 用户数据不同步 | 高 | 1. 事件驱动同步<br>2. 重试机制<br>3. 监控告警 |
| **OAuth 集成中断** | 🟢 低 - 第三方登录失败 | 低 | 1. Keycloak 原生支持<br>2. 配置迁移 |

### 8.2 缓解策略详细说明

#### 策略 1: 双 JWT 支持

```json
// Hasura JWT 配置支持数组
{
  "type": "array",
  "value": [
    {
      "type": "HS256",
      "key": "old-secret-key",
      "claims_namespace": "https://hasura.io/jwt/claims"
    },
    {
      "type": "RS256",
      "jwk_url": "http://keycloak:8080/realms/synmetrix/protocol/openid-connect/certs",
      "claims_namespace": "https://hasura.io/jwt/claims"
    }
  ]
}
```

#### 策略 2: 健康检查

```javascript
// services/keycloak-sync/health.js
app.get('/health', async (req, res) => {
  const checks = {
    keycloak: await checkKeycloakHealth(),
    hasura: await checkHasuraHealth(),
    database: await checkDatabaseHealth()
  };

  const allHealthy = Object.values(checks).every(c => c.healthy);

  res.status(allHealthy ? 200 : 503).json(checks);
});

async function checkKeycloakHealth() {
  try {
    const response = await fetch(`${KEYCLOAK_URL}/health/ready`);
    return { healthy: response.ok };
  } catch (error) {
    return { healthy: false, error: error.message };
  }
}
```

#### 策略 3: 监控告警

```yaml
# docker-compose.monitoring.yml
services:
  prometheus:
    image: prom/prometheus
    volumes:
      - ./monitoring/prometheus.yml:/etc/prometheus/prometheus.yml

  grafana:
    image: grafana/grafana
    ports:
      - "3002:3000"
```

**监控指标**:
- JWT 验证成功率
- JWKS 缓存命中率
- 用户同步延迟
- Keycloak 响应时间

---

## 9. 回滚方案

### 9.1 快速回滚（1小时内）

```bash
# 1. 恢复 hasura_plus 服务
docker-compose up -d hasura_plus

# 2. 恢复旧的 Hasura JWT 配置
# 修改 .env
HASURA_GRAPHQL_JWT_SECRET='{"type":"HS256","key":"your-old-secret"}'

# 3. 重启 Hasura
docker-compose restart hasura

# 4. 停止 Keycloak 相关服务
docker-compose stop keycloak keycloak_sync

# 5. 恢复前端代码
git revert <keycloak-integration-commit>
npm run build && npm run deploy
```

### 9.2 数据恢复（如有数据损坏）

```bash
# 1. 停止所有服务
docker-compose down

# 2. 恢复数据库
docker exec -i synmetrix-postgres psql -U postgres synmetrix < backup_YYYYMMDD.sql

# 3. 验证数据
docker exec synmetrix-postgres psql -U postgres -d synmetrix -c \
  "SELECT COUNT(*) FROM public.users;"

# 4. 重启服务
docker-compose up -d
```

---

## 10. 参考资料

### 10.1 官方文档

- [Keycloak Documentation](https://www.keycloak.org/documentation)
- [Hasura JWT Authentication](https://hasura.io/docs/latest/auth/authentication/jwt/)
- [Keycloak Admin REST API](https://www.keycloak.org/docs-api/latest/rest-api/)
- [Keycloak SPI Development](https://www.keycloak.org/docs/latest/server_development/)

### 10.2 相关文章

- [Integrating Keycloak with Hasura](https://hasura.io/learn/graphql/hasura-authentication/integrations/keycloak/)
- [Keycloak Event Listener Tutorial](https://medium.com/@mihirpaldhikar/create-a-custom-event-listener-in-keycloak-5b2a1ec90e62)

### 10.3 工具库

- [keycloak-js](https://www.npmjs.com/package/keycloak-js) - 前端集成
- [@keycloak/keycloak-admin-client](https://www.npmjs.com/package/@keycloak/keycloak-admin-client) - Node.js Admin API
- [jwks-rsa](https://www.npmjs.com/package/jwks-rsa) - JWKS 验证

---

## 附录

### A. 环境变量完整清单

```bash
# Keycloak
KEYCLOAK_URL=http://keycloak:8080
KEYCLOAK_REALM=synmetrix
KEYCLOAK_CLIENT_ID=synmetrix-frontend
KEYCLOAK_CLIENT_SECRET=changeme
KEYCLOAK_ADMIN_PASSWORD=admin123
KEYCLOAK_JWKS_URL=http://keycloak:8080/realms/synmetrix/protocol/openid-connect/certs
KEYCLOAK_ISSUER=http://localhost:8082/realms/synmetrix

# Hasura JWT (更新)
HASURA_GRAPHQL_JWT_SECRET='{"type":"RS256","jwk_url":"http://keycloak:8080/realms/synmetrix/protocol/openid-connect/certs","claims_namespace":"https://hasura.io/jwt/claims","claims_format":"json","audience":"synmetrix-frontend","issuer":"http://localhost:8082/realms/synmetrix"}'

# 移除 (旧配置)
# JWT_KEY=your-secret-key
# JWT_ALGORITHM=HS256
# JWT_EXPIRES_IN=60
# JWT_CLAIMS_NAMESPACE=https://hasura.io/jwt/claims
```

### B. 迁移检查清单

#### 迁移前

- [ ] 完整备份数据库
- [ ] 备份 `auth` schema
- [ ] 备份 `.env` 文件
- [ ] 记录所有现有用户数量
- [ ] 准备回滚脚本

#### 迁移中

- [ ] Keycloak 部署成功
- [ ] Realm 配置完成
- [ ] Client 配置正确
- [ ] JWT Mapper 测试通过
- [ ] 用户同步服务运行
- [ ] 测试用户创建成功
- [ ] JWT 包含正确的 Hasura claims

#### 迁移后

- [ ] 所有用户可以登录
- [ ] GraphQL 查询正常
- [ ] Cube.js API 正常
- [ ] SQL API 正常（如使用）
- [ ] 权限控制正确
- [ ] 性能指标正常
- [ ] 监控告警配置
- [ ] 文档更新完成

### C. Keycloak Realm 导出示例

```json
{
  "realm": "synmetrix",
  "enabled": true,
  "sslRequired": "external",
  "registrationAllowed": true,
  "loginWithEmailAllowed": true,
  "duplicateEmailsAllowed": false,
  "resetPasswordAllowed": true,
  "editUsernameAllowed": false,
  "bruteForceProtected": true,
  "clients": [
    {
      "clientId": "synmetrix-frontend",
      "enabled": true,
      "publicClient": true,
      "redirectUris": [
        "http://localhost:9055/*",
        "https://yourdomain.com/*"
      ],
      "webOrigins": ["+"],
      "protocol": "openid-connect",
      "fullScopeAllowed": false,
      "defaultClientScopes": [
        "web-origins",
        "profile",
        "roles",
        "email",
        "synmetrix-hasura"
      ]
    }
  ],
  "clientScopes": [
    {
      "name": "synmetrix-hasura",
      "protocol": "openid-connect",
      "protocolMappers": [
        {
          "name": "hasura-user-id",
          "protocol": "openid-connect",
          "protocolMapper": "oidc-usermodel-attribute-mapper",
          "config": {
            "user.attribute": "synmetrix_user_id",
            "claim.name": "https://hasura.io/jwt/claims.x-hasura-user-id",
            "jsonType.label": "String",
            "id.token.claim": "true",
            "access.token.claim": "true"
          }
        },
        {
          "name": "hasura-default-role",
          "protocol": "openid-connect",
          "protocolMapper": "oidc-hardcoded-claim-mapper",
          "config": {
            "claim.name": "https://hasura.io/jwt/claims.x-hasura-default-role",
            "claim.value": "user",
            "jsonType.label": "String",
            "id.token.claim": "true",
            "access.token.claim": "true"
          }
        },
        {
          "name": "hasura-allowed-roles",
          "protocol": "openid-connect",
          "protocolMapper": "oidc-hardcoded-claim-mapper",
          "config": {
            "claim.name": "https://hasura.io/jwt/claims.x-hasura-allowed-roles",
            "claim.value": "[\"user\"]",
            "jsonType.label": "JSON",
            "id.token.claim": "true",
            "access.token.claim": "true"
          }
        }
      ]
    }
  ]
}
```

---

## 总结

将 Synmetrix 从 Hasura Backend Plus 迁移到 Keycloak 是一个**系统性工程**，涉及：

1. **架构调整**: 从内置认证服务转向独立的 OIDC 提供商
2. **JWT 集成**: 从 HS256 对称密钥切换到 RS256 公钥验证
3. **用户同步**: 建立 Keycloak → Synmetrix 的数据同步机制
4. **代码修改**: 更新 JWT 验证逻辑和前端集成

**关键优势**:
- ✅ 企业级认证功能（SSO、MFA、Federation）
- ✅ 更好的安全性（公钥加密、标准 OIDC 协议）
- ✅ 可扩展性（支持多 Realm、多 Client）
- ✅ 统一身份管理（可集成其他系统）

**迁移建议**:
- 🔄 采用灰度发布，逐步切换用户
- 🔄 保持双 JWT 支持一段时间
- 🔄 充分测试所有认证流程
- 🔄 准备快速回滚方案

**预计迁移周期**: 2-4 周（取决于用户规模和测试复杂度）
