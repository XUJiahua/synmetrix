up:
	docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env up -d
	# docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env up postgres keycloak hasura
ps:
	docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env ps
down:
	docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env down
# 环境变量生效得靠 up,而不是 restart
restart:
	docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env up -d client
# cubejs package.json 更新没法自动生效，需要重建镜像
# build client 先使用 ./frontend_build.sh 构建出 dist 包
rebuild:
	docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env build --no-cache client
init-my:
	ENV=my ./init.sh
