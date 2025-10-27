# Synmetrix Authentication and Login System

## Overview

Synmetrix uses a multi-layered authentication system built with:
- **Hasura Backend Plus (v2.7.1)** - Handles user registration, login, password management, and JWT token generation
- **Hasura GraphQL Engine (v2.40.2)** - GraphQL API with JWT validation
- **Cube.js** - Analytics engine with per-datasource security context
- **Custom Auth Middleware** - Validates JWT tokens and builds user scope

---

## 1. Authentication Entry Points

### Primary Login Endpoint
**Location**: `hasura_plus` service (port 8081, externally routed to port 3000)
**Service**: nhost/hasura-backend-plus:v2.7.1

**Login Endpoints**:
- `POST /auth/login` - Login user
- `POST /auth/register` - Register new user
- `GET /auth/token/refresh?refresh_token={token}` - Refresh access token
- `POST /auth/change-password` - Change password
- `POST /auth/logout?refresh_token={token}` - Logout user

**Example Login Request** (from test flow):
```yaml
POST /auth/login
Content-Type: application/json
{
  "email": "user@example.com",
  "password": "pass123",
  "cookie": false
}
```

**Response**:
```json
{
  "jwt_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "user": {
    "id": "user-uuid",
    "email": "user@example.com"
  },
  "refresh_token": "refresh-token-value"
}
```

---

## 2. JWT Token Generation and Configuration

### Key Files:
- **File**: `services/actions/src/utils/jwt.js:12`
- **Environment Variables** (`.env`):
  ```
  JWT_EXPIRES_IN=10800          # 3 hours in minutes
  JWT_ALGORITHM=HS256
  JWT_CLAIMS_NAMESPACE=hasura
  JWT_KEY=LGB6j3RkoVuOuqKzjgnCeq7vwfqBYJDw
  HASURA_GRAPHQL_JWT_SECRET={"type": "HS256", "key": "...", "claims_namespace": "hasura"}
  ```

### JWT Generation Function:
**File**: `services/actions/src/utils/jwt.js:12`

```javascript
const generateUserAccessToken = async (userId) => {
  const hasuraCompatibleJwtPayload = {
    [JWT_CLAIMS_NAMESPACE]: {
      ["x-hasura-user-id"]: userId,
      ["x-hasura-allowed-roles"]: ["user"],
      ["x-hasura-default-role"]: "user",
    },
  };

  const secret = new TextEncoder().encode(JWT_KEY);
  const accessToken = await new SignJWT(hasuraCompatibleJwtPayload)
    .setProtectedHeader({ alg: JWT_ALGORITHM })
    .setIssuedAt()
    .setIssuer("services:actions")
    .setAudience("services:hasura")
    .setExpirationTime(`${JWT_EXPIRES_IN}m`)
    .setSubject(userId)
    .sign(secret);

  return accessToken;
};
```

---

## 3. JWT Token Validation and Security Context

### Token Validation - checkAuth Middleware
**File**: `services/cubejs/src/utils/checkAuth.js:14`

**Purpose**: Express middleware that validates incoming JWT tokens and sets up security context

**Process**:
1. Extract JWT from `Authorization` header (Bearer token format)
2. Verify token using `jwt.verify()` with `JWT_KEY` and algorithm
3. Extract `x-hasura-user-id` from token claims
4. Validate required headers: `x-hasura-datasource-id`, optional `x-hasura-branch-id`, `x-hasura-branch-version-id`
5. Query user from Hasura GraphQL using `findUser()`
6. Build complete user scope using `defineUserScope()`
7. Attach security context to request object

**Code Flow**:
```javascript
const checkAuth = async (req) => {
  // Extract authorization header
  const authHeader = req.headers.authorization;

  if (!authHeader) {
    throw new Error("Provide Hasura Authorization token");
  }

  // Extract data source and branch info
  const dataSourceId = req.headers["x-hasura-datasource-id"];
  const branchId = req.headers["x-hasura-branch-id"];
  const branchVersionId = req.headers["x-hasura-branch-version-id"];

  // Parse Bearer token
  let authToken = authHeader;
  if (authHeader.startsWith("Bearer ")) {
    authToken = authHeader.split(" ")[1];
  }

  // Verify JWT signature
  let jwtDecoded = jwt.verify(authToken, JWT_KEY, {
    algorithms: [JWT_ALGORITHM],
  });

  // Extract user ID from token claims
  const { "x-hasura-user-id": userId } = jwtDecoded?.hasura || {};

  // Find user in database
  const user = await findUser({ userId });

  if (!user.dataSources?.length || !user.members?.length) {
    throw new Error(`404: user "${userId}" not found`);
  }

  // Build user scope and security context
  const userScope = defineUserScope(
    user.dataSources,
    user.members,
    dataSourceId,
    branchId,
    branchVersionId
  );

  req.securityContext = {
    authToken,
    userId,
    userScope,
  };
};
```

---

## 4. User Scope Building

### defineUserScope Function
**File**: `services/cubejs/src/utils/defineUserScope.js:13`

**Purpose**: Builds complete user authorization scope for accessing datasources and data models

**Process**:
1. Find the selected datasource in user's datasources list
2. Validate user has access to the datasource
3. Select the appropriate branch (specified or default active branch)
4. Select the appropriate version (specified or latest)
5. Get user's role and access list from member roles
6. Build datasource context with security hashing
7. Return complete scope with role and access list

**Key Function**:
```javascript
const defineUserScope = (
  allDataSources,
  allMembers,
  selectedDataSourceId,
  selectedBranchId,
  selectedVersionId
) => {
  // Find requested datasource
  const dataSource = allDataSources.find(
    (source) => source.id === selectedDataSourceId
  );

  // Select branch (use specified or default active)
  let selectedBranch = selectedBranchId
    ? dataSource.branches.find(b => b.id === selectedBranchId)
    : dataSource.branches.find(b => b.status === "active");

  // Select version (use specified or latest)
  let selectedVersion = selectedVersionId
    ? selectedBranch.versions.find(v => v.id === selectedVersionId)
    : undefined;

  // Get user role and access list
  const dataSourceAccessList = getDataSourceAccessList(
    allMembers,
    selectedDataSourceId,
    dataSource.team_id
  );

  // Build security context for datasource
  const dataSourceContext = buildSecurityContext(
    dataSource,
    selectedBranch,
    selectedVersion
  );

  return {
    dataSource: dataSourceContext,
    ...dataSourceAccessList,
  };
};
```

---

## 5. Security Context Building

### buildSecurityContext Function
**File**: `services/cubejs/src/utils/buildSecurityContext.js:27`

**Purpose**: Creates hashed security identifiers for Cube.js isolation

**Returns**:
```javascript
{
  dataSourceId,           // Datasource UUID
  dbType,                // Database type (lowercase)
  dbParams,              // Connection parameters (prepared)
  dataSourceVersion,     // SHA256 hash of data + params
  preAggregationSchema,  // MD5 hash for pre-aggregation table names
  schemaVersion,         // MD5 hash of schema files
  files,                 // Array of schema file IDs
}
```

**Key Hashing**:
- `dataSourceVersion`: Used in orchestrator ID isolation
- `schemaVersion`: Used in app ID isolation
- `preAggregationSchema`: Ensures pre-aggregations are per-datasource

---

## 6. SQL Authentication (for SQL API)

### checkSqlAuth Function
**File**: `services/cubejs/src/utils/checkSqlAuth.js:8`

**Purpose**: Authenticates users connecting via Cube.js SQL API (MySQL or PostgreSQL protocols)

**Process**:
1. Receive username (email) from SQL client
2. Query database for SQL credentials matching username
3. Validate password against stored credentials
4. Build security context from datasource in credentials
5. Return password for validation and security context

**Function**:
```javascript
const checkSqlAuth = async (_, user) => {
  // Find SQL credentials by username
  const sqlCredentials = await findSqlCredentials(user);

  return {
    password: sqlCredentials?.password,
    securityContext: {
      userId: sqlCredentials?.user_id,
      userScope: buildSqlSecurityContext(sqlCredentials),
    },
  };
};
```

---

## 7. User Data Fetching

### findUser Helper
**File**: `services/cubejs/src/utils/dataSourceHelpers.js:10`

**GraphQL Query**:
```graphql
query ($_or: [users_bool_exp!]) {
  users(where: {_or: $_or}, limit: 1) {
    datasources {
      id, name, db_type, db_params, team_id
      branches {
        id, name, status
        versions (limit: 1, order_by: {created_at: desc}) {
          dataschemas { id, name, code }
        }
      }
    }
    members {
      id, team_id
      member_roles {
        id, team_role
        access_list { config }
      }
    }
  }
}
```

**Returns**:
```javascript
{
  dataSources: [...],  // Array of datasources user has access to
  members: [...]       // User's team memberships and roles
}
```

---

## 8. Cube.js Integration with Auth

### Cubejs Server Configuration
**File**: `services/cubejs/index.js:28`

**Auth Integration**:
```javascript
const options = {
  checkAuth,              // Middleware for REST API auth
  checkSqlAuth,          // Auth for SQL protocol
  apiSecret: CUBEJS_SECRET,
  contextToAppId,        // Isolates app based on datasource version
  contextToOrchestratorId,  // Isolates orchestrator per datasource
  preAggregationsSchema, // Per-datasource pre-agg schema
};

const cubejs = new ServerCore(options);
cubejs.initApp(app);

if (CUBEJS_SQL_API === "true") {
  const sqlServer = cubejs.initSQLServer();
  sqlServer.init(options);
}
```

**Routes Using checkAuthMiddleware**:
- `POST /api/v1/run-sql` - Run SQL queries
- `GET /api/v1/test` - Test connection
- `GET /api/v1/get-schema` - Get schema
- `POST /api/v1/generate-models` - Generate models
- `POST /api/v1/pre-aggregation-preview` - Preview pre-aggregations
- `GET /api/v1/pre-aggregations` - Get pre-aggregations

---

## 9. Hasura GraphQL Authorization

### Database Tables for Auth
**File**: `services/hasura/metadata/tables.yaml`

**Auth Schema Tables**:
- `auth.accounts` - User accounts with password hashes
- `auth.account_providers` - OAuth provider associations
- `auth.account_roles` - User role assignments
- `auth.roles` - Available roles
- `auth.refresh_tokens` - Refresh token storage

**Public Schema Tables**:
- `public.teams` - Team management
- `public.members` - Team members with roles
- `public.member_roles` - Role definitions with access lists
- `public.datasources` - Data source configurations
- `public.branches` - Schema branches per datasource
- `public.users` - User profiles

### GraphQL Actions (RPC Methods)
**File**: `services/hasura/metadata/actions.yaml`

**Key Actions**:
- `create_team` - Create team (user role required)
- `invite_team_member` - Invite member (user role required)
- `check_connection` - Test datasource (user role required)
- `gen_dataschemas` - Generate schemas (user role required)

---

## 10. Authentication Flow Diagram

```
┌─────────────┐
│ 1. Login    │
│ POST /auth/login
│ hasura_plus:3000
│ {email, password}
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 2. Token    │
│ Generation  │
│ - Verify password
│ - Generate JWT
│ - Return token
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 3. API      │
│ Request     │
│ GET /api/v1/load
│ cubejs:4000 │
│ Headers:    │
│ - Authorization: Bearer {jwt}
│ - x-hasura-datasource-id
│ - x-hasura-branch-id
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 4. Token    │
│ Validation  │
│ checkAuth() │
│ - Parse JWT │
│ - Verify signature
│ - Extract user ID
│ - Query user data
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 5. User     │
│ Scope       │
│ defineUserScope()
│ - Find datasource
│ - Select branch/version
│ - Get role & access list
│ - Build security context
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 6. Query    │
│ Execution   │
│ - Apply isolation
│ - Use user's driver
│ - Execute query
│ - Return results
└─────────────┘
```

---

## 11. Key Files Summary

| File | Purpose | Lines |
|------|---------|-------|
| `services/actions/src/utils/jwt.js` | JWT token generation for users | :12 |
| `services/cubejs/src/utils/checkAuth.js` | REST API auth middleware, JWT validation | :14 |
| `services/cubejs/src/utils/checkSqlAuth.js` | SQL API authentication | :8 |
| `services/cubejs/src/utils/buildSecurityContext.js` | Creates security hashes for isolation | :27 |
| `services/cubejs/src/utils/defineUserScope.js` | Builds user's datasource access scope | :13 |
| `services/cubejs/src/utils/dataSourceHelpers.js` | GraphQL queries for user/datasource lookup | :10 |
| `services/cubejs/src/utils/graphql.js` | Hasura GraphQL client | N/A |
| `services/cubejs/src/routes/index.js` | API routes with auth middleware | N/A |
| `services/cubejs/index.js` | Cube.js server config with auth integration | :28 |
| `services/actions/index.js` | Express RPC server (no built-in auth) | N/A |
| `services/hasura/metadata/tables.yaml` | Auth & user database schema | N/A |
| `services/hasura/metadata/actions.yaml` | GraphQL action definitions | N/A |

---

## 12. Security Features

### Multi-Tenant Isolation
Each datasource gets unique orchestrator & app ID via hashing:
- **Orchestrator ID**: `${dataSourceId}_${dataSourceVersion}` - Isolates query execution
- **App ID**: `${dataSourceId}_${schemaVersion}` - Isolates schema compilation
- **Pre-Aggregation Schema**: `pre_aggregations_{MD5(dataSourceId)}` - Isolates cached tables

### Per-Datasource Drivers
SQL driver instances isolated by datasource:
- Each datasource creates its own database connection
- Connection parameters stored encrypted in `db_params`
- Prepared by `prepareDbParams()` before driver creation

### Role-Based Access Control
Teams, members, and access lists:
- **Teams**: Organization-level grouping
- **Members**: Users belong to teams with roles
- **Member Roles**: Define permissions (admin, editor, viewer)
- **Access List**: Row-level security filters applied to queries

### JWT Security
- **Algorithm**: HS256 (HMAC SHA-256)
- **Expiration**: 3 hours (10800 minutes)
- **Claims Namespace**: `hasura` (contains user ID and roles)
- **Refresh Tokens**: Long-lived tokens for renewing access tokens
- **Secret Key**: Shared between services via `JWT_KEY` environment variable

### SQL API Authentication
Username/password authentication for SQL clients:
- SQL credentials stored in `sql_credentials` table
- Each credential tied to specific datasource and branch
- Password validated before granting SQL protocol access
- Security context built from credential's datasource

### Hasura GraphQL Permissions
Row-level security based on user context:
- Permission rules defined in `tables.yaml` metadata
- User ID extracted from JWT claims
- Filters applied automatically to all queries
- Actions require specific roles (e.g., "user" role)

---

## 13. Common Authentication Scenarios

### Scenario 1: Web Client Login
1. User enters email/password in frontend
2. Frontend sends `POST /auth/login` to hasura_plus
3. hasura_plus validates credentials against `auth.accounts`
4. Returns JWT token + refresh token
5. Frontend stores tokens in localStorage/sessionStorage
6. Subsequent API calls include `Authorization: Bearer {jwt}` header

### Scenario 2: REST API Query
1. Client sends `POST /api/v1/load` with query to Cube.js
2. Request includes:
   - `Authorization: Bearer {jwt}` header
   - `x-hasura-datasource-id` header (which datasource to query)
   - Optional: `x-hasura-branch-id`, `x-hasura-branch-version-id`
3. `checkAuth` middleware validates JWT and builds security context
4. Cube.js executes query with user's datasource driver
5. Results returned to client

### Scenario 3: SQL Client Connection
1. User connects via MySQL/PostgreSQL client
2. Connection string: `mysql://email:password@host:13306/database`
3. Cube.js SQL server calls `checkSqlAuth` with username (email)
4. Looks up credentials in `sql_credentials` table
5. Validates password and builds security context from credential's datasource
6. Client can execute SQL queries through Cube.js

### Scenario 4: Token Refresh
1. Access token expires after 3 hours
2. Frontend sends `GET /auth/token/refresh?refresh_token={token}`
3. hasura_plus validates refresh token
4. Issues new access token with updated expiration
5. Frontend updates stored token

### Scenario 5: GraphQL Query via Hasura
1. Client sends GraphQL query to Hasura endpoint
2. Request includes `Authorization: Bearer {jwt}` header
3. Hasura validates JWT using `HASURA_GRAPHQL_JWT_SECRET`
4. Extracts `x-hasura-user-id` from claims
5. Applies permission rules based on user ID and role
6. Executes query with row-level security filters
7. Returns filtered results

---

## 14. Environment Configuration

### Required Environment Variables

```bash
# JWT Configuration
JWT_KEY=LGB6j3RkoVuOuqKzjgnCeq7vwfqBYJDw
JWT_ALGORITHM=HS256
JWT_EXPIRES_IN=10800  # 3 hours in minutes
JWT_CLAIMS_NAMESPACE=hasura

# Hasura Configuration
HASURA_GRAPHQL_ADMIN_SECRET=hasura-secret
HASURA_GRAPHQL_JWT_SECRET={"type":"HS256","key":"LGB6j3RkoVuOuqKzjgnCeq7vwfqBYJDw","claims_namespace":"hasura"}

# Cube.js Configuration
CUBEJS_SECRET=cube-secret
CUBEJS_SQL_API=true
CUBEJS_SQL_PORT=13306
CUBEJS_PG_SQL_PORT=15432

# Database
DATABASE_URL=postgresql://user:password@postgres:5432/database
```

---

## 15. Testing Authentication

### Test User Credentials (from seeds)
```
Email: demo@synmetrix.org
Password: demodemo
```

### Manual Testing with curl

**Login**:
```bash
curl -X POST http://localhost:3000/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "demo@synmetrix.org",
    "password": "demodemo",
    "cookie": false
  }'
```

**Query Cube.js with Token**:
```bash
curl -X POST http://localhost:4000/api/v1/load \
  -H "Authorization: Bearer {jwt_token}" \
  -H "x-hasura-datasource-id: {datasource_uuid}" \
  -H "Content-Type: application/json" \
  -d '{
    "query": {
      "measures": ["Orders.count"],
      "timeDimensions": []
    }
  }'
```

**Connect via MySQL Client**:
```bash
mysql -h localhost -P 13306 -u user@example.com -p
# Enter SQL credential password
```

---

## 16. Common Issues and Debugging

### Issue: "Provide Hasura Authorization token"
**Cause**: Missing or malformed `Authorization` header
**Fix**: Ensure header is `Authorization: Bearer {jwt_token}`

### Issue: "404: user not found"
**Cause**: User has no datasources or team memberships
**Fix**:
1. Verify user exists in `auth.accounts`
2. Check user has entry in `public.users`
3. Verify user is member of a team in `public.members`
4. Ensure team has access to datasources

### Issue: "jwt malformed" or "invalid signature"
**Cause**: JWT_KEY mismatch between services
**Fix**: Ensure all services use same `JWT_KEY` environment variable

### Issue: SQL API authentication fails
**Cause**: No SQL credentials configured for user
**Fix**: Create entry in `sql_credentials` table with username/password

### Issue: "Permission denied" on GraphQL queries
**Cause**: Hasura permission rules blocking access
**Fix**: Check permission rules in `services/hasura/metadata/tables.yaml`

---

## 17. Future Improvements

### Recommended Enhancements
1. **OAuth2 Support**: Add social login (Google, GitHub)
2. **MFA/2FA**: Implement multi-factor authentication
3. **API Key Authentication**: Support API keys for service accounts
4. **Audit Logging**: Log all authentication events
5. **Rate Limiting**: Prevent brute force attacks on login
6. **Password Complexity**: Enforce stronger password requirements
7. **Session Management**: Track active sessions and allow revocation
8. **Token Rotation**: Implement automatic refresh token rotation
9. **IP Whitelisting**: Restrict access by IP address
10. **RBAC Granularity**: More fine-grained permission controls

---

## Summary

Synmetrix implements a **comprehensive, multi-layered authentication system** that provides:

- **Secure JWT-based authentication** with token expiration and refresh
- **Multi-tenant isolation** at the datasource level
- **Role-based access control** through teams and members
- **SQL API authentication** for database client connections
- **GraphQL permission system** via Hasura
- **Security context propagation** across all services

The architecture ensures that each user can only access their authorized datasources and data models, with complete isolation between different tenants and datasources.
