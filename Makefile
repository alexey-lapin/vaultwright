# vaultwright build.
#
# The stubs must be compiled before vaultwright, because vaultwright embeds them. `make`
# builds the darwin/arm64 stubs into internal/builtin, then builds vaultwright.

GOFLAGS  := -trimpath
LDFLAGS  := -s -w
STUBDIR  := internal/builtin
GOOS     ?= darwin
GOARCH   ?= arm64

.PHONY: all stubs vaultwright stubs-matrix clean test vet fmt-check

all: vaultwright

stubs:
	rm -f $(STUBDIR)/vault.stub $(STUBDIR)/warden.stub
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(STUBDIR)/vault.stub ./cmd/vault
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(STUBDIR)/warden.stub ./cmd/warden

vaultwright: stubs
	go build $(GOFLAGS) -o bin/vaultwright ./cmd/vaultwright

# Cross-compile the full stub matrix + SHA-256 manifest into dist/ (see plan §13).
stubs-matrix:
	./scripts/build-stubs.sh

test:
	go test ./...

vet:
	go vet ./...

fmt-check:
	test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)

clean:
	rm -rf bin dist
	printf 'placeholder: run `make` to build the real stub\n' > $(STUBDIR)/vault.stub
	printf 'placeholder: run `make` to build the real stub\n' > $(STUBDIR)/warden.stub
