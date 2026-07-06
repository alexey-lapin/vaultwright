// Package builtin embeds the build-time assets that only vaultwright needs: the
// BIP39 wordlist, the host's precompiled vault/warden stubs, and the SHA-256 manifest
// used to verify downloaded stubs. It is imported solely by vaultwright, so the wordlist
// never ends up in a distributed vault/warden binary.
//
// The host stubs are embedded only under the "embed_stubs" build tag, via per-platform
// files (stub_<os>_<arch>.go), so a tagged binary embeds solely its own GOOS/GOARCH stubs.
// A plain `go build` (no tag) embeds nothing and needs no stub files present — stubs/ is
// git-ignored and populated by `make` / the release build. Non-host (and untagged) targets
// are fetched on demand (see internal/stubs).
package builtin

import (
	_ "embed"
	"runtime"
)

//go:embed english.txt
var Wordlist []byte

// manifestBytes is the SHA-256 manifest of release stubs (one "<hash>  <path>" line
// per stub). A placeholder in source control; the real one is embedded at release.
//
//go:embed SHA256SUMS
var manifestBytes []byte

// Manifest returns the embedded SHA-256 stub manifest.
func Manifest() []byte { return manifestBytes }

// Version is the release version, set via
// -ldflags "-X github.com/alexey-lapin/vaultwright/internal/builtin.Version=vX.Y.Z".
var Version = "dev"

// EmbeddedStub returns the embedded stub bytes for role ("vault"/"warden") and the given
// GOOS/GOARCH, and whether one is embedded. Only the host platform's stubs are ever
// embedded, and only in an "embed_stubs" build (vaultStub/wardenStub are empty otherwise),
// so any non-host or untagged target reports false and is resolved via download.
func EmbeddedStub(role, goos, goarch string) ([]byte, bool) {
	if goos != runtime.GOOS || goarch != runtime.GOARCH {
		return nil, false
	}
	switch role {
	case "vault":
		return vaultStub, len(vaultStub) > 0
	case "warden":
		return wardenStub, len(wardenStub) > 0
	}
	return nil, false
}
