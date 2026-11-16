#!/bin/bash
# Prepare Docker Compose files without starting services.
# Optional: SUB2API_REPOSITORY, SUB2API_REF, SUB2API_IMAGE,
# COMPOSE_PROJECT_NAME and BIND_HOST.

# Never trace generated credentials, including when invoked with bash -x.
set +x
set -euo pipefail
umask 077

SUB2API_REPOSITORY=${SUB2API_REPOSITORY:-h614626370-del/sub2api}
SUB2API_REF=${SUB2API_REF:-main}
SUB2API_IMAGE=${SUB2API_IMAGE:-ghcr.io/${SUB2API_REPOSITORY,,}:latest}
BIND_HOST=${BIND_HOST:-127.0.0.1}
COMPOSE_PROJECT_NAME=${COMPOSE_PROJECT_NAME:-}
STAGING_DIR=

fail() {
    printf '[ERROR] %s\n' "$1" >&2
    exit 1
}

cleanup() {
    if [[ -n "$STAGING_DIR" ]]; then
        rm -f "$STAGING_DIR/compose" "$STAGING_DIR/compose.normalized" \
            "$STAGING_DIR/env.example" "$STAGING_DIR/env"
        rmdir "$STAGING_DIR" 2>/dev/null || true
    fi
}
trap cleanup EXIT

command_exists() {
    command -v "$1" >/dev/null 2>&1
}

download() {
    if command_exists curl; then
        curl -q -fsSL "$1" -o "$2"
    else
        wget -q "$1" -O "$2"
    fi
}

generate_secret() {
    local value
    value=$(openssl rand -hex 32) || return 1
    [[ "$value" =~ ^[0-9a-f]{64}$ ]] || return 1
    printf '%s' "$value"
}

main() {
    [[ "$SUB2API_REPOSITORY" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || fail 'Invalid SUB2API_REPOSITORY (expected owner/repository).'
    [[ "$SUB2API_REF" =~ ^[A-Za-z0-9_][A-Za-z0-9_./-]*$ && "$SUB2API_REF" != *..* ]] || fail 'Invalid SUB2API_REF.'
    [[ "$SUB2API_IMAGE" =~ ^[a-z0-9][a-z0-9._:/@-]*$ ]] || fail 'Invalid SUB2API_IMAGE (use a lowercase registry image reference).'
    [[ "$BIND_HOST" =~ ^[A-Za-z0-9_.-]+$ || "$BIND_HOST" =~ ^\[[0-9a-fA-F:]+\]$ ]] || fail 'Invalid BIND_HOST.'
    [[ -z "$COMPOSE_PROJECT_NAME" || "$COMPOSE_PROJECT_NAME" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || fail 'Invalid COMPOSE_PROJECT_NAME.'
    command_exists openssl || fail 'Install openssl before running this script.'
    command_exists awk || fail 'Install awk before running this script.'
    if ! command_exists curl && ! command_exists wget; then
        fail 'Install curl or wget before running this script.'
    fi
    # Existing credentials and data belong to the deployment, not its template.
    for path in docker-compose.yml .env .env.example; do
        [[ ! -e "$path" && ! -L "$path" ]] || fail "$path already exists; prepare a new directory or update the existing deployment manually."
    done

    STAGING_DIR=$(mktemp -d ./.sub2api-prepare.XXXXXX)
    local raw_url="https://raw.githubusercontent.com/${SUB2API_REPOSITORY}/${SUB2API_REF}/deploy"
    printf '[INFO] Downloading deployment templates from %s at %s.\n' "$SUB2API_REPOSITORY" "$SUB2API_REF"
    download "$raw_url/docker-compose.local.yml" "$STAGING_DIR/compose"
    download "$raw_url/.env.example" "$STAGING_DIR/env.example"
    [[ -s "$STAGING_DIR/compose" && -s "$STAGING_DIR/env.example" ]] || fail 'Downloaded an empty deployment template.'

    # Keep all resources scoped by the Compose project and use this fork's image.
    awk -v image="$SUB2API_IMAGE" '
        /^  sub2api:[[:space:]]*$/ { in_app = 1; found_app++; print; next }
        in_app && /^  [A-Za-z0-9_-]+:/ { in_app = 0 }
        /^[[:space:]]+container_name:/ { next }
        in_app && /^    image:/ { print "    image: " image; found_image++; next }
        { print }
        END { if (found_app != 1 || found_image != 1) exit 1 }
    ' "$STAGING_DIR/compose" > "$STAGING_DIR/compose.normalized" || fail 'Unexpected Compose template; deployment files were not installed.'

    local jwt_secret totp_key postgres_password redis_password admin_password admin_email
    jwt_secret=$(generate_secret)
    totp_key=$(generate_secret)
    postgres_password=$(generate_secret)
    redis_password=$(generate_secret)
    admin_password=$(generate_secret)
    admin_email="admin-$(openssl rand -hex 6)@sub2api.local"
    [[ "$admin_email" =~ ^admin-[0-9a-f]{12}@sub2api.local$ ]] || fail 'Failed to generate the administrator username.'

    # Shell builtins keep secret values out of subprocess argv.
    local line
    while IFS= read -r line || [[ -n "$line" ]]; do
        case "$line" in
            JWT_SECRET=*|TOTP_ENCRYPTION_KEY=*|POSTGRES_PASSWORD=*|REDIS_PASSWORD=*|ADMIN_EMAIL=*|ADMIN_PASSWORD=*|BIND_HOST=*|COMPOSE_PROJECT_NAME=*) ;;
            *) printf '%s\n' "$line" ;;
        esac
    done < "$STAGING_DIR/env.example" > "$STAGING_DIR/env"
    {
        printf '\n# Generated for this deployment; keep this file private.\n'
        printf 'BIND_HOST=%s\n' "$BIND_HOST"
        printf 'JWT_SECRET=%s\n' "$jwt_secret"
        printf 'TOTP_ENCRYPTION_KEY=%s\n' "$totp_key"
        printf 'POSTGRES_PASSWORD=%s\n' "$postgres_password"
        printf 'REDIS_PASSWORD=%s\n' "$redis_password"
        printf 'ADMIN_EMAIL=%s\n' "$admin_email"
        printf 'ADMIN_PASSWORD=%s\n' "$admin_password"
        if [[ -n "$COMPOSE_PROJECT_NAME" ]]; then
            printf 'COMPOSE_PROJECT_NAME=%s\n' "$COMPOSE_PROJECT_NAME"
        fi
    } >> "$STAGING_DIR/env"
    chmod 600 "$STAGING_DIR/env"

    mkdir -p data postgres_data redis_data
    mv "$STAGING_DIR/compose.normalized" docker-compose.yml
    mv "$STAGING_DIR/env.example" .env.example
    mv "$STAGING_DIR/env" .env
    printf '[SUCCESS] Deployment files prepared; credentials are saved only in .env (0600).\n'
    printf '[INFO] Application bind address: %s (default port 8080).\n' "$BIND_HOST"
    printf '[INFO] Image: %s. Authenticate to its registry if pull access is required.\n' "$SUB2API_IMAGE"
    printf '[INFO] Keep a distinct COMPOSE_PROJECT_NAME for each domain deployment.\n'
    printf '[INFO] Review .env privately, then start with: docker compose up -d\n'
    printf '[INFO] Administrator login credentials are in .env; do not publish them or collect them from logs.\n'
}

main "$@"
