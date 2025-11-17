
cli-migrations-v3 有 bug？没法自动部署？？？
hasura-1    | time="2025-11-17T14:51:40Z" level=fatal msg="error applying metadata \n{\n  \"error\": \"key \\"tables\\" not found\",\n  \"path\": \"$.args.metadata\",\n  \"code\": \"parse-failed\"\n}"

现在只能使用非 cli-migrations-v3，然后使用 hasura console deploy （cli 功能）
这样就 OK 了。。。

