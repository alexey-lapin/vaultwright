//go:build windows && arm64

package builtin

import _ "embed"

// Host stubs for windows/arm64. The build tag ensures only this platform's stubs are
// compiled into the binary, so each CLI embeds solely its own host stubs — no build-time
// mutation of the stubs directory (see EmbeddedStub in builtin.go).

//go:embed stubs/vault/windows_arm64.stub
var vaultStub []byte

//go:embed stubs/warden/windows_arm64.stub
var wardenStub []byte
