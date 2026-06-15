package wordcodec

import (
	"bytes"
	"crypto/rand"
	"os"
	"testing"
)

func testList(t testing.TB) *List {
	t.Helper()
	b, err := os.ReadFile("../forgeasset/english.txt")
	if err != nil {
		t.Fatalf("read wordlist: %v", err)
	}
	l, err := NewList(ParseWordlist(b))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestKnownVector(t *testing.T) {
	// BIP39 reference: all-zero 128-bit entropy -> 12x "abandon" + "about".
	l := testList(t)
	got, err := l.Encode(make([]byte, 16))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"abandon", "abandon", "abandon", "abandon", "abandon", "abandon",
		"abandon", "abandon", "abandon", "abandon", "abandon", "about",
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("word %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRoundTripSizes(t *testing.T) {
	l := testList(t)
	for _, n := range []int{16, 32} { // response (12 words) and challenge (24 words)
		ent := make([]byte, n)
		if _, err := rand.Read(ent); err != nil {
			t.Fatal(err)
		}
		words, err := l.Encode(ent)
		if err != nil {
			t.Fatal(err)
		}
		wantWords := map[int]int{16: 12, 32: 24}[n]
		if len(words) != wantWords {
			t.Fatalf("%d bytes -> %d words, want %d", n, len(words), wantWords)
		}
		got, err := l.Decode(words)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, ent) {
			t.Fatalf("round-trip mismatch for %d bytes", n)
		}
	}
}

func TestChecksumCatchesSingleWordError(t *testing.T) {
	l := testList(t)
	ent := make([]byte, 32)
	rand.Read(ent)
	words, _ := l.Encode(ent)
	// Flip one word to a different valid word; checksum should reject it.
	if words[5] == "zoo" {
		words[5] = "zone"
	} else {
		words[5] = "zoo"
	}
	if _, err := l.Decode(words); err == nil {
		t.Fatal("expected checksum failure after altering a word")
	}
}

func TestUnknownWordRejected(t *testing.T) {
	l := testList(t)
	words, _ := l.Encode(make([]byte, 16))
	words[0] = "notabip39word"
	if _, err := l.Decode(words); err == nil {
		t.Fatal("expected error for unknown word")
	}
}

func TestComplete(t *testing.T) {
	l := testList(t)
	got := l.Complete("aban")
	if len(got) != 1 || got[0] != "abandon" {
		t.Fatalf("Complete(aban) = %v, want [abandon]", got)
	}
	if len(l.Complete("ab")) < 2 {
		t.Fatalf("expected several matches for 'ab'")
	}
}

func TestNormalize(t *testing.T) {
	l := testList(t)
	got, err := l.Normalize([]string{"aban", "ABILITY", " able "})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"abandon", "ability", "able"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token %d = %q, want %q", i, got[i], want[i])
		}
	}
	if _, err := l.Normalize([]string{"zzzz"}); err == nil {
		t.Fatal("expected unknown-word error")
	}
	if _, err := l.Normalize([]string{"ab"}); err == nil {
		t.Fatal("expected ambiguous-word error")
	}
}

func FuzzRoundTrip(f *testing.F) {
	l := testList(f)
	f.Add(make([]byte, 16))
	f.Add(make([]byte, 32))
	f.Fuzz(func(t *testing.T, ent []byte) {
		if len(ent) == 0 || len(ent)%4 != 0 {
			return
		}
		words, err := l.Encode(ent)
		if err != nil {
			return
		}
		got, err := l.Decode(words)
		if err != nil {
			t.Fatalf("decode after encode failed: %v", err)
		}
		if !bytes.Equal(got, ent) {
			t.Fatalf("round-trip mismatch")
		}
	})
}
