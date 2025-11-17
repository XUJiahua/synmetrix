up:
	docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env up -d
	# docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env up postgres keycloak hasura
ps:
	docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env ps
down:
	docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env down
# rebuild:
# 	docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env build --no-cache hasura_cli
init-my:
	ENV=my ./init.sh