import jwt from "jsonwebtoken";
import JwksRsa from "jwks-rsa";

import { findUser } from "./dataSourceHelpers.js";
import defineUserScope from "./defineUserScope.js";

const { JWT_KEY, JWT_ALGORITHM, JWK_URL } = process.env;

// =====================================================================
// JWKS Client Initialization (with caching)
// =====================================================================

let jwksClient = null;

/**
 * Get or create JWKS client for RS256 token verification.
 * The JWKS client is created lazily and cached for performance.
 *
 * @returns {JwksRsa.JwksClient|null} JWKS client instance or null if JWK_URL not configured
 */
const getJwksClient = () => {
  if (!JWK_URL) {
    return null;
  }

  if (!jwksClient) {
    jwksClient = JwksRsa({
      jwksUri: JWK_URL,
      cache: true,
      cacheMaxEntries: 5,
      cacheMaxAge: 10 * 60 * 1000, // 10 minutes
      rateLimit: true,
      jwksRequestsPerMinute: 10,
    });
  }

  return jwksClient;
};

/**
 * Get the signing key for token verification.
 * Attempts to get the key from JWKS for RS256 tokens.
 *
 * @param {string} token - The JWT token
 * @returns {Promise<string|Buffer>} The signing key or secret
 */
const getSigningKey = async (token) => {
  try {
    // Decode without verification to get the kid (key ID)
    const decoded = jwt.decode(token, { complete: true });

    if (!decoded) {
      throw new Error("Invalid token format");
    }

    const { header } = decoded;
    const alg = header?.alg;
    const kid = header?.kid;

    // HS256 tokens: use JWT_KEY (symmetric)
    if (alg === "HS256") {
      return JWT_KEY;
    }

    // RS256 tokens: use JWKS (asymmetric)
    if (alg === "RS256") {
      const client = getJwksClient();

      if (!client) {
        throw new Error(
          "RS256 token received but JWK_URL is not configured. " +
            "Set JWK_URL environment variable."
        );
      }

      if (!kid) {
        throw new Error("RS256 token missing 'kid' (key ID) in header");
      }

      const signingKey = await client.getSigningKey(kid);
      return signingKey.getPublicKey();
    }

    // Unknown algorithm
    throw new Error(`Unsupported token algorithm: ${alg}`);
  } catch (err) {
    if (err.message.includes("Unable to find a signing key")) {
      throw new Error(
        `JWKS key not found for kid: ${jwt.decode(token, { complete: true })?.header?.kid}`
      );
    }
    throw err;
  }
};

/**
 * Checks the authorization of the request and sets the security context.
 *
 * Supports dual authentication:
 * - HS256: Local JWT_KEY verification (backward compatible)
 * - RS256: JWKS endpoint verification (Keycloak)
 *
 * @param {Object} req - The request object.
 * @throws {Error} If the Hasura Authorization token is not provided.
 * @throws {Error} If no x-hasura-datasource-id is provided in the headers.
 * @throws {Error} If token verification fails.
 * @throws {Error} If the user is not found.
 * @returns {Promise<void>} A promise that resolves when the security context is set.
 */
const checkAuth = async (req) => {
  // Extract the authorization header from the request
  const authHeader = req.headers.authorization;

  if (!authHeader) {
    throw new Error("Provide Hasura Authorization token");
  }

  // Extract the data source ID from the request headers
  const dataSourceId = req.headers["x-hasura-datasource-id"];
  // Extract the branch ID from the request headers
  const branchId = req.headers["x-hasura-branch-id"];
  // Extract the branch version ID from the request headers
  const branchVersionId = req.headers["x-hasura-branch-version-id"];

  let jwtDecoded;
  let authToken;

  if (authHeader.startsWith("Bearer ")) {
    authToken = authHeader.split(" ")[1];
  } else {
    authToken = authHeader;
  }

  if (!authToken) {
    throw new Error("Provide Hasura Authorization token");
  }

  try {
    // Detect the token algorithm first to set correct verification options
    const decodedHeader = jwt.decode(authToken, { complete: true });
    const tokenAlgorithm = decodedHeader?.header?.alg;

    // Get the appropriate signing key based on token algorithm
    const signingKey = await getSigningKey(authToken);

    // Verify the token with appropriate algorithm
    // For RS256 tokens, allow RS256; for HS256, allow HS256
    const algorithms = tokenAlgorithm === "RS256" ? ["RS256"] : [JWT_ALGORITHM || "HS256"];

    jwtDecoded = jwt.verify(authToken, signingKey, {
      algorithms: algorithms,
    });
  } catch (err) {
    // Log detailed error for debugging
    console.error("[checkAuth] Token verification failed:", {
      error: err.message,
      algorithm: jwt.decode(authToken, { complete: true })?.header?.alg,
      hasJwkUrl: !!JWK_URL,
    });
    throw err;
  }

  const { "x-hasura-user-id": userId } = jwtDecoded?.hasura || {};

  if (!dataSourceId) {
    throw new Error(
      "400: No x-hasura-datasource-id provided, headers: " +
        JSON.stringify(req.headers)
    );
  }

  const user = await findUser({
    userId,
  });

  if (!user.dataSources?.length || !user.members?.length) {
    throw new Error(`404: user "${userId}" not found`);
  }

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

const checkAuthMiddleware = async (req, _, next) => {
  try {
    await checkAuth(req);
    next();
  } catch (err) {
    next(err);
  }
};

export { checkAuth };
export default checkAuthMiddleware;
