#!/usr/bin/env bash
# Stage the extra release assets GoReleaser uploads via `release.extra_files`.
#
# The stub downloader (internal/stubs/download.go) fetches each stub as the flat asset
# name "<role>-<os>_<arch>.stub"; extra_files uploads by basename and can't rename, and
# the per-role stub paths (vault/<os>_<arch>.stub, warden/<os>_<arch>.stub) share a
# basename. So flatten them here into build/assets/ alongside the SHA-256 trust root.
#
# Run after scripts/build-stubs.sh (needs build/stubs/** and build/SHA256SUMS).
set -euo pipefail
cd "$(dirname "$0")/.."

OUT=${OUT:-build}
test -f "$OUT/SHA256SUMS" || { echo "missing $OUT/SHA256SUMS — run scripts/build-stubs.sh first" >&2; exit 1; }

rm -rf "$OUT/assets"
mkdir -p "$OUT/assets"
cp "$OUT/SHA256SUMS" "$OUT/assets/"

while IFS= read -r f; do
  rel=${f#"$OUT"/stubs/}                 # e.g. vault/darwin_arm64.stub
  cp "$f" "$OUT/assets/${rel/\//-}"      # → vault-darwin_arm64.stub
done < <(find "$OUT/stubs" -type f -name '*.stub' | LC_ALL=C sort)

echo "staged $(find "$OUT/assets" -type f | wc -l | tr -d ' ') files into $OUT/assets"
