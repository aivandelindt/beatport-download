APP     := beatportdl-ui
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -trimpath -ldflags="-s -w -X main.version=$(VERSION)"
DIST    := dist

.PHONY: all macos windows linux docker clean run tidy mcp-server audio-analyzer-mcp audio-analyzer-cli stem-splitter

all: macos windows linux

mcp-server:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(DIST)/beatport-download-mcp ./cmd/mcp-server
	@echo "✓  $(DIST)/beatport-download-mcp"

audio-analyzer-mcp:
	cargo build --release --manifest-path third_party/audio-analyzer-rs/Cargo.toml --bin mcp-server
	@echo "✓  third_party/audio-analyzer-rs/target/release/mcp-server"

audio-analyzer-cli:
	cargo build --release --manifest-path third_party/audio-analyzer-rs/Cargo.toml --bin cli
	@echo "✓  third_party/audio-analyzer-rs/target/release/cli"

stem-splitter:
	@mkdir -p dist/tools
	cargo install stem-splitter-core --version 1.2.0 --locked --root dist/tools --force
	@echo "✓  dist/tools/bin/stem-splitter"

# ── macOS ──────────────────────────────────────────────────────────────────────
macos: macos-arm64 macos-amd64

macos-arm64:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 \
		go build $(LDFLAGS) -o $(DIST)/$(APP)-macos-arm64 .
	@echo "✓  $(DIST)/$(APP)-macos-arm64"

macos-amd64:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 \
		go build $(LDFLAGS) -o $(DIST)/$(APP)-macos-amd64 .
	@echo "✓  $(DIST)/$(APP)-macos-amd64"

macos-universal: macos-arm64 macos-amd64
	lipo -create -output $(DIST)/$(APP)-macos-universal \
		$(DIST)/$(APP)-macos-arm64 \
		$(DIST)/$(APP)-macos-amd64
	@echo "✓  $(DIST)/$(APP)-macos-universal (universal binary)"

# ── Windows ────────────────────────────────────────────────────────────────────
windows:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
		go build $(LDFLAGS) -o $(DIST)/$(APP)-windows-amd64.exe .
	@echo "✓  $(DIST)/$(APP)-windows-amd64.exe"

windows-arm64:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 \
		go build $(LDFLAGS) -o $(DIST)/$(APP)-windows-arm64.exe .
	@echo "✓  $(DIST)/$(APP)-windows-arm64.exe"

# ── Linux ──────────────────────────────────────────────────────────────────────
linux:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build $(LDFLAGS) -o $(DIST)/$(APP)-linux-amd64 .
	@echo "✓  $(DIST)/$(APP)-linux-amd64"

linux-arm64:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
		go build $(LDFLAGS) -o $(DIST)/$(APP)-linux-arm64 .
	@echo "✓  $(DIST)/$(APP)-linux-arm64"

# ── Docker ─────────────────────────────────────────────────────────────────────
docker:
	docker build -t $(APP):$(VERSION) -t $(APP):latest .
	@echo "✓  Docker image $(APP):$(VERSION)"

docker-run:
	docker compose up

# ── Dev ────────────────────────────────────────────────────────────────────────
run:
	go run .

tidy:
	go mod tidy

clean:
	rm -rf $(DIST)

# ── Release zip ────────────────────────────────────────────────────────────────
release: all
	cd $(DIST) && \
	zip $(APP)-macos-arm64.zip $(APP)-macos-arm64 && \
	zip $(APP)-macos-amd64.zip $(APP)-macos-amd64 && \
	zip $(APP)-windows-amd64.zip $(APP)-windows-amd64.exe && \
	zip $(APP)-linux-amd64.zip $(APP)-linux-amd64
	@echo "✓  Release zips in $(DIST)/"
