// Package stubs resolves a vault/warden stub for a target platform, in priority
// order: an explicit stub directory, the stubs embedded in vaultwright, then (added
// in a later slice) the local cache and an on-demand download verified against the
// embedded SHA-256 manifest.
package stubs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alexey-lapin/vaultwright/internal/builtin"
)

// Roles.
const (
	RoleVault  = "vault"
	RoleWarden = "warden"
)

// Options controls resolution sources.
type Options struct {
	// StubDir, if set, is checked first: <StubDir>/<role>/<os>_<arch>.stub.
	StubDir string
}

// Resolve returns the stub bytes for role and the GOOS/GOARCH target.
func Resolve(role, goos, goarch string, opt Options) ([]byte, error) {
	name := goos + "_" + goarch + ".stub"

	// 1. Explicit stub directory (local mirror / air-gap).
	if opt.StubDir != "" {
		p := filepath.Join(opt.StubDir, role, name)
		if b, err := os.ReadFile(p); err == nil {
			return b, nil
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stubs: reading %s: %w", p, err)
		}
	}

	// 2. Embedded in this vaultwright build.
	if b, ok := builtin.EmbeddedStub(role, goos, goarch); ok {
		if builtin.IsPlaceholder(b) {
			return nil, fmt.Errorf("stubs: %s/%s/%s is a placeholder — run `make` to build it", role, goos, goarch)
		}
		return b, nil
	}

	// 3. TODO(§13): local cache, then download + verify against the manifest.
	return nil, fmt.Errorf("stubs: no stub for %s %s/%s (not embedded; --stub-dir or `fetch-stubs` not yet implemented)", role, goos, goarch)
}
