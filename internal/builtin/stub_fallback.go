//go:build !embed_stubs || !((darwin || linux || windows) && (amd64 || arm64))

package builtin

// No stubs to embed — the case complementary to the six stub_<os>_<arch>.go files, i.e.
// either the "embed_stubs" tag is absent (a plain go build/test, so no stub files need to
// exist) or it's set for a platform outside the release matrix. EmbeddedStub then reports
// "not embedded" for every target and the CLI resolves stubs via download.

var (
	vaultStub  []byte
	wardenStub []byte
)
