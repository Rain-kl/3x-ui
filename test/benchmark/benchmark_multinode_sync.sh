#!/usr/bin/env bash
# End-to-end Master + Sub-node sync simulation in OrbStack Linux VM.
# Verifies real RPC sync of per-inbound rate limits and traffic quotas across panels.
set -euo pipefail

# Dispatch to OrbStack VM if running on macOS host
if [[ "$(uname)" == "Darwin" ]]; then
    echo "==> Running on macOS. Dispatching multi-node sync simulation into OrbStack Ubuntu VM..."
    SCRIPT_PATH="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
    exec orb -m ubuntu sudo bash "${SCRIPT_PATH}" "$@"
fi

if [[ "$(id -u)" -ne 0 ]]; then
    echo "Error: this benchmark requires root privileges on Linux." >&2
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${REPO_DIR}"

TMP_DIR="/tmp/xui_multinode_sync_test"
rm -rf "$TMP_DIR"
mkdir -p "$TMP_DIR/master" "$TMP_DIR/subnode"

SUBNODE_PID=""
MASTER_PID=""

cleanup() {
    echo ""
    echo "======================================================="
    echo "==> Cleaning up multi-node test environment..."
    echo "======================================================="
    if [[ -n "$SUBNODE_PID" ]]; then
        kill -9 "$SUBNODE_PID" 2>/dev/null || true
    fi
    if [[ -n "$MASTER_PID" ]]; then
        kill -9 "$MASTER_PID" 2>/dev/null || true
    fi
    pkill -9 -f "${TMP_DIR}/x-ui" 2>/dev/null || true
    pkill -9 -f "xray" 2>/dev/null || true
    echo "==> Cleanup complete."
}
trap cleanup EXIT INT TERM

echo "======================================================="
echo "  3x-ui Master + Sub-node Real RPC Sync Simulation"
echo "  Environment: OrbStack Linux VM ($(uname -s) $(uname -r) $(uname -m))"
echo "======================================================="

# Step 0: Compile x-ui binary
echo ""
echo "[Step 0/9] Compiling x-ui binary in Linux VM..."
go build -o "${TMP_DIR}/x-ui" .
echo "==> Binary built successfully: ${TMP_DIR}/x-ui"

# Step 1: Initialize Sub-node (port 2054, disable subServer to avoid port 2096 collision)
echo ""
echo "[Step 1/9] Initializing Sub-node on port 2054..."
XUI_PORT=2054 XUI_DB_FOLDER="${TMP_DIR}/subnode" "${TMP_DIR}/x-ui" setting -port 2054 -username subnode -password subnode123 -webBasePath / >/dev/null 2>&1
sqlite3 "${TMP_DIR}/subnode/x-ui.db" "INSERT OR REPLACE INTO settings (key, value) VALUES ('subEnable', 'false');"
SUBCONF=$(sed -e 's/62789/62790/g' -e 's/11111/11112/g' "${REPO_DIR}/internal/web/service/config.json")
sqlite3 "${TMP_DIR}/subnode/x-ui.db" "INSERT OR REPLACE INTO settings (key, value) VALUES ('xrayTemplateConfig', '${SUBCONF}');"

SUBNODE_TOKEN_OUTPUT=$(XUI_PORT=2054 XUI_DB_FOLDER="${TMP_DIR}/subnode" "${TMP_DIR}/x-ui" setting -getApiToken)
SUBNODE_TOKEN=$(echo "$SUBNODE_TOKEN_OUTPUT" | awk -F': ' '/apiToken:/ {print $2}' | tr -d '\r\n')
echo "==> Sub-node API Token: ${SUBNODE_TOKEN}"

# Step 2: Initialize Master (port 2053)
echo ""
echo "[Step 2/9] Initializing Master on port 2053..."
XUI_PORT=2053 XUI_DB_FOLDER="${TMP_DIR}/master" "${TMP_DIR}/x-ui" setting -port 2053 -username master -password master123 -webBasePath / >/dev/null 2>&1
MASTER_TOKEN_OUTPUT=$(XUI_PORT=2053 XUI_DB_FOLDER="${TMP_DIR}/master" "${TMP_DIR}/x-ui" setting -getApiToken)
MASTER_TOKEN=$(echo "$MASTER_TOKEN_OUTPUT" | awk -F': ' '/apiToken:/ {print $2}' | tr -d '\r\n')
echo "==> Master API Token: ${MASTER_TOKEN}"

# Step 3: Launch Sub-node and Master web processes
echo ""
echo "[Step 3/9] Starting Sub-node and Master processes..."
XUI_PORT=2054 XUI_DB_FOLDER="${TMP_DIR}/subnode" XUI_DEBUG=true "${TMP_DIR}/x-ui" run > "${TMP_DIR}/subnode.log" 2>&1 &
SUBNODE_PID=$!
XUI_PORT=2053 XUI_DB_FOLDER="${TMP_DIR}/master" XUI_DEBUG=true "${TMP_DIR}/x-ui" run > "${TMP_DIR}/master.log" 2>&1 &
MASTER_PID=$!

echo "==> Waiting for panels to become ready..."
READY=false
for i in {1..30}; do
    if curl -s "http://127.0.0.1:2054/panel/api/inbounds/list" -H "Authorization: Bearer ${SUBNODE_TOKEN}" >/dev/null 2>&1 && \
       curl -s "http://127.0.0.1:2053/panel/api/inbounds/list" -H "Authorization: Bearer ${MASTER_TOKEN}" >/dev/null 2>&1 && \
       curl -s "http://127.0.0.1:2054/panel/api/server/status" -H "Authorization: Bearer ${SUBNODE_TOKEN}" | jq -e '.obj != null' >/dev/null 2>&1; then
        echo "==> Both panels online and status ready (Sub-node :2054, Master :2053)."
        READY=true
        break
    fi
    sleep 0.5
done
if [[ "$READY" != "true" ]]; then
    echo "ERROR: Panels failed to start within timeout!" >&2
    exit 1
fi

# Step 4: Master adds Sub-node
echo ""
echo "[Step 4/9] Adding Sub-node to Master fleet..."
NODE_RESP=$(curl -s -X POST "http://127.0.0.1:2053/panel/api/nodes/add" \
  -H "Authorization: Bearer ${MASTER_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "subnode-1",
    "scheme": "http",
    "address": "127.0.0.1",
    "port": 2054,
    "basePath": "/",
    "apiToken": "'"${SUBNODE_TOKEN}"'",
    "enable": true,
    "allowPrivateAddress": true
  }')
NODE_ID=$(echo "$NODE_RESP" | jq -r '.obj.id // empty')
if [[ -z "$NODE_ID" ]]; then
    echo "ERROR: Failed to add Sub-node to Master: ${NODE_RESP}" >&2
    exit 1
fi
echo "==> Sub-node added to Master with Node ID: ${NODE_ID}"

# Step 5: Master creates 2 Inbounds on Sub-node
echo ""
echo "[Step 5/9] Master creating Inbound 1 & Inbound 2 on Sub-node..."
IB1_RESP=$(curl -s -X POST "http://127.0.0.1:2053/panel/api/inbounds/add" \
  -H "Authorization: Bearer ${MASTER_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "nodeId": '"${NODE_ID}"',
    "protocol": "vless",
    "port": 25001,
    "tag": "sub-ib-1",
    "remark": "Subnode Inbound 1",
    "enable": true,
    "settings": "{\"clients\":[]}",
    "streamSettings": "{\"network\":\"tcp\",\"security\":\"none\"}"
  }')
MASTER_IB1_ID=$(echo "$IB1_RESP" | jq -r '.obj.id // empty')

IB2_RESP=$(curl -s -X POST "http://127.0.0.1:2053/panel/api/inbounds/add" \
  -H "Authorization: Bearer ${MASTER_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "nodeId": '"${NODE_ID}"',
    "protocol": "vless",
    "port": 25002,
    "tag": "sub-ib-2",
    "remark": "Subnode Inbound 2",
    "enable": true,
    "settings": "{\"clients\":[]}",
    "streamSettings": "{\"network\":\"tcp\",\"security\":\"none\"}"
  }')
MASTER_IB2_ID=$(echo "$IB2_RESP" | jq -r '.obj.id // empty')

echo "==> Master created Inbounds: IB1=${MASTER_IB1_ID}, IB2=${MASTER_IB2_ID}"

# Resolve Sub-node local inbound IDs (waiting for sync)
echo "==> Waiting for Master to push Inbounds to Sub-node..."
for i in {1..20}; do
    SUBNODE_IBS=$(curl -s "http://127.0.0.1:2054/panel/api/inbounds/list" -H "Authorization: Bearer ${SUBNODE_TOKEN}")
    SUBNODE_IB1_ID=$(echo "$SUBNODE_IBS" | jq -r '.obj[] | select(.tag=="sub-ib-1") | .id // empty')
    SUBNODE_IB2_ID=$(echo "$SUBNODE_IBS" | jq -r '.obj[] | select(.tag=="sub-ib-2") | .id // empty')
    if [[ -n "$SUBNODE_IB1_ID" && -n "$SUBNODE_IB2_ID" ]]; then
        break
    fi
    sleep 1
done
if [[ -z "$SUBNODE_IB1_ID" || -z "$SUBNODE_IB2_ID" ]]; then
    echo "ERROR: Sub-node did not receive inbounds from Master within timeout!" >&2
    exit 1
fi
echo "==> Sub-node local Inbound IDs: IB1=${SUBNODE_IB1_ID}, IB2=${SUBNODE_IB2_ID}"

# Step 6: Master adds Client A with differential rate limits and quotas
echo ""
echo "[Step 6/9] Master creating Client A: IB1=100M (quota 10G), IB2=200M (quota 20G), Global=0M..."
ADD_CLIENT_RESP=$(curl -s -X POST "http://127.0.0.1:2053/panel/api/clients/add" \
  -H "Authorization: Bearer ${MASTER_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "client": {
      "email": "clientA@test.com",
      "id": "11111111-2222-3333-4444-555555555555",
      "subId": "sub-clientA",
      "enable": true,
      "downLimit": 0,
      "downLimitByInbound": {
        "'"${MASTER_IB1_ID}"'": 100,
        "'"${MASTER_IB2_ID}"'": 200
      },
      "totalGB": 107374182400,
      "totalGBByInbound": {
        "'"${MASTER_IB1_ID}"'": 10737418240,
        "'"${MASTER_IB2_ID}"'": 21474836480
      }
    },
    "inboundIds": ['"${MASTER_IB1_ID}"', '"${MASTER_IB2_ID}"']
  }')

if [[ $(echo "$ADD_CLIENT_RESP" | jq -r '.success') != "true" ]]; then
    echo "ERROR: Failed to add client on Master: ${ADD_CLIENT_RESP}" >&2
    exit 1
fi
echo "==> Client A added on Master successfully."

# Step 7: Verify Sub-node DB & API consistency
echo ""
echo "[Step 7/9] Verifying Sub-node state consistency..."
REC_DOWN_LIMIT=$(sqlite3 "${TMP_DIR}/subnode/x-ui.db" "SELECT down_limit FROM clients WHERE email='clientA@test.com';")
CI1_DOWN=$(sqlite3 "${TMP_DIR}/subnode/x-ui.db" "SELECT down_limit FROM client_inbounds WHERE inbound_id=${SUBNODE_IB1_ID};")
CI2_DOWN=$(sqlite3 "${TMP_DIR}/subnode/x-ui.db" "SELECT down_limit FROM client_inbounds WHERE inbound_id=${SUBNODE_IB2_ID};")
CI1_QUOTA=$(sqlite3 "${TMP_DIR}/subnode/x-ui.db" "SELECT total_gb FROM client_inbounds WHERE inbound_id=${SUBNODE_IB1_ID};")
CI2_QUOTA=$(sqlite3 "${TMP_DIR}/subnode/x-ui.db" "SELECT total_gb FROM client_inbounds WHERE inbound_id=${SUBNODE_IB2_ID};")

echo "  Sub-node DB clients.down_limit: ${REC_DOWN_LIMIT} (expected: 0)"
echo "  Sub-node DB client_inbounds[IB1].down_limit: ${CI1_DOWN} (expected: 100)"
echo "  Sub-node DB client_inbounds[IB2].down_limit: ${CI2_DOWN} (expected: 200)"
echo "  Sub-node DB client_inbounds[IB1].total_gb: ${CI1_QUOTA} (expected: 10737418240)"
echo "  Sub-node DB client_inbounds[IB2].total_gb: ${CI2_QUOTA} (expected: 21474836480)"

if [[ "$REC_DOWN_LIMIT" != "0" ]]; then
    echo "FAIL: Sub-node global down_limit was contaminated with single inbound limit!" >&2
    exit 1
fi
if [[ "$CI1_DOWN" != "100" || "$CI2_DOWN" != "200" ]]; then
    echo "FAIL: Sub-node per-inbound down_limits do not match Master configuration!" >&2
    exit 1
fi
if [[ "$CI1_QUOTA" != "10737418240" || "$CI2_QUOTA" != "21474836480" ]]; then
    echo "FAIL: Sub-node per-inbound quotas do not match Master configuration!" >&2
    exit 1
fi

# Query Sub-node API endpoint
SUBNODE_CLIENT_API=$(curl -s "http://127.0.0.1:2054/panel/api/clients/get/clientA@test.com" -H "Authorization: Bearer ${SUBNODE_TOKEN}")
API_GLOBAL_DOWN=$(echo "$SUBNODE_CLIENT_API" | jq -r '.obj.client.downLimit // 0')
API_IB1_DOWN=$(echo "$SUBNODE_CLIENT_API" | jq -r ".obj.downLimitByInbound[\"${SUBNODE_IB1_ID}\"] // 0")
API_IB2_DOWN=$(echo "$SUBNODE_CLIENT_API" | jq -r ".obj.downLimitByInbound[\"${SUBNODE_IB2_ID}\"] // 0")

echo "  Sub-node API client.downLimit: ${API_GLOBAL_DOWN} (expected: 0)"
echo "  Sub-node API downLimitByInbound[IB1]: ${API_IB1_DOWN} (expected: 100)"
echo "  Sub-node API downLimitByInbound[IB2]: ${API_IB2_DOWN} (expected: 200)"

if [[ "$API_GLOBAL_DOWN" != "0" ]]; then
    echo "FAIL: Sub-node API reports non-zero global down_limit!" >&2
    exit 1
fi
if [[ "$API_IB1_DOWN" != "100" || "$API_IB2_DOWN" != "200" ]]; then
    echo "FAIL: Sub-node API per-inbound downLimits mismatch!" >&2
    exit 1
fi
echo "==> Initial sync verification PASSED."

# Step 8: Master updates rate limits: IB1 -> 80M, IB2 -> 150M
echo ""
echo "[Step 8/9] Master modifying rate limits: IB1=80M, IB2=150M..."
UPDATE_CLIENT_RESP=$(curl -s -X POST "http://127.0.0.1:2053/panel/api/clients/update/clientA@test.com" \
  -H "Authorization: Bearer ${MASTER_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "email": "clientA@test.com",
    "id": "11111111-2222-3333-4444-555555555555",
    "subId": "sub-clientA",
    "enable": true,
    "downLimit": 0,
    "downLimitByInbound": {
      "'"${MASTER_IB1_ID}"'": 80,
      "'"${MASTER_IB2_ID}"'": 150
    },
    "totalGB": 107374182400,
    "totalGBByInbound": {
      "'"${MASTER_IB1_ID}"'": 10737418240,
      "'"${MASTER_IB2_ID}"'": 21474836480
    }
  }')

if [[ $(echo "$UPDATE_CLIENT_RESP" | jq -r '.success') != "true" ]]; then
    echo "ERROR: Failed to update client on Master: ${UPDATE_CLIENT_RESP}" >&2
    exit 1
fi

CI1_DOWN_UPDATED=$(sqlite3 "${TMP_DIR}/subnode/x-ui.db" "SELECT down_limit FROM client_inbounds WHERE inbound_id=${SUBNODE_IB1_ID};")
CI2_DOWN_UPDATED=$(sqlite3 "${TMP_DIR}/subnode/x-ui.db" "SELECT down_limit FROM client_inbounds WHERE inbound_id=${SUBNODE_IB2_ID};")
REC_DOWN_UPDATED=$(sqlite3 "${TMP_DIR}/subnode/x-ui.db" "SELECT down_limit FROM clients WHERE email='clientA@test.com';")

echo "  Sub-node DB clients.down_limit: ${REC_DOWN_UPDATED} (expected: 0)"
echo "  Sub-node DB client_inbounds[IB1].down_limit: ${CI1_DOWN_UPDATED} (expected: 80)"
echo "  Sub-node DB client_inbounds[IB2].down_limit: ${CI2_DOWN_UPDATED} (expected: 150)"

if [[ "$REC_DOWN_UPDATED" != "0" ]]; then
    echo "FAIL: Sub-node global down_limit contaminated after update!" >&2
    exit 1
fi
if [[ "$CI1_DOWN_UPDATED" != "80" || "$CI2_DOWN_UPDATED" != "150" ]]; then
    echo "FAIL: Sub-node per-inbound down_limit did not update to 80 / 150!" >&2
    exit 1
fi
echo "==> Modification sync verification PASSED."

# Step 9: Master clears rate limit on IB1 (sets to 0) while keeping IB2 = 150M
echo ""
echo "[Step 9/9] Master clearing rate limit on IB1 (sets to 0)..."
CLEAR_IB1_RESP=$(curl -s -X POST "http://127.0.0.1:2053/panel/api/clients/update/clientA@test.com" \
  -H "Authorization: Bearer ${MASTER_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "email": "clientA@test.com",
    "id": "11111111-2222-3333-4444-555555555555",
    "subId": "sub-clientA",
    "enable": true,
    "downLimit": 0,
    "downLimitByInbound": {
      "'"${MASTER_IB1_ID}"'": 0,
      "'"${MASTER_IB2_ID}"'": 150
    },
    "totalGB": 107374182400,
    "totalGBByInbound": {
      "'"${MASTER_IB1_ID}"'": 10737418240,
      "'"${MASTER_IB2_ID}"'": 21474836480
    }
  }')

if [[ $(echo "$CLEAR_IB1_RESP" | jq -r '.success') != "true" ]]; then
    echo "ERROR: Failed to update client on Master: ${CLEAR_IB1_RESP}" >&2
    exit 1
fi

CI1_DOWN_CLEARED=$(sqlite3 "${TMP_DIR}/subnode/x-ui.db" "SELECT down_limit FROM client_inbounds WHERE inbound_id=${SUBNODE_IB1_ID};")
CI2_DOWN_RETAINED=$(sqlite3 "${TMP_DIR}/subnode/x-ui.db" "SELECT down_limit FROM client_inbounds WHERE inbound_id=${SUBNODE_IB2_ID};")
REC_DOWN_CLEARED=$(sqlite3 "${TMP_DIR}/subnode/x-ui.db" "SELECT down_limit FROM clients WHERE email='clientA@test.com';")

echo "  Sub-node DB clients.down_limit: ${REC_DOWN_CLEARED} (expected: 0)"
echo "  Sub-node DB client_inbounds[IB1].down_limit: ${CI1_DOWN_CLEARED} (expected: 0)"
echo "  Sub-node DB client_inbounds[IB2].down_limit: ${CI2_DOWN_RETAINED} (expected: 150)"

if [[ "$CI1_DOWN_CLEARED" != "0" ]]; then
    echo "FAIL: Sub-node Inbound 1 rate limit was not cleared to 0!" >&2
    exit 1
fi
if [[ "$CI2_DOWN_RETAINED" != "150" ]]; then
    echo "FAIL: Sub-node Inbound 2 rate limit was unexpectedly modified!" >&2
    exit 1
fi
if [[ "$REC_DOWN_CLEARED" != "0" ]]; then
    echo "FAIL: Sub-node global down_limit contaminated after clearing!" >&2
    exit 1
fi

# Step 10: Simulate distinct per-inbound traffic on Sub-node and verify Master sync
echo ""
echo "[Step 10/10] Verifying per-inbound traffic sync from Sub-node to Master..."
echo "==> Waiting for Master to establish initial baseline for Client A..."
for i in {1..30}; do
    BASE_CNT=$(sqlite3 "${TMP_DIR}/master/x-ui.db" "SELECT count(*) FROM node_client_traffics WHERE email='clientA@test.com';" || echo "0")
    if [[ "$BASE_CNT" -gt 0 ]]; then
        break
    fi
    sleep 0.5
done
echo "==> Initial baseline established. Injecting per-inbound traffic on Sub-node..."
sqlite3 "${TMP_DIR}/subnode/x-ui.db" "UPDATE client_inbounds SET up = 1000000, down = 2000000 WHERE inbound_id=${SUBNODE_IB1_ID};"
sqlite3 "${TMP_DIR}/subnode/x-ui.db" "UPDATE client_inbounds SET up = 30000000, down = 40000000 WHERE inbound_id=${SUBNODE_IB2_ID};"
sqlite3 "${TMP_DIR}/subnode/x-ui.db" "UPDATE client_traffics SET up = 31000000, down = 42000000 WHERE email='clientA@test.com';"

echo "==> Waiting for Master NodeTrafficSyncJob to poll Sub-node (up to 15s)..."
SYNCED=false
for i in {1..30}; do
    M_IB1_UP=$(sqlite3 "${TMP_DIR}/master/x-ui.db" "SELECT up FROM client_inbounds WHERE inbound_id=${MASTER_IB1_ID};" || echo "0")
    M_IB1_DOWN=$(sqlite3 "${TMP_DIR}/master/x-ui.db" "SELECT down FROM client_inbounds WHERE inbound_id=${MASTER_IB1_ID};" || echo "0")
    M_IB2_UP=$(sqlite3 "${TMP_DIR}/master/x-ui.db" "SELECT up FROM client_inbounds WHERE inbound_id=${MASTER_IB2_ID};" || echo "0")
    M_IB2_DOWN=$(sqlite3 "${TMP_DIR}/master/x-ui.db" "SELECT down FROM client_inbounds WHERE inbound_id=${MASTER_IB2_ID};" || echo "0")
    M_TOTAL_UP=$(sqlite3 "${TMP_DIR}/master/x-ui.db" "SELECT up FROM client_traffics WHERE email='clientA@test.com';" || echo "0")
    M_TOTAL_DOWN=$(sqlite3 "${TMP_DIR}/master/x-ui.db" "SELECT down FROM client_traffics WHERE email='clientA@test.com';" || echo "0")

    if [[ "$M_IB1_UP" == "1000000" && "$M_IB1_DOWN" == "2000000" && \
          "$M_IB2_UP" == "30000000" && "$M_IB2_DOWN" == "40000000" && \
          "$M_TOTAL_UP" == "31000000" && "$M_TOTAL_DOWN" == "42000000" ]]; then
        SYNCED=true
        break
    fi
    sleep 0.5
done

echo "  Master client_inbounds[IB1]: up=${M_IB1_UP}, down=${M_IB1_DOWN} (expected: 1000000 / 2000000)"
echo "  Master client_inbounds[IB2]: up=${M_IB2_UP}, down=${M_IB2_DOWN} (expected: 30000000 / 40000000)"
echo "  Master client_traffics: up=${M_TOTAL_UP}, down=${M_TOTAL_DOWN} (expected: 31000000 / 42000000)"

if [[ "$SYNCED" != "true" ]]; then
    echo "FAIL: Master per-inbound traffic or global traffic did not sync correctly!" >&2
    echo "--- Master node_client_traffics ---"
    sqlite3 "${TMP_DIR}/master/x-ui.db" "SELECT * FROM node_client_traffics;"
    echo "--- Master client_traffics ---"
    sqlite3 "${TMP_DIR}/master/x-ui.db" "SELECT email, up, down, enable FROM client_traffics;"
    echo "--- Subnode inbounds list ClientStats ---"
    curl -s "http://127.0.0.1:2054/panel/api/inbounds/list" -H "Authorization: Bearer ${SUBNODE_TOKEN}" | jq '.obj[] | {id: .id, tag: .tag, clientStats: .clientStats}'
    echo "--- Master log tail ---"
    tail -n 80 "${TMP_DIR}/master.log"
    exit 1
fi
echo "==> Per-inbound traffic sync verification PASSED."

echo ""
echo "======================================================="
echo "  ALL TESTS PASSED: Master + Sub-node sync is consistent!"
echo "  - Sub-node global down_limit was never contaminated (remained 0)"
echo "  - Inbound 1 and 2 had independent limits (100M and 200M)"
echo "  - Modification correctly synced (80M and 150M)"
echo "  - Clearing limit on Inbound 1 cleanly reset to 0 while keeping Inbound 2 intact"
echo "  - Per-inbound quotas (10GB and 20GB) correctly mapped across panels"
echo "  - Distinct per-inbound client traffic (IB1=3MB, IB2=70MB) accurately synced to Master"
echo "======================================================="

