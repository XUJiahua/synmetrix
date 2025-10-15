

本地开发环境搭建：
https://docs.synmetrix.org/docs/development/local-development

Login: demo@synmetrix.org
Password: demodemo


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

### Hasura

Frontend -> Hasura GraphQL -> Actions API -> CubeJS API

1. 根据数据库的模式自动生成 GraphQL API
2. Hasura 提供了灵活的权限管理功能，开发者可以根据用户角色和操作类型设置不同的权限规则
3. Hasura 可以与各种现有系统进行集成，如身份验证系统、缓存系统、消息队列等。它支持多种身份验证方式，如 JWT 认证、OAuth 等，可以方便地与现有的用户认证系统集成
4. 通过使用 Actions 来扩展、处理用例

