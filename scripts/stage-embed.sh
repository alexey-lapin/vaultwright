#!/usr/bin/env bash
# GoReleaser before-hook: stage ALL freshly built stubs + the SHA-256 manifest into
# internal/builtin/ so the native builds embed the real host stubs. Each target's build
# picks only its own stubs via the build tags in stub_<os>_<arch>.go, so nothing is
# mutated per-target and builds can run in parallel.
#
# Requires scripts/build-stubs.sh to have produced build/stubs/** + build/SHA256SUMS.
# Overwrites the committed placeholders; restore the tree afterward with:
#   git checkout -- internal/builtin/stubs internal/builtin/SHA256SUMS
set -euo pipefail
cd "$(dirname "$0")/.."

OUT=${OUT:-build}
test -f "$OUT/SHA256SUMS" || { echo "missing $OUT/SHA256SUMS — run scripts/build-stubs.sh first" >&2; exit 1; }

cp "$OUT/SHA256SUMS" internal/builtin/SHA256SUMS
for role in vault warden; do
  mkdir -p "internal/builtin/stubs/$role"   # stubs/ is git-ignored, may not exist yet
  for f in "$OUT/stubs/$role/"*.stub; do
    cp "$f" "internal/builtin/stubs/$role/$(basename "$f")"
  done
done
echo "staged $(find "$OUT/stubs" -name '*.stub' | wc -l | tr -d ' ') stubs + manifest into internal/builtin/"
