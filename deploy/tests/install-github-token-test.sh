#!/bin/bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

cat > "$TEMP_DIR/curl" <<'EOF'
#!/bin/bash
printf '%s\n' "$@" > "$CURL_ARGS_LOG"
env > "${CURL_ARGS_LOG}.env"
cat > "${CURL_ARGS_LOG}.stdin"
printf '%s' "${CURL_FAKE_STATUS:-}"
EOF
chmod +x "$TEMP_DIR/curl"

mkdir "$TEMP_DIR/home"
cat > "$TEMP_DIR/home/.curlrc" <<'EOF'
url = "https://example.com/collect"
header = "X-Leaked-From-Curlrc: yes"
EOF

run_api_curl() {
    CURL_ARGS_LOG="$1" HOME="$TEMP_DIR/home" PATH="$TEMP_DIR:$PATH" UPDATE_GITHUB_TOKEN="${2:-}" \
        GITHUB_TOKEN="github-fallback" GH_TOKEN="gh-fallback" \
        bash -c 'source <(head -n -1 "$1"); github_api_curl -s "$2"' bash \
        "$ROOT_DIR/deploy/install.sh" "https://api.github.com/repos/Wei-Shaw/sub2api/releases/latest"
}

run_api_curl "$TEMP_DIR/authenticated" "update-secret"
test "$(head -n 1 "$TEMP_DIR/authenticated")" = '-q'
grep -Fxq -- '--config' "$TEMP_DIR/authenticated"
grep -Fxq -- '-' "$TEMP_DIR/authenticated"
grep -Fxq -- '--globoff' "$TEMP_DIR/authenticated"
grep -Fxq 'header = "Authorization: Bearer update-secret"' "$TEMP_DIR/authenticated.stdin"
if grep -Fq 'update-secret' "$TEMP_DIR/authenticated"; then
    echo "installer exposed the update token in curl argv" >&2
    exit 1
fi
if grep -Eq 'update-secret|github-fallback|gh-fallback' "$TEMP_DIR/authenticated.env"; then
    echo "installer exposed a token in curl environment" >&2
    exit 1
fi
test "$(grep -Fxc 'https://api.github.com/repos/Wei-Shaw/sub2api/releases/latest' "$TEMP_DIR/authenticated")" -eq 1
if grep -Fq 'example.com/collect' "$TEMP_DIR/authenticated" || grep -Fq 'X-Leaked-From-Curlrc' "$TEMP_DIR/authenticated" ||
    grep -Fq 'example.com/collect' "$TEMP_DIR/authenticated.stdin" || grep -Fq 'X-Leaked-From-Curlrc' "$TEMP_DIR/authenticated.stdin"; then
    echo "installer allowed hostile curl config into authenticated invocation" >&2
    exit 1
fi

run_api_curl "$TEMP_DIR/anonymous"
test "$(head -n 1 "$TEMP_DIR/anonymous")" = '-q'
if grep -Eq 'github-fallback|gh-fallback' "$TEMP_DIR/anonymous.env"; then
    echo "installer exposed a fallback token in anonymous curl environment" >&2
    exit 1
fi
if grep -Fq 'Authorization:' "$TEMP_DIR/anonymous"; then
    echo "installer unexpectedly used a fallback token" >&2
    exit 1
fi
test ! -s "$TEMP_DIR/anonymous.stdin"
test "$(grep -Fxc 'https://api.github.com/repos/Wei-Shaw/sub2api/releases/latest' "$TEMP_DIR/anonymous")" -eq 1
if grep -Fq 'example.com/collect' "$TEMP_DIR/anonymous" || grep -Fq 'X-Leaked-From-Curlrc' "$TEMP_DIR/anonymous"; then
    echo "installer allowed hostile curl config into anonymous invocation" >&2
    exit 1
fi

assert_unsafe_invocation_rejected() {
    local name=$1
    shift
    rm -f "$TEMP_DIR/$name" "$TEMP_DIR/$name.stdin"
    if CURL_ARGS_LOG="$TEMP_DIR/$name" PATH="$TEMP_DIR:$PATH" UPDATE_GITHUB_TOKEN="update-secret" \
        bash -c 'source <(head -n -1 "$1"); shift; github_api_curl "$@"' bash \
        "$ROOT_DIR/deploy/install.sh" "$@" 2>/dev/null; then
        echo "installer accepted unsafe curl invocation: $name" >&2
        exit 1
    fi
    if [ -e "$TEMP_DIR/$name" ]; then
        echo "installer invoked curl for unsafe request: $name" >&2
        exit 1
    fi
}

assert_unsafe_invocation_rejected non-api -s \
    "https://github.com/Wei-Shaw/sub2api/releases/download/v1/asset"
assert_unsafe_invocation_rejected mixed-host -s \
    "https://api.github.com/repos/Wei-Shaw/sub2api/releases/latest" \
    "https://example.com/collect"
assert_unsafe_invocation_rejected multiple-api -s \
    "https://api.github.com/repos/Wei-Shaw/sub2api/releases/latest" \
    "https://api.github.com/repos/Wei-Shaw/sub2api/releases"
assert_unsafe_invocation_rejected url-option -s --url \
    "https://example.com/collect" \
    "https://api.github.com/repos/Wei-Shaw/sub2api/releases/latest"

run_asset_curl() {
    CURL_ARGS_LOG="$1" CURL_FAKE_STATUS="${3:-}" HOME="$TEMP_DIR/home" PATH="$TEMP_DIR:$PATH" UPDATE_GITHUB_TOKEN="${2:-}" \
        GITHUB_TOKEN="github-fallback" GH_TOKEN="gh-fallback" \
        bash -c 'source <(head -n -1 "$1"); github_asset_curl "$2" "$3"' bash \
        "$ROOT_DIR/deploy/install.sh" \
        "https://api.github.com/repos/h614626370-del/sub2api/releases/assets/123" \
        "$TEMP_DIR/private-asset"
}

run_asset_curl "$TEMP_DIR/asset-authenticated" "update-secret" "200"
if grep -Fxq -- '--location' "$TEMP_DIR/asset-authenticated"; then
    echo "authenticated asset request followed redirects with the token attached" >&2
    exit 1
fi
grep -Fxq -- '--fail' "$TEMP_DIR/asset-authenticated"
grep -Fxq -- '--output' "$TEMP_DIR/asset-authenticated"
grep -Fxq -- '--dump-header' "$TEMP_DIR/asset-authenticated"
grep -Fxq -- '--write-out' "$TEMP_DIR/asset-authenticated"
grep -Fxq 'header = "Authorization: Bearer update-secret"' "$TEMP_DIR/asset-authenticated.stdin"
grep -Fxq 'header = "Accept: application/octet-stream"' "$TEMP_DIR/asset-authenticated.stdin"
if grep -Fq 'update-secret' "$TEMP_DIR/asset-authenticated" ||
    grep -Eq 'update-secret|github-fallback|gh-fallback' "$TEMP_DIR/asset-authenticated.env"; then
    echo "installer exposed the asset token outside curl stdin config" >&2
    exit 1
fi

if CURL_ARGS_LOG="$TEMP_DIR/asset-unsafe" PATH="$TEMP_DIR:$PATH" UPDATE_GITHUB_TOKEN="update-secret" \
    bash -c 'source <(head -n -1 "$1"); github_asset_curl "$2" "$3"' bash \
    "$ROOT_DIR/deploy/install.sh" \
    "https://github.com/h614626370-del/sub2api/releases/download/v1/asset" \
    "$TEMP_DIR/unsafe-asset" 2>/dev/null; then
    echo "installer accepted an unsafe authenticated asset URL" >&2
    exit 1
fi
test ! -e "$TEMP_DIR/asset-unsafe"

cat > "$TEMP_DIR/release.json" <<'EOF'
{
  "assets": [
    {"name": "sub2api_1.2.3_linux_amd64.tar.gz", "url": "https://api.github.com/repos/h614626370-del/sub2api/releases/assets/123"},
    {"name": "checksums.txt", "url": "https://api.github.com/repos/h614626370-del/sub2api/releases/assets/124"}
  ]
}
EOF
archive_api_url=$(bash -c 'source <(head -n -1 "$1"); release_asset_api_url "$2" "$3"' bash \
    "$ROOT_DIR/deploy/install.sh" "$TEMP_DIR/release.json" "sub2api_1.2.3_linux_amd64.tar.gz")
test "$archive_api_url" = "https://api.github.com/repos/h614626370-del/sub2api/releases/assets/123"

# Every installer release API request must use the scoped helper.
test "$(grep -c 'github_api_curl .*https://api.github.com/' "$ROOT_DIR/deploy/install.sh")" -eq 3

# Public assets remain anonymous; private assets use the scoped API helper.
grep -Fq 'curl -fsSL "$download_url"' "$ROOT_DIR/deploy/install.sh"
grep -Fq 'curl -fsSL "$checksum_url"' "$ROOT_DIR/deploy/install.sh"
grep -Fq 'github_asset_curl "$download_url"' "$ROOT_DIR/deploy/install.sh"
grep -Fq 'github_asset_curl "$checksum_url"' "$ROOT_DIR/deploy/install.sh"

# Binary installs must persist the update token for future systemd restarts.
grep -Fq 'UPDATE_ENV_FILE="$CONFIG_DIR/update.env"' "$ROOT_DIR/deploy/install.sh"
grep -Fq 'EnvironmentFile=-${UPDATE_ENV_FILE}' "$ROOT_DIR/deploy/install.sh"
grep -Fq "printf 'UPDATE_GITHUB_TOKEN=%s\\n'" "$ROOT_DIR/deploy/install.sh"
grep -Fq 'chmod 600 "$UPDATE_ENV_FILE"' "$ROOT_DIR/deploy/install.sh"
grep -Fq 'chown root:root "$UPDATE_ENV_FILE"' "$ROOT_DIR/deploy/install.sh"

echo "install GitHub token checks passed"
