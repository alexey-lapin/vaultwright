// Command vaultwright is the stateless builder. `vaultwright seal <dir>` mints a
// fresh keypair, encrypts the directory, and writes matched vault + warden binaries
// for one or more target platforms. It then forgets the keypair — no secret is
// stored anywhere.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/alexey-lapin/vaultwright/internal/blob"
	"github.com/alexey-lapin/vaultwright/internal/builtin"
	"github.com/alexey-lapin/vaultwright/internal/prompt"
	"github.com/alexey-lapin/vaultwright/internal/scheme"
	"github.com/alexey-lapin/vaultwright/internal/stubs"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "version", "--version", "-v":
		fmt.Println("vaultwright", builtin.Version)
		return
	case "seal":
		err = seal(os.Args[2:])
	case "fetch-stubs":
		err = fetchStubs(os.Args[2:])
	case "cache":
		err = printCacheDir()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "vaultwright:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `vaultwright — build an encrypted, embedded static-file server

usage:
  vaultwright seal <assets-dir> [flags]
  vaultwright fetch-stubs [--all | <os>/<arch> ...] [--stub-dir dir]
  vaultwright cache
  vaultwright version

seal flags:
  -o name              output base name (default: the assets dir name)
  --vault-target os/arch    vault target (repeatable; default: host)
  --warden-target os/arch   warden target (repeatable; default: host)
  --stub-dir dir       resolve stubs from this directory first
  --offline            never download stubs (embedded/cache/stub-dir only)
  --no-warden          single-factor: password only, no warden produced

seal always prompts for a vault password and a warden passphrase; leave the
warden passphrase empty to produce a warden with no passphrase protection.
--no-warden skips the warden entirely (conflicts with --warden-target) — the
vault then unlocks on the password alone, with no 2nd-factor handshake. This
is a real reduction in the security model (see SECURITY.md); use only when
the warden's threat model doesn't apply to your use case.

produces (host default):
  <name>.vault   the server you run/distribute (public key + encrypted assets)
  <name>.warden  the responder you keep on a trusted machine (the 2nd factor;
                 omitted with --no-warden)
with explicit targets the outputs are suffixed, e.g. <name>.vault-linux-arm64,
and .exe is added for windows.

targets (--vault-target, --warden-target, fetch-stubs):
  form:  <os>/<arch>, e.g. linux/amd64, darwin/arm64, windows/amd64
  os:    darwin, linux, windows
  arch:  amd64, arm64
  (any os/arch combination of the above; a target with no matching stub
  falls back to a download and fails there if none exists)
`)
}

type sealOpts struct {
	dir           string
	out           string
	stubDir       string
	offline       bool
	noWarden      bool
	vaultTargets  []stubs.Target
	wardenTargets []stubs.Target
}

func seal(args []string) error {
	o, err := parseSealArgs(args)
	if err != nil {
		return err
	}
	if o.noWarden && len(o.wardenTargets) > 0 {
		return fmt.Errorf("--no-warden conflicts with --warden-target")
	}
	info, err := os.Stat(o.dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("assets path %q is not a directory", o.dir)
	}
	if o.out == "" {
		o.out = filepath.Base(filepath.Clean(o.dir))
	}

	// Explicit targets get suffixed output names; the plain host default does not.
	vaultSuffixed := len(o.vaultTargets) > 0
	if !vaultSuffixed {
		o.vaultTargets = []stubs.Target{hostTarget(stubs.RoleVault)}
	}
	var wardenSuffixed bool
	if !o.noWarden {
		wardenSuffixed = len(o.wardenTargets) > 0
		if !wardenSuffixed {
			o.wardenTargets = []stubs.Target{hostTarget(stubs.RoleWarden)}
		}
	}

	// Resolve every stub up front so we fail before prompting for a password.
	resOpt := stubs.Options{StubDir: o.stubDir, Offline: o.offline, Log: logToStderr}
	vaultStubs, err := resolveAll(stubs.RoleVault, o.vaultTargets, resOpt)
	if err != nil {
		return err
	}
	var wardenStubs [][]byte
	if !o.noWarden {
		wardenStubs, err = resolveAll(stubs.RoleWarden, o.wardenTargets, resOpt)
		if err != nil {
			return err
		}
	}

	password, err := readNewPassword("Password (for the vault): ")
	if err != nil {
		return err
	}
	var wardenPass []byte
	if !o.noWarden {
		wardenPass, err = readNewPassword("Warden passphrase (empty = none): ")
		if err != nil {
			return err
		}
	}

	// One keypair / one payload-pair, stamped onto every requested stub.
	vaultPayload, wardenPayload, err := scheme.Seal(o.dir, builtin.Wordlist, password, wardenPass, o.noWarden)
	prompt.Wipe(password)
	prompt.Wipe(wardenPass)
	if err != nil {
		return err
	}

	var produced []string
	write := func(targets []stubs.Target, stubBytes [][]byte, payload []byte, suffixed bool) error {
		for i, t := range targets {
			path := outName(o.out, t, suffixed)
			if err := blob.WriteSealedBytes(stubBytes[i], path, payload); err != nil {
				return err
			}
			produced = append(produced, path)
		}
		return nil
	}
	if err := write(o.vaultTargets, vaultStubs, vaultPayload, vaultSuffixed); err != nil {
		return err
	}
	if !o.noWarden {
		if err := write(o.wardenTargets, wardenStubs, wardenPayload, wardenSuffixed); err != nil {
			return err
		}
	}

	fmt.Println("Sealed:")
	for _, p := range produced {
		fmt.Println("  " + p)
	}
	return nil
}

func resolveAll(role string, targets []stubs.Target, opt stubs.Options) ([][]byte, error) {
	out := make([][]byte, len(targets))
	for i, t := range targets {
		b, err := stubs.Resolve(role, t.OS, t.Arch, opt)
		if err != nil {
			return nil, err
		}
		out[i] = b
	}
	return out, nil
}

// outName builds an output path: <base>.<role>[-<os>-<arch>][.exe].
func outName(base string, t stubs.Target, suffixed bool) string {
	name := base + "." + t.Role
	if suffixed {
		name += "-" + t.OS + "-" + t.Arch
	}
	if t.OS == "windows" {
		name += ".exe"
	}
	return name
}

func hostTarget(role string) stubs.Target {
	return stubs.Target{Role: role, OS: runtime.GOOS, Arch: runtime.GOARCH}
}

func parseSealArgs(args []string) (sealOpts, error) {
	var o sealOpts
	var positional []string
	need := func(i int) (string, error) {
		if i+1 >= len(args) {
			return "", fmt.Errorf("%s needs a value", args[i])
		}
		return args[i+1], nil
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-o", "--output":
			v, err := need(i)
			if err != nil {
				return o, err
			}
			o.out, i = v, i+1
		case "--offline":
			o.offline = true
		case "--no-warden":
			o.noWarden = true
		case "--stub-dir":
			v, err := need(i)
			if err != nil {
				return o, err
			}
			o.stubDir, i = v, i+1
		case "--vault-target", "--warden-target":
			v, err := need(i)
			if err != nil {
				return o, err
			}
			i++
			goos, goarch, err := parseTarget(v)
			if err != nil {
				return o, err
			}
			if a == "--vault-target" {
				o.vaultTargets = append(o.vaultTargets, stubs.Target{Role: stubs.RoleVault, OS: goos, Arch: goarch})
			} else {
				o.wardenTargets = append(o.wardenTargets, stubs.Target{Role: stubs.RoleWarden, OS: goos, Arch: goarch})
			}
		default:
			if strings.HasPrefix(a, "-") {
				return o, fmt.Errorf("unknown flag %q", a)
			}
			positional = append(positional, a)
		}
	}
	if len(positional) != 1 {
		return o, fmt.Errorf("expected exactly one assets directory")
	}
	o.dir = positional[0]
	return o, nil
}

func parseTarget(s string) (goos, goarch string, err error) {
	goos, goarch, ok := strings.Cut(s, "/")
	if !ok || goos == "" || goarch == "" {
		return "", "", fmt.Errorf("target %q must be in os/arch form, e.g. linux/amd64, darwin/arm64, windows/amd64 (run `vaultwright` for the full list)", s)
	}
	return goos, goarch, nil
}

// fetchStubs pre-populates the cache for offline use.
func fetchStubs(args []string) error {
	var all bool
	var stubDir string
	var targets []stubs.Target
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--all":
			all = true
		case a == "--stub-dir":
			if i+1 >= len(args) {
				return fmt.Errorf("--stub-dir needs a value")
			}
			stubDir, i = args[i+1], i+1
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q", a)
		default:
			goos, goarch, err := parseTarget(a)
			if err != nil {
				return err
			}
			targets = append(targets,
				stubs.Target{Role: stubs.RoleVault, OS: goos, Arch: goarch},
				stubs.Target{Role: stubs.RoleWarden, OS: goos, Arch: goarch})
		}
	}

	if all {
		targets = stubs.ParseManifest(builtin.Manifest()).Entries()
		if len(targets) == 0 {
			return fmt.Errorf("manifest is empty (dev build) — nothing to fetch")
		}
	}
	if len(targets) == 0 {
		return fmt.Errorf("nothing to fetch: pass --all or one or more os/arch")
	}

	opt := stubs.Options{StubDir: stubDir, Log: logToStderr}
	for _, t := range targets {
		if _, err := stubs.Resolve(t.Role, t.OS, t.Arch, opt); err != nil {
			return fmt.Errorf("%s %s/%s: %w", t.Role, t.OS, t.Arch, err)
		}
		fmt.Printf("  ok  %s %s/%s\n", t.Role, t.OS, t.Arch)
	}
	return nil
}

// logToStderr is passed as stubs.Options.Log so a network fetch is visible
// (they can take a while, and otherwise happen silently mid-command).
func logToStderr(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
}

// printCacheDir prints the download cache directory (like `brew --cache`).
func printCacheDir() error {
	dir, err := stubs.DefaultCacheDir()
	if err != nil {
		return err
	}
	fmt.Println(dir)
	return nil
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
		prompt.Wipe(again)
		return nil, fmt.Errorf("passwords do not match")
	}
	prompt.Wipe(again)
	return first, nil
}
