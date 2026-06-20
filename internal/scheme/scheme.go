// Package scheme orchestrates the cryptocore primitives, blob framing, archiving,
// and word codec into the two high-level operations: sealing a directory into a
// vault+warden pair, and the unlock handshake. UI (prompts, printing) lives in the
// commands; this package is pure logic so it can be tested headlessly.
package scheme

import (
	"crypto/ecdh"
	"crypto/sha256"
	"fmt"

	"vaultwright/internal/archive"
	"vaultwright/internal/blob"
	"vaultwright/internal/cryptocore"
)

const pkLen = 32 // X25519 public/private key length

// wardenStaticKey is the obfuscation key used when no warden passphrase is set.
// It lives in the binary, so this is obfuscation (not encryption) by design —
// the warden binary's security rests on possession, as agreed.
func wardenStaticKey() []byte {
	h := sha256.Sum256([]byte("vaultwright/warden-static-obfuscation/v1"))
	return h[:]
}

// Seal encrypts assetsDir and returns the payloads to append to the vault and
// warden stubs. wordlist is the raw newline-separated BIP39 list. wardenPass may
// be empty (obfuscation only).
func Seal(assetsDir string, wordlist, password, wardenPass []byte) (vaultPayload, wardenPayload []byte, err error) {
	salt, err := cryptocore.NewSalt()
	if err != nil {
		return nil, nil, err
	}
	p := cryptocore.DeriveP(password, salt)

	sk, err := cryptocore.NewKeyPair()
	if err != nil {
		return nil, nil, err
	}
	pk := sk.PublicKey().Bytes()
	share := cryptocore.DeriveShare(sk)
	ka := cryptocore.DeriveAssetKey(share, p)

	tarBytes, err := archive.Create(assetsDir)
	if err != nil {
		return nil, nil, err
	}
	assetCT, err := cryptocore.Seal(ka, tarBytes, nil)
	if err != nil {
		return nil, nil, err
	}

	metaCT, err := cryptocore.Seal(p, concat(pk, wordlist), nil)
	if err != nil {
		return nil, nil, err
	}
	vaultPayload = blob.VaultPayload{Salt: salt, MetaCT: metaCT, AssetCT: assetCT}.Marshal()

	// Warden payload.
	wsalt, err := cryptocore.NewSalt()
	if err != nil {
		return nil, nil, err
	}
	var wkey []byte
	hasPass := len(wardenPass) > 0
	if hasPass {
		wkey = cryptocore.DeriveP(wardenPass, wsalt)
	} else {
		wkey = wardenStaticKey()
	}
	wCT, err := cryptocore.Seal(wkey, concat(sk.Bytes(), wordlist), nil)
	if err != nil {
		return nil, nil, err
	}
	wardenPayload = blob.WardenPayload{HasPass: hasPass, Salt: wsalt, CT: wCT}.Marshal()

	return vaultPayload, wardenPayload, nil
}

// VaultMeta is the result of opening the vault's password-protected metadata.
type VaultMeta struct {
	PK       []byte // warden public key
	Wordlist []byte // raw BIP39 list
	p        []byte // password key, kept for asset decryption
	assetCT  []byte
}

// OpenVaultMeta decrypts the metadata with the password. A wrong password fails
// here, before the handshake.
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
	if len(metaPlain) < pkLen {
		return nil, fmt.Errorf("scheme: corrupt metadata")
	}
	return &VaultMeta{
		PK:       metaPlain[:pkLen],
		Wordlist: metaPlain[pkLen:],
		p:        p,
		assetCT:  vp.AssetCT,
	}, nil
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
