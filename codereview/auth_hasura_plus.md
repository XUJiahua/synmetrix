# Hasura Backend Plus (hasura_plus) - Complete Reference

## Overview

**Hasura Backend Plus (HBP)** is a specialized authentication and storage service that extends the core Hasura GraphQL Engine with user management, JWT authentication, and file storage capabilities. Synmetrix uses **version 2.7.1** from the official nhost/hasura-backend-plus Docker image.

**Official Repository**: https://github.com/nhost/hasura-backend-plus

---

## Table of Contents

1. [Architecture Role](#1-architecture-role)
2. [Docker Compose Configuration](#2-docker-compose-configuration)
3. [Environment Variables](#3-environment-variables)
4. [Dockerfile & Customizations](#4-dockerfile--customizations)
5. [API Endpoints](#5-api-endpoints)
6. [Authentication Features](#6-authentication-features)
7. [Storage & File Management](#7-storage--file-management)
8. [JWT Token Management](#8-jwt-token-management)
9. [Database Schema](#9-database-schema)
10. [Integration with Other Services](#10-integration-with-other-services)
11. [Nginx Routing Configuration](#11-nginx-routing-configuration)
12. [Testing & Verification](#12-testing--verification)
13. [Hasura vs Hasura Plus](#13-hasura-vs-hasura-plus)
14. [Deployment Models](#14-deployment-models)
15. [Security Features](#15-security-features)
16. [Is hasura_plus Required?](#16-is-hasura_plus-required)
17. [Troubleshooting](#17-troubleshooting)
18. [Key Files Reference](#18-key-files-reference)

---

## 1. Architecture Role

In the Synmetrix multi-service architecture, hasura_plus serves as the **authentication and extended functionality layer**:

```
┌──────────────────┐
│  Frontend Client │
│   (React App)    │
└────────┬─────────┘
         │ HTTP Requests
         ▼
    ┌─────────────────┐
    │  Nginx Proxy    │ (Port 80/8888)
    │  Reverse Proxy  │
    └────────┬────────┘
             │
      ┌──────┼──────────────────┐
      │      │                  │
      ▼      ▼                  ▼
   /auth   /v1,/v2           /api/v1
      │      │                  │
      ▼      ▼                  ▼
┌──────────┐ ┌──────────┐ ┌──────────┐
│ hasura_  │ │  Hasura  │ │  Cube.js │
│  plus    │ │ GraphQL  │ │ Analytics│
│  :3000   │ │  :8080   │ │  :4000   │
└────┬─────┘ └────┬─────┘ └────┬─────┘
     │            │             │
     └────────────┼─────────────┘
                  │
                  ▼
         ┌─────────────────┐
         │   PostgreSQL    │ (Port 5432)
         │  Shared Database│
         ├─────────────────┤
         │ auth.* tables   │ ← HBP Managed
         │ public.* tables │ ← Shared
         └─────────────────┘

Supporting Services:
├─ Minio (9000)    - S3-compatible file storage
├─ Redis (6379)    - Session cache
└─ Mailhog (1025)  - Email testing (dev)
```

**Key Responsibilities**:
- User registration and login
- JWT token generation and validation
- Password management and reset
- File upload/download via S3 integration
- Email notifications (verification, password reset)
- OAuth provider integration (partial support)

---

## 2. Docker Compose Configuration

### Development Environment

**File**: `docker-compose.dev.yml`

```yaml
hasura_plus:
  build:
    context: ./scripts/containers/hasura-backend-plus
  restart: always
  volumes:
    - ./scripts/containers/hasura-backend-plus/storage-rules/rules.yaml:/app/custom/storage-rules/rules.yaml
  ports:
    - 8081:3000
  env_file:
    - .env
    - .dev.env
  depends_on:
    - postgres
  networks:
    - synmetrix_default
```

**Key Configuration**:
- **Internal Port**: 3000
- **External Port**: 8081 (mapped for direct access in dev)
- **Custom Storage Rules**: Mounted from local filesystem
- **Hot Reload**: Enabled via volume mounts
- **Dependencies**: Requires PostgreSQL to be running

### Staging Environment

**File**: `docker-compose.stage.yml`

```yaml
hasura_plus:
  image: ${REGISTRY_HOST}/synmetrix/hasura-plus:latest
  build:
    context: ./scripts/containers/hasura-backend-plus
  env_file:
    - .env
    - .stage.env
  environment:
    SERVER_URL: ${PROTOCOL:-http}://app.${DOMAIN}
    REDIRECT_URL_SUCCESS: ${PROTOCOL:-http}://app.${DOMAIN}/callback
    REDIRECT_URL_ERROR: ${PROTOCOL:-http}://app.${DOMAIN}/callback
  networks:
    - synmetrix_default
```

**Differences from Dev**:
- Uses pre-built images from container registry
- No direct port exposure (internal only, accessed via Nginx)
- Domain-based environment variables for OAuth redirects
- Production-like configuration

### Test Environment

**File**: `docker-compose.test.yml`

```yaml
hasura_plus:
  image: synmetrix/hasura-plus:latest
  env_file:
    - .env
    - .test.env
  environment:
    SERVER_URL: ${PROTOCOL:-http}://app.${DOMAIN}
    REDIRECT_URL_SUCCESS: ${PROTOCOL:-http}://app.${DOMAIN}/callback
    REDIRECT_URL_ERROR: ${PROTOCOL:-http}://app.${DOMAIN}/callback
  networks:
    - synmetrix_default
```

### Stack Environment (PM2 Process)

**File**: `scripts/containers/stack/ecosystem.config.js`

```javascript
{
  name: "hasura_plus",
  script: `npx wait-on http-get://localhost:8080 && \
           cd ${SERVICES_DIR}/hasura-backend-plus && \
           yarn install --loglevel=error && \
           yarn build && \
           yarn start`,
  out_file: null,
  error_file: null,
  env: {
    PORT: HASURA_PLUS_PORT,                    // 8081
    AUTO_ACTIVATE_NEW_USERS: true,
    MAGIC_LINK_ENABLED: true,
    EMAILS_ENABLED: true,
    MIN_PASSWORD_LENGTH: 6,
    DATABASE_URL: process.env.HASURA_GRAPHQL_DATABASE_URL,
    SERVER_URL: HASURA_PLUS_SERVER_URL,
    JWT_EXPIRES_IN: 10800,                     // 3 hours
    JWT_KEY: process.env.JWT_KEY,
    JWT_ALGORITHM: "HS256",
    JWT_CLAIMS_NAMESPACE: "hasura",
    HASURA_ENDPOINT: "http://localhost:8080",
  },
}
```

**Stack Mode Features**:
- Runs as PM2 process in monolithic container
- Waits for Hasura to be ready before starting
- Builds from source on startup
- Shares process lifecycle with other services

---

## 3. Environment Variables

### Core Configuration

**Base Configuration** (`.env`):

```bash
# JWT Configuration (Shared with all services)
JWT_KEY=LGB6j3RkoVuOuqKzjgnCeq7vwfqBYJDw
JWT_ALGORITHM=HS256
JWT_EXPIRES_IN=10800                    # 3 hours in seconds
JWT_CLAIMS_NAMESPACE=hasura

# Service Endpoints
HASURA_PLUS_ENDPOINT=http://hasura_plus:3000
HASURA_ENDPOINT=http://hasura:8080
```

### Development Configuration (`.dev.env`)

```bash
# Environment
NODE_ENV=development

# Server URLs
SERVER_URL=http://localhost
REDIRECT_URL_SUCCESS=http://localhost/callback
REDIRECT_URL_ERROR=http://localhost/callback

# Database
DATABASE_URL=postgres://postgres:pg_pass@postgres:5432/default_db
HASURA_GRAPHQL_DATABASE_URL=postgres://postgres:pg_pass@postgres:5432/default_db

# Authentication Settings
AUTO_ACTIVATE_NEW_USERS=true
MAGIC_LINK_ENABLED=true
EMAILS_ENABLED=true
MIN_PASSWORD_LENGTH=6

# Email/SMTP (Mailhog for testing)
SMTP_HOST=mailhog
SMTP_PORT=1025
SMTP_SECURE=false
SMTP_USER=
SMTP_PASS=
SMTP_SENDER=noreply@synmetrix.org

# S3/Minio Storage
AWS_S3_ENDPOINT=http://minio:9000
AWS_S3_ACCESS_KEY_ID=minio
AWS_S3_SECRET_ACCESS_KEY=minio123
AWS_S3_REGION=us-east-1
AWS_S3_BUCKET_NAME=synmetrix-explorations
S3_SSL_ENABLED=false

# Hasura Integration
HASURA_GRAPHQL_ADMIN_SECRET=hasura-secret
```

### Staging Configuration (`.stage.env`)

```bash
NODE_ENV=production

# Dynamic URLs based on domain
SERVER_URL=${PROTOCOL:-http}://app.${DOMAIN}
REDIRECT_URL_SUCCESS=${PROTOCOL:-http}://app.${DOMAIN}/callback
REDIRECT_URL_ERROR=${PROTOCOL:-http}://app.${DOMAIN}/callback

# Database from secrets
DATABASE_URL=postgres://${SECRETS_PG_USER}:${SECRETS_PG_PASS}@${SECRETS_PG_HOST}/${SECRETS_PG_DB}
HASURA_GRAPHQL_DATABASE_URL=postgres://${SECRETS_PG_USER}:${SECRETS_PG_PASS}@${SECRETS_PG_HOST}/${SECRETS_PG_DB}

# Production SMTP
SMTP_HOST=${SMTP_HOST}
SMTP_PORT=${SMTP_PORT}
SMTP_SECURE=true
SMTP_USER=${SMTP_USER}
SMTP_PASS=${SMTP_PASS}

# Production S3 (AWS)
AWS_S3_ENDPOINT=https://s3.${AWS_REGION}.amazonaws.com
AWS_S3_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID}
AWS_S3_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY}
S3_SSL_ENABLED=true
```

### Environment Variable Reference

| Variable | Purpose | Default | Required |
|----------|---------|---------|----------|
| `DATABASE_URL` | PostgreSQL connection string | - | ✓ |
| `JWT_KEY` | Secret key for JWT signing | - | ✓ |
| `JWT_ALGORITHM` | JWT algorithm | HS256 | ✓ |
| `JWT_EXPIRES_IN` | Access token lifetime (seconds) | 10800 | ✓ |
| `JWT_CLAIMS_NAMESPACE` | Hasura claims namespace | hasura | ✓ |
| `SERVER_URL` | Base URL for service | - | ✓ |
| `REDIRECT_URL_SUCCESS` | OAuth success redirect | - | ✗ |
| `REDIRECT_URL_ERROR` | OAuth error redirect | - | ✗ |
| `AUTO_ACTIVATE_NEW_USERS` | Skip email verification | false | ✗ |
| `MAGIC_LINK_ENABLED` | Enable magic link login | false | ✗ |
| `EMAILS_ENABLED` | Enable email sending | false | ✗ |
| `MIN_PASSWORD_LENGTH` | Minimum password length | 8 | ✗ |
| `SMTP_HOST` | SMTP server host | - | ✗ |
| `SMTP_PORT` | SMTP server port | 587 | ✗ |
| `SMTP_SECURE` | Use TLS for SMTP | true | ✗ |
| `SMTP_USER` | SMTP username | - | ✗ |
| `SMTP_PASS` | SMTP password | - | ✗ |
| `SMTP_SENDER` | From email address | - | ✗ |
| `AWS_S3_ENDPOINT` | S3 endpoint URL | - | ✗ |
| `AWS_S3_ACCESS_KEY_ID` | S3 access key | - | ✗ |
| `AWS_S3_SECRET_ACCESS_KEY` | S3 secret key | - | ✗ |
| `AWS_S3_REGION` | S3 region | us-east-1 | ✗ |
| `AWS_S3_BUCKET_NAME` | S3 bucket name | - | ✗ |
| `HASURA_ENDPOINT` | Hasura GraphQL endpoint | - | ✓ |

---

## 4. Dockerfile & Customizations

### Dockerfile

**Location**: `scripts/containers/hasura-backend-plus/Dockerfile`

```dockerfile
FROM nhost/hasura-backend-plus:v2.7.1
COPY storage-rules/rules.yaml /app/custom/storage-rules/
```

**Customizations**:
1. Based on official nhost image (pinned to v2.7.1)
2. Includes custom storage access rules
3. Minimal modifications to maintain upstream compatibility

### Storage Rules Configuration

**Location**: `scripts/containers/hasura-backend-plus/storage-rules/rules.yaml`

```yaml
functions:
  isAuthenticated: 'return !!request.auth'

paths:
  /public*:
    read: 'true'                    # Public files are readable by anyone
    write: 'isAuthenticated()'      # Only authenticated users can write
```

**Rule Structure**:
- **functions**: JavaScript functions for permission checks
- **paths**: Path-based access control rules
  - `read`: Who can read files
  - `write`: Who can write/upload files
  - `delete`: Who can delete files (not shown, defaults to admin)

**Example Custom Rules**:

```yaml
functions:
  isAuthenticated: 'return !!request.auth'
  isOwner: 'return request.auth.user_id === resource.user_id'
  isAdmin: 'return request.auth.role === "admin"'

paths:
  /public/*:
    read: 'true'
    write: 'isAuthenticated()'

  /private/*:
    read: 'isAuthenticated() && isOwner()'
    write: 'isAuthenticated() && isOwner()'
    delete: 'isAuthenticated() && isOwner()'

  /admin/*:
    read: 'isAdmin()'
    write: 'isAdmin()'
    delete: 'isAdmin()'
```

---

## 5. API Endpoints

All hasura_plus endpoints are prefixed with `/auth` and routed through Nginx on port 80 (dev: 8081 direct access).

### 5.1 User Registration

```http
POST /auth/register
Content-Type: application/json

{
  "email": "user@example.com",
  "password": "securepassword123",
  "cookie": false
}
```

**Response** (200 OK):
```json
{
  "jwt_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "user": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "email": "user@example.com",
    "display_name": null,
    "created_at": "2024-01-15T10:30:00.000Z"
  },
  "refresh_token": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
}
```

**Fields**:
- `email` (required): User email address
- `password` (required): Password (min length from `MIN_PASSWORD_LENGTH`)
- `cookie` (optional): Store token in HTTP-only cookie (default: false)
- `user_data` (optional): Additional user profile data

### 5.2 User Login

```http
POST /auth/login
Content-Type: application/json

{
  "email": "user@example.com",
  "password": "securepassword123",
  "cookie": false
}
```

**Response** (200 OK):
```json
{
  "jwt_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "user": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "email": "user@example.com"
  },
  "refresh_token": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
}
```

**Error Responses**:
- `401 Unauthorized`: Invalid credentials
- `404 Not Found`: User not found
- `403 Forbidden`: Account not activated

### 5.3 Token Refresh

```http
GET /auth/token/refresh?refresh_token={refresh_token}
Authorization: Bearer {current_jwt_token}
```

**Response** (200 OK):
```json
{
  "jwt_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "new-refresh-token-value"
}
```

**Notes**:
- Current access token can be expired (but must be structurally valid)
- Refresh token must be valid and not expired
- Returns new access token and optionally rotates refresh token

### 5.4 Change Password

```http
POST /auth/change-password
Content-Type: application/json
Authorization: Bearer {jwt_token}

{
  "old_password": "oldpassword123",
  "new_password": "newsecurepassword456"
}
```

**Response** (204 No Content)

**Error Responses**:
- `401 Unauthorized`: Invalid current password
- `400 Bad Request`: New password doesn't meet requirements

### 5.5 Logout

```http
POST /auth/logout?refresh_token={refresh_token}
Authorization: Bearer {jwt_token}
```

**Response** (204 No Content)

**Effect**:
- Invalidates the provided refresh token
- Blacklists the current access token
- User must login again to get new tokens

### 5.6 Additional Endpoints (Available but not heavily used)

#### Password Reset Request
```http
POST /auth/reset-password/request
Content-Type: application/json

{
  "email": "user@example.com"
}
```

#### Password Reset Confirmation
```http
POST /auth/reset-password/confirm
Content-Type: application/json

{
  "ticket": "reset-token-from-email",
  "new_password": "newsecurepassword"
}
```

#### Email Verification
```http
GET /auth/activate?ticket={verification_token}
```

#### Magic Link Login
```http
POST /auth/magic-link
Content-Type: application/json

{
  "email": "user@example.com"
}
```

---

## 6. Authentication Features

### 6.1 User Registration & Login

**Registration Flow**:
1. User submits email and password
2. HBP validates password requirements
3. Creates entry in `auth.accounts` with bcrypt hash
4. Creates entry in `public.users` for profile
5. Sends verification email (if `EMAILS_ENABLED=true`)
6. Auto-activates user (if `AUTO_ACTIVATE_NEW_USERS=true`)
7. Returns JWT token and refresh token

**Login Flow**:
1. User submits credentials
2. HBP queries `auth.accounts` for email
3. Validates password hash with bcrypt
4. Checks if account is activated
5. Generates JWT with Hasura claims
6. Creates refresh token in `auth.refresh_tokens`
7. Returns tokens

### 6.2 JWT Token Structure

**Access Token Claims**:
```json
{
  "sub": "550e8400-e29b-41d4-a716-446655440000",
  "iat": 1642248600,
  "exp": 1642259400,
  "iss": "hasura-auth",
  "aud": "hasura",
  "hasura": {
    "x-hasura-user-id": "550e8400-e29b-41d4-a716-446655440000",
    "x-hasura-allowed-roles": ["user"],
    "x-hasura-default-role": "user"
  }
}
```

**Claim Descriptions**:
- `sub`: Subject (user ID)
- `iat`: Issued at (Unix timestamp)
- `exp`: Expiration (Unix timestamp, 3 hours from issue)
- `iss`: Issuer (hasura-auth)
- `aud`: Audience (hasura)
- `hasura`: Custom namespace with Hasura claims
  - `x-hasura-user-id`: User identifier for permissions
  - `x-hasura-allowed-roles`: Roles user can assume
  - `x-hasura-default-role`: Default role if not specified

### 6.3 Password Security

**Hashing Algorithm**: bcrypt with salt rounds (default: 10)

**Password Requirements**:
- Minimum length: Configurable via `MIN_PASSWORD_LENGTH` (default: 6)
- No complexity requirements by default
- Can be extended with custom validation

**Best Practices**:
```bash
# Recommended for production
MIN_PASSWORD_LENGTH=12
```

### 6.4 User Activation

**Manual Activation** (`AUTO_ACTIVATE_NEW_USERS=false`):
1. User registers
2. Email sent with activation link
3. User clicks link: `GET /auth/activate?ticket={token}`
4. Account activated in database
5. User can now login

**Auto Activation** (`AUTO_ACTIVATE_NEW_USERS=true`):
- Users can login immediately after registration
- No email verification required
- Useful for development and internal tools

### 6.5 Magic Link Authentication

**Enabled with**: `MAGIC_LINK_ENABLED=true`

**Flow**:
1. User requests magic link with email
2. Email sent with temporary login token
3. User clicks link with token
4. Automatically logged in (JWT issued)
5. Token invalidated after use

**Use Cases**:
- Passwordless authentication
- Quick login for returning users
- Reduced support burden (no password resets)

---

## 7. Storage & File Management

### 7.1 S3 Integration

hasura_plus integrates with S3-compatible storage (AWS S3 or Minio):

**Configuration**:
```bash
AWS_S3_ENDPOINT=http://minio:9000          # or https://s3.amazonaws.com
AWS_S3_ACCESS_KEY_ID=minio
AWS_S3_SECRET_ACCESS_KEY=minio123
AWS_S3_REGION=us-east-1
AWS_S3_BUCKET_NAME=synmetrix-explorations
S3_SSL_ENABLED=false                       # true for AWS S3
```

**Architecture**:
```
Client → hasura_plus → S3/Minio
             ↓
       Storage Rules
       (rules.yaml)
             ↓
      Access Control
```

### 7.2 File Upload

```http
POST /storage/o/{file_path}
Content-Type: multipart/form-data
Authorization: Bearer {jwt_token}

{file_data}
```

**Example with curl**:
```bash
curl -X POST http://localhost:3000/storage/o/public/avatar.jpg \
  -H "Authorization: Bearer ${JWT_TOKEN}" \
  -F "file=@/path/to/avatar.jpg"
```

**Response**:
```json
{
  "key": "public/avatar.jpg",
  "AcceptRanges": "bytes",
  "LastModified": "2024-01-15T10:30:00.000Z",
  "ContentLength": 102400,
  "ETag": "\"d41d8cd98f00b204e9800998ecf8427e\"",
  "ContentType": "image/jpeg"
}
```

### 7.3 File Download

```http
GET /storage/o/{file_path}
Authorization: Bearer {jwt_token}  # Optional, depends on storage rules
```

**Example**:
```bash
curl http://localhost:3000/storage/o/public/avatar.jpg \
  -H "Authorization: Bearer ${JWT_TOKEN}" \
  -o avatar.jpg
```

### 7.4 File Delete

```http
DELETE /storage/o/{file_path}
Authorization: Bearer {jwt_token}
```

### 7.5 Storage Rules Examples

**Public read, authenticated write**:
```yaml
paths:
  /public/*:
    read: 'true'
    write: 'isAuthenticated()'
```

**Owner-only access**:
```yaml
functions:
  isAuthenticated: 'return !!request.auth'
  isOwner: 'return request.auth && request.auth.user_id === resource.metadata.user_id'

paths:
  /private/*:
    read: 'isAuthenticated() && isOwner()'
    write: 'isAuthenticated() && isOwner()'
    delete: 'isAuthenticated() && isOwner()'
```

**Admin-only access**:
```yaml
functions:
  isAdmin: 'return request.auth && request.auth.role === "admin"'

paths:
  /admin/*:
    read: 'isAdmin()'
    write: 'isAdmin()'
    delete: 'isAdmin()'
```

---

## 8. JWT Token Management

### 8.1 Token Lifecycle

```
┌────────────┐
│ Registration│
│  or Login  │
└──────┬─────┘
       │
       ▼
┌────────────────┐
│ Generate Tokens│
│ - Access (3h)  │
│ - Refresh (7d) │
└──────┬─────────┘
       │
       ▼
┌────────────────┐
│  Use Access    │
│    Token       │
│ (API requests) │
└──────┬─────────┘
       │
    ┌──┴──┐
    │     │
    ▼     ▼
  Valid  Expired
    │     │
    │     ▼
    │  ┌──────────┐
    │  │  Refresh │
    │  │  Token   │
    │  └─────┬────┘
    │        │
    │        ▼
    │  ┌──────────┐
    │  │   New    │
    │  │  Access  │
    │  │  Token   │
    │  └─────┬────┘
    │        │
    └────────┘
         │
         ▼
    ┌──────────┐
    │  Logout  │
    │ (Revoke) │
    └──────────┘
```

### 8.2 Token Storage

**Access Token** (Short-lived, 3 hours):
- Stored in frontend (localStorage or sessionStorage)
- Sent with every API request in `Authorization` header
- Cannot be revoked (expires naturally)

**Refresh Token** (Long-lived, 7 days default):
- Stored in frontend (localStorage) or HTTP-only cookie
- Stored in `auth.refresh_tokens` table
- Can be revoked via logout or admin action
- One-time use (rotates on refresh)

### 8.3 Token Validation in Services

**Hasura GraphQL**:
```bash
HASURA_GRAPHQL_JWT_SECRET={
  "type": "HS256",
  "key": "LGB6j3RkoVuOuqKzjgnCeq7vwfqBYJDw",
  "claims_namespace": "hasura"
}
```

**Cube.js** (`services/cubejs/src/utils/checkAuth.js`):
```javascript
const jwtDecoded = jwt.verify(authToken, JWT_KEY, {
  algorithms: [JWT_ALGORITHM],
});
const userId = jwtDecoded?.hasura?.["x-hasura-user-id"];
```

**Actions Service** (`services/actions/src/utils/jwt.js`):
- Generates new tokens (not validation)
- Used for custom auth flows

---

## 9. Database Schema

### 9.1 Auth Schema Tables

hasura_plus creates and manages these tables in the `auth` schema:

#### auth.accounts
```sql
CREATE TABLE auth.accounts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email VARCHAR(255) UNIQUE NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  active BOOLEAN DEFAULT FALSE,
  default_role VARCHAR(50) DEFAULT 'user',
  created_at TIMESTAMP DEFAULT NOW(),
  updated_at TIMESTAMP DEFAULT NOW()
);
```

**Purpose**: Stores user credentials and basic account info

#### auth.refresh_tokens
```sql
CREATE TABLE auth.refresh_tokens (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  refresh_token UUID UNIQUE NOT NULL,
  account_id UUID NOT NULL REFERENCES auth.accounts(id),
  expires_at TIMESTAMP NOT NULL,
  created_at TIMESTAMP DEFAULT NOW()
);
```

**Purpose**: Stores valid refresh tokens for token renewal

#### auth.account_providers
```sql
CREATE TABLE auth.account_providers (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id UUID NOT NULL REFERENCES auth.accounts(id),
  provider VARCHAR(50) NOT NULL,
  provider_user_id VARCHAR(255) NOT NULL,
  created_at TIMESTAMP DEFAULT NOW(),
  UNIQUE (provider, provider_user_id)
);
```

**Purpose**: Links accounts to OAuth providers (Google, GitHub, etc.)

#### auth.account_roles
```sql
CREATE TABLE auth.account_roles (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id UUID NOT NULL REFERENCES auth.accounts(id),
  role VARCHAR(50) NOT NULL,
  created_at TIMESTAMP DEFAULT NOW(),
  UNIQUE (account_id, role)
);
```

**Purpose**: Assigns multiple roles to users

#### auth.roles
```sql
CREATE TABLE auth.roles (
  role VARCHAR(50) PRIMARY KEY,
  description TEXT
);
```

**Purpose**: Defines available roles in the system

### 9.2 Public Schema Tables (Shared with Hasura)

#### public.users
```sql
CREATE TABLE public.users (
  id UUID PRIMARY KEY REFERENCES auth.accounts(id),
  display_name VARCHAR(255),
  avatar_url TEXT,
  created_at TIMESTAMP DEFAULT NOW(),
  updated_at TIMESTAMP DEFAULT NOW()
);
```

**Purpose**: Extended user profile information (managed by Hasura)

#### public.teams
```sql
CREATE TABLE public.teams (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name VARCHAR(255) NOT NULL,
  created_at TIMESTAMP DEFAULT NOW()
);
```

#### public.members
```sql
CREATE TABLE public.members (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES public.users(id),
  team_id UUID NOT NULL REFERENCES public.teams(id),
  created_at TIMESTAMP DEFAULT NOW(),
  UNIQUE (user_id, team_id)
);
```

#### public.member_roles
```sql
CREATE TABLE public.member_roles (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  member_id UUID NOT NULL REFERENCES public.members(id),
  team_role VARCHAR(50) NOT NULL,  -- admin, editor, viewer
  access_list_id UUID REFERENCES public.access_lists(id),
  created_at TIMESTAMP DEFAULT NOW()
);
```

---

## 10. Integration with Other Services

### 10.1 PostgreSQL Integration

**Shared Database Architecture**:
```
┌─────────────────────────────────┐
│      PostgreSQL (5432)          │
├─────────────────────────────────┤
│  auth.*                         │ ← hasura_plus manages
│    - accounts                   │
│    - refresh_tokens             │
│    - account_providers          │
│    - account_roles              │
│    - roles                      │
├─────────────────────────────────┤
│  public.*                       │ ← Shared (Hasura + HBP)
│    - users                      │
│    - teams                      │
│    - members                    │
│    - datasources                │
│    - branches                   │
│    - dataschemas                │
└─────────────────────────────────┘
```

**Connection String** (Shared):
```bash
DATABASE_URL=postgres://user:password@postgres:5432/db
HASURA_GRAPHQL_DATABASE_URL=postgres://user:password@postgres:5432/db
```

### 10.2 Hasura GraphQL Integration

**JWT Configuration Synchronization**:

hasura_plus generates tokens:
```javascript
// services/actions/src/utils/jwt.js
const jwt = await new SignJWT({
  hasura: {
    "x-hasura-user-id": userId,
    "x-hasura-allowed-roles": ["user"],
    "x-hasura-default-role": "user"
  }
})
.sign(secret);
```

Hasura validates tokens:
```bash
HASURA_GRAPHQL_JWT_SECRET={
  "type": "HS256",
  "key": "LGB6j3RkoVuOuqKzjgnCeq7vwfqBYJDw",
  "claims_namespace": "hasura"
}
```

**Requirements**:
- Same `JWT_KEY` across all services
- Same `JWT_ALGORITHM` (HS256)
- Same `JWT_CLAIMS_NAMESPACE` (hasura)
- Matching token expiration settings

### 10.3 Cube.js Integration

Cube.js validates tokens from hasura_plus:

**File**: `services/cubejs/src/utils/checkAuth.js`

```javascript
// Extract JWT from Authorization header
const authToken = req.headers.authorization.split(" ")[1];

// Verify with same key
const jwtDecoded = jwt.verify(authToken, JWT_KEY, {
  algorithms: [JWT_ALGORITHM],
});

// Extract user ID
const userId = jwtDecoded?.hasura?.["x-hasura-user-id"];

// Use for authorization
const user = await findUser({ userId });
```

### 10.4 Minio/S3 Integration

**File Storage Flow**:
```
Client Upload Request
      ↓
hasura_plus (:3000)
      ↓
Storage Rules Check (rules.yaml)
      ↓
JWT Validation (if required)
      ↓
Minio/S3 API
      ↓
File Stored in Bucket
```

**Configuration Sync**:
```bash
# hasura_plus
AWS_S3_ENDPOINT=http://minio:9000
AWS_S3_BUCKET_NAME=synmetrix-explorations

# Minio service
MINIO_ROOT_USER=minio
MINIO_ROOT_PASSWORD=minio123
MINIO_DEFAULT_BUCKETS=synmetrix-explorations
```

### 10.5 Email Service Integration

**Mailhog (Development)**:
```bash
SMTP_HOST=mailhog
SMTP_PORT=1025
SMTP_SECURE=false
EMAILS_ENABLED=true
```

**External SMTP (Production)**:
```bash
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_SECURE=true
SMTP_USER=noreply@synmetrix.org
SMTP_PASS=app-specific-password
SMTP_SENDER=noreply@synmetrix.org
EMAILS_ENABLED=true
```

**Email Templates** (Built-in):
- Account activation
- Password reset
- Magic link login
- Welcome email

---

## 11. Nginx Routing Configuration

### 11.1 Routing Rules

**File**: `services/client/nginx/default.conf.template`

```nginx
server {
  listen 8888;
  server_name _;

  # Route authentication requests to hasura_plus
  location ~ ^/auth {
    proxy_pass http://hasura_plus:3000;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection 'upgrade';
    proxy_set_header Host $host;
    proxy_cache_bypass $http_upgrade;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
  }

  # Route GraphQL requests to Hasura
  location ~ ^/v1 {
    proxy_pass http://hasura:8080;
    # ... proxy settings
  }

  # Route GraphQL v2 to Hasura
  location ~ ^/v2 {
    proxy_pass http://hasura:8080;
    # ... proxy settings
  }

  # Route REST API to Cube.js
  location ~ ^/api/v1 {
    proxy_pass http://cubejs:4000;
    # ... proxy settings
  }

  # Serve frontend static files
  location / {
    root /usr/share/nginx/html;
    try_files $uri $uri/ /index.html;
  }
}
```

### 11.2 Request Routing Examples

| Request URL | Proxied To | Service | Purpose |
|-------------|------------|---------|---------|
| `http://localhost/auth/login` | `http://hasura_plus:3000/auth/login` | hasura_plus | User login |
| `http://localhost/auth/register` | `http://hasura_plus:3000/auth/register` | hasura_plus | User registration |
| `http://localhost/v1/graphql` | `http://hasura:8080/v1/graphql` | Hasura | GraphQL API |
| `http://localhost/api/v1/load` | `http://cubejs:4000/api/v1/load` | Cube.js | Analytics query |
| `http://localhost/storage/o/file` | `http://hasura_plus:3000/storage/o/file` | hasura_plus | File download |

### 11.3 Development vs Production Routing

**Development**:
- Direct access to hasura_plus on port 8081
- Can bypass Nginx for debugging: `http://localhost:8081/auth/login`

**Production/Staging**:
- All requests go through Nginx on port 80
- hasura_plus not exposed externally
- Only `/auth` paths routed to hasura_plus

---

## 12. Testing & Verification

### 12.1 Integration Tests

**File**: `tests/stepci/owner_flow.yml`

```yaml
version: "1.1"
name: Owner Flow Test
env:
  HASURA_PLUS_ENDPOINT: http://hasura_plus:3000

tests:
  owner_flow:
    steps:
      # 1. User Registration
      - name: sign_up
        http:
          url: ${HASURA_PLUS_ENDPOINT}/auth/register
          method: POST
          headers:
            Content-Type: application/json
          json:
            email: "test@test.com"
            password: "pass321"
            cookie: false
          check:
            status: 200
            schema:
              type: object
              required: [jwt_token, user, refresh_token]
          captures:
            accessToken:
              jsonpath: $.jwt_token
            refreshToken:
              jsonpath: $.refresh_token

      # 2. User Login
      - name: login
        http:
          url: ${HASURA_PLUS_ENDPOINT}/auth/login
          method: POST
          json:
            email: "test@test.com"
            password: "pass321"
            cookie: false
          check:
            status: 200
          captures:
            accessToken:
              jsonpath: $.jwt_token
            userId:
              jsonpath: $.user.id
            refreshToken:
              jsonpath: $.refresh_token

      # 3. Change Password
      - name: change_password
        http:
          url: ${HASURA_PLUS_ENDPOINT}/auth/change-password
          method: POST
          headers:
            Authorization: Bearer ${accessToken}
          json:
            old_password: "pass321"
            new_password: "pass123"
          check:
            status: 204

      # 4. Login with New Password
      - name: login_new_password
        http:
          url: ${HASURA_PLUS_ENDPOINT}/auth/login
          method: POST
          json:
            email: "test@test.com"
            password: "pass123"
            cookie: false
          check:
            status: 200
          captures:
            accessToken:
              jsonpath: $.jwt_token
            refreshToken:
              jsonpath: $.refresh_token

      # 5. Refresh Token
      - name: refresh_token
        http:
          url: ${HASURA_PLUS_ENDPOINT}/auth/token/refresh?refresh_token=${refreshToken}
          method: GET
          headers:
            Authorization: Bearer ${accessToken}
          check:
            status: 200
            schema:
              type: object
              required: [jwt_token]

      # 6. Logout
      - name: logout
        http:
          url: ${HASURA_PLUS_ENDPOINT}/auth/logout?refresh_token=${refreshToken}
          method: POST
          headers:
            Authorization: Bearer ${accessToken}
          check:
            status: 204
```

### 12.2 Manual Testing with curl

**Register New User**:
```bash
curl -X POST http://localhost:8081/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "email": "newuser@example.com",
    "password": "securepass123",
    "cookie": false
  }'
```

**Login**:
```bash
curl -X POST http://localhost:8081/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "demo@synmetrix.org",
    "password": "demodemo",
    "cookie": false
  }' | jq -r '.jwt_token' > token.txt
```

**Use Token with Hasura**:
```bash
JWT_TOKEN=$(cat token.txt)

curl -X POST http://localhost:8080/v1/graphql \
  -H "Authorization: Bearer ${JWT_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "query { users { id email } }"
  }'
```

**Use Token with Cube.js**:
```bash
curl -X POST http://localhost:4000/api/v1/load \
  -H "Authorization: Bearer ${JWT_TOKEN}" \
  -H "x-hasura-datasource-id: {datasource-uuid}" \
  -H "Content-Type: application/json" \
  -d '{
    "query": {
      "measures": ["Orders.count"]
    }
  }'
```

**Refresh Token**:
```bash
REFRESH_TOKEN="your-refresh-token"

curl -X GET "http://localhost:8081/auth/token/refresh?refresh_token=${REFRESH_TOKEN}" \
  -H "Authorization: Bearer ${JWT_TOKEN}"
```

**Change Password**:
```bash
curl -X POST http://localhost:8081/auth/change-password \
  -H "Authorization: Bearer ${JWT_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "old_password": "oldpass",
    "new_password": "newpass123"
  }'
```

**Logout**:
```bash
curl -X POST "http://localhost:8081/auth/logout?refresh_token=${REFRESH_TOKEN}" \
  -H "Authorization: Bearer ${JWT_TOKEN}"
```

### 12.3 Demo User Credentials

Pre-seeded demo account for testing:

```
Email: demo@synmetrix.org
Password: demodemo
```

**Usage**:
```bash
# Quick login for testing
curl -X POST http://localhost:8081/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"demo@synmetrix.org","password":"demodemo","cookie":false}'
```

---

## 13. Hasura vs Hasura Plus

### Comparison Table

| Feature | Hasura GraphQL Engine | Hasura Backend Plus |
|---------|----------------------|---------------------|
| **GraphQL API** | ✓ Primary function | ✗ No GraphQL |
| **REST API** | ✓ Optional | ✓ REST only |
| **User Registration** | ✗ | ✓ Built-in |
| **User Login** | ✗ | ✓ Built-in |
| **JWT Generation** | ✗ Only validates | ✓ Generates tokens |
| **JWT Validation** | ✓ | ✗ Delegates to Hasura |
| **Password Management** | ✗ | ✓ Change, reset |
| **Email Notifications** | ✗ | ✓ SMTP integration |
| **File Storage** | ✗ | ✓ S3-compatible |
| **OAuth Integration** | ✗ | ✓ Google, GitHub, etc. |
| **Magic Links** | ✗ | ✓ Passwordless auth |
| **Database Permissions** | ✓ Row-level security | ✗ |
| **GraphQL Subscriptions** | ✓ Real-time | ✗ |
| **Actions & Events** | ✓ Custom logic | ✗ |
| **Metadata Management** | ✓ | ✗ |
| **Schema Management** | ✓ Migrations | ✗ |

### Relationship Diagram

```
┌────────────────────────────────────────────┐
│         Synmetrix Architecture             │
├────────────────────────────────────────────┤
│                                            │
│  ┌──────────────┐      ┌───────────────┐  │
│  │ Hasura Plus  │      │    Hasura     │  │
│  │   (HBP)      │      │  GraphQL API  │  │
│  ├──────────────┤      ├───────────────┤  │
│  │ • Register   │      │ • GraphQL     │  │
│  │ • Login      │      │ • Permissions │  │
│  │ • Generate   │─────▶│ • Validate    │  │
│  │   JWT        │ JWT  │   JWT         │  │
│  │ • File       │      │ • Metadata    │  │
│  │   Storage    │      │ • Actions     │  │
│  └──────┬───────┘      └───────┬───────┘  │
│         │                      │           │
│         └──────────┬───────────┘           │
│                    │                       │
│              ┌─────▼─────┐                 │
│              │PostgreSQL │                 │
│              │  Database │                 │
│              ├───────────┤                 │
│              │ auth.*    │ ← HBP           │
│              │ public.*  │ ← Shared        │
│              └───────────┘                 │
└────────────────────────────────────────────┘
```

### Why Both Services?

**Separation of Concerns**:
1. **Hasura**: Focuses on GraphQL API, permissions, and data access
2. **Hasura Plus**: Focuses on authentication, user management, and file storage

**Benefits**:
- Clean separation of auth logic from API logic
- Easier to upgrade/replace either service independently
- Better security isolation (auth separate from data)
- Simplified configuration for each service

**Alternatives** (Not used in Synmetrix):
- Auth0, Firebase Auth (external auth services)
- Custom auth service (more maintenance)
- Hasura Actions for auth (more complex configuration)

---

## 14. Deployment Models

### 14.1 Development Deployment

**Configuration**: `docker-compose.dev.yml`

```bash
# Start all services including hasura_plus
./cli.sh compose up -e dev

# Access hasura_plus directly
curl http://localhost:8081/auth/login

# Or through Nginx
curl http://localhost:80/auth/login
```

**Characteristics**:
- Direct port mapping (8081)
- Hot reload enabled
- Local Minio for file storage
- Mailhog for email testing
- Debug logging enabled

### 14.2 Staging Deployment

**Configuration**: `docker-compose.stage.yml`

```bash
# Start staging stack
./cli.sh compose up -e stage

# Access only through Nginx (no direct port)
curl https://app.domain.com/auth/login
```

**Characteristics**:
- No direct port exposure
- Pre-built images from registry
- Environment-specific URLs
- Production-like configuration
- External SMTP for emails
- AWS S3 for file storage

### 14.3 Production Deployment (Stack)

**Configuration**: `scripts/containers/stack/ecosystem.config.js`

```bash
# Runs as part of monolithic stack container
docker run synmetrix/stack:latest
```

**Characteristics**:
- Single container with all services
- PM2 process management
- Shared service lifecycle
- Optimized resource usage
- Production logging
- Health checks enabled

**Process Management**:
```javascript
{
  name: "hasura_plus",
  script: "yarn start",
  instances: 1,
  autorestart: true,
  max_memory_restart: "500M",
  env: {
    NODE_ENV: "production",
    PORT: 8081
  }
}
```

---

## 15. Security Features

### 15.1 Password Security

**Bcrypt Hashing**:
- Algorithm: bcrypt with adaptive cost factor
- Salt rounds: 10 (default, configurable)
- Rainbow table protection: Automatic salting
- GPU attack resistance: Slow by design

**Password Requirements**:
```bash
MIN_PASSWORD_LENGTH=6              # Configurable minimum
# No default complexity requirements
# Can add custom validation hooks
```

**Best Practices for Production**:
```bash
MIN_PASSWORD_LENGTH=12
# Consider adding:
# - Uppercase requirement
# - Number requirement
# - Special character requirement
# - Password history (prevent reuse)
```

### 15.2 Token Security

**JWT Configuration**:
- **Algorithm**: HS256 (HMAC SHA-256)
- **Secret Key**: Shared across all services (`JWT_KEY`)
- **Expiration**: 3 hours (10800 seconds)
- **Claims Namespace**: `hasura` (for Hasura compatibility)

**Token Characteristics**:
- **Stateless**: No server-side session storage
- **Verifiable**: Signature validates integrity
- **Non-revocable**: Access tokens expire naturally
- **Refresh Required**: Long-running sessions need token refresh

**Security Considerations**:
```bash
# Production recommendations:
JWT_EXPIRES_IN=900         # 15 minutes (more secure)
# Use JWT_EXPIRES_IN=10800  # 3 hours (current, more convenient)

# Ensure strong secret key (32+ characters)
JWT_KEY=$(openssl rand -base64 32)
```

### 15.3 File Access Control

**Rule-Based Security** (`storage-rules/rules.yaml`):

```yaml
functions:
  # Check if user is authenticated
  isAuthenticated: 'return !!request.auth'

  # Check if user owns the resource
  isOwner: |
    return request.auth &&
           request.auth.user_id === resource.metadata.user_id

  # Check if user has admin role
  isAdmin: |
    return request.auth &&
           request.auth.hasura['x-hasura-default-role'] === 'admin'

paths:
  # Public files: anyone can read, auth users can write
  /public/*:
    read: 'true'
    write: 'isAuthenticated()'
    delete: 'isAdmin()'

  # Private files: owner-only access
  /private/*:
    read: 'isAuthenticated() && isOwner()'
    write: 'isAuthenticated() && isOwner()'
    delete: 'isAuthenticated() && isOwner()'

  # Admin files: admin-only access
  /admin/*:
    read: 'isAdmin()'
    write: 'isAdmin()'
    delete: 'isAdmin()'
```

**Rule Evaluation**:
1. Extract JWT from Authorization header
2. Validate token signature
3. Decode user claims
4. Execute JavaScript functions with context
5. Allow or deny based on rule result

### 15.4 Database Security

**Schema Isolation**:
```
auth.*    - Only hasura_plus has write access
public.*  - Shared, Hasura manages permissions
```

**Connection Security**:
```bash
# Use SSL in production
DATABASE_URL=postgres://user:pass@host:5432/db?sslmode=require

# Separate read-only user for Cube.js
CUBEJS_DB_USER=cubejs_readonly
```

**Row-Level Security** (via Hasura):
```sql
-- Example: Users can only see their own data
CREATE POLICY user_isolation ON public.datasources
  FOR SELECT
  USING (
    id IN (
      SELECT datasource_id FROM public.members
      WHERE user_id = current_setting('hasura.user')::uuid
    )
  );
```

### 15.5 HTTPS & Transport Security

**Development**:
```nginx
# HTTP only (localhost)
listen 8888;
```

**Production**:
```nginx
# HTTPS with TLS
listen 443 ssl http2;
ssl_certificate /etc/nginx/ssl/cert.pem;
ssl_certificate_key /etc/nginx/ssl/key.pem;
ssl_protocols TLSv1.2 TLSv1.3;
ssl_ciphers HIGH:!aNULL:!MD5;

# Redirect HTTP to HTTPS
server {
  listen 80;
  return 301 https://$host$request_uri;
}
```

### 15.6 Rate Limiting (Recommended)

Not currently implemented, but recommended for production:

```nginx
# Nginx rate limiting
limit_req_zone $binary_remote_addr zone=auth_limit:10m rate=5r/m;

location /auth/login {
  limit_req zone=auth_limit burst=10 nodelay;
  proxy_pass http://hasura_plus:3000;
}
```

**Or via hasura_plus configuration** (if supported):
```bash
RATE_LIMIT_LOGIN=5         # Max 5 login attempts per minute
RATE_LIMIT_REGISTER=3      # Max 3 registrations per minute
```

---

## 16. Is hasura_plus Required?

### Necessity Analysis

**Status**: ⚠️ **Optional, but strongly recommended**

### When hasura_plus is Required

✓ **User Registration**: Need built-in user signup flows
✓ **File Uploads**: Need S3-compatible file storage
✓ **Email Notifications**: Need password resets, verification emails
✓ **OAuth Integration**: Need social login (Google, GitHub, etc.)
✓ **Magic Links**: Need passwordless authentication
✓ **JWT Generation**: Need automatic token generation on login

### When hasura_plus Can Be Removed

✗ Using external auth service (Auth0, Firebase Auth, AWS Cognito)
✗ Custom authentication implementation
✗ API-only application (no user registration)
✗ File storage handled separately (direct S3, Cloudinary, etc.)

### Impact of Removing hasura_plus

**Loss of Features**:
- No built-in user registration/login UI
- No automatic JWT generation
- No file upload/storage management
- No email notifications for auth events
- No OAuth provider integration

**Required Alternatives**:
1. **External Auth Service**:
   ```bash
   # Example: Auth0
   AUTH0_DOMAIN=your-tenant.auth0.com
   AUTH0_CLIENT_ID=your-client-id
   ```

2. **Custom Auth Service**:
   - Implement own login/register endpoints
   - Generate JWTs compatible with Hasura
   - Manage password hashing and validation

3. **File Storage Alternative**:
   - Direct S3 integration in frontend
   - Cloudinary or similar service
   - Custom file upload service

### Minimal Configuration (No hasura_plus)

**Required Changes**:

1. **Remove from docker-compose**:
   ```yaml
   # Comment out or remove hasura_plus service
   # hasura_plus:
   #   ...
   ```

2. **Update Nginx routing**:
   ```nginx
   # Remove /auth routing
   # location ~ ^/auth {
   #   proxy_pass http://hasura_plus:3000;
   # }
   ```

3. **Configure alternative JWT source**:
   ```bash
   # Hasura still needs to validate JWTs
   HASURA_GRAPHQL_JWT_SECRET={
     "type": "RS256",                    # Use asymmetric if external
     "jwk_url": "https://auth.provider.com/.well-known/jwks.json"
   }
   ```

4. **Update frontend auth calls**:
   ```javascript
   // Before (hasura_plus)
   await fetch('/auth/login', { ... })

   // After (external auth)
   await fetch('https://auth.provider.com/login', { ... })
   ```

---

## 17. Troubleshooting

### 17.1 Common Issues

#### Issue: "Could not connect to database"

**Symptoms**:
```
Error: connect ECONNREFUSED 127.0.0.1:5432
```

**Causes**:
- PostgreSQL not running
- Incorrect `DATABASE_URL`
- Network connectivity issues

**Solutions**:
```bash
# Check if postgres is running
docker-compose ps postgres

# Verify connection string
echo $DATABASE_URL

# Test connection manually
docker exec -it postgres psql -U postgres -d default_db

# Restart postgres
docker-compose restart postgres
```

#### Issue: "JWT verification failed"

**Symptoms**:
```
Error: invalid signature
Error: jwt expired
```

**Causes**:
- Mismatched `JWT_KEY` between services
- Token expired (3 hours)
- Wrong `JWT_ALGORITHM`

**Solutions**:
```bash
# Verify JWT_KEY is same across all services
docker exec hasura_plus env | grep JWT_KEY
docker exec cubejs env | grep JWT_KEY

# Check token expiration
echo $JWT_TOKEN | cut -d. -f2 | base64 -d | jq .exp

# Refresh token
curl -X GET "http://localhost:8081/auth/token/refresh?refresh_token=$REFRESH_TOKEN"
```

#### Issue: "Auth table not found"

**Symptoms**:
```
Error: relation "auth.accounts" does not exist
```

**Causes**:
- Database migrations not run
- hasura_plus never started (creates tables on first run)

**Solutions**:
```bash
# Let hasura_plus create tables on startup
docker-compose up hasura_plus

# Or run migrations manually (if available)
docker exec hasura_plus yarn migrate
```

#### Issue: "Storage upload fails"

**Symptoms**:
```
Error: AccessDenied
Error: NoSuchBucket
```

**Causes**:
- Minio not running
- Bucket doesn't exist
- Incorrect S3 credentials

**Solutions**:
```bash
# Check Minio is running
docker-compose ps minio

# Verify bucket exists
docker exec minio mc ls minio/

# Create bucket if needed
docker exec minio mc mb minio/synmetrix-explorations

# Check S3 configuration
docker exec hasura_plus env | grep AWS_S3
```

#### Issue: "Emails not sending"

**Symptoms**:
- No verification emails
- No password reset emails

**Causes**:
- `EMAILS_ENABLED=false`
- SMTP configuration incorrect
- Mailhog not running (dev)

**Solutions**:
```bash
# Enable emails
EMAILS_ENABLED=true

# Check SMTP settings
docker exec hasura_plus env | grep SMTP

# Test Mailhog (dev)
curl http://localhost:8025/api/v2/messages

# Send test email
curl -X POST http://localhost:8081/auth/reset-password/request \
  -H "Content-Type: application/json" \
  -d '{"email":"demo@synmetrix.org"}'
```

### 17.2 Debugging Tips

**Enable Debug Logging**:
```bash
# Set in environment
DEBUG=*
LOG_LEVEL=debug

# Restart hasura_plus
docker-compose restart hasura_plus

# View logs
docker-compose logs -f hasura_plus
```

**Inspect Database**:
```bash
# Connect to database
docker exec -it postgres psql -U postgres -d default_db

# Check auth tables
\dt auth.*

# Query users
SELECT id, email, active FROM auth.accounts;

# Query refresh tokens
SELECT account_id, expires_at, created_at FROM auth.refresh_tokens;
```

**Test Endpoints Manually**:
```bash
# Health check (if available)
curl http://localhost:8081/healthz

# Version info
curl http://localhost:8081/version

# Register test user
curl -X POST http://localhost:8081/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"test@test.com","password":"test123","cookie":false}'
```

**Decode JWT Tokens**:
```bash
# Install jwt-cli
npm install -g jwt-cli

# Decode token
jwt decode $JWT_TOKEN

# Or use online tool: jwt.io
```

---

## 18. Key Files Reference

### Configuration Files

| File | Purpose | Lines |
|------|---------|-------|
| `docker-compose.dev.yml` | Development service config | hasura_plus section |
| `docker-compose.stage.yml` | Staging service config | hasura_plus section |
| `docker-compose.test.yml` | Test service config | hasura_plus section |
| `.env` | Base environment variables | JWT_*, DATABASE_URL |
| `.dev.env` | Development overrides | SMTP, S3, SERVER_URL |
| `.stage.env` | Staging overrides | Production URLs, secrets |
| `.test.env` | Test overrides | Test database, settings |

### Docker Files

| File | Purpose |
|------|---------|
| `scripts/containers/hasura-backend-plus/Dockerfile` | Container build definition |
| `scripts/containers/hasura-backend-plus/storage-rules/rules.yaml` | File access control rules |

### Integration Files

| File | Purpose |
|------|---------|
| `services/client/nginx/default.conf.template` | Nginx routing configuration |
| `scripts/containers/stack/ecosystem.config.js` | PM2 process configuration |
| `services/actions/src/utils/jwt.js` | JWT generation utility |
| `services/cubejs/src/utils/checkAuth.js` | JWT validation (Cube.js) |

### Test Files

| File | Purpose |
|------|---------|
| `tests/stepci/owner_flow.yml` | End-to-end auth flow test |

### Documentation Files

| File | Purpose |
|------|---------|
| `codereview/auth.md` | Overall authentication architecture |
| `codereview/auth_hasura_plus.md` | This document (hasura_plus deep dive) |
| `codereview/docker-compose.md` | Docker compose analysis |
| `CLAUDE.md` | Project overview and guidelines |

---

## Summary

**Hasura Backend Plus (v2.7.1)** is a critical authentication layer in Synmetrix that provides:

### Core Capabilities
✓ User registration and login with email/password
✓ JWT token generation and refresh (3-hour access tokens)
✓ Password management (change, reset via email)
✓ S3-compatible file storage with rule-based access control
✓ Email notifications via SMTP integration
✓ OAuth provider integration (Google, GitHub, etc.)
✓ Magic link passwordless authentication

### Architecture Role
- Generates JWTs consumed by Hasura and Cube.js
- Manages `auth.*` database schema
- Shares PostgreSQL database with Hasura
- Routed via Nginx at `/auth` path prefix

### Integration Points
- **Hasura**: Validates tokens, enforces permissions
- **Cube.js**: Validates tokens, builds security context
- **PostgreSQL**: Shared database for auth and user data
- **Minio/S3**: File storage backend
- **Nginx**: Request routing and proxying

### Production Readiness
⚠️ **Recommended Hardening**:
- Increase `MIN_PASSWORD_LENGTH` to 12+
- Reduce `JWT_EXPIRES_IN` to 900 (15 min)
- Enable HTTPS with TLS 1.2+
- Implement rate limiting on auth endpoints
- Use external SMTP (not Mailhog)
- Use AWS S3 (not local Minio)
- Enable audit logging
- Implement MFA/2FA

### Alternatives
If hasura_plus is removed, you must provide:
- External auth service (Auth0, Firebase Auth)
- Custom JWT generation compatible with Hasura
- Alternative file storage solution
- Email notification service

hasura_plus is **essential for the default Synmetrix deployment** and provides a complete, production-ready authentication system with minimal configuration required.
