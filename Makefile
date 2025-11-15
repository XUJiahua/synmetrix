up:
	docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env up postgres keycloak hasura
down:
	docker compose -f docker-compose.my.yml --env-file .env --env-file .my.env down
