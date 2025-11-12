# Row-Level Security (RLS) 设计方案

## 概述

为 Synmetrix 设计一个通用的行级权限控制系统，允许基于用户身份、角色、部门等维度动态过滤查询结果。

## 当前架构分析

### 现有权限模型

**数据结构**：
- `access_lists` 表：存储权限配置（JSONB）
- `member_roles` 表：关联用户、团队和权限列表

**当前权限配置结构**：
```json
{
  "config": {
    "datasources": {
      "{datasourceId}": {
        "cubes": {
          "CubeName": {
            "dimensions": ["dimension1", "dimension2"],
            "measures": ["measure1", "measure2"],
            "segments": ["segment1"]
          }
        }
      }
    }
  }
}
```

**现有实现**（`queryRewrite.js:22-47`）：
- ✅ 列级权限控制（Column-level Security）
- ✅ 角色检查（owner/admin 跳过）
- ❌ 无行级过滤（Row-level Security）

## 行级权限设计

### 关注点分离原则

**纵切（列级权限）** vs **横切（行级权限）**：

```
┌─────────────────────────────────────────────┐
│  access_lists 表 (列级权限 - Role-Based)    │
│  ┌───────────────────────────────────────┐  │
│  │ 不同角色可以访问哪些 Cube 和字段？     │  │
│  │ • owner:  all dimensions & measures  │  │
│  │ • admin:  most dimensions & measures │  │
│  │ • member: limited dimensions         │  │
│  └───────────────────────────────────────┘  │
└─────────────────────────────────────────────┘
                    ⊥ (正交)
┌─────────────────────────────────────────────┐
│  datasources 表 (行级权限 - User-Based)     │
│  ┌───────────────────────────────────────┐  │
│  │ 每个用户可以访问哪些数据行？           │  │
│  │ • WHERE userId = {{userId}}          │  │
│  │ • WHERE department = {{department}}  │  │
│  │ ✅ 对所有角色生效（owner/admin 除外） │  │
│  └───────────────────────────────────────┘  │
└─────────────────────────────────────────────┘
```

### 核心洞察

1. **列级权限是 Role-Based**：
   - owner 看所有字段
   - admin 看大部分字段
   - member 看部分字段
   - 每个 role 需要独立的 access_list 配置 ✅

2. **行级权限是 User-Based**：
   - 所有用户（非 owner/admin）都应该只看到 `userId = 自己` 的数据
   - 规则是通用的，不需要为每个 role 重复定义 ✅
   - 应该在 **datasource 级别** 定义，而不是在 access_list 级别

3. **RLS 不应该在 access_lists 中**：
   - ❌ access_lists 是 per-role 的
   - ✅ RLS 应该是 per-datasource 的（所有角色共享）

### 1. 配置模型

在 `datasources` 表添加 `rls_config` 字段：

```sql
ALTER TABLE public.datasources
ADD COLUMN rls_config JSONB DEFAULT NULL;

COMMENT ON COLUMN public.datasources.rls_config IS
'Row-level security policies for this datasource (applies to all non-admin users)';
```

**rls_config 结构**：
```json
{
  "enabled": true,
  "exemptRoles": ["owner", "admin"],
  "policies": {
    "Users": {
      "enabled": true,
      "rules": [
        {
          "member": "Users.userId",
          "operator": "equals",
          "values": ["{{userId}}"]
        }
      ]
    },
    "Orders": {
      "enabled": true,
      "logic": "AND",
      "rules": [
        {
          "member": "Orders.userId",
          "operator": "equals",
          "values": ["{{userId}}"]
        },
        {
          "member": "Orders.status",
          "operator": "notEquals",
          "values": ["deleted"]
        }
      ]
    },
    "SalesData": {
      "enabled": true,
      "logic": "OR",
      "rules": [
        {
          "member": "SalesData.salesPerson",
          "operator": "equals",
          "values": ["{{userId}}"]
        },
        {
          "member": "SalesData.region",
          "operator": "in",
          "values": ["{{userRegions}}"]
        }
      ]
    }
  }
}
```

**优点**：
- ✅ RLS 配置在 datasource 级别（所有角色共享）
- ✅ 不需要为每个 role 重复定义
- ✅ `exemptRoles` 明确指定哪些角色跳过 RLS
- ✅ 单次查询获取 datasource + RLS 配置
- ✅ 逻辑清晰：datasource 拥有其行级安全策略

**数据流**：
```
datasources 表
└─ id: ds-1
   ├─ name: "Production DB"
   ├─ db_type: "postgres"
   └─ rls_config: { /* RLS 配置 */ }  ← 所有用户共享
```

### 2. 配置字段说明

#### rowLevelSecurity 对象（顶层）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `enabled` | boolean | 是 | 是否启用该数据源的行级安全 |
| `policies` | object | 是 | Cube 名称到策略的映射 |

#### policy 对象（per-Cube）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `enabled` | boolean | 是 | 是否启用该 Cube 的行级过滤 |
| `logic` | string | 否 | 多规则组合逻辑：`AND`（且）或 `OR`（或）<br/>默认：`AND` |
| `rules` | array | 是 | 过滤规则数组 |

#### rule 对象

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `type` | string | 是 | 固定值：`filter` |
| `member` | string | 是 | Cube 成员名称，格式：`CubeName.fieldName` |
| `operator` | string | 是 | 运算符（见下表） |
| `values` | array | 是 | 过滤值数组，支持模板变量 `{{variable}}` |

#### 支持的运算符

| 运算符 | 说明 | 示例 |
|--------|------|------|
| `equals` | 等于 | `"userId" equals "123"` |
| `notEquals` | 不等于 | `"status" notEquals "deleted"` |
| `contains` | 包含（字符串） | `"name" contains "John"` |
| `notContains` | 不包含（字符串） | `"email" notContains "spam"` |
| `in` | 在列表中 | `"region" in ["US", "EU"]` |
| `notIn` | 不在列表中 | `"status" notIn ["deleted", "banned"]` |
| `gt` | 大于 | `"amount" gt 1000` |
| `gte` | 大于等于 | `"age" gte 18` |
| `lt` | 小于 | `"score" lt 50` |
| `lte` | 小于等于 | `"priority" lte 5` |
| `set` | 字段已设置（非空） | `"email" set` |
| `notSet` | 字段未设置（空） | `"deletedAt" notSet` |
| `startsWith` | 以...开头 | `"code" startsWith "PRE-"` |
| `endsWith` | 以...结尾 | `"email" endsWith "@company.com"` |

### 4. 模板变量系统

行级过滤支持从 `securityContext` 动态注入值：

#### 内置变量

| 变量 | 来源 | 示例值 |
|------|------|--------|
| `{{userId}}` | `securityContext.userId` | `"bd254cd6-ada3-4803-88ec-a47749459169"` |
| `{{dataSourceId}}` | `securityContext.userScope.dataSource.id` | `"f9401258-4630-4005-8d47-1a682ae94cf3"` |
| `{{role}}` | `securityContext.userScope.role` | `"member"` / `"admin"` / `"owner"` |
| `{{teamId}}` | `securityContext.userScope.dataSource.teamId` | `"team-uuid"` |

#### 自定义变量（扩展）

通过在 JWT token 中添加自定义 claims：

```javascript
// JWT payload
{
  "hasura": {
    "x-hasura-user-id": "user-123",
    "x-hasura-department": "sales",
    "x-hasura-regions": "US,EU,ASIA",
    "x-hasura-manager-id": "manager-456"
  }
}
```

在行级过滤中使用：
```json
{
  "member": "Employees.department",
  "operator": "equals",
  "values": ["{{department}}"]
}
```

### 5. 多表关联场景

当查询涉及多个 Cube 时，每个 Cube 的 RLS policy 独立生效：

**示例查询**：
```json
{
  "dimensions": ["Users.name", "Orders.amount"],
  "measures": ["Orders.count"]
}
```

**应用的过滤**：
- `Users.userId = {{userId}}` （来自 rowLevelSecurity.policies.Users）
- `Orders.userId = {{userId}}` （来自 rowLevelSecurity.policies.Orders）

最终生成的 SQL（伪代码）：
```sql
SELECT
  users.name,
  orders.amount,
  COUNT(orders.id)
FROM users
JOIN orders ON users.id = orders.user_id
WHERE users.user_id = 'bd254cd6-...'  -- Users RLS policy
  AND orders.user_id = 'bd254cd6-...'  -- Orders RLS policy
```


## 实现方案

### 1. 更新 queryRewrite.js

```javascript
// services/cubejs/src/utils/queryRewrite.js

import { applyRowLevelSecurity } from './rowLevelSecurity.js';

const queryRewrite = async (query, { securityContext }) => {
  const { userScope } = securityContext;
  const { dataSourceAccessList, role } = userScope;

  // 1. 角色检查
  if (["owner", "admin"].includes(role)) {
    return query;
  }

  // 2. 数据源访问检查
  if (!dataSourceAccessList) {
    throw new Error("403: You have no access to the datasource");
  }

  // 3. 列级权限检查（现有逻辑）
  const queryNames = getColumnsArray(query);
  const accessNames = Object.values(dataSourceAccessList).reduce(
    (acc, cube) => [...acc, ...getColumnsArray(cube)],
    []
  );

  queryNames.forEach((cn) => {
    if (!accessNames.includes(cn)) {
      throw new Error(`403: You have no access to "${cn}" cube property`);
    }
  });

  // 4. 应用行级权限过滤 ✨ NEW
  const rewrittenQuery = await applyRowLevelSecurity(
    query,
    securityContext
  );

  return rewrittenQuery;
};

export default queryRewrite;
```

### 2. 创建 rowLevelSecurity.js

```javascript
// services/cubejs/src/utils/rowLevelSecurity.js

import { interpolateTemplate } from './templateVariables.js';
import { logging } from './logging.js';

/**
 * 从查询中提取所有涉及的 Cube 名称
 */
const extractCubeNamesFromQuery = (query) => {
  const cubeNames = new Set();

  const extractFromMembers = (members = []) => {
    members.forEach(member => {
      const cubeName = member.split('.')[0];
      cubeNames.add(cubeName);
    });
  };

  extractFromMembers(query.dimensions);
  extractFromMembers(query.measures);
  extractFromMembers(query.segments);

  if (query.filters) {
    query.filters.forEach(filter => {
      const cubeName = filter.member?.split('.')[0];
      if (cubeName) cubeNames.add(cubeName);
    });
  }

  return Array.from(cubeNames);
};

/**
 * 将模板变量转换为实际值
 */
const resolveFilterValues = (values, securityContext) => {
  return values.map(value =>
    interpolateTemplate(value, securityContext)
  );
};

/**
 * 应用行级安全过滤
 */
export const applyRowLevelSecurity = async (
  query,
  securityContext
) => {
  const { userScope } = securityContext;

  // ✨ 从 userScope 中获取行级安全配置（从 datasource 获取）
  const rowLevelSecurity = userScope.rowLevelSecurity;

  // 检查是否启用了行级安全
  if (!rowLevelSecurity || !rowLevelSecurity.enabled) {
    return query;
  }

  // ✨ 检查当前用户角色是否豁免
  const exemptRoles = rowLevelSecurity.exemptRoles || ['owner', 'admin'];
  if (exemptRoles.includes(userScope.role)) {
    logging('info', `[RLS] User role "${userScope.role}" is exempt from row-level security`);
    return query;
  }

  const policies = rowLevelSecurity.policies || {};
  const cubeNames = extractCubeNamesFromQuery(query);

  // 初始化 filters 数组
  const filters = query.filters || [];

  // 为每个涉及的 Cube 应用行级过滤
  for (const cubeName of cubeNames) {
    const policy = policies[cubeName];

    // 跳过没有配置策略的 Cube
    if (!policy) {
      continue;
    }

    // 检查是否启用
    if (!policy.enabled) {
      continue;
    }

    const logic = policy.logic || 'AND';
    const rules = policy.rules || [];

    if (rules.length === 0) {
      continue;
    }

    try {
      // 构建过滤条件
      const rowFilterConditions = rules.map(rule => ({
        member: rule.member,
        operator: rule.operator,
        values: resolveFilterValues(rule.values, securityContext)
      }));

      if (logic === 'OR' && rowFilterConditions.length > 1) {
        // OR 逻辑：包装在一个 or 组中
        filters.push({
          or: rowFilterConditions
        });
      } else {
        // AND 逻辑：直接添加到 filters
        filters.push(...rowFilterConditions);
      }

      logging('info', `[RLS] Applied row-level filters for ${cubeName}`, {
        cubeName,
        rulesCount: rowFilterConditions.length,
        logic
      });

    } catch (error) {
      logging('error', `[RLS] Failed to apply row filters for ${cubeName}:`, error);
      throw new Error(`500: Failed to apply row-level security for ${cubeName}`);
    }
  }

  // 返回带有行级过滤的查询
  return {
    ...query,
    filters: filters.length > 0 ? filters : query.filters
  };
};

/**
 * 验证行级过滤配置
 */
export const validateRowFiltersConfig = (rowFilters) => {
  if (!rowFilters || typeof rowFilters !== 'object') {
    return { valid: false, error: 'rowFilters must be an object' };
  }

  if (typeof rowFilters.enabled !== 'boolean') {
    return { valid: false, error: 'rowFilters.enabled must be a boolean' };
  }

  if (!rowFilters.enabled) {
    return { valid: true };
  }

  const { rules = [] } = rowFilters;

  if (!Array.isArray(rules)) {
    return { valid: false, error: 'rowFilters.rules must be an array' };
  }

  const validOperators = [
    'equals', 'notEquals', 'contains', 'notContains',
    'in', 'notIn', 'gt', 'gte', 'lt', 'lte',
    'set', 'notSet', 'startsWith', 'endsWith'
  ];

  for (const rule of rules) {
    if (!rule.member || typeof rule.member !== 'string') {
      return { valid: false, error: 'Each rule must have a member string' };
    }

    if (!validOperators.includes(rule.operator)) {
      return {
        valid: false,
        error: `Invalid operator: ${rule.operator}. Must be one of: ${validOperators.join(', ')}`
      };
    }

    if (!Array.isArray(rule.values)) {
      return { valid: false, error: 'rule.values must be an array' };
    }
  }

  return { valid: true };
};
```

### 3. 创建 templateVariables.js

```javascript
// services/cubejs/src/utils/templateVariables.js

/**
 * 从 JWT 的 hasura claims 中提取自定义变量
 */
const extractCustomVariables = (jwtDecoded) => {
  const hasura = jwtDecoded?.hasura || {};
  const customVars = {};

  for (const [key, value] of Object.entries(hasura)) {
    if (key.startsWith('x-hasura-') && key !== 'x-hasura-user-id') {
      // 转换 x-hasura-department -> department
      const varName = key.replace('x-hasura-', '');

      // 处理逗号分隔的列表
      if (typeof value === 'string' && value.includes(',')) {
        customVars[varName] = value.split(',').map(v => v.trim());
      } else {
        customVars[varName] = value;
      }
    }
  }

  return customVars;
};

/**
 * 构建模板变量上下文
 */
export const buildTemplateContext = (securityContext, jwtDecoded = null) => {
  const { userId, userScope } = securityContext;
  const { role, dataSource } = userScope || {};

  const context = {
    userId,
    role,
    dataSourceId: dataSource?.id,
    teamId: dataSource?.teamId,
    branchId: dataSource?.branchId,
    schemaVersion: dataSource?.schemaVersion
  };

  // 添加自定义变量
  if (jwtDecoded) {
    const customVars = extractCustomVariables(jwtDecoded);
    Object.assign(context, customVars);
  }

  return context;
};

/**
 * 插值模板字符串
 *
 * @param {string} template - 模板字符串，如 "{{userId}}"
 * @param {object} securityContext - 安全上下文
 * @returns {string} - 插值后的字符串
 */
export const interpolateTemplate = (template, securityContext) => {
  if (typeof template !== 'string') {
    return template;
  }

  // 检查是否包含模板变量
  if (!template.includes('{{')) {
    return template;
  }

  const context = buildTemplateContext(securityContext);

  // 替换 {{variable}} 为实际值
  return template.replace(/\{\{(\w+)\}\}/g, (match, varName) => {
    if (varName in context) {
      const value = context[varName];
      // 处理数组
      if (Array.isArray(value)) {
        return value.join(',');
      }
      return value;
    }

    // 变量不存在时抛出错误
    throw new Error(`Template variable "{{${varName}}}" not found in security context`);
  });
};
```

### 4. 更新 defineUserScope.js 提取 RLS 配置

```javascript
// services/cubejs/src/utils/defineUserScope.js

export const getDataSourceAccessList = (
  allMembers,
  selectedDataSourceId,
  selectedTeamId
) => {
  const dataSourceMemberRole = allMembers.find(
    (member) => member.team_id === selectedTeamId
  )?.member_roles?.[0];

  if (!dataSourceMemberRole) {
    throw new Error(`403: member role not found`);
  }

  const { access_list: accessList } = dataSourceMemberRole;
  const config = accessList?.config || {};

  const datasourceConfig = config?.datasources?.[selectedDataSourceId] || {};

  // 只提取列级权限（纵切）
  const dataSourceAccessList = datasourceConfig.cubes || {};

  return {
    role: dataSourceMemberRole?.team_role,
    dataSourceAccessList  // 纵切：列级权限
  };
};

const defineUserScope = (
  allDataSources,
  allMembers,
  selectedDataSourceId,
  selectedBranchId,
  selectedVersionId
) => {
  const dataSource = allDataSources.find(
    (source) => source.id === selectedDataSourceId
  );

  if (!dataSource) {
    throw new Error(`404: source "${selectedDataSourceId}" not found`);
  }

  // ... branch/version 查找逻辑 ...

  const {
    role,
    dataSourceAccessList
  } = getDataSourceAccessList(
    allMembers,
    selectedDataSourceId,
    dataSource.team_id
  );

  const dataSourceContext = buildSecurityContext(
    dataSource,
    selectedBranch,
    selectedVersion
  );

  // ✨ 从 datasource 获取 RLS 配置（横切）
  const rowLevelSecurity = dataSource.rls_config || {
    enabled: false,
    exemptRoles: ['owner', 'admin'],
    policies: {}
  };

  return {
    dataSource: dataSourceContext,
    role,
    dataSourceAccessList,   // 纵切：从 access_list 获取
    rowLevelSecurity        // 横切：从 datasource 获取 ✨
  };
};

export default defineUserScope;
```

### 5. 更新 checkAuth.js 传递 JWT

```javascript
// services/cubejs/src/utils/checkAuth.js

const checkAuth = async (req) => {
  // ... 现有代码 ...

  let jwtDecoded;
  try {
    jwtDecoded = jwt.verify(authToken, JWT_KEY, {
      algorithms: [JWT_ALGORITHM],
    });
  } catch (err) {
    throw err;
  }

  // ... 现有代码 ...

  req.securityContext = {
    authToken,
    userId,
    userScope,  // ✨ 现在包含 rowLevelSecurity
    jwtDecoded  // ✨ 添加 JWT decoded 对象供模板变量使用
  };
};
```

## 使用示例

### 场景 1：用户只能查看自己的数据

**行级权限配置**（`datasources.rls_config`）：
```json
{
  "enabled": true,
  "exemptRoles": ["owner", "admin"],
  "policies": {
    "Orders": {
      "enabled": true,
      "rules": [
        {
          "member": "Orders.userId",
          "operator": "equals",
          "values": ["{{userId}}"]
        }
      ]
    }
  }
}
```

**用户查询**：
```json
{
  "dimensions": ["Orders.id", "Orders.amount"],
  "measures": ["Orders.count"]
}
```

**自动注入的过滤**：
```json
{
  "dimensions": ["Orders.id", "Orders.amount"],
  "measures": ["Orders.count"],
  "filters": [
    {
      "member": "Orders.userId",
      "operator": "equals",
      "values": ["bd254cd6-ada3-4803-88ec-a47749459169"]
    }
  ]
}
```

### 场景 2：部门经理查看本部门数据

**JWT Payload**：
```json
{
  "hasura": {
    "x-hasura-user-id": "manager-123",
    "x-hasura-department": "sales",
    "x-hasura-role": "manager"
  }
}
```

**行级权限配置**（`datasources.rls_config`）：
```json
{
  "enabled": true,
  "exemptRoles": ["owner"],
  "policies": {
    "Employees": {
      "enabled": true,
      "logic": "OR",
      "rules": [
        {
          "member": "Employees.employeeId",
          "operator": "equals",
          "values": ["{{userId}}"]
        },
        {
          "member": "Employees.department",
          "operator": "equals",
          "values": ["{{department}}"]
        }
      ]
    }
  }
}
```

**结果**：经理可以看到自己的数据 OR 本部门的数据

### 场景 3：多区域销售数据隔离

**JWT Payload**：
```json
{
  "hasura": {
    "x-hasura-user-id": "sales-456",
    "x-hasura-regions": "US,EU"
  }
}
```

**行级权限配置**（`datasources.rls_config`）：
```json
{
  "enabled": true,
  "exemptRoles": ["owner", "admin"],
  "policies": {
    "Sales": {
      "enabled": true,
      "rules": [
        {
          "member": "Sales.region",
          "operator": "in",
          "values": ["{{regions}}"]
        },
        {
          "member": "Sales.status",
          "operator": "notEquals",
          "values": ["deleted"]
        }
      ]
    }
  }
}
```

## 安全考虑

### 1. 防止权限绕过

- ✅ **强制应用**：`owner`/`admin` 外的所有角色必须经过行级过滤
- ✅ **合并过滤**：用户传入的 filters 与行级过滤合并（AND 逻辑）
- ✅ **模板变量验证**：未找到的变量抛出错误而非静默忽略

### 2. 性能优化

- 缓存模板变量上下文（相同请求只构建一次）
- 使用 Cube.js 的 pre-aggregation 时确保行级过滤包含在 key 中
- 考虑在数据库层创建索引优化常用的过滤字段

### 3. 审计日志

记录行级过滤的应用情况：

```javascript
logging('info', '[RLS] Applied row-level security', {
  userId: securityContext.userId,
  dataSourceId: securityContext.userScope.dataSource.id,
  cubesFiltered: ['Orders', 'Users'],
  appliedFilters: [/* ... */],
  timestamp: new Date().toISOString()
});
```

## 数据库迁移

创建迁移脚本为 datasources 表添加 RLS 配置字段：

```sql
-- services/hasura/migrations/{timestamp}_add_row_security_to_datasources/up.sql

-- 为 datasources 表添加行级安全配置字段
ALTER TABLE public.datasources
ADD COLUMN rls_config JSONB DEFAULT NULL;

CREATE INDEX idx_datasources_row_security_enabled
  ON public.datasources((rls_config->>'enabled'))
  WHERE rls_config IS NOT NULL;

COMMENT ON COLUMN public.datasources.rls_config IS
'Row-level security policies (user-based, applies to all roles except exempt ones)';

-- 为现有 datasources 添加默认配置（禁用状态）
UPDATE public.datasources
SET rls_config = jsonb_build_object(
  'enabled', false,
  'exemptRoles', ARRAY['owner', 'admin']::TEXT[],
  'policies', '{}'::jsonb
)
WHERE rls_config IS NULL;
```

**down.sql（回滚）**：
```sql
-- services/hasura/migrations/{timestamp}_add_row_security_to_datasources/down.sql

DROP INDEX IF EXISTS idx_datasources_row_security_enabled;
ALTER TABLE public.datasources DROP COLUMN IF EXISTS rls_config;
```

**迁移后的数据结构**：
```
datasources 表
├─ id: "ds-uuid"
├─ name: "Production DB"
├─ db_type: "postgres"
├─ db_params: { ... }
└─ rls_config: {
     "enabled": false,
     "exemptRoles": ["owner", "admin"],
     "policies": {}
   }
```

## 测试计划

### 1. 单元测试

```javascript
// services/cubejs/src/utils/__tests__/rowLevelSecurity.test.js

describe('applyRowLevelSecurity', () => {
  test('should apply single filter rule', async () => {
    const query = {
      dimensions: ['Orders.id'],
      measures: ['Orders.count']
    };

    const securityContext = {
      userId: 'user-123',
      userScope: {
        role: 'member',
        rowLevelSecurity: {
          enabled: true,
          policies: {
            Orders: {
              enabled: true,
              rules: [
                {
                  member: 'Orders.userId',
                  operator: 'equals',
                  values: ['{{userId}}']
                }
              ]
            }
          }
        }
      }
    };

    const result = await applyRowLevelSecurity(
      query,
      securityContext
    );

    expect(result.filters).toHaveLength(1);
    expect(result.filters[0]).toEqual({
      member: 'Orders.userId',
      operator: 'equals',
      values: ['user-123']
    });
  });

  test('should handle OR logic', async () => {
    // ...
  });

  test('should skip when disabled', async () => {
    // ...
  });

  test('should throw error for missing template variable', async () => {
    // ...
  });
});
```

### 2. 集成测试

```javascript
// services/cubejs/src/__tests__/integration/rowLevelSecurity.integration.test.js

describe('Row-Level Security Integration', () => {
  test('should filter query results based on user context', async () => {
    // 1. Setup test datasource with RLS config
    // 2. Make authenticated request
    // 3. Verify filters are applied
    // 4. Verify results only contain authorized data
  });
});
```

## 部署清单

- [ ] 创建 `rowLevelSecurity.js` 模块
- [ ] 创建 `templateVariables.js` 模块
- [ ] 更新 `queryRewrite.js`
- [ ] 更新 `checkAuth.js` 传递 jwtDecoded
- [ ] 创建数据库迁移
- [ ] 编写单元测试
- [ ] 编写集成测试
- [ ] 更新 API 文档
- [ ] 创建用户配置指南

## 未来扩展

### 1. 动态函数支持

```json
{
  "member": "Orders.createdAt",
  "operator": "gte",
  "values": ["{{date.subtract(30, 'days')}}"]
}
```

## 总结

### 架构优势

这个重新设计的方案提供了：

#### 1. 关注点分离 ✅
```
纵切（列级权限 - Role-Based）     横切（行级权限 - User-Based）
access_lists.config               datasources.rls_config
├─ datasources[id]                ├─ enabled
   └─ cubes                       ├─ exemptRoles: ["owner", "admin"]
      ├─ dimensions               └─ policies
      ├─ measures                    └─ CubeName
      └─ segments                        └─ rules

每个 role 独立配置 ❌ 重复      所有用户共享 ✅ 无重复
```

- **纵切和横切完全解耦**：
  - 列级权限在 `access_lists` 表（per-role）
  - 行级权限在 `datasources` 表（per-datasource）
- **避免重复定义**：RLS 规则不需要为每个 role 重复定义
- **清晰的职责边界**：
  - 列级权限：基于角色（owner 看所有字段，member 看部分字段）
  - 行级权限：基于用户个体（`userId = {{userId}}`）
- **豁免机制**：通过 `exemptRoles` 指定哪些角色跳过 RLS

#### 2. 核心特性 ✅

- ✅ **通用性**：支持14种运算符和 AND/OR 组合逻辑
- ✅ **灵活性**：模板变量系统支持从 JWT/securityContext 动态注入
- ✅ **安全性**：强制模式确保权限不被绕过
- ✅ **兼容性**：与现有列级权限无缝集成，零破坏性变更
- ✅ **可扩展性**：易于添加新的运算符、变量和策略引擎
- ✅ **可维护性**：清晰的配置结构和验证机制
- ✅ **性能**：单次查询获取所有权限配置

#### 3. 实施方案

**在 `datasources` 表添加 `rls_config` 字段**：
- ✅ **完全避免重复定义**：RLS 规则在 datasource 级别定义一次，所有角色共享
- ✅ **配置与 datasource 天然关联**：每个数据源拥有自己的行级安全策略
- ✅ **查询性能最优**：无额外 JOIN，直接从 datasource 获取
- ✅ **实施简单**：只需添加一个 JSONB 字段
- ✅ **豁免机制**：通过 `exemptRoles` 灵活控制哪些角色跳过 RLS

### 适用场景

该方案满足以下企业级需求：

- 🏢 **多租户 SaaS**：每个租户只能看到自己的数据
- 🏛️ **部门数据隔离**：销售部门只能看销售数据，财务部门只能看财务数据
- 🌍 **地理区域隔离**：EU 用户只能访问 EU 数据（GDPR 合规）
- 👥 **层级权限**：经理可以看到下属的数据 + 自己的数据
- 🔐 **合规审计**：记录所有行级过滤的应用情况
- 📊 **动态权限**：基于用户属性（部门、角色、clearance level）动态过滤

### 与 Cube.js 最佳实践对比

Cube.js 官方推荐的行级安全实现：

```javascript
// Cube.js 官方方式（在 cube schema 中硬编码）
cube('Orders', {
  sql: `SELECT * FROM orders WHERE user_id = '${SECURITY_CONTEXT.userId}'`,
  // ...
});
```

**问题**：
- ❌ 权限逻辑硬编码在 schema 中
- ❌ 无法在运行时动态修改权限
- ❌ 需要重新部署才能修改权限规则

**Synmetrix 的方案**：
- ✅ 权限配置存储在数据库中
- ✅ 可以通过 UI 动态配置权限
- ✅ 支持多角色、多策略
- ✅ 无需重新部署即可调整权限

### 数据查询更新

需要更新 `dataSourceHelpers.js` 的 GraphQL 查询以获取 `rls_config`：

```javascript
// services/cubejs/src/utils/dataSourceHelpers.js

const sourceFragment = `
  id
  name
  db_type
  db_params
  team_id
  rls_config  // ✨ 添加这一行
`;

// 查询结果自动包含 rls_config
const dataSource = allDataSources.find(
  (source) => source.id === selectedDataSourceId
);

// 在 defineUserScope 中可以直接访问
const rowLevelSecurity = dataSource.rls_config || {
  enabled: false,
  exemptRoles: ['owner', 'admin'],
  policies: {}
};
```
