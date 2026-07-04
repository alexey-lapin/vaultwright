## Summary

<!--
What does this change and why? PRs squash-merge, so the PR *title* becomes the commit
subject and the release-note line — keep it imperative and scoped, e.g.
"serve: fix idle-timeout race". No `Co-Authored-By` trailers.
-->

## Details

-

## Checklist

- [ ] `gofmt`, `go vet`, `go build ./...`, `go test ./...` pass (CI runs these)
- [ ] No real stub binaries staged — the pre-commit hook guards this (`make install-hooks` if unset)
- [ ] Labeled for release notes: `enhancement` / `bug` / `documentation` / `dependencies`
