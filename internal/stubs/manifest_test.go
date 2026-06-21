package stubs

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestParseAndVerify(t *testing.T) {
	data := []byte("pretend stub bytes")
	sum := sha256.Sum256(data)
	hexsum := hex.EncodeToString(sum[:])

	raw := "# comment\n\n" +
		hexsum + "  stubs/vault/linux_amd64.stub\n" +
		"deadbeef" + // too short, ignored
		"\n"
	m := ParseManifest([]byte(raw))

	if got, ok := m.Expected("vault", "linux", "amd64"); !ok || got != hexsum {
		t.Fatalf("Expected = %q,%v want %q,true", got, ok, hexsum)
	}
	if err := m.Verify("vault", "linux", "amd64", data); err != nil {
		t.Fatalf("Verify good data: %v", err)
	}
	if err := m.Verify("vault", "linux", "amd64", []byte("tampered")); err == nil {
		t.Fatal("expected hash mismatch error")
	}
	if err := m.Verify("warden", "linux", "amd64", data); err == nil {
		t.Fatal("expected missing-entry error for unlisted stub")
	}
}

func TestParseToleratesStarAndSingleSpace(t *testing.T) {
	sum := sha256.Sum256([]byte("x"))
	h := hex.EncodeToString(sum[:])
	m := ParseManifest([]byte(h + " *stubs/warden/windows_amd64.stub\n"))
	if got, ok := m.Expected("warden", "windows", "amd64"); !ok || got != h {
		t.Fatalf("Expected = %q,%v", got, ok)
	}
}

func TestEmptyManifestVerifiesNothing(t *testing.T) {
	m := ParseManifest([]byte("# only comments\n"))
	if len(m) != 0 {
		t.Fatalf("expected empty manifest, got %d", len(m))
	}
	if err := m.Verify("vault", "darwin", "arm64", []byte("anything")); err == nil {
		t.Fatal("empty manifest must reject (no entry)")
	}
}
