// Package cryptocore holds the cryptographic primitives for cypembed:
// password hardening, the per-seal key hierarchy, the authenticated-encryption
// envelope, and the fresh ephemeral challenge-response handshake.
//
// Key hierarchy (both factors strictly required):
//
//	P    = Argon2id(password, salt)              -- factor 1 (operator)
//	sk,pk                                        -- fresh X25519 keypair per seal
//	S    = HKDF(sk, "cypembed/asset-share/v1")   -- 16 bytes; factor 2 lives in warden
//	K_a  = HKDF(S || P)                          -- asset key
package cryptocore

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// Sizes used across the project.
const (
	SaltLen     = 16 // password salt, stored plaintext in the vault blob
	ShareLen    = 16 // S, the asset-key share carried by the handshake (12 words)
	AssetKeyLen = 32 // K_a
	pKeyLen     = 32 // Argon2id output
)

// Argon2id parameters. Memory-hard so each offline guess of a factor is costly.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 4
)

// info labels for HKDF derivations; distinct labels domain-separate the outputs.
const (
	infoShare   = "cypembed/asset-share/v1"
	infoAssetK  = "cypembed/asset-key/v1"
	infoRespPad = "cypembed/response-pad/v1"
)

// NewSalt returns a fresh random salt.
func NewSalt() ([]byte, error) { return randBytes(SaltLen) }

// DeriveP hardens the password into a 32-byte key with Argon2id.
func DeriveP(password, salt []byte) []byte {
	return argon2.IDKey(password, salt, argonTime, argonMemory, argonThreads, pKeyLen)
}

// NewKeyPair generates a fresh X25519 keypair for a seal.
func NewKeyPair() (*ecdh.PrivateKey, error) {
	return ecdh.X25519().GenerateKey(rand.Reader)
}

// DeriveShare reproduces S from the private key. Only the warden (which holds sk)
// can compute it; the vault never can.
func DeriveShare(sk *ecdh.PrivateKey) []byte {
	return hkdfKey(sk.Bytes(), nil, infoShare, ShareLen)
}

// DeriveAssetKey combines the handshake share S with the password key P into K_a.
func DeriveAssetKey(share, p []byte) []byte {
	in := make([]byte, 0, len(share)+len(p))
	in = append(in, share...)
	in = append(in, p...)
	return hkdfKey(in, nil, infoAssetK, AssetKeyLen)
}

// Seal encrypts plaintext with XChaCha20-Poly1305 under key, prepending a random
// 24-byte nonce. aad is authenticated but not encrypted (may be nil).
func Seal(key, plaintext, aad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce, err := randBytes(aead.NonceSize())
	if err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, aad), nil
}

// Open reverses Seal. A wrong key, tampered ciphertext, or wrong aad fails here.
func Open(key, ciphertext, aad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize() {
		return nil, errors.New("cryptocore: ciphertext too short")
	}
	nonce, ct := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	return aead.Open(nil, nonce, ct, aad)
}

// --- Handshake ---------------------------------------------------------------
//
// vault generates an ephemeral keypair and shows ePub as the challenge.
// warden computes shared = DH(sk, ePub) and returns S XOR pad(shared).
// vault recovers S via shared = DH(ePriv, pk).

// Respond is the warden side: given the vault's ephemeral public key, wrap the
// share S with a one-time pad derived from the shared secret.
func Respond(sk *ecdh.PrivateKey, ePub []byte) ([]byte, error) {
	peer, err := ecdh.X25519().NewPublicKey(ePub)
	if err != nil {
		return nil, fmt.Errorf("cryptocore: bad challenge key: %w", err)
	}
	shared, err := sk.ECDH(peer)
	if err != nil {
		return nil, err
	}
	pad := hkdfKey(shared, nil, infoRespPad, ShareLen)
	return xor(DeriveShare(sk), pad), nil
}

// RecoverShare is the vault side: unwrap S from the warden's response using the
// ephemeral private key and the vault's stored public key pk.
func RecoverShare(ePriv *ecdh.PrivateKey, pk, response []byte) ([]byte, error) {
	if len(response) != ShareLen {
		return nil, fmt.Errorf("cryptocore: response must be %d bytes, got %d", ShareLen, len(response))
	}
	peer, err := ecdh.X25519().NewPublicKey(pk)
	if err != nil {
		return nil, fmt.Errorf("cryptocore: bad public key: %w", err)
	}
	shared, err := ePriv.ECDH(peer)
	if err != nil {
		return nil, err
	}
	pad := hkdfKey(shared, nil, infoRespPad, ShareLen)
	return xor(response, pad), nil
}

func hkdfKey(secret, salt []byte, info string, length int) []byte {
	out, err := hkdf.Key(sha256.New, secret, salt, info, length)
	if err != nil {
		// hkdf.Key only errors on absurd length requests, which we never make.
		panic("cryptocore: hkdf: " + err.Error())
	}
	return out
}

func xor(a, b []byte) []byte {
	out := make([]byte, len(a))
	for i := range a {
		out[i] = a[i] ^ b[i]
	}
	return out
}

func randBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}
