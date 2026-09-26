#!/usr/bin/env bash
# Client per-node traffic limit and monitoring real-device simulation in OrbStack VM.
set -euo pipefail

# Dispatch to OrbStack VM if running on macOS host
if [[ "$(uname)" == "Darwin" ]]; then
    echo "==> Running on macOS. Dispatching simulation into OrbStack Ubuntu VM..."
    SCRIPT_PATH="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
    exec orb -m ubuntu sudo bash "${SCRIPT_PATH}" "$@"
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${REPO_DIR}"

cleanup() {
    pkill -9 -f "xray" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo "====================================================================="
echo "  3x-ui Client Per-Node Traffic Limit Real Simulation (OrbStack VM)  "
echo "  Environment: Linux $(uname -r) ($(uname -m))                       "
echo "====================================================================="

XRAY_BIN="/tmp/xray"
if [[ ! -x "$XRAY_BIN" ]]; then
    echo "==> Building Xray-core binary..."
    go build -o "$XRAY_BIN" github.com/xtls/xray-core/main
fi

echo "==> Xray-core binary ready: $($XRAY_BIN version | head -n 1)"
echo "==> Launching simulation test suite..."
echo ""

XRAY_BIN="$XRAY_BIN" go run "${SCRIPT_DIR}/simtest_traffic_limit.go"
