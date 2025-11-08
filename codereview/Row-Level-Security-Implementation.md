# Row-Level Security (RLS) 实现方案

## 概述

当前 Synmetrix 项目主要在数据源、Schema、Cube 层面做隔离，缺少**行级数据隔离**机制。本文档提供完整的实现方案，使同一张表可以根据字段值（如 `tenant_id`, `user_id`）为不同用户隔离数据。

## 方案对比

| 方案 | 优点 | 缺点 | 适用场景 |
|------|------|------|----------|
| SECURITY_CONTEXT | 灵活、与 Cube.js 深度集成 | 需要修改所有 schema | 多租户场景，需要细粒度控制 |
| queryTransformer | 统一处理，无需修改 schema | 只能按 cube 过滤，不够灵活 | 简单的全局过滤 |
| 数据库 RLS | 性能好、安全性高 | 依赖特定数据库 | PostgreSQL 数据源 |

## 推荐方案：SECURITY_CONTEXT + Schema 层过滤

### 步骤 1: 扩展 Security Context

修改 `services/cubejs/src/utils/defineUserScope.js`：

```javascript
const defineUserScope = (
  allDataSources,
  allMembers,
  selectedDataSourceId,
  selectedBranchId,
  selectedVersionId
) => {
  // ... 现有代码 ...

  const dataSourceAccessList = getDataSourceAccessList(
    allMembers,
    selectedDataSourceId,
    dataSource.team_id
  );

  const dataSourceContext = buildSecurityContext(
    dataSource,
    selectedBranch,
    selectedVersion
  );

  // 获取用户的租户信息
  const member = allMembers.find(m => m.team_id === dataSource.team_id);
  const tenantId = dataSource.team_id; // 或从其他地方获取
  const organizationId = member?.organization_id;

  return {
    dataSource: dataSourceContext,
    ...dataSourceAccessList,

    // 新增：用于行级过滤的上下文
    rowLevelContext: {
      tenantId,
      organizationId,
      // 可以添加更多过滤字段
    }
  };
};

export default defineUserScope;
```

修改 `services/cubejs/src/utils/checkAuth.js`：

```javascript
const checkAuth = async (req) => {
  // ... 现有代码 ...

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

    // 新增：将行级上下文暴露给 Cube.js
    ...userScope.rowLevelContext,
  };
};
```

### 步骤 2: 在 Schema 中使用 SECURITY_CONTEXT

#### 选项 A: JavaScript 格式 (推荐)

修改 schema 生成逻辑，生成 JS 格式的 schema：

```javascript
// Users.js - 示例
cube(`Users`, {
  sql: `
    SELECT * FROM public.users
    WHERE team_id = ${SECURITY_CONTEXT.tenantId}
  `,

  joins: {
    Orders: {
      sql: `${CUBE}.id = ${Orders}.user_id`,
      relationship: `hasMany`
    }
  },

  dimensions: {
    id: {
      sql: `id`,
      type: `number`,
      primaryKey: true
    },

    email: {
      sql: `email`,
      type: `string`
    },

    teamId: {
      sql: `team_id`,
      type: `number`,
      // 可以隐藏该字段，只用于过滤
      shown: false
    }
  },

  measures: {
    count: {
      type: `count`
    }
  }
});
```

```javascript
// Orders.js - 示例（关联表也需要过滤）
cube(`Orders`, {
  sql: `
    SELECT o.* FROM orders o
    INNER JOIN users u ON o.user_id = u.id
    WHERE u.team_id = ${SECURITY_CONTEXT.tenantId}
  `,

  dimensions: {
    id: {
      sql: `id`,
      type: `number`,
      primaryKey: true
    },

    status: {
      sql: `status`,
      type: `string`
    },

    createdAt: {
      sql: `created_at`,
      type: `time`
    }
  },

  measures: {
    count: {
      type: `count`
    },

    totalAmount: {
      sql: `amount`,
      type: `sum`
    }
  }
});
```

#### 选项 B: YAML 格式 (项目当前格式)

修改 schema 生成器，在 YAML 中使用模板变量：

```yaml
# Users.yml
cubes:
  - name: Users
    sql: >
      SELECT * FROM public.users
      WHERE team_id = {SECURITY_CONTEXT.tenantId}

    dimensions:
      - name: id
        sql: id
        type: number
        primaryKey: true

      - name: email
        sql: email
        type: string

      - name: teamId
        sql: team_id
        type: number
        shown: false

    measures:
      - name: count
        type: count
```

**注意**: YAML 格式中需要确保 Cube.js 能正确解析 `{SECURITY_CONTEXT.xxx}` 语法。如果不支持，需要修改 `mapSchemaToFile.js` 进行转换。

### 步骤 3: 修改 Schema 生成器

修改 `services/cubejs/src/routes/generateDataSchema.js`，在自动生成 schema 时注入 RLS 条件：

```javascript
const generateSchemaWithRLS = (scaffoldingTemplate, tables, rlsConfig) => {
  const files = scaffoldingTemplate.generateFilesByTableNames(tables);

  // 为每个 schema 注入 RLS SQL
  return files.map(file => {
    const cubeName = file.fileName.replace(/\.(yml|js)$/, '');
    const rlsFilter = rlsConfig[cubeName];

    if (rlsFilter) {
      // 修改 SQL，添加 WHERE 条件
      file.content = injectRLSFilter(file.content, rlsFilter);
    }

    return file;
  });
};

const injectRLSFilter = (schemaContent, rlsFilter) => {
  // 根据格式（YAML 或 JS）注入过滤条件
  // 例如：在 sql 字段末尾添加 WHERE tenant_id = {SECURITY_CONTEXT.tenantId}
  const { field, contextKey } = rlsFilter;
  const filterClause = `WHERE ${field} = {SECURITY_CONTEXT.${contextKey}}`;

  // 简化示例，实际需要更复杂的 AST 解析
  return schemaContent.replace(
    /sql: SELECT \* FROM ([\w.]+)/g,
    `sql: SELECT * FROM $1 ${filterClause}`
  );
};
```

### 步骤 4: 配置 RLS 规则

在数据库中添加 RLS 配置表：

```sql
CREATE TABLE public.datasource_rls_config (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  datasource_id UUID NOT NULL REFERENCES datasources(id),
  table_name VARCHAR(255) NOT NULL,
  filter_field VARCHAR(255) NOT NULL,  -- 例如: team_id, organization_id
  context_key VARCHAR(255) NOT NULL,    -- 例如: tenantId, organizationId
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- 示例数据
INSERT INTO datasource_rls_config (datasource_id, table_name, filter_field, context_key)
VALUES
  ('715dfae1-1044-42ec-ac48-dd4cefa567e6', 'users', 'team_id', 'tenantId'),
  ('715dfae1-1044-42ec-ac48-dd4cefa567e6', 'orders', 'team_id', 'tenantId');
```

在 `buildSecurityContext.js` 中加载 RLS 配置：

```javascript
const buildSecurityContext = async (dataSource, branch, version) => {
  // ... 现有代码 ...

  // 加载 RLS 配置
  const rlsConfig = await fetchGraphQL(`
    query GetRLSConfig($datasourceId: uuid!) {
      datasource_rls_config(where: {datasource_id: {_eq: $datasourceId}}) {
        table_name
        filter_field
        context_key
      }
    }
  `, { datasourceId: dataSource.id });

  return {
    ...data,
    dataSourceVersion,
    preAggregationSchema,
    schemaVersion,
    files,
    rlsConfig: rlsConfig.data?.datasource_rls_config || [],
  };
};
```

## 方案 2: 使用 queryTransformer（补充方案）

如果不想修改 schema，可以在查询时动态注入过滤条件：

修改 `services/cubejs/index.js`：

```javascript
const queryTransformer = (query, { securityContext }) => {
  const { tenantId, role } = securityContext;

  // owner/admin 不受限制
  if (['owner', 'admin'].includes(role)) {
    return query;
  }

  // 为所有 cube 添加租户过滤
  const modifiedQuery = {
    ...query,
    filters: [
      ...(query.filters || []),
    ]
  };

  // 为每个查询的 cube 添加过滤
  const cubes = [
    ...(query.dimensions || []),
    ...(query.measures || [])
  ].map(item => item.split('.')[0]);

  const uniqueCubes = [...new Set(cubes)];

  // 为每个 cube 添加 tenantId 过滤（如果该 cube 有 teamId 字段）
  uniqueCubes.forEach(cube => {
    modifiedQuery.filters.push({
      member: `${cube}.teamId`,
      operator: 'equals',
      values: [String(tenantId)]
    });
  });

  return modifiedQuery;
};

const options = {
  // ... 其他配置
  queryRewrite,
  queryTransformer,  // 添加
  // ...
};
```

**局限性**: 这种方式要求所有表都有 `teamId` 字段，且 cube 中必须定义该维度。

## 方案 3: 数据库层 RLS（PostgreSQL）

对于 PostgreSQL 数据源，可以使用数据库原生的 Row-Level Security：

### 在数据源数据库执行

```sql
-- 启用 RLS
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE orders ENABLE ROW LEVEL SECURITY;

-- 创建策略
CREATE POLICY tenant_isolation_users ON users
  FOR ALL
  USING (team_id = current_setting('app.current_tenant_id', true)::uuid);

CREATE POLICY tenant_isolation_orders ON orders
  FOR ALL
  USING (
    user_id IN (
      SELECT id FROM users WHERE team_id = current_setting('app.current_tenant_id', true)::uuid
    )
  );

-- 为 superuser/admin 绕过 RLS
ALTER TABLE users FORCE ROW LEVEL SECURITY;
ALTER TABLE orders FORCE ROW LEVEL SECURITY;
```

### 修改 Driver Factory

修改 `services/cubejs/src/utils/driverFactory.js`：

```javascript
import ServerCore from "@cubejs-backend/server-core";
import prepareDbParams from "./prepareDbParams.js";

const driverFactory = async ({ securityContext }) => {
  const { dbType, dbParams } = securityContext.userScope.dataSource;
  const { tenantId, role } = securityContext;

  const params = prepareDbParams(dbParams, dbType);

  const driver = ServerCore.createDriver(dbType, params);

  // 为 PostgreSQL 设置会话变量
  if (dbType === 'postgres' && tenantId) {
    try {
      await driver.query(
        `SET app.current_tenant_id = '${tenantId}'`
      );

      // 可选：设置其他上下文
      if (role) {
        await driver.query(
          `SET app.current_user_role = '${role}'`
        );
      }
    } catch (err) {
      console.error('Failed to set RLS context:', err);
    }
  }

  return driver;
};

export default driverFactory;
```

**优势**:
- 安全性最高，无法绕过数据库层的限制
- 性能好，利用数据库索引
- 无需修改 Cube.js schema

**劣势**:
- 只支持 PostgreSQL（MySQL 8.0+ 也有类似功能）
- 需要数据库管理员权限
- 调试困难

## 测试方案

### 单元测试

创建 `services/cubejs/src/utils/__tests__/rowLevelSecurity.test.js`：

```javascript
import { jest } from '@jest/globals';
import checkAuth from '../checkAuth.js';
import defineUserScope from '../defineUserScope.js';

describe('Row-Level Security', () => {
  test('should inject tenantId into security context', async () => {
    const req = {
      headers: {
        authorization: 'Bearer valid-jwt-token',
        'x-hasura-datasource-id': 'test-datasource-id'
      }
    };

    await checkAuth(req);

    expect(req.securityContext.tenantId).toBeDefined();
    expect(req.securityContext.userScope.rowLevelContext).toBeDefined();
  });

  test('should filter queries by tenantId', async () => {
    const query = {
      dimensions: ['Users.name'],
      measures: ['Users.count']
    };

    const securityContext = {
      tenantId: 'tenant-123',
      role: 'member',
      userScope: { /* ... */ }
    };

    const transformedQuery = await queryTransformer(query, { securityContext });

    expect(transformedQuery.filters).toContainEqual({
      member: 'Users.teamId',
      operator: 'equals',
      values: ['tenant-123']
    });
  });
});
```

### 集成测试

```bash
# 使用 stepci 测试
# services/tests/stepci/rls.yml

version: "1.1"
name: Row-Level Security Tests
env:
  host: http://localhost:4000
tests:
  tenant_isolation:
    steps:
      - name: Query as Tenant A
        http:
          url: ${{env.host}}/api/v1/load
          method: POST
          headers:
            Authorization: Bearer ${{env.tenant_a_token}}
            x-hasura-datasource-id: ${{env.datasource_id}}
          json:
            query:
              dimensions:
                - Users.email
              measures:
                - Users.count
          check:
            status: 200
            jsonpath:
              $.data[*].Users.email:
                # 只包含 Tenant A 的数据
                contains: tenant-a-user@example.com
                not_contains: tenant-b-user@example.com

      - name: Query as Tenant B
        http:
          url: ${{env.host}}/api/v1/load
          method: POST
          headers:
            Authorization: Bearer ${{env.tenant_b_token}}
            x-hasura-datasource-id: ${{env.datasource_id}}
          json:
            query:
              dimensions:
                - Users.email
              measures:
                - Users.count
          check:
            status: 200
            jsonpath:
              $.data[*].Users.email:
                # 只包含 Tenant B 的数据
                contains: tenant-b-user@example.com
                not_contains: tenant-a-user@example.com
```

## 实施建议

1. **渐进式迁移**:
   - 第一阶段：先实现 `securityContext` 扩展
   - 第二阶段：修改关键 schema 添加 RLS 过滤
   - 第三阶段：更新 schema 生成器自动注入 RLS

2. **向后兼容**:
   - 为没有配置 RLS 的数据源保持原有行为
   - 添加功能开关：`ENABLE_ROW_LEVEL_SECURITY=true`

3. **性能优化**:
   - 确保过滤字段有索引（`team_id`, `organization_id`）
   - 使用 Cube.js pre-aggregations 缓存按租户的聚合数据

4. **安全审计**:
   - 记录 RLS 过滤条件到日志
   - 定期审查 schema 确保所有敏感表都启用 RLS

## 参考资料

- [Cube.js Security Context](https://cube.dev/docs/security/context)
- [PostgreSQL Row Security Policies](https://www.postgresql.org/docs/current/ddl-rowsecurity.html)
- [Multi-Tenancy Patterns in SaaS](https://docs.aws.amazon.com/whitepapers/latest/saas-architecture-fundamentals/multi-tenancy-patterns.html)
