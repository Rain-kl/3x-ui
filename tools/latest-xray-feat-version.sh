#!/bin/sh
set -eu

releases=$(curl -fsSL --retry 3 \
    'https://api.github.com/repos/Rain-kl/Xray-core/releases?per_page=100')
version=$(printf '%s\n' "$releases" | awk '
    function newer(candidate, current, a, b, i) {
        if (current == "") return 1
        sub(/^v/, "", candidate)
        sub(/^v/, "", current)
        split(candidate, a, /[.-]/)
        split(current, b, /[.-]/)
        for (i = 1; i <= 5; i++) {
            if ((a[i] + 0) != (b[i] + 0)) return (a[i] + 0) > (b[i] + 0)
        }
        return 0
    }
    /"tag_name":/ {
        tag = $0
        sub(/^.*"tag_name":[[:space:]]*"/, "", tag)
        sub(/".*/, "", tag)
    }
    /"draft":/ {
        draft = $0
        sub(/^.*"draft":[[:space:]]*/, "", draft)
        sub(/,.*/, "", draft)
    }
    /"prerelease":/ {
        prerelease = $0
        sub(/^.*"prerelease":[[:space:]]*/, "", prerelease)
        sub(/,.*/, "", prerelease)
        if (tag ~ /^v[0-9]+\.[0-9]+\.[0-9]+-feat\.[0-9]+$/ && draft == "false" && prerelease == "false" && newer(tag, best)) best = tag
    }
    END { print best }
')

if [ -z "$version" ]; then
    echo "No published v*-feat.* release found in Rain-kl/Xray-core" >&2
    exit 1
fi

printf '%s\n' "$version"
