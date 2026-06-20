# vaultwright — encrypted embedded static-file server

**Date:** 2026-06-14
**Status:** Design complete, ready to implement

## 1. Goal

A Go tool that serves a folder of static files where the files are **embedded in
a single binary** and **encrypted at rest** so the binary is not scannable (you
can't tell *what* assets are inside). Serving requires two factors: a **password**
and a **fresh asymmetric challenge–response** with a separate "responder" binary
held on a trusted machine. After unlock, files are served from memory on a random
loopback port.

## 2. Threat model

- **Full binary access.** The attacker can copy, run, disassemble, and patch the
  distributed `vault` binary. Therefore *nothing in `vault` may be sufficient to
  decrypt*, and any yes/no gate is assumed patchable.
- Consequence: the decryption key must require secrets **not present in `vault`** —
  the password (in the operator's head) and the responder's private key.
- **In scope:** hiding asset *content and type*; resisting a leaked password
  (second factor still protects); resisting replay of an observed response.
- **Out of scope:** hiding the *existence* of encrypted data (entropy analysis can
  still see a high-entropy blob — steganography is a non-goal); a compromised
  trusted machine that holds `warden`.

## 3. Architecture — 3 binaries

| Binary | Holds | Role |
|--------|-------|------|
| **`vaultwright`** | nothing (stateless) | Builder. `vaultwright seal <dir>` mints a fresh keypair, encrypts assets, emits `vault` + `warden`, then forgets the keypair. Carries the two stubs. |
| **`vault`** | `pk` + encrypted assets | The server you run/distribute. Public material only. |
| **`warden`** | `sk` + responder logic | The second factor. Kept on a trusted machine. Paste challenge → prints response. |

Each `vaultwright seal` produces a **fresh keypair** ⇒ per-vault isolation. Compromising
one `warden` exposes only its matching `vault`. No shared keystore, no global secret.

```
vaultwright seal ./assets -o myapp
   → myapp.vault     (run / distribute)
   → myapp.warden    (keep on trusted machine — the 2nd factor)
```

Initial target platform: **darwin/arm64 only** (one stub each for vault + warden).

## 4. Crypto design

### Key hierarchy (both factors strictly required)

```
P    = Argon2id(password, salt)                  # factor 1: operator
sk, pk                                           # fresh X25519 keypair per seal
S    = HKDF(sk, "vaultwright/asset-share/v1")       # 16 bytes; factor 2 lives in warden
K_a  = HKDF(S ‖ P)                               # asset key
assets_ct = XChaCha20-Poly1305(K_a, files)
```

`vault` stores `pk`, `salt`, `assets_ct`. It can compute `P` but not `S`, so it
cannot derive `K_a` alone. `warden` embeds `sk`, so it (and only it) can reproduce `S`.

### Unlock handshake (fresh, replay-proof, every unlock)

1. `vault` prompts password → computes `P`.
2. `vault` generates an ephemeral X25519 keypair `(e_sk, e_pk)`; shows `e_pk`
   as a **24-word** BIP39 phrase (the *challenge*).
3. Operator enters the 24 words into `warden`. `warden` computes
   `shared = DH(sk, e_pk)`, returns `S ⊕ HKDF(shared)` as a **12-word** phrase
   (the *response*).
4. Operator enters the 12 words into `vault`. `vault` recovers `S` via
   `DH(e_sk, pk)`, derives `K_a = HKDF(S ‖ P)`, decrypts.
5. Wrong password **or** wrong/garbled response ⇒ asset AEAD tag fails ⇒ rejected.

**Properties:** leaked password alone is useless (needs `S`); stolen `vault` alone
is useless (needs `warden`); `e_pk` is fresh each time so a captured response can't
be replayed; the responder is authenticated implicitly (wrong `S` ⇒ decrypt fails).
Why codes aren't shorter: an unforgeable, non-replayable asymmetric exchange needs
~256-bit ephemeral + ~128-bit wrapped share; word-encoding makes that typeable, not
short. Residual risk: `warden` is an oracle for `S` (still needs the password too).

## 5. On-disk layout & unscannability

`vault` embedded region — **only the salt is plaintext**, no magic bytes / headers:

```
[ salt ]      16 random bytes (plaintext; needed to derive P)
[ meta_ct ]   AEAD(P,   { pk, wordlist })     # opaque; needs password
[ asset_ct ]  AEAD(K_a, files)                # opaque; needs password + handshake
```

- The BIP39 wordlist is embedded **encrypted** (it's the biggest fingerprint — a
  plaintext list would reveal seed-phrase crypto via `strings`). No chicken-and-egg:
  it's only needed for the handshake, which is after password entry.
- `pk` is encrypted too (uniform opaque region + prevents linking vaults to a builder).
- Boundaries found by fixed sizes; the app reads its own executable for the blob.
- `warden` embeds `sk` obfuscated (or AEAD-encrypted under an optional warden
  passphrase, empty by default). Same opaque treatment, but possession-based.

## 6. Build mechanism (stub append)

`vaultwright` ships with precompiled **vault-stub** and **warden-stub** (darwin/arm64).
`vaultwright seal`:
1. Prompt password (twice); optional `--warden-pass`.
2. Generate `(sk, pk)`, derive `S`, `K_a`; build `salt ‖ meta_ct ‖ asset_ct`.
3. Copy vault-stub, append the vault blob + small length trailer → `*.vault`.
4. Copy warden-stub, append the warden blob (`sk`, wordlist) + trailer → `*.warden`.
5. Zero `sk`, `pk`, `K_a`, `S` from memory.

At runtime each stub opens `os.Executable()`, reads the trailer, loads its blob.
Stubs built with `-ldflags="-s -w"` to trim generic Go fingerprints.

v1 emits darwin/arm64 only. Multi-target (independent vault/warden targets,
on-demand stub download) is specified in **§13**.

## 7. Runtime serving (`vault`, after unlock)

- Decrypt `asset_ct` into an in-memory `fs.FS`; serve via `http.FileServer`.
  **No plaintext to disk.** Wipe `K_a` and buffers on exit.
- Bind `127.0.0.1:0` (loopback only) ⇒ random port.
- Serve under an unguessable **path-key** segment: `http://127.0.0.1:<port>/<path-key>/`;
  bare/other paths → 404. (`--no-path-key` to disable.) The path-key is a high-entropy
  random URL segment acting as a capability — it keeps other local processes that
  probe the port from reaching the assets.
- **Entry document & routing.** Directory root serves the entry-point document
  (default `index.html`, override with `--entry-point <file>`). The printed
  path-key root URL therefore lands directly on the entry-point. Unmatched paths
  return 404 by default; with `--fallback`, unmatched **non-file** paths serve the
  entry-point (200) so SPA client-side routing survives refresh/deep links.
- `Cache-Control: no-store` (pair with "open in a private window" hint).
- Idle auto-shutdown, default 15 min, `--idle` flag (`0` = never).

```
Unlocked. Serving 42 files in memory.
  →  http://127.0.0.1:53847/c8f3a91b/
Open in a private browser window. Ctrl-C to stop (auto-stops after 15m idle).
```

## 8. CLIs

```
vaultwright seal <dir> [-o name] [--warden-pass]   # → name.vault + name.warden
vault   (the produced binary)
   --idle 15m        # idle auto-shutdown (0 = never)
   --port N               # override random port
   --addr 127.0.0.1       # override bind address
   --no-path-key          # drop the URL path-key segment
   --entry-point index.html  # directory document served at the root
   --fallback             # serve entry-point for unmatched non-file routes (SPA)
warden  (the produced binary)                # paste challenge → print response
   (prompts for warden-pass only if one was set at seal time)
```

Interactive word prompts in `vault` and `warden`: BIP39 prefix autocomplete +
checksum validation (typos flagged before any crypto runs).

## 9. Word encoding

- **BIP39 English (2048 words, 11 bits/word)**, reuse its checksum scheme.
- `e_pk` 32 B → 24 words; `S` 16 B → 12 words.
- Wordlist never in plaintext in any distributed binary.

## 10. Testing

- **Unit:** seal→unlock happy path; wrong password rejected; wrong/garbled response
  rejected; byte-flip tamper in `meta_ct`/`asset_ct` fails; word codec round-trip +
  checksum detects single-word errors + rejects unknown words (**fuzz** the codec);
  replay (old response vs fresh `e_pk`) fails.
- **Integration:** `vaultwright seal` a fixture → drive full handshake programmatically →
  `vault` serves → HTTP GET matches original; bare port → 404; idle timeout fires.
- **Unscannability guard:** assert produced `vault` bytes contain no BIP39 words and
  none of the fixture filenames/content.

## 11. Implementation steps

Status: **all complete** (2026-06-14). `go test ./...` passes; full ceremony
verified end-to-end through the sealed binaries (challenge → warden → response →
HTTP fetch).

- [x] Scaffold Go module + layout: `cmd/vaultwright`, `cmd/vault`, `cmd/warden`,
      `internal/{cryptocore,wordcodec,blob,serve,archive,scheme,prompt,builtin}`. `Makefile`.
- [x] `internal/cryptocore`: Argon2id, HKDF (stdlib `crypto/hkdf`), X25519 DH,
      XChaCha20-Poly1305; `K_a` derivation; handshake. Unit + tamper + replay tests.
- [x] `internal/wordcodec`: BIP39 encode/decode + checksum + prefix expansion
      (`Normalize`). Round-trip + fuzz tests (found & fixed an out-of-range panic).
- [x] `internal/blob`: `salt ‖ uint32(len meta) ‖ meta ‖ asset` framing + 8-byte
      length trailer; append-to-executable (`WriteSealedBytes`) and `ReadSelf`.
- [x] `internal/archive`: tar bundle/extract of the asset dir (in memory).
- [x] `internal/scheme`: ties it together — `Seal`, `OpenVaultMeta`, `NewChallenge`,
      `OpenAssets`, `OpenWarden`, `Respond`. End-to-end tests incl. unscannability guard.
- [x] `internal/serve`: in-memory file map, loopback random port, path-key segment,
      entry-point + `--fallback`, `no-store`, idle shutdown. Routing tests.
- [x] `internal/prompt`: no-echo password + word-phrase entry (prefix expansion, checksum).
- [x] `vault` stub: load blob → password (3 tries) → challenge → response → serve.
- [x] `warden` stub: load `sk` (optional passphrase) → challenge prompt → response.
- [x] `vaultwright seal`: keypair gen, encrypt, append blobs to both stubs, zeroize.
- [x] Build wiring: compile vault/warden stubs (darwin/arm64, `-s -w`), embed into
      `vaultwright` via `internal/builtin`. `make` target with stub-before-vaultwright ordering.
- [x] Integration test + unscannability guard (`TestPayloadUnscannable`).
- [x] README: ceremony walkthrough, flags, security model, limitations.

## 12. Future enhancements (out of v1)

- Multi-target builds — **specified in §13** (hybrid stub registry + download).
- Optional `warden` passphrase hardening via OS keychain.
- macOS Keychain / Secure Enclave option for `sk`.
- Hardware-key (FIDO2 `hmac-secret`) factor as an alternative to the word handshake.

## 13. Multi-target builds & distribution (hybrid stub registry)

### Key insight: the payload is platform-independent

The keypair and the payload (`salt ‖ meta_ct ‖ asset_ct`) are pure data — only the
*stub* differs per OS/arch. So one `seal` makes **one keypair + one vault-payload +
one warden-payload**, then stamps each payload onto however many stubs you ask for.
vault and warden never interoperate as binaries (they talk through the human typing
words), so their targets are **fully independent and multi-valued**: e.g. vault for
`windows/amd64` + `linux/arm64`, warden for `darwin/arm64`, all sharing one keypair,
all interoperable.

### CLI

```
vaultwright seal ./site -o demo \
    --vault-target windows/amd64 --vault-target linux/arm64 \
    --warden-target darwin/arm64
# → demo.vault-windows-amd64.exe   demo.vault-linux-arm64   demo.warden-darwin-arm64
```

Repeatable `--vault-target` / `--warden-target`, each defaulting to the host. `.exe`
auto-appended for `windows`. One keypair behind all outputs.

### Stub registry (matrix = data, not code)

Stubs are indexed by role × os × arch and embedded as a *directory*, so adding a
platform is dropping a file, not changing code:

```
internal/builtin/stubs/
  vault/   darwin_arm64.stub  linux_arm64.stub  windows_amd64.stub …
  warden/  darwin_arm64.stub  …
//go:embed stubs   →  var stubsFS embed.FS
```

### Hybrid provisioning (recommended)

Keep `vaultwright` small **and** safe:

- **Embed only the host-platform** vault+warden stubs → the common same-platform
  seal works fully offline, zero downloads.
- **Embed a full-matrix checksum manifest** (SHA-256 per stub, a few KB) — this is
  the **trust root**. Stubs are downloaded on demand from the versioned GitHub
  release and verified against the embedded hash *before use*. A tampered/MITM'd
  download is rejected, so transport need not be trusted; `vaultwright`'s own
  integrity gates everything.
- **Cache** verified stubs under `~/.cache/vaultwright/stubs/<version>/<role>/<os>_<arch>.stub`.

Resolver order for `resolve(role, os, arch)`:

```
1. --stub-dir / $VAULTWRIGHT_STUBS         (local mirror / air-gap)
2. embedded stubs/<role>/<os>_<arch>.stub  (host platform)
3. local cache                              (~/.cache/vaultwright/stubs/<ver>/…)
4. download from release → verify sha256 against embedded manifest → cache
5. error: "no stub for <os>/<arch>; run `vaultwright fetch-stubs` or pass --stub-dir"
```

### Version pinning (correctness + security)

A stub's blob-reading code must match `vaultwright`'s blob-writing format, so
`vaultwright` only ever downloads stubs **for its own embedded build version**
(`vX.Y.Z`). Never mix versions; pinning to the embedded version enforces this.

### CI release pipeline (ordering resolves the chicken-and-egg)

`vaultwright` embeds hashes *of* the stubs, so stubs are built first:

```
tag vX.Y.Z → GitHub Actions:
  1. matrix cross-compile cmd/vault, cmd/warden → stubs/<role>/<os>_<arch>.stub  (-trimpath -s -w)
  2. generate SHA256SUMS manifest
  3. build vaultwright embedding (manifest + version "vX.Y.Z" + host stubs)
  4. upload stubs + manifest + vaultwright binaries to release vX.Y.Z
```

Reproducible builds (`-trimpath`) so published hashes are independently verifiable.

### Offline support

- Host target always works embedded (no network).
- `vaultwright fetch-stubs [--all | <targets>]` pre-populates the cache before going offline.
- `--stub-dir` / `$VAULTWRIGHT_STUBS` points at a local/air-gapped mirror.

### Portability & signing

- The append-overlay trick works on **Mach-O, ELF, and PE** — all ignore trailing
  bytes after their declared structures. `os.Executable` self-read is portable.
- Appending an overlay **invalidates code signatures** (macOS hardened runtime,
  Windows Authenticode). If signing, sign each *output* after sealing, never the stub.

### Implementation steps (multi-target)

- [ ] Stub registry: `internal/builtin` embeds `stubs/` (embed.FS) + host stubs;
      `Resolve(role, os, arch)` with the order above.
- [ ] Checksum manifest: embed `SHA256SUMS` + build version; verify after download.
- [ ] Downloader + cache (`~/.cache/vaultwright/...`), HTTPS, hash-gated.
- [ ] CLI: repeatable `--vault-target`/`--warden-target`, `.exe` suffix, output naming,
      `--stub-dir`, `fetch-stubs` subcommand.
- [ ] Makefile: `VAULT_TARGETS` / `WARDEN_TARGETS` matrix → `stubs/`; manifest gen.
- [ ] GitHub Actions release workflow (steps above); `-trimpath` reproducibility.
