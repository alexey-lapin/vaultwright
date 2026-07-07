# Migrate the release process to GoReleaser

Status: implemented (build-tag variant; see "Why build-tagged embeds")
Date: 2026-07-05
Related: the main plan is `docs/plans/2026-06-14-vaultwright.md` (§13 = multi-target stub
distribution, the constraint that shapes this migration).

## Goal

Replace the hand-rolled release scripting with GoReleaser to get **fewer custom moving
parts** and **native Homebrew automation**. Specifically, delete the `gh release create`
+ stub-rename loop and the `release/formula-<ver>` PR-merge dance, and let GoReleaser own
packaging, the GitHub release, and the tap push.

Non-goals: no change to the crypto/handshake, the stub download/verify protocol, or the
`internal/stubs` runtime behavior (the per-host embed selection moves to compile-time build
tags in `internal/builtin`, but the embed-host / download-others behavior is unchanged).
GoReleaser compiles the CLIs and owns packaging/publishing (see "Why build-tagged embeds").

## The constraint that shapes everything

Each per-host `vaultwright` CLI must embed **different bytes**: only its host's
`vault`/`warden` stubs, plus the full `SHA256SUMS` manifest and the version via
`-ldflags`. GoReleaser's normal model — one source tree, compile a matrix — can't express
"embed a different file per target" if that selection depends on the on-disk contents of a
shared directory.

### Why build-tagged embeds (the journey: prebuilt → per-target hook → build tags)

Two dead ends came first:
1. The design was approved around GoReleaser's `builder: prebuilt` (keep the old scripts,
   let GoReleaser adopt their output). **`prebuilt` is GoReleaser Pro-only** — the OSS
   binary errors `field prebuilt not found`.
2. Native builds with a **per-target build pre-hook** that staged each host's stubs into
   `internal/builtin/stubs` right before its `go build`. This works, but all targets share
   that one directory, so builds had to be **serialized** (`--parallelism 1`) with pre/post
   staging+restore hooks.

The shipped approach moves the per-host selection to **compile time via Go build tags**,
gated by the **`embed_stubs`** tag. `internal/builtin/stub_<os>_<arch>.go` (one per target)
carries `//go:build embed_stubs && <os> && <arch>` and `//go:embed`s that platform's stubs
into `vaultStub`/`wardenStub`; `EmbeddedStub` returns them only when the requested target is
`runtime.GOOS/GOARCH`. A tagged build (`make`, GoReleaser's `tags: [embed_stubs]`) therefore
embeds only the host's stubs for each target — **no on-disk mutation during the build**, so
GoReleaser builds all targets **in parallel** with a vanilla `builds:` block (no hooks, no
`--parallelism 1`). The `before` hooks stage the real stubs into `internal/builtin/` once,
up front. `scripts/build-release.sh` is deleted (GoReleaser compiles + bakes the version).

The `embed_stubs` gate is what lets the **stubs directory be git-ignored** (no committed
files at all): a plain `go build`/`go test` (no tag) compiles `stub_fallback.go` (empty
stubs) and needs no stub files present — fresh clones and CI build with nothing to set up.
The same `stub_fallback.go` also covers `embed_stubs` builds for platforms outside the
matrix (its tag is `!embed_stubs || !(matrix-platform)`). This
retires the old placeholder-stub apparatus wholesale: **0 committed stubs (was 12/2),
no `skip-worktree`, no `pre-commit` stub guard, no `make install-hooks`.**

Rejected alternatives:
- **Buy GoReleaser Pro** for `prebuilt` (the originally approved shape) — a paid
  subscription for a solo OSS project.
- **Embed-all / always-download** — a runtime/threat-model change (defeats "embed host,
  download the rest"). Out of scope. (Build tags keep that behavior exactly.)

## Load-bearing names (do NOT change)

- **Stub release assets** must be exactly `<role>-<os>_<arch>.stub` (e.g.
  `vault-darwin_arm64.stub`). `internal/stubs/download.go` builds those URLs and verifies
  them against the embedded `SHA256SUMS`. GoReleaser uploads them via `release.extra_files`.
- **`SHA256SUMS`** is the stub trust root, embedded in the CLI. It stays a build artifact
  from `build-stubs.sh` and is also uploaded as a release asset.

CLI asset names are **not** load-bearing (no code downloads the CLI), so they move to
idiomatic GoReleaser archives and the docs are updated to match.

## Architecture

### Workflow topology — reusable `release.yml` + dispatch caller

`release.yml` becomes a **reusable** workflow with two entry points:

```
release.yml            # the release job (GoReleaser)
  on:
    push:  { tags: ['v*'] }            # entry 1: human/PAT tag push
    workflow_call:                      # entry 2: called by another workflow
      inputs:  { tag: {type: string, required: true} }
      secrets: { TAP_GITHUB_TOKEN: {required: true} }

release-dispatch.yml   # manual entry point
  on: workflow_dispatch: { inputs: { bump, version } }
```

Version handling and the no-double-release guarantee:

- **Tag push**: `release.yml` runs directly; `TAG = github.ref_name`; tag already exists.
- **Manual**: `release-dispatch.yml` runs the existing "Resolve version" logic
  (bump/exact/validate/refuse-existing), creates and **pushes a real tag** with the default
  `GITHUB_TOKEN`, then calls `release.yml` via `workflow_call` with `tag: vX.Y.Z`.

Pushing the tag with `GITHUB_TOKEN` does **not** re-trigger `release.yml`'s `on: push:
tags` — GitHub's documented rule: "if a workflow run pushes code using the repository's
`GITHUB_TOKEN`, a new workflow will not run even when the repository contains a workflow
configured to run when `push` events occur" (exceptions: only `workflow_dispatch` /
`repository_dispatch` and some `pull_request` types). So the manual path triggers the
release exactly once, via the explicit `workflow_call`. Both paths hand GoReleaser a real,
already-pushed tag.

Sketch:

```yaml
# release-dispatch.yml
on:
  workflow_dispatch:
    inputs:
      bump:    { type: choice, options: [patch, minor, major], default: patch }
      version: { required: false, default: "" }
jobs:
  tag:
    runs-on: ubuntu-latest
    outputs: { version: ${{ steps.resolve.outputs.version }} }
    steps:
      - uses: actions/checkout@v7
        with: { fetch-depth: 0 }         # need tags/history to resolve + refuse-existing
      - id: resolve
        run: |                            # existing bump/exact/validate/refuse-existing logic
          ...
          echo "version=$VERSION" >> "$GITHUB_OUTPUT"
      - run: |
          git tag "$VERSION" && git push origin "$VERSION"   # GITHUB_TOKEN → no recursion
  release:
    needs: tag
    permissions: { contents: write }   # caps the nested goreleaser job; else it startup-fails
    uses: ./.github/workflows/release.yml
    with:    { tag: ${{ needs.tag.outputs.version }} }
    secrets: inherit
```

```yaml
# release.yml
on:
  push: { tags: ['v*'] }
  workflow_call:
    inputs:  { tag: { type: string, required: true } }
    secrets: { TAP_GITHUB_TOKEN: { required: true } }
jobs:
  goreleaser:
    runs-on: ubuntu-latest
    permissions: { contents: write }
    steps:
      - uses: actions/checkout@v7
        with: { ref: ${{ inputs.tag || github.ref }}, fetch-depth: 0 }
      - uses: actions/setup-go@v6
        with: { go-version: "1.26" }
      - uses: goreleaser/goreleaser-action@v7
        with: { version: "~> v2", args: "release --clean" }
        env:
          GITHUB_TOKEN:     ${{ secrets.GITHUB_TOKEN }}      # release on this repo
          TAP_GITHUB_TOKEN: ${{ secrets.TAP_GITHUB_TOKEN }}  # formula push to homebrew-tap
```

### `.goreleaser.yaml`

GoReleaser owns `dist/`; our scripts write intermediates to `build/` (GoReleaser rejects a
non-empty `dist/` after the before-hooks otherwise).

```yaml
version: 2

before:
  hooks:
    - ./scripts/build-stubs.sh                 # 12 stub binaries + build/SHA256SUMS (trust root)
    - ./scripts/stage-embed.sh                 # copy real stubs + manifest → internal/builtin/
    - ./scripts/stage-release-assets.sh        # flat stub names + SHA256SUMS → build/assets/

builds:
  - id: vaultwright                            # vanilla: build tags pick each host's stubs
    main: ./cmd/vaultwright
    binary: vaultwright
    goos:   [darwin, linux, windows]
    goarch: [amd64, arm64]                       # full matrix incl. windows/arm64
    flags: [-trimpath]
    ldflags:
      - -s -w -X github.com/alexey-lapin/vaultwright/internal/builtin.Version={{ .Tag }}

archives:
  - id: vaultwright
    ids: [vaultwright]
    formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
    name_template: >-
      {{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}   # vaultwright_0.0.4_darwin_arm64.tar.gz

checksum:
  name_template: checksums.txt                 # CLI-archive checksums; NOT the stub trust root

changelog:
  use: github-native                           # GitHub's notes API → honors .github/release.yml

release:
  extra_files:
    - glob: build/assets/*.stub                # vault-darwin_arm64.stub, warden-windows_arm64.stub, …
    - glob: build/assets/SHA256SUMS            # stub trust root, downloaded + verified by the CLI

brews:                                          # deprecated but functional; `homebrew_casks` would change install UX
  - repository:
      owner: alexey-lapin
      name:  homebrew-tap
      token: "{{ .Env.TAP_GITHUB_TOKEN }}"     # fine-grained PAT, NOT the default token
    directory:   Formula                       # → homebrew-tap/Formula/vaultwright.rb
    homepage:    "https://github.com/alexey-lapin/vaultwright"
    description: "Build an encrypted, embedded static-file server from a single binary"
    license:     "Apache-2.0"
    skip_upload: auto                          # skip formula push for prereleases
    install: |
      bin.install "vaultwright"
    test: |
      assert_match "vaultwright", shell_output("#{bin}/vaultwright version")

scoops:                                         # Windows: manifest → root of scoop-bucket
  - repository:
      owner: alexey-lapin
      name:  scoop-bucket
      branch: main
      token: "{{ .Env.SCOOP_GITHUB_TOKEN }}"   # fine-grained PAT scoped to scoop-bucket
    homepage:    "https://github.com/alexey-lapin/vaultwright"
    description: "Build an encrypted, embedded static-file server from a single binary"
    license:     Apache-2.0
    skip_upload: auto                          # no `directory` — Scoop wants root manifests
```

Notes:
- `github-native` disables GoReleaser's own grouping/sort — fine, categorization lives in
  `.github/release.yml` (kept).
- `windows/arm64` is in the CLI matrix; `brews` auto-skips non-darwin/linux, so no Windows
  in Homebrew. `scoops` uses the Windows `.zip` archives → a manifest with `64bit` (amd64)
  + `arm64` blocks.
- No build hooks and no `--parallelism 1`: build tags select each host's stubs at compile
  time, so the shared `internal/builtin/stubs` isn't mutated per target and builds run in
  parallel. The version is baked by GoReleaser's own `-ldflags` (`{{ .Tag }}` → `vX.Y.Z`).
- `windows/arm64` is in the CLI matrix; `brews` auto-skips non-darwin/linux, so no Windows
  in Homebrew.

### `internal/builtin` changes

- `stub_<os>_<arch>.go` (×6) — `//go:build embed_stubs && <os> && <arch>`, `//go:embed`s
  that platform's stubs into `vaultStub`/`wardenStub`. `stub_fallback.go`
  (`!embed_stubs || !(matrix-platform)`) provides empty stubs so untagged and off-matrix
  builds compile with no stub files.
- `builtin.go` — drops the `embed.FS` directory embed and `IsPlaceholder`; `EmbeddedStub`
  returns the host stub only when `goos/goarch == runtime.GOOS/GOARCH` and it's non-empty.
- `internal/stubs/stubs.go` — drops the now-dead placeholder check.
- **Zero committed stub files** — `internal/builtin/stubs/` is git-ignored, built on demand.
  No `skip-worktree`, no `pre-commit` guard.

### Scripts

- `scripts/build-stubs.sh` — add `windows/arm64` to the default matrices; default `OUT`
  changed `dist` → `build` (GoReleaser owns `dist/`).
- `scripts/build-release.sh` — **deleted**; GoReleaser compiles the CLIs natively now.
- `scripts/stage-embed.sh` — **new.** `before` hook: copies all built
  `build/stubs/**` + `build/SHA256SUMS` into `internal/builtin/` (once, up front) so the
  build tags embed the real stubs. No per-target work → parallel-safe.
- `scripts/stage-release-assets.sh` — **new.** Replaces the rename loop that was inline in
  `release.yml`: `extra_files` uploads by basename and can't rename, and `vault/…`+`warden/…`
  basenames collide. Copies each `build/stubs/<role>/<os>_<arch>.stub` to
  `build/assets/<role>-<os>_<arch>.stub` and copies `build/SHA256SUMS` to `build/assets/`.

## Package buckets: dedicated repos + tokens

The package definitions live in separate, pre-created, unprotected repos so GoReleaser
commits straight to their default branch — no PR-merge workaround, no ruleset bypass:

- **Homebrew** → `alexey-lapin/homebrew-tap`, formula at `Formula/vaultwright.rb`
  (`brew tap alexey-lapin/tap`).
- **Scoop** → `alexey-lapin/scoop-bucket`, manifest `vaultwright.json` at the repo **root**
  (`scoop bucket add alexey-lapin https://github.com/alexey-lapin/scoop-bucket`).

Those pushes are cross-repo, so the default `GITHUB_TOKEN` (scoped to `vaultwright`) can't
do them. Least-privilege, one token per bucket:

- **Release on `vaultwright`** → default `GITHUB_TOKEN` (`contents: write`).
- **Formula push** → `TAP_GITHUB_TOKEN`, a **fine-grained PAT** scoped to only
  `alexey-lapin/homebrew-tap`, **Contents: Read and write** (`brews[].repository.token`).
- **Manifest push** → `SCOOP_GITHUB_TOKEN`, same shape but scoped to
  `alexey-lapin/scoop-bucket` (`scoops[].repository.token`).

Setup (one-time, manual): create each fine-grained PAT, add the matching Actions secret.
Fine-grained PATs expire in ≤1 year → calendar a rotation reminder.

## Before / after inventory

Deleted:
- `release.yml` steps: `Publish release` (`gh release create` + rename loop + hand-rolled
  `checksums.txt`) and `Update Homebrew formula via PR` (the whole PR/merge dance).
- `scripts/update-formula.sh`.
- `Formula/vaultwright.rb` (this repo) — now GoReleaser-authored in `homebrew-tap`.

Also deleted:
- `scripts/build-release.sh` — GoReleaser compiles the CLIs natively now.
- `scripts/hooks/pre-commit` + the `make install-hooks` target — obsolete once stubs are
  git-ignored (nothing to guard against committing).
- All 12 committed placeholder stubs (`internal/builtin/stubs/` is now git-ignored).

Changed `internal/builtin` (embed_stubs build-tag refactor):
- `builtin.go` — drop `embed.FS` + `IsPlaceholder`; host-only, non-empty `EmbeddedStub`.
- Added `stub_<os>_<arch>.go` (×6, `embed_stubs`-gated) + `stub_fallback.go` (empty stubs).
- `internal/stubs/stubs.go` — drop the now-dead placeholder check (same `EmbeddedStub` API).

Kept, lightly touched:
- `scripts/build-stubs.sh` — add `windows/arm64`; default `OUT` `dist` → `build`.
- `scripts/stage-embed.sh` — `mkdir -p` the now-ignored stubs dir before copying.
- `.github/release.yml` — unchanged; consumed by `changelog: use: github-native`.
- `.github/workflows/ci.yml` — bump action versions.
- `Makefile` — build CLI with `-tags embed_stubs`; simpler `clean`; drop `install-hooks`.
- `.gitignore` — ignore `build/` and `internal/builtin/stubs/`.

Added:
- `.goreleaser.yaml`, `scripts/stage-embed.sh`, `scripts/stage-release-assets.sh`.
- `internal/builtin/stub_<os>_<arch>.go` (×6), `stub_fallback.go`.
- `release.yml` rewritten as reusable (push:tags + workflow_call).
- `release-dispatch.yml` (manual bump/version → push tag → call release.yml).
- Secret `TAP_GITHUB_TOKEN`; formula lives in `homebrew-tap`.

Docs updated:
- `README.md`: tap command → `brew tap alexey-lapin/tap`; prebuilt-binary section →
  download + extract the archive (`tar xzf` / unzip) + `checksums.txt`. Archives now carry
  the version in their name (idiomatic GoReleaser), so `releases/latest/download/<asset>`
  is no longer a stable URL — point readers at the latest-release **page** (or
  `gh release download`) and have them pick the asset, without hardcoding a version.
- `CLAUDE.md` "Releases" section → describe the GoReleaser flow, the two workflows, the tap
  repo + PAT; drop `update-formula.sh` / `Formula/` references and the formula-PR rationale.
- `SECURITY.md` — review release/verification wording (SHA256SUMS trust root unchanged; CLI
  `checksums.txt` still present under archive names).

## Testing / validation

- **Local, no tag** (done): `goreleaser release --snapshot --clean` runs the scripts +
  **parallel** native builds + packaging end-to-end. Verified: 6 archives (tar.gz/zip,
  version in name) + `checksums.txt`; 12 flat `<role>-<os>_<arch>.stub` + `SHA256SUMS`
  staged for `extra_files`; a correct Homebrew formula; the host archive's binary reports
  `vaultwright v0.0.3`, an `--offline` **host** seal succeeds, and an `--offline` **non-host**
  seal fails with `not embedded … and offline` (proves each binary embeds only its host
  stub). Cross-compiles for all 6 targets. Also verified: a **default** `go build`/`go test`
  (no `embed_stubs` tag) compiles with **zero stub files present**, and `make` builds a
  tagged host CLI that seals offline. `goreleaser check` reports config valid (non-zero only
  on the `brews` deprecation). A local snapshot leaves only the git-ignored `stubs/` dir plus
  a modified `internal/builtin/SHA256SUMS` (restore with `git checkout --`).
- **First real release**: cut a low patch via `release-dispatch.yml` (`bump=patch`) and
  verify categorized notes match `.github/release.yml`, the formula lands in `homebrew-tap`,
  `brew tap alexey-lapin/tap && brew install vaultwright` works, and an end-to-end `seal`
  that downloads a non-host stub (verifies stub asset naming + trust root line up on the
  real release).

## Risks / open questions

- **Bucket token secrets** — `TAP_GITHUB_TOKEN` (Contents: RW on `homebrew-tap`) and
  `SCOOP_GITHUB_TOKEN` (Contents: RW on `scoop-bucket`) must exist before a release, or the
  respective `brews`/`scoops` push fails.
- **First-tag changelog** — `github-native` on the very first release has no prior tag;
  confirm it degrades gracefully (full history vs. empty).
- **`brews` deprecation** — functional today; a future GoReleaser may remove it, at which
  point migrate to `homebrew_casks` (changes install to a cask + quarantine hook).
```
