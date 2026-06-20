// Package prompt provides the small interactive bits the vault and warden
// commands share: a no-echo password reader and a word-phrase reader.
//
// On a real terminal the phrase reader is an interactive per-word editor: typing
// letters narrows the BIP39 prefix, Tab cycles the matches in place, and Enter
// accepts — auto-completing when the prefix has exactly one match. When stdin is
// not a terminal (piped input, tests) it falls back to a line reader that expands
// prefixes and validates the checksum.
package prompt

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"vaultwright/internal/wordcodec"
)

// Password reads a line without echoing it.
func Password(label string) ([]byte, error) {
	fmt.Fprint(os.Stderr, label)
	defer fmt.Fprintln(os.Stderr)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		return term.ReadPassword(fd)
	}
	return readLine(os.Stdin)
}

// ReadPhrase reads a wantWords-long BIP39 phrase and returns the decoded bytes.
func ReadPhrase(label string, list *wordcodec.List, wantWords int) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		return interactivePhrase(fd, label, list, wantWords)
	}
	return linePhrase(os.Stdin, label, list, wantWords)
}

// interactivePhrase runs the raw-mode per-word editor.
func interactivePhrase(fd int, label string, list *wordcodec.List, wantWords int) ([]byte, error) {
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return linePhrase(os.Stdin, label, list, wantWords)
	}
	defer term.Restore(fd, oldState)

	out := os.Stderr
	in := bufio.NewReader(os.Stdin)
	fmt.Fprintf(out, "%s — %d words.  type letters · Tab cycles matches · Enter accepts\r\n", label, wantWords)

	for { // re-collect the whole phrase if the checksum fails
		words, err := collectWords(in, out, list, wantWords)
		if err != nil {
			return nil, err
		}
		entropy, err := list.Decode(words)
		if err == nil {
			return entropy, nil
		}
		fmt.Fprintf(out, "  ! %v — start over\r\n", err)
	}
}

func collectWords(in *bufio.Reader, out io.Writer, list *wordcodec.List, wantWords int) ([]string, error) {
	words := make([]string, 0, wantWords)
	for len(words) < wantWords {
		var typed []rune
		var matches []string // completions of the current prefix
		cycle := -1          // index into matches when cycling, else -1

		slot := len(words) + 1
		draw := func() {
			disp := string(typed)
			hint := ""
			if cycle >= 0 {
				disp = matches[cycle]
				hint = fmt.Sprintf("  (%d/%d)", cycle+1, len(matches))
			} else if len(typed) > 0 {
				switch n := len(matches); {
				case n == 0:
					hint = "  ✗"
				case n == 1:
					hint = "  ✓"
				default:
					hint = fmt.Sprintf("  [%d]", n)
				}
			}
			fmt.Fprintf(out, "\r\x1b[K  %2d/%d: %s%s", slot, wantWords, disp, hint)
		}
		recompute := func() {
			if len(typed) == 0 {
				matches = nil
			} else {
				matches = list.Complete(string(typed))
			}
			cycle = -1
		}
		draw()

		for accepted := false; !accepted; {
			b, err := in.ReadByte()
			if err != nil {
				return nil, err
			}
			switch {
			case b == 3: // Ctrl-C
				fmt.Fprint(out, "\r\n")
				return nil, fmt.Errorf("aborted")
			case b == '\r' || b == '\n' || b == ' ':
				word, ok := resolve(list, typed, matches, cycle)
				if !ok {
					beep(out)
					continue
				}
				fmt.Fprintf(out, "\r\x1b[K  %2d/%d: %s\r\n", slot, wantWords, word)
				words = append(words, word)
				accepted = true
			case b == '\t':
				if len(matches) == 0 {
					beep(out)
					continue
				}
				cycle = (cycle + 1) % len(matches)
				draw()
			case b == 127 || b == 8: // backspace / DEL
				if cycle >= 0 {
					cycle = -1 // first backspace just drops the cycled candidate
				} else if len(typed) > 0 {
					typed = typed[:len(typed)-1]
					recompute()
				} else {
					beep(out)
					continue
				}
				draw()
			case b >= 'A' && b <= 'Z':
				typed = append(typed, rune(b-'A'+'a'))
				recompute()
				draw()
			case b >= 'a' && b <= 'z':
				typed = append(typed, rune(b))
				recompute()
				draw()
			case b == 27: // swallow escape sequences (arrow keys etc.)
				for in.Buffered() > 0 {
					if c, _ := in.ReadByte(); c >= 'A' && c <= 'Z' || c == '~' {
						break
					}
				}
			default:
				beep(out)
			}
		}
	}
	return words, nil
}

// resolve decides which word Enter accepts: the cycled candidate if any, else the
// typed text if it's a whole word, else the sole completion of the prefix.
func resolve(list *wordcodec.List, typed []rune, matches []string, cycle int) (string, bool) {
	if cycle >= 0 {
		return matches[cycle], true
	}
	s := string(typed)
	if list.Valid(s) {
		return s, true
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

func beep(w io.Writer) { fmt.Fprint(w, "\a") }

// linePhrase is the non-terminal fallback: read tokens (prefixes ok) across lines
// until a valid wantWords-long phrase is assembled.
func linePhrase(r io.Reader, label string, list *wordcodec.List, wantWords int) ([]byte, error) {
	fmt.Fprintf(os.Stderr, "%s (%d words, prefixes ok):\n", label, wantWords)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var collected []string
	for {
		need := wantWords - len(collected)
		fmt.Fprintf(os.Stderr, "  %2d/%d > ", len(collected), wantWords)
		if !sc.Scan() {
			if err := sc.Err(); err != nil {
				return nil, err
			}
			return nil, io.EOF
		}
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		norm, err := list.Normalize(fields)
		if err != nil {
			fmt.Fprintf(os.Stderr, "    ! %v — re-enter\n", err)
			continue
		}
		if len(norm) > need {
			fmt.Fprintf(os.Stderr, "    ! too many words (need %d more) — re-enter\n", need)
			continue
		}
		collected = append(collected, norm...)
		if len(collected) < wantWords {
			continue
		}
		entropy, err := list.Decode(collected)
		if err != nil {
			fmt.Fprintf(os.Stderr, "    ! %v — start over\n", err)
			collected = collected[:0]
			continue
		}
		return entropy, nil
	}
}

func readLine(r io.Reader) ([]byte, error) {
	var line []byte
	var b [1]byte
	for {
		n, err := r.Read(b[:])
		if n > 0 {
			if b[0] == '\n' {
				break
			}
			line = append(line, b[0])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return []byte(strings.TrimRight(string(line), "\r")), nil
}
