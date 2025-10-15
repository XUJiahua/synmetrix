

本地开发环境搭建：
https://docs.synmetrix.org/docs/development/local-development

Login: demo@synmetrix.org
Password: demodemo


1. 代码定位问题有点困难。需要补充日志。
2. 很难用，特别是网络慢的时候。需要添加取消按钮。
3. Apple Silicon 上使用 x86 架构的 v1.2.3 版本的 cubestore 有问题： CPU 占用 100%，无法接收任何请求（3030 端口）。—— 使用 cubestore 的 arm 版本即可。
4. model 文件仍然是 YAML 文件的形式保存？但是权限管理（创建 Role）上倒是可以做到字段粒度
5. hasura/graphql-engine 开启了 console， http://localhost:8080/console 可以直接访问 PG 数据库和 Actions 设置

