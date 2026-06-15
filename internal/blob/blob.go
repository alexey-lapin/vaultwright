// Package blob handles the on-disk container: appending an opaque payload to a
// stub binary and reading it back from the running executable, plus the byte
// framing of the vault and warden payloads. It performs no cryptography itself —
// callers encrypt the fields with cryptocore before framing them here.
//
// A sealed binary is: <stub bytes> <payload> <uint64 big-endian payload length>.
// The only structural tell is the 8-byte trailer; there is no magic header.
package blob

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const trailerLen = 8

// ErrNotSealed means the executable has no appended payload.
var ErrNotSealed = errors.New("blob: binary is not sealed")

// WriteSealed copies the stub at stubPath to dstPath and appends payload plus a
// length trailer. dstPath is made executable.
func WriteSealed(stubPath, dstPath string, payload []byte) error {
	stub, err := os.ReadFile(stubPath)
	if err != nil {
		return fmt.Errorf("blob: read stub: %w", err)
	}
	return WriteSealedBytes(stub, dstPath, payload)
}

// WriteSealedBytes appends payload plus a length trailer to the in-memory stub
// and writes the result to dstPath as an executable.
func WriteSealedBytes(stub []byte, dstPath string, payload []byte) error {
	out := make([]byte, 0, len(stub)+len(payload)+trailerLen)
	out = append(out, stub...)
	out = append(out, payload...)
	var trailer [trailerLen]byte
	binary.BigEndian.PutUint64(trailer[:], uint64(len(payload)))
	out = append(out, trailer[:]...)
	return os.WriteFile(dstPath, out, 0o755)
}

// ReadSelf returns the payload appended to the running executable.
func ReadSelf() ([]byte, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return Read(exe)
}

// Read returns the payload appended to the binary at path.
func Read(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < trailerLen {
		return nil, ErrNotSealed
	}
	n := binary.BigEndian.Uint64(data[len(data)-trailerLen:])
	if n == 0 || n > uint64(len(data)-trailerLen) {
		return nil, ErrNotSealed
	}
	start := len(data) - trailerLen - int(n)
	return data[start : len(data)-trailerLen], nil
}

// --- Vault payload framing ---------------------------------------------------

// VaultPayload is what gets appended to the vault stub. Salt is plaintext; the
// other two fields are ciphertext produced by the caller.
type VaultPayload struct {
	Salt    []byte // SaltLen bytes, plaintext
	MetaCT  []byte // AEAD(P, {pk, wordlist})
	AssetCT []byte // AEAD(K_a, archived files)
}

// Marshal frames the payload as: salt | uint32(len meta) | meta | asset.
func (p VaultPayload) Marshal() []byte {
	out := make([]byte, 0, len(p.Salt)+4+len(p.MetaCT)+len(p.AssetCT))
	out = append(out, p.Salt...)
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(p.MetaCT)))
	out = append(out, l[:]...)
	out = append(out, p.MetaCT...)
	out = append(out, p.AssetCT...)
	return out
}

// ParseVaultPayload reverses Marshal. saltLen must match what was written.
func ParseVaultPayload(b []byte, saltLen int) (VaultPayload, error) {
	if len(b) < saltLen+4 {
		return VaultPayload{}, fmt.Errorf("blob: vault payload too short")
	}
	salt := b[:saltLen]
	rest := b[saltLen:]
	metaLen := int(binary.BigEndian.Uint32(rest[:4]))
	rest = rest[4:]
	if metaLen > len(rest) {
		return VaultPayload{}, fmt.Errorf("blob: vault meta length %d exceeds payload", metaLen)
	}
	return VaultPayload{Salt: salt, MetaCT: rest[:metaLen], AssetCT: rest[metaLen:]}, nil
}

// --- Warden payload framing --------------------------------------------------

// WardenPayload is appended to the warden stub. CT is AEAD over {sk, wordlist},
// keyed either by a static obfuscation key (HasPass=false) or by an Argon2id key
// derived from a passphrase (HasPass=true). Salt is plaintext.
type WardenPayload struct {
	HasPass bool
	Salt    []byte
	CT      []byte
}

// Marshal frames as: flag(1) | salt | ct. saltLen is fixed and known to readers.
func (p WardenPayload) Marshal() []byte {
	out := make([]byte, 0, 1+len(p.Salt)+len(p.CT))
	if p.HasPass {
		out = append(out, 1)
	} else {
		out = append(out, 0)
	}
	out = append(out, p.Salt...)
	out = append(out, p.CT...)
	return out
}

// ParseWardenPayload reverses Marshal.
func ParseWardenPayload(b []byte, saltLen int) (WardenPayload, error) {
	if len(b) < 1+saltLen {
		return WardenPayload{}, fmt.Errorf("blob: warden payload too short")
	}
	return WardenPayload{
		HasPass: b[0] == 1,
		Salt:    b[1 : 1+saltLen],
		CT:      b[1+saltLen:],
	}, nil
}
