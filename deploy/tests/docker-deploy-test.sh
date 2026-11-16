#!/bin/bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
TEMP_DIR=$(mktemp -d)
cleanup() {
    # Only remove the absolute temporary directory created by this test.
    case "$TEMP_DIR" in "${TMPDIR:-/tmp}"/*|/tmp/*) rm -rf -- "$TEMP_DIR" ;; esac
}
trap cleanup EXIT
mkdir -p "$TEMP_DIR/bin" "$TEMP_DIR/fixtures"
cat > "$TEMP_DIR/fixtures/compose" <<'EOF'
services:
  sub2api:
    image: ghcr.io/ranxi2001/sub2api:latest
    container_name: sub2api
    ports:
      - "${BIND_HOST:-0.0.0.0}:${SERVER_PORT:-8080}:8080"
  postgres:
    image: postgres:18-alpine
    container_name: sub2api-postgres
  redis:
    image: redis:8-alpine
    container_name: sub2api-redis
EOF
cat > "$TEMP_DIR/fixtures/env" <<'EOF'
BIND_HOST=0.0.0.0
JWT_SECRET=
TOTP_ENCRYPTION_KEY=
POSTGRES_PASSWORD=old-placeholder
REDIS_PASSWORD=
ADMIN_EMAIL=
ADMIN_PASSWORD=
SERVER_PORT=8080
EOF
cat > "$TEMP_DIR/bin/curl" <<'EOF'
#!/bin/bash
set -eu
url= output=
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o) output=$2; shift 2 ;;
        https://*) url=$1; shift ;;
        *) shift ;;
    esac
done
printf '%s\n' "$url" >> "$DOWNLOAD_LOG"
[ "${FAIL_DOWNLOAD:-0}" != 1 ] || exit 22
case "$url" in
    */docker-compose.local.yml) cp "$FIXTURE_DIR/compose" "$output" ;;
    */.env.example) [ "${FAIL_ENV_DOWNLOAD:-0}" != 1 ] || exit 22; cp "$FIXTURE_DIR/env" "$output" ;;
    *) exit 23 ;;
esac
EOF
chmod +x "$TEMP_DIR/bin/curl"
export PATH="$TEMP_DIR/bin:$PATH"
export FIXTURE_DIR="$TEMP_DIR/fixtures"
export DOWNLOAD_LOG="$TEMP_DIR/downloads"
unset SUB2API_REPOSITORY SUB2API_REF SUB2API_IMAGE BIND_HOST COMPOSE_PROJECT_NAME

run_case() {
    local name=$1
    shift
    mkdir "$TEMP_DIR/$name"
    (cd "$TEMP_DIR/$name" && "$@" bash "$ROOT_DIR/deploy/docker-deploy.sh") > "$TEMP_DIR/$name.log" 2>&1
}

run_case default env
grep -Fxq 'https://raw.githubusercontent.com/h614626370-del/sub2api/main/deploy/docker-compose.local.yml' "$DOWNLOAD_LOG"
grep -Fxq '    image: ghcr.io/h614626370-del/sub2api:latest' "$TEMP_DIR/default/docker-compose.yml"
! grep -q 'container_name:' "$TEMP_DIR/default/docker-compose.yml"
grep -Fxq '    image: postgres:18-alpine' "$TEMP_DIR/default/docker-compose.yml"
grep -Fxq 'BIND_HOST=127.0.0.1' "$TEMP_DIR/default/.env"
for key in JWT_SECRET TOTP_ENCRYPTION_KEY POSTGRES_PASSWORD REDIS_PASSWORD ADMIN_PASSWORD; do
    value=$(sed -n "s/^$key=//p" "$TEMP_DIR/default/.env")
    [[ "$value" =~ ^[a-f0-9]{64}$ ]]
    ! grep -Fq "$value" "$TEMP_DIR/default.log"
    test "$(grep -c "^$key=" "$TEMP_DIR/default/.env")" -eq 1
done
grep -Eq '^ADMIN_EMAIL=admin-[a-f0-9]{12}@sub2api.local$' "$TEMP_DIR/default/.env"
test -d "$TEMP_DIR/default/data"
test -d "$TEMP_DIR/default/postgres_data"
test -d "$TEMP_DIR/default/redis_data"
# Windows ACL-backed mounts cannot represent Unix mode bits reliably.
case "$(uname -s)" in
    MINGW*|MSYS*) ;;
    *) test "$(stat -c '%a' "$TEMP_DIR/default/.env" 2>/dev/null || stat -f '%Lp' "$TEMP_DIR/default/.env")" = 600 ;;
esac

run_case custom env SUB2API_REPOSITORY=example/project SUB2API_REF=v1.2.3 SUB2API_IMAGE=registry.example/project:1.2.3 COMPOSE_PROJECT_NAME=aiziyou-sub2api BIND_HOST=127.0.0.2
grep -Fxq 'https://raw.githubusercontent.com/example/project/v1.2.3/deploy/docker-compose.local.yml' "$DOWNLOAD_LOG"
grep -Fxq '    image: registry.example/project:1.2.3' "$TEMP_DIR/custom/docker-compose.yml"
grep -Fxq 'COMPOSE_PROJECT_NAME=aiziyou-sub2api' "$TEMP_DIR/custom/.env"
grep -Fxq 'BIND_HOST=127.0.0.2' "$TEMP_DIR/custom/.env"

before=$(cksum "$TEMP_DIR/default/.env")
if (cd "$TEMP_DIR/default" && bash "$ROOT_DIR/deploy/docker-deploy.sh") > "$TEMP_DIR/existing.log" 2>&1; then
    echo 'existing deployment was overwritten' >&2; exit 1
fi
test "$before" = "$(cksum "$TEMP_DIR/default/.env")"

if run_case invalid env SUB2API_REF='../main'; then echo 'invalid ref accepted' >&2; exit 1; fi
test ! -e "$TEMP_DIR/invalid/.env"
if run_case failed env FAIL_DOWNLOAD=1; then echo 'failed download accepted' >&2; exit 1; fi
test ! -e "$TEMP_DIR/failed/.env"
test ! -e "$TEMP_DIR/failed/docker-compose.yml"
test -z "$(find "$TEMP_DIR/failed" -mindepth 1 -print -quit)"
if run_case partial env FAIL_ENV_DOWNLOAD=1; then echo 'partial download accepted' >&2; exit 1; fi
test -z "$(find "$TEMP_DIR/partial" -mindepth 1 -print -quit)"

printf 'not a compose document\n' > "$FIXTURE_DIR/compose"
if run_case malformed env; then echo 'malformed compose accepted' >&2; exit 1; fi
test ! -e "$TEMP_DIR/malformed/.env"
test ! -e "$TEMP_DIR/malformed/docker-compose.yml"

printf 'docker deployment preparation checks passed\n'
