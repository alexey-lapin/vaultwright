// Command warden is the responder stub — the second factor. vaultwright appends the
// (obfuscated or passphrase-encrypted) private key to a copy of this binary. Run
// it on your trusted machine: paste the vault's challenge, get the response.
package main

import (
	"fmt"
	"os"

	"github.com/alexey-lapin/vaultwright/internal/blob"
	"github.com/alexey-lapin/vaultwright/internal/prompt"
	"github.com/alexey-lapin/vaultwright/internal/scheme"
	"github.com/alexey-lapin/vaultwright/internal/wordcodec"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "warden:", err)
		os.Exit(1)
	}
}

func run() error {
	payload, err := blob.ReadSelf()
	if err != nil {
		return fmt.Errorf("%w (this binary was not produced by `vaultwright seal`)", err)
	}

	hasPass, err := scheme.WardenHasPass(payload)
	if err != nil {
		return err
	}

	wk, err := unlockWarden(payload, hasPass)
	if err != nil {
		return err
	}
	list, err := wordcodec.NewList(wordcodec.ParseWordlist(wk.Wordlist))
	if err != nil {
		return err
	}

	challenge, err := prompt.ReadPhrase("Enter the 24-word challenge from vault", list, 24)
	if err != nil {
		return err
	}
	response, err := wk.Respond(challenge)
	if err != nil {
		return err
	}
	words, err := list.Encode(response)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "\nType this response back into vault:")
	printWords(words)
	return nil
}

// unlockWarden prompts for the passphrase (if the warden payload needs one) and
// retries on a wrong guess, same as the vault's own password prompt.
func unlockWarden(payload []byte, hasPass bool) (*scheme.WardenKey, error) {
	if !hasPass {
		return scheme.OpenWarden(payload, nil)
	}
	for i := 0; i < prompt.Attempts; i++ {
		pass, err := prompt.Password("Warden passphrase: ")
		if err != nil {
			return nil, err
		}
		wk, err := scheme.OpenWarden(payload, pass)
		prompt.Wipe(pass)
		if err == nil {
			return wk, nil
		}
		fmt.Fprintln(os.Stderr, "  !", err)
	}
	return nil, fmt.Errorf("too many wrong passphrase attempts")
}

func printWords(words []string) {
	for i, w := range words {
		fmt.Printf("%2d.%-12s", i+1, w)
		if (i+1)%4 == 0 {
			fmt.Println()
		}
	}
	if len(words)%4 != 0 {
		fmt.Println()
	}
}
