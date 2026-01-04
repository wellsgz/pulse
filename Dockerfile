# Build stage
FROM golang:1.21-bookworm AS builder

# Install librrd for CGO
RUN apt-get update && apt-get install -y librrd-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build with CGO enabled for RRD support
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o pulse ./cmd/pulse

# Runtime stage
FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    librrd8 \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Create directories
RUN mkdir -p /etc/pulse /var/lib/pulse

# Copy binary
COPY --from=builder /app/pulse /usr/bin/pulse

# Copy default config
COPY config.example.yaml /etc/pulse/config.yaml

# Data directory
VOLUME /var/lib/pulse

# API port
EXPOSE 8080

# Run in daemon mode (headless)
ENTRYPOINT ["/usr/bin/pulse"]
CMD ["daemon", "--headless"]
