// Command forge is the stateless builder. `forge seal <dir>` mints a fresh
// keypair, encrypts the directory, and writes a matched pair of binaries:
// <name>.vault (the server) and <name>.warden (the responder / second factor).
// It then forgets the keypair — no secret is stored anywhere.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"cypembed/internal/blob"
	"cypembed/internal/forgeasset"
	"cypembed/internal/prompt"
	"cypembed/internal/scheme"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "seal" {
		usage()
		os.Exit(2)
	}
	if err := seal(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "forge:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `forge — build an encrypted, embedded static-file server

usage:
  forge seal <assets-dir> [-o name] [--warden-pass]

flags:
  -o name        output base name (default: the assets dir name)
  --warden-pass  also protect the warden binary with a passphrase (prompted)

produces:
  <name>.vault   the server you run/distribute (public key + encrypted assets)
  <name>.warden  the responder you keep on a trusted machine (the 2nd factor)
`)
}

func seal(args []string) error {
	dir, out, wantWardenPass, err := parseSealArgs(args)
	if err != nil {
		return err
	}
	if !forgeasset.StubsBuilt() {
		return errors.New("stubs are placeholders — run `make` to build the vault/warden stubs first")
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("assets path %q is not a directory", dir)
	}
	if out == "" {
		out = filepath.Base(filepath.Clean(dir))
	}

	password, err := readNewPassword("Password (for the vault): ")
	if err != nil {
		return err
	}
	var wardenPass []byte
	if wantWardenPass {
		wardenPass, err = readNewPassword("Warden passphrase: ")
		if err != nil {
			return err
		}
	}

	vaultPayload, wardenPayload, err := scheme.Seal(dir, forgeasset.Wordlist, password, wardenPass)
	wipe(password)
	wipe(wardenPass)
	if err != nil {
		return err
	}

	vaultPath := out + ".vault"
	wardenPath := out + ".warden"
	if err := blob.WriteSealedBytes(forgeasset.VaultStub, vaultPath, vaultPayload); err != nil {
		return err
	}
	if err := blob.WriteSealedBytes(forgeasset.WardenStub, wardenPath, wardenPayload); err != nil {
		return err
	}

	fmt.Printf("Sealed.\n  %s   (run / distribute)\n  %s  (keep on your trusted machine — the 2nd factor)\n", vaultPath, wardenPath)
	return nil
}

func parseSealArgs(args []string) (dir, out string, wardenPass bool, err error) {
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o", "--output":
			if i+1 >= len(args) {
				return "", "", false, fmt.Errorf("%s needs a value", args[i])
			}
			i++
			out = args[i]
		case "--warden-pass":
			wardenPass = true
		default:
			if len(args[i]) > 0 && args[i][0] == '-' {
				return "", "", false, fmt.Errorf("unknown flag %q", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 1 {
		return "", "", false, fmt.Errorf("expected exactly one assets directory")
	}
	return positional[0], out, wardenPass, nil
}

// readNewPassword prompts twice and confirms the two entries match.
func readNewPassword(label string) ([]byte, error) {
	first, err := prompt.Password(label)
	if err != nil {
		return nil, err
	}
	again, err := prompt.Password("Confirm: ")
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(first, again) {
		wipe(again)
		return nil, fmt.Errorf("passwords do not match")
	}
	wipe(again)
	return first, nil
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
