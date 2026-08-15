GO ?= go
VERSION ?= 0.1.0
LDFLAGS = -s -w -X main.version=$(VERSION)
PLUGIN_DIR = plugins/signposts
BIN_DIR = $(PLUGIN_DIR)/bin

.PHONY: test build build-all code-mode-proof clean

test:
	$(GO) test ./...

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/signposts-$$($(GO) env GOOS)-$$($(GO) env GOARCH) ./cmd/signposts

build-all:
	mkdir -p $(BIN_DIR)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/signposts-darwin-arm64 ./cmd/signposts
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/signposts-darwin-amd64 ./cmd/signposts
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/signposts-linux-arm64 ./cmd/signposts
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/signposts-linux-amd64 ./cmd/signposts
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/signposts-windows-arm64.exe ./cmd/signposts
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/signposts-windows-amd64.exe ./cmd/signposts

code-mode-proof: build
	$(PLUGIN_DIR)/scripts/test-code-mode

clean:
	rm -f $(BIN_DIR)/signposts-*
