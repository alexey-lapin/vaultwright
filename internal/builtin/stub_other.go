//go:build !(darwin || linux || windows) || !(amd64 || arm64)

package builtin

// Fallback for platforms outside the release matrix ({darwin,linux,windows} ×
// {amd64,arm64}). No stubs are embedded, so EmbeddedStub always reports "not embedded"
// and such a host resolves every stub via download. Keeps `go build` working anywhere.

var (
	vaultStub  []byte
	wardenStub []byte
)
