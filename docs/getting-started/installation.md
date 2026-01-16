---
title: "Installation"
description: "Download and install Cbox Init for your platform in minutes"
weight: 3
---

# Installation

Cbox Init is distributed as a single static binary with zero dependencies, making installation straightforward.

## Download Pre-Built Binary

### Latest Release

Download the latest version from GitHub releases:

```bash
# Linux AMD64
wget https://github.com/cboxdk/init/releases/latest/download/cbox-init-linux-amd64
chmod +x cbox-init-linux-amd64
mv cbox-init-linux-amd64 /usr/local/bin/cbox-init

# Linux ARM64
wget https://github.com/cboxdk/init/releases/latest/download/cbox-init-linux-arm64
chmod +x cbox-init-linux-arm64
mv cbox-init-linux-arm64 /usr/local/bin/cbox-init

# macOS AMD64
wget https://github.com/cboxdk/init/releases/latest/download/cbox-init-darwin-amd64
chmod +x cbox-init-darwin-amd64
mv cbox-init-darwin-amd64 /usr/local/bin/cbox-init

# macOS ARM64 (Apple Silicon)
wget https://github.com/cboxdk/init/releases/latest/download/cbox-init-darwin-arm64
chmod +x cbox-init-darwin-arm64
mv cbox-init-darwin-arm64 /usr/local/bin/cbox-init
```

### Verify Installation

```bash
cbox-init --version
# Output: Cbox Init v1.0.0
```

## Docker Installation

### Using Official Image (Recommended)

```dockerfile
FROM cboxdk/init:latest AS cbox

FROM php:8.3-fpm-alpine

# Copy cbox-init binary
COPY --from=cbox /usr/local/bin/cbox-init /usr/local/bin/cbox-init

# Copy configuration
COPY cbox-init.yaml /etc/cbox-init/cbox-init.yaml

ENTRYPOINT ["/usr/local/bin/cbox-init"]
```

### Download in Dockerfile

```dockerfile
FROM php:8.3-fpm-alpine

# Install Cbox Init
RUN wget -O /usr/local/bin/cbox-init \
    https://github.com/cboxdk/init/releases/latest/download/cbox-init-linux-amd64 \
    && chmod +x /usr/local/bin/cbox-init

# Copy configuration
COPY cbox-init.yaml /etc/cbox-init/cbox-init.yaml

ENTRYPOINT ["/usr/local/bin/cbox-init"]
```

## Build from Source

### Prerequisites

- Go 1.23 or later
- Git

### Clone and Build

```bash
# Clone repository
git clone https://github.com/cboxdk/init.git
cd cbox-init

# Build for current platform
make build

# Binary created at: build/cbox-init
./build/cbox-init --version

# Build for all platforms
make build-all

# Binaries created in build/ directory:
# - cbox-init-linux-amd64
# - cbox-init-linux-arm64
# - cbox-init-darwin-amd64
# - cbox-init-darwin-arm64
```

### Build Options

```bash
# Development build (includes debug symbols)
make dev

# Run tests
make test

# Clean build artifacts
make clean

# Install dependencies
make deps
```

## Configuration Setup

Create a basic configuration file:

```bash
# Create directory
sudo mkdir -p /etc/cbox-init

# Create minimal configuration
cat > /etc/cbox-init/cbox-init.yaml <<EOF
version: "1.0"

global:
  shutdown_timeout: 30
  log_level: info

processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    restart: always
EOF
```

## Verify Installation

Test your installation with a simple configuration:

```bash
# Run with explicit config path
cbox-init --config cbox-init.yaml

# Or use environment variable
CBOX_INIT_CONFIG=cbox-init.yaml cbox-init

# Or use default location
# Cbox Init looks for config in order:
# 1. CBOX_INIT_CONFIG env var
# 2. /etc/cbox-init/cbox-init.yaml
# 3. ./cbox-init.yaml (current directory)
```

## Platform Support

| Platform | Architecture | Status |
|----------|--------------|--------|
| Linux | AMD64 | ✅ Full Support |
| Linux | ARM64 | ✅ Full Support |
| macOS | AMD64 | ✅ Full Support |
| macOS | ARM64 | ✅ Full Support |
| Windows | - | ❌ Not Supported |

## System Requirements

**Minimum**
- 512MB RAM
- 50MB disk space
- Linux kernel 3.10+ or macOS 10.15+

**Recommended**
- 1GB+ RAM (depends on managed processes)
- 100MB disk space
- Recent Linux kernel (5.x+) or macOS 12+

## Next Steps

Now that Cbox Init is installed, proceed to:

- [Quick Start](quickstart) - Run your first multi-process setup
- [Docker Integration](docker-integration) - Use Cbox Init as PID 1
- [Configuration](../configuration/overview) - Learn about configuration options
