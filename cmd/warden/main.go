// Command warden is the responder stub — the second factor. vaultwright appends the
// (obfuscated or passphrase-encrypted) private key to a copy of this binary. Run
// it on your trusted machine: paste the vault's challenge, get the response.
package main

import (
	"fmt"
	"os"

	"vaultwright/internal/blob"
	"vaultwright/internal/prompt"
	"vaultwright/internal/scheme"
	"vaultwright/internal/wordcodec"
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

	var pass []byte
	if hasPass, err := scheme.WardenHasPass(payload); err != nil {
		return err
	} else if hasPass {
		pass, err = prompt.Password("Warden passphrase: ")
		if err != nil {
			return err
		}
	}

	wk, err := scheme.OpenWarden(payload, pass)
	wipe(pass)
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

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
