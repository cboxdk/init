---
title: "Development"
description: "Development guides for Cbox Init testing and contribution"
weight: 50
---

# Development

Development guides and resources for contributing to Cbox Init.

## Contents

- [Testing](testing) - Testing guide

## Getting Started

### Prerequisites

- Go 1.23+
- Docker (for integration testing)

### Development Workflow

```bash
# Backend development
make dev

# Run tests
make test              # Go tests with race detection
```

## Project Structure

```
cbox-init/
├── cmd/cbox-init/      # Main entry point
├── internal/           # Go packages
│   ├── config/        # Configuration
│   ├── process/       # Process management
│   ├── signals/       # Signal handling
│   ├── logger/        # Logging
│   ├── api/           # REST API server
│   ├── tui/           # Terminal UI
│   ├── metrics/       # Resource metrics
│   └── tracing/       # OpenTelemetry tracing
└── docs/             # Documentation
```

## Contributing

We welcome contributions! Please:

1. Fork the repository
2. Create a feature branch
3. Write tests for new features
4. Ensure all tests pass
5. Submit a pull request

## Code Standards

### Go

- Follow standard Go formatting (`gofmt`)
- Write tests for new features (aim for >80% coverage)
- Use structured logging with `slog`
- Handle errors explicitly
- Use interfaces for testability

## Testing

All new features must include tests:

- **Unit tests**: In `_test.go` files alongside source
- **Integration tests**: In `tests/integration/`

See [Testing Guide](testing) for detailed information.
