# vaultwright

[![CI](https://github.com/alexey-lapin/vaultwright/actions/workflows/ci.yml/badge.svg)](https://github.com/alexey-lapin/vaultwright/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Serve a folder of static files from a **single binary** where the files are
**embedded and encrypted**, so the binary on disk reveals nothing about what's
inside. Unlocking requires **two factors**: a password *and* a fresh
challenge–response with a separate responder binary you keep on a trusted machine.

After unlock the files are served from memory on a random loopback port.

**Status:** v1 (darwin/arm64) works end-to-end. Multi-target builds and on-demand
stub download are designed in `docs/plans/2026-06-14-vaultwright.md` §13, not yet built.

## Install

```sh
# From source (host platform):
make                    # → bin/vaultwright
# Or, once a version is tagged:
go install github.com/alexey-lapin/vaultwright/cmd/vaultwright@latest
```

See [SECURITY.md](SECURITY.md) for the threat model and how to report issues.

## Three binaries

| Binary | Holds | Role |
|--------|-------|------|
| `vaultwright` | nothing | Stateless builder. Each `vaultwright seal` mints a fresh keypair and emits the pair below, then forgets the keypair. |
| `*.vault` | public key + encrypted assets | The server you run/distribute. |
| `*.warden` | private key | The **second factor** — keep it on a trusted machine. |

## Build

```sh
make            # builds the darwin/arm64 stubs, then bin/vaultwright
```

`vaultwright` embeds the two stubs and the wordlist; the stubs are compiled first
because `vaultwright` bakes them in. (Target is darwin/arm64 for now.)

**Stub files & git.** `internal/builtin/{vault,warden}.stub` are committed as
small text *placeholders* so `go build ./...` works on a fresh clone; `make`
overwrites them with real (multi-MB) compiled binaries. To keep those local
rebuilds from showing up as changes, mark them skip-worktree after cloning:

```sh
git update-index --skip-worktree internal/builtin/vault.stub internal/builtin/warden.stub
```

Never commit the built stubs. `make clean` restores the placeholders.

## Use

```sh
# 1. Seal a directory (prompts for a password, twice):
bin/vaultwright seal ./site -o demo
#   → demo.vault   (run / distribute)
#   → demo.warden  (keep on your trusted machine)

# 2. Run the server:
./demo.vault
#   Password: ********
#   Read this challenge to your warden:
#    1.define   2.word     3.tape    ...        (24 words)

# 3. On your trusted machine, answer it:
./demo.warden
#   Enter the challenge from vault (24 words): define word tape ...
#   Type this response back into vault:
#    1.cash     2.primary  3.young   ...        (12 words)

# 4. Back in the vault, type the 12 words. It then prints:
#   Unlocked. Serving 3 files in memory.
#     →  http://127.0.0.1:53847/c8f3a91b/
```

Open the printed URL in a private browser window. Prefixes are accepted when
typing words (e.g. `aban` → `abandon`); a mistyped word is caught by the checksum.

### `vault` flags

| Flag | Default | Meaning |
|------|---------|---------|
| `--idle` | `15m` | Auto-shutdown after this much inactivity (`0` = never). |
| `--port` | random | Fixed TCP port. |
| `--addr` | `127.0.0.1` | Bind address (loopback only by default). |
| `--no-path-key` | off | Drop the unguessable URL path-key segment. |
| `--entry-point` | `index.html` | Directory document served at the root. |
| `--fallback` | off | Serve the entry-point for unmatched non-file routes (SPA refresh/deep links). |

### `vaultwright seal` flags

| Flag | Meaning |
|------|---------|
| `-o <name>` | Output base name (default: the assets dir name). |
| `--warden-pass` | Also protect the warden binary with a passphrase (prompted). |

## How it works

```
P    = Argon2id(password, salt)              factor 1 (you)
sk,pk                                        fresh X25519 keypair per seal
S    = HKDF(sk, "…asset-share…")             16 bytes; factor 2 lives in warden
K_a  = HKDF(S || P)                          asset key
assets = XChaCha20-Poly1305(K_a, files)
```

Unlock: `vault` derives `P` from the password, then runs a fresh ephemeral
handshake — challenge = its ephemeral public key (24 words), response =
`S ⊕ HKDF(shared)` from the warden (12 words). It recovers `S`, derives `K_a`,
and decrypts. A wrong password fails at the metadata step; a wrong response fails
at asset decryption.

On disk the vault contains only a plaintext random salt followed by opaque
ciphertext (assets, and a password-encrypted blob holding the public key and the
wordlist). No magic headers, no plaintext wordlist.

## Security model & limits

- **Threat model:** full binary access — the attacker can run, disassemble, and
  patch `vault`. Nothing in `vault` alone can decrypt.
- **Two factors, both required:** a leaked password is useless without the warden;
  a stolen `vault` is useless without both.
- **Replay-proof:** the handshake is fresh each unlock, so a captured response is
  useless next time. (This is why the codes are word phrases, not short PINs — a
  non-replayable asymmetric exchange can't fit in a handful of characters.)
- **`warden` is the factor:** whoever has the `warden` binary + the password can
  unlock. Protect it like a hardware key; optionally add `--warden-pass`.
- **In scope:** hiding asset *content and type*. **Out of scope:** hiding that
  encrypted data exists at all (entropy analysis still sees a high-entropy blob);
  a compromised trusted machine.

## Tests

```sh
go test ./...
go test ./internal/wordcodec -run=x -fuzz=FuzzRoundTrip -fuzztime=10s
```
