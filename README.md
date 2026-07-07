# vaultwright

[![CI](https://github.com/alexey-lapin/vaultwright/actions/workflows/ci.yml/badge.svg)](https://github.com/alexey-lapin/vaultwright/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Serve a folder of static files from a **single binary** where the files are
**embedded and encrypted**, so the binary on disk reveals nothing about what's
inside. Unlocking requires **two factors**: a password *and* a fresh
challenge–response with a separate responder binary you keep on a trusted machine.

After unlock the files are served from memory on a random loopback port.

**Status:** released. Multi-target builds with on-demand, hash-verified stub download
work end-to-end.

## Install

**Homebrew (recommended)** — installs from the `alexey-lapin/homebrew-tap` tap. `brew`
downloads without the macOS quarantine attribute, so the CLI runs without a Gatekeeper
prompt:

```sh
brew tap alexey-lapin/tap
brew install vaultwright
```

**Scoop (Windows)** — installs from the `alexey-lapin/scoop-bucket` bucket:

```powershell
scoop bucket add alexey-lapin https://github.com/alexey-lapin/scoop-bucket
scoop install vaultwright
```

**Prebuilt binary** — grab the archive for your os/arch from the
[latest release](https://github.com/alexey-lapin/vaultwright/releases/latest) (assets are
named `vaultwright_<version>_<os>_<arch>.tar.gz`, `.zip` on Windows). These seal for the
host and download + verify other targets on demand. Extract and verify against the release
`checksums.txt` — e.g. with the GitHub CLI, which resolves the latest release for you:

```sh
gh release download -R alexey-lapin/vaultwright \
  -p '*_darwin_arm64.tar.gz' -p checksums.txt   # pick your os/arch
shasum -a 256 -c checksums.txt --ignore-missing  # → "…_darwin_arm64.tar.gz: OK"
tar xzf vaultwright_*_darwin_arm64.tar.gz        # → ./vaultwright
```

> On macOS, a binary downloaded via a **browser** (not `curl`) is quarantined; clear it
> with `xattr -d com.apple.quarantine vaultwright` before running. The same applies to the
> `*.vault` / `*.warden` binaries you hand to others if they save them from a browser,
> email, or AirDrop.

**From source** — builds the host stubs locally (seals host targets; multi-target needs a
release binary, whose embedded SHA-256 manifest authorizes downloads):

```sh
git clone https://github.com/alexey-lapin/vaultwright && cd vaultwright && make   # → bin/vaultwright
```

> `go install` is **not** supported: the working stubs and the trust-root manifest are
> assembled by CI into the release binaries and are not in the committed source, so a
> `go install` build can't seal.

See [SECURITY.md](SECURITY.md) for the threat model and how to report issues.

## Three binaries

| Binary | Holds | Role |
|--------|-------|------|
| `vaultwright` | nothing | Stateless builder. Each `vaultwright seal` mints a fresh keypair and emits the pair below, then forgets the keypair. |
| `*.vault` | public key + encrypted assets | The server you run/distribute. |
| `*.warden` | private key | The **second factor** — keep it on a trusted machine. |

## Build

```sh
make            # builds the host (GOOS/GOARCH) stubs, then bin/vaultwright
```

`vaultwright` embeds the host's vault/warden stubs plus the wordlist; `make` compiles the
stubs first, then builds the CLI with `-tags embed_stubs` so they're baked in. A locally
built CLI seals for the host platform; other targets are downloaded on demand — only a
release binary's embedded SHA-256 manifest authorizes those downloads, so a `make` build
refuses them.

**Stub files & git.** The compiled stubs live at
`internal/builtin/stubs/<role>/<os>_<arch>.stub` and are **git-ignored** — `make` builds
them there on demand. A plain `go build`/`go test` (no `embed_stubs` tag) embeds no stubs
and needs none present, so a fresh clone builds with nothing to set up. `make clean`
removes them.

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
| `--vault-target os/arch` | Vault target platform (repeatable; default: host). |
| `--warden-target os/arch` | Warden target platform (repeatable; default: host). |
| `--stub-dir <dir>` | Resolve stubs from this directory first (offline mirror). |
| `--offline` | Never download stubs (embedded / cache / `--stub-dir` only). |

Targets are independent and may be repeated, e.g. build a Windows + Linux vault with a
macOS warden — all from one keypair:

```sh
vaultwright seal ./site -o demo \
  --vault-target windows/amd64 --vault-target linux/arm64 \
  --warden-target darwin/arm64
# → demo.vault-windows-amd64.exe  demo.vault-linux-arm64  demo.warden-darwin-arm64
```

With explicit targets the outputs are suffixed (and `.exe` is added for Windows);
the plain host default writes `demo.vault` / `demo.warden`. Non-host stubs are
downloaded from the release and verified against the embedded SHA-256 manifest;
pre-fetch them with `vaultwright fetch-stubs --all` (or `fetch-stubs os/arch …`).

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
