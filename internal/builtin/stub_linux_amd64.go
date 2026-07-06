//go:build embed_stubs && linux && amd64

package builtin

import _ "embed"

// Host stubs for linux/amd64, compiled in only under the "embed_stubs" build tag (set by
// `make` and the release build). Without the tag, stub_fallback.go supplies empty stubs so
// no stub files need exist; a tagged build embeds solely this platform's stubs.

//go:embed stubs/vault/linux_amd64.stub
var vaultStub []byte

//go:embed stubs/warden/linux_amd64.stub
var wardenStub []byte
