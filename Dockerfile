# ─── Build Stage ──────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod ./
RUN go mod download || true

COPY . .
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o beatportdl-ui .

# ─── Runtime Stage ────────────────────────────────────────────────────────────
FROM alpine:3.19

RUN apk add --no-cache ffmpeg ca-certificates tzdata

WORKDIR /app
COPY --from=builder /src/beatportdl-ui .

RUN mkdir -p /config /downloads

VOLUME ["/config", "/downloads"]
EXPOSE 8989

ENV XDG_CONFIG_HOME=/config
ENV HOME=/config

ENTRYPOINT ["./beatportdl-ui", "--no-open", "--port", "8989"]
