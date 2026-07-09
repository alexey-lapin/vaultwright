// Package scheme orchestrates the cryptocore primitives, blob framing, archiving,
// and word codec into the two high-level operations: sealing a directory into a
// vault+warden pair, and the unlock handshake. UI (prompts, printing) lives in the
// commands; this package is pure logic so it can be tested headlessly.
package scheme

import (
	"crypto/ecdh"
	"crypto/sha256"
	"fmt"

	"github.com/alexey-lapin/vaultwright/internal/archive"
	"github.com/alexey-lapin/vaultwright/internal/blob"
	"github.com/alexey-lapin/vaultwright/internal/cryptocore"
)

const pkLen = 32 // X25519 public/private key length

// wardenStaticKey is the obfuscation key used when no warden passphrase is set.
// It lives in the binary, so this is obfuscation (not encryption) by design —
// the warden binary's security rests on possession, as agreed.
func wardenStaticKey() []byte {
	h := sha256.Sum256([]byte("vaultwright/warden-static-obfuscation/v1"))
	return h[:]
}

// metaMode tags the vault metadata plaintext (inside MetaCT, so it's only visible
// once the password is known) with which unlock path the vault expects.
const (
	metaModeTwoFactor byte = 0
	metaModeSolo      byte = 1
)

// Seal encrypts assetsDir and returns the payloads to append to the vault and
// warden stubs. wordlist is the raw newline-separated BIP39 list. wardenPass may
// be empty (obfuscation only). If noWarden is true, the vault is single-factor
// (password only): no keypair/handshake, no warden — wardenPayload is nil and
// wardenPass is ignored.
func Seal(assetsDir string, wordlist, password, wardenPass []byte, noWarden bool) (vaultPayload, wardenPayload []byte, err error) {
	salt, err := cryptocore.NewSalt()
	if err != nil {
		return nil, nil, err
	}
	p := cryptocore.DeriveP(password, salt)

	var ka, metaPlain []byte
	if noWarden {
		ka = cryptocore.DeriveAssetKeySolo(p)
		metaPlain = []byte{metaModeSolo}
	} else {
		sk, err := cryptocore.NewKeyPair()
		if err != nil {
			return nil, nil, err
		}
		pk := sk.PublicKey().Bytes()
		share := cryptocore.DeriveShare(sk)
		ka = cryptocore.DeriveAssetKey(share, p)
		metaPlain = append([]byte{metaModeTwoFactor}, concat(pk, wordlist)...)

		wardenPayload, err = sealWarden(sk.Bytes(), wordlist, wardenPass)
		if err != nil {
			return nil, nil, err
		}
	}

	tarBytes, err := archive.Create(assetsDir)
	if err != nil {
		return nil, nil, err
	}
	assetCT, err := cryptocore.Seal(ka, tarBytes, nil)
	if err != nil {
		return nil, nil, err
	}

	metaCT, err := cryptocore.Seal(p, metaPlain, nil)
	if err != nil {
		return nil, nil, err
	}
	vaultPayload = blob.VaultPayload{Salt: salt, MetaCT: metaCT, AssetCT: assetCT}.Marshal()

	return vaultPayload, wardenPayload, nil
}

func sealWarden(skBytes, wordlist, wardenPass []byte) ([]byte, error) {
	wsalt, err := cryptocore.NewSalt()
	if err != nil {
		return nil, err
	}
	var wkey []byte
	hasPass := len(wardenPass) > 0
	if hasPass {
		wkey = cryptocore.DeriveP(wardenPass, wsalt)
	} else {
		wkey = wardenStaticKey()
	}
	wCT, err := cryptocore.Seal(wkey, concat(skBytes, wordlist), nil)
	if err != nil {
		return nil, err
	}
	return blob.WardenPayload{HasPass: hasPass, Salt: wsalt, CT: wCT}.Marshal(), nil
}

// VaultMeta is the result of opening the vault's password-protected metadata.
type VaultMeta struct {
	TwoFactor bool   // false for a single-factor (--no-warden) vault
	PK        []byte // warden public key; empty unless TwoFactor
	Wordlist  []byte // raw BIP39 list; empty unless TwoFactor
	p         []byte // password key, kept for asset decryption
	assetCT   []byte
}

// OpenVaultMeta decrypts the metadata with the password. A wrong password fails
// here, before the handshake (if any).
func OpenVaultMeta(vaultPayload, password []byte) (*VaultMeta, error) {
	vp, err := blob.ParseVaultPayload(vaultPayload, cryptocore.SaltLen)
	if err != nil {
		return nil, err
	}
	p := cryptocore.DeriveP(password, vp.Salt)
	metaPlain, err := cryptocore.Open(p, vp.MetaCT, nil)
	if err != nil {
		return nil, fmt.Errorf("wrong password")
	}
	if len(metaPlain) < 1 {
		return nil, fmt.Errorf("scheme: corrupt metadata")
	}
	mode, rest := metaPlain[0], metaPlain[1:]
	switch mode {
	case metaModeSolo:
		return &VaultMeta{p: p, assetCT: vp.AssetCT}, nil
	case metaModeTwoFactor:
		if len(rest) < pkLen {
			return nil, fmt.Errorf("scheme: corrupt metadata")
		}
		return &VaultMeta{
			TwoFactor: true,
			PK:        rest[:pkLen],
			Wordlist:  rest[pkLen:],
			p:         p,
			assetCT:   vp.AssetCT,
		}, nil
	default:
		return nil, fmt.Errorf("scheme: corrupt metadata")
	}
}

// NewChallenge generates an ephemeral keypair; the public bytes are the challenge.
func NewChallenge() (ePriv *ecdh.PrivateKey, challenge []byte, err error) {
	ePriv, err = cryptocore.NewKeyPair()
	if err != nil {
		return nil, nil, err
	}
	return ePriv, ePriv.PublicKey().Bytes(), nil
}

// OpenAssets completes the unlock: recover the share from the warden response,
// derive K_a, decrypt and extract the files.
func (m *VaultMeta) OpenAssets(ePriv *ecdh.PrivateKey, response []byte) (map[string][]byte, error) {
	if !m.TwoFactor {
		return nil, fmt.Errorf("scheme: vault is single-factor, no handshake to complete")
	}
	share, err := cryptocore.RecoverShare(ePriv, m.PK, response)
	if err != nil {
		return nil, err
	}
	ka := cryptocore.DeriveAssetKey(share, m.p)
	tarBytes, err := cryptocore.Open(ka, m.assetCT, nil)
	if err != nil {
		return nil, fmt.Errorf("unlock failed: wrong response or password")
	}
	return archive.Extract(tarBytes)
}

// OpenAssetsSolo completes the unlock for a single-factor (--no-warden) vault:
// no handshake, K_a is derived from the password alone.
func (m *VaultMeta) OpenAssetsSolo() (map[string][]byte, error) {
	if m.TwoFactor {
		return nil, fmt.Errorf("scheme: vault is two-factor, a warden handshake is required")
	}
	ka := cryptocore.DeriveAssetKeySolo(m.p)
	tarBytes, err := cryptocore.Open(ka, m.assetCT, nil)
	if err != nil {
		return nil, fmt.Errorf("wrong password")
	}
	return archive.Extract(tarBytes)
}

// WardenKey holds the responder's secret and wordlist after deobfuscation.
type WardenKey struct {
	sk       *ecdh.PrivateKey
	Wordlist []byte
}

// OpenWarden deobfuscates (or decrypts, if a passphrase is set) the warden payload.
// pass may be empty when HasPass is false.
func OpenWarden(wardenPayload, pass []byte) (*WardenKey, error) {
	wp, err := blob.ParseWardenPayload(wardenPayload, cryptocore.SaltLen)
	if err != nil {
		return nil, err
	}
	var wkey []byte
	if wp.HasPass {
		if len(pass) == 0 {
			return nil, fmt.Errorf("warden passphrase required")
		}
		wkey = cryptocore.DeriveP(pass, wp.Salt)
	} else {
		wkey = wardenStaticKey()
	}
	plain, err := cryptocore.Open(wkey, wp.CT, nil)
	if err != nil {
		return nil, fmt.Errorf("wrong warden passphrase")
	}
	if len(plain) < pkLen {
		return nil, fmt.Errorf("scheme: corrupt warden payload")
	}
	sk, err := ecdh.X25519().NewPrivateKey(plain[:pkLen])
	if err != nil {
		return nil, err
	}
	return &WardenKey{sk: sk, Wordlist: plain[pkLen:]}, nil
}

// WardenHasPass reports whether the warden payload needs a passphrase, without
// decrypting it (so the command can decide whether to prompt).
func WardenHasPass(wardenPayload []byte) (bool, error) {
	wp, err := blob.ParseWardenPayload(wardenPayload, cryptocore.SaltLen)
	if err != nil {
		return false, err
	}
	return wp.HasPass, nil
}

// Respond computes the 16-byte response for a challenge (warden side).
func (k *WardenKey) Respond(challenge []byte) ([]byte, error) {
	return cryptocore.Respond(k.sk, challenge)
}

func concat(a, b []byte) []byte {
	out := make([]byte, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}
