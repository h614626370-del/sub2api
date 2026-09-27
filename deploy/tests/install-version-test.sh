#!/bin/bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

cat > "$TEMP_DIR/sub2api" <<'EOF'
#!/bin/bash
printf 'Sub2API %s (commit: fixture)\n' "$TEST_VERSION"
EOF
chmod +x "$TEMP_DIR/sub2api"

for version in 0.2.8 v0.2.8 0.2.8.0 v0.2.8.0 0.2.8.1 0.2.8.10; do
    actual=$(TEST_VERSION="$version" bash -c '
        source <(head -n -1 "$1")
        INSTALL_DIR="$2"
        get_current_version
    ' bash "$ROOT_DIR/deploy/install.sh" "$TEMP_DIR")
    if [[ "$actual" != "$version" ]]; then
        printf 'expected %s, got %s\n' "$version" "$actual" >&2
        exit 1
    fi
done

grep -Fq 'CURRENT_VERSION=$(get_current_version)' "$ROOT_DIR/deploy/install.sh"
echo "installer version tests passed"
