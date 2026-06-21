// Command vaultwright is the stateless builder. `vaultwright seal <dir>` mints a fresh
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

	"github.com/alexey-lapin/vaultwright/internal/blob"
	"github.com/alexey-lapin/vaultwright/internal/builtin"
	"github.com/alexey-lapin/vaultwright/internal/prompt"
	"github.com/alexey-lapin/vaultwright/internal/scheme"
)

// version is set at release time via -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version", "--version", "-v":
		fmt.Println("vaultwright", version)
		return
	case "seal":
		if err := seal(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "vaultwright:", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `vaultwright — build an encrypted, embedded static-file server

usage:
  vaultwright seal <assets-dir> [-o name] [--warden-pass]
  vaultwright version

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
	if !builtin.StubsBuilt() {
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

	vaultPayload, wardenPayload, err := scheme.Seal(dir, builtin.Wordlist, password, wardenPass)
	wipe(password)
	wipe(wardenPass)
	if err != nil {
		return err
	}

	vaultPath := out + ".vault"
	wardenPath := out + ".warden"
	if err := blob.WriteSealedBytes(builtin.VaultStub, vaultPath, vaultPayload); err != nil {
		return err
	}
	if err := blob.WriteSealedBytes(builtin.WardenStub, wardenPath, wardenPayload); err != nil {
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
