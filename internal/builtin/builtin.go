// Package builtin embeds the build-time assets that only vaultwright needs: the
// BIP39 wordlist, a registry of precompiled vault/warden stubs (keyed by
// role/os_arch), and the SHA-256 manifest used to verify downloaded stubs. It is
// imported solely by vaultwright, so the wordlist never ends up in a distributed
// vault/warden binary.
//
// Stub files under stubs/ are placeholders in source control; `make` overwrites
// the host-platform ones with real compiled binaries before building vaultwright.
// Non-host stubs are fetched on demand (see internal/stubs).
package builtin

import (
	"bytes"
	"embed"
)

//go:embed english.txt
var Wordlist []byte

// stubsFS holds whatever stubs were present at build time, as stubs/<role>/<os>_<arch>.stub.
//
//go:embed stubs
var stubsFS embed.FS

// Version is the release version, set via
// -ldflags "-X github.com/alexey-lapin/vaultwright/internal/builtin.Version=vX.Y.Z".
var Version = "dev"

// placeholderPrefix marks a committed stub that has not been compiled yet.
var placeholderPrefix = []byte("placeholder")

// EmbeddedStub returns the embedded stub bytes for role ("vault"/"warden") and
// the given GOOS/GOARCH, and whether one is embedded at all.
func EmbeddedStub(role, goos, goarch string) ([]byte, bool) {
	b, err := stubsFS.ReadFile(stubPath(role, goos, goarch))
	if err != nil {
		return nil, false
	}
	return b, true
}

// IsPlaceholder reports whether stub bytes are an uncompiled placeholder.
func IsPlaceholder(b []byte) bool {
	return bytes.HasPrefix(b, placeholderPrefix)
}

func stubPath(role, goos, goarch string) string {
	return "stubs/" + role + "/" + goos + "_" + goarch + ".stub"
}
