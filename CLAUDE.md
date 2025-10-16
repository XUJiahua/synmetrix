# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Synmetrix (previously MLCraft) is an open source data engineering platform and semantic layer for centralized metrics management. It leverages [Cube.js](https://github.com/cube-js/cube) as the core analytics engine to implement flexible data models that consolidate metrics from multiple data sources into a unified semantic layer.

## Core Architecture

### Multi-Service Architecture

Synmetrix is built as a microservices architecture with the following core components:

1. **cubejs** (`services/cubejs/`) - The core analytics engine
   - Built on Cube.js v1.3.39
   - Handles data modeling, query generation, and SQL API
   - Exposes REST API on port 4000 and SQL APIs (MySQL: 13306, PostgreSQL: 15432)
   - Uses `repositoryFactory` to dynamically load data schema files from the database
   - Uses `driverFactory` to create database drivers based on datasource configuration
   - Supports multi-tenancy via security contexts with per-datasource orchestrator isolation

2. **actions** (`services/actions/`) - Business logic service
   - Express.js RPC server on port 3000
   - Handles operations like schema generation, data exploration, alerts, reports
   - RPC endpoints at `/rpc/:method` (e.g., `/rpc/gen-schemas`)
   - Communicates with Cube.js API and Hasura GraphQL

3. **hasura** (`services/hasura/`) - GraphQL API layer
   - Hasura v2.40.2 on port 8080
   - Provides GraphQL API for frontend and auth
   - Stores metadata in `services/hasura/metadata/`
   - Migrations in `services/hasura/migrations/`

4. **client** (`services/client/`) - Frontend UI
   - Web interface on port 80 (dev: 9055)
   - React-based application (separate repo: mlcraft-io/client-v2)

5. **hasura_plus** - Extended Hasura functionality
   - Runs on port 8081
   - Handles storage and authentication extensions

### Supporting Services

- **postgres** - Primary database (port 5435)
- **redis** - Caching layer (port 6379)
- **cubestore** - Cube.js distributed cache/query engine (port 3030)
- **minio** - S3-compatible object storage (ports 9000, 9001)
- **mailhog** - Email testing (SMTP: 1025, UI: 8025)

### Key Architectural Patterns

**Dynamic Schema Loading**: Cube.js schemas are stored in the database (not files). The `repositoryFactory` fetches schemas dynamically based on security context:
- `dataSchemaFiles()` retrieves schemas from database by datasource/branch
- Schemas are mapped to Cube.js format via `mapSchemaToFile`

**Multi-Tenancy**: Each datasource gets isolated Cube.js instances:
- `contextToOrchestratorId` creates unique orchestrator per datasource version
- `contextToAppId` isolates schema compilation contexts
- Pre-aggregation schemas are isolated: `pre_aggregations_{dataSourceId}`

**Security Context Flow**:
- JWT tokens contain Hasura claims (`x-hasura-user-id`, etc.)
- `checkAuth` validates tokens and builds `securityContext`
- Security context includes: user, datasource, branch, schema files
- `defineUserScope` builds the complete user scope object

## Development Commands

### Local Development with Docker Compose

```bash
# Start all services in development mode
./cli.sh compose up -e dev

# Start with build
./cli.sh compose up -e dev --build

# View logs
./cli.sh compose logs [service-name]

# Stop services
./cli.sh compose stop [service-name]

# Restart services
./cli.sh compose restart [service-name]

# Destroy stack
./cli.sh compose destroy
```

### CLI Tool (smcli)

The CLI is built with oclif (TypeScript):

```bash
# Build CLI
cd cli
yarn build

# Run tests
yarn test

# Lint
yarn lint
```

### Hasura Management

```bash
# Run Hasura CLI commands
./cli.sh hasura cli "migrate status"
./cli.sh hasura cli "console"

# Apply migrations
./cli.sh hasura cli "migrate apply"

# Export metadata
./cli.sh hasura cli "metadata export"
```

### Testing

```bash
# Run integration tests
./cli.sh tests stepci

# CLI tests
cd cli && yarn tests
```

## Environment Configuration

Primary environment files:
- `.env` - Base configuration (versioning, ports, secrets)
- `.dev.env` - Development overrides
- `.stage.env` - Staging configuration
- `.test.env` - Test configuration

Key environment variables:
- `HASURA_GRAPHQL_ADMIN_SECRET` - Hasura admin access (default: set in .dev.env)
- `CUBEJS_SECRET` - Cube.js API secret
- `JWT_KEY` - JWT signing key
- `POSTGRES_VERSION` - Currently 12
- `CUBESTORE_VERSION` - Currently v1.3.39
- `HASURA_VERSION` - Currently v2.40.2

## Common Development Workflows

### Adding New RPC Methods to Actions Service

1. Create file in `services/actions/src/rpc/{methodName}.js`
2. Export default async function: `(session, input, headers) => { ... }`
3. The RPC router auto-loads based on filename (hyphens → camelCase)
4. Access via POST to `/rpc/{method-name}`

### Modifying Cube.js Schema Generation

Schema generation happens in:
- `services/actions/src/rpc/genSchemas.js` - Triggers schema generation
- `services/cubejs/src/routes/generateDataSchema.js` - Core generation logic
- Uses Cube.js API's schema generation features

### Working with Data Sources

Data source configuration is stored in PostgreSQL via Hasura. Key queries:
- `services/cubejs/src/utils/dataSourceHelpers.js` - Helper functions
- `prepareDbParams.js` - Converts datasource config to driver params
- `driverFactory.js` - Creates appropriate Cube.js driver

### Running Individual Services for Development

Services use nodemon for hot reload:

```bash
# Cubejs with debugging
docker-compose up cubejs
# Debugger on port 9231

# Actions service
docker-compose up actions
```

## Code Organization Patterns

### Services Structure
```
services/{service}/
  ├── index.js          # Entry point
  ├── package.json      # Dependencies
  └── src/
      ├── routes/       # (cubejs) HTTP routes
      ├── rpc/          # (actions) RPC methods
      └── utils/        # Shared utilities
```

### Common Utilities

Both cubejs and actions services share similar utilities:
- `graphql.js` - Hasura GraphQL client
- `redis.js` - Redis connection
- `logger.js` / `logging.js` - Logging utilities
- `fromPairs.js` - Object utilities
- `md5Hex.js` - Hashing utilities

## Important Technical Notes

### Cube.js Version
Currently on v1.3.39 (all @cubejs-backend/* packages must match)

### Database Driver Support
Cube.js service includes drivers for:
- PostgreSQL, MySQL, ClickHouse, BigQuery
- Athena, Redshift, Snowflake, Databricks
- Druid, DuckDB, Elasticsearch, MongoDB
- And many more (see `services/cubejs/package.json`)

### Pre-Aggregations
- Stored in Cubestore (distributed cache)
- Schema isolation via `pre_aggregations_{dataSourceId}`
- Routes: `services/cubejs/src/routes/preAggregations.js`

### SQL API
Cube.js exposes SQL API on two ports:
- MySQL protocol: 13306 (`CUBEJS_SQL_PORT`)
- PostgreSQL protocol: 15432 (`CUBEJS_PG_SQL_PORT`)
- Auth handled by `checkSqlAuth.js`

### Authentication Flow
1. Frontend → Hasura (JWT auth)
2. Hasura → Actions/Cubejs (JWT forwarded)
3. Actions/Cubejs validate JWT and extract user context
4. Security context built with datasource permissions

## Docker Compose Environments

Different compose files for different environments:
- `docker-compose.dev.yml` - Local development (all services with hot reload)
- `docker-compose.stage.yml` - Staging environment
- `docker-compose.test.yml` - Testing environment
- `docker-compose.stack.yml` - Production-like stack

## Migration and Initialization

```bash
# Initialize database
./init.sh

# Run migrations
./migrate.sh
```

Hasura seeds contain demo data:
- Demo user: `demo@synmetrix.org` / `demodemo`
- Demo datasources with SQL API credentials

## Deployment

AWS infrastructure code available in `infra/aws/` directory.

Docker images published to Docker Hub under `synmetrix/*` organization.
