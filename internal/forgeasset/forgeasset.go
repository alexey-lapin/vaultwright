// Package forgeasset embeds the build-time assets that only forge needs: the
// BIP39 wordlist and the precompiled vault/warden stubs. It is imported solely by
// forge, so the wordlist never ends up in a distributed vault/warden binary.
//
// The .stub files are placeholders in source control; `make` overwrites them with
// real compiled binaries before building forge.
package forgeasset

import _ "embed"

//go:embed english.txt
var Wordlist []byte

//go:embed vault.stub
var VaultStub []byte

//go:embed warden.stub
var WardenStub []byte

// minStubSize guards against building forge with placeholder stubs.
const minStubSize = 200_000

// StubsBuilt reports whether the embedded stubs look like real binaries.
func StubsBuilt() bool {
	return len(VaultStub) >= minStubSize && len(WardenStub) >= minStubSize
}
