#!/bin/sh
set -e
case $1 in
    amd64)
        ARCH="64"
        FNAME="amd64"
        ;;
    i386)
        ARCH="32"
        FNAME="i386"
        ;;
    armv8 | arm64 | aarch64)
        ARCH="arm64-v8a"
        FNAME="arm64"
        ;;
    armv7 | arm | arm32)
        ARCH="arm32-v7a"
        FNAME="arm32"
        ;;
    armv6)
        ARCH="arm32-v6"
        FNAME="armv6"
        ;;
    *)
        ARCH="64"
        FNAME="amd64"
        ;;
esac
MTG_MULTI_VER=$(curl -sfL "https://api.github.com/repos/mhsanaei/mtg-multi/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
if [ -z "$MTG_MULTI_VER" ]; then
    echo "DockerInit: could not resolve the latest mtg-multi release tag" >&2
    exit 1
fi
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
XRAY_VERSION=$("$SCRIPT_DIR/tools/latest-xray-feat-version.sh")
mkdir -p build/bin
cd build/bin
XRAY_URL="https://github.com/Rain-kl/Xray-core/releases/download/${XRAY_VERSION}/Xray-linux-${ARCH}.zip"
attempt=1
until curl -sfL -o "Xray-linux-${ARCH}.zip.tmp" "$XRAY_URL"; do
    if [ "$attempt" -ge 40 ]; then
        echo "DockerInit: failed to download custom Xray ${XRAY_VERSION} after ${attempt} attempts" >&2
        exit 1
    fi
    echo "DockerInit: waiting for custom Xray ${XRAY_VERSION} release assets (attempt ${attempt}/40)" >&2
    sleep 15
    attempt=$((attempt + 1))
done
mv "Xray-linux-${ARCH}.zip.tmp" "Xray-linux-${ARCH}.zip"
# FORK(custom-core-only): Verify every architecture before adding its binary to the image.
curl -sfL --retry 5 --retry-all-errors --retry-delay 3 -o "Xray-linux-${ARCH}.zip.dgst" "${XRAY_URL}.dgst"
digest=$(awk '$1 == "SHA2-256=" {print $2}' "Xray-linux-${ARCH}.zip.dgst")
if ! printf '%s\n' "$digest" | grep -Eq '^[a-fA-F0-9]{64}$'; then
    echo "DockerInit: missing custom core SHA256" >&2
    exit 1
fi
printf '%s  %s\n' "$digest" "Xray-linux-${ARCH}.zip" | sha256sum -c -
unzip "Xray-linux-${ARCH}.zip"
test -s xray
rm -f "Xray-linux-${ARCH}.zip" "Xray-linux-${ARCH}.zip.dgst" geoip.dat geosite.dat
mv xray "xray-linux-${FNAME}"
# mtg-multi (MTProto sidecar) ships prebuilt release binaries for every target
# we package, so download and unpack the matching one instead of compiling.
case $FNAME in
    i386) MTGARCH="386" ;;
    arm32) MTGARCH="armv7" ;;
    *) MTGARCH="$FNAME" ;;
esac
MTG_PKG="mtg-multi-${MTG_MULTI_VER#v}-linux-${MTGARCH}"
curl -sfLRO "https://github.com/mhsanaei/mtg-multi/releases/download/${MTG_MULTI_VER}/${MTG_PKG}.tar.gz"
tar -xzf "${MTG_PKG}.tar.gz"
mv "${MTG_PKG}/mtg-multi" "mtg-linux-${FNAME}"
rm -rf "${MTG_PKG}" "${MTG_PKG}.tar.gz"
chmod +x "mtg-linux-${FNAME}"
curl -sfLRO https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat
curl -sfLRO https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat
curl -sfLRo geoip_IR.dat https://github.com/chocolate4u/Iran-v2ray-rules/releases/latest/download/geoip.dat
curl -sfLRo geosite_IR.dat https://github.com/chocolate4u/Iran-v2ray-rules/releases/latest/download/geosite.dat
curl -sfLRo geoip_RU.dat https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/latest/download/geoip.dat
curl -sfLRo geosite_RU.dat https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/latest/download/geosite.dat
cd ../../
