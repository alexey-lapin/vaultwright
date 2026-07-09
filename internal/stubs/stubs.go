// Package stubs resolves a vault/warden stub for a target platform, in priority
// order: an explicit stub directory, the stubs embedded in vaultwright, the local
// cache, then an on-demand download verified against the embedded SHA-256 manifest
// (the trust root). Unverifiable stubs are never used.
package stubs

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/alexey-lapin/vaultwright/internal/builtin"
)

// Roles.
const (
	RoleVault  = "vault"
	RoleWarden = "warden"
)

// Options controls resolution sources. Zero-valued fields are filled from the
// vaultwright build (embedded manifest/version, default cache dir and release URL);
// tests override them.
type Options struct {
	StubDir    string       // checked first: <StubDir>/<role>/<os>_<arch>.stub
	Offline    bool         // disable network download
	Version    string       // default: builtin.Version
	Manifest   Manifest     // default: parsed builtin.Manifest()
	BaseURL    string       // default: https://github.com/<repo>/releases/download
	CacheDir   string       // default: <user cache>/vaultwright/stubs
	HTTPClient *http.Client // default: 60s-timeout client
}

func (o *Options) fillDefaults() error {
	if o.Version == "" {
		o.Version = builtin.Version
	}
	if o.Manifest == nil {
		o.Manifest = ParseManifest(builtin.Manifest())
	}
	if o.BaseURL == "" {
		o.BaseURL = "https://github.com/" + releaseRepo() + "/releases/download"
	}
	if o.CacheDir == "" {
		dir, err := DefaultCacheDir()
		if err != nil {
			return err
		}
		o.CacheDir = dir
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	return nil
}

// DefaultCacheDir returns the default download cache directory:
// <user cache>/vaultwright/stubs.
func DefaultCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("stubs: locating cache dir: %w", err)
	}
	return filepath.Join(base, "vaultwright", "stubs"), nil
}

// Resolve returns the stub bytes for role and the GOOS/GOARCH target.
func Resolve(role, goos, goarch string, opt Options) ([]byte, error) {
	if err := opt.fillDefaults(); err != nil {
		return nil, err
	}

	// 1. Explicit stub directory (local mirror / air-gap).
	if opt.StubDir != "" {
		p := filepath.Join(opt.StubDir, role, goos+"_"+goarch+".stub")
		if b, err := os.ReadFile(p); err == nil {
			return b, nil
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stubs: reading %s: %w", p, err)
		}
	}

	// 2. Embedded in this (trusted) vaultwright build (host only, "embed_stubs" builds).
	if b, ok := builtin.EmbeddedStub(role, goos, goarch); ok {
		return b, nil
	}

	// 3 & 4. Local cache, else download — both verified against the manifest.
	return fetchFromCacheOrDownload(role, goos, goarch, opt)
}
