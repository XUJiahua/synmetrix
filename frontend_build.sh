#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
AGENT_UI_DIR="$ROOT_DIR/../agent-ui"
CLIENT_DIR="$ROOT_DIR/../synmetrix-client-v2"
CLIENT_DIST_DIR="$CLIENT_DIR/dist"
CLIENT_DIST_TAR="$ROOT_DIR/services/client/dist.tar.gz"

# build agent-ui
cd "$AGENT_UI_DIR"
pnpm build

# build main client
cd "$CLIENT_DIR"
yarn build

# merge agent-ui bundle into client dist
rm -rf "$CLIENT_DIST_DIR/agents"
mv "$AGENT_UI_DIR/out" "$CLIENT_DIST_DIR/agents"

# package client dist for Docker build context
mkdir -p "$(dirname "$CLIENT_DIST_TAR")"
rm -f "$CLIENT_DIST_TAR"
tar -czf "$CLIENT_DIST_TAR" -C "$CLIENT_DIST_DIR/.." dist
