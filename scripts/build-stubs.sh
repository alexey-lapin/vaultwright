#!/usr/bin/env bash
# Cross-compile the vault/warden stub matrix and emit a SHA-256 manifest.
#
# Used by the release workflow and runnable locally. Outputs:
#   dist/stubs/<role>/<os>_<arch>.stub
#   dist/SHA256SUMS   (sha256sum format: "<hash>  stubs/<role>/<os>_<arch>.stub")
#
# Override the matrices via env, e.g.:
#   VAULT_TARGETS="linux/amd64 windows/amd64" ./scripts/build-stubs.sh
set -euo pipefail
cd "$(dirname "$0")/.."

VAULT_TARGETS=${VAULT_TARGETS:-"darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64"}
WARDEN_TARGETS=${WARDEN_TARGETS:-"darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64"}
OUT=${OUT:-dist}
LDFLAGS=${LDFLAGS:-"-s -w"}

# sha256 one file → standard "<hash>  <path>" line. Hashing a single file keeps the
# format identical across macOS/Linux (multi-file mode can switch to BSD tag format).
sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1"; else shasum -a 256 "$1"; fi
}

build_role() {
  role=$1 cmd=$2
  shift 2
  for t in "$@"; do
    os=${t%/*} arch=${t#*/}
    out="$OUT/stubs/$role/${os}_${arch}.stub"
    mkdir -p "$(dirname "$out")"
    GOOS=$os GOARCH=$arch go build -trimpath -ldflags "$LDFLAGS" -o "$out" "./cmd/$cmd"
    echo "built $out"
  done
}

rm -rf "$OUT/stubs"
mkdir -p "$OUT/stubs"
build_role vault vault $VAULT_TARGETS
build_role warden warden $WARDEN_TARGETS

# Manifest with paths relative to dist/ so verification is location-independent.
# Hash per file (sorted) for a deterministic, host-independent "<hash>  <path>" format.
(
  cd "$OUT"
  : > SHA256SUMS
  find stubs -type f -name '*.stub' | LC_ALL=C sort | while IFS= read -r f; do
    sha256 "$f" >> SHA256SUMS
  done
)
echo "wrote $OUT/SHA256SUMS"
