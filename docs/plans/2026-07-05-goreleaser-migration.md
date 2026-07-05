# Migrate the release process to GoReleaser

Status: design (approved for planning)
Date: 2026-07-05
Related: the main plan is `docs/plans/2026-06-14-vaultwright.md` (§13 = multi-target stub
distribution, the constraint that shapes this migration).

## Goal

Replace the hand-rolled release scripting with GoReleaser to get **fewer custom moving
parts** and **native Homebrew automation**. Specifically, delete the `gh release create`
+ stub-rename loop and the `release/formula-<ver>` PR-merge dance, and let GoReleaser own
packaging, the GitHub release, and the tap push.

Non-goals: no change to the crypto/handshake, the stub download/verify protocol, or the
`internal/builtin` / `internal/stubs` runtime behavior. GoReleaser is a packager/publisher
here, not the compiler (see "Why prebuilt").

## The constraint that shapes everything

Each per-host `vaultwright` CLI embeds **different bytes**: only its host's
`vault`/`warden` stubs (`//go:embed stubs`), the full `SHA256SUMS` manifest, and the
version via `-ldflags -X ...builtin.Version`. So the build is inherently two-phase and
mutates `internal/builtin/stubs/` per target. GoReleaser's normal model — one source tree,
compile a matrix with ldflags — cannot express per-target source mutation.

### Why prebuilt (not native builds, not restructuring)

We keep `scripts/build-stubs.sh` + `scripts/build-release.sh` (they already produce exactly
the right per-host binaries + manifest) and point GoReleaser at them via
`builder: prebuilt`. Rejected alternatives:

- **Native builds with per-target hooks**: one `build` entry per target with a pre-hook
  staging stubs. Re-implements the fragile embedding inside GoReleaser config, forces
  serial builds (shared source dir race), produces identical binaries. No upside.
- **Restructure embedding** (always-download or embed-all): a product/threat-model change
  touching `internal/builtin` + `internal/stubs`, solving a problem `prebuilt` sidesteps
  for free. Out of scope.

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
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }         # need tags/history to resolve + refuse-existing
      - id: resolve
        run: |                            # existing bump/exact/validate/refuse-existing logic
          ...
          echo "version=$VERSION" >> "$GITHUB_OUTPUT"
      - run: |
          git tag "$VERSION" && git push origin "$VERSION"   # GITHUB_TOKEN → no recursion
  release:
    needs: tag
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
concurrency: { group: release-${{ inputs.tag || github.ref_name }}, cancel-in-progress: false }
jobs:
  goreleaser:
    runs-on: ubuntu-latest
    permissions: { contents: write }
    steps:
      - uses: actions/checkout@v4
        with: { ref: ${{ inputs.tag || github.ref }}, fetch-depth: 0 }
      - uses: actions/setup-go@v5
        with: { go-version: "1.26" }
      - uses: goreleaser/goreleaser-action@v6
        with: { version: "~> v2", args: "release --clean" }
        env:
          GITHUB_TOKEN:     ${{ secrets.GITHUB_TOKEN }}      # release on this repo
          TAP_GITHUB_TOKEN: ${{ secrets.TAP_GITHUB_TOKEN }}  # formula push to homebrew-tap
```

### `.goreleaser.yaml`

```yaml
version: 2

before:
  hooks:
    - ./scripts/build-stubs.sh                 # 10 stub binaries + SHA256SUMS (trust root)
    - cmd: ./scripts/build-release.sh          # 6 per-host CLIs (embed stubs + manifest + version)
      env: [VERSION={{ .Tag }}]
    - ./scripts/stage-release-assets.sh        # flat stub names + SHA256SUMS → dist/assets/

builds:
  - id: vaultwright
    builder: prebuilt                          # adopt dist/ binaries; GoReleaser does NOT compile
    binary: vaultwright                         # in-archive name
    goos:   [darwin, linux, windows]
    goarch: [amd64, arm64]                       # full matrix incl. windows/arm64
    prebuilt:
      path: dist/vaultwright-{{ .Os }}-{{ .Arch }}{{ if eq .Os "windows" }}.exe{{ end }}

archives:
  - id: vaultwright
    formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
    # No custom name_template → GoReleaser's idiomatic default,
    # e.g. vaultwright_0.0.4_darwin_arm64.tar.gz (version in the name).

checksum:
  name_template: checksums.txt                 # CLI-archive checksums; NOT the stub trust root

changelog:
  use: github-native                           # GitHub's notes API → honors .github/release.yml

release:
  extra_files:
    - glob: dist/assets/*.stub                 # vault-darwin_arm64.stub, warden-windows_arm64.stub, …
    - glob: dist/assets/SHA256SUMS             # stub trust root, downloaded + verified by the CLI

brews:
  - repository:
      owner: alexey-lapin
      name:  homebrew-tap
      token: "{{ .Env.TAP_GITHUB_TOKEN }}"     # fine-grained PAT, NOT the default token
    homepage:    "https://github.com/alexey-lapin/vaultwright"
    description: "Build an encrypted, embedded static-file server from a single binary"
    license:     "Apache-2.0"
    skip_upload: auto                          # skip formula push for prereleases
    install: |
      bin.install "vaultwright"
    test: |
      assert_match "vaultwright", shell_output("#{bin}/vaultwright version")
```

Notes:
- `github-native` disables GoReleaser's own grouping/sort — fine, categorization lives in
  `.github/release.yml` (kept).
- `windows/arm64` is included in the CLI matrix; `brews` auto-skips non-darwin/linux, so no
  Windows in Homebrew.
- `VERSION={{ .Tag }}` gives `build-release.sh` the `vX.Y.Z` it bakes into
  `-X ...builtin.Version`. Prebuilt binaries are already compiled, so GoReleaser cannot
  inject ldflags — the version must be baked by `build-release.sh`, which it already does.

### Scripts

- `scripts/build-stubs.sh` — add `windows/arm64` to the default `VAULT_TARGETS` /
  `WARDEN_TARGETS`. Otherwise unchanged.
- `scripts/build-release.sh` — add `windows/arm64` to the default `CLI_TARGETS`. Still
  bakes the version via ldflags and restores the committed `builtin/` tree on exit.
- `scripts/stage-release-assets.sh` — **new, small.** Replaces the rename loop currently
  inline in `release.yml`: `extra_files` uploads by basename and cannot rename, and
  `vault/…` + `warden/…` basenames collide. It copies each `dist/stubs/<role>/<os>_<arch>.stub`
  to `dist/assets/<role>-<os>_<arch>.stub` and copies `dist/SHA256SUMS` to `dist/assets/`.

## Homebrew: dedicated tap + token

The formula moves out of this repo into the pre-created `alexey-lapin/homebrew-tap`
(`brew tap alexey-lapin/tap`). Because that's a separate, unprotected repo, GoReleaser
commits the generated formula straight to its default branch — no PR-merge workaround, no
ruleset bypass.

That push is cross-repo, so the default `GITHUB_TOKEN` (scoped to `vaultwright`) cannot do
it. Use **two tokens**, least-privilege:

- **Release on `vaultwright`** → default `GITHUB_TOKEN` (`contents: write`).
- **Formula push to `homebrew-tap`** → `TAP_GITHUB_TOKEN`, a **fine-grained PAT** scoped to
  only `alexey-lapin/homebrew-tap`, permission **Contents: Read and write**. Stored as an
  Actions secret in `vaultwright`. Referenced only via `brews[].repository.token`.

Setup (one-time, manual): create the fine-grained PAT, add the `TAP_GITHUB_TOKEN` secret.
Fine-grained PATs expire in ≤1 year → calendar a rotation reminder.

## Before / after inventory

Deleted:
- `release.yml` steps: `Publish release` (`gh release create` + rename loop + hand-rolled
  `checksums.txt`) and `Update Homebrew formula via PR` (the whole PR/merge dance).
- `scripts/update-formula.sh`.
- `Formula/vaultwright.rb` (this repo) — now GoReleaser-authored in `homebrew-tap`.

Kept, lightly touched:
- `scripts/build-stubs.sh`, `scripts/build-release.sh` — add `windows/arm64`.
- `.github/release.yml` — unchanged; consumed by `changelog: use: github-native`.
- `internal/builtin`, `internal/stubs`, `Makefile`, `scripts/hooks` — untouched.

Added:
- `.goreleaser.yaml`, `scripts/stage-release-assets.sh`.
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

- **Local, no tag**: `goreleaser release --snapshot --clean` runs both scripts + packaging
  end-to-end; `goreleaser check` validates config. Add these to local dev notes.
- **CI dry run**: a PR can run `goreleaser check` (advisory) alongside the existing
  `build-test`.
- **First real release**: cut a low patch (e.g. via `release-dispatch.yml` with
  `bump=patch`) and verify: 6 archives + `checksums.txt`, all `<role>-<os>_<arch>.stub` +
  `SHA256SUMS` present, categorized notes match `.github/release.yml`, and the formula lands
  in `homebrew-tap`. Then confirm `brew tap alexey-lapin/tap && brew install vaultwright`
  and an end-to-end `seal` that downloads a non-host stub (verifies stub asset naming +
  trust root still line up).

## Risks / open questions

- **`before.hooks` templated `env`** — confirm the installed GoReleaser v2 accepts the
  object hook form with `env: [VERSION={{ .Tag }}]`; if not, derive `VERSION` from
  `git describe` inside `build-release.sh`'s invocation.
- **Prebuilt + archive in-binary name** — confirm the archive contains the binary as
  `vaultwright` (set by `binary:`) so `bin.install "vaultwright"` matches.
- **First-tag changelog** — `github-native` on the very first release has no prior tag;
  confirm it degrades gracefully (full history vs. empty).
```
