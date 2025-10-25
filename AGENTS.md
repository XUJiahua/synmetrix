# Repository Guidelines

## Project Structure & Module Organization
- `cli/`: TypeScript oclif CLI; sources live in `src/commands`, tests in `test`, artefacts under `dist/`.
- `services/cubejs/`: Cube semantic layer service (`index.js`, `src/utils/`) shipped as a Node container.
- `services/actions/`: Express-based automation service handling invites, exports, and S3 workflows.
- `services/hasura/`: Hasura metadata, migrations, and seeds; keep changes additive and versioned.
- `docker-compose.*.yml`: Environment-specific stacks; pair with the matching `.env` files in the repo root.
- `tests/stepci/`: StepCI integration flows; reusable SQL fixtures live in `tests/data/`.

## Build, Test, and Development Commands
- `./cli.sh compose up --env dev` / `... stop`: Boot or halt the dev stack defined in `docker-compose.dev.yml`.
- `yarn --cwd cli build`: Compile the CLI to `dist/`; runs automatically before packaging.
- `yarn --cwd cli lint` and `yarn --cwd cli test`: ESLint + Prettier checks and Mocha specs for CLI behaviour.
- `yarn --cwd services/cubejs start.dev`: Hot-reload the cube service with nodemon for local debugging.
- `./cli.sh tests stepci --env dev`: Build the StepCI runner image and execute `tests/stepci/workflow.yml` inside Docker.

## Coding Style & Naming Conventions
- Follow the shared `@umijs/fabric` ESLint rules and Prettier defaults (2-space indentation, double quotes in JSON).
- Use ES modules throughout (`type: module`); prefer `camelCase` for functions/files, `PascalCase` for classes, and `SCREAMING_SNAKE_CASE` for env variables.
- Keep cube schema and Hasura metadata in sync with service names; store secrets in `.env` variants, never in source.

## Testing Guidelines
- Unit-level coverage targets the CLI; add Mocha specs inside `cli/test` and run through `yarn --cwd cli test`.
- Integration coverage relies on StepCI; update `tests/stepci/*.yml` with clear step names and idempotent GraphQL mutations.
- After StepCI runs, verify the analytics DB (`psql default`) or extend the CLI assertion in `tests/stepci.ts` if schema moves.

## Commit & Pull Request Guidelines
- Follow the existing Git history pattern: `type: short imperative summary` (e.g., `feat: add owner invite flow`); group related file changes per commit.
- Reference issues in the body (`Refs #123`) and note required migrations or env updates.
- Pull requests should include: purpose, key changes, manual/automated test evidence (CLI, StepCI, Docker), and screenshots when touching user-facing assets.
- Run lint, unit tests, and (when relevant) StepCI before requesting review; attach command outputs or links in the PR discussion.
