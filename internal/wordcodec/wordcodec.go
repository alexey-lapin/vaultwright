// Package wordcodec encodes byte strings as BIP39 word phrases and back, with the
// standard BIP39 checksum. It deliberately does NOT embed the wordlist: the list
// is sensitive (a plaintext copy is the biggest scannability fingerprint), so it
// is supplied at runtime from the caller's decrypted blob.
//
// Sizes used by vaultwright: a 32-byte challenge -> 24 words, a 16-byte response -> 12.
package wordcodec

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// ListSize is the required number of words in a BIP39 list.
const ListSize = 2048

// List is an indexed BIP39 wordlist.
type List struct {
	words []string
	index map[string]uint16
}

// NewList builds a List from exactly 2048 words.
func NewList(words []string) (*List, error) {
	if len(words) != ListSize {
		return nil, fmt.Errorf("wordcodec: wordlist must have %d words, got %d", ListSize, len(words))
	}
	idx := make(map[string]uint16, ListSize)
	for i, w := range words {
		if _, dup := idx[w]; dup {
			return nil, fmt.Errorf("wordcodec: duplicate word %q", w)
		}
		idx[w] = uint16(i)
	}
	cp := make([]string, len(words))
	copy(cp, words)
	return &List{words: cp, index: idx}, nil
}

// ParseWordlist splits raw newline-separated bytes into a word slice.
func ParseWordlist(b []byte) []string {
	lines := strings.Split(string(b), "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if w := strings.TrimSpace(ln); w != "" {
			out = append(out, w)
		}
	}
	return out
}

// Encode turns entropy (length a multiple of 4 bytes) into a BIP39 phrase.
func (l *List) Encode(entropy []byte) ([]string, error) {
	ent := len(entropy) * 8
	// BIP39 is defined for 128..256 bits in 32-bit steps (12..24 words); this
	// keeps the checksum within the SHA-256 output.
	if ent < 128 || ent > 256 || ent%32 != 0 {
		return nil, fmt.Errorf("wordcodec: entropy must be 16..32 bytes in 4-byte steps, got %d", len(entropy))
	}
	cs := ent / 32
	h := sha256.Sum256(entropy)
	nwords := (ent + cs) / 11

	readBit := func(i int) int {
		if i < ent {
			return int(entropy[i/8]>>(7-uint(i%8))) & 1
		}
		j := i - ent
		return int(h[j/8]>>(7-uint(j%8))) & 1
	}

	words := make([]string, 0, nwords)
	for w := 0; w < nwords; w++ {
		var idx uint16
		for b := 0; b < 11; b++ {
			idx = idx<<1 | uint16(readBit(w*11+b))
		}
		words = append(words, l.words[idx])
	}
	return words, nil
}

// Decode reverses Encode, verifying the checksum. An unknown word, wrong length,
// or bad checksum is an error (this is what catches typos).
func (l *List) Decode(words []string) ([]byte, error) {
	n := len(words)
	if n == 0 || (n*11)%33 != 0 {
		return nil, fmt.Errorf("wordcodec: phrase length %d is not a valid BIP39 size", n)
	}
	total := n * 11
	ent := total * 32 / 33
	cs := total - ent

	entropy := make([]byte, ent/8)
	checksum := 0
	pos := 0
	for _, word := range words {
		idx, ok := l.index[strings.TrimSpace(word)]
		if !ok {
			return nil, fmt.Errorf("wordcodec: unknown word %q", word)
		}
		for b := 10; b >= 0; b-- {
			bit := int(idx>>uint(b)) & 1
			if pos < ent {
				entropy[pos/8] |= byte(bit) << (7 - uint(pos%8))
			} else {
				checksum = checksum<<1 | bit
			}
			pos++
		}
	}

	h := sha256.Sum256(entropy)
	expected := 0
	for i := 0; i < cs; i++ {
		expected = expected<<1 | int(h[i/8]>>(7-uint(i%8)))&1
	}
	if expected != checksum {
		return nil, fmt.Errorf("wordcodec: checksum mismatch (a word is wrong)")
	}
	return entropy, nil
}

// Complete returns wordlist entries that begin with prefix (for input autocomplete).
func (l *List) Complete(prefix string) []string {
	prefix = strings.TrimSpace(strings.ToLower(prefix))
	if prefix == "" {
		return nil
	}
	var out []string
	for _, w := range l.words {
		if strings.HasPrefix(w, prefix) {
			out = append(out, w)
		}
	}
	return out
}

// Valid reports whether word is in the list.
func (l *List) Valid(word string) bool {
	_, ok := l.index[strings.TrimSpace(word)]
	return ok
}

// Normalize expands each token to a full wordlist entry: a full word is kept, an
// unambiguous prefix is completed (BIP39 has unique 4-letter prefixes), and an
// unknown or ambiguous token is an error. This lets a user type prefixes.
func (l *List) Normalize(tokens []string) ([]string, error) {
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		t := strings.ToLower(strings.TrimSpace(tok))
		if t == "" {
			continue
		}
		if l.Valid(t) {
			out = append(out, t)
			continue
		}
		m := l.Complete(t)
		switch {
		case len(m) == 1:
			out = append(out, m[0])
		case len(m) == 0:
			return nil, fmt.Errorf("unknown word %q", tok)
		default:
			return nil, fmt.Errorf("ambiguous word %q (could be %s...)", tok, strings.Join(m[:min(3, len(m))], ", "))
		}
	}
	return out, nil
}
