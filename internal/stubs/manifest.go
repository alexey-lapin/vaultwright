package stubs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Manifest maps a stub asset path ("stubs/<role>/<os>_<arch>.stub") to its expected
// lowercase hex SHA-256. It is the trust root for on-demand stub downloads.
type Manifest map[string]string

// ParseManifest reads sha256sum-format lines ("<hex>  <path>"); blank and
// '#'-comment lines are ignored.
func ParseManifest(b []byte) Manifest {
	m := Manifest{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// "<hex>  <path>" — split on the first run of spaces.
		fields := strings.SplitN(line, "  ", 2)
		if len(fields) != 2 {
			fields = strings.SplitN(line, " ", 2) // tolerate single space
			if len(fields) != 2 {
				continue
			}
		}
		hash := strings.ToLower(strings.TrimSpace(fields[0]))
		path := strings.TrimSpace(strings.TrimPrefix(fields[1], "*")) // sha256sum binary marker
		if len(hash) == 64 && path != "" {
			m[path] = hash
		}
	}
	return m
}

// assetPath is the manifest key for a stub.
func assetPath(role, goos, goarch string) string {
	return "stubs/" + role + "/" + goos + "_" + goarch + ".stub"
}

// Expected returns the expected hash for a stub, if the manifest lists it.
func (m Manifest) Expected(role, goos, goarch string) (string, bool) {
	h, ok := m[assetPath(role, goos, goarch)]
	return h, ok
}

// Verify checks data against the manifest entry for the given stub. A missing
// entry is an error: unlisted stubs are never trusted.
func (m Manifest) Verify(role, goos, goarch string, data []byte) error {
	want, ok := m.Expected(role, goos, goarch)
	if !ok {
		return fmt.Errorf("stubs: no manifest entry for %s %s/%s", role, goos, goarch)
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("stubs: %s %s/%s hash mismatch (want %s, got %s)", role, goos, goarch, want, got)
	}
	return nil
}
