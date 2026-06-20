# vaultwright build.
#
# The stubs must be compiled before vaultwright, because vaultwright embeds them. `make`
# builds the darwin/arm64 stubs into internal/builtin, then builds vaultwright.

GOFLAGS  := -trimpath
LDFLAGS  := -s -w
STUBDIR  := internal/builtin
GOOS     ?= darwin
GOARCH   ?= arm64

.PHONY: all stubs vaultwright clean test

all: vaultwright

stubs:
	rm -f $(STUBDIR)/vault.stub $(STUBDIR)/warden.stub
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(STUBDIR)/vault.stub ./cmd/vault
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(STUBDIR)/warden.stub ./cmd/warden

vaultwright: stubs
	go build $(GOFLAGS) -o bin/vaultwright ./cmd/vaultwright

test:
	go test ./...

clean:
	rm -rf bin
	printf 'placeholder: run `make` to build the real stub\n' > $(STUBDIR)/vault.stub
	printf 'placeholder: run `make` to build the real stub\n' > $(STUBDIR)/warden.stub
