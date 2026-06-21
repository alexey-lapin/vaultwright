#!/usr/bin/env bash
# Build the vaultwright CLI for each host platform, embedding that host's stubs and
# the full SHA-256 manifest, plus the version. Run scripts/build-stubs.sh first.
#
#   VERSION=v1.2.3 ./scripts/build-release.sh
#
# Outputs dist/vaultwright-<os>-<arch>[.exe]. Restores the committed builtin/ layout
# on exit so the working tree isn't left dirty.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=${VERSION:?set VERSION to the release tag, e.g. v1.2.3}
CLI_TARGETS=${CLI_TARGETS:-"darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64"}
OUT=${OUT:-dist}
VERPKG="github.com/alexey-lapin/vaultwright/internal/builtin"

test -f "$OUT/SHA256SUMS" || { echo "missing $OUT/SHA256SUMS — run scripts/build-stubs.sh first" >&2; exit 1; }

restore() { git checkout -- internal/builtin/stubs internal/builtin/SHA256SUMS 2>/dev/null || true; }
trap restore EXIT

for t in $CLI_TARGETS; do
  os=${t%/*}; arch=${t#*/}; host="${os}_${arch}"
  # Embed ONLY this host's stubs (so non-host targets fall through to download,
  # rather than hitting an embedded placeholder) plus the manifest.
  rm -rf internal/builtin/stubs
  mkdir -p internal/builtin/stubs/vault internal/builtin/stubs/warden
  cp "$OUT/stubs/vault/$host.stub"  internal/builtin/stubs/vault/
  cp "$OUT/stubs/warden/$host.stub" internal/builtin/stubs/warden/
  cp "$OUT/SHA256SUMS" internal/builtin/SHA256SUMS

  bin="$OUT/vaultwright-${os}-${arch}"
  [ "$os" = windows ] && bin="$bin.exe"
  GOOS=$os GOARCH=$arch go build -trimpath \
    -ldflags "-s -w -X $VERPKG.Version=$VERSION" \
    -o "$bin" ./cmd/vaultwright
  echo "built $bin"
done
