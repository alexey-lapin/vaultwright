package cryptocore

import (
	"bytes"
	"testing"
)

// fullFlow exercises seal-time derivation and the unlock handshake end to end.
func TestHandshakeRoundTrip(t *testing.T) {
	password := []byte("correct horse battery staple")
	salt, err := NewSalt()
	if err != nil {
		t.Fatal(err)
	}

	// Seal time (forge): derive everything and encrypt some assets.
	sk, err := NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	pk := sk.PublicKey().Bytes()
	p := DeriveP(password, salt)
	share := DeriveShare(sk)
	ka := DeriveAssetKey(share, p)

	assets := []byte("the secret static files")
	assetCT, err := Seal(ka, assets, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Unlock time (vault): password known, run the handshake with warden.
	ePriv, err := NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	challenge := ePriv.PublicKey().Bytes() // shown as 24 words

	response, err := Respond(sk, challenge) // warden side
	if err != nil {
		t.Fatal(err)
	}
	if len(response) != ShareLen {
		t.Fatalf("response len = %d, want %d", len(response), ShareLen)
	}

	gotShare, err := RecoverShare(ePriv, pk, response) // vault side
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotShare, share) {
		t.Fatalf("recovered share mismatch")
	}

	// vault re-derives K_a and decrypts.
	gotKa := DeriveAssetKey(gotShare, DeriveP(password, salt))
	got, err := Open(gotKa, assetCT, nil)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if !bytes.Equal(got, assets) {
		t.Fatalf("decrypted assets mismatch")
	}
}

func TestWrongPasswordFails(t *testing.T) {
	salt, _ := NewSalt()
	sk, _ := NewKeyPair()
	share := DeriveShare(sk)
	ka := DeriveAssetKey(share, DeriveP([]byte("right"), salt))
	ct, _ := Seal(ka, []byte("data"), nil)

	wrongKa := DeriveAssetKey(share, DeriveP([]byte("wrong"), salt))
	if _, err := Open(wrongKa, ct, nil); err == nil {
		t.Fatal("expected decrypt failure with wrong password")
	}
}

func TestWrongResponderFails(t *testing.T) {
	// A response from a different warden yields a different share -> wrong K_a.
	salt, _ := NewSalt()
	p := DeriveP([]byte("pw"), salt)
	sk, _ := NewKeyPair()
	pk := sk.PublicKey().Bytes()
	ka := DeriveAssetKey(DeriveShare(sk), p)
	ct, _ := Seal(ka, []byte("data"), nil)

	other, _ := NewKeyPair() // attacker's warden
	ePriv, _ := NewKeyPair()
	resp, _ := Respond(other, ePriv.PublicKey().Bytes())
	badShare, _ := RecoverShare(ePriv, pk, resp)
	badKa := DeriveAssetKey(badShare, p)
	if _, err := Open(badKa, ct, nil); err == nil {
		t.Fatal("expected decrypt failure with wrong responder")
	}
}

func TestTamperFails(t *testing.T) {
	key, _ := NewSalt()
	key = append(key, key...) // 32 bytes
	ct, _ := Seal(key, []byte("hello world"), nil)
	ct[len(ct)-1] ^= 0xff
	if _, err := Open(key, ct, nil); err == nil {
		t.Fatal("expected decrypt failure on tampered ciphertext")
	}
}

func TestResponseReplayUselessAcrossSessions(t *testing.T) {
	// A response captured for one ephemeral key does not recover S for another.
	sk, _ := NewKeyPair()
	pk := sk.PublicKey().Bytes()
	share := DeriveShare(sk)

	e1, _ := NewKeyPair()
	resp1, _ := Respond(sk, e1.PublicKey().Bytes())

	// New session: different ephemeral key, attacker replays resp1.
	e2, _ := NewKeyPair()
	got, _ := RecoverShare(e2, pk, resp1)
	if bytes.Equal(got, share) {
		t.Fatal("replayed response should not recover the share for a new session")
	}
}
