

本地开发环境搭建：
https://docs.synmetrix.org/docs/development/local-development

Login: demo@synmetrix.org
Password: demodemo

测试分析库：
https://gh-api.clickhouse.com/


1. 代码定位问题有点困难。需要补充日志。
2. 很难用，特别是网络慢的时候。需要添加取消按钮。有提供操作手册，可以参考使用 https://docs.synmetrix.org/docs/user-guide
    1. 没有 reset 按钮，将选择的 measure/dimension 清理掉。现在需要找到 measure/dimension 后再删。
    2. todo: 缺少可视化功能。
    3. 可能前端需要重新做一个
    4. fixme: filter 里不知道怎么选择空字符串呀
3. Apple Silicon 上使用 x86 架构的 v1.2.3 版本的 cubestore 有问题： CPU 占用 100%，无法接收任何请求（3030 端口）。—— 使用 cubestore 的 arm 版本即可。
4. model 文件仍然是 YAML 文件的形式保存？但是权限管理（创建 Role）上倒是可以做到字段粒度
5. hasura/graphql-engine 开启了 console， http://localhost:8080/console 可以直接访问 PG 数据库和 Actions 设置。 https://hub.docker.com/r/hasura/graphql-engine
6. 依赖 redis 的功能并不是核心功能

## Hasura

https://github.com/hasura/graphql-engine/blob/master/V2-README.md

Frontend -> Hasura GraphQL -> Actions API -> CubeJS API

1. 根据数据库的模式自动生成 GraphQL API
2. Hasura 提供了灵活的权限管理功能，开发者可以根据用户角色和操作类型设置不同的权限规则
3. Hasura 可以与各种现有系统进行集成，如身份验证系统、缓存系统、消息队列等。它支持多种身份验证方式，如 JWT 认证、OAuth 等，可以方便地与现有的用户认证系统集成
4. 通过使用 Actions 来扩展、处理用例


### 核心概念：元数据 vs. 业务数据

要理解这几个变量，首先要区分两种数据库：

1.  **元数据数据库 (Metadata Database)**: 这个数据库存储 Hasura 自身的配置信息。它就像是 Hasura 的“大脑”或“项目文件”。里面不包含你的用户、产品等业务数据，而是存储了诸如：

      * 你通过 Hasura 追踪了哪些表和视图。
      * 表之间的关系（Relationships）。
      * 权限规则（Permissions）。
      * 动作（Actions）、远程模式（Remote Schemas）、事件触发器（Event Triggers）等所有你在 Hasura 控制台上的配置。
      * **这个数据库对 Hasura 的运行至关重要。**

2.  **业务数据库 (Application Database)**: 这就是你实际存放应用程序数据的数据库，比如 `users`、`products`、`orders` 表等。这是你希望通过 Hasura GraphQL API 来操作的数据源。你可以连接一个或多个业务数据库。


| 环境变量 | 主要用途 | 指向的数据库 | 是否必需？ | 备注 |
| :--- | :--- | :--- | :--- | :--- |
| **`HASURA_GRAPHQL_METADATA_DATABASE_URL`** | **Hasura 的配置（元数据）** | 元数据数据库 | **是**（除非设置了 `PG_DATABASE_URL` 作为备用） | 生产环境强烈建议独立设置。 |
| **`HASURA_GRAPHQL_DATABASE_URL`** | **你的应用程序数据（业务数据）** | 业务数据库 | **是**（除非设置了 `PG_DATABASE_URL` 作为备用） | 连接你的主数据源。 |
| **`PG_DATABASE_URL`** | **备用/默认选项** | 元数据和/或业务数据库 | **否** | 如果前两个变量未设置，Hasura 会用它来填充。适合简单部署。 |

### **Remote Schemas** vs. **Actions**

都是用来扩展 Hasura 默认生成的数据库 CRUD API 的，但它们的**目标和实现方式完全不同**。

一个最核心的比喻：
* **Remote Schemas 是“联邦”**：你已经有了一个独立的、完整的 GraphQL 服务，你想把它无缝地“拼接”到 Hasura 的 GraphQL API 中，形成一个统一的入口。
* **Actions 是“扩展”**：你没有现成的 GraphQL 服务，但你需要实现一些自定义的业务逻辑（比如调用一个 REST API、发送邮件、处理支付），你想把这些逻辑“变成”GraphQL API 的一部分。

| 特性 | Remote Schemas | Actions |
| :--- | :--- | :--- |
| **核心用途** | **联邦 (Federation)**：合并已有的 GraphQL 服务 | **扩展 (Extension)**：添加新的自定义业务逻辑 |
| **服务端点类型** | 必须是 **GraphQL 服务** | 任何 **HTTP(S) Webhook** (REST API, Serverless 等) |
| **Schema 定义方** | 由**远程服务**自己定义，Hasura 负责读取 | 由**你在 Hasura 中**手动定义 |
| **通信协议** | `Client <-> Hasura <-> Remote Service` (全程 GraphQL) | `Client <-(GraphQL)-> Hasura <-(HTTP/JSON)-> Your Service` |
| **主要解决的问题** | 如何统一管理和访问多个分散的 GraphQL 服务 | 如何将非 CRUD 或非数据库的业务逻辑暴露为 GraphQL API |
| **何时选择** | 当你已经有一个或多个想集成的 GraphQL 服务时 | 当你需要实现自定义逻辑、调用 REST API 或 Serverless 函数时 |

* 如果你要连接的服务**已经提供了 GraphQL API**，那么毫无疑问，使用 **Remote Schemas**。这是最直接、最高效的方式。
* 如果你需要实现一段**自定义代码**（比如数据验证、调用另一个 REST API、发送通知等），并且希望把它作为 GraphQL API 的一部分暴露给前端，那么使用 **Actions**。
