SELECT 'CREATE DATABASE hasura'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'hasura')
\gexec

SELECT 'CREATE DATABASE hasura_metadata'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'hasura_metadata')
\gexec

SELECT 'CREATE DATABASE keycloak'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'keycloak')
\gexec

SELECT 'CREATE DATABASE agents'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'agents')
\gexec
