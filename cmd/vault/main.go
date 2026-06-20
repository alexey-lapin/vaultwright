// Command vault is the sealed server stub. vaultwright appends an encrypted payload to
// a copy of this binary; at runtime it unlocks with a password plus a fresh
// challenge-response with the matching warden, then serves the files from memory.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"vaultwright/internal/blob"
	"vaultwright/internal/prompt"
	"vaultwright/internal/scheme"
	"vaultwright/internal/serve"
	"vaultwright/internal/wordcodec"
)

const passwordAttempts = 3

func main() {
	var (
		idle     = flag.Duration("idle", 15*time.Minute, "auto-shutdown after this much inactivity (0 = never)")
		port     = flag.Int("port", 0, "TCP port (0 = random)")
		addr     = flag.String("addr", "127.0.0.1", "bind address")
		noKey    = flag.Bool("no-path-key", false, "drop the unguessable URL path-key segment")
		entry    = flag.String("entry-point", "index.html", "directory document served at the root")
		fallback = flag.Bool("fallback", false, "serve entry-point for unmatched non-file routes (SPA)")
	)
	flag.Parse()

	if err := run(*idle, *port, *addr, !*noKey, *entry, *fallback); err != nil {
		fmt.Fprintln(os.Stderr, "vault:", err)
		os.Exit(1)
	}
}

func run(idle time.Duration, port int, addr string, pathKey bool, entry string, fallback bool) error {
	payload, err := blob.ReadSelf()
	if err != nil {
		return fmt.Errorf("%w (this binary was not produced by `vaultwright seal`)", err)
	}

	meta, err := unlockMeta(payload)
	if err != nil {
		return err
	}
	list, err := wordcodec.NewList(wordcodec.ParseWordlist(meta.Wordlist))
	if err != nil {
		return err
	}

	files, err := handshake(meta, list)
	if err != nil {
		return err
	}

	srv, err := serve.New(files, serve.Options{
		Addr: addr, Port: port, PathKey: pathKey,
		EntryPoint: entry, Fallback: fallback, Idle: idle,
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\nUnlocked. Serving %d files in memory.\n", srv.FileCount())
	fmt.Fprintf(os.Stderr, "  →  %s\n", srv.URL())
	fmt.Fprint(os.Stderr, "Open this in a private browser window. Ctrl-C to stop")
	if idle > 0 {
		fmt.Fprintf(os.Stderr, " (auto-stops after %s idle)", idle)
	}
	fmt.Fprintln(os.Stderr, ".")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = srv.Run(ctx)
	if err == context.Canceled || err == context.DeadlineExceeded {
		fmt.Fprintln(os.Stderr, "\nStopped; keys wiped.")
		return nil
	}
	return err
}

func unlockMeta(payload []byte) (*scheme.VaultMeta, error) {
	for i := 0; i < passwordAttempts; i++ {
		pw, err := prompt.Password("Password: ")
		if err != nil {
			return nil, err
		}
		meta, err := scheme.OpenVaultMeta(payload, pw)
		wipe(pw)
		if err == nil {
			return meta, nil
		}
		fmt.Fprintln(os.Stderr, "  ! wrong password")
	}
	return nil, fmt.Errorf("too many wrong password attempts")
}

func handshake(meta *scheme.VaultMeta, list *wordcodec.List) (map[string][]byte, error) {
	ePriv, challenge, err := scheme.NewChallenge()
	if err != nil {
		return nil, err
	}
	words, err := list.Encode(challenge)
	if err != nil {
		return nil, err
	}
	fmt.Fprintln(os.Stderr, "\nRead this challenge to your warden:")
	printWords(words)

	for i := 0; i < passwordAttempts; i++ {
		resp, err := prompt.ReadPhrase("\nEnter the 12-word response from warden", list, 12)
		if err != nil {
			return nil, err
		}
		files, err := meta.OpenAssets(ePriv, resp)
		wipe(resp)
		if err == nil {
			return files, nil
		}
		fmt.Fprintln(os.Stderr, "  !", err)
	}
	return nil, fmt.Errorf("unlock failed")
}

// printWords lays the phrase out numbered, four per line.
func printWords(words []string) {
	var b strings.Builder
	for i, w := range words {
		fmt.Fprintf(&b, "%2d.%-12s", i+1, w)
		if (i+1)%4 == 0 {
			b.WriteByte('\n')
		}
	}
	if len(words)%4 != 0 {
		b.WriteByte('\n')
	}
	fmt.Fprint(os.Stderr, b.String())
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
