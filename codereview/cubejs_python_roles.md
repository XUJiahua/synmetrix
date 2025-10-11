# Cube.js 项目中 Python 的作用

## 概述

Cube.js 项目主体是用 **TypeScript/JavaScript** 实现的，但也支持 **Python** 作为配置语言和数据模型语言。

## Python 的作用

### 1. 配置语言（Configuration Language）

Python 可以用来编写 Cube.js 的配置文件 `cube.py`，作为 JavaScript 配置文件 `cube.js` 的替代方案。

#### 1.1 基本用法

**Python 配置示例** (`cube.py`):
```python
from cube import config

# 基础配置
config.base_path = '/cube-api'
config.schema_path = 'models'
config.pg_sql_port = 5555
config.telemetry = False

# 认证函数
@config
async def check_auth(req, authorization):
    return {
        "security_context": {
            "sub": "1234567890",
            "user_id": 42
        }
    }

# 查询重写
@config('query_rewrite')
def query_rewrite(query: dict, ctx: dict) -> dict:
    if 'order_id' in ctx['securityContext']:
        query['filters'].append({
            'member': 'orders_view.id',
            'operator': 'equals',
            'values': [ctx['securityContext']['order_id']]
        })
    return query

# 多租户配置
@config('context_to_roles')
def context_to_roles(context):
    return context.get("securityContext", {}).get("auth", {}).get("roles", [])

@config('context_to_groups')
def context_to_groups(ctx):
    return ["dev", "analytics"]
```

**等价的 JavaScript 配置** (`cube.js`):
```javascript
module.exports = {
  basePath: '/cube-api',
  schemaPath: 'models',
  pgSqlPort: 5555,
  telemetry: false,

  checkAuth: async (req, authorization) => {
    return {
      securityContext: {
        sub: "1234567890",
        userId: 42
      }
    };
  },

  queryRewrite: (query, { securityContext }) => {
    if (securityContext.order_id) {
      query.filters.push({
        member: 'orders_view.id',
        operator: 'equals',
        values: [securityContext.order_id]
      });
    }
    return query;
  },

  contextToRoles: ({ securityContext }) => {
    return securityContext?.auth?.roles || [];
  },

  contextToGroups: () => {
    return ["dev", "analytics"];
  }
};
```

#### 1.2 支持的配置选项

Python 配置支持所有 JavaScript 配置选项，包括：

**基本配置属性**：
- `api_secret` - API 密钥
- `base_path` - REST API 基础路径
- `schema_path` - 数据模型路径
- `pg_sql_port` - PostgreSQL 协议端口
- `telemetry` - 遥测开关
- `cache_and_queue_driver` - 缓存和队列驱动
- `dev_server` - 开发服务器配置
- `web_sockets` - WebSocket 配置

**配置函数**（使用 `@config` 装饰器）：
- `check_auth` - 认证检查
- `check_sql_auth` - SQL 认证检查
- `query_rewrite` - 查询重写
- `context_to_app_id` - 多租户应用 ID
- `context_to_orchestrator_id` - 编排器 ID
- `context_to_cube_store_router_id` - CubeStore 路由 ID
- `context_to_roles` - 角色映射
- `context_to_groups` - 组映射
- `context_to_api_scopes` - API 作用域
- `extend_context` - 上下文扩展
- `repository_factory` - 仓库工厂
- `scheduled_refresh_contexts` - 定时刷新上下文
- `scheduled_refresh_time_zones` - 定时刷新时区
- `schema_version` - Schema 版本
- `pre_aggregations_schema` - 预聚合 Schema
- `logger` - 日志记录器
- `driver_factory` - 驱动工厂
- `external_driver_factory` - 外部驱动工厂

### 2. 数据模型语言（Data Modeling Language）

除了配置，Python 还可以用于编写数据模型（Cubes、Views、Dimensions、Measures）。这是**动态数据建模**的一部分，与 Jinja 模板配合使用。

#### 2.1 Python 在数据模型中的应用

虽然大多数数据模型使用 JavaScript 或 YAML 格式，但 Python 可以通过 Jinja 模板引擎进行动态生成。

官方文档提到：
> "You can read more about Python and JavaScript support in the dynamic data modeling section of the documentation."

引用位置：`docs/pages/product/configuration.mdx:134`

### 3. 技术实现

#### 3.1 架构层级

```
┌─────────────────────────────────────┐
│   Cube.js Core (TypeScript/JS)     │
│   - Server Core                     │
│   - API Gateway                     │
│   - Schema Compiler                 │
│   - Query Orchestrator              │
└──────────────┬──────────────────────┘
               │
               ▼
┌─────────────────────────────────────┐
│  @cubejs-backend/native (Rust)     │
│  - Node.js 绑定（N-API/Neon）       │
│  - Python 绑定（PyO3）              │
│  - 配置加载和解析                    │
└──────────────┬──────────────────────┘
               │
      ┌────────┴────────┐
      ▼                 ▼
┌─────────────┐   ┌─────────────┐
│  cube.js    │   │  cube.py    │
│  (JS 配置)  │   │ (Python配置)│
└─────────────┘   └─────────────┘
```

#### 3.2 Rust 原生模块实现

`@cubejs-backend/native` 包是一个 **Rust 原生模块**，通过以下技术实现：

1. **Neon** - Node.js 绑定
   - 将 Rust 代码编译为 Node.js 原生模块
   - 文件：`src/lib.rs`, `src/node_export.rs`

2. **PyO3** - Python 绑定（可选）
   - 将 Rust 代码嵌入 Python 解释器
   - 文件：`src/python/mod.rs`, `src/python/runtime.rs`
   - 特性标志：`features = ["python"]`

3. **跨语言通信**
   - `src/cross/clrepr.rs` - 跨语言数据表示
   - `src/cross/clrepr_python.rs` - Python 数据转换
   - `src/cross/py_in_js.rs` - Python 到 JavaScript 的桥接

#### 3.3 Cargo.toml 配置

```toml
[dependencies]
# Python 绑定（可选特性）
pyo3 = { version = "0.20.0", features = [], optional = true }
pyo3-asyncio = { version = "0.20.0", features = [
    "tokio-runtime",
    "attributes",
], optional = true }

# Node.js 绑定
[dependencies.neon]
version = "=1"
default-features = false
features = ["napi-1", "napi-4", "napi-6", "futures"]

[features]
default = ["neon-entrypoint"]
python = ["pyo3", "pyo3-asyncio"]
```

#### 3.4 Python 配置加载流程

```
cube.py 文件
    │
    ▼
PyO3 加载 Python 解释器
    │
    ▼
执行 Python 代码
提取配置对象和函数
    │
    ▼
CLRepr (跨语言表示)
转换 Python 对象为中间格式
    │
    ▼
Neon 转换为 JavaScript 对象
    │
    ▼
传递给 Cube.js Core
(TypeScript/JavaScript)
```

关键代码位置：
- `src/python/cube_config.rs` - Python 配置解析
- `src/python/runtime.rs` - Python 运行时管理
- `src/cross/clrepr_python.rs` - Python 到中间格式转换
- `src/cross/py_in_js.rs` - 中间格式到 JavaScript 转换

### 4. 平台支持

#### 4.1 带 Python 支持的构建

支持的 Python 版本：`3.12`, `3.11`, `3.10`, `3.9`

| 平台 | 架构 | 支持 |
|------|------|------|
| Linux (GNU) | x86_64 | ✅ |
| Linux (GNU) | ARM64 | ✅ |
| macOS | x86_64 | ❌ |
| macOS | ARM64 | ❌ |
| Windows | x86_64 | ❌ |
| Windows | ARM64 | ❌ |

#### 4.2 Fallback 构建（无 Python）

当系统不支持 Python 或无法检测到 `libpython` 库时，使用 fallback 构建。

| 平台 | 架构 | 支持 |
|------|------|------|
| Linux (GNU) | x86_64 | ✅ |
| Linux (GNU) | ARM64 | ✅ |
| macOS | x86_64 | ✅ |
| macOS | ARM64 | ✅ |
| Windows | x86_64 | ✅ |

来源：`packages/cubejs-backend-native/README.md`

#### 4.3 依赖安装

**Python 依赖** (`requirements.txt`):
```txt
jinja2==3.1.2
pandas==1.5.0
```

**安装方式**：
- **Cube Core (Docker)**：在容器内运行 `pip install -r requirements.txt`
- **Cube Cloud**：自动安装

**JavaScript 依赖** (`package.json`):
```json
{
  "dependencies": {
    "moment": "^2.29.4"
  }
}
```

**安装方式**：
- **Cube Core (Docker)**：在容器内运行 `npm install`
- **Cube Cloud**：自动安装

来源：`docs/pages/product/configuration.mdx:156-178`

### 5. 实际使用案例

#### 5.1 多租户 + RBAC 配置

来自测试用例：`packages/cubejs-testing/birdbox-fixtures/rbac-python/cube.py`

```python
from cube import config

@config('context_to_roles')
def context_to_roles(context):
    """从安全上下文中提取角色"""
    return context.get("securityContext", {}).get("auth", {}).get("roles", [])

@config('query_rewrite')
def query_rewrite(query: dict, ctx: dict) -> dict:
    """强制查询必须包含过滤器"""
    filters = extract_matching_dicts(query.get('filters'))

    for value in range(len(query['timeDimensions'])):
        filters.append(query['timeDimensions'][value]['dateRange'])

    if not filters or None in filters:
        raise Exception("Queries can't be run without a filter")
    return query

@config('check_sql_auth')
def check_sql_auth(query: dict, username: str, password: str) -> dict:
    """SQL 接口认证"""
    if username == 'admin':
        return {
            'username': 'admin',
            'password': password,
            'securityContext': {
                'auth': {
                    'username': 'admin',
                    'userAttributes': {
                        'canHaveAdmin': True,
                        'city': 'New York'
                    },
                    'roles': ['admin']
                }
            }
        }
    raise Exception("Invalid username or password")
```

#### 5.2 完整配置示例

来自测试用例：`packages/cubejs-backend-native/test/config.py`

```python
from cube import config, file_repository
from utils import test_function

# 属性配置
config.schema_path = "models"
config.pg_sql_port = 5555
config.telemetry = False

# 查询重写
@config
def query_rewrite(query, ctx):
    query = test_function(query)
    print("[python] query_rewrite query=", query, " ctx=", ctx)
    return query

# 异步认证
@config
async def check_auth(req, authorization):
    print("[python] check_auth req=", req, " authorization=", authorization)
    return {
        "security_context": {
            "sub": "1234567890",
            "iat": 1516239022,
            "user_id": 42
        },
        "ignoredField": "should not be visible",
    }

# 上下文扩展
@config('extend_context')
def extend_context(req):
    print("[python] extend_context req=", req)
    if "securityContext" not in req:
        return {
            "security_context": {
                "error": "missing",
            }
        }

    req["securityContext"]["extended_by_config"] = True

    return {
        "security_context": req["securityContext"],
    }

# 仓库工厂（动态 schema 路径）
@config
async def repository_factory(ctx):
    print("[python] repository_factory ctx=", ctx)
    return file_repository(ctx["securityContext"]["schemaPath"])

# API 作用域
@config
async def context_to_api_scopes():
    print("[python] context_to_api_scopes")
    return ["meta", "data", "jobs"]

# 定时刷新时区
@config
async def scheduled_refresh_time_zones(ctx):
    print("[python] scheduled_refresh_time_zones ctx=", ctx)
    return ["Europe/Kyiv", "Antarctica/Troll", "Australia/Sydney"]

# 定时刷新上下文（多租户）
@config
async def scheduled_refresh_contexts(ctx):
    print("[python] scheduled_refresh_contexts ctx=", ctx)
    return [
        {
            "securityContext": {
                "appid": 'test1',
                "u": {"prop1": "value1"}
            }
        },
        {
            "securityContext": {
                "appid": 'test2',
                "u": {"prop1": "value2"}
            }
        },
        {
            "securityContext": {
                "appid": 'test3',
                "u": {"prop1": "value3"}
            }
        },
    ]

# Schema 版本控制
@config
def schema_version(ctx):
    print("[python] schema_version", ctx)
    return "1"

# 预聚合 Schema
@config
def pre_aggregations_schema(ctx):
    print("[python] pre_aggregations_schema", ctx)
    return "schema"

# 自定义日志
@config
def logger(msg, params):
    print("[python] logger msg", msg, "params=", params)

# 角色映射
@config
def context_to_roles(ctx):
    print("[python] context_to_roles", ctx)
    return ["admin"]

# 组映射
@config
def context_to_groups(ctx):
    print("[python] context_to_groups", ctx)
    return ["dev", "analytics"]
```

### 6. Python vs JavaScript 对比

| 维度 | Python | JavaScript |
|------|--------|-----------|
| **配置文件** | `cube.py` | `cube.js` |
| **导入方式** | `from cube import config` | `module.exports = {...}` |
| **属性设置** | `config.schema_path = "models"` | `schemaPath: "models"` |
| **函数装饰器** | `@config('query_rewrite')` | `queryRewrite: (query, ctx) => {...}` |
| **异步支持** | `async def check_auth(...)` | `async (req, auth) => {...}` |
| **命名风格** | `snake_case` | `camelCase` |
| **运行时** | CPython (3.9-3.12) | Node.js |
| **平台支持** | 仅 Linux (x64/ARM64) | 全平台 |
| **性能** | 略慢（跨语言调用开销） | 快（原生运行） |
| **生态系统** | 可使用 Python 库 (Pandas, NumPy) | 可使用 npm 包 |
| **官方推荐** | "When in doubt, use Python" | - |

来源：`docs/pages/product/configuration.mdx:115`

### 7. Python 的优势和使用场景

#### 7.1 优势

1. **简洁的语法**：Python 的装饰器语法比 JavaScript 对象更直观
2. **强大的数据处理库**：可以使用 Pandas、NumPy 等进行复杂的数据转换
3. **类型提示**：Python 的类型提示（Type Hints）可以提高代码可读性
4. **异步支持**：原生支持 `async/await`
5. **与数据科学工具集成**：如 Jupyter Notebook、Streamlit

#### 7.2 使用场景

**推荐使用 Python 的场景**：
- 需要复杂的数据转换逻辑
- 团队主要使用 Python
- 需要集成 Python 数据科学库
- 在 Linux 环境部署
- 与 Jupyter、Streamlit 等工具集成

**推荐使用 JavaScript 的场景**：
- 需要跨平台支持（特别是 macOS、Windows）
- 团队主要使用 JavaScript/TypeScript
- 需要最佳性能（避免跨语言调用）
- 需要使用 npm 生态系统

### 8. 官方推荐

根据官方文档：

> "Both ways are equivalent; **when in doubt, use Python**."

引用位置：`docs/pages/product/configuration.mdx:115`

这表明 Cube 官方**推荐优先使用 Python** 作为配置语言。

## 技术栈总结

### 核心技术栈

| 层级 | 语言/技术 | 用途 |
|------|-----------|------|
| **核心服务** | TypeScript/JavaScript | 主要业务逻辑 |
| **原生模块** | Rust | 高性能组件（CubeSQL、CubeStore、配置加载） |
| **配置层** | Python 或 JavaScript | 用户配置文件 |
| **数据建模** | JavaScript/Python/YAML | 数据模型定义 |
| **模板引擎** | Jinja2 (Python) | 动态模板渲染 |

### 依赖的运行时

| 运行时 | 版本 | 用途 |
|--------|------|------|
| **Node.js** | 22.20.0 | 主运行时 |
| **Python** | 3.9-3.12 | 配置和模板（可选） |
| **Rust** | nightly-2025-08-01 | 编译原生模块 |

来源：
- Node.js 版本：`packages/cubejs-docker/latest.Dockerfile:1`
- Python 版本：`packages/cubejs-backend-native/README.md:24`
- Rust 版本：`rust/cubestore/rust-toolchain.toml`

## 运行时环境

### 1. Cube Core (Docker)

Docker 镜像包含：
- ✅ Node.js 22.20.0
- ✅ Python 3.x（仅 Linux x64/ARM64 镜像）
- ✅ 所有 npm 包
- ✅ CubeStore 和 CubeSQL（Rust 编译的二进制）

### 2. Cube Cloud

自动支持：
- ✅ Python 配置 (`cube.py`)
- ✅ JavaScript 配置 (`cube.js`)
- ✅ 自动安装 `requirements.txt` 依赖
- ✅ 自动安装 `package.json` 依赖

## 总结

### Python 的作用

1. **配置语言**：作为 `cube.js` 的替代方案，用于编写 Cube 配置
2. **数据建模**：与 Jinja 模板配合进行动态数据建模
3. **数据处理**：利用 Python 生态（Pandas、NumPy）进行复杂数据转换
4. **集成工具**：与 Jupyter、Streamlit 等数据科学工具集成

**技术实现**：
- 通过 Rust 原生模块 (`@cubejs-backend/native`) 嵌入 Python 解释器
- 使用 PyO3 实现 Rust 和 Python 的互操作
- 使用跨语言表示（CLRepr）在 Python、Rust、JavaScript 之间转换数据

**平台限制**：
- 仅在 Linux (x64/ARM64) 上支持完整的 Python 功能
- 其他平台使用 fallback 构建（无 Python 支持）

### 官方推荐

> "Both ways are equivalent; when in doubt, use Python."

当在 Python 和 JavaScript 之间选择时，**官方推荐优先使用 Python**。

---

**文档创建时间**：2025-10-11
**Cube.js 版本**：master 分支（最新版）
**分析工具**：Claude Code
