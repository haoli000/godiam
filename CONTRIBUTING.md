# Contributing to godiam

Thank you for your interest in contributing! Here are guidelines to help you
get started.

## Development Setup

1. **Go 1.23+** is required.
2. Clone the repository:
   ```bash
   git clone https://github.com/haoli000/godiam.git
   cd godiam
   ```
3. Install dependencies:
   ```bash
   go mod download
   ```
4. (Optional) Install [golangci-lint](https://golangci-lint.run/welcome/install/)
   for local linting.

## Making Changes

1. Fork the repository and create a feature branch from `main`.
2. Write clear, concise commit messages.
3. Add or update tests for any changed behavior.
4. Run the quality checks before submitting:
   ```bash
   make check   # runs fmt, tidy, vet, lint, unit-test
   ```

## Pull Requests

- Keep PRs focused on a single change.
- Reference any related issues in the PR description.
- All CI checks must pass before merging.
- Maintainers may request changes; please address feedback promptly.

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`).
- All exported symbols must have doc comments.
- Error messages should be lowercase without trailing punctuation.
- Every `.go` file must include the SPDX license header:
  ```go
  // Copyright (c) 2026 Hao Li
  // SPDX-License-Identifier: BSD-3-Clause
  ```

## Testing

```bash
# Unit tests with race detector
go test -race ./...

# Integration tests (require Docker)
cd tests/dra && make build && make test
```

## Reporting Bugs

Open a [GitHub issue](https://github.com/haoli000/godiam/issues) with:
- Go version (`go version`)
- OS and architecture
- Steps to reproduce
- Expected vs. actual behavior

## License

By contributing you agree that your contributions will be licensed under the
[BSD 3-Clause License](LICENSE).
