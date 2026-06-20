package scheme

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"vaultwright/internal/wordcodec"
)

func loadWordlist(t testing.TB) []byte {
	t.Helper()
	b, err := os.ReadFile("../builtin/english.txt")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeAssets(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hi</h1>"), 0o644)
	os.MkdirAll(filepath.Join(dir, "css"), 0o755)
	os.WriteFile(filepath.Join(dir, "css", "style.css"), []byte("body{}"), 0o644)
	return dir
}

// fullUnlock runs the whole ceremony, including the word encode/decode that the
// human would do by hand.
func fullUnlock(t *testing.T, vaultPayload, wardenPayload, password, wardenPass []byte) map[string][]byte {
	t.Helper()

	// vault: open meta with the password.
	meta, err := OpenVaultMeta(vaultPayload, password)
	if err != nil {
		t.Fatalf("open meta: %v", err)
	}
	list, err := wordcodec.NewList(wordcodec.ParseWordlist(meta.Wordlist))
	if err != nil {
		t.Fatal(err)
	}

	// vault: make a challenge and render it as words.
	ePriv, challenge, err := NewChallenge()
	if err != nil {
		t.Fatal(err)
	}
	challengeWords, err := list.Encode(challenge)
	if err != nil {
		t.Fatal(err)
	}
	if len(challengeWords) != 24 {
		t.Fatalf("challenge = %d words, want 24", len(challengeWords))
	}

	// warden: decode challenge, respond, render response as words.
	wk, err := OpenWarden(wardenPayload, wardenPass)
	if err != nil {
		t.Fatalf("open warden: %v", err)
	}
	wlist, _ := wordcodec.NewList(wordcodec.ParseWordlist(wk.Wordlist))
	decodedChallenge, err := wlist.Decode(challengeWords)
	if err != nil {
		t.Fatal(err)
	}
	response, err := wk.Respond(decodedChallenge)
	if err != nil {
		t.Fatal(err)
	}
	responseWords, err := wlist.Encode(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(responseWords) != 12 {
		t.Fatalf("response = %d words, want 12", len(responseWords))
	}

	// vault: decode response and finish.
	decodedResponse, err := list.Decode(responseWords)
	if err != nil {
		t.Fatal(err)
	}
	files, err := meta.OpenAssets(ePriv, decodedResponse)
	if err != nil {
		t.Fatalf("open assets: %v", err)
	}
	return files
}

func TestSealUnlockEndToEnd(t *testing.T) {
	dir := writeAssets(t)
	wl := loadWordlist(t)
	password := []byte("hunter2-but-better")

	vaultPayload, wardenPayload, err := Seal(dir, wl, password, nil)
	if err != nil {
		t.Fatal(err)
	}

	files := fullUnlock(t, vaultPayload, wardenPayload, password, nil)
	if got := string(files["index.html"]); got != "<h1>hi</h1>" {
		t.Fatalf("index.html = %q", got)
	}
	if got := string(files["css/style.css"]); got != "body{}" {
		t.Fatalf("css/style.css = %q", got)
	}
}

func TestSealUnlockWithWardenPass(t *testing.T) {
	dir := writeAssets(t)
	wl := loadWordlist(t)
	vaultPayload, wardenPayload, err := Seal(dir, wl, []byte("pw"), []byte("warden-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if has, _ := WardenHasPass(wardenPayload); !has {
		t.Fatal("expected warden to require a passphrase")
	}
	files := fullUnlock(t, vaultPayload, wardenPayload, []byte("pw"), []byte("warden-secret"))
	if string(files["index.html"]) != "<h1>hi</h1>" {
		t.Fatal("unlock with warden pass failed")
	}
	// Wrong warden passphrase must fail.
	if _, err := OpenWarden(wardenPayload, []byte("nope")); err == nil {
		t.Fatal("expected wrong warden passphrase to fail")
	}
}

func TestWrongPasswordRejectedAtMeta(t *testing.T) {
	dir := writeAssets(t)
	wl := loadWordlist(t)
	vaultPayload, _, err := Seal(dir, wl, []byte("right"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenVaultMeta(vaultPayload, []byte("wrong")); err == nil {
		t.Fatal("expected wrong password to fail at meta")
	}
}

func TestWrongWardenProducesUndecryptableAssets(t *testing.T) {
	dir := writeAssets(t)
	wl := loadWordlist(t)
	password := []byte("pw")
	vaultPayload, _, err := Seal(dir, wl, password, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A different seal => different warden => wrong response.
	_, otherWarden, _ := Seal(dir, wl, password, nil)

	meta, _ := OpenVaultMeta(vaultPayload, password)
	ePriv, challenge, _ := NewChallenge()
	wk, _ := OpenWarden(otherWarden, nil)
	resp, _ := wk.Respond(challenge)
	if _, err := meta.OpenAssets(ePriv, resp); err == nil {
		t.Fatal("expected decrypt failure with response from a different warden")
	}
}

// TestPayloadUnscannable guards the core requirement: the bytes vaultwright appends to
// the vault stub must reveal neither the asset content nor the (plaintext) wordlist.
func TestPayloadUnscannable(t *testing.T) {
	dir := t.TempDir()
	marker := "TOP-SECRET-ASSET-MARKER-9f3a2b"
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>"+marker+"</h1>"), 0o644)
	wl := loadWordlist(t)

	vaultPayload, wardenPayload, err := Seal(dir, wl, []byte("pw"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string][]byte{"vault": vaultPayload, "warden": wardenPayload} {
		if bytes.Contains(p, []byte(marker)) {
			t.Fatalf("%s payload contains plaintext asset marker", name)
		}
		// Rare wordlist entries would appear if the list were embedded in plaintext.
		for _, w := range []string{"abandon", "zoo", "crouch", "kangaroo"} {
			if bytes.Contains(p, []byte(w)) {
				t.Fatalf("%s payload contains plaintext wordlist word %q", name, w)
			}
		}
	}
}
