package blob

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSealReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "stub")
	if err := os.WriteFile(stub, []byte("PRETEND-BINARY-CONTENTS"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("opaque appended payload \x00\x01\x02")
	sealed := filepath.Join(dir, "sealed")
	if err := WriteSealed(stub, sealed, payload); err != nil {
		t.Fatal(err)
	}
	got, err := Read(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: got %q", got)
	}
}

func TestUnsealedBinary(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "plain")
	os.WriteFile(p, []byte("just a normal binary with no payload"), 0o755)
	if _, err := Read(p); err != ErrNotSealed {
		t.Fatalf("got %v, want ErrNotSealed", err)
	}
}

func TestVaultPayloadFraming(t *testing.T) {
	in := VaultPayload{
		Salt:    bytes.Repeat([]byte{0xAB}, 16),
		MetaCT:  []byte("meta-ciphertext"),
		AssetCT: []byte("asset-ciphertext-which-is-longer"),
	}
	out, err := ParseVaultPayload(in.Marshal(), 16)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Salt, in.Salt) || !bytes.Equal(out.MetaCT, in.MetaCT) || !bytes.Equal(out.AssetCT, in.AssetCT) {
		t.Fatalf("vault payload round-trip mismatch: %+v", out)
	}
}

func TestWardenPayloadFraming(t *testing.T) {
	for _, hasPass := range []bool{false, true} {
		in := WardenPayload{
			HasPass: hasPass,
			Salt:    bytes.Repeat([]byte{0x11}, 16),
			CT:      []byte("warden-ciphertext"),
		}
		out, err := ParseWardenPayload(in.Marshal(), 16)
		if err != nil {
			t.Fatal(err)
		}
		if out.HasPass != in.HasPass || !bytes.Equal(out.Salt, in.Salt) || !bytes.Equal(out.CT, in.CT) {
			t.Fatalf("warden payload round-trip mismatch (hasPass=%v): %+v", hasPass, out)
		}
	}
}
