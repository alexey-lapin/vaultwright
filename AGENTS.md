# AGENTS.md — working notes for AI agents

Operational guide for working in this repo. User-facing docs are `README.md` and
`SECURITY.md`; the **full design + rationale** is `docs/plans/2026-06-14-vaultwright.md`
(referred to as "the plan"; §-numbers below point into it). Keep plan references out of
the user-facing docs — put them here.

## What this is

`vaultwright` builds an encrypted, embedded static-file server. Three binaries:
- **`vaultwright`** (`cmd/vaultwright`) — the builder CLI you run. `vaultwright seal`.
- **`vault`** (`cmd/vault`) — stub for the produced server binary (public key + assets).
- **`warden`** (`cmd/warden`) — stub for the produced responder (private key; 2nd factor).

Module path: `github.com/alexey-lapin/vaultwright`. Go 1.26 (uses stdlib `crypto/hkdf`).
Crypto/handshake design and threat model are in plan §2–§5; multi-target stub
distribution is plan §13.

## Layout

```
cmd/{vaultwright,vault,warden}
internal/cryptocore   Argon2id, HKDF, X25519, XChaCha20-Poly1305, the key hierarchy + handshake
internal/scheme       ties primitives together: Seal / OpenVaultMeta / handshake / OpenWarden
internal/wordcodec    BIP39 encode/decode (+ checksum, prefix Normalize). NO embedded wordlist.
internal/blob         append-payload-to-executable container + framing
internal/archive      tar bundle/extract of the asset dir (in memory)
internal/serve        in-memory loopback HTTP (random port, path-key, entry-point, fallback, idle)
internal/prompt       no-echo password + interactive (raw-mode) word entry; line fallback
internal/builtin      embeds english.txt + stubs/<role>/<os>_<arch>.stub + SHA256SUMS + Version
internal/stubs        Resolve(role,os,arch): stub-dir → embedded → cache → verified download
scripts/build-stubs.sh    cross-compile the stub matrix + deterministic dist/SHA256SUMS
scripts/build-release.sh  build per-host CLI embedding host stubs + manifest + version
```

## Build / test / lint

```sh
make                 # build host stubs into internal/builtin/stubs/, then bin/vaultwright
make clean           # reset host stubs to placeholders, remove bin/ and dist/
go test ./...        # all tests pass with placeholder stubs (tests never run sealed binaries)
go vet ./...
gofmt -l cmd internal   # must be empty
make stubs-matrix    # cross-compile the full matrix + manifest into dist/ (release prep)
```

CI (`.github/workflows/ci.yml`) runs gofmt/vet/build/test on ubuntu + macOS.

## ⚠️ Stub files — the #1 footgun

`internal/builtin/stubs/<role>/<os>_<arch>.stub` are **committed as ~42-byte text
placeholders**. `make` overwrites them with real (multi-MB) binaries for the host;
`git update-index --skip-worktree` keeps those local rebuilds from showing as changes.

- **NEVER commit a real stub binary.** Before committing anything touching them, verify:
  `git cat-file -s HEAD:internal/builtin/stubs/vault/darwin_arm64.stub` (should be tiny).
- To (re)set skip-worktree after adding a host placeholder:
  `git update-index --skip-worktree internal/builtin/stubs/*/*.stub`
- **Makefile placeholder strings must contain NO backticks** — in a recipe they become
  shell command substitution. A backtick in `PLACEHOLDER` once made `make clean` run
  `make` and commit a 5.6 MB stub; history had to be purged with filter-branch + force
  push. Keep `PLACEHOLDER` backtick-free.
- `.DS_Store` is gitignored; don't let macOS sweep it into `internal/builtin/stubs/`.

The wordlist and stubs live in `internal/builtin`, which is imported **only** by
`cmd/vaultwright` — never by `cmd/vault`/`cmd/warden`, or the wordlist would leak into a
distributed binary (defeats unscannability; see plan §5). Don't add that import.

## Releases

Trigger: push a `v*` tag, or **Actions → Release → Run workflow** (workflow_dispatch with
a `tag` input), or `gh workflow run release.yml -f tag=vX.Y.Z`.

The workflow (`.github/workflows/release.yml`):
1. `build-stubs.sh` → `dist/stubs/<role>/<os>_<arch>.stub` + `dist/SHA256SUMS`.
2. `build-release.sh` → per-host `dist/vaultwright-<os>-<arch>` embedding that host's stubs
   + manifest + `-X internal/builtin.Version`.
3. Publishes via the **built-in `gh` CLI** (no third-party actions). Stub assets are
   renamed to unique `<role>-<os>_<arch>.stub` (basenames collide otherwise — gh's
   `path#name` sets only the label, not the asset name).

A released CLI embeds only its host stubs; non-host targets are **downloaded** from the
release and verified against the embedded `SHA256SUMS` (the trust root). Dev builds
(`Version == dev`, empty manifest) refuse to download. Repo overridable via
`$VAULTWRIGHT_RELEASE_REPO`; cache at `<user cache>/vaultwright/stubs/<ver>/...`.

## Testing the unlock ceremony

Interactive word entry needs a TTY. For headless end-to-end, drive the sealed `vault`
via a fifo (feed password, scrape the 24-word challenge from its output, run `warden` to
get the 12-word response, feed it back), then `curl` the served file. The piped path uses
the line-based reader in `internal/prompt` (not raw mode). `internal/scheme` also has a
full programmatic ceremony test (`TestSealUnlockEndToEnd`).

## Conventions

- **Commit messages: no `Co-Authored-By` trailer** (or any co-author line).
- Keep `gofmt` clean; prefer small, focused commits.
- Don't reference the plan from `README.md` / `SECURITY.md` — reference it here.
