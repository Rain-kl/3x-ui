#!/bin/sh
set -e
# $1 is Docker's TARGETARCH and $2 its TARGETVARIANT (v6/v7 on arm). The
# platform names match the release archives and the mtg-multi packages.
case $1 in
    amd64 | "")
        PLATFORM="amd64"
        ;;
    arm64 | armv8 | aarch64)
        PLATFORM="arm64"
        ;;
    arm | arm32 | armv7)
        PLATFORM="armv7"
        if [ "$2" = "v6" ]; then
            PLATFORM="armv6"
        fi
        ;;
    armv6)
        PLATFORM="armv6"
        ;;
    386 | i386)
        PLATFORM="386"
        ;;
    *)
        echo "DockerInit: unsupported architecture '$1'" >&2
        exit 1
        ;;
esac
case $PLATFORM in
    amd64) XRAY_ZIP="64" ;;
    arm64) XRAY_ZIP="arm64-v8a" ;;
    armv7) XRAY_ZIP="arm32-v7a" ;;
    armv6) XRAY_ZIP="arm32-v6" ;;
    386) XRAY_ZIP="32" ;;
esac
# The panel looks binaries up by GOARCH; on arm Xray's is "arm32"
# (internal/xray/process.go) and mtg's plain "arm", as install.sh renames them.
case $PLATFORM in
    armv7 | armv6)
        XRAY_BIN="xray-linux-arm32"
        MTG_BIN="mtg-linux-arm"
        ;;
    *)
        XRAY_BIN="xray-linux-${PLATFORM}"
        MTG_BIN="mtg-linux-${PLATFORM}"
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
XRAY_URL="https://github.com/Rain-kl/Xray-core/releases/download/${XRAY_VERSION}/Xray-linux-${XRAY_ZIP}.zip"
attempt=1
until curl -sfL -o "Xray-linux-${XRAY_ZIP}.zip.tmp" "$XRAY_URL"; do
    if [ "$attempt" -ge 40 ]; then
        echo "DockerInit: failed to download custom Xray ${XRAY_VERSION} after ${attempt} attempts" >&2
        exit 1
    fi
    echo "DockerInit: waiting for custom Xray ${XRAY_VERSION} release assets (attempt ${attempt}/40)" >&2
    sleep 15
    attempt=$((attempt + 1))
done
mv "Xray-linux-${XRAY_ZIP}.zip.tmp" "Xray-linux-${XRAY_ZIP}.zip"
# FORK(custom-core-only): Verify every architecture before adding its binary to the image.
curl -sfL --retry 5 --retry-all-errors --retry-delay 3 -o "Xray-linux-${XRAY_ZIP}.zip.dgst" "${XRAY_URL}.dgst"
digest=$(awk '$1 == "SHA2-256=" {print $2}' "Xray-linux-${XRAY_ZIP}.zip.dgst")
if ! printf '%s\n' "$digest" | grep -Eq '^[a-fA-F0-9]{64}$'; then
    echo "DockerInit: missing custom core SHA256" >&2
    exit 1
fi
printf '%s  %s\n' "$digest" "Xray-linux-${XRAY_ZIP}.zip" | sha256sum -c -
unzip "Xray-linux-${XRAY_ZIP}.zip"
test -s xray
rm -f "Xray-linux-${XRAY_ZIP}.zip" "Xray-linux-${XRAY_ZIP}.zip.dgst" geoip.dat geosite.dat
mv xray "${XRAY_BIN}"
# mtg-multi (MTProto sidecar) ships prebuilt release binaries for every target
# we package, so download and unpack the matching one instead of compiling.
MTG_PKG="mtg-multi-${MTG_MULTI_VER#v}-linux-${PLATFORM}"
curl -sfLRO "https://github.com/mhsanaei/mtg-multi/releases/download/${MTG_MULTI_VER}/${MTG_PKG}.tar.gz"
tar -xzf "${MTG_PKG}.tar.gz"
mv "${MTG_PKG}/mtg-multi" "${MTG_BIN}"
rm -rf "${MTG_PKG}" "${MTG_PKG}.tar.gz"
chmod +x "${MTG_BIN}"
curl -sfLRO https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat
curl -sfLRO https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat
curl -sfLRo geoip_IR.dat https://github.com/chocolate4u/Iran-v2ray-rules/releases/latest/download/geoip.dat
curl -sfLRo geosite_IR.dat https://github.com/chocolate4u/Iran-v2ray-rules/releases/latest/download/geosite.dat
curl -sfLRo geoip_RU.dat https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/latest/download/geoip.dat
curl -sfLRo geosite_RU.dat https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/latest/download/geosite.dat
cd ../../
