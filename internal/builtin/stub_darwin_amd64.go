//go:build darwin && amd64

package builtin

import _ "embed"

// Host stubs for darwin/amd64. The build tag ensures only this platform's stubs are
// compiled into the binary, so each CLI embeds solely its own host stubs — no build-time
// mutation of the stubs directory (see EmbeddedStub in builtin.go).

//go:embed stubs/vault/darwin_amd64.stub
var vaultStub []byte

//go:embed stubs/warden/darwin_amd64.stub
var wardenStub []byte
