//go:build embed_stubs && darwin && amd64

package builtin

import _ "embed"

// Host stubs for darwin/amd64, compiled in only under the "embed_stubs" build tag (set by
// `make` and the release build). Without the tag, stub_fallback.go supplies empty stubs so
// no stub files need exist; a tagged build embeds solely this platform's stubs.

//go:embed stubs/vault/darwin_amd64.stub
var vaultStub []byte

//go:embed stubs/warden/darwin_amd64.stub
var wardenStub []byte
