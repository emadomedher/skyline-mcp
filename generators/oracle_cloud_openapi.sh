#!/usr/bin/env bash
set -euo pipefail

OUT_FILE="oci_spec_pages.txt"

echo "🔍 Locating OCI CLI installation..."

OCI_PYTHON=$(python3 - <<'EOF'
import oci, os
print(os.path.dirname(oci.__file__))
EOF
)

SERVICES_DIR="$OCI_PYTHON/services"

if [[ ! -d "$SERVICES_DIR" ]]; then
  echo "❌ OCI services directory not found"
  exit 1
fi

echo "📦 Reading services from: $SERVICES_DIR"

SERVICES=$(ls "$SERVICES_DIR" \
  | grep -vE '^(__init__|base|models)$' \
  | sort -u)

if [[ -z "$SERVICES" ]]; then
  echo "❌ No services found (this should not happen)"
  exit 1
fi

echo "📊 Found $(echo "$SERVICES" | wc -l | tr -d ' ') services"

: > "$OUT_FILE"
for svc in $SERVICES; do
  echo "https://docs.oracle.com/en-us/iaas/api/#/$svc" >> "$OUT_FILE"
done

echo "✅ Written $OUT_FILE"
echo "Preview:"
head -n 10 "$OUT_FILE"