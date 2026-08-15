GO ?= go
VERSION ?= 0.1.0
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: test build build-all code-mode-proof clean

test:
	$(GO) test ./...

build:
	mkdir -p bin
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o bin/signposts-$$($(GO) env GOOS)-$$($(GO) env GOARCH) ./cmd/signposts

build-all:
	mkdir -p bin
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o bin/signposts-darwin-arm64 ./cmd/signposts
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o bin/signposts-darwin-amd64 ./cmd/signposts
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o bin/signposts-linux-arm64 ./cmd/signposts
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o bin/signposts-linux-amd64 ./cmd/signposts
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o bin/signposts-windows-arm64.exe ./cmd/signposts
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o bin/signposts-windows-amd64.exe ./cmd/signposts

code-mode-proof: build
	scripts/test-code-mode

clean:
	rm -f bin/signposts-*
