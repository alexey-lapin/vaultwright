# vaultwright build.
#
# The host stubs must be compiled before vaultwright, because vaultwright embeds them
# (under -tags embed_stubs). `make` builds the host (GOOS/GOARCH) stubs into
# internal/builtin/stubs/<role>/ (git-ignored), then builds vaultwright with that tag.
# Non-host stubs are fetched on demand at seal time.

GOFLAGS  := -trimpath
LDFLAGS  := -s -w
VERSION  ?= dev
VERPKG   := github.com/alexey-lapin/vaultwright/internal/builtin
STUBDIR  := internal/builtin/stubs
GOOS     ?= $(shell go env GOOS)
GOARCH   ?= $(shell go env GOARCH)
HOST     := $(GOOS)_$(GOARCH)

.PHONY: all stubs vaultwright stubs-matrix clean test vet fmt-check

all: vaultwright

stubs:
	mkdir -p $(STUBDIR)/vault $(STUBDIR)/warden
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(STUBDIR)/vault/$(HOST).stub ./cmd/vault
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(STUBDIR)/warden/$(HOST).stub ./cmd/warden

vaultwright: stubs
	go build $(GOFLAGS) -tags embed_stubs -ldflags "-X $(VERPKG).Version=$(VERSION)" -o bin/vaultwright ./cmd/vaultwright

# Cross-compile the full stub matrix + SHA-256 manifest into build/.
stubs-matrix:
	./scripts/build-stubs.sh

test:
	go test ./...

vet:
	go vet ./...

fmt-check:
	test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)

clean:
	rm -rf bin dist build $(STUBDIR)
