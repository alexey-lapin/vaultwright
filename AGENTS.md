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
internal/builtin      embeds english.txt + SHA256SUMS + Version; host stubs via -tags embed_stubs (stub_<os>_<arch>.go); stubs/ git-ignored
internal/stubs        Resolve(role,os,arch): stub-dir → embedded → cache → verified download
scripts/build-stubs.sh    cross-compile the stub matrix + deterministic build/SHA256SUMS
scripts/stage-embed.sh    copy built stubs + manifest into internal/builtin/ (GoReleaser before-hook)
scripts/stage-release-assets.sh  flatten stub asset names + SHA256SUMS into build/assets/ (GoReleaser extra_files)
```

## Build / test / lint

```sh
make                 # build host stubs into internal/builtin/stubs/, then bin/vaultwright (-tags embed_stubs)
make clean           # remove bin/, dist/, build/, and the git-ignored internal/builtin/stubs/
go test ./...        # passes with no stubs present (default build embeds none; see embed_stubs below)
go vet ./...
gofmt -l cmd internal   # must be empty
make stubs-matrix    # cross-compile the full matrix + manifest into build/ (release prep)
```

CI (`.github/workflows/ci.yml`) runs gofmt/vet/build/test on ubuntu + macOS.

## Stub files & the `embed_stubs` tag

The compiled stub binaries `internal/builtin/stubs/<role>/<os>_<arch>.stub` are
**git-ignored** — never committed. They're built on demand: `make` compiles the host pair,
the release build (`build-stubs.sh` + `stage-embed.sh`) compiles the whole matrix.

Embedding is gated by the **`embed_stubs`** build tag (files `stub_<os>_<arch>.go`, one per
target, each `//go:embed`ing that platform's stubs):

- **Plain `go build` / `go test` (no tag)** compiles `stub_fallback.go` (empty stubs), so it
  needs **no stub files present** — a fresh clone builds with nothing to set up, and CI
  needs no stubs. `EmbeddedStub` then reports "not embedded" and stubs resolve via download.
- **`-tags embed_stubs`** (added by `make` and by GoReleaser) embeds the host's real stubs;
  the host's stub files must exist first (hence `make stubs` before the CLI build).

Because the stubs are git-ignored, there's no placeholder/skip-worktree/pre-commit dance
anymore — you can't accidentally `git add` a multi-MB stub. (Earlier this was the repo's #1
footgun; a committed 5.6 MB stub once forced a history purge. The tag design removes it.)
Don't add committed stub files or a `//go:embed stubs` directory glob back.

The wordlist and stubs live in `internal/builtin`, which is imported **only** by
`cmd/vaultwright` — never by `cmd/vault`/`cmd/warden`, or the wordlist would leak into a
distributed binary (defeats unscannability; see plan §5). Don't add that import.

## Releases

Releases run **GoReleaser** (`.goreleaser.yaml`; design:
`docs/plans/2026-07-05-goreleaser-migration.md`). GoReleaser compiles the CLIs natively.
Each must embed ONLY its host's stubs — that selection happens at **compile time** via the
`embed_stubs`-tagged `internal/builtin/stub_<os>_<arch>.go` files (GoReleaser sets
`tags: [embed_stubs]`), so it builds every target **in parallel** from an unmutated tree
(no serial `--parallelism 1`, no per-target hooks). GoReleaser also owns archiving,
checksums, the Release, and the Homebrew push.

Two workflows:
- **`release.yml`** — reusable release job. Triggers on a `v*` **tag push** *or*
  `workflow_call` (input `tag`). GoReleaser needs the tag to already exist; both entries
  satisfy that. Runs `goreleaser release --clean`.
- **`release-dispatch.yml`** — manual entry (**Actions → Release (dispatch)**, or
  `gh workflow run release-dispatch.yml`). Pick a `bump` (`-f bump=minor`, default `patch`)
  or force `-f version=vX.Y.Z` (overrides `bump`). The "Resolve version" step computes the
  version and refuses to reuse an existing release/tag, then **pushes the tag** with
  `GITHUB_TOKEN` (which does NOT re-trigger `release.yml`'s `push:tags` — GitHub blocks
  runs from `GITHUB_TOKEN` pushes) and calls `release.yml` via `workflow_call`.

Build pipeline (GoReleaser owns `dist/`; our scripts write intermediates to `build/`):
1. `before.hooks`, in order: `build-stubs.sh` → `build/stubs/<role>/<os>_<arch>.stub` +
   `build/SHA256SUMS`; `stage-embed.sh` copies those real stubs + manifest into the
   git-ignored `internal/builtin/stubs/` + `internal/builtin/SHA256SUMS` so the
   `embed_stubs` build embeds them; `stage-release-assets.sh` → flat
   `build/assets/<role>-<os>_<arch>.stub` + `SHA256SUMS` (basenames collide across roles;
   `extra_files` uploads by basename and can't rename).
2. GoReleaser builds all targets in parallel (`tags: [embed_stubs]`), version baked via
   `-ldflags -X internal/builtin.Version={{ .Tag }}`. The only tree change is the ignored
   `stubs/` dir (invisible to git) + a modified `internal/builtin/SHA256SUMS`; locally
   restore the latter with `git checkout -- internal/builtin/SHA256SUMS`.

GoReleaser then: archives the CLIs (`tar.gz`, `.zip` on Windows; version in the name) +
`checksums.txt`; uploads the flat stub assets + `SHA256SUMS` via `release.extra_files`;
generates notes via `changelog: use: github-native` (honors `.github/release.yml`); and
pushes the formula to `alexey-lapin/homebrew-tap` using the `TAP_GITHUB_TOKEN` secret (a
fine-grained PAT, Contents: RW on that repo — NOT the default token, which can't write
cross-repo). `brews` is deprecated in GoReleaser but still works (so `goreleaser check`
exits non-zero on the deprecation warning — the release itself is fine). Local dry run:
`goreleaser release --snapshot --clean` (then restore the tree as noted above).

A released CLI embeds only its host stubs; non-host targets are **downloaded** from the
release and verified against the embedded `SHA256SUMS` (the trust root). Dev builds
(`Version == dev`, empty manifest) refuse to download. Repo overridable via
`$VAULTWRIGHT_RELEASE_REPO`; cache at `<user cache>/vaultwright/stubs/<ver>/...`.

**`go install` is NOT supported** and must not be advertised: it builds without
`-tags embed_stubs` and with an empty manifest, so it embeds no stubs and can't seal (host
stub absent) and can't download (dev build / no trust-root manifest). Distribute the CLI via
the release binaries (built by GoReleaser in CI); local dev uses `make`.
The CLI `main` is at `cmd/vaultwright` (not the module root).

## Testing the unlock ceremony

Interactive word entry needs a TTY. For headless end-to-end, drive the sealed `vault`
via a fifo (feed password, scrape the 24-word challenge from its output, run `warden` to
get the 12-word response, feed it back), then `curl` the served file. The piped path uses
the line-based reader in `internal/prompt` (not raw mode). `internal/scheme` also has a
full programmatic ceremony test (`TestSealUnlockEndToEnd`).

## Git flow

`main` is protected — **no direct pushes**. All changes land via pull request:

1. Branch from `main` with a `feature/` prefix: `git switch -c feature/<short-slug>`.
2. Commit, push (`git push -u origin HEAD`), open a PR (`gh pr create`).
3. CI (`build-test` on ubuntu + macOS) runs on every PR. It's **advisory** — the ruleset
   requires a PR but not passing checks or approvals (solo repo), so don't merge red.
4. **Squash-merge** — the PR *title* becomes the single commit subject and the
   release-note line, so write it imperative + scoped (e.g. `serve: fix idle race`).
   The branch is auto-deleted on merge. (Squash is the only merge method the repo allows.)
5. **Label the PR** (`enhancement` / `bug` / `documentation` / `dependencies`) so the
   auto-generated release notes (`.github/release.yml`) file it under the right heading;
   `ignore-for-release` keeps a PR out of the notes entirely.

Release notes come from GoReleaser's `changelog: use: github-native`, which calls GitHub's
release-notes API to build "What's Changed" from the merged PRs since the last tag —
categorized per `.github/release.yml`. Good PR titles + labels = good release descriptions.

`main` is enforced by a branch ruleset (`gh api /repos/OWNER/REPO/rulesets`). The Homebrew
formula now lives in the separate `alexey-lapin/homebrew-tap` repo (GoReleaser pushes it
there via `TAP_GITHUB_TOKEN`), so releases no longer touch `main` at all — the old
`release/formula-<ver>` PR dance is gone. The repo-admin role is the one bypass actor, so a
maintainer can still push to `main` directly in a pinch.

## Conventions

- **Commit messages: no `Co-Authored-By` trailer** (or any co-author line).
- Keep `gofmt` clean; prefer small, focused commits.
- Don't reference the plan from `README.md` / `SECURITY.md` — reference it here.
