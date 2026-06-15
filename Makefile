# cypembed build.
#
# The stubs must be compiled before forge, because forge embeds them. `make`
# builds the darwin/arm64 stubs into internal/forgeasset, then builds forge.

GOFLAGS  := -trimpath
LDFLAGS  := -s -w
STUBDIR  := internal/forgeasset
GOOS     ?= darwin
GOARCH   ?= arm64

.PHONY: all stubs forge clean test

all: forge

stubs:
	rm -f $(STUBDIR)/vault.stub $(STUBDIR)/warden.stub
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(STUBDIR)/vault.stub ./cmd/vault
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(STUBDIR)/warden.stub ./cmd/warden

forge: stubs
	go build $(GOFLAGS) -o bin/forge ./cmd/forge

test:
	go test ./...

clean:
	rm -rf bin
	printf 'placeholder: run `make` to build the real stub\n' > $(STUBDIR)/vault.stub
	printf 'placeholder: run `make` to build the real stub\n' > $(STUBDIR)/warden.stub
