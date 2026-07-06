//go:build embed_stubs && windows && arm64

package builtin

import _ "embed"

// Host stubs for windows/arm64, compiled in only under the "embed_stubs" build tag (set by
// `make` and the release build). Without the tag, stub_fallback.go supplies empty stubs so
// no stub files need exist; a tagged build embeds solely this platform's stubs.

//go:embed stubs/vault/windows_arm64.stub
var vaultStub []byte

//go:embed stubs/warden/windows_arm64.stub
var wardenStub []byte
