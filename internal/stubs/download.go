package stubs

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// defaultRepo is the GitHub "owner/name" whose releases host the stubs. Override
// with $VAULTWRIGHT_RELEASE_REPO (e.g. for forks).
const defaultRepo = "alexey-lapin/vaultwright"

// maxStubBytes caps a downloaded stub to guard against absurd responses.
const maxStubBytes = 64 << 20 // 64 MiB

func releaseRepo() string {
	if r := os.Getenv("VAULTWRIGHT_RELEASE_REPO"); r != "" {
		return r
	}
	return defaultRepo
}

// downloadAssetName is the flattened release-asset name (matches release.yml).
func downloadAssetName(role, goos, goarch string) string {
	return role + "-" + goos + "_" + goarch + ".stub"
}

func cachePath(cacheDir, version, role, goos, goarch string) string {
	return filepath.Join(cacheDir, version, role, goos+"_"+goarch+".stub")
}

// fetchFromCacheOrDownload returns a verified stub from the local cache, else
// downloads it, verifies it against the manifest, and caches it.
func fetchFromCacheOrDownload(role, goos, goarch string, opt Options) ([]byte, error) {
	cp := cachePath(opt.CacheDir, opt.Version, role, goos, goarch)

	// Cache hit, re-verified (defends against on-disk tampering).
	if b, err := os.ReadFile(cp); err == nil {
		if verr := opt.Manifest.Verify(role, goos, goarch, b); verr == nil {
			return b, nil
		}
		// Corrupt/stale cache entry: ignore and re-fetch.
	}

	if opt.Offline {
		return nil, fmt.Errorf("stubs: %s %s/%s not embedded or cached, and offline", role, goos, goarch)
	}
	if opt.Version == "" || opt.Version == "dev" || len(opt.Manifest) == 0 {
		return nil, fmt.Errorf("stubs: cannot fetch %s %s/%s — this build has no release manifest (dev build); run `make` or pass --stub-dir", role, goos, goarch)
	}

	url := fmt.Sprintf("%s/%s/%s", opt.BaseURL, opt.Version, downloadAssetName(role, goos, goarch))
	opt.Log("downloading %s %s/%s stub (%s)...\n", role, goos, goarch, opt.Version)
	data, err := httpGet(opt.HTTPClient, url)
	if err != nil {
		return nil, err
	}
	// Trust gate: a download is only used if it matches the embedded manifest.
	if err := opt.Manifest.Verify(role, goos, goarch, data); err != nil {
		return nil, err
	}
	if err := writeCache(cp, data); err != nil {
		// Caching is best-effort; still return the verified bytes.
		_ = err
	}
	return data, nil
}

func httpGet(client *http.Client, url string) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("stubs: download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stubs: download %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxStubBytes+1))
	if err != nil {
		return nil, fmt.Errorf("stubs: reading %s: %w", url, err)
	}
	if len(data) > maxStubBytes {
		return nil, fmt.Errorf("stubs: %s exceeds %d bytes", url, maxStubBytes)
	}
	return data, nil
}

func writeCache(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
