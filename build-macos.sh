#!/bin/bash
# One-shot build script for macOS (arm64 + universal binary)
set -e

# ── Install Go if needed ──────────────────────────────────────────────────────
if ! command -v go &>/dev/null; then
  echo "Go not found. Installing via Homebrew..."
  if ! command -v brew &>/dev/null; then
    echo "Homebrew also not found. Install from https://brew.sh first."
    exit 1
  fi
  brew install go
fi

echo "Go $(go version)"

# ── Install ffmpeg if needed ──────────────────────────────────────────────────
if ! command -v ffmpeg &>/dev/null; then
  echo "ffmpeg not found. Installing via Homebrew..."
  brew install ffmpeg
fi

# ── Build ─────────────────────────────────────────────────────────────────────
cd "$(dirname "$0")"
go mod tidy

ARCH=$(uname -m)
mkdir -p dist

if [[ "$ARCH" == "arm64" ]]; then
  echo "Building native arm64 binary..."
  CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/beatportdl-ui .
  echo "✓  dist/beatportdl-ui  (arm64)"
else
  echo "Building native amd64 binary..."
  CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/beatportdl-ui .
  echo "✓  dist/beatportdl-ui  (amd64)"
fi

echo ""
echo "Run with:  ./dist/beatportdl-ui"
echo "Then open: http://localhost:8989"
