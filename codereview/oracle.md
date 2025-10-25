
https://gemini.google.com/app/2750de741e12171a

根据实践，目前 cube.js 不支持 Oracle 11g：
1. 要求 thick mode 连接，要求安装客户端库（已经解决）
2. 不支持一些现代的 SQL 语法（需要修改 cube.js，写兼容 Oracle 11g 的代码）

## Oracle 版本

| 版本 | 主要定位 | 架构 | 关键新特性 |
| :--- | :--- | :--- | :--- |
| **11g** | 传统稳定版 | 非多租户 | RAT, 结果缓存 |
| **12c** | 云与多租户 | **多租户架构 (CDB/PDB)** | In-Memory, 原生 JSON |
| **18c** | 创新版 | 多租户 | 年度命名, AI 雏形 |
| **19c** | **长期支持版 (LTS)** | 多租户 | **自动索引**, 实时统计信息 |
| **21c** | 创新版 | 多租户 | **区块链表**, SQL 宏, 原生 JSON 二进制 |
| **23c/ai**| **下一代 LTS** | 多租户 | **AI 向量搜索**, **JSON-关系双视图**, SQL 域 |


## NJS-138: connections to this database server version are not supported by node-oracledb in Thin mode


1. 目前 cube.js 并不支持低版本的 Oracle（比如 11），见 issue https://github.com/cube-js/cube/issues/9477 
2. Thin mode 要求 Oracle 版本 12.1 及之后版本 https://github.com/oracle/node-oracledb
3. 比如 11g 就无法支持 Thin mode，需要以 Thick mode 连接，也就是需要安装 Oracle Client libraries。
4. 安装 Oracle Client libraries 参考 services/cubejs/Dockerfile.oracle 。目前在 x64 上已经验证成功。arm64 上待验证（TODO）。
5. 目前通过安装 Oracle Client libraries 暂时解决了连接 Oracle 11g 的问题。


检查 Oracle 版本：
```
SELECT * FROM v$version;

Oracle Database 11g Express Edition Release 11.2.0.2.0 - 64bit Production
PL/SQL Release 11.2.0.2.0 - Production
"CORE	11.2.0.2.0	Production"
TNS for Linux: Version 11.2.0.2.0 - Production
NLSRTL Version 11.2.0.2.0 - Production
```

## ORA-00933: SQL command not properly ended

以 Oracle 11g 为例，以下报错：
```
SELECT
      count("b_i_customer_code"."BI_CUSTOMER_ID") "b_i_customer_code__count"
    FROM
      "HAIFENG"."BI_CUSTOMER_CODE"  "b_i_customer_code"  FETCH NEXT 1000 ROWS ONLY
```

去掉 FETCH NEXT 1000 ROWS ONLY 就 work 了。

1. FETCH NEXT 1000 ROWS ONLY 仅在 Oracle 12c、18c、19c、21c、23c 等所有 12.1 版本及之后的数据库中受支持。如果您在 11g 或更早的版本上运行它，将会收到一个语法错误。
2. 11g 限制行数： 使用 ROWNUM <= N
