// Command vaultwright is the stateless builder. `vaultwright seal <dir>` mints a
// fresh keypair, encrypts the directory, and writes matched vault + warden binaries
// for one or more target platforms. It then forgets the keypair — no secret is
// stored anywhere.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/alexey-lapin/vaultwright/internal/blob"
	"github.com/alexey-lapin/vaultwright/internal/builtin"
	"github.com/alexey-lapin/vaultwright/internal/prompt"
	"github.com/alexey-lapin/vaultwright/internal/scheme"
	"github.com/alexey-lapin/vaultwright/internal/stubs"
)

const targetsHelp = `targets take the form <os>/<arch>, e.g. linux/amd64, darwin/arm64,
windows/amd64 (os: darwin, linux, windows; arch: amd64, arm64; any combination
of the above). A target with no matching stub falls back to a download and
fails there if none exists.`

type rootOptions struct {
	Version bool `short:"v" long:"version" description:"print the version and exit"`
}

func main() {
	var root rootOptions
	// flags.Default also sets PrintErrors, which would print every error a
	// second time (unprefixed) before our own "vaultwright: <err>" line below.
	parser := flags.NewParser(&root, flags.HelpFlag|flags.PassDoubleDash)
	parser.SubcommandsOptional = true

	parser.AddCommand("seal", "seal a directory into a vault (+ warden)", sealLongHelp, &SealCommand{})
	parser.AddCommand("fetch-stubs", "pre-populate the stub cache for offline use", fetchStubsLongHelp, &FetchStubsCommand{})
	parser.AddCommand("cache", "print the download cache directory", "", &CacheCommand{})
	parser.AddCommand("version", "print the version and exit", "", &VersionCommand{})

	_, err := parser.Parse()
	if err != nil {
		var flagsErr *flags.Error
		if errors.As(err, &flagsErr) {
			if flagsErr.Type == flags.ErrHelp {
				fmt.Println(err) // the message *is* the formatted help text
				os.Exit(0)
			}
			fmt.Fprintln(os.Stderr, "vaultwright:", err)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "vaultwright:", err)
		os.Exit(1)
	}

	if root.Version {
		fmt.Println("vaultwright", builtin.Version)
		return
	}
	if parser.Active == nil {
		parser.WriteHelp(os.Stderr)
		os.Exit(2)
	}
}

// osArch is a go-flags value type for the os/arch target inputs shared by
// --vault-target, --warden-target, and fetch-stubs' positional list: it parses
// "os/arch" and completes from the embedded release manifest.
type osArch struct {
	OS, Arch string
}

func (t *osArch) UnmarshalFlag(value string) error {
	goos, goarch, err := parseTarget(value)
	if err != nil {
		return err
	}
	t.OS, t.Arch = goos, goarch
	return nil
}

func (t osArch) MarshalFlag() (string, error) {
	return t.OS + "/" + t.Arch, nil
}

// Complete offers real, always-current os/arch pairs sourced from the embedded
// release manifest, rather than a hand-maintained list.
func (t *osArch) Complete(match string) []flags.Completion {
	seen := make(map[string]bool)
	var out []flags.Completion
	for _, e := range stubs.ParseManifest(builtin.Manifest()).Entries() {
		pair := e.OS + "/" + e.Arch
		if seen[pair] || !strings.HasPrefix(pair, match) {
			continue
		}
		seen[pair] = true
		out = append(out, flags.Completion{Item: pair})
	}
	return out
}

const sealLongHelp = `seal always prompts for a vault password and a warden passphrase; leave the
warden passphrase empty to produce a warden with no passphrase protection.

--no-warden skips the warden entirely (conflicts with --warden-target) — the
vault then unlocks on the password alone, with no 2nd-factor handshake. This
is a real reduction in the security model (see SECURITY.md); use only when
the warden's threat model doesn't apply to your use case.

Produces (host default): <name>.vault (the server you run/distribute — public
key + encrypted assets) and <name>.warden (the responder you keep on a trusted
machine, the 2nd factor; omitted with --no-warden). With explicit targets the
outputs are suffixed, e.g. <name>.vault-linux-arm64, and .exe is added for
windows.

` + targetsHelp

type SealCommand struct {
	Output        string   `short:"o" long:"output" value-name:"name" description:"output base name (default: the assets dir name)"`
	VaultTargets  []osArch `long:"vault-target" value-name:"os/arch" description:"vault target (repeatable; default: host)"`
	WardenTargets []osArch `long:"warden-target" value-name:"os/arch" description:"warden target (repeatable; default: host)"`
	StubDir       string   `long:"stub-dir" value-name:"dir" description:"resolve stubs from this directory first"`
	Offline       bool     `long:"offline" description:"never download stubs (embedded/cache/stub-dir only)"`
	NoWarden      bool     `long:"no-warden" description:"single-factor: password only, no warden produced"`

	Args struct {
		Dir string `positional-arg-name:"assets-dir" required:"yes" description:"directory of files to serve"`
	} `positional-args:"yes"`
}

func (cmd *SealCommand) Execute(args []string) error {
	if cmd.NoWarden && len(cmd.WardenTargets) > 0 {
		return fmt.Errorf("--no-warden conflicts with --warden-target")
	}
	dir := cmd.Args.Dir
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("assets path %q is not a directory", dir)
	}
	out := cmd.Output
	if out == "" {
		out = filepath.Base(filepath.Clean(dir))
	}

	vaultTargets := toTargets(stubs.RoleVault, cmd.VaultTargets)
	wardenTargets := toTargets(stubs.RoleWarden, cmd.WardenTargets)

	// Explicit targets get suffixed output names; the plain host default does not.
	vaultSuffixed := len(vaultTargets) > 0
	if !vaultSuffixed {
		vaultTargets = []stubs.Target{hostTarget(stubs.RoleVault)}
	}
	var wardenSuffixed bool
	if !cmd.NoWarden {
		wardenSuffixed = len(wardenTargets) > 0
		if !wardenSuffixed {
			wardenTargets = []stubs.Target{hostTarget(stubs.RoleWarden)}
		}
	}

	// Resolve every stub up front so we fail before prompting for a password.
	resOpt := stubs.Options{StubDir: cmd.StubDir, Offline: cmd.Offline, Log: logToStderr}
	vaultStubs, err := resolveAll(stubs.RoleVault, vaultTargets, resOpt)
	if err != nil {
		return err
	}
	var wardenStubs [][]byte
	if !cmd.NoWarden {
		wardenStubs, err = resolveAll(stubs.RoleWarden, wardenTargets, resOpt)
		if err != nil {
			return err
		}
	}

	password, err := readNewPassword("Vault password: ", "Vault password confirm: ")
	if err != nil {
		return err
	}
	var wardenPass []byte
	if !cmd.NoWarden {
		wardenPass, err = readNewPassword("Warden passphrase (empty = none): ", "Warden passphrase confirm: ")
		if err != nil {
			return err
		}
	}

	// One keypair / one payload-pair, stamped onto every requested stub.
	vaultPayload, wardenPayload, err := scheme.Seal(dir, builtin.Wordlist, password, wardenPass, cmd.NoWarden)
	prompt.Wipe(password)
	prompt.Wipe(wardenPass)
	if err != nil {
		return err
	}

	var produced []string
	write := func(targets []stubs.Target, stubBytes [][]byte, payload []byte, suffixed bool) error {
		for i, t := range targets {
			path := outName(out, t, suffixed)
			if err := blob.WriteSealedBytes(stubBytes[i], path, payload); err != nil {
				return err
			}
			produced = append(produced, path)
		}
		return nil
	}
	if err := write(vaultTargets, vaultStubs, vaultPayload, vaultSuffixed); err != nil {
		return err
	}
	if !cmd.NoWarden {
		if err := write(wardenTargets, wardenStubs, wardenPayload, wardenSuffixed); err != nil {
			return err
		}
	}

	fmt.Println("Sealed:")
	for _, p := range produced {
		fmt.Println("  " + p)
	}
	return nil
}

func toTargets(role string, pairs []osArch) []stubs.Target {
	if len(pairs) == 0 {
		return nil
	}
	out := make([]stubs.Target, len(pairs))
	for i, p := range pairs {
		out[i] = stubs.Target{Role: role, OS: p.OS, Arch: p.Arch}
	}
	return out
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

func parseTarget(s string) (goos, goarch string, err error) {
	goos, goarch, ok := strings.Cut(s, "/")
	if !ok || goos == "" || goarch == "" {
		return "", "", fmt.Errorf("target %q must be in os/arch form, e.g. linux/amd64, darwin/arm64, windows/amd64 (run `vaultwright` for the full list)", s)
	}
	return goos, goarch, nil
}

const fetchStubsLongHelp = "pre-populates the stub cache for offline use.\n\n" + targetsHelp

type FetchStubsCommand struct {
	All     bool   `long:"all" description:"fetch every target in the release manifest"`
	StubDir string `long:"stub-dir" value-name:"dir" description:"write stubs into this directory instead of the cache"`

	Args struct {
		Targets []osArch `positional-arg-name:"os/arch"`
	} `positional-args:"yes"`
}

func (cmd *FetchStubsCommand) Execute(args []string) error {
	var targets []stubs.Target
	for _, p := range cmd.Args.Targets {
		targets = append(targets,
			stubs.Target{Role: stubs.RoleVault, OS: p.OS, Arch: p.Arch},
			stubs.Target{Role: stubs.RoleWarden, OS: p.OS, Arch: p.Arch})
	}

	if cmd.All {
		targets = stubs.ParseManifest(builtin.Manifest()).Entries()
		if len(targets) == 0 {
			return fmt.Errorf("manifest is empty (dev build) — nothing to fetch")
		}
	}
	if len(targets) == 0 {
		return fmt.Errorf("nothing to fetch: pass --all or one or more os/arch")
	}

	opt := stubs.Options{StubDir: cmd.StubDir, Log: logToStderr}
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

type CacheCommand struct{}

// Execute prints the download cache directory (like `brew --cache`).
func (cmd *CacheCommand) Execute(args []string) error {
	dir, err := stubs.DefaultCacheDir()
	if err != nil {
		return err
	}
	fmt.Println(dir)
	return nil
}

type VersionCommand struct{}

func (cmd *VersionCommand) Execute(args []string) error {
	fmt.Println("vaultwright", builtin.Version)
	return nil
}

// readNewPassword prompts twice and confirms the two entries match.
func readNewPassword(label, confirmLabel string) ([]byte, error) {
	first, err := prompt.Password(label)
	if err != nil {
		return nil, err
	}
	again, err := prompt.Password(confirmLabel)
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
