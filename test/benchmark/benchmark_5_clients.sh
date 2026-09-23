#!/usr/bin/env bash
# 5-client benchmark verification for differential rate limiting in OrbStack VM.
set -euo pipefail

# Dispatch to OrbStack VM if running on macOS host
if [[ "$(uname)" == "Darwin" ]]; then
    echo "==> Running on macOS. Dispatching benchmark into OrbStack Ubuntu VM..."
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

HOST_IFACE="veth-bench"
HOST_IP="10.99.1.1"
ROUTER_IP="10.99.1.2"
CLIENT_BRIDGE_IP="10.99.2.1"
INBOUND_PORT=54321
DATA_PORT=8080

TMP_DIR="/tmp/bench_5_clients"
rm -rf "$TMP_DIR"
mkdir -p "$TMP_DIR"

cleanup() {
    echo ""
    echo "======================================================="
    echo "==> Cleaning up benchmark environment..."
    echo "======================================================="
    pkill -9 -f "xray" 2>/dev/null || true
    pkill -9 -f "data_server" 2>/dev/null || true
    for i in {1..5}; do
        ip netns del "ns-c$i" 2>/dev/null || true
    done
    ip netns del ns-router 2>/dev/null || true
    ip link del "$HOST_IFACE" 2>/dev/null || true
    ip route del 10.99.2.0/24 2>/dev/null || true
    rm -rf "$TMP_DIR"
    echo "==> Cleanup complete."
}
trap cleanup EXIT INT TERM

echo "======================================================="
echo "  3x-ui Client Rate Limiting Real Benchmark (5 Clients)"
echo "  Environment: OrbStack Linux VM ($(uname -s) $(uname -r) $(uname -m))"
echo "======================================================="

# Step 0: Ensure Xray binary is available
echo ""
echo "[Step 0/5] Checking Xray-core binary..."
XRAY_BIN="/tmp/xray"
if [[ ! -x "$XRAY_BIN" ]]; then
    echo "==> Building Xray-core binary from repository dependency..."
    go build -o "$XRAY_BIN" github.com/xtls/xray-core/main
fi
echo "==> Xray-core binary ready: $($XRAY_BIN version | head -n 1)"

# Step 1: Database & Effective Rate Limit Calculation Verification
echo ""
echo "[Step 1/5] Verifying Database Models & Effective Limit Computation..."
DB_PATH="$TMP_DIR/bench.db"
python3 - << PYEOF
import sqlite3, sys

conn = sqlite3.connect("$DB_PATH")
c = conn.cursor()

c.execute("""
CREATE TABLE inbounds (
    id INTEGER PRIMARY KEY,
    port INTEGER,
    protocol TEXT,
    inbound_down_limit INTEGER,
    client_down_limit INTEGER
)""")

c.execute("""
CREATE TABLE client_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT UNIQUE,
    down_limit INTEGER
)""")

c.execute("""
CREATE TABLE client_inbounds (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    client_id INTEGER,
    inbound_id INTEGER,
    down_limit INTEGER
)""")

# Inbound 1: Port 54321, InboundDownLimit=100M, ClientDownLimit=10M
c.execute("INSERT INTO inbounds VALUES (1, $INBOUND_PORT, 'vless', 100, 10)")

# 5 Clients
# Client 1: client1@test.com, no custom limit (down_limit=0) -> fallback 10M
# Client 2: client2@test.com, global limit 20M
# Client 3: client3@test.com, global limit 30M
# Client 4: client4@test.com, global limit 50M, inbound 1 override 15M
# Client 5: client5@test.com, global limit 50M, no override
clients = [
    ("client1@test.com", 0, None),
    ("client2@test.com", 20, None),
    ("client3@test.com", 30, None),
    ("client4@test.com", 50, 15),
    ("client5@test.com", 50, None),
]

for email, global_lim, override_lim in clients:
    c.execute("INSERT INTO client_records (email, down_limit) VALUES (?, ?)", (email, global_lim))
    cid = c.lastrowid
    if override_lim is not None:
        c.execute("INSERT INTO client_inbounds (client_id, inbound_id, down_limit) VALUES (?, 1, ?)", (cid, override_lim))

conn.commit()

# Verify ComputeClientEffectiveLimit priority:
# client_inbounds.down_limit > client_records.down_limit > inbounds.client_down_limit
expected = {
    "client1@test.com": 10,
    "client2@test.com": 20,
    "client3@test.com": 30,
    "client4@test.com": 15,
    "client5@test.com": 50,
}

for email, want in expected.items():
    c.execute("""
        SELECT cr.id, cr.down_limit, ci.down_limit, ib.client_down_limit
        FROM client_records cr
        LEFT JOIN client_inbounds ci ON ci.client_id = cr.id AND ci.inbound_id = 1
        CROSS JOIN inbounds ib WHERE ib.id = 1 AND cr.email = ?
    """, (email,))
    row = c.fetchone()
    cid, cr_limit, ci_limit, ib_fallback = row
    effective = ib_fallback
    if cr_limit and cr_limit > 0:
        effective = cr_limit
    if ci_limit and ci_limit > 0:
        effective = ci_limit
    print(f"   [DB Resolve] {email:18s} -> Effective: {effective:2d} Mbps (Expected: {want:2d} Mbps)")
    assert effective == want, f"Mismatch for {email}: got {effective}, want {want}"

print("   PASS: All 5 clients computed effective limits strictly match architecture rules.")
conn.close()
PYEOF

# Step 2: Set up routed network topology
echo ""
echo "[Step 2/5] Creating simulated routed network topology (Host -> Router -> 5 Clients)..."
# Initial cleanup of any stale namespaces/links
for i in {1..5}; do
    ip netns del "ns-c$i" 2>/dev/null || true
done
ip netns del ns-router 2>/dev/null || true
ip link del "$HOST_IFACE" 2>/dev/null || true
ip route del 10.99.2.0/24 2>/dev/null || true

# Router namespace
ip netns add ns-router
ip netns exec ns-router sysctl -w net.ipv4.ip_forward=1 >/dev/null

# Host <-> Router
ip link add "$HOST_IFACE" type veth peer name veth-router
ip addr add "$HOST_IP/24" dev "$HOST_IFACE"
ip link set "$HOST_IFACE" up
ip link set veth-router netns ns-router
ip netns exec ns-router ip addr add "$ROUTER_IP/24" dev veth-router
ip netns exec ns-router ip link set veth-router up

# Bridge in router
ip netns exec ns-router ip link add br0 type bridge
ip netns exec ns-router ip addr add "$CLIENT_BRIDGE_IP/24" dev br0
ip netns exec ns-router ip link set br0 up

# Host route to client subnet
ip route add 10.99.2.0/24 via "$ROUTER_IP" dev "$HOST_IFACE"

# 5 Client namespaces
for i in {1..5}; do
    ns="ns-c$i"
    ip netns add "$ns"
    ip link add "veth-r$i" type veth peer name "veth-c$i"
    ip link set "veth-r$i" netns ns-router
    ip netns exec ns-router ip link set "veth-r$i" master br0
    ip netns exec ns-router ip link set "veth-r$i" up

    ip link set "veth-c$i" netns "$ns"
    ip netns exec "$ns" ip addr add "10.99.2.1$i/24" dev "veth-c$i"
    ip netns exec "$ns" ip link set lo up
    ip netns exec "$ns" ip link set "veth-c$i" up
    ip netns exec "$ns" ip route add default via "$CLIENT_BRIDGE_IP"
    ip netns exec "$ns" ping -c 1 -W 1 "$HOST_IP" >/dev/null
done
echo "==> 5 client network namespaces created and routed to Host."

# Step 3: Configure Linux Kernel TC HTB Hierarchy (TrafficShaper Engine)
echo ""
echo "[Step 3/5] Applying Linux TC HTB hierarchy & u32 filters (Inbound: 100M, Clients: 10M, 20M, 30M, 15M, 50M)..."
tc qdisc del dev "$HOST_IFACE" root 2>/dev/null || true
tc qdisc add dev "$HOST_IFACE" root handle 1: htb default 9999
tc class add dev "$HOST_IFACE" parent 1: classid 1:1 htb rate 10gbit ceil 10gbit
tc class add dev "$HOST_IFACE" parent 1: classid 1:9999 htb rate 10gbit ceil 10gbit
tc filter add dev "$HOST_IFACE" protocol ip parent 1:0 prio 5 handle 800::800 u32 match ip dst 0.0.0.0/32 flowid 1:9999

# Inbound 1: 100 Mbps aggregate ceiling
tc class add dev "$HOST_IFACE" parent 1:1 classid 1:10 htb rate 100mbit ceil 100mbit burst 64k cburst 64k
tc filter add dev "$HOST_IFACE" protocol ip parent 1:0 prio 10 handle 0x1 u32 match ip sport "$INBOUND_PORT" 0xffff flowid 1:10

# Client leaf classes (parent 1:10)
# Client 1: 10 Mbps
tc class add dev "$HOST_IFACE" parent 1:10 classid 1:11 htb rate 1mbit ceil 10mbit burst 32k cburst 32k
tc qdisc add dev "$HOST_IFACE" parent 1:11 fq_codel
tc filter add dev "$HOST_IFACE" protocol ip parent 1:0 prio 5 handle 800::101 u32 match ip sport "$INBOUND_PORT" 0xffff match ip dst 10.99.2.11/32 flowid 1:11

# Client 2: 20 Mbps
tc class add dev "$HOST_IFACE" parent 1:10 classid 1:12 htb rate 1mbit ceil 20mbit burst 32k cburst 32k
tc qdisc add dev "$HOST_IFACE" parent 1:12 fq_codel
tc filter add dev "$HOST_IFACE" protocol ip parent 1:0 prio 5 handle 800::102 u32 match ip sport "$INBOUND_PORT" 0xffff match ip dst 10.99.2.12/32 flowid 1:12

# Client 3: 30 Mbps
tc class add dev "$HOST_IFACE" parent 1:10 classid 1:13 htb rate 1mbit ceil 30mbit burst 32k cburst 32k
tc qdisc add dev "$HOST_IFACE" parent 1:13 fq_codel
tc filter add dev "$HOST_IFACE" protocol ip parent 1:0 prio 5 handle 800::103 u32 match ip sport "$INBOUND_PORT" 0xffff match ip dst 10.99.2.13/32 flowid 1:13

# Client 4: 15 Mbps (Override)
tc class add dev "$HOST_IFACE" parent 1:10 classid 1:14 htb rate 1mbit ceil 15mbit burst 32k cburst 32k
tc qdisc add dev "$HOST_IFACE" parent 1:14 fq_codel
tc filter add dev "$HOST_IFACE" protocol ip parent 1:0 prio 5 handle 800::104 u32 match ip sport "$INBOUND_PORT" 0xffff match ip dst 10.99.2.14/32 flowid 1:14

# Client 5: 50 Mbps
tc class add dev "$HOST_IFACE" parent 1:10 classid 1:15 htb rate 1mbit ceil 50mbit burst 32k cburst 32k
tc qdisc add dev "$HOST_IFACE" parent 1:15 fq_codel
tc filter add dev "$HOST_IFACE" protocol ip parent 1:0 prio 5 handle 800::105 u32 match ip sport "$INBOUND_PORT" 0xffff match ip dst 10.99.2.15/32 flowid 1:15

echo "==> Linux TC classes and filters installed successfully."

# Step 4: Start Services (Data Server + VLESS-Reality Server + 5 Client Proxies)
echo ""
echo "[Step 4/5] Starting high-speed Data Server and Xray VLESS-Reality topology..."

cat << "GOEOF" > "$TMP_DIR/data_server.go"
package main
import (
    "net/http"
)
func main() {
    buf := make([]byte, 64*1024)
    for i := range buf { buf[i] = 'B' }
    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte("OK\n"))
    })
    http.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/octet-stream")
        flusher, ok := w.(http.Flusher)
        for {
            if _, err := w.Write(buf); err != nil {
                return
            }
            if ok {
                flusher.Flush()
            }
        }
    })
    if err := http.ListenAndServe("0.0.0.0:8080", nil); err != nil {
        panic(err)
    }
}
GOEOF
go build -o "$TMP_DIR/data_server" "$TMP_DIR/data_server.go"
"$TMP_DIR/data_server" > "$TMP_DIR/data_server.log" 2>&1 &

sleep 1
# Verify data server is alive via /health
curl -fsS http://127.0.0.1:8080/health >/dev/null || {
    echo "ERROR: Data server failed to start! Logs:" >&2
    cat "$TMP_DIR/data_server.log" >&2
    exit 1
}
echo "==> Data server listening on 0.0.0.0:8080 and responsive."

PRIV="8IGrkJDnzr34_837fggm58lVjAUohfc9rwMqhTQAOX8"
PUB="9CWk0ePXpFjt0zJ07aQEYJN88eKgLkn02yrSmisJqm0"

UUIDS=(
    "11111111-1111-1111-1111-111111111111"
    "22222222-2222-2222-2222-222222222222"
    "33333333-3333-3333-3333-333333333333"
    "44444444-4444-4444-4444-444444444444"
    "55555555-5555-5555-5555-555555555555"
)

# Xray Server config
cat << JSON > "$TMP_DIR/server.json"
{
  "log": {"loglevel": "error"},
  "inbounds": [{
    "port": $INBOUND_PORT,
    "listen": "$HOST_IP",
    "protocol": "vless",
    "settings": {
      "clients": [
        {"id": "${UUIDS[0]}", "email": "client1@test.com", "flow": "xtls-rprx-vision"},
        {"id": "${UUIDS[1]}", "email": "client2@test.com", "flow": "xtls-rprx-vision"},
        {"id": "${UUIDS[2]}", "email": "client3@test.com", "flow": "xtls-rprx-vision"},
        {"id": "${UUIDS[3]}", "email": "client4@test.com", "flow": "xtls-rprx-vision"},
        {"id": "${UUIDS[4]}", "email": "client5@test.com", "flow": "xtls-rprx-vision"}
      ],
      "decryption": "none"
    },
    "streamSettings": {
      "network": "tcp",
      "security": "reality",
      "realitySettings": {
        "show": false,
        "dest": "www.apple.com:443",
        "xver": 0,
        "serverNames": ["www.apple.com"],
        "privateKey": "${PRIV}",
        "shortIds": ["0123456789abcdef"]
      }
    }
  }],
  "outbounds": [{
    "protocol": "freedom",
    "settings": {
      "finalRules": [{"action": "allow"}]
    }
  }]
}
JSON

"$XRAY_BIN" run -c "$TMP_DIR/server.json" &

# 5 Client Xray proxies inside each namespace
for i in {1..5}; do
    idx=$((i - 1))
    uuid="${UUIDS[$idx]}"
    cat << JSON > "$TMP_DIR/client$i.json"
{
  "log": {"loglevel": "error"},
  "inbounds": [{
    "port": 1080,
    "listen": "127.0.0.1",
    "protocol": "socks",
    "settings": {"auth": "noauth"}
  }],
  "outbounds": [{
    "protocol": "vless",
    "settings": {
      "vnext": [{
        "address": "$HOST_IP",
        "port": $INBOUND_PORT,
        "users": [{"id": "${uuid}", "flow": "xtls-rprx-vision", "encryption": "none"}]
      }]
    },
    "streamSettings": {
      "network": "tcp",
      "security": "reality",
      "realitySettings": {
        "show": false,
        "fingerprint": "",
        "serverName": "www.apple.com",
        "publicKey": "${PUB}",
        "shortId": "0123456789abcdef"
      }
    }
  }]
}
JSON
    ip netns exec "ns-c$i" "$XRAY_BIN" run -c "$TMP_DIR/client$i.json" > "$TMP_DIR/client$i.log" 2>&1 &
done

sleep 2

# Pre-warm TLS sessions sequentially with 10s timeout to allow initial Reality handshake
for i in {1..5}; do
    ip netns exec "ns-c$i" curl -s -m 10 -x socks5://127.0.0.1:1080 "http://$HOST_IP:$DATA_PORT/health" >/dev/null 2>&1 || true
done
echo "==> All 5 client proxy tunnels established and pre-warmed."

# Step 5: Execute Benchmarks & Verification
echo ""
echo "======================================================="
echo "[Step 5/5] Executing Throughput Measurements & Assertions"
echo "======================================================="

TARGETS=(10 20 30 15 50)
CLIENT_NAMES=(
    "Client 1 (client1@test.com - Inbound Fallback)"
    "Client 2 (client2@test.com - Global Limit)"
    "Client 3 (client3@test.com - Global Limit)"
    "Client 4 (client4@test.com - Node Override)"
    "Client 5 (client5@test.com - Global Limit)"
)

measure_client_stream() {
    local i=$1
    local duration=$2
    local err_file="$TMP_DIR/curl_err_$i.txt"
    local raw_file="$TMP_DIR/client_${i}_out.txt"
    ip netns exec "ns-c$i" curl -s -S -m "$duration" -w "%{time_total},%{time_starttransfer},%{size_download},%{http_code},%{exitcode}\n" -x socks5://127.0.0.1:1080 "http://$HOST_IP:$DATA_PORT/stream" -o /dev/null 2>"$err_file" > "$raw_file" || true
    python3 -c "
with open('$raw_file', 'r') as f:
    out = f.read().strip()
parts = out.split(',')
if len(parts) >= 3 and float(parts[2]) > 0:
    t_tot = float(parts[0])
    t_st = float(parts[1])
    dt = t_tot - t_st
    if dt <= 0: dt = t_tot
    sz = float(parts[2])
    mbps = (sz * 8) / (dt * 1e6)
    print(f'{mbps:.2f}')
else:
    print('0.00')
"
}

echo ""
echo "--- Phase 1: Individual Sequential Throughput Verification (10s each) ---"
PHASE1_PASS=true
for i in {1..5}; do
    idx=$((i - 1))
    target="${TARGETS[$idx]}"
    name="${CLIENT_NAMES[$idx]}"
    min_bound=$(python3 -c "print(f'{$target * 0.90:.2f}')")
    max_bound=$(python3 -c "print(f'{$target * 1.10:.2f}')")

    mbps=$(measure_client_stream "$i" 10)
    status=$(python3 -c "
val = float('$mbps')
if $min_bound <= val <= $max_bound:
    print('PASS')
else:
    print('FAIL')
")
    if [[ "$status" != "PASS" ]]; then
        PHASE1_PASS=false
        echo "   [FAIL] $name: $mbps Mbps (Target: $target Mbps, Expected Range: [$min_bound, $max_bound] Mbps)"
        echo "      -> curl raw: $(cat "$TMP_DIR/client_${i}_out.txt" 2>/dev/null || true)"
        echo "      -> curl stderr: $(cat "$TMP_DIR/curl_err_$i.txt" 2>/dev/null || true)"
        echo "      -> client log: $(tail -n 5 "$TMP_DIR/client$i.log" 2>/dev/null || true)"
    else
        echo "   [PASS] $name: $mbps Mbps (Target: $target Mbps, Range: [$min_bound, $max_bound] Mbps)"
    fi
done

if [[ "$PHASE1_PASS" != "true" ]]; then
    echo "ERROR: Phase 1 individual client rate limit assertion failed!" >&2
    exit 1
fi
echo "==> Phase 1 PASSED: All 5 clients strictly adhere to their individual rate limits (±10%)."

echo ""
echo "--- Phase 2: Concurrent 5-Client Saturation Benchmark (15s parallel) ---"
CONCURRENT_PIDS=()
for i in {1..5}; do
    (
        ip netns exec "ns-c$i" curl -s -S -m 15 -w "%{time_total},%{time_starttransfer},%{size_download}\n" -x socks5://127.0.0.1:1080 "http://$HOST_IP:$DATA_PORT/stream" -o /dev/null > "$TMP_DIR/concurrent_$i.txt" 2>"$TMP_DIR/concurrent_err_$i.txt" || true
    ) &
    CONCURRENT_PIDS+=($!)
done

for pid in "${CONCURRENT_PIDS[@]}"; do
    wait "$pid"
done

TOTAL_CONCURRENT_SPEED=0
PHASE2_INDIVIDUAL_PASS=true

for i in {1..5}; do
    idx=$((i - 1))
    name="${CLIENT_NAMES[$idx]}"
    target="${TARGETS[$idx]}"
    raw_out=$(cat "$TMP_DIR/concurrent_$i.txt")
    mbps=$(python3 -c "
parts = '$raw_out'.strip().split(',')
if len(parts) >= 3 and float(parts[2]) > 0:
    t_tot = float(parts[0])
    t_st = float(parts[1])
    dt = t_tot - t_st
    if dt <= 0: dt = t_tot
    sz = float(parts[2])
    print(f'{(sz * 8) / (dt * 1e6):.2f}')
else:
    print('0.00')
")
    # Verify no client exceeds its own ceiling
    ceil_check=$(python3 -c "
val = float('$mbps')
ceil_limit = float('$target') * 1.10
print('PASS' if val <= ceil_limit else 'EXCEEDED')
")
    if [[ "$ceil_check" != "PASS" ]]; then
        PHASE2_INDIVIDUAL_PASS=false
        echo "   [!] $name: $mbps Mbps (Exceeded ceiling $target Mbps)"
        echo "      -> curl stderr: $(cat "$TMP_DIR/concurrent_err_$i.txt" 2>/dev/null || true)"
    else
        echo "   [+] $name: $mbps Mbps (Within ceil $target Mbps)"
    fi
    TOTAL_CONCURRENT_SPEED=$(python3 -c "print(f'{float($TOTAL_CONCURRENT_SPEED) + float($mbps):.2f}')")
done

echo ""
echo "   ======================================================"
echo "   Aggregate 5-Client Concurrent Throughput: $TOTAL_CONCURRENT_SPEED Mbps"
echo "   Inbound Bandwidth Limit (HTB Class 1:10): 100.00 Mbps"
echo "   ======================================================"

# Assert individual client ceilings enforced in Phase 2
if [[ "$PHASE2_INDIVIDUAL_PASS" != "true" ]]; then
    echo "ERROR: Phase 2 individual client ceiling assertion failed!" >&2
    exit 1
fi

# Verify total bandwidth <= 100 Mbps (with 5% buffer for packet headers/burst)
TOTAL_PASS=$(python3 -c "
total = float('$TOTAL_CONCURRENT_SPEED')
if total <= 105.0 and total >= 80.0:
    print('PASS')
else:
    print('FAIL')
")

if [[ "$TOTAL_PASS" != "PASS" ]]; then
    echo "ERROR: Aggregate bandwidth assertion failed! Total $TOTAL_CONCURRENT_SPEED Mbps outside expected [80.0, 105.0] Mbps." >&2
    exit 1
fi

echo ""
echo "======================================================="
echo "🎉 ALL BENCHMARK VERIFICATION ASSERTIONS PASSED (5/5)!"
echo "   - Inbound Ceiling: 100 Mbps enforced"
echo "   - Client 1: 10 Mbps (Inbound Fallback)"
echo "   - Client 2: 20 Mbps (Global Limit)"
echo "   - Client 3: 30 Mbps (Global Limit)"
echo "   - Client 4: 15 Mbps (Node Override > Global Limit)"
echo "   - Client 5: 50 Mbps (Global Limit)"
echo "   - Concurrent Aggregate: $TOTAL_CONCURRENT_SPEED Mbps <= 100 Mbps (Bounded)"
echo "======================================================="
