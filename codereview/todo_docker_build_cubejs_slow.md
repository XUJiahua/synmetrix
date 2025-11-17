还有就是 cubejs 编译的问题。。。太多坑了。。。
太多时间花在了下载依赖包了？？？现在困难点在于 cubejs 编译，挺浪费时间的。。。
不知道突然又好了，看起来需要好好研究下。。

➜  cubejs git:(codereview) yarn
yarn install v1.22.19
[1/4] Resolving packages...
warning @cubejs-backend/snowflake-driver > snowflake-sdk > google-auth-library > gaxios > node-fetch > fetch-blob > node-domexception@1.0.0: Use your platform's native DOMException instead
[2/4] Fetching packages...
warning lru.min@1.1.1: The engine "bun" appears to be invalid.
warning lru.min@1.1.1: The engine "deno" appears to be invalid.
[3/4] Linking dependencies...
warning "@cubejs-backend/snowflake-driver > snowflake-sdk@2.3.1" has unmet peer dependency "asn1.js@^5.4.1".
warning "@cubejs-backend/snowflake-driver > snowflake-sdk > asn1.js-rfc2560@5.0.1" has unmet peer dependency "asn1.js@^5.0.0".
warning Workspaces can only be enabled in private projects.
[4/4] Building fresh packages...
[6/8] ⡀ ws
[2/8] ⡀ @cubejs-backend/native
[3/8] ⡀ @cubejs-backend/cubestore
[4/8] ⡀ java
warning Error running install script for optional dependency: "/home/jiahua/workspace/20251014_ai_cubejs/synmetrix/services/cubejs/node_modules/java: Command failed.
Exit code: 1
Command: node-gyp rebuild
Arguments: 
Directory: /home/jiahua/workspace/20251014_ai_cubejs/synmetrix/services/cubejs/node_modules/java
Output:
gyp info it worked if it ends with ok
gyp info using node-gyp@10.2.0
gyp info using node@22.21.0 | linux | x64
gyp info find Python using Python version 3.10.12 found at \"/usr/bin/python3\"

gyp info spawn /usr/bin/python3
gyp info spawn args [
gyp info spawn args '/home/jiahua/workspace/20251014_ai_cubejs/synmetrix/services/cubejs/node_modules/java/node_modules/node-gyp/gyp/gyp_main.py',
gyp info spawn args 'binding.gyp',
gyp info spawn args '-f',
gyp info spawn args 'make',
gyp info spawn args '-I',
gyp info spawn args '/home/jiahua/workspace/20251014_ai_cubejs/synmetrix/services/cubejs/node_modules/java/build/config.gypi',
gyp info spawn args '-I',
gyp info spawn args '/home/jiahua/workspace/20251014_ai_cubejs/synmetrix/services/cubejs/node_modules/java/node_modules/node-gyp/addon.gypi',
gyp info spawn args '-I',
gyp info spawn args '/home/jiahua/.cache/node-gyp/22.21.0/include/node/common.gypi',
gyp info spawn args '-Dlibrary=shared_library',
gyp info spawn args '-Dvisibility=default',
gyp info spawn args '-Dnode_root_dir=/home/jiahua/.cache/node-gyp/22.21.0',
gyp info spawn args '-Dnode_gyp_dir=/home/jiahua/workspace/20251014_ai_cubejs/synmetrix/services/cubejs/node_modules/java/node_modules/node-gyp',
gyp info spawn args '-Dnode_lib_file=/home/jiahua/.cache/node-gyp/22.21.0/<(target_arch)/node.lib',
gyp info spawn args '-Dmodule_root_dir=/home/jiahua/workspace/20251014_ai_cubejs/synmetrix/services/cubejs/node_modules/java',
gyp info spawn args '-Dnode_engine=v8',
gyp info spawn args '--depth=.',
gyp info spawn args '--no-parallel',
gyp info spawn args '--generator-output',
gyp info spawn args 'build',
gyp info spawn args '-Goutput_dir=.'
gyp info spawn args ]
gyp: Call to 'node findJavaHome.js' returned exit status 1 while in binding.gyp. while trying to load binding.gyp
gyp ERR! configure error 
gyp ERR! stack Error: `gyp` failed with exit code: 1
gyp ERR! stack at ChildProcess.<anonymous> (/home/jiahua/workspace/20251014_ai_cubejs/synmetrix/services/cubejs/node_modules/java/node_modules/node-gyp/lib/configure.js:317:18)
gyp ERR! stack at ChildProcess.emit (node:events:519:28)
gyp ERR! stack at ChildProcess._handle.onexit (node:internal/child_process:293:12)
gyp ERR! System Linux 6.6.87.2-microsoft-standard-WSL2
gyp ERR! command \"/home/jiahua/.nvm/versions/node/v22.21.0/bin/node\" \"/home/jiahua/workspace/20251014_ai_cubejs/synmetrix/services/cubejs/node_modules/java/node_modules/.bin/node-gyp\" \"rebuild\"
gyp ERR! cwd /home/jiahua/workspace/20251014_ai_cubejs/synmetrix/services/cubejs/node_modules/java
success Saved lockfile.
Done in 1626.61s.