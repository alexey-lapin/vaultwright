# vaultwright build.
#
# The host stubs must be compiled before vaultwright, because vaultwright embeds them.
# `make` builds the host (GOOS/GOARCH) stubs into internal/builtin/stubs/<role>/, then
# builds vaultwright. Non-host stubs are fetched on demand at seal time.

GOFLAGS  := -trimpath
LDFLAGS  := -s -w
VERSION  ?= dev
VERPKG   := github.com/alexey-lapin/vaultwright/internal/builtin
STUBDIR  := internal/builtin/stubs
GOOS     ?= $(shell go env GOOS)
GOARCH   ?= $(shell go env GOARCH)
HOST     := $(GOOS)_$(GOARCH)
# NOTE: no backticks — they would be shell command substitution in the recipe.
PLACEHOLDER := placeholder - run make to build this stub

.PHONY: all stubs vaultwright stubs-matrix clean test vet fmt-check install-hooks

all: vaultwright

# Point git at the version-controlled hooks (blocks committing built stubs).
install-hooks:
	git config core.hooksPath scripts/hooks
	@echo "git hooks installed (core.hooksPath=scripts/hooks)"

stubs:
	rm -f $(STUBDIR)/vault/$(HOST).stub $(STUBDIR)/warden/$(HOST).stub
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(STUBDIR)/vault/$(HOST).stub ./cmd/vault
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(STUBDIR)/warden/$(HOST).stub ./cmd/warden

vaultwright: stubs
	go build $(GOFLAGS) -ldflags "-X $(VERPKG).Version=$(VERSION)" -o bin/vaultwright ./cmd/vaultwright

# Cross-compile the full stub matrix + SHA-256 manifest into build/ (see plan §13).
stubs-matrix:
	./scripts/build-stubs.sh

test:
	go test ./...

vet:
	go vet ./...

fmt-check:
	test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)

clean:
	rm -rf bin dist build
	printf '%s\n' "$(PLACEHOLDER)" > $(STUBDIR)/vault/$(HOST).stub
	printf '%s\n' "$(PLACEHOLDER)" > $(STUBDIR)/warden/$(HOST).stub
