up:
	docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env up -d
	# docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env up postgres keycloak hasura
ps:
	docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env ps
down:
	docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env down
# 环境变量生效得靠 up,而不是 restart
restart:
	docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env up cubejs
# cubejs package.json 更新没法自动生效，需要重建镜像
rebuild:
	docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env build --no-cache cubejs
init-my:
	ENV=my ./init.sh