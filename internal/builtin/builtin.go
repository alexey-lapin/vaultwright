// Package builtin embeds the build-time assets that only vaultwright needs: the
// BIP39 wordlist, the host's precompiled vault/warden stubs, and the SHA-256 manifest
// used to verify downloaded stubs. It is imported solely by vaultwright, so the wordlist
// never ends up in a distributed vault/warden binary.
//
// The host stubs are embedded via per-platform, build-tagged files (stub_<os>_<arch>.go),
// so a binary compiled for a given GOOS/GOARCH embeds only that platform's stubs — no
// build-time mutation of the stubs directory is needed. Every target's stub file under
// stubs/ is a placeholder in source control; `make` overwrites the host-platform ones
// with real compiled binaries. Non-host stubs are fetched on demand (see internal/stubs).
package builtin

import (
	"bytes"
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

// placeholderPrefix marks a committed stub that has not been compiled yet.
var placeholderPrefix = []byte("placeholder")

// EmbeddedStub returns the embedded stub bytes for role ("vault"/"warden") and the given
// GOOS/GOARCH, and whether one is embedded. Only the host platform's stubs are embedded
// (vaultStub/wardenStub come from the build-tagged stub_<os>_<arch>.go for this build), so
// any non-host target reports false and is resolved via download.
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

// IsPlaceholder reports whether stub bytes are an uncompiled placeholder.
func IsPlaceholder(b []byte) bool {
	return bytes.HasPrefix(b, placeholderPrefix)
}
