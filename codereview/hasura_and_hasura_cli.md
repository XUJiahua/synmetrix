hasura / hasura_cli notes
==========================

- compose mounts `services/hasura/{migrations,metadata,seeds,config.yaml}` into both `hasura` (engine) and `hasura_cli` (console sidecar) so they share the same artifacts on host.
- Engine does not auto-apply files just because they are mounted; it exposes HTTP API. Loading happens via Hasura CLI commands (`migrate apply`, `seeds apply`, `metadata apply/reload`) triggered by scripts or the console. 因为是通过 cli 的方式 apply 数据的。 Hasura CLI 的工作主要是方便 Hasura 开发。
- Sidecar purpose: run `hasura-cli console` with a socat proxy to point `localhost:8080` (as in `config.yaml`) to the engine service. It keeps the console available at host ports 9695/9693.
- Sidecar writes migration/metadata files back to the mounted host dirs when you make changes in the console; the engine still needs CLI apply actions to persist them in Hasura.
- `./cli.sh hasura cli ...` launches a one-off container (same image) on the Docker network and applies migrations/metadata via HTTP; it doesn’t rely on the long-lived sidecar.
- `init.sh` runs an early `migrate apply --database-name default --version 1628429118205` before the full `migrate.sh` to ensure the initial auth tables exist before bringing up the rest; `migrate.sh` is the “apply all + seeds + metadata” routine for ongoing use.


# Synmetrix CLI capabilities and manual equivalents

## What the CLI does
- Docker Compose lifecycle: up/stop/restart/destroy stacks or individual containers, push images, list containers, and tail logs (commands under `smcli compose`).
- Single container exec: run an arbitrary command inside a chosen container (`smcli docker ex`).
- Hasura wrapper: run Hasura CLI commands with preset endpoint, admin secret, and metadata directory options (`smcli hasura cli`).
- Docker Swarm lifecycle: deploy/stop/restart/destroy stack services, view service list and logs, optionally init swarm, build images, and target a registry (`smcli swarm`).
- Integration tests: run StepCI-based workflows with configurable environment and YAML file (`smcli tests stepci`).

## How to do the same without the CLI
- Compose lifecycle: use `docker compose -f docker-compose.<env>.yml up -d`, `... stop <service>`, `... restart <service>`, `... rm -sf <service>`, `... push <service>`, `... ps`, and `... logs -f --tail 499 <service>`.
- Container exec: `docker compose exec <service> /bin/bash` or `docker exec -it <container> <cmd>`.
- Hasura: install the official Hasura CLI; from `services/hasura`, run commands like `hasura metadata apply --endpoint http://hasura:8079 --admin-secret <secret>`.
- Swarm: `docker swarm init` (once), `docker network create <net>`, `docker stack deploy -c <stack-file> <stack>`, `docker service ls`, `docker service logs <svc> --tail 499`, `docker service update --force <svc>` for restart, and `docker service rm <svc>` to stop/remove.
- StepCI tests: install/ship the StepCI CLI or use its Docker image; run `stepci run tests/stepci/workflow.yml` or `docker compose run --rm stepci stepci run tests/stepci/workflow.yml` with the needed env and network settings.
