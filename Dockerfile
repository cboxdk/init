# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-w -s -X main.version=${VERSION}" \
    -o cbox-init \
    ./cmd/cbox-init

# Runtime stage
FROM alpine:3.24

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata

# Create non-root user
RUN addgroup -g 1000 cbox && \
    adduser -D -u 1000 -G cbox cbox

# Copy binary from builder
COPY --from=builder /build/cbox-init /usr/local/bin/cbox-init
RUN chmod +x /usr/local/bin/cbox-init

# Set up directories
RUN mkdir -p /etc/cbox-init && \
    chown -R cbox:cbox /etc/cbox-init

# Switch to non-root user
USER cbox

# Expose ports
EXPOSE 9090 9180

# Health check
HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
    CMD wget -q -O- http://localhost:9180/api/v1/health || exit 1

# Run cbox-init
ENTRYPOINT ["/usr/local/bin/cbox-init"]
