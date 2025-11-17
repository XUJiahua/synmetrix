## Context
TODO: JsonWebTokenError: invalid algorithm
看起来生成 JWT 的逻辑还是兼容得不够！
background RPCs that need to act as a user。。。模拟用户的权限来操作啊。。。不能直接使用用户的 token 么？？？

  要点：

  - Hasura OSS 只能接受单一 HASURA_GRAPHQL_JWT_SECRET；当前 .my.env 配的是 Keycloak jwk_url。HASURA_GRAPHQL_JWT_SECRETS 支持多种 JWT 生成方式。。。
https://hasura.io/docs/2.0/auth/authentication/multiple-jwt-secrets/#resolution-logic 开源版本没有！
  - Actions 并未读取该变量，而是用 JWT_KEY/JWT_ALGORITHM 本地 HS256 签发用户 Token，并在多处后台调用中使用，这与 jwk_url 不兼容。
  - CubeJS 已改为 JWKS 验证，只接受 Keycloak RS256。
  - 结论：若坚持 “全走 Keycloak jwk_url”，需要改为从 Keycloak 获取用户 Token（token endpoint + impersonation/token exchange 或密码流），而非本地 HS 签发。文件中给出了环境
    变量需求、改造步骤与风险说明。

- Hasura OSS only accepts a single JWT config via `HASURA_GRAPHQL_JWT_SECRET`; `HASURA_GRAPHQL_JWT_SECRETS` (multi) is enterprise-only.
- In the current `docker-compose.my.yml` stack, Hasura is configured with `HASURA_GRAPHQL_JWT_SECRET={"jwk_url": "${JWK_URL}"}` (Keycloak JWKS, RS256).
- The Actions service issues its own user tokens via `services/actions/src/utils/jwt.js`, signing with `JWT_ALGORITHM` (default HS256) and `JWT_KEY`, not with Keycloak.
- CubeJS now verifies incoming tokens via JWKS (`services/cubejs/src/utils/checkAuth.js`) and therefore already expects RS256 Keycloak-issued tokens, not HS256.
- Result: HS256 tokens minted by Actions bypass Hasura/CubeJS verification when Hasura uses `jwk_url` only, so background calls fail with `JsonWebTokenError: invalid algorithm`.

## How `HASURA_GRAPHQL_JWT_SECRET` is used relative to Actions
- Hasura: Verifies every GraphQL request based on the single JWT secret (`jwk_url` in `.my.env`). Only RS256 tokens signed by Keycloak JWKS are accepted.
- Actions: Does **not** read `HASURA_GRAPHQL_JWT_SECRET`. Instead, it signs tokens locally (HS256) for:
  - Alert checks (`services/actions/src/rpc/checkAlert.js`).
  - Schema documentation generation (`services/actions/src/rpc/genSchemasDocs.js`).
  - Exploration screenshots (`services/actions/src/rpc/sendExplorationScreenshot.js`).
- Actions uses those tokens to call Hasura/CubeJS as a user. With `jwk_url` configured, these HS tokens are rejected.

## Design options to align with Keycloak JWKS (single-secret constraint)
1) **Use real Keycloak-issued access tokens (recommended)**
   - Obtain tokens from Keycloak (token endpoint) instead of self-signing.
   - Required env:
     - `KEYCLOAK_URL` (e.g., `http://keycloak:8080`).
     - `KEYCLOAK_REALM` (e.g., `hasura-app`).
     - A client that can get tokens: typically public flow (password) or confidential client credentials + impersonation.
     - For user tokens without passwords, enable token exchange or impersonation and provide `KEYCLOAK_CLIENT_ID`/`KEYCLOAK_CLIENT_SECRET`.
   - Implementation sketch:
     - Replace `generateUserAccessToken` with a helper fetching from Keycloak:
       - POST `${KEYCLOAK_URL}/realms/${KEYCLOAK_REALM}/protocol/openid-connect/token`
       - Use either:
         - Resource Owner Password (needs user password — often not viable), or
         - Token Exchange / Impersonation: obtain client credentials token, exchange for the target user (`subject_token`/`requested_subject`).
     - Returned RS256 token will satisfy Hasura/CubeJS JWKS verification.
   - Pros: Single source of truth, matches `jwk_url`, no extra secrets.
   - Cons: Requires Keycloak setup (token exchange/impersonation or passwords).

2) **Switch Hasura back to HS256 (not compatible with jwk_url-only requirement)**
   - Would set `HASURA_GRAPHQL_JWT_SECRET` to `{"type":"HS256","key":"...","claims_namespace":"hasura"}`.
   - Enables existing Actions HS tokens, but loses Keycloak JWKS verification. This conflicts with the “all via jwk_url” requirement.

Given “all via Keycloak jwk_url”, option 1 is the viable path.

## Proposed adaptation steps
1) Add Keycloak client env for Actions:
   - `KEYCLOAK_URL`, `KEYCLOAK_REALM`, `KEYCLOAK_CLIENT_ID`, `KEYCLOAK_CLIENT_SECRET`.
   - If using token exchange/impersonation, enable it for the client in Keycloak and grant permissions.
2) Replace `services/actions/src/utils/jwt.js` to fetch Keycloak-issued access tokens instead of HS signing.
3) Update call sites (`checkAlert`, `genSchemasDocs`, `sendExplorationScreenshot`) to use the new fetch-token helper; ensure they handle failures gracefully.
4) Ensure Hasura keeps `HASURA_GRAPHQL_JWT_SECRET` pointing to the JWKS URL; CubeJS already verifies via JWKS.
5) Redeploy/restart actions, hasura, cubejs after env changes.

## Risks / notes
- Token exchange/impersonation needs explicit Keycloak configuration; without it, backend flows cannot mint user tokens.
- If only user credentials are available (password grant), security review is required; prefer impersonation/exchange.
- Clock skew between containers can still break JWT validation; keep NTP/time sync.
